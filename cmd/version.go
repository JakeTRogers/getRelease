package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var version = "1.5.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), versionLine()); err != nil {
			return fmt.Errorf("writing version output: %w", err)
		}
		return nil
	},
}

// versionLine is the output of both `getRelease version` and `getRelease --version`.
func versionLine() string {
	return fmt.Sprintf("getRelease %s (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
}

func init() {
	// Setting Version makes Cobra add a --version flag; its -v shorthand is
	// taken by --verbose, so the flag has none.
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(versionLine() + "\n")
	rootCmd.AddCommand(versionCmd)
}
