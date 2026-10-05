package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/JakeTRogers/getRelease/internal/archive"
	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/platform"
	"github.com/JakeTRogers/getRelease/internal/selector"
)

var cfgViper = viper.New()

type rootCommandResult struct {
	Owner        string          `json:"owner"`
	Repo         string          `json:"repo"`
	RequestedTag string          `json:"requestedTag,omitempty"`
	ReleaseTag   string          `json:"releaseTag"`
	ReleaseName  string          `json:"releaseName,omitempty"`
	Asset        github.Asset    `json:"asset"`
	DownloadPath string          `json:"downloadPath"`
	DownloadSize int64           `json:"downloadSize"`
	Extracted    bool            `json:"extracted"`
	ExtractDir   string          `json:"extractDir,omitempty"`
	DownloadOnly bool            `json:"downloadOnly"`
	Binaries     []string        `json:"binaries,omitempty"`
	Installed    []string        `json:"installed,omitempty"`
	HistoryPath  string          `json:"historyPath,omitempty"`
	Untracked    []string        `json:"untracked,omitempty"`
	Cooldown     *cooldownReport `json:"cooldown,omitempty"`
}

// cooldownReport records that the latest release was skipped because it was
// inside the cooldown window and a fallback release was installed instead.
type cooldownReport struct {
	Days           int    `json:"days"`
	SkippedTag     string `json:"skippedTag"`
	SkippedAgeDays int    `json:"skippedAgeDays"`
}

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "getRelease",
	Short: "Download, extract, and install binary releases from GitHub",
	Long: `getRelease downloads binary releases from GitHub repositories,
extracts archives, and installs selected binaries to a configurable
target directory.

Specify a repository using --owner and --repo flags or a --url flag.
By default, the latest release is installed. The asset matching the
current OS and architecture is selected automatically, with a prompt
when several match equally well.`,
	Example: `  # Install the latest release
  getRelease --owner sharkdp --repo bat
  getRelease --url https://github.com/junegunn/fzf

  # Install a specific release, or only download it
  getRelease -o junegunn -r fzf --tag v0.66.0
  getRelease -o sharkdp -r fd --download-only

  # Upgrade everything installed with getRelease
  getRelease upgrade --all`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return initConfig(cmd)
	},
	RunE: runRoot,
}

// exitCancelled is the exit status when the user cancels an interactive prompt.
const exitCancelled = 2

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(reportError(os.Stderr, err))
	}
}

// reportError prints err for the user and returns the process exit status:
// exitCancelled, printing nothing, when the user cancelled a prompt, and 1
// otherwise. A rate limit error is printed without its wrapping context.
func reportError(w io.Writer, err error) int {
	if errors.Is(err, selector.ErrCancelled) {
		return exitCancelled
	}
	var rateLimitErr *github.RateLimitError
	if errors.As(err, &rateLimitErr) {
		err = rateLimitErr
	}
	// The process exits next either way, so a failed write is not reported.
	_, _ = fmt.Fprintf(w, "Error: %s\n", err)
	return 1
}

func init() {
	// Persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().CountP("verbose", "v", "increase log verbosity: -v for info, -vv for debug")

	// Root-specific flags
	rootCmd.Flags().StringP("owner", "o", "", "GitHub owner/org name")
	rootCmd.Flags().StringP("repo", "r", "", "GitHub repository name")
	rootCmd.Flags().StringP("url", "u", "", "GitHub repository URL")
	rootCmd.Flags().String("host", "", "GitHub host for --owner/--repo: github.com (default) or a *.ghe.com host (GitHub Enterprise Cloud with data residency)")
	rootCmd.Flags().StringP("tag", "t", "", "release tag/version (default: latest)")
	rootCmd.Flags().BoolP("download-only", "d", false, "download without installing; archives are extracted unless autoExtract is false")
	rootCmd.Flags().String("install-as", "", "override installed filename when exactly one binary is installed")
	rootCmd.Flags().String("format", "text", "output format: text, json")
	rootCmd.Flags().Int("cooldown", 0, "minimum release age in days (overrides config; 0 disables)")

	// Mutual exclusivity: --url vs --owner/--repo/--host (a URL carries its
	// own host)
	rootCmd.MarkFlagsMutuallyExclusive("url", "owner")
	rootCmd.MarkFlagsMutuallyExclusive("url", "repo")
	rootCmd.MarkFlagsMutuallyExclusive("url", "host")

	// Registered explicitly (Cobra's Execute() would add this too, but only once
	// the command is run) so TestRootHasCompletionCommand can assert its
	// presence without calling Execute(); InitDefaultCompletionCmd is a no-op
	// if a completion command is already registered, so this is safe.
	rootCmd.InitDefaultCompletionCmd()
	registerOwnerRepoHistoryCompletions(rootCmd, false)
	mustRegisterFlagCompletion(rootCmd, "format", completeOutputFormatValues)
}

