package cmd

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
)

var cooldownTestNow = time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

func testPolicy(days int) cooldownPolicy {
	return cooldownPolicy{days: days, reason: "config", now: func() time.Time { return cooldownTestNow }}
}

func daysAgo(days int) time.Time {
	return cooldownTestNow.Add(-time.Duration(days) * 24 * time.Hour)
}

func TestResolveCooldownSettings(t *testing.T) {
	tests := []struct {
		name       string
		configDays int
		flagValue  string
		wantDays   int
		wantSource string
	}{
		{name: "config value when flag unset", configDays: 10, wantDays: 10, wantSource: "config"},
		{name: "explicit flag zero disables", configDays: 10, flagValue: "0", wantDays: 0, wantSource: "flag"},
		{name: "flag overrides config", configDays: 10, flagValue: "3", wantDays: 3, wantSource: "flag"},
		{name: "negative config clamps to zero", configDays: -5, wantDays: 0, wantSource: "config"},
	}

	for _, tt := range tests {
		cmd := &cobra.Command{}
		cmd.Flags().Int("cooldown", 0, "")
		if tt.flagValue != "" {
			if err := cmd.Flags().Set("cooldown", tt.flagValue); err != nil {
				t.Fatalf("%s: set cooldown: %v", tt.name, err)
			}
		}

		cfg := &config.AppConfig{Cooldown: tt.configDays, TrustedOwners: []string{"OwnerX"}}
		got := resolveCooldownSettings(cmd, cfg)
		if got.days != tt.wantDays || got.source != tt.wantSource {
			t.Fatalf("%s: resolveCooldownSettings() = {days: %d, source: %q}, want {days: %d, source: %q}", tt.name, got.days, got.source, tt.wantDays, tt.wantSource)
		}
		if len(got.trustedOwners) != 1 {
			t.Fatalf("%s: trustedOwners = %v, want config value carried through", tt.name, got.trustedOwners)
		}
	}
}

func TestResolveCooldownSettingsWithoutFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cfg := &config.AppConfig{Cooldown: 7}
	got := resolveCooldownSettings(cmd, cfg)
	if got.days != 7 || got.source != "config" {
		t.Fatalf("resolveCooldownSettings() = {days: %d, source: %q}, want config fallback on command without flag", got.days, got.source)
	}
}

func TestCooldownPolicyFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trusted []string
		repo    hostOwner
		want    bool
	}{
		{name: "bare entry on github.com", trusted: []string{"JakeTRogers"}, repo: hostOwner{host: "github.com", owner: "JakeTRogers"}, want: true},
		{name: "bare entry with empty host", trusted: []string{"JakeTRogers"}, repo: hostOwner{owner: "jaketrogers"}, want: true},
		{name: "bare entry not on ghe host", trusted: []string{"JakeTRogers"}, repo: hostOwner{host: "acme.ghe.com", owner: "JakeTRogers"}, want: false},
		{name: "untrusted owner", trusted: []string{"JakeTRogers"}, repo: hostOwner{host: "github.com", owner: "stranger"}, want: false},
		{name: "ghe entry on its host", trusted: []string{"acme.ghe.com/acme"}, repo: hostOwner{host: "acme.ghe.com", owner: "acme"}, want: true},
		{name: "ghe entry matches host case-insensitively", trusted: []string{"acme.ghe.com/acme"}, repo: hostOwner{host: "ACME.ghe.com", owner: "ACME"}, want: true},
		{name: "ghe entry not on github.com", trusted: []string{"acme.ghe.com/acme"}, repo: hostOwner{host: "github.com", owner: "acme"}, want: false},
		{name: "ghe entry not on other ghe host", trusted: []string{"acme.ghe.com/acme"}, repo: hostOwner{host: "other.ghe.com", owner: "acme"}, want: false},
		{name: "github.com entry", trusted: []string{"github.com/JakeTRogers"}, repo: hostOwner{owner: "JakeTRogers"}, want: true},
		{name: "uppercase github.com entry", trusted: []string{"GITHUB.COM/jaketrogers"}, repo: hostOwner{host: "github.com", owner: "JakeTRogers"}, want: true},
		{name: "www.github.com entry", trusted: []string{"www.github.com/JakeTRogers"}, repo: hostOwner{host: "github.com", owner: "JakeTRogers"}, want: true},
		{name: "github.com entry not on ghe host", trusted: []string{"github.com/JakeTRogers"}, repo: hostOwner{host: "acme.ghe.com", owner: "JakeTRogers"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := resolveCooldownSettings(&cobra.Command{}, &config.AppConfig{Cooldown: 10, TrustedOwners: tt.trusted})
			got := settings.policyFor(tt.repo)
			if trusted := got.days == 0; trusted != tt.want {
				t.Fatalf("policyFor(%+v) with trustedOwners %q days = %d, want trusted = %v", tt.repo, tt.trusted, got.days, tt.want)
			}
		})
	}
}

