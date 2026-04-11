package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestValidateUpgradeArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		all       bool
		owner     string
		repo      string
		wantError bool
	}{
		{name: "single target", args: []string{"k9s"}},
		{name: "all without target", all: true},
		{name: "missing target", wantError: true},
		{name: "all with target", args: []string{"k9s"}, all: true, wantError: true},
		{name: "all with owner", all: true, owner: "derailed", wantError: true},
		{name: "all with repo", all: true, repo: "k9s", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{Use: "upgrade"}
			cmd.Flags().Bool("all", false, "")
			cmd.Flags().String("owner", "", "")
			cmd.Flags().String("repo", "", "")

			if err := cmd.Flags().Set("all", strconv.FormatBool(tt.all)); err != nil {
				t.Fatalf("set all flag: %v", err)
			}
			if tt.owner != "" {
				if err := cmd.Flags().Set("owner", tt.owner); err != nil {
					t.Fatalf("set owner flag: %v", err)
				}
			}
			if tt.repo != "" {
				if err := cmd.Flags().Set("repo", tt.repo); err != nil {
					t.Fatalf("set repo flag: %v", err)
				}
			}

			err := validateUpgradeArgs(cmd, tt.args)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateUpgradeArgs() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestRunUpgradeUnpinnedUsesLatestRelease(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			if owner != "cli" || repo != "tool" {
				t.Fatalf("GetLatestRelease() called with %s/%s", owner, repo)
			}
			return &github.Release{
				TagName: "v1.0.1",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
					Size:        2048,
				}},
			}, nil
		},
		listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
			t.Fatalf("ListReleases() should not be called for unpinned upgrades")
			return nil, nil
		},
	}
	useTestCommandDeps(t, client)

	setTestConfig(filepath.Join(t.TempDir(), "downloads"), filepath.Join(t.TempDir(), "bin"))
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool_linux_amd64", "tool", filepath.Join(t.TempDir(), "bin", "tool")),
	})

	cmd := &cobra.Command{}
	addUpgradeTestFlags(cmd)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runUpgrade(cmd, []string{"tool"}); err != nil {
		t.Fatalf("runUpgrade() error: %v", err)
	}
	if !strings.Contains(out.String(), "Would upgrade https://github.com/cli/tool/releases from v1.0.0 to v1.0.1") {
		t.Fatalf("runUpgrade() output = %q, want dry-run summary", out.String())
	}
}

func TestPresentHistoryRecords(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	presentPath := filepath.Join(tmp, "installed-bin")
	if err := os.WriteFile(presentPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write present binary: %v", err)
	}

	records := []history.Record{
		{
			Owner: "one",
			Repo:  "present",
			Binaries: []history.Binary{
				{InstallPath: presentPath},
			},
			InstalledAt: time.Now(),
		},
		{
			Owner: "two",
			Repo:  "missing",
			Binaries: []history.Binary{
				{InstallPath: filepath.Join(tmp, "missing-bin")},
			},
			InstalledAt: time.Now(),
		},
		{
			Owner:       "three",
			Repo:        "empty",
			Binaries:    nil,
			InstalledAt: time.Now(),
		},
	}

	present := presentHistoryRecords(records)
	if len(present) != 1 {
		t.Fatalf("presentHistoryRecords() returned %d records, want 1", len(present))
	}
	if present[0].Owner != "one" || present[0].Repo != "present" {
		t.Fatalf("presentHistoryRecords() returned unexpected record: %+v", present[0])
	}
}

