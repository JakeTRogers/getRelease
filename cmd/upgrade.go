package cmd

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/archive"
	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/platform"
	"github.com/JakeTRogers/getRelease/internal/selector"
	"github.com/JakeTRogers/getRelease/internal/semver"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [<target> | --owner <owner> --repo <repo> | --all]",
	Short: "Upgrade a previously installed binary",
	Long: `Upgrade a previously installed binary using install history, or use --all to upgrade every installed package still present on disk.

The target is an installed binary name or owner/repo; --owner and --repo together select it instead.`,
	Args: validateUpgradeArgs,
	RunE: runUpgrade,
}

type upgradeMapping struct {
	src  string
	dst  string
	name string // binary path within the new payload, recorded in history
	bin  history.Binary
}

func init() {
	upgradeCmd.Flags().StringP("owner", "o", "", ownerTargetFlagUsage)
	upgradeCmd.Flags().StringP("repo", "r", "", repoTargetFlagUsage)
	upgradeCmd.Flags().Bool("all", false, "upgrade all installed packages still present on disk")
	upgradeCmd.Flags().Bool("dry-run", false, "show what would be upgraded")
	upgradeCmd.Flags().Int("cooldown", 0, "minimum release age in days (overrides config; 0 disables)")
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

	return validateTargetArgs(cmd, args)
}

const (
	ownerTargetFlagUsage = "GitHub owner/org of the installed target (with --repo, instead of <target>)"
	repoTargetFlagUsage  = "GitHub repository of the installed target (with --owner, instead of <target>)"
)

// validateTargetArgs requires an installed target selected by exactly one of:
// a <target> argument (binary name or owner/repo), or --owner and --repo.
func validateTargetArgs(cmd *cobra.Command, args []string) error {
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")

	if ownerFlag == "" && repoFlag == "" {
		if len(args) != 1 {
			return fmt.Errorf("specify one installed target as a binary name or owner/repo, or with --owner and --repo (got %d arguments)", len(args))
		}
		return nil
	}
	if ownerFlag == "" || repoFlag == "" {
		return errors.New("--owner and --repo must be used together")
	}
	if len(args) != 0 {
		return errors.New("specify the target either as an argument or with --owner and --repo, not both")
	}
	return nil
}

// targetArg returns the positional target, or "" when --owner and --repo
// select it instead.
func targetArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
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

	cds := resolveCooldownSettings(cmd, cfg)

	if upgradeAll {
		return runUpgradeAll(cmd, store, cfg, dryRun, cds)
	}

	rec, err := resolveUpgradeRecord(store, targetArg(args), ownerFlag, repoFlag)
	if err != nil {
		return err
	}

	_, err = upgradeRecord(cmd, store, cfg, rec, dryRun, cds)
	return err
}