// captureWarnLogs routes the default slog logger to a buffer at warn level
// for the rest of the test. It swaps a global, so callers must run serially.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	oldLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return &logs
}

func TestResolveCooldownSettingsIgnoresInvalidTrustedOwners(t *testing.T) {
	logs := captureWarnLogs(t)

	invalid := []string{"a/b/c", "/owner", "acme.ghe.com/", "example.com/owner", "  "}
	cmd := &cobra.Command{}
	settings := resolveCooldownSettings(cmd, &config.AppConfig{Cooldown: 10, TrustedOwners: append(invalid, "cli")})

	repos := []hostOwner{
		{"github.com", "a"}, {"github.com", "b"}, {"github.com", "c"}, {"github.com", "owner"},
		{"acme.ghe.com", ""}, {"acme.ghe.com", "acme"}, {"example.com", "owner"}, {"github.com", ""},
	}
	for _, repo := range repos {
		if got := settings.policyFor(repo); got.days != 10 {
			t.Fatalf("policyFor(%+v) days = %d, want 10: invalid entries must not exempt anything", repo, got.days)
		}
	}
	if got := settings.policyFor(hostOwner{host: "github.com", owner: "cli"}); got.days != 0 {
		t.Fatalf("policyFor(github.com, cli) days = %d, want 0: valid entry alongside invalid ones must still apply", got.days)
	}
	if got := strings.Count(logs.String(), "ignoring invalid trustedOwners entry"); got != len(invalid) {
		t.Fatalf("logged %d invalid-entry warnings, want %d; logs:\n%s", got, len(invalid), logs.String())
	}
}

func TestCooldownPolicyForInvalidHostWarnsAndEnforcesCooldown(t *testing.T) {
	logs := captureWarnLogs(t)

	settings := resolveCooldownSettings(&cobra.Command{}, &config.AppConfig{Cooldown: 10})
	if got := settings.policyFor(hostOwner{host: "example.com", owner: "cli"}); got.days != 10 {
		t.Fatalf("policyFor(example.com, cli) days = %d, want 10: an unverifiable host must keep the cooldown", got.days)
	}
	if !strings.Contains(logs.String(), "enforcing cooldown") || !strings.Contains(logs.String(), "host=example.com") {
		t.Fatalf("logs = %q, want a warning naming host example.com", logs.String())
	}
}