func TestUpgradeRecordPinnedWithoutEligibleReleaseIsUnchanged(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			t.Fatalf("GetLatestRelease() should not be called for pinned upgrades")
			return nil, nil
		},
		listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
			if owner != "cli" || repo != "tool" {
				t.Fatalf("ListReleases() called with %s/%s", owner, repo)
			}
			if limit != 100 {
				t.Fatalf("ListReleases() limit = %d, want 100", limit)
			}
			return []github.Release{
				{TagName: "v1.3.0"},
				{TagName: "v2.0.0"},
				{TagName: "v1.2.4", Draft: true},
				{TagName: "v1.2.5", Prerelease: true},
				{TagName: "nightly"},
			}, nil
		},
	}
	useTestCommandDeps(t, client)

	cfg := &config.AppConfig{}
	rec := newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool_linux_amd64", "tool", filepath.Join(t.TempDir(), "bin", "tool"))
	rec.PinLevel = history.PinMinor

	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)

	upgraded, err := upgradeRecord(cmd, history.NewStore(filepath.Join(t.TempDir(), "history.json")), cfg, &rec, false)
	if err != nil {
		t.Fatalf("upgradeRecord() error: %v", err)
	}
	if upgraded {
		t.Fatal("upgradeRecord() upgraded = true, want false")
	}
	if !strings.Contains(out.String(), "Pin policy: minor (allows v1.2.x)") {
		t.Fatalf("upgradeRecord() output = %q, want policy line", out.String())
	}
	if !strings.Contains(out.String(), "no newer eligible release found") {
		t.Fatalf("upgradeRecord() output = %q, want unchanged message", out.String())
	}
}
func TestRecordInstalled(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "installed-bin")
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatalf("write present binary: %v", err)
	}

	tests := []struct {
		name string
		rec  history.Record
		want bool
	}{
		{
			name: "installed binary exists",
			rec:  history.Record{Binaries: []history.Binary{{InstallPath: path}}},
			want: true,
		},
		{
			name: "binary missing",
			rec:  history.Record{Binaries: []history.Binary{{InstallPath: filepath.Join(tmp, "missing")}}},
			want: false,
		},
		{
			name: "empty install path",
			rec:  history.Record{Binaries: []history.Binary{{InstallPath: ""}}},
			want: false,
		},
		{
			name: "no binaries",
			rec:  history.Record{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := recordInstalled(tt.rec); got != tt.want {
				t.Fatalf("recordInstalled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildArchiveUpgradeMappings(t *testing.T) {
	t.Parallel()

	rec := history.Record{
		Binaries: []history.Binary{
			{Name: "tool", InstalledAs: "tool", InstallPath: "/usr/local/bin/tool"},
			{Name: "helper", InstalledAs: "helper", InstallPath: "/usr/local/bin/helper"},
		},
	}

	maps, missing := buildArchiveUpgradeMappings(rec, "/tmp/extracted", []string{"tool", "nested/helper"})
	if len(missing) != 0 {
		t.Fatalf("buildArchiveUpgradeMappings() missing = %v, want none", missing)
	}
	if len(maps) != 2 {
		t.Fatalf("buildArchiveUpgradeMappings() returned %d mappings, want 2", len(maps))
	}
	if maps[0].src != "/tmp/extracted/tool" {
		t.Fatalf("first mapping src = %q, want %q", maps[0].src, "/tmp/extracted/tool")
	}
	if maps[1].src != "/tmp/extracted/nested/helper" {
		t.Fatalf("second mapping src = %q, want %q", maps[1].src, "/tmp/extracted/nested/helper")
	}
}

func TestBuildArchiveUpgradeMappings_UsesInstalledAsFallback(t *testing.T) {
	t.Parallel()

	rec := history.Record{
		Binaries: []history.Binary{
			{Name: "argocd-linux-amd64", InstalledAs: "argocd", InstallPath: "/usr/local/bin/argocd"},
		},
	}

	maps, missing := buildArchiveUpgradeMappings(rec, "/tmp/extracted", []string{"argocd"})
	if len(missing) != 0 {
		t.Fatalf("buildArchiveUpgradeMappings() missing = %v, want none", missing)
	}
	if len(maps) != 1 {
		t.Fatalf("buildArchiveUpgradeMappings() returned %d mappings, want 1", len(maps))
	}
	if maps[0].src != "/tmp/extracted/argocd" {
		t.Fatalf("mapping src = %q, want %q", maps[0].src, "/tmp/extracted/argocd")
	}
	if maps[0].dst != "/usr/local/bin/argocd" {
		t.Fatalf("mapping dst = %q, want %q", maps[0].dst, "/usr/local/bin/argocd")
	}
}

func TestBuildSingleAssetUpgradeMappings_MissingBinary(t *testing.T) {
	t.Parallel()

	rec := history.Record{
		Binaries: []history.Binary{
			{Name: "tool", InstalledAs: "tool", InstallPath: "/usr/local/bin/tool"},
			{Name: "helper", InstalledAs: "helper", InstallPath: "/usr/local/bin/helper"},
		},
	}

	maps, missing := buildSingleAssetUpgradeMappings(rec, github.Asset{Name: "tool"}, "/tmp/tool")
	if len(maps) != 1 {
		t.Fatalf("buildSingleAssetUpgradeMappings() returned %d mappings, want 1", len(maps))
	}
	wantMissing := []string{"helper"}
	if !reflect.DeepEqual(missing, wantMissing) {
		t.Fatalf("buildSingleAssetUpgradeMappings() missing = %v, want %v", missing, wantMissing)
	}
}

func TestUpgradeRecordPinnedReleaseSelection(t *testing.T) {
	tests := []struct {
		name           string
		level          history.PinLevel
		releases       []github.Release
		wantTag        string
		wantPolicyLine string
	}{
		{
			name:  "minor selects highest eligible patch",
			level: history.PinMinor,
			releases: []github.Release{
				{TagName: "v2.0.0"},
				{TagName: "v1.3.0"},
				{TagName: "v1.2.4", Assets: []github.Asset{{Name: "tool_linux_amd64", DownloadURL: "https://example.invalid/tool-124", Size: 124}}},
				{TagName: "v1.2.9", Assets: []github.Asset{{Name: "tool_linux_amd64", DownloadURL: "https://example.invalid/tool-129", Size: 129}}},
				{TagName: "latest"},
				{TagName: "v1.2.10-rc1"},
			},
			wantTag:        "v1.2.9",
			wantPolicyLine: "Pin policy: minor (allows v1.2.x)",
		},
		{
			name:  "major selects highest eligible minor",
			level: history.PinMajor,
			releases: []github.Release{
				{TagName: "v2.0.0"},
				{TagName: "v1.9.1", Assets: []github.Asset{{Name: "tool_linux_amd64", DownloadURL: "https://example.invalid/tool-191", Size: 191}}},
				{TagName: "v1.4.0", Assets: []github.Asset{{Name: "tool_linux_amd64", DownloadURL: "https://example.invalid/tool-140", Size: 140}}},
				{TagName: "v1.9.2", Draft: true},
				{TagName: "v1.8.9", Prerelease: true},
			},
			wantTag:        "v1.9.1",
			wantPolicyLine: "Pin policy: major (allows v1.x)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeReleaseClient{
				getLatestRelease: func(owner, repo string) (*github.Release, error) {
					t.Fatalf("GetLatestRelease() should not be called for pinned upgrades")
					return nil, nil
				},
				listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
					if owner != "cli" || repo != "tool" {
						t.Fatalf("ListReleases() called with %s/%s", owner, repo)
					}
					if limit != 100 {
						t.Fatalf("ListReleases() limit = %d, want 100", limit)
					}
					return tt.releases, nil
				},
			}
			useTestCommandDeps(t, client)

			cfg := &config.AppConfig{AssetPreferences: config.AssetPreferences{Formats: []string{"tar.gz", "zip"}}}
			rec := newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool_linux_amd64", "tool", filepath.Join(t.TempDir(), "bin", "tool"))
			rec.PinLevel = tt.level

			cmd := &cobra.Command{}
			addUpgradeTestFlags(cmd)
			if err := cmd.Flags().Set("dry-run", "true"); err != nil {
				t.Fatalf("set dry-run: %v", err)
			}

			var out strings.Builder
			cmd.SetOut(&out)

			upgraded, err := upgradeRecord(cmd, history.NewStore(filepath.Join(t.TempDir(), "history.json")), cfg, &rec, true)
			if err != nil {
				t.Fatalf("upgradeRecord() error: %v", err)
			}
			if !upgraded {
				t.Fatal("upgradeRecord() upgraded = false, want true")
			}
			if !strings.Contains(out.String(), tt.wantPolicyLine) {
				t.Fatalf("upgradeRecord() output = %q, want policy line %q", out.String(), tt.wantPolicyLine)
			}
			if !strings.Contains(out.String(), "to "+tt.wantTag) {
				t.Fatalf("upgradeRecord() output = %q, want upgrade target %q", out.String(), tt.wantTag)
			}
		})
	}
}

func TestUpgradeRecordPatchPinIsUnchanged(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			t.Fatalf("GetLatestRelease() should not be called for patch pin")
			return nil, nil
		},
		listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
			t.Fatalf("ListReleases() should not be called for patch pin")
			return nil, nil
		},
	}
	useTestCommandDeps(t, client)

	cfg := &config.AppConfig{}
	rec := newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool_linux_amd64", "tool", filepath.Join(t.TempDir(), "bin", "tool"))
	rec.PinLevel = history.PinPatch

	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)

	upgraded, err := upgradeRecord(cmd, history.NewStore(filepath.Join(t.TempDir(), "history.json")), cfg, &rec, false)
	if err != nil {
		t.Fatalf("upgradeRecord() error: %v", err)
	}
	if upgraded {
		t.Fatal("upgradeRecord() upgraded = true, want false")
	}
	if !strings.Contains(out.String(), "Pin policy: patch (locked to exact release v1.2.3)") {
		t.Fatalf("upgradeRecord() output = %q, want patch policy", out.String())
	}
}

