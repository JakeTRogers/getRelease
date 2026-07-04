package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestInitConfigSetsLogLevel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)

	oldLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
	})

	tests := []struct {
		name        string
		verbose     string
		enableInfo  bool
		enableDebug bool
	}{
		{name: "default", verbose: "0", enableInfo: false, enableDebug: false},
		{name: "info", verbose: "2", enableInfo: true, enableDebug: false},
		{name: "debug", verbose: "3", enableInfo: true, enableDebug: true},
	}

	for _, tt := range tests {
		cmd := &cobra.Command{}
		cmd.Flags().Count("verbose", "")
		if err := cmd.Flags().Set("verbose", tt.verbose); err != nil {
			t.Fatalf("%s: set verbose: %v", tt.name, err)
		}

		if err := initConfig(cmd); err != nil {
			t.Fatalf("%s: initConfig() error: %v", tt.name, err)
		}

		logger := slog.Default()
		if got := logger.Enabled(context.Background(), slog.LevelInfo); got != tt.enableInfo {
			t.Fatalf("%s: info enabled = %v, want %v", tt.name, got, tt.enableInfo)
		}
		if got := logger.Enabled(context.Background(), slog.LevelDebug); got != tt.enableDebug {
			t.Fatalf("%s: debug enabled = %v, want %v", tt.name, got, tt.enableDebug)
		}
	}
}

func TestResolveRepo(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	tests := []struct {
		name      string
		owner     string
		repo      string
		url       string
		host      string
		wantOwner string
		wantRepo  string
		wantHost  string
		wantErr   string
	}{
		{name: "owner repo flags default to github.com", owner: "cli", repo: "tool", wantOwner: "cli", wantRepo: "tool", wantHost: "github.com"},
		{name: "url flag", url: "https://github.com/cli/tool", wantOwner: "cli", wantRepo: "tool", wantHost: "github.com"},
		{name: "enterprise url carries its host", url: "https://acme.ghe.com/cli/tool", wantOwner: "cli", wantRepo: "tool", wantHost: "acme.ghe.com"},
		{name: "host flag targets enterprise", owner: "cli", repo: "tool", host: "acme.ghe.com", wantOwner: "cli", wantRepo: "tool", wantHost: "acme.ghe.com"},
		{name: "host flag rejects unsupported host", owner: "cli", repo: "tool", host: "github.acme.internal", wantErr: "unsupported GitHub host"},
		{name: "unsupported url host", url: "https://gitlab.com/cli/tool", wantErr: "not a supported GitHub URL"},
		{name: "missing repo", owner: "cli", wantErr: "--repo is required"},
		{name: "missing owner", repo: "tool", wantErr: "--owner is required"},
		{name: "missing all", wantErr: "specify a repository"},
	}

	for _, tt := range tests {
		cmd := &cobra.Command{}
		cmd.Flags().String("owner", "", "")
		cmd.Flags().String("repo", "", "")
		cmd.Flags().String("url", "", "")
		cmd.Flags().String("host", "", "")

		if err := cmd.Flags().Set("owner", tt.owner); err != nil {
			t.Fatalf("%s: set owner: %v", tt.name, err)
		}
		if err := cmd.Flags().Set("repo", tt.repo); err != nil {
			t.Fatalf("%s: set repo: %v", tt.name, err)
		}
		if err := cmd.Flags().Set("url", tt.url); err != nil {
			t.Fatalf("%s: set url: %v", tt.name, err)
		}
		if err := cmd.Flags().Set("host", tt.host); err != nil {
			t.Fatalf("%s: set host: %v", tt.name, err)
		}

		owner, repo, host, err := resolveRepo(cmd)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("%s: resolveRepo() error = %v, want substring %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: resolveRepo() error = %v", tt.name, err)
		}
		if owner != tt.wantOwner || repo != tt.wantRepo {
			t.Fatalf("%s: resolveRepo() = %s/%s, want %s/%s", tt.name, owner, repo, tt.wantOwner, tt.wantRepo)
		}
		if host != tt.wantHost {
			t.Fatalf("%s: resolveRepo() host = %q, want %q", tt.name, host, tt.wantHost)
		}
	}
}