func TestCooldownCheckRelease(t *testing.T) {
	policy := testPolicy(10)

	if err := policy.checkRelease(&github.Release{TagName: "v1.0.0", PublishedAt: daysAgo(30)}); err != nil {
		t.Fatalf("checkRelease(old release) error = %v, want nil", err)
	}

	err := policy.checkRelease(&github.Release{TagName: "v1.1.0", PublishedAt: daysAgo(2)})
	if err == nil || !strings.Contains(err.Error(), "2 day(s) old") || !strings.Contains(err.Error(), "--cooldown 0") {
		t.Fatalf("checkRelease(fresh release) error = %v, want age and override hint", err)
	}

	err = policy.checkRelease(&github.Release{TagName: "v1.2.0"})
	if err == nil || !strings.Contains(err.Error(), "no published date") {
		t.Fatalf("checkRelease(zero published) error = %v, want unverifiable-date error", err)
	}

	if err := testPolicy(0).checkRelease(&github.Release{TagName: "v1.1.0", PublishedAt: daysAgo(1)}); err != nil {
		t.Fatalf("checkRelease(disabled policy) error = %v, want nil", err)
	}

	if err := policy.checkRelease(&github.Release{TagName: "v1.3.0", PublishedAt: cooldownTestNow.Add(48 * time.Hour)}); err == nil {
		t.Fatal("checkRelease(future timestamp) error = nil, want blocked")
	}
}

func TestCooldownCheckReleaseTrustedOwnersHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo hostOwner
		want string
	}{
		{name: "github.com suggests bare owner", repo: hostOwner{owner: "cli"}, want: `add "cli" to trustedOwners`},
		{name: "ghe host suggests host/owner entry", repo: hostOwner{host: "ACME.ghe.com", owner: "acme"}, want: `add "acme.ghe.com/acme" to trustedOwners`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := resolveCooldownSettings(&cobra.Command{}, &config.AppConfig{Cooldown: 10})
			err := settings.policyFor(tt.repo).checkRelease(&github.Release{TagName: "v1.1.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("checkRelease() error = %v, want hint %q", err, tt.want)
			}
		})
	}
}

func TestCooldownCheckAsset(t *testing.T) {
	policy := testPolicy(10)
	rel := &github.Release{TagName: "v1.0.0", PublishedAt: daysAgo(60)}

	tests := []struct {
		name    string
		asset   github.Asset
		wantErr string
	}{
		{name: "asset updated recently after publish", asset: github.Asset{Name: "tool.tar.gz", UpdatedAt: daysAgo(1)}, wantErr: "tampering"},
		{name: "asset updated long ago", asset: github.Asset{Name: "tool.tar.gz", UpdatedAt: daysAgo(50)}, wantErr: ""},
		{name: "created-at fallback when updated missing", asset: github.Asset{Name: "tool.tar.gz", CreatedAt: daysAgo(1)}, wantErr: "tampering"},
		{name: "no timestamps", asset: github.Asset{Name: "tool.tar.gz"}, wantErr: ""},
		{name: "asset time before publish", asset: github.Asset{Name: "tool.tar.gz", UpdatedAt: daysAgo(70)}, wantErr: ""},
	}

	for _, tt := range tests {
		err := policy.checkAsset(rel, tt.asset)
		if tt.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: checkAsset() error = %v, want nil", tt.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("%s: checkAsset() error = %v, want substring %q", tt.name, err, tt.wantErr)
		}
	}

	if err := testPolicy(0).checkAsset(rel, github.Asset{Name: "tool.tar.gz", UpdatedAt: daysAgo(1)}); err != nil {
		t.Fatalf("checkAsset(disabled policy) error = %v, want nil", err)
	}
}

func TestFindCooldownFallback(t *testing.T) {
	policy := testPolicy(10)

	releases := []github.Release{
		{TagName: "v1.4.0", PublishedAt: daysAgo(2)},
		{TagName: "v1.4.0-rc1", PublishedAt: daysAgo(20), Prerelease: true},
		{TagName: "v1.3.9", PublishedAt: daysAgo(20), Draft: true},
		{TagName: "v1.3.5"},
		{TagName: "v1.3.2", PublishedAt: daysAgo(15)},
		{TagName: "v1.3.0", PublishedAt: daysAgo(40)},
	}

	got, err := findCooldownFallback(releases, "cli", "tool", policy)
	if err != nil {
		t.Fatalf("findCooldownFallback() error: %v", err)
	}
	if got.TagName != "v1.3.2" {
		t.Fatalf("findCooldownFallback() = %s, want v1.3.2 (first eligible, skipping fresh/prerelease/draft/undated)", got.TagName)
	}

	noneEligible := []github.Release{{TagName: "v1.0.0", PublishedAt: daysAgo(1)}}
	if _, err := findCooldownFallback(noneEligible, "cli", "tool", policy); err == nil || !strings.Contains(err.Error(), "satisfies the 10-day cooldown") {
		t.Fatalf("findCooldownFallback(none eligible) error = %v, want no-eligible-release error", err)
	}
}

