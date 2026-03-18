package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
)

type fakeReleaseClient struct {
	getLatestRelease func(owner, repo string) (*github.Release, error)
	getReleaseByTag  func(owner, repo, tag string) (*github.Release, error)
	listReleases     func(owner, repo string, limit int) ([]github.Release, error)
	downloadAsset    func(downloadURL, destPath string) (int64, error)
}

func (f *fakeReleaseClient) GetLatestRelease(owner, repo string) (*github.Release, error) {
	if f.getLatestRelease == nil {
		return nil, nil
	}
	return f.getLatestRelease(owner, repo)
}

func (f *fakeReleaseClient) GetReleaseByTag(owner, repo, tag string) (*github.Release, error) {
	if f.getReleaseByTag == nil {
		return nil, nil
	}
	return f.getReleaseByTag(owner, repo, tag)
}

func (f *fakeReleaseClient) ListReleases(owner, repo string, limit int) ([]github.Release, error) {
	if f.listReleases == nil {
		return nil, nil
	}
	return f.listReleases(owner, repo, limit)
}

func (f *fakeReleaseClient) DownloadAsset(downloadURL, destPath string) (int64, error) {
	if f.downloadAsset == nil {
		return 0, nil
	}
	return f.downloadAsset(downloadURL, destPath)
}

func useTestCommandDeps(t *testing.T, client releaseClient) {
	t.Helper()

	oldClient := newGitHubClient
	oldSelect := selectItems
	oldConfirm := confirmAction
	oldInstaller := newBinaryInstaller
	oldViper := cfgViper

	if client != nil {
		newGitHubClient = func() releaseClient {
			return client
		}
	}
	selectItems = func(_ []string, _ string) (int, error) {
		return 0, nil
	}
	confirmAction = func(_ string, defaultYes bool) (bool, error) {
		return defaultYes, nil
	}
	cfgViper = viper.New()
	config.SetDefaults(cfgViper)

	t.Cleanup(func() {
		newGitHubClient = oldClient
		selectItems = oldSelect
		confirmAction = oldConfirm
		newBinaryInstaller = oldInstaller
		cfgViper = oldViper
	})
}

func setTestConfig(downloadDir, installDir string) {
	cfgViper.Set("downloadDir", downloadDir)
	cfgViper.Set("installDir", installDir)
	cfgViper.Set("installCommand", "")
	cfgViper.Set("autoExtract", false)
	cfgViper.Set("assetPreferences.os", "linux")
	cfgViper.Set("assetPreferences.arch", "amd64")
	cfgViper.Set("assetPreferences.formats", []string{"tar.gz", "zip"})
	cfgViper.Set("assetPreferences.excludePatterns", []string{})
}

func addRootTestFlags(cmd *cobra.Command) {
	cmd.Flags().String("owner", "", "")
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("tag", "", "")
	cmd.Flags().Bool("download-only", false, "")
	cmd.Flags().String("install-as", "", "")
	cmd.Flags().String("format", "text", "")
}

func addListTestFlags(cmd *cobra.Command) {
	cmd.Flags().String("owner", "", "")
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("tag", "", "")
	cmd.Flags().Int("limit", 30, "")
	cmd.Flags().String("format", "text", "")
}

func addUpgradeTestFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().String("owner", "", "")
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Bool("dry-run", false, "")
}

func writeDownloadedBinary(t *testing.T, destPath string) int64 {
	t.Helper()

	data := []byte("#!/bin/sh\necho test\n")
	if err := os.WriteFile(destPath, data, 0o755); err != nil {
		t.Fatalf("write downloaded binary: %v", err)
	}
	return int64(len(data))
}

func historyPathForTest(t *testing.T) string {
	t.Helper()

	path, err := config.HistoryFilePath()
	if err != nil {
		t.Fatalf("history file path: %v", err)
	}
	return path
}

func writeHistoryRecords(t *testing.T, records []history.Record) string {
	t.Helper()

	path := historyPathForTest(t)
	store := history.NewStore(path)
	for _, rec := range records {
		if err := store.Add(rec); err != nil {
			t.Fatalf("add history record: %v", err)
		}
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save history: %v", err)
	}
	return path
}

func loadHistoryRecords(t *testing.T) []history.Record {
	t.Helper()

	store := history.NewStore(historyPathForTest(t))
	if err := store.Load(); err != nil {
		t.Fatalf("load history: %v", err)
	}
	return store.Records()
}

func newHistoryRecord(id, owner, repo, tag, binaryName, installedAs, installPath string) history.Record {
	return history.Record{
		ID:          id,
		Owner:       owner,
		Repo:        repo,
		Tag:         tag,
		Asset:       history.AssetInfo{Name: binaryName, URL: "https://example.invalid/download"},
		Binaries:    []history.Binary{{Name: binaryName, InstalledAs: installedAs, InstallPath: installPath}},
		OS:          "linux",
		Arch:        "amd64",
		InstalledAt: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
	}
}

func writeExecutableFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create executable dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable file: %v", err)
	}
}
