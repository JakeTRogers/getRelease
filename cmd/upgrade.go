package cmd

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/archive"
	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/platform"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade <target> | --all",
	Short: "Upgrade a previously installed binary",
	Long:  `Upgrade a previously installed binary using install history, or use --all to upgrade every installed package still present on disk.`,
	Args:  validateUpgradeArgs,
	RunE:  runUpgrade,
}

type upgradeMapping struct {
	src string
	dst string
	bin history.Binary
}

func init() {
	upgradeCmd.Flags().StringP("owner", "o", "", "GitHub owner/org (skip history lookup)")
	upgradeCmd.Flags().StringP("repo", "r", "", "GitHub repository (skip history lookup)")
	upgradeCmd.Flags().Bool("all", false, "upgrade all installed packages still present on disk")
	upgradeCmd.Flags().Bool("dry-run", false, "show what would be upgraded")
	upgradeCmd.ValidArgsFunction = completeInstalledUpgradeTargets
	registerOwnerRepoHistoryCompletions(upgradeCmd, true)
	rootCmd.AddCommand(upgradeCmd)
}

func validateUpgradeArgs(cmd *cobra.Command, args []string) error {
	upgradeAll, _ := cmd.Flags().GetBool("all")
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")

	if upgradeAll {
		if len(args) != 0 {
			return errors.New("--all does not take a target argument")
		}
		if ownerFlag != "" || repoFlag != "" {
			return errors.New("--all cannot be combined with --owner or --repo")
		}
		return nil
	}

	return cobra.ExactArgs(1)(cmd, args)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	upgradeAll, _ := cmd.Flags().GetBool("all")
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	cfg, err := config.Load(cfgViper)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Resolve history store
	histPath, err := config.HistoryFilePath()
	if err != nil {
		return fmt.Errorf("resolving history path: %w", err)
	}
	store := history.NewStore(histPath)
	if err := store.Load(); err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	if upgradeAll {
		return runUpgradeAll(cmd, store, cfg, dryRun)
	}

	rec, err := resolveUpgradeRecord(store, args[0], ownerFlag, repoFlag)
	if err != nil {
		return err
	}

	_, err = upgradeRecord(cmd, store, cfg, rec, dryRun)
	return err
}

func runUpgradeAll(cmd *cobra.Command, store *history.Store, cfg *config.AppConfig, dryRun bool) error {
	records := presentHistoryRecords(store.Records())
	skippedMissing := len(store.Records()) - len(records)
	if len(records) == 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No installed history records found."); err != nil {
			return fmt.Errorf("writing empty upgrade history message: %w", err)
		}
		return nil
	}

	var changed int
	var current int
	var failed int
	var failures []string

	for i := range records {
		rec := records[i]
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "==> %s/%s : %s\n", rec.Owner, rec.Repo, githubRepoReleasesURL(rec.Owner, rec.Repo)); err != nil {
			return fmt.Errorf("writing upgrade header for %s/%s: %w", rec.Owner, rec.Repo, err)
		}

		upgraded, err := upgradeRecord(cmd, store, cfg, &rec, dryRun)
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s/%s: %v", rec.Owner, rec.Repo, err))
			if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "Failed upgrading %s/%s: %v\n", rec.Owner, rec.Repo, err); writeErr != nil {
				return fmt.Errorf("writing upgrade failure for %s/%s: %w", rec.Owner, rec.Repo, writeErr)
			}
			if _, writeErr := fmt.Fprintln(cmd.OutOrStdout()); writeErr != nil {
				return fmt.Errorf("writing upgrade separator: %w", writeErr)
			}
			continue
		}

		if upgraded {
			changed++
		} else {
			current++
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
			return fmt.Errorf("writing upgrade separator: %w", err)
		}
	}

	action := "upgraded"
	if dryRun {
		action = "would upgrade"
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Summary: %d checked, %d %s, %d already at latest, %d skipped (missing), %d failed\n", len(records), changed, action, current, skippedMissing, failed); err != nil {
		return fmt.Errorf("writing upgrade summary: %w", err)
	}

	if failed > 0 {
		return fmt.Errorf("upgrade --all completed with failures:\n%s", strings.Join(failures, "\n"))
	}

	return nil
}