func TestUpgradeFallbackIsNewer(t *testing.T) {
	// Most-recent-first publish order, mixing semver and date-based tags.
	releases := []github.Release{
		{TagName: "build-2026-07-04"},
		{TagName: "build-2026-07-03"},
		{TagName: "v1.4.0"},
		{TagName: "build-2026-06-20"},
		{TagName: "v1.3.2"},
	}

	tests := []struct {
		name       string
		fallback   string
		currentTag string
		want       bool
	}{
		{name: "same tag", fallback: "v1.3.2", currentTag: "v1.3.2", want: false},
		{name: "semver newer", fallback: "v1.4.0", currentTag: "v1.3.2", want: true},
		{name: "semver older", fallback: "v1.3.2", currentTag: "v1.4.0", want: false},
		{name: "semver older than installed prerelease", fallback: "v1.4.0", currentTag: "v1.5.0-rc.1", want: false},
		{name: "semver release of installed prerelease", fallback: "v1.4.0", currentTag: "v1.4.0-rc.1", want: true},
		{name: "larger numeric prerelease fallback", fallback: "v1.0.0-100000000000000000000", currentTag: "v1.0.0-90000000000000000000", want: true},
		{name: "smaller numeric prerelease fallback", fallback: "v1.0.0-90000000000000000000", currentTag: "v1.0.0-100000000000000000000", want: false},
		{name: "non-semver current published after fallback", fallback: "build-2026-06-20", currentTag: "build-2026-07-03", want: false},
		{name: "non-semver fallback published after current", fallback: "build-2026-07-03", currentTag: "build-2026-06-20", want: true},
		{name: "non-semver current not in recent releases", fallback: "build-2026-06-20", currentTag: "build-2025-01-01", want: true},
		{name: "mixed semver current with non-semver fallback uses publish order", fallback: "build-2026-06-20", currentTag: "v1.4.0", want: false},
	}

	for _, tt := range tests {
		got := upgradeFallbackIsNewer(releases, &github.Release{TagName: tt.fallback}, tt.currentTag)
		if got != tt.want {
			t.Fatalf("%s: upgradeFallbackIsNewer(%q over %q) = %v, want %v", tt.name, tt.fallback, tt.currentTag, got, tt.want)
		}
	}
}

// newCooldownRootFixture builds a fake client whose latest release v1.4.0 is 2
// days old and whose list contains an eligible v1.3.2 released 30 days ago.
// listCalls counts ListReleases invocations.
func newCooldownRootFixture(t *testing.T, listCalls *int) *fakeReleaseClient {
	t.Helper()

	freshRelease := github.Release{
		TagName:     "v1.4.0",
		PublishedAt: time.Now().Add(-2 * 24 * time.Hour),
		Assets: []github.Asset{{
			Name:        "tool_linux_amd64",
			DownloadURL: "https://example.invalid/v1.4.0/tool_linux_amd64",
		}},
	}
	oldRelease := github.Release{
		TagName:     "v1.3.2",
		PublishedAt: time.Now().Add(-30 * 24 * time.Hour),
		Assets: []github.Asset{{
			Name:        "tool_linux_amd64",
			DownloadURL: "https://example.invalid/v1.3.2/tool_linux_amd64",
		}},
	}

	return &fakeReleaseClient{
		getLatestRelease: func(_, _ string) (*github.Release, error) {
			rel := freshRelease
			return &rel, nil
		},
		getReleaseByTag: func(_, _, tag string) (*github.Release, error) {
			rel := freshRelease
			return &rel, nil
		},
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			*listCalls++
			return []github.Release{freshRelease, oldRelease}, nil
		},
		downloadAsset: func(_ github.Asset, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
}

func setupCooldownRootTest(t *testing.T, client releaseClient, cooldownDays int) *cobra.Command {
	t.Helper()

	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	useTestCommandDeps(t, client)

	installDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	setTestConfig(filepath.Join(t.TempDir(), "downloads"), installDir)
	cfgViper.Set("cooldown", cooldownDays)

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}
	return cmd
}