// initConfig sets up Viper and configures the slog logger.
func initConfig(cmd *cobra.Command) error {
	if err := config.Init(cfgViper); err != nil {
		return err
	}

	// Set log level based on verbosity count. Warnings are always shown:
	// they report problems such as skipped history records.
	verbose, _ := cmd.Flags().GetCount("verbose")
	var level slog.Level
	switch {
	case verbose >= 2:
		level = slog.LevelDebug
	case verbose == 1:
		level = slog.LevelInfo
	default:
		level = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	return nil
}

// resolveRepo determines the owner, repo, and GitHub host from flags,
// returning an error if the input is invalid or incomplete. The host is
// taken from the --url host or the --host flag; with --owner/--repo alone it
// comes from the repository's install history record, and defaults to
// github.com for repositories not installed before.
func resolveRepo(cmd *cobra.Command) (owner, repo, host string, err error) {
	urlFlag, _ := cmd.Flags().GetString("url")
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")
	hostFlag, _ := cmd.Flags().GetString("host")

	if urlFlag != "" {
		owner, repo, host, err = github.ParseRepoURL(urlFlag)
		if err != nil {
			return "", "", "", fmt.Errorf("parsing URL: %w", err)
		}
		return owner, repo, host, nil
	}

	if ownerFlag == "" && repoFlag == "" {
		return "", "", "", errors.New("specify a repository with --owner and --repo, or use --url")
	}
	if ownerFlag == "" {
		return "", "", "", errors.New("--owner is required when using --repo")
	}
	if repoFlag == "" {
		return "", "", "", errors.New("--repo is required when using --owner")
	}

	if hostFlag != "" {
		host, err = github.NormalizeHost(hostFlag)
		if err != nil {
			return "", "", "", err
		}
		return ownerFlag, repoFlag, host, nil
	}

	return ownerFlag, repoFlag, historyHostFor(ownerFlag, repoFlag), nil
}

// historyHostFor returns the GitHub host recorded for owner/repo in install
// history, defaulting to github.com when the repository has no record. This
// is the only automatic host source: a package installed from a *.ghe.com
// host keeps targeting that host without needing --host again.
func historyHostFor(owner, repo string) string {
	histPath, err := config.HistoryFilePath()
	if err != nil {
		return github.DefaultHost
	}
	store := history.NewStore(histPath)
	if err := store.Load(); err != nil {
		slog.Debug("skipping history host lookup", "err", err)
		return github.DefaultHost
	}
	rec := store.FindByRepo(owner, repo)
	if rec == nil || rec.Host == "" {
		return github.DefaultHost
	}
	host, err := github.NormalizeHost(rec.Host)
	if err != nil {
		slog.Warn("ignoring invalid host in history record", "owner", owner, "repo", repo, "host", rec.Host, "err", err)
		return github.DefaultHost
	}
	slog.Debug("using GitHub host from install history", "owner", owner, "repo", repo, "host", host)
	return host
}

// canonicalRepoName returns GitHub's spelling of owner/repo, taken from the
// release's web URL, so history records the same name however the user
// capitalized it. The requested names are kept when the URL is missing or
// names a different repository, as after a rename redirect.
func canonicalRepoName(owner, repo string, rel *github.Release) (string, string) {
	urlOwner, urlRepo, _, err := github.ParseRepoURL(rel.HTMLURL)
	if err != nil || !strings.EqualFold(urlOwner, owner) || !strings.EqualFold(urlRepo, repo) {
		return owner, repo
	}
	return urlOwner, urlRepo
}

// anyFlagChanged reports whether any flag was set on the command line.
func anyFlagChanged(cmd *cobra.Command) bool {
	changed := false
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		changed = changed || f.Changed
	})
	return changed
}