func TestResolveRepoUsesHistoryHost(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	ghe := newHistoryRecord("id-1", "cli", "tool", "v1.0.0", "tool", "tool", "/usr/local/bin/tool")
	ghe.Host = "acme.ghe.com"
	public := newHistoryRecord("id-2", "cli", "other", "v1.0.0", "other", "other", "/usr/local/bin/other")
	writeHistoryRecords(t, []history.Record{ghe, public})

	newCmd := func(owner, repo, host string) *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().String("owner", owner, "")
		cmd.Flags().String("repo", repo, "")
		cmd.Flags().String("url", "", "")
		cmd.Flags().String("host", host, "")
		return cmd
	}

	t.Run("recorded enterprise host used automatically", func(t *testing.T) {
		_, _, host, err := resolveRepo(newCmd("cli", "tool", ""))
		if err != nil {
			t.Fatalf("resolveRepo() error = %v", err)
		}
		if host != "acme.ghe.com" {
			t.Errorf("resolveRepo() host = %q, want %q", host, "acme.ghe.com")
		}
	})

	t.Run("record without host defaults to github.com", func(t *testing.T) {
		_, _, host, err := resolveRepo(newCmd("cli", "other", ""))
		if err != nil {
			t.Fatalf("resolveRepo() error = %v", err)
		}
		if host != "github.com" {
			t.Errorf("resolveRepo() host = %q, want %q", host, "github.com")
		}
	})

	t.Run("host flag overrides recorded host", func(t *testing.T) {
		_, _, host, err := resolveRepo(newCmd("cli", "tool", "github.com"))
		if err != nil {
			t.Fatalf("resolveRepo() error = %v", err)
		}
		if host != "github.com" {
			t.Errorf("resolveRepo() host = %q, want %q", host, "github.com")
		}
	})
}

func TestRunRootDownloadOnlyJSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v1.2.3",
				Name:    "Tool 1.2.3",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
				}},
			}, nil
		},
		downloadAsset: func(_ github.Asset, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
	useTestCommandDeps(t, client)

	workDir := filepath.Join(t.TempDir(), "downloads")
	installDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	setTestConfig(workDir, installDir)

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}
	if err := cmd.Flags().Set("download-only", "true"); err != nil {
		t.Fatalf("set download-only: %v", err)
	}
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
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
	if !got.DownloadOnly {
		t.Fatalf("runRoot() downloadOnly = false, want true")
	}
	if got.ReleaseTag != "v1.2.3" || got.Asset.Name != "tool_linux_amd64" {
		t.Fatalf("runRoot() result = %+v, want release and asset details", got)
	}
	if _, err := os.Stat(got.DownloadPath); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
}

func TestRunRootInstallsBinaryAndUpdatesHistory(t *testing.T) {
	baseDir := t.TempDir()
	xdgData := filepath.Join(baseDir, "xdg-data")
	t.Setenv("XDG_DATA_HOME", xdgData)

	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v2.0.0",
				Name:    "Tool 2.0.0",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
				}},
			}, nil
		},
		downloadAsset: func(_ github.Asset, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
	useTestCommandDeps(t, client)

	workDir := filepath.Join(baseDir, "downloads")
	installDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	setTestConfig(workDir, installDir)

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}

	installedPath := filepath.Join(installDir, "tool")
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatalf("installed file missing: %v", err)
	}
	if !strings.Contains(out.String(), "History updated: cli/tool v2.0.0") {
		t.Fatalf("runRoot() output = %q, want history update message", out.String())
	}
	if !strings.Contains(out.String(), "Review the release notes: https://github.com/cli/tool/releases/tag/v2.0.0") {
		t.Fatalf("runRoot() output = %q, want release notes link", out.String())
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 {
		t.Fatalf("history records = %d, want 1", len(records))
	}
	if records[0].Owner != "cli" || records[0].Repo != "tool" || records[0].Tag != "v2.0.0" {
		t.Fatalf("history record = %+v, want updated release metadata", records[0])
	}
	if len(records[0].Binaries) != 1 || records[0].Binaries[0].InstalledAs != "tool" {
		t.Fatalf("history binaries = %+v, want installed binary entry", records[0].Binaries)
	}
	if records[0].Host != "" {
		t.Fatalf("history record host = %q, want empty for github.com", records[0].Host)
	}
}