func resolveUpgradeRecord(store *history.Store, target, ownerFlag, repoFlag string) (*history.Record, error) {
	if ownerFlag != "" && repoFlag != "" {
		if r := store.FindByRepo(ownerFlag, repoFlag); r != nil {
			return r, nil
		}
		return nil, fmt.Errorf("no history found for %q; use 'getRelease history list' to see installed binaries", ownerFlag+"/"+repoFlag)
	}

	if strings.Contains(target, "/") {
		parts := strings.SplitN(target, "/", 2)
		if parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid target '%s'", target)
		}
		if r := store.FindByRepo(parts[0], parts[1]); r != nil {
			return r, nil
		}
		return nil, fmt.Errorf("no history found for %q; use 'getRelease history list' to see installed binaries", target)
	}

	matches := store.FindByBinary(target)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no history found for %q; use 'getRelease history list' to see installed binaries", target)
	}
	if len(matches) == 1 {
		r := matches[0]
		return &r, nil
	}

	items := make([]string, len(matches))
	for i, m := range matches {
		items[i] = fmt.Sprintf("%s/%s %s (installed %s)", m.Owner, m.Repo, m.Tag, m.InstalledAt.Format("2006-01-02"))
	}
	idx, err := selectItems(items, "Multiple history records match; choose one:")
	if err != nil {
		return nil, err
	}
	r := matches[idx]
	return &r, nil
}