// runRoot implements the full download/extract/select/install pipeline.
func runRoot(cmd *cobra.Command, args []string) error {
	// Validate here to preserve Cobra's typo suggestions during command lookup.
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	if !anyFlagChanged(cmd) {
		// Bare invocation: show how to use the tool rather than an error.
		return cmd.Help()
	}

	owner, repo, host, err := resolveRepo(cmd)
	if err != nil {
		return err
	}

	tag, _ := cmd.Flags().GetString("tag")
	format, _ := cmd.Flags().GetString("format")
	format, err = normalizeOutputFormat(format)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	textOutput := format == "text"
	result := rootCommandResult{
		Owner:        owner,
		Repo:         repo,
		RequestedTag: tag,
	}

	slog.Info("resolved repository", "owner", owner, "repo", repo, "host", host, "tag", tag)

	// Load configuration
	cfg, err := config.Load(cfgViper)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Determine OS/Arch (allow config overrides)
	var osName, arch string
	if cfg.AssetPreferences.OS != "" {
		osName = cfg.AssetPreferences.OS
	} else {
		osName = platform.Detect().OS
	}
	if cfg.AssetPreferences.Arch != "" {
		arch = cfg.AssetPreferences.Arch
	} else {
		arch = platform.Detect().Arch
	}

	// Create GitHub client and fetch release
	client, err := newGitHubClient(host)
	if err != nil {
		return err
	}
	policy := resolveCooldownSettings(cmd, cfg).policyFor(owner)
	var rel *github.Release
	if tag != "" {
		rel, err = client.GetReleaseByTag(owner, repo, tag)
		if err != nil {
			return err
		}
		if err := policy.checkRelease(rel); err != nil {
			return err
		}
	} else {
		rel, err = client.GetLatestRelease(owner, repo)
		if err != nil {
			return err
		}
		if policy.enabled() && !policy.releaseEligible(rel) {
			releases, err := client.ListReleases(owner, repo, 100)
			if err != nil {
				return err
			}
			fallback, err := findCooldownFallback(releases, owner, repo, policy)
			if err != nil {
				return err
			}
			result.Cooldown = &cooldownReport{
				Days:           policy.days,
				SkippedTag:     rel.TagName,
				SkippedAgeDays: policy.ageDays(rel.PublishedAt),
			}
			rel = fallback
		}
	}
	owner, repo = canonicalRepoName(owner, repo, rel)
	result.Owner = owner
	result.Repo = repo
	result.ReleaseTag = rel.TagName
	result.ReleaseName = rel.DisplayName()

	which := "latest"
	if tag != "" {
		which = tag
	}
	if textOutput {
		if _, err := fmt.Fprintf(out, "Fetching %s release for %s/%s...\n", which, owner, repo); err != nil {
			return fmt.Errorf("writing release heading: %w", err)
		}
		if tag != "" && rel.TagName != tag {
			if _, err := fmt.Fprintf(out, "  tag %s not found, using %s\n", tag, rel.TagName); err != nil {
				return fmt.Errorf("writing tag fallback message: %w", err)
			}
		}
		if cd := result.Cooldown; cd != nil {
			if _, err := fmt.Fprintf(out, "  release %s is %d day(s) old, cooldown is %d days — falling back to %s\n", cd.SkippedTag, cd.SkippedAgeDays, cd.Days, rel.TagName); err != nil {
				return fmt.Errorf("writing cooldown fallback message: %w", err)
			}
		}
		if _, err := fmt.Fprintf(out, "  Release: %s (%s)\n\n", rel.DisplayName(), rel.TagName); err != nil {
			return fmt.Errorf("writing release heading: %w", err)
		}
	}

	// Match assets
	matches := platform.MatchAssets(rel.Assets, osName, arch, cfg.AssetPreferences.Formats, cfg.AssetPreferences.ExcludePatterns)
	bestAsset, uniqueBest := platform.BestAsset(matches, osName, cfg.AssetPreferences.Formats)

	var selectedAsset github.Asset
	switch len(matches) {
	case 0:
		var names []string
		for _, a := range rel.Assets {
			names = append(names, a.Name)
		}
		return fmt.Errorf("no matching assets for %s/%s; available: %s", osName, arch, strings.Join(names, ", "))
	case 1:
		selectedAsset = bestAsset
		if textOutput {
			if _, err := fmt.Fprintf(out, "  -> %s (auto-selected, single match)\n", selectedAsset.Name); err != nil {
				return fmt.Errorf("writing single asset selection: %w", err)
			}
		}
	default:
		if uniqueBest {
			selectedAsset = bestAsset
			if textOutput {
				if _, err := fmt.Fprintf(out, "  -> %s (auto-selected, preferred match)\n", selectedAsset.Name); err != nil {
					return fmt.Errorf("writing preferred asset selection: %w", err)
				}
			}
			break
		}
		var items []string
		for _, a := range matches {
			items = append(items, a.Name)
		}
		idx, err := selectItems(items, "Select an asset to download")
		if err != nil {
			return fmt.Errorf("selecting asset: %w", err)
		}
		selectedAsset = matches[idx]
	}
	if err := policy.checkAsset(rel, selectedAsset); err != nil {
		return err
	}
	result.Asset = selectedAsset

	// Prepare work directory
	workDir, err := newWorkDir(cfg.DownloadDir, repo)
	if err != nil {
		return err
	}

	// Download asset
	assetPath := filepath.Join(workDir, selectedAsset.Name)
	if textOutput {
		if _, err := fmt.Fprintf(out, "Downloading %s...\n", selectedAsset.Name); err != nil {
			return fmt.Errorf("writing download message: %w", err)
		}
	}
	size, err := client.DownloadAsset(selectedAsset, assetPath)
	if err != nil {
		return fmt.Errorf("downloading asset: %w", err)
	}
	result.DownloadPath = assetPath
	result.DownloadSize = size
	if textOutput {
		if _, err := fmt.Fprintf(out, "  Downloaded %s\n\n", formatBytes(size)); err != nil {
			return fmt.Errorf("writing download summary: %w", err)
		}
	}

	// Extract archives. Installing requires it; autoExtract only decides
	// whether --download-only extracts.
	downloadOnly, _ := cmd.Flags().GetBool("download-only")
	if archive.IsArchive(selectedAsset.Name) && (cfg.AutoExtract || !downloadOnly) {
		if textOutput {
			if _, err := fmt.Fprintf(out, "Extracting %s...\n", selectedAsset.Name); err != nil {
				return fmt.Errorf("writing extraction message: %w", err)
			}
		}
		extractDir := filepath.Join(workDir, extractedDirName)
		if err := archive.Extract(assetPath, extractDir); err != nil {
			return fmt.Errorf("extracting asset: %w", err)
		}
		result.Extracted = true
		result.ExtractDir = extractDir
	}

	// If download-only, report path and exit
	if downloadOnly {
		result.DownloadOnly = true
		if textOutput {
			if _, err := fmt.Fprintf(out, "Downloaded to %s\n", assetPath); err != nil {
				return fmt.Errorf("writing download-only result: %w", err)
			}
			if result.Extracted {
				if _, err := fmt.Fprintf(out, "Extracted to %s\n", result.ExtractDir); err != nil {
					return fmt.Errorf("writing extraction result: %w", err)
				}
			}
			return nil
		}
		return outputRootResult(out, result)
	}

	// Find binaries, as paths relative to payloadDir
	payloadDir := workDir
	var bins []string
	if result.Extracted {
		payloadDir = result.ExtractDir
		bins, err = archive.FindBinaries(payloadDir)
		if err != nil {
			return fmt.Errorf("finding binaries: %w", err)
		}
	} else {
		// downloaded file itself is the binary
		if err := checkRawAssetExecutable(assetPath, selectedAsset.Name); err != nil {
			return err
		}
		bins = []string{selectedAsset.Name}
	}

	if len(bins) == 0 {
		return fmt.Errorf("no installable binaries found in %s", payloadDir)
	}

	// Select binaries to install
	var toInstall []string
	if len(bins) == 1 {
		if textOutput {
			if _, err := fmt.Fprintf(out, "  -> %s (auto-selected, single binary)\n", bins[0]); err != nil {
				return fmt.Errorf("writing single binary selection: %w", err)
			}
		}
		toInstall = []string{bins[0]}
	} else {
		ok, err := confirmAction(fmt.Sprintf("Install all %d binaries?", len(bins)), true)
		if err != nil {
			return fmt.Errorf("confirmation prompt: %w", err)
		}
		if ok {
			toInstall = bins
		} else {
			idx, err := selectItems(bins, "Select a binary to install")
			if err != nil {
				return fmt.Errorf("selecting binary: %w", err)
			}
			toInstall = []string{bins[idx]}
		}
	}

	// Install selected binaries
	var installedPaths []string
	var installedNames []string
	installer := newBinaryInstaller(cfg.InstallCommand)
	installAs, _ := cmd.Flags().GetString("install-as")
	installNames, err := resolveInstallNamesForSelection(repo, selectedAsset.Name, osName, arch, rel.TagName, toInstall, installAs)
	if err != nil {
		return err
	}
	for _, bin := range toInstall {
		src := filepath.Join(payloadDir, bin)
		absSrc, err := filepath.Abs(src)
		if err != nil {
			return fmt.Errorf("resolving source path: %w", err)
		}
		installedAs := installNames[bin]
		if installedAs == "" {
			installedAs = filepath.Base(bin)
		}
		target := filepath.Join(cfg.InstallDir, installedAs)
		if textOutput {
			if _, err := fmt.Fprintf(out, "  Installing %s -> %s\n", bin, target); err != nil {
				return fmt.Errorf("writing install message for %s: %w", bin, err)
			}
		}
		if err := installer.Install(absSrc, target); err != nil {
			return fmt.Errorf("installing %s: %w", bin, err)
		}
		installedNames = append(installedNames, installedAs)
		installedPaths = append(installedPaths, target)
	}
	result.Binaries = append([]string(nil), toInstall...)
	result.Installed = append([]string(nil), installedPaths...)

	// Update history
	histPath, err := config.HistoryFilePath()
	if err != nil {
		return fmt.Errorf("resolving history path: %w", err)
	}
	store := history.NewStore(histPath)
	if err := store.Load(); err != nil {
		return fmt.Errorf("loading history: %w", err)
	}
	existing := store.FindByRepo(owner, repo)

	rec := history.Record{
		Owner: owner,
		Repo:  repo,
		Tag:   rel.TagName,
		Asset: history.AssetInfo{Name: selectedAsset.Name, URL: selectedAsset.DownloadURL},
		OS:    osName,
		Arch:  arch,
	}
	if host != github.DefaultHost {
		rec.Host = host
	}
	if existing != nil {
		rec.PinLevel = existing.PinLevel
	}
	for i, bin := range toInstall {
		rec.Binaries = append(rec.Binaries, history.Binary{
			Name:        bin,
			InstalledAs: installedNames[i],
			InstallPath: installedPaths[i],
		})
	}

	if err := store.Add(rec); err != nil {
		return fmt.Errorf("adding history record: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("saving history: %w", err)
	}
	result.HistoryPath = histPath
	if existing != nil {
		result.Untracked = untrackedBinaries(existing.Binaries, installedPaths)
	}

	if textOutput {
		if _, err := fmt.Fprintf(out, "\nHistory updated: %s/%s %s -> %s\n", owner, repo, rel.TagName, strings.Join(installedPaths, ", ")); err != nil {
			return fmt.Errorf("writing history update message: %w", err)
		}
		if _, err := fmt.Fprintf(out, "Review the release notes: %s\n", githubReleasePageURL(host, owner, repo, rel)); err != nil {
			return fmt.Errorf("writing release notes message: %w", err)
		}
	} else if err := outputRootResult(out, result); err != nil {
		return err
	}
	return writeUntrackedNotes(cmd.ErrOrStderr(), result.Untracked, owner, repo, existing)
}

// writeUntrackedNotes tells the user about binaries from the previous install
// that are no longer tracked. History holds one record per repository, so a
// reinstall that installs different binaries stops tracking the old ones.
func writeUntrackedNotes(w io.Writer, untracked []string, owner, repo string, previous *history.Record) error {
	for _, path := range untracked {
		if _, err := fmt.Fprintf(w, "Note: %s from the previous %s/%s install (%s) is no longer tracked; it was left in place\n", path, owner, repo, previous.Tag); err != nil {
			return fmt.Errorf("writing untracked binary note: %w", err)
		}
	}
	return nil
}

// untrackedBinaries returns the install paths of previous binaries that are
// still on disk but not among the newly installed paths.
func untrackedBinaries(previous []history.Binary, installedPaths []string) []string {
	var untracked []string
	for _, bin := range previous {
		if bin.InstallPath == "" || slices.Contains(installedPaths, bin.InstallPath) {
			continue
		}
		if _, err := os.Stat(bin.InstallPath); err == nil {
			untracked = append(untracked, bin.InstallPath)
		}
	}
	return untracked
}

func normalizeOutputFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported output format %q: use text or json", format)
	}
}

