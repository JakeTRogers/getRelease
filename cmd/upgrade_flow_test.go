package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestResolveUpgradeRecordMultipleMatchesUsesSelection(t *testing.T) {
	useTestCommandDeps(t, nil)
	selectItems = func(_ []string, _ string) (int, error) {
		return 1, nil
	}

	store := history.NewStore(filepath.Join(t.TempDir(), "history.json"))
	firstPath := filepath.Join(t.TempDir(), "bin", "tool-a")
	secondPath := filepath.Join(t.TempDir(), "bin", "tool-b")
	writeExecutableFile(t, firstPath)
	writeExecutableFile(t, secondPath)
	if err := store.Add(newHistoryRecord("rec1", "cli", "tool-a", "v1.0.0", "tool", "tool", firstPath)); err != nil {
		t.Fatalf("add first record: %v", err)
	}
	if err := store.Add(newHistoryRecord("rec2", "ops", "tool-b", "v1.0.0", "tool", "tool", secondPath)); err != nil {
		t.Fatalf("add second record: %v", err)
	}

	rec, err := resolveUpgradeRecord(store, "tool", "", "")
	if err != nil {
		t.Fatalf("resolveUpgradeRecord() error: %v", err)
	}
	if rec.Owner != "ops" || rec.Repo != "tool-b" {
		t.Fatalf("resolveUpgradeRecord() = %+v, want selected record", rec)
	}
}

func TestRunUpgradeDryRun(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			if owner != "cli" || repo != "tool" {
				t.Fatalf("GetLatestRelease() called with %s/%s", owner, repo)
			}
			return &github.Release{
				TagName: "v2.0.0",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
					Size:        2048,
				}},
			}, nil
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
	if !strings.Contains(out.String(), "Would upgrade https://github.com/cli/tool/releases from v1.0.0 to v2.0.0") {
		t.Fatalf("runUpgrade() output = %q, want dry-run summary", out.String())
	}
}

func TestResolveUpgradeRecordPaths(t *testing.T) {
	useTestCommandDeps(t, nil)

	store := history.NewStore(filepath.Join(t.TempDir(), "history.json"))
	targetPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, targetPath)
	if err := store.Add(newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool", "tool", targetPath)); err != nil {
		t.Fatalf("add record: %v", err)
	}

	rec, err := resolveUpgradeRecord(store, "ignored", "cli", "tool")
	if err != nil {
		t.Fatalf("resolveUpgradeRecord() owner/repo error: %v", err)
	}
	if rec.Owner != "cli" || rec.Repo != "tool" {
		t.Fatalf("resolveUpgradeRecord() owner/repo = %+v, want cli/tool", rec)
	}

	rec, err = resolveUpgradeRecord(store, "cli/tool", "", "")
	if err != nil {
		t.Fatalf("resolveUpgradeRecord() target path error: %v", err)
	}
	if rec.Owner != "cli" || rec.Repo != "tool" {
		t.Fatalf("resolveUpgradeRecord() target path = %+v, want cli/tool", rec)
	}

	if _, err := resolveUpgradeRecord(store, "cli/", "", ""); err == nil || !strings.Contains(err.Error(), "invalid target") {
		t.Fatalf("resolveUpgradeRecord() invalid target error = %v, want invalid target", err)
	}
	if _, err := resolveUpgradeRecord(store, "missing", "", ""); err == nil || !strings.Contains(err.Error(), "no history found") {
		t.Fatalf("resolveUpgradeRecord() missing error = %v, want no history found", err)
	}
}

func TestRunUpgradeAllDryRunSummary(t *testing.T) {
	useTestCommandDeps(t, &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			switch owner + "/" + repo {
			case "cli/current":
				return &github.Release{TagName: "v1.0.0", Assets: []github.Asset{{Name: "current_linux_amd64", DownloadURL: "https://example.invalid/current", Size: 1024}}}, nil
			case "cli/updated":
				return &github.Release{TagName: "v2.0.0", Assets: []github.Asset{{Name: "updated_linux_amd64", DownloadURL: "https://example.invalid/updated", Size: 2048}}}, nil
			case "cli/failing":
				return nil, errors.New("boom")
			default:
				return nil, errors.New("unexpected repo")
			}
		},
	})

	cfg := &config.AppConfig{AssetPreferences: config.AssetPreferences{Formats: []string{"tar.gz", "zip"}}}
	store := history.NewStore(filepath.Join(t.TempDir(), "history.json"))

	currentPath := filepath.Join(t.TempDir(), "bin", "current")
	updatedPath := filepath.Join(t.TempDir(), "bin", "updated")
	failingPath := filepath.Join(t.TempDir(), "bin", "failing")
	writeExecutableFile(t, currentPath)
	writeExecutableFile(t, updatedPath)
	writeExecutableFile(t, failingPath)

	for _, rec := range []history.Record{
		newHistoryRecord("rec1", "cli", "current", "v1.0.0", "current_linux_amd64", "current", currentPath),
		newHistoryRecord("rec2", "cli", "updated", "v1.0.0", "updated_linux_amd64", "updated", updatedPath),
		newHistoryRecord("rec3", "cli", "failing", "v1.0.0", "failing_linux_amd64", "failing", failingPath),
		newHistoryRecord("rec4", "cli", "missing", "v1.0.0", "missing_linux_amd64", "missing", filepath.Join(t.TempDir(), "bin", "missing")),
	} {
		if err := store.Add(rec); err != nil {
			t.Fatalf("add record: %v", err)
		}
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := runUpgradeAll(cmd, store, cfg, true)
	if err == nil || !strings.Contains(err.Error(), "completed with failures") {
		t.Fatalf("runUpgradeAll() error = %v, want failure summary", err)
	}
	if !strings.Contains(out.String(), "Summary: 3 checked, 1 would upgrade, 1 already at latest, 1 skipped (missing), 1 failed") {
		t.Fatalf("runUpgradeAll() output = %q, want summary", out.String())
	}
	if !strings.Contains(out.String(), "==> cli/current : https://github.com/cli/current/releases") {
		t.Fatalf("runUpgradeAll() output = %q, want clickable releases header", out.String())
	}
	if !strings.Contains(errOut.String(), "Failed upgrading cli/failing: fetching latest release for cli/failing: boom") {
		t.Fatalf("runUpgradeAll() stderr = %q, want failure line", errOut.String())
	}
}