func upgradeRecord(cmd *cobra.Command, store *history.Store, cfg *config.AppConfig, rec *history.Record, dryRun bool) (bool, error) {
	if rec == nil {
		return false, errors.New("internal: no history record resolved")
	}

	owner := rec.Owner
	repo := rec.Repo

	client := newGitHubClient()
	release, err := client.GetLatestRelease(owner, repo)
	if err != nil {
		return false, fmt.Errorf("fetching latest release for %s/%s: %w", owner, repo, err)
	}

	if release.TagName == rec.Tag {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Already at latest version (%s)\n", rec.Tag); err != nil {
			return false, fmt.Errorf("writing current-version message: %w", err)
		}
		return false, nil
	}

	// Find candidate asset: prefer exact name match
	var chosen github.Asset
	if rec.Asset.Name != "" {
		for _, a := range release.Assets {
			if a.Name == rec.Asset.Name {
				chosen = a
				break
			}
		}
	}

	if chosen.Name == "" {
		// Fallback to platform matching using recorded OS/Arch and user prefs
		prefs := cfg.AssetPreferences
		candidates := platform.MatchAssets(release.Assets, rec.OS, rec.Arch, prefs.Formats, prefs.ExcludePatterns)
		bestAsset, uniqueBest := platform.BestAsset(candidates, rec.OS, prefs.Formats)
		if len(candidates) == 0 {
			return false, fmt.Errorf("no matching assets found in release %s for %s/%s", release.TagName, owner, repo)
		}
		if len(candidates) == 1 || uniqueBest {
			chosen = bestAsset
		} else {
			items := make([]string, len(candidates))
			for i, a := range candidates {
				items[i] = fmt.Sprintf("%s (%s)", a.Name, formatBytes(a.Size))
			}
			idx, err := selectItems(items, "Select an asset to upgrade:")
			if err != nil {
				return false, err
			}
			chosen = candidates[idx]
		}
	}

	// Dry-run: show what would happen
	if dryRun {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s from %s to %s\n", githubRepoReleasesURL(owner, repo), rec.Tag, release.TagName); err != nil {
			return false, fmt.Errorf("writing dry-run upgrade summary: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Asset: %s (%s)\n", chosen.Name, formatBytes(chosen.Size)); err != nil {
			return false, fmt.Errorf("writing dry-run asset summary: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Download URL: %s\n", chosen.DownloadURL); err != nil {
			return false, fmt.Errorf("writing dry-run download URL: %w", err)
		}
		return true, nil
	}

	// Prepare download workspace
	ts := time.Now().Format("20060102T150405")
	workDir := filepath.Join(cfg.DownloadDir, fmt.Sprintf("%s-%s", repo, ts))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return false, fmt.Errorf("create download dir %s: %w", workDir, err)
	}

	destPath := filepath.Join(workDir, chosen.Name)
	slog.Info("downloading asset", "url", chosen.DownloadURL, "dest", destPath)
	n, err := client.DownloadAsset(chosen.DownloadURL, destPath)
	if err != nil {
		return false, fmt.Errorf("download asset: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Downloaded %s (%s)\n", chosen.Name, formatBytes(n)); err != nil {
		return false, fmt.Errorf("writing download summary: %w", err)
	}

	var extractedDir string
	if archive.IsArchive(chosen.Name) {
		extractedDir = filepath.Join(workDir, "extracted")
		if err := archive.Extract(destPath, extractedDir); err != nil {
			return false, fmt.Errorf("extracting archive: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Extracted to %s\n", extractedDir); err != nil {
			return false, fmt.Errorf("writing extraction summary: %w", err)
		}
	}

	// Map recorded binaries to files in the download/extracted payload
	var maps []upgradeMapping
	var missing []string

	if extractedDir != "" {
		found, err := archive.FindBinaries(extractedDir)
		if err != nil {
			return false, fmt.Errorf("scanning extracted files: %w", err)
		}
		maps, missing = buildArchiveUpgradeMappings(*rec, extractedDir, found)
	} else {
		maps, missing = buildSingleAssetUpgradeMappings(*rec, chosen, destPath)
	}

	if len(missing) > 0 {
		return false, fmt.Errorf("release %s for %s/%s is missing recorded binaries: %s", release.TagName, owner, repo, strings.Join(missing, ", "))
	}

	if len(maps) == 0 {
		return false, fmt.Errorf("no binaries found to install for %s/%s", owner, repo)
	}

	installer := newBinaryInstaller(cfg.InstallCommand)
	for _, m := range maps {
		slog.Info("installing binary", "source", m.src, "target", m.dst)
		if err := installer.Install(m.src, m.dst); err != nil {
			return false, fmt.Errorf("installing %s -> %s: %w", m.src, m.dst, err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Installed %s -> %s\n", m.src, m.dst); err != nil {
			return false, fmt.Errorf("writing install summary: %w", err)
		}
	}

	// Update history record
	updated := *rec
	updated.Tag = release.TagName
	updated.Asset = history.AssetInfo{Name: chosen.Name, URL: chosen.DownloadURL}
	if err := store.Add(updated); err != nil {
		return false, fmt.Errorf("updating history: %w", err)
	}
	if err := store.Save(); err != nil {
		return false, fmt.Errorf("saving history: %w", err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s to %s\n", githubRepoReleasesURL(owner, repo), release.TagName); err != nil {
		return false, fmt.Errorf("writing upgrade completion: %w", err)
	}
	return true, nil
}

func buildArchiveUpgradeMappings(rec history.Record, extractedDir string, found []string) ([]upgradeMapping, []string) {
	lookup := make(map[string]string, len(found))
	for _, rel := range found {
		base := filepath.Base(rel)
		candidate := filepath.Join(extractedDir, rel)
		if existing, ok := lookup[base]; ok {
			lookup[base] = cmp.Or(preferredUpgradePath(candidate, existing), existing)
			continue
		}
		lookup[base] = candidate
	}

	maps := make([]upgradeMapping, 0, len(rec.Binaries))
	missing := make([]string, 0)
	for _, bin := range rec.Binaries {
		src, ok := upgradeBinarySource(lookup, bin)
		if !ok {
			missing = append(missing, displayBinaryName(bin))
			continue
		}
		maps = append(maps, upgradeMapping{src: src, dst: bin.InstallPath, bin: bin})
	}

	return maps, missing
}

func buildSingleAssetUpgradeMappings(rec history.Record, chosen github.Asset, destPath string) ([]upgradeMapping, []string) {
	maps := make([]upgradeMapping, 0, len(rec.Binaries))
	missing := make([]string, 0)
	for _, bin := range rec.Binaries {
		if bin.Name == chosen.Name || bin.InstalledAs == chosen.Name {
			maps = append(maps, upgradeMapping{src: destPath, dst: bin.InstallPath, bin: bin})
			continue
		}
		missing = append(missing, displayBinaryName(bin))
	}

	return maps, missing
}

func upgradeBinarySource(lookup map[string]string, bin history.Binary) (string, bool) {
	for _, name := range uniqueBinaryNames(bin) {
		if src, ok := lookup[name]; ok {
			return src, true
		}
	}
	return "", false
}

func uniqueBinaryNames(bin history.Binary) []string {
	names := []string{bin.Name, bin.InstalledAs}
	return slices.DeleteFunc(names, func(name string) bool {
		return name == ""
	})
}

func displayBinaryName(bin history.Binary) string {
	if bin.InstalledAs != "" {
		return bin.InstalledAs
	}
	return bin.Name
}

func preferredUpgradePath(candidate, existing string) string {
	currentDepth := strings.Count(existing, string(os.PathSeparator))
	candidateDepth := strings.Count(candidate, string(os.PathSeparator))
	if candidateDepth < currentDepth {
		return candidate
	}
	return ""
}

func presentHistoryRecords(records []history.Record) []history.Record {
	var present []history.Record
	for _, rec := range records {
		if recordInstalled(rec) {
			present = append(present, rec)
		}
	}
	return present
}

func recordInstalled(rec history.Record) bool {
	for _, bin := range rec.Binaries {
		if bin.InstallPath == "" {
			continue
		}
		if _, err := os.Stat(bin.InstallPath); err == nil {
			return true
		}
	}
	return false
}
