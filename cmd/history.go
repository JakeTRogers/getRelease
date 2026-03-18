package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/selector"
)

var historyCmd = &cobra.Command{
	Use:          "history",
	Short:        "Manage install history",
	SilenceUsage: true,
}

var historyListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List all history records",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolve history path: %w", err)
		}
		store := history.NewStore(path)
		if err := store.Load(); err != nil {
			return fmt.Errorf("loading history: %w", err)
		}

		records := store.Records()
		if len(records) == 0 {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No history records found."); err != nil {
				return fmt.Errorf("writing empty history message: %w", err)
			}
			return nil
		}

		format, _ := cmd.Flags().GetString("format")
		if format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(records)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "ID\tOWNER\tREPO\tTAG\tBINARIES\tINSTALLED"); err != nil {
			return fmt.Errorf("writing history header: %w", err)
		}
		for _, r := range records {
			var bins []string
			for _, b := range r.Binaries {
				bins = append(bins, b.InstalledAs)
			}
			installed := ""
			if !r.InstalledAt.IsZero() {
				installed = r.InstalledAt.Format("2006-01-02")
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				r.ID, r.Owner, r.Repo, r.Tag, strings.Join(bins, ","), installed); err != nil {
				return fmt.Errorf("writing history record %s: %w", r.ID, err)
			}
		}
		return w.Flush()
	},
}

var historyRemoveCmd = &cobra.Command{
	Use:          "remove <id|name>",
	Short:        "Remove record(s) by ID or binary name",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		path, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolve history path: %w", err)
		}
		store := history.NewStore(path)
		if err := store.Load(); err != nil {
			return fmt.Errorf("loading history: %w", err)
		}

		// Try remove by ID first (but capture the record for output)
		var found *history.Record
		for _, r := range store.Records() {
			if r.ID == key {
				rr := r
				found = &rr
				break
			}
		}
		if found != nil {
			if !store.Remove(found.ID) {
				return fmt.Errorf("failed to remove record %s", found.ID)
			}
			if err := store.Save(); err != nil {
				return fmt.Errorf("saving history: %w", err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed history record %s (%s/%s)\n", found.ID, found.Owner, found.Repo); err != nil {
				return fmt.Errorf("writing remove confirmation: %w", err)
			}
			return nil
		}

		// Try matching by binary name
		matches := store.FindByBinary(key)
		if len(matches) == 0 {
			return fmt.Errorf("no history record matching '%s'", key)
		}
		if len(matches) > 1 {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Multiple history records match '%s':\n", key); err != nil {
				return fmt.Errorf("writing duplicate match header: %w", err)
			}
			for _, r := range matches {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s/%s\n", r.ID, r.Owner, r.Repo); err != nil {
					return fmt.Errorf("writing duplicate match row %s: %w", r.ID, err)
				}
			}
			return fmt.Errorf("multiple matches, use ID to disambiguate")
		}

		// Single match
		rec := matches[0]
		if !store.Remove(rec.ID) {
			return fmt.Errorf("failed to remove record %s", rec.ID)
		}
		if err := store.Save(); err != nil {
			return fmt.Errorf("saving history: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed history record %s (%s/%s)\n", rec.ID, rec.Owner, rec.Repo); err != nil {
			return fmt.Errorf("writing remove confirmation: %w", err)
		}
		return nil
	},
}

var historyClearCmd = &cobra.Command{
	Use:          "clear",
	Short:        "Clear all history records",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		force, _ := cmd.Flags().GetBool("force")
		path, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolve history path: %w", err)
		}
		store := history.NewStore(path)
		if err := store.Load(); err != nil {
			return fmt.Errorf("loading history: %w", err)
		}
		n := len(store.Records())
		if n == 0 {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No history records found."); err != nil {
				return fmt.Errorf("writing empty history message: %w", err)
			}
			return nil
		}

		if !force {
			ok, err := selector.Confirm(fmt.Sprintf("Clear all %d history records?", n), false)
			if err != nil {
				return err
			}
			if !ok {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted."); err != nil {
					return fmt.Errorf("writing clear abort message: %w", err)
				}
				return nil
			}
		}

		// Create an empty store and save it (truncates history)
		newStore := history.NewStore(path)
		if err := newStore.Save(); err != nil {
			return fmt.Errorf("clearing history: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Cleared %d history records.\n", n); err != nil {
			return fmt.Errorf("writing clear confirmation: %w", err)
		}
		return nil
	},
}

var historyPruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove records for binaries no longer on disk",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		path, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolve history path: %w", err)
		}
		store := history.NewStore(path)
		if err := store.Load(); err != nil {
			return fmt.Errorf("loading history: %w", err)
		}

		if dryRun {
			var wouldRemove []history.Record
			for _, rec := range store.Records() {
				keep := false
				for _, b := range rec.Binaries {
					if b.InstallPath == "" {
						continue
					}
					if _, err := os.Stat(b.InstallPath); err == nil {
						keep = true
						break
					}
				}
				if !keep {
					wouldRemove = append(wouldRemove, rec)
				}
			}
			if len(wouldRemove) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No history records would be pruned."); err != nil {
					return fmt.Errorf("writing prune dry-run empty message: %w", err)
				}
				return nil
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "The following records would be removed:"); err != nil {
				return fmt.Errorf("writing prune dry-run header: %w", err)
			}
			for _, r := range wouldRemove {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s/%s\n", r.ID, r.Owner, r.Repo); err != nil {
					return fmt.Errorf("writing prune dry-run row %s: %w", r.ID, err)
				}
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\n%d record(s) would be removed.\n", len(wouldRemove)); err != nil {
				return fmt.Errorf("writing prune dry-run summary: %w", err)
			}
			return nil
		}

		removed, err := store.Prune()
		if err != nil {
			return fmt.Errorf("pruning history: %w", err)
		}
		if len(removed) == 0 {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No history records to prune."); err != nil {
				return fmt.Errorf("writing prune empty message: %w", err)
			}
			return nil
		}
		if err := store.Save(); err != nil {
			return fmt.Errorf("saving history: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed %d history record(s).\n", len(removed)); err != nil {
			return fmt.Errorf("writing prune summary: %w", err)
		}
		for _, r := range removed {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s/%s\n", r.ID, r.Owner, r.Repo); err != nil {
				return fmt.Errorf("writing prune row %s: %w", r.ID, err)
			}
		}
		return nil
	},
}

var historyEditCmd = &cobra.Command{
	Use:          "edit",
	Short:        "Open history file in $EDITOR",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		path, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolve history path: %w", err)
		}
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create history dir: %w", err)
		}
		// Ensure the file exists
		f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("ensure history file: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("closing history file: %w", err)
		}

		e := exec.Command(editor, path)
		e.Stdin = os.Stdin
		e.Stdout = os.Stdout
		e.Stderr = os.Stderr
		if err := e.Run(); err != nil {
			return fmt.Errorf("running editor: %w", err)
		}
		return nil
	},
}

var historyPathCmd = &cobra.Command{
	Use:          "path",
	Short:        "Print history file path",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolve history path: %w", err)
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), path); err != nil {
			return fmt.Errorf("writing history path: %w", err)
		}
		return nil
	},
}

func init() {
	historyCmd.AddCommand(historyListCmd, historyRemoveCmd, historyClearCmd, historyPruneCmd, historyEditCmd, historyPathCmd)
	historyListCmd.Flags().String("format", "text", "output format: text, json")
	historyClearCmd.Flags().Bool("force", false, "skip confirmation prompt")
	historyPruneCmd.Flags().Bool("dry-run", false, "show what would be pruned without removing")
	rootCmd.AddCommand(historyCmd)
}