func TestUpgradeRecordInstallsBinaryAndUpdatesHistory(t *testing.T) {
	baseDir := t.TempDir()
	client := &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			return &github.Release{
				TagName: "v2.0.0",
				Assets: []github.Asset{{
					Name:        "tool_linux_amd64",
					DownloadURL: "https://example.invalid/tool_linux_amd64",
					Size:        2048,
				}},
			}, nil
		},
		downloadAsset: func(_ string, destPath string) (int64, error) {
			return writeDownloadedBinary(t, destPath), nil
		},
	}
	useTestCommandDeps(t, client)

	installDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}
	targetPath := filepath.Join(installDir, "tool")
	writeExecutableFile(t, targetPath)

	rec := newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool_linux_amd64", "tool", targetPath)
	storePath := filepath.Join(baseDir, "history.json")
	store := history.NewStore(storePath)
	if err := store.Add(rec); err != nil {
		t.Fatalf("add history record: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save history store: %v", err)
	}

	cfg := &config.AppConfig{
		DownloadDir: baseDir,
		InstallDir:  installDir,
		AssetPreferences: config.AssetPreferences{
			Formats: []string{"tar.gz", "zip"},
		},
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	upgraded, err := upgradeRecord(cmd, store, cfg, &rec, false)
	if err != nil {
		t.Fatalf("upgradeRecord() error: %v", err)
	}
	if !upgraded {
		t.Fatal("upgradeRecord() upgraded = false, want true")
	}
	if !strings.Contains(out.String(), "Upgraded https://github.com/cli/tool/releases to v2.0.0") {
		t.Fatalf("upgradeRecord() output = %q, want upgrade completion", out.String())
	}

	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("upgraded binary missing: %v", err)
	}
	if err := store.Load(); err != nil {
		t.Fatalf("reload history store: %v", err)
	}
	records := store.Records()
	if len(records) != 1 || records[0].Tag != "v2.0.0" {
		t.Fatalf("history records after upgrade = %+v, want updated tag", records)
	}
}

func TestUpgradeRecordAlreadyLatestAndFallbackAssetSelection(t *testing.T) {
	useTestCommandDeps(t, &fakeReleaseClient{
		getLatestRelease: func(owner, repo string) (*github.Release, error) {
			switch repo {
			case "current":
				return &github.Release{TagName: "v1.0.0", Assets: []github.Asset{{Name: "current_linux_amd64"}}}, nil
			case "fallback":
				return &github.Release{TagName: "v2.0.0", Assets: []github.Asset{
					{Name: "fallback_linux_amd64.zip", DownloadURL: "https://example.invalid/fallback.zip", Size: 1024},
					{Name: "fallback_linux_amd64.tar.gz", DownloadURL: "https://example.invalid/fallback.tar.gz", Size: 2048},
				}}, nil
			default:
				return nil, errors.New("unexpected repo")
			}
		},
	})

	cfg := &config.AppConfig{AssetPreferences: config.AssetPreferences{Formats: []string{"tar.gz", "zip"}}}
	cmd := &cobra.Command{}

	var out bytes.Buffer
	cmd.SetOut(&out)
	current := newHistoryRecord("rec1", "cli", "current", "v1.0.0", "current_linux_amd64", "current", filepath.Join(t.TempDir(), "bin", "current"))
	upgraded, err := upgradeRecord(cmd, history.NewStore(filepath.Join(t.TempDir(), "history.json")), cfg, &current, true)
	if err != nil {
		t.Fatalf("upgradeRecord() current error: %v", err)
	}
	if upgraded {
		t.Fatal("upgradeRecord() current upgraded = true, want false")
	}
	if !strings.Contains(out.String(), "Already at latest version (v1.0.0)") {
		t.Fatalf("upgradeRecord() current output = %q, want already latest message", out.String())
	}

	out.Reset()
	fallback := newHistoryRecord("rec2", "cli", "fallback", "v1.0.0", "legacy-name", "fallback", filepath.Join(t.TempDir(), "bin", "fallback"))
	upgraded, err = upgradeRecord(cmd, history.NewStore(filepath.Join(t.TempDir(), "history-2.json")), cfg, &fallback, true)
	if err != nil {
		t.Fatalf("upgradeRecord() fallback error: %v", err)
	}
	if !upgraded {
		t.Fatal("upgradeRecord() fallback upgraded = false, want true")
	}
	if !strings.Contains(out.String(), "Asset: fallback_linux_amd64.tar.gz") {
		t.Fatalf("upgradeRecord() fallback output = %q, want preferred fallback asset", out.String())
	}
}

func TestPreferredUpgradePath(t *testing.T) {
	shallow := preferredUpgradePath("/tmp/extracted/tool", "/tmp/extracted/nested/tool")
	if shallow != "/tmp/extracted/tool" {
		t.Fatalf("preferredUpgradePath() = %q, want shallower path", shallow)
	}

	if got := preferredUpgradePath("/tmp/extracted/nested/tool", "/tmp/extracted/tool"); got != "" {
		t.Fatalf("preferredUpgradePath() = %q, want empty string for deeper path", got)
	}
}