func runUpgradeAll(cmd *cobra.Command, store *history.Store, cfg *config.AppConfig, dryRun bool, cds cooldownSettings) error {
	records := presentHistoryRecords(store.Records())
	skippedMissing := len(store.Records()) - len(records)
	if len(records) == 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No installed history records found."); err != nil {
			return fmt.Errorf("writing empty upgrade history message: %w", err)
		}
		return nil
	}

	var changed int
	var unchanged int
	var failed int
	var failures []string

	for i := range records {
		rec := records[i]
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "==> %s/%s\n", rec.Owner, rec.Repo); err != nil {
			return fmt.Errorf("writing upgrade header for %s/%s: %w", rec.Owner, rec.Repo, err)
		}

		upgraded, err := upgradeRecord(cmd, store, cfg, &rec, dryRun, cds)
		if errors.Is(err, selector.ErrCancelled) {
			// The user cancelled a prompt: stop rather than move on to the
			// remaining packages.
			return err
		}
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
			unchanged++
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
			return fmt.Errorf("writing upgrade separator: %w", err)
		}
	}

	action := "upgraded"
	if dryRun {
		action = "would upgrade"
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Summary: %d checked, %d %s, %d unchanged, %d skipped (missing), %d failed\n", len(records), changed, action, unchanged, skippedMissing, failed); err != nil {
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

func upgradeRecord(cmd *cobra.Command, store *history.Store, cfg *config.AppConfig, rec *history.Record, dryRun bool, cds cooldownSettings) (bool, error) {
	if rec == nil {
		return false, errors.New("internal: no history record resolved")
	}

	owner := rec.Owner
	repo := rec.Repo
	policy := cds.policyFor(owner)

	// Target the host the package was originally installed from (empty
	// means github.com).
	client, err := newGitHubClient(rec.Host)
	if err != nil {
		return false, err
	}
	release, unchanged, err := resolveUpgradeRelease(cmd, client, rec, policy)
	if err != nil {
		return false, err
	}
	if unchanged {
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

	if err := policy.checkAsset(release, chosen); err != nil {
		return false, err
	}

	// Dry-run: show what would happen
	if dryRun {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s/%s from %s to %s\n", owner, repo, rec.Tag, release.TagName); err != nil {
			return false, fmt.Errorf("writing dry-run upgrade summary: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Asset: %s (%s)\n", chosen.Name, formatBytes(chosen.Size)); err != nil {
			return false, fmt.Errorf("writing dry-run asset summary: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Download URL: %s\n", chosen.DownloadURL); err != nil {
			return false, fmt.Errorf("writing dry-run download URL: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Review the release notes: %s\n", githubReleasePageURL(rec.Host, owner, repo, release)); err != nil {
			return false, fmt.Errorf("writing dry-run release notes link: %w", err)
		}
		return true, nil
	}

	// Prepare download workspace
	workDir, err := newWorkDir(cfg.DownloadDir, repo)
	if err != nil {
		return false, err
	}

	destPath := filepath.Join(workDir, chosen.Name)
	slog.Info("downloading asset", "url", chosen.DownloadURL, "dest", destPath)
	n, err := client.DownloadAsset(chosen, destPath)
	if err != nil {
		return false, fmt.Errorf("download asset: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Downloaded %s (%s)\n", chosen.Name, formatBytes(n)); err != nil {
		return false, fmt.Errorf("writing download summary: %w", err)
	}

	var extractedDir string
	if archive.IsArchive(chosen.Name) {
		extractedDir = filepath.Join(workDir, extractedDirName)
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
		maps, missing = buildArchiveUpgradeMappings(*rec, chosen.Name, release.TagName, extractedDir, found)
	} else {
		if err := checkRawAssetExecutable(destPath, chosen.Name); err != nil {
			return false, err
		}
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

	// Update history record. Binary names are refreshed so the next upgrade
	// matches against this release's payload rather than the original one.
	updated := *rec
	updated.Tag = release.TagName
	updated.Asset = history.AssetInfo{Name: chosen.Name, URL: chosen.DownloadURL}
	updated.Binaries = make([]history.Binary, 0, len(maps))
	for _, m := range maps {
		bin := m.bin
		bin.Name = m.name
		updated.Binaries = append(updated.Binaries, bin)
	}
	if err := store.Add(updated); err != nil {
		return false, fmt.Errorf("updating history: %w", err)
	}
	if err := store.Save(); err != nil {
		return false, fmt.Errorf("saving history: %w", err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s/%s to %s\n", owner, repo, release.TagName); err != nil {
		return false, fmt.Errorf("writing upgrade completion: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Review the release notes: %s\n", githubReleasePageURL(rec.Host, owner, repo, release)); err != nil {
		return false, fmt.Errorf("writing release notes link: %w", err)
	}
	return true, nil
}

func resolveUpgradeRelease(cmd *cobra.Command, client releaseClient, rec *history.Record, policy cooldownPolicy) (*github.Release, bool, error) {
	owner := rec.Owner
	repo := rec.Repo

	switch rec.PinLevel {
	case history.PinNone:
		release, err := client.GetLatestRelease(owner, repo)
		if err != nil {
			return nil, false, err
		}

		if release.TagName == rec.Tag {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Already at latest version (%s)\n", rec.Tag); err != nil {
				return nil, false, fmt.Errorf("writing current-version message: %w", err)
			}
			return nil, true, nil
		}
		// The installed release can be newer than "latest", e.g. a
		// prerelease installed with --tag; upgrading would downgrade it.
		if c, ok := semver.CompareTags(release.TagName, rec.Tag); ok && c < 0 {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Installed %s is newer than the latest release (%s); not downgrading\n", rec.Tag, release.TagName); err != nil {
				return nil, false, fmt.Errorf("writing newer-installed message: %w", err)
			}
			return nil, true, nil
		}

		if policy.enabled() && !policy.releaseEligible(release) {
			releases, err := client.ListReleases(owner, repo, 100)
			if err != nil {
				return nil, false, err
			}
			fallback, err := findCooldownFallback(releases, owner, repo, policy)
			if err != nil {
				return nil, false, err
			}
			if !upgradeFallbackIsNewer(releases, fallback, rec.Tag) {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "latest %s is in the %d-day cooldown; already at newest eligible version (%s)\n", release.TagName, policy.days, rec.Tag); err != nil {
					return nil, false, fmt.Errorf("writing cooldown unchanged message: %w", err)
				}
				return nil, true, nil
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "release %s is %d day(s) old, cooldown is %d days — falling back to %s\n", release.TagName, policy.ageDays(release.PublishedAt), policy.days, fallback.TagName); err != nil {
				return nil, false, fmt.Errorf("writing cooldown fallback message: %w", err)
			}
			return fallback, false, nil
		}

		return release, false, nil

	case history.PinPatch:
		allowance := pinAllowanceDescription(rec.PinLevel, semver.Version{})
		slog.Info("pin policy active", "owner", owner, "repo", repo, "level", rec.PinLevel, "tag", rec.Tag, "allowance", allowance)
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), pinPolicySummary(rec.PinLevel, rec.Tag, semver.Version{})); err != nil {
			return nil, false, fmt.Errorf("writing pin policy message: %w", err)
		}
		return nil, true, nil

	case history.PinMinor, history.PinMajor:
		currentVersion, err := semver.Parse(rec.Tag)
		if err != nil {
			return nil, false, fmt.Errorf("parse pinned version %q for %s/%s: %w", rec.Tag, owner, repo, err)
		}

		allowance := pinAllowanceDescription(rec.PinLevel, currentVersion)
		slog.Info("pin policy active", "owner", owner, "repo", repo, "level", rec.PinLevel, "tag", rec.Tag, "allowance", allowance)
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), pinPolicySummary(rec.PinLevel, rec.Tag, currentVersion)); err != nil {
			return nil, false, fmt.Errorf("writing pin policy message: %w", err)
		}

		releases, err := client.ListReleases(owner, repo, 100)
		if err != nil {
			return nil, false, err
		}

		bestIndex := -1
		var bestVersion semver.Version
		blockedByCooldown := 0
		for i := range releases {
			release := releases[i]
			if release.TagName == rec.Tag || release.Draft || release.Prerelease {
				continue
			}
			if policy.enabled() && !policy.releaseEligible(&release) {
				slog.Debug("skipping release inside cooldown", "owner", owner, "repo", repo, "tag", release.TagName, "cooldownDays", policy.days)
				blockedByCooldown++
				continue
			}

			version, err := semver.Parse(release.TagName)
			if err != nil {
				slog.Debug("skipping non-semver release tag", "owner", owner, "repo", repo, "tag", release.TagName, "err", err)
				continue
			}
			if version.Compare(currentVersion) <= 0 {
				continue
			}

			allowed := false
			switch rec.PinLevel {
			case history.PinMinor:
				allowed = version.SameMinor(currentVersion)
			case history.PinMajor:
				allowed = version.SameMajor(currentVersion)
			}
			if !allowed {
				continue
			}

			if bestIndex == -1 || version.Compare(bestVersion) > 0 {
				bestIndex = i
				bestVersion = version
			}
		}

		if bestIndex == -1 {
			msg := "no newer eligible release found"
			if blockedByCooldown > 0 {
				msg = fmt.Sprintf("no newer eligible release found (%d blocked by %d-day cooldown)", blockedByCooldown, policy.days)
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), msg); err != nil {
				return nil, false, fmt.Errorf("writing unchanged pin message: %w", err)
			}
			return nil, true, nil
		}

		return &releases[bestIndex], false, nil

	default:
		return nil, false, fmt.Errorf("unsupported pin level %q for %s/%s", rec.PinLevel, owner, repo)
	}
}