func outputRootResult(w io.Writer, result rootCommandResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func resolveInstallNamesForSelection(repo, assetName, osName, arch, tag string, binaries []string, installAs string) (map[string]string, error) {
	if strings.TrimSpace(installAs) == "" {
		return platform.ResolveInstallNames(repo, assetName, osName, arch, tag, binaries), nil
	}

	if len(binaries) != 1 {
		return nil, errors.New("--install-as requires exactly one binary to install")
	}

	name, err := validateInstallName(installAs)
	if err != nil {
		return nil, fmt.Errorf("invalid --install-as value: %w", err)
	}

	return map[string]string{binaries[0]: name}, nil
}

// extractedDirName is the directory inside a work directory that archives
// are extracted into.
const extractedDirName = "extracted"

// newWorkDir creates a directory under downloadDir for one download, named
// after the repository and the current time plus a random suffix, so runs in
// the same second or repositories sharing a name never share a directory.
func newWorkDir(downloadDir, repo string) (string, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return "", fmt.Errorf("creating download dir %s: %w", downloadDir, err)
	}
	dir, err := os.MkdirTemp(downloadDir, fmt.Sprintf("%s-%s-*", repo, time.Now().Format("20060102T150405")))
	if err != nil {
		return "", fmt.Errorf("creating work dir: %w", err)
	}
	// MkdirTemp creates the directory 0700; keep the 0755 previously used,
	// so an installCommand running as another user can read the payload.
	if err := os.Chmod(dir, 0o755); err != nil {
		return "", fmt.Errorf("setting work dir permissions: %w", err)
	}
	return dir, nil
}

// checkRawAssetExecutable refuses to install a downloaded non-archive asset
// that does not look like an executable, such as a file compressed in a
// format getRelease cannot unpack.
func checkRawAssetExecutable(path, assetName string) error {
	ok, err := archive.LooksExecutable(path)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", assetName, err)
	}
	if !ok {
		return fmt.Errorf("asset %s does not look like an executable (no ELF, Mach-O, PE, or #! header); it may use an unsupported archive or compression format, use --download-only to fetch it anyway", assetName)
	}
	return nil
}

func validateInstallName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("name is empty")
	}
	if trimmed == "." || trimmed == ".." {
		return "", errors.New("name must not be '.' or '..'")
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", errors.New("name must not contain path separators")
	}
	if filepath.Base(trimmed) != trimmed {
		return "", errors.New("name must be a basename")
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", errors.New("name contains control characters")
		}
	}
	return trimmed, nil
}
