package cmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	versionCmd.SetOut(&out)

	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("versionCmd.RunE() error: %v", err)
	}

	want := fmt.Sprintf("getRelease %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	if out.String() != want {
		t.Fatalf("versionCmd output = %q, want %q", out.String(), want)
	}
}

// executeRoot runs the real root command with args and returns its output.
// Flag values, arguments, writers, and the default logger set by the run are
// restored afterwards, since rootCmd is shared by the whole package.
func executeRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	prevLogger := slog.Default()
	var out bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&out)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Changed {
				if err := f.Value.Set(f.DefValue); err != nil {
					t.Errorf("reset --%s: %v", f.Name, err)
				}
				f.Changed = false
			}
		})
		slog.SetDefault(prevLogger)
	})
	err := rootCmd.Execute()
	return out.String(), err
}

func TestRootVersionFlag(t *testing.T) {
	// A malformed config file proves --version answers without loading config.
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	if err := os.MkdirAll(filepath.Join(cfgDir, "getRelease"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "getRelease", "config.yaml"), []byte("cooldown: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := executeRoot(t, "--version")
	if err != nil {
		t.Fatalf("getRelease --version error: %v", err)
	}
	if want := versionLine() + "\n"; out != want {
		t.Fatalf("getRelease --version output = %q, want %q", out, want)
	}
}

func TestRootWithoutArgumentsShowsHelp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	out, err := executeRoot(t)
	if err != nil {
		t.Fatalf("getRelease error: %v", err)
	}
	for _, want := range []string{"Usage:", "Examples:", "getRelease sharkdp/bat", "getRelease --owner sharkdp --repo bat"} {
		if !strings.Contains(out, want) {
			t.Errorf("getRelease output = %q, want it to contain %q", out, want)
		}
	}

}

func TestRootWithPartialFlagsStillErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if _, err := executeRoot(t, "--tag", "v1.0.0"); err == nil || !strings.Contains(err.Error(), "specify a repository") {
		t.Fatalf("getRelease --tag error = %v, want repository error", err)
	}
}

func TestRootRejectsPositionalArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "positional only", args: []string{"typo"}},
		{name: "repository flags", args: []string{"--owner", "owner", "--repo", "repo", "typo"}},
		{name: "URL flag", args: []string{"--url", "https://github.com/owner/repo", "typo"}},
		{name: "after double dash", args: []string{"--", "typo"}},
		{name: "repository flags and double dash", args: []string{"--owner", "owner", "--repo", "repo", "--", "typo"}},
		{name: "URL flag and double dash", args: []string{"--url", "https://github.com/owner/repo", "--", "typo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			useTestCommandDeps(t, nil)
			newGitHubClient = func(string) (releaseClient, error) {
				t.Fatal("positional arguments must be rejected before creating a GitHub client")
				return nil, nil
			}

			out, err := executeRoot(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), `unknown command "typo" for "getRelease"`) {
				t.Fatalf("getRelease %v error = %v, want positional argument error", tt.args, err)
			}
			if strings.Contains(out, "Usage:") {
				t.Errorf("getRelease %v printed help instead of rejecting positional arguments", tt.args)
			}
		})
	}
}

func TestRootUnknownCommandKeepsSuggestions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := executeRoot(t, "versio")
	if err == nil || !strings.Contains(err.Error(), "Did you mean this?") || !strings.Contains(err.Error(), "\tversion\n") {
		t.Fatalf("getRelease versio error = %v, want version suggestion", err)
	}
}

func TestRootRejectsMultipleRepositoryArguments(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	useTestCommandDeps(t, nil)
	newGitHubClient = func(string) (releaseClient, error) {
		t.Fatal("extra arguments must be rejected before creating a GitHub client")
		return nil, nil
	}

	_, err := executeRoot(t, "cli/tool", "cli/other")
	if err == nil || !strings.Contains(err.Error(), "accepts at most one repository argument, received 2") {
		t.Fatalf("getRelease cli/tool cli/other error = %v, want argument count error", err)
	}
}

func TestRootRepositoryArgumentConflicts(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "owner flag", args: []string{"cli/tool", "--owner", "cli"}, wantErr: "not both"},
		{name: "url flag", args: []string{"cli/tool", "--url", "https://github.com/cli/tool"}, wantErr: "not both"},
		{name: "tag flag", args: []string{"cli/tool@v1.0.0", "--tag", "v2.0.0"}, wantErr: "--tag, not both"},
		{name: "host flag with host argument", args: []string{"acme.ghe.com/cli/tool", "--host", "acme.ghe.com"}, wantErr: "--host cannot be used"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			useTestCommandDeps(t, nil)
			newGitHubClient = func(string) (releaseClient, error) {
				t.Fatal("conflicting arguments must be rejected before creating a GitHub client")
				return nil, nil
			}

			if _, err := executeRoot(t, tt.args...); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("getRelease %v error = %v, want substring %q", tt.args, err, tt.wantErr)
			}
		})
	}
}
