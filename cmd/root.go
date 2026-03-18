package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
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
	Owner        string       `json:"owner"`
	Repo         string       `json:"repo"`
	RequestedTag string       `json:"requestedTag,omitempty"`
	ReleaseTag   string       `json:"releaseTag"`
	ReleaseName  string       `json:"releaseName,omitempty"`
	Asset        github.Asset `json:"asset"`
	DownloadPath string       `json:"downloadPath"`
	DownloadSize int64        `json:"downloadSize"`
	Extracted    bool         `json:"extracted"`
	DownloadOnly bool         `json:"downloadOnly"`
	Binaries     []string     `json:"binaries,omitempty"`
	Installed    []string     `json:"installed,omitempty"`
	HistoryPath  string       `json:"historyPath,omitempty"`
}

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "getRelease",
	Short: "Download, extract, and install binary releases from GitHub",
	Long: `getRelease downloads binary releases from GitHub repositories,
extracts archives, and installs selected binaries to a configurable
target directory.

Specify a repository using --owner and --repo flags or a --url flag.
By default, the latest release is fetched and assets matching the
current OS and architecture are presented for selection.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return initConfig(cmd)
	},
	RunE: runRoot,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		var rateLimitErr *github.RateLimitError
		if errors.As(err, &rateLimitErr) {
			if _, writeErr := fmt.Fprintf(os.Stderr, "Error: %s\n", rateLimitErr); writeErr != nil {
				os.Exit(1)
			}
			os.Exit(1)
		}
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error: %s\n", err); writeErr != nil {
			os.Exit(1)
		}
		os.Exit(1)
	}
}

func init() {
	// Persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().CountP("verbose", "v", "increase log verbosity (repeatable: -v, -vv, -vvv)")

	// Root-specific flags
	rootCmd.Flags().StringP("owner", "o", "", "GitHub owner/org name")
	rootCmd.Flags().StringP("repo", "r", "", "GitHub repository name")
	rootCmd.Flags().StringP("url", "u", "", "GitHub repository URL")
	rootCmd.Flags().StringP("tag", "t", "", "release tag/version (default: latest)")
	rootCmd.Flags().BoolP("download-only", "d", false, "download and extract without installing")
	rootCmd.Flags().String("install-as", "", "override installed filename when exactly one binary is installed")
	rootCmd.Flags().String("format", "text", "output format: text, json")

	// Mutual exclusivity: --url vs --owner/--repo
	rootCmd.MarkFlagsMutuallyExclusive("url", "owner")
	rootCmd.MarkFlagsMutuallyExclusive("url", "repo")

	rootCmd.InitDefaultCompletionCmd()
	registerOwnerRepoHistoryCompletions(rootCmd, false)
}

// initConfig sets up Viper and configures the slog logger.
func initConfig(cmd *cobra.Command) error {
	if err := config.Init(cfgViper); err != nil {
		return err
	}

	// Set log level based on verbosity count
	verbose, _ := cmd.Flags().GetCount("verbose")
	var level slog.Level
	switch {
	case verbose >= 3:
		level = slog.LevelDebug
	case verbose == 2:
		level = slog.LevelInfo
	case verbose == 1:
		level = slog.LevelWarn
	default:
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	return nil
}

// resolveRepo determines the owner and repo from flags, returning an error if
// the input is invalid or incomplete.
func resolveRepo(cmd *cobra.Command) (owner, repo string, err error) {
	urlFlag, _ := cmd.Flags().GetString("url")
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")

	if urlFlag != "" {
		owner, repo, err = github.ParseRepoURL(urlFlag)
		if err != nil {
			return "", "", fmt.Errorf("parsing URL: %w", err)
		}
		return owner, repo, nil
	}

	if ownerFlag == "" && repoFlag == "" {
		return "", "", errors.New("specify a repository with --owner and --repo, or use --url")
	}
	if ownerFlag == "" {
		return "", "", errors.New("--owner is required when using --repo")
	}
	if repoFlag == "" {
		return "", "", errors.New("--repo is required when using --owner")
	}

	return ownerFlag, repoFlag, nil
}

// runRoot implements the full download/extract/select/install pipeline.
func runRoot(cmd *cobra.Command, _ []string) error {
	owner, repo, err := resolveRepo(cmd)
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

	slog.Info("resolved repository", "owner", owner, "repo", repo, "tag", tag)

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
	client := newGitHubClient()
	var rel *github.Release
	if tag != "" {
		rel, err = client.GetReleaseByTag(owner, repo, tag)
	} else {
		rel, err = client.GetLatestRelease(owner, repo)
	}
	if err != nil {
		return fmt.Errorf("fetching release: %w", err)
	}
	result.ReleaseTag = rel.TagName
	result.ReleaseName = rel.DisplayName()

	which := "latest"
	if tag != "" {
		which = tag
	}
	if textOutput {
		if _, err := fmt.Fprintf(out, "Fetching %s release for %s/%s...\n  Release: %s (%s)\n\n", which, owner, repo, rel.DisplayName(), rel.TagName); err != nil {
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
			if errors.Is(err, selector.ErrCancelled) {
				os.Exit(2)
			}
			return fmt.Errorf("selecting asset: %w", err)
		}
		selectedAsset = matches[idx]
	}
	result.Asset = selectedAsset

	// Prepare work directory
	workDir := filepath.Join(cfg.DownloadDir, fmt.Sprintf("%s-%d", repo, time.Now().Unix()))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("creating work dir: %w", err)
	}

	// Download asset
	assetPath := filepath.Join(workDir, selectedAsset.Name)
	if textOutput {
		if _, err := fmt.Fprintf(out, "Downloading %s...\n", selectedAsset.Name); err != nil {
			return fmt.Errorf("writing download message: %w", err)
		}
	}
	size, err := client.DownloadAsset(selectedAsset.DownloadURL, assetPath)
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

	// Extract if appropriate
	extracted := false
	if cfg.AutoExtract && archive.IsArchive(selectedAsset.Name) {
		if textOutput {
			if _, err := fmt.Fprintf(out, "Extracting %s...\n", selectedAsset.Name); err != nil {
				return fmt.Errorf("writing extraction message: %w", err)
			}
		}
		if err := archive.Extract(assetPath, workDir); err != nil {
			return fmt.Errorf("extracting asset: %w", err)
		}
		extracted = true
	}
	result.Extracted = extracted

	// If download-only, report path and exit
	downloadOnly, _ := cmd.Flags().GetBool("download-only")
	if downloadOnly {
		result.DownloadOnly = true
		if textOutput {
			if _, err := fmt.Fprintf(out, "Downloaded to %s\n", assetPath); err != nil {
				return fmt.Errorf("writing download-only result: %w", err)
			}
			return nil
		}
		return outputRootResult(out, result)
	}

	// Find binaries
	var bins []string
	if extracted {
		bins, err = archive.FindBinaries(workDir)
		if err != nil {
			return fmt.Errorf("finding binaries: %w", err)
		}
	} else {
		if archive.IsArchive(selectedAsset.Name) {
			// archive was not extracted - no binaries available
			bins = nil
		} else {
			// downloaded file itself is the binary
			bins = []string{selectedAsset.Name}
		}
	}

	if len(bins) == 0 {
		return fmt.Errorf("no installable binaries found in %s", workDir)
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
			if errors.Is(err, selector.ErrCancelled) {
				os.Exit(2)
			}
			return fmt.Errorf("confirmation prompt: %w", err)
		}
		if ok {
			toInstall = bins
		} else {
			idx, err := selectItems(bins, "Select a binary to install")
			if err != nil {
				if errors.Is(err, selector.ErrCancelled) {
					os.Exit(2)
				}
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
	installNames, err := resolveInstallNamesForSelection(repo, selectedAsset.Name, osName, arch, toInstall, installAs)
	if err != nil {
		return err
	}
	for _, bin := range toInstall {
		src := filepath.Join(workDir, bin)
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

	rec := history.Record{
		Owner: owner,
		Repo:  repo,
		Tag:   rel.TagName,
		Asset: history.AssetInfo{Name: selectedAsset.Name, URL: selectedAsset.DownloadURL},
		OS:    osName,
		Arch:  arch,
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

	if textOutput {
		if _, err := fmt.Fprintf(out, "\nHistory updated: %s/%s %s -> %s\n", owner, repo, rel.TagName, strings.Join(installedPaths, ", ")); err != nil {
			return fmt.Errorf("writing history update message: %w", err)
		}
		if _, err := fmt.Fprintf(out, "Review the release notes: %s\n", githubReleasePageURL(owner, repo, rel)); err != nil {
			return fmt.Errorf("writing release notes message: %w", err)
		}
		return nil
	}
	return outputRootResult(out, result)
}

func normalizeOutputFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported output format: %s", format)
	}
}

func outputRootResult(w io.Writer, result rootCommandResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func resolveInstallNamesForSelection(repo, assetName, osName, arch string, binaries []string, installAs string) (map[string]string, error) {
	if strings.TrimSpace(installAs) == "" {
		return platform.ResolveInstallNames(repo, assetName, osName, arch, binaries), nil
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