func TestRunRootCooldownFallsBackToOlderRelease(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	cmd := setupCooldownRootTest(t, client, 10)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}
	// The fallback note belongs under the heading, before the chosen release.
	wantHeading := "Fetching latest release for cli/tool...\n" +
		"  release v1.4.0 is 2 day(s) old, cooldown is 10 days — falling back to v1.3.2\n" +
		"  Release: v1.3.2 (v1.3.2)\n"
	if !strings.HasPrefix(out.String(), wantHeading) {
		t.Fatalf("runRoot() output = %q, want it to start with %q", out.String(), wantHeading)
	}
	if listCalls != 1 {
		t.Fatalf("ListReleases calls = %d, want 1", listCalls)
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].Tag != "v1.3.2" {
		t.Fatalf("history records = %+v, want single v1.3.2 install", records)
	}
}

func TestRunRootCooldownEligibleLatestSkipsListCall(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	client.getLatestRelease = func(_, _ string) (*github.Release, error) {
		return &github.Release{
			TagName:     "v1.3.2",
			PublishedAt: time.Now().Add(-30 * 24 * time.Hour),
			Assets: []github.Asset{{
				Name:        "tool_linux_amd64",
				DownloadURL: "https://example.invalid/tool_linux_amd64",
			}},
		}, nil
	}
	cmd := setupCooldownRootTest(t, client, 10)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}
	if listCalls != 0 {
		t.Fatalf("ListReleases calls = %d, want 0 when latest is eligible", listCalls)
	}
}

func TestRunRootCooldownBlocksExplicitTag(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	cmd := setupCooldownRootTest(t, client, 10)
	if err := cmd.Flags().Set("tag", "v1.4.0"); err != nil {
		t.Fatalf("set tag: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runRoot(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--cooldown 0") {
		t.Fatalf("runRoot() error = %v, want cooldown block with override hint", err)
	}
	if listCalls != 0 {
		t.Fatalf("ListReleases calls = %d, want 0 (no fallback for explicit tag)", listCalls)
	}
	if len(loadHistoryRecords(t)) != 0 {
		t.Fatal("history records exist, want none after blocked install")
	}
}

func TestRunRootCooldownFlagZeroInstallsFreshRelease(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	cmd := setupCooldownRootTest(t, client, 10)
	if err := cmd.Flags().Set("cooldown", "0"); err != nil {
		t.Fatalf("set cooldown: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}
	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].Tag != "v1.4.0" {
		t.Fatalf("history records = %+v, want fresh v1.4.0 install with cooldown disabled", records)
	}
}

func TestRunRootCooldownTrustedOwnerInstallsFreshRelease(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	cmd := setupCooldownRootTest(t, client, 10)
	cfgViper.Set("trustedOwners", []string{"CLI"})

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}
	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].Tag != "v1.4.0" {
		t.Fatalf("history records = %+v, want fresh v1.4.0 install for trusted owner", records)
	}
}