func TestRunRootRecordsEnterpriseHostInHistory(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(baseDir, "xdg-data"))

	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v2.0.0",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://acme.ghe.com/cli/tool/releases/download/v2.0.0/tool_linux_amd64",
				}},
			}, nil
		},
		downloadAsset: func(_ github.Asset, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
	useTestCommandDeps(t, client)

	var gotHost string
	newGitHubClient = func(host string) (releaseClient, error) {
		gotHost = host
		return client, nil
	}

	installDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	setTestConfig(filepath.Join(baseDir, "downloads"), installDir)

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}
	if err := cmd.Flags().Set("host", "acme.ghe.com"); err != nil {
		t.Fatalf("set host: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}

	if gotHost != "acme.ghe.com" {
		t.Fatalf("newGitHubClient host = %q, want %q", gotHost, "acme.ghe.com")
	}
	if !strings.Contains(out.String(), "Review the release notes: https://acme.ghe.com/cli/tool/releases/tag/v2.0.0") {
		t.Fatalf("runRoot() output = %q, want enterprise release notes link", out.String())
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 {
		t.Fatalf("history records = %d, want 1", len(records))
	}
	if records[0].Host != "acme.ghe.com" {
		t.Fatalf("history record host = %q, want %q", records[0].Host, "acme.ghe.com")
	}
}

func TestRunRootPreservesExistingPinLevel(t *testing.T) {
	baseDir := t.TempDir()
	xdgData := filepath.Join(baseDir, "xdg-data")
	t.Setenv("XDG_DATA_HOME", xdgData)

	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v1.2.4",
				Name:    "Tool 1.2.4",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
				}},
			}, nil
		},
		downloadAsset: func(_ github.Asset, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
	useTestCommandDeps(t, client)

	workDir := filepath.Join(baseDir, "downloads")
	installDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	setTestConfig(workDir, installDir)

	existingPath := filepath.Join(installDir, "tool")
	writeExecutableFile(t, existingPath)
	existing := newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool_linux_amd64", "tool", existingPath)
	existing.PinLevel = history.PinMinor
	writeHistoryRecords(t, []history.Record{existing})

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 {
		t.Fatalf("history records = %d, want 1", len(records))
	}
	if records[0].PinLevel != history.PinMinor {
		t.Fatalf("history pin level = %q, want %q", records[0].PinLevel, history.PinMinor)
	}
}

func TestRunRootPrefersBestAssetAmongMultipleMatches(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v1.2.3",
				Assets: []github.Asset{
					{Name: "tool_linux_amd64.zip", DownloadURL: "https://example.invalid/tool.zip"},
					{Name: "tool_linux_amd64.tar.gz", DownloadURL: "https://example.invalid/tool.tar.gz"},
				},
			}, nil
		},
		downloadAsset: func(_ github.Asset, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
	useTestCommandDeps(t, client)

	baseDir := t.TempDir()
	setTestConfig(filepath.Join(baseDir, "downloads"), filepath.Join(baseDir, "bin"))

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}
	if err := cmd.Flags().Set("download-only", "true"); err != nil {
		t.Fatalf("set download-only: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runRoot(cmd, nil); err != nil {
		t.Fatalf("runRoot() error: %v", err)
	}
	if !strings.Contains(out.String(), "tool_linux_amd64.tar.gz (auto-selected, preferred match)") {
		t.Fatalf("runRoot() output = %q, want preferred asset selection", out.String())
	}
}

func TestRunRootReturnsErrorWhenNoAssetsMatch(t *testing.T) {
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v1.2.3",
				Assets:  []github.Asset{{Name: "tool_darwin_arm64.zip"}},
			}, nil
		},
	}
	useTestCommandDeps(t, client)

	baseDir := t.TempDir()
	setTestConfig(filepath.Join(baseDir, "downloads"), filepath.Join(baseDir, "bin"))

	cmd := &cobra.Command{}
	addRootTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}

	err := runRoot(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching assets for linux/amd64") {
		t.Fatalf("runRoot() error = %v, want no matching assets error", err)
	}
}

func TestValidateInstallName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "trimmed basename", input: " tool ", want: "tool"},
		{name: "empty", input: "   ", wantErr: "name is empty"},
		{name: "dot", input: ".", wantErr: "must not be '.' or '..'"},
		{name: "control", input: "tool\x01", wantErr: "contains control characters"},
	}

	for _, tt := range tests {
		got, err := validateInstallName(tt.input)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("%s: validateInstallName() error = %v, want %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: validateInstallName() error = %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: validateInstallName() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