// upgradeFallbackIsNewer reports whether a cooldown fallback release is an
// actual upgrade over the currently installed tag. Semver tags, including
// prereleases, are compared by precedence; otherwise the release list's
// publish order (most-recent-first) decides, so a non-semver fallback never
// downgrades an installed release that was published after it.
func upgradeFallbackIsNewer(releases []github.Release, fallback *github.Release, currentTag string) bool {
	if fallback.TagName == currentTag {
		return false
	}
	if c, ok := semver.CompareTags(fallback.TagName, currentTag); ok {
		return c > 0
	}
	for i := range releases {
		switch releases[i].TagName {
		case currentTag:
			return false
		case fallback.TagName:
			return true
		}
	}
	// The current tag is not among the recent releases; assume the fallback
	// supersedes it.
	return true
}

// buildArchiveUpgradeMappings maps each recorded binary to a file in the new
// release payload by exact recorded path, basename, then suggested install name.
// Only if those fail is the installed alias tried as a basename, then as a
// suggested install name. Suggested names strip platform and version suffixes.
func buildArchiveUpgradeMappings(rec history.Record, assetName, tag, extractedDir string, found []string) ([]upgradeMapping, []string) {
	byPath := make(map[string]string, len(found))
	byBase := make(map[string]string, len(found))
	byInstallName := make(map[string]string, len(found))
	for _, rel := range found {
		byPath[rel] = rel
		addUpgradeCandidate(byBase, filepath.Base(rel), rel)
		addUpgradeCandidate(byInstallName, platform.SuggestInstallName(rec.Repo, assetName, rel, rec.OS, rec.Arch, tag), rel)
	}

	maps := make([]upgradeMapping, 0, len(rec.Binaries))
	missing := make([]string, 0)
	for _, bin := range rec.Binaries {
		var recordedBase, recordedInstallName string
		if bin.Name != "" {
			recordedBase = filepath.Base(bin.Name)
			recordedInstallName = platform.SuggestInstallName(rec.Repo, rec.Asset.Name, bin.Name, rec.OS, rec.Arch, rec.Tag)
		}

		rel, ok := lookupUpgradeSource(byPath, bin.Name)
		if !ok {
			rel, ok = lookupUpgradeSource(byBase, recordedBase)
		}
		if !ok {
			rel, ok = lookupUpgradeSource(byInstallName, recordedInstallName)
		}
		if !ok {
			rel, ok = lookupUpgradeSource(byBase, bin.InstalledAs)
		}
		if !ok {
			rel, ok = lookupUpgradeSource(byInstallName, bin.InstalledAs)
		}
		if !ok {
			missing = append(missing, displayBinaryName(bin))
			continue
		}
		maps = append(maps, upgradeMapping{src: filepath.Join(extractedDir, rel), dst: bin.InstallPath, name: rel, bin: bin})
	}

	return maps, missing
}