func TestResolveUpgradeReleasePatchPinWithNonSemverTagIsUnchanged(t *testing.T) {
	t.Parallel()

	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			t.Fatalf("GetLatestRelease() should not be called for patch pin")
			return nil, nil
		},
		listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
			t.Fatalf("ListReleases() should not be called for patch pin")
			return nil, nil
		},
	}

	rec := &history.Record{
		Owner:    "cli",
		Repo:     "tool",
		Tag:      "latest",
		PinLevel: history.PinPatch,
	}

	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)

	release, unchanged, err := resolveUpgradeRelease(cmd, client, rec)
	if err != nil {
		t.Fatalf("resolveUpgradeRelease() error: %v", err)
	}
	if release != nil {
		t.Fatalf("resolveUpgradeRelease() release = %+v, want nil", release)
	}
	if !unchanged {
		t.Fatal("resolveUpgradeRelease() unchanged = false, want true")
	}
	if !strings.Contains(out.String(), "Pin policy: patch (locked to exact release latest)") {
		t.Fatalf("resolveUpgradeRelease() output = %q, want patch policy", out.String())
	}
}

func TestResolveUpgradeReleaseNonSemverPinnedVersionFailsForMinorAndMajorPins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level history.PinLevel
	}{
		{name: "minor pin", level: history.PinMinor},
		{name: "major pin", level: history.PinMajor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &fakeReleaseClient{
				getLatestRelease: func(owner, repo string) (*github.Release, error) {
					t.Fatalf("GetLatestRelease() should not be called for %s", tt.level)
					return nil, nil
				},
				listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
					t.Fatalf("ListReleases() should not be called when parsing the current pinned tag fails for %s", tt.level)
					return nil, nil
				},
			}

			rec := &history.Record{
				Owner:    "cli",
				Repo:     "tool",
				Tag:      "latest",
				PinLevel: tt.level,
			}

			cmd := &cobra.Command{}
			var out strings.Builder
			cmd.SetOut(&out)

			release, unchanged, err := resolveUpgradeRelease(cmd, client, rec)
			if err == nil {
				t.Fatal("resolveUpgradeRelease() error = nil, want parse error")
			}
			wantErr := "parse pinned version \"latest\" for cli/tool: invalid version format: latest"
			if err.Error() != wantErr {
				t.Fatalf("resolveUpgradeRelease() error = %q, want %q", err.Error(), wantErr)
			}
			if release != nil {
				t.Fatalf("resolveUpgradeRelease() release = %+v, want nil", release)
			}
			if unchanged {
				t.Fatal("resolveUpgradeRelease() unchanged = true, want false")
			}
			if out.Len() != 0 {
				t.Fatalf("resolveUpgradeRelease() output = %q, want empty output", out.String())
			}
		})
	}
}