func TestRunRootCooldownTrustedOwnerScopedToHost(t *testing.T) {
	tests := []struct {
		name    string
		trusted []string
		wantErr string
	}{
		{name: "bare entry does not trust ghe owner", trusted: []string{"cli"}, wantErr: `add "acme.ghe.com/cli" to trustedOwners`},
		{name: "host/owner entry trusts ghe owner", trusted: []string{"acme.ghe.com/cli"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var listCalls int
			client := newCooldownRootFixture(t, &listCalls)
			cmd := setupCooldownRootTest(t, client, 10)
			cfgViper.Set("trustedOwners", tt.trusted)
			for flag, value := range map[string]string{"host": "acme.ghe.com", "tag": "v1.4.0"} {
				if err := cmd.Flags().Set(flag, value); err != nil {
					t.Fatalf("set %s: %v", flag, err)
				}
			}
			cmd.SetOut(&bytes.Buffer{})

			err := runRoot(cmd, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("runRoot() error = %v, want cooldown block with hint %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("runRoot() error: %v", err)
			}
			records := loadHistoryRecords(t)
			if len(records) != 1 || records[0].Tag != "v1.4.0" || records[0].Host != "acme.ghe.com" {
				t.Fatalf("history records = %+v, want fresh v1.4.0 install from acme.ghe.com", records)
			}
		})
	}
}

func TestRunRootCooldownBlocksReuploadedAsset(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	client.getLatestRelease = func(_, _ string) (*github.Release, error) {
		return &github.Release{
			TagName:     "v1.3.2",
			PublishedAt: time.Now().Add(-30 * 24 * time.Hour),
			Assets: []github.Asset{{
				Name:        "tool_linux_amd64",
				DownloadURL: "https://example.invalid/tool_linux_amd64",
				UpdatedAt:   time.Now().Add(-1 * 24 * time.Hour),
			}},
		}, nil
	}
	cmd := setupCooldownRootTest(t, client, 10)

	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runRoot(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "tampering") || !strings.Contains(err.Error(), "tool_linux_amd64") {
		t.Fatalf("runRoot() error = %v, want asset tamper refusal", err)
	}
	if len(loadHistoryRecords(t)) != 0 {
		t.Fatal("history records exist, want none after blocked install")
	}
}

func TestRunRootCooldownFallbackJSONReport(t *testing.T) {
	var listCalls int
	client := newCooldownRootFixture(t, &listCalls)
	cmd := setupCooldownRootTest(t, client, 10)
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}
	if err := cmd.Flags().Set("download-only", "true"); err != nil {
		t.Fatalf("set download-only: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}

	var got rootCommandResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if got.ReleaseTag != "v1.3.2" {
		t.Fatalf("runRoot() releaseTag = %q, want fallback v1.3.2", got.ReleaseTag)
	}
	if got.Cooldown == nil || got.Cooldown.SkippedTag != "v1.4.0" || got.Cooldown.Days != 10 || got.Cooldown.SkippedAgeDays != 2 {
		t.Fatalf("runRoot() cooldown report = %+v, want skipped v1.4.0 details", got.Cooldown)
	}
}

func cooldownUpgradeSettings(days int) cooldownSettings {
	return cooldownSettings{days: days, source: "config", now: time.Now}
}

func TestResolveUpgradeReleaseCooldownFallback(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(_, _ string) (*github.Release, error) {
			return &github.Release{TagName: "v1.4.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)}, nil
		},
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{
				{TagName: "v1.4.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)},
				{TagName: "v1.3.2", PublishedAt: time.Now().Add(-30 * 24 * time.Hour)},
			}, nil
		},
	}
	rec := &history.Record{Owner: "cli", Repo: "tool", Tag: "v1.2.0"}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	release, unchanged, err := resolveUpgradeRelease(cmd, client, rec, cooldownUpgradeSettings(10).policyFor(hostOwner{owner: "cli"}))
	if err != nil {
		t.Fatalf("resolveUpgradeRelease() error: %v", err)
	}
	if unchanged || release == nil || release.TagName != "v1.3.2" {
		t.Fatalf("resolveUpgradeRelease() = (%+v, %v), want fallback v1.3.2", release, unchanged)
	}
	if !strings.Contains(out.String(), "falling back to v1.3.2") {
		t.Fatalf("resolveUpgradeRelease() output = %q, want fallback message", out.String())
	}
}

func TestResolveUpgradeReleaseCooldownAlreadyAtNewestEligible(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(_, _ string) (*github.Release, error) {
			return &github.Release{TagName: "v1.4.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)}, nil
		},
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{
				{TagName: "v1.4.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)},
				{TagName: "v1.3.2", PublishedAt: time.Now().Add(-30 * 24 * time.Hour)},
			}, nil
		},
	}
	rec := &history.Record{Owner: "cli", Repo: "tool", Tag: "v1.3.2"}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	release, unchanged, err := resolveUpgradeRelease(cmd, client, rec, cooldownUpgradeSettings(10).policyFor(hostOwner{owner: "cli"}))
	if err != nil {
		t.Fatalf("resolveUpgradeRelease() error: %v", err)
	}
	if !unchanged || release != nil {
		t.Fatalf("resolveUpgradeRelease() = (%+v, %v), want unchanged", release, unchanged)
	}
	if !strings.Contains(out.String(), "already at newest eligible version (v1.3.2)") {
		t.Fatalf("resolveUpgradeRelease() output = %q, want newest-eligible message", out.String())
	}
}

func TestResolveUpgradeReleaseCooldownNonSemverNoDowngrade(t *testing.T) {
	// The installed date-tagged release is itself still fresh (installed via
	// an earlier override); the fallback must not downgrade past it.
	client := &fakeReleaseClient{
		getLatestRelease: func(_, _ string) (*github.Release, error) {
			return &github.Release{TagName: "build-2026-07-04", PublishedAt: time.Now().Add(-1 * 24 * time.Hour)}, nil
		},
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{
				{TagName: "build-2026-07-04", PublishedAt: time.Now().Add(-1 * 24 * time.Hour)},
				{TagName: "build-2026-07-03", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)},
				{TagName: "build-2026-06-20", PublishedAt: time.Now().Add(-15 * 24 * time.Hour)},
			}, nil
		},
	}
	rec := &history.Record{Owner: "cli", Repo: "tool", Tag: "build-2026-07-03"}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	release, unchanged, err := resolveUpgradeRelease(cmd, client, rec, cooldownUpgradeSettings(10).policyFor(hostOwner{owner: "cli"}))
	if err != nil {
		t.Fatalf("resolveUpgradeRelease() error: %v", err)
	}
	if !unchanged || release != nil {
		t.Fatalf("resolveUpgradeRelease() = (%+v, %v), want unchanged instead of downgrade to build-2026-06-20", release, unchanged)
	}
	if !strings.Contains(out.String(), "already at newest eligible version (build-2026-07-03)") {
		t.Fatalf("resolveUpgradeRelease() output = %q, want newest-eligible message", out.String())
	}
}