// buildSingleAssetUpgradeMappings maps a non-archive asset, which is itself
// the binary, onto the recorded binaries. A single recorded binary is replaced
// whatever the asset is called, since asset names often embed the version
// (e.g. shfmt_v3.10.0_linux_amd64); otherwise the names must match.
func buildSingleAssetUpgradeMappings(rec history.Record, chosen github.Asset, destPath string) ([]upgradeMapping, []string) {
	if len(rec.Binaries) == 1 {
		bin := rec.Binaries[0]
		return []upgradeMapping{{src: destPath, dst: bin.InstallPath, name: chosen.Name, bin: bin}}, nil
	}

	maps := make([]upgradeMapping, 0, len(rec.Binaries))
	missing := make([]string, 0)
	for _, bin := range rec.Binaries {
		if bin.Name == chosen.Name || bin.InstalledAs == chosen.Name {
			maps = append(maps, upgradeMapping{src: destPath, dst: bin.InstallPath, name: chosen.Name, bin: bin})
			continue
		}
		missing = append(missing, displayBinaryName(bin))
	}

	return maps, missing
}

// addUpgradeCandidate records rel under name, keeping the shallower path when
// two payload files share a name.
func addUpgradeCandidate(lookup map[string]string, name, rel string) {
	if existing, ok := lookup[name]; ok {
		lookup[name] = cmp.Or(preferredUpgradePath(rel, existing), existing)
		return
	}
	lookup[name] = rel
}

func lookupUpgradeSource(lookup map[string]string, names ...string) (string, bool) {
	for _, name := range names {
		if name == "" {
			continue
		}
		if rel, ok := lookup[name]; ok {
			return rel, true
		}
	}
	return "", false
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
