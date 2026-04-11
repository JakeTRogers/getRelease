package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/semver"
)

var pinCmd = &cobra.Command{
	Use:   "pin <target>",
	Short: "Pin an installed target to its current patch, minor, or major line",
	Long: `Pin a previously installed target so future upgrades stay within the selected semver ceiling.
The default level, patch, locks the target to its current exact release.`,
	Args: cobra.ExactArgs(1),
	RunE: runPin,
}

var unpinCmd = &cobra.Command{
	Use:   "unpin <target>",
	Short: "Remove version pinning from an installed target",
	Args:  cobra.ExactArgs(1),
	RunE:  runUnpin,
}

func init() {
	pinCmd.Flags().String("level", string(history.PinPatch), "pin level: patch, minor, major")
	pinCmd.Flags().StringP("owner", "o", "", "GitHub owner/org (skip history lookup)")
	pinCmd.Flags().StringP("repo", "r", "", "GitHub repository (skip history lookup)")
	pinCmd.ValidArgsFunction = completeInstalledUpgradeTargets
	registerOwnerRepoHistoryCompletions(pinCmd, true)
	mustRegisterFlagCompletion(pinCmd, "level", completePinLevels)

	unpinCmd.Flags().StringP("owner", "o", "", "GitHub owner/org (skip history lookup)")
	unpinCmd.Flags().StringP("repo", "r", "", "GitHub repository (skip history lookup)")
	unpinCmd.ValidArgsFunction = completeInstalledUpgradeTargets
	registerOwnerRepoHistoryCompletions(unpinCmd, true)

	rootCmd.AddCommand(pinCmd, unpinCmd)
}

func runPin(cmd *cobra.Command, args []string) error {
	levelValue, _ := cmd.Flags().GetString("level")
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")

	histPath, err := config.HistoryFilePath()
	if err != nil {
		return fmt.Errorf("resolving history path: %w", err)
	}

	store := history.NewStore(histPath)
	if err := store.Load(); err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	rec, err := resolveUpgradeRecord(store, args[0], ownerFlag, repoFlag)
	if err != nil {
		return err
	}

	level, err := history.ParsePinLevel(levelValue)
	if err != nil {
		return err
	}

	var currentVersion semver.Version
	// Only require semver parsing for minor/major pins; patch pins can use any tag
	if level != history.PinPatch {
		v, err := semver.Parse(rec.Tag)
		if err != nil {
			return fmt.Errorf("current tag %q is not valid semver and cannot be pinned to %s: %w", rec.Tag, level, err)
		}
		currentVersion = v
	}

	rec.PinLevel = level
	if err := store.Add(*rec); err != nil {
		return fmt.Errorf("updating history: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("saving history: %w", err)
	}

	descr := pinAllowanceDescription(level, currentVersion)
	if _, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"Pinned %s/%s at %s (current %s; %s)\n",
		rec.Owner,
		rec.Repo,
		level,
		rec.Tag,
		descr,
	); err != nil {
		return fmt.Errorf("writing pin confirmation: %w", err)
	}

	return nil
}

func runUnpin(cmd *cobra.Command, args []string) error {
	ownerFlag, _ := cmd.Flags().GetString("owner")
	repoFlag, _ := cmd.Flags().GetString("repo")

	histPath, err := config.HistoryFilePath()
	if err != nil {
		return fmt.Errorf("resolving history path: %w", err)
	}

	store := history.NewStore(histPath)
	if err := store.Load(); err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	rec, err := resolveUpgradeRecord(store, args[0], ownerFlag, repoFlag)
	if err != nil {
		return err
	}

	if rec.PinLevel == history.PinNone {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s/%s is not pinned\n", rec.Owner, rec.Repo); err != nil {
			return fmt.Errorf("writing unpin status: %w", err)
		}
		return nil
	}

	rec.PinLevel = history.PinNone
	if err := store.Add(*rec); err != nil {
		return fmt.Errorf("updating history: %w", err)
	}
	if err := store.Save(); err != nil {
		return fmt.Errorf("saving history: %w", err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed pin from %s/%s; future upgrades will track the latest release\n", rec.Owner, rec.Repo); err != nil {
		return fmt.Errorf("writing unpin confirmation: %w", err)
	}

	return nil
}

func pinAllowanceDescription(level history.PinLevel, version semver.Version) string {
	switch level {
	case history.PinPatch:
		return "locked to exact release"
	case history.PinMinor:
		return fmt.Sprintf("allowing updates within v%d.%d.x", version.Major, version.Minor)
	case history.PinMajor:
		return fmt.Sprintf("allowing updates within v%d.x", version.Major)
	default:
		return "tracking latest release"
	}
}

func pinPolicySummary(level history.PinLevel, tag string, version semver.Version) string {
	switch level {
	case history.PinPatch:
		return fmt.Sprintf("Pin policy: patch (locked to exact release %s)", tag)
	case history.PinMinor:
		return fmt.Sprintf("Pin policy: minor (allows v%d.%d.x)", version.Major, version.Minor)
	case history.PinMajor:
		return fmt.Sprintf("Pin policy: major (allows v%d.x)", version.Major)
	default:
		return ""
	}
}