func TestResolveUpgradeReleaseCooldownNothingEligible(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(_, _ string) (*github.Release, error) {
			return &github.Release{TagName: "v1.4.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)}, nil
		},
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{{TagName: "v1.4.0", PublishedAt: time.Now().Add(-2 * 24 * time.Hour)}}, nil
		},
	}
	rec := &history.Record{Owner: "cli", Repo: "tool", Tag: "v1.2.0"}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	_, _, err := resolveUpgradeRelease(cmd, client, rec, cooldownUpgradeSettings(10).policyFor(hostOwner{owner: "cli"}))
	if err == nil || !strings.Contains(err.Error(), "satisfies the 10-day cooldown") {
		t.Fatalf("resolveUpgradeRelease() error = %v, want no-eligible-release error", err)
	}
}

func TestResolveUpgradeReleasePinMinorBlockedByCooldown(t *testing.T) {
	client := &fakeReleaseClient{
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{
				{TagName: "v1.2.5", PublishedAt: time.Now().Add(-1 * 24 * time.Hour)},
				{TagName: "v1.2.3", PublishedAt: time.Now().Add(-30 * 24 * time.Hour)},
			}, nil
		},
	}
	rec := &history.Record{Owner: "cli", Repo: "tool", Tag: "v1.2.3", PinLevel: history.PinMinor}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	release, unchanged, err := resolveUpgradeRelease(cmd, client, rec, cooldownUpgradeSettings(10).policyFor(hostOwner{owner: "cli"}))
	if err != nil {
		t.Fatalf("resolveUpgradeRelease() error: %v", err)
	}
	if !unchanged || release != nil {
		t.Fatalf("resolveUpgradeRelease() = (%+v, %v), want unchanged", release, unchanged)
	}
	if !strings.Contains(out.String(), "no newer eligible release found (1 blocked by 10-day cooldown)") {
		t.Fatalf("resolveUpgradeRelease() output = %q, want blocked-by-cooldown message", out.String())
	}
}

