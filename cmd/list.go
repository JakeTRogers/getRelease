package cmd

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List releases or assets for a GitHub repository",
	Long: `List the most recent releases for a repository, or list the
downloadable assets for a specific release tag.

By default, the last 30 releases are shown. Use --tag to list assets
for a specific release instead.`,
	SilenceUsage: true,
	RunE:         runList,
}

func init() {
	listCmd.Flags().StringP("owner", "o", "", "GitHub owner/org name")
	listCmd.Flags().StringP("repo", "r", "", "GitHub repository name")
	listCmd.Flags().StringP("url", "u", "", "GitHub repository URL")
	listCmd.Flags().StringP("tag", "t", "", "list assets for this release tag instead of listing releases")
	listCmd.Flags().IntP("limit", "l", 30, "number of releases to show")
	listCmd.Flags().String("format", "text", "output format: text, json")

	listCmd.MarkFlagsMutuallyExclusive("url", "owner")
	listCmd.MarkFlagsMutuallyExclusive("url", "repo")
	registerOwnerRepoHistoryCompletions(listCmd, false)

	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, _ []string) error {
	owner, repo, err := resolveRepo(cmd)
	if err != nil {
		return err
	}

	tag, _ := cmd.Flags().GetString("tag")
	format, _ := cmd.Flags().GetString("format")

	client := newGitHubClient()

	if tag != "" {
		return listAssets(cmd, client, owner, repo, tag, format)
	}
	return listReleases(cmd, client, owner, repo, format)
}

func listReleases(cmd *cobra.Command, client releaseClient, owner, repo, format string) error {
	limit, _ := cmd.Flags().GetInt("limit")

	releases, err := client.ListReleases(owner, repo, limit)
	if err != nil {
		return fmt.Errorf("listing releases: %w", err)
	}

	if len(releases) == 0 {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "No releases found for %s/%s\n", owner, repo); err != nil {
			return fmt.Errorf("writing empty releases message: %w", err)
		}
		return nil
	}

	if format == "json" {
		return outputJSON(cmd, releases)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Releases for %s/%s:\n\n", owner, repo); err != nil {
		return fmt.Errorf("writing releases heading: %w", err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "TAG\tNAME\tDATE\tASSETS"); err != nil {
		return fmt.Errorf("writing releases table header: %w", err)
	}
	for _, r := range releases {
		name := r.DisplayName()
		if name == r.TagName {
			name = ""
		}
		date := r.PublishedAt.Format("2006-01-02")
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", r.TagName, name, date, len(r.Assets)); err != nil {
			return fmt.Errorf("writing release row %s: %w", r.TagName, err)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\nBrowse releases: %s\n", githubRepoReleasesURL(owner, repo)); err != nil {
		return fmt.Errorf("writing releases footer: %w", err)
	}
	return nil
}

func listAssets(cmd *cobra.Command, client releaseClient, owner, repo, tag, format string) error {
	release, err := client.GetReleaseByTag(owner, repo, tag)
	if err != nil {
		return fmt.Errorf("fetching release %s: %w", tag, err)
	}

	if len(release.Assets) == 0 {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Release %s has no downloadable assets\n", tag); err != nil {
			return fmt.Errorf("writing empty assets message: %w", err)
		}
		return nil
	}

	if format == "json" {
		return outputJSON(cmd, release.Assets)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Assets for %s/%s %s:\n\n", owner, repo, release.DisplayName()); err != nil {
		return fmt.Errorf("writing assets heading: %w", err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tSIZE"); err != nil {
		return fmt.Errorf("writing assets table header: %w", err)
	}
	for _, a := range release.Assets {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", a.Name, formatBytes(a.Size)); err != nil {
			return fmt.Errorf("writing asset row %s: %w", a.Name, err)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\nBrowse this release: %s\n", githubReleasePageURL(owner, repo, release)); err != nil {
		return fmt.Errorf("writing assets footer: %w", err)
	}
	return nil
}

func outputJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