// setupCooldownUpgradeTest swaps in client and a fresh test config, applies
// configure to cfgViper, and returns the loaded config, a command capturing
// output in out, and an empty history store. It swaps package-level seams, so
// callers must run serially.
func setupCooldownUpgradeTest(t *testing.T, client releaseClient, configure func()) (*config.AppConfig, *cobra.Command, *bytes.Buffer, *history.Store) {
	t.Helper()

	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	useTestCommandDeps(t, client)
	setTestConfig(filepath.Join(t.TempDir(), "downloads"), filepath.Join(t.TempDir(), "bin"))
	configure()
	cfg, err := config.Load(cfgViper)
	if err != nil {
		t.Fatalf("config.Load() error: %v", err)
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	return cfg, cmd, &out, history.NewStore(filepath.Join(t.TempDir(), "history.json"))
}

func TestUpgradeRecordCooldownTrustedOwnerScopedToHost(t *testing.T) {
	tests := []struct {
		name         string
		host         string
		wantFallback bool
	}{
		{name: "github.com package trusted by bare entry", host: "", wantFallback: false},
		{name: "ghe package not trusted by bare entry", host: "acme.ghe.com", wantFallback: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var listCalls int
			cfg, cmd, out, store := setupCooldownUpgradeTest(t, newCooldownRootFixture(t, &listCalls), func() {
				cfgViper.Set("cooldown", 10)
				cfgViper.Set("trustedOwners", []string{"cli"})
			})
			rec := newHistoryRecord("id-1", "cli", "tool", "v1.2.0", "tool_linux_amd64", "tool", "/usr/local/bin/tool")
			rec.Host = tt.host

			if _, err := upgradeRecord(cmd, store, cfg, &rec, true, resolveCooldownSettings(cmd, cfg)); err != nil {
				t.Fatalf("upgradeRecord() error: %v", err)
			}
			if got := strings.Contains(out.String(), "falling back to v1.3.2"); got != tt.wantFallback {
				t.Fatalf("upgradeRecord() output = %q, want cooldown fallback = %v", out.String(), tt.wantFallback)
			}
		})
	}
}

func TestUpgradeRecordCooldownBlocksReuploadedAsset(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(_, _ string) (*github.Release, error) {
			return &github.Release{
				TagName:     "v1.3.2",
				PublishedAt: time.Now().Add(-30 * 24 * time.Hour),
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
					UpdatedAt:   time.Now().Add(-1 * 24 * time.Hour),
				}},
			}, nil
		},
	}
	cfg, cmd, _, store := setupCooldownUpgradeTest(t, client, func() {})
	rec := newHistoryRecord("id-1", "cli", "tool", "v1.2.0", "tool_linux_amd64", "tool", "/usr/local/bin/tool")

	// Dry-run must also refuse: the check runs before the dry-run branch.
	_, err := upgradeRecord(cmd, store, cfg, &rec, true, cooldownUpgradeSettings(10))
	if err == nil || !strings.Contains(err.Error(), "tampering") {
		t.Fatalf("upgradeRecord() error = %v, want asset tamper refusal", err)
	}
}
