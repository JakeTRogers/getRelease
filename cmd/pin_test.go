package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestRunPinSetsPinLevelAndPrintsAllowance(t *testing.T) {
	tests := []struct {
		name           string
		level          string
		wantLevel      history.PinLevel
		wantAllowance  string
		wantLevelLabel string
	}{
		{
			name:           "default patch",
			level:          "",
			wantLevel:      history.PinPatch,
			wantAllowance:  "locked to exact release",
			wantLevelLabel: "patch",
		},
		{
			name:           "minor",
			level:          "minor",
			wantLevel:      history.PinMinor,
			wantAllowance:  "allowing updates within v1.2.x",
			wantLevelLabel: "minor",
		},
		{
			name:           "major",
			level:          "major",
			wantLevel:      history.PinMajor,
			wantAllowance:  "allowing updates within v1.x",
			wantLevelLabel: "major",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTestCommandDeps(t, nil)
			t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

			installPath := filepath.Join(t.TempDir(), "bin", "tool")
			writeExecutableFile(t, installPath)
			writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool", "tool", installPath)})

			cmd := &cobra.Command{}
			addPinTestFlags(cmd)
			if tt.level != "" {
				if err := cmd.Flags().Set("level", tt.level); err != nil {
					t.Fatalf("set level: %v", err)
				}
			}

			var out bytes.Buffer
			cmd.SetOut(&out)

			if err := runPin(cmd, []string{"tool"}); err != nil {
				t.Fatalf("runPin() error: %v", err)
			}

			records := loadHistoryRecords(t)
			if len(records) != 1 || records[0].PinLevel != tt.wantLevel {
				t.Fatalf("history records = %+v, want pin level %q", records, tt.wantLevel)
			}
			if !strings.Contains(out.String(), "Pinned cli/tool at "+tt.wantLevelLabel) {
				t.Fatalf("runPin() output = %q, want pin level label", out.String())
			}
			if !strings.Contains(out.String(), tt.wantAllowance) {
				t.Fatalf("runPin() output = %q, want allowance %q", out.String(), tt.wantAllowance)
			}
		})
	}
}

func TestRunPinRejectsInvalidLevel(t *testing.T) {
	useTestCommandDeps(t, nil)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool", "tool", installPath)})

	cmd := &cobra.Command{}
	addPinTestFlags(cmd)
	if err := cmd.Flags().Set("level", "invalid"); err != nil {
		t.Fatalf("set level: %v", err)
	}

	err := runPin(cmd, []string{"tool"})
	if err == nil || !strings.Contains(err.Error(), "invalid pin level") {
		t.Fatalf("runPin() error = %v, want invalid pin level", err)
	}
}

func TestRunPinAllowsNonSemverCurrentTagForPatch(t *testing.T) {
	useTestCommandDeps(t, nil)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "latest", "tool", "tool", installPath)})

	cmd := &cobra.Command{}
	addPinTestFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runPin(cmd, []string{"tool"}); err != nil {
		t.Fatalf("runPin() error: %v", err)
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].PinLevel != history.PinPatch {
		t.Fatalf("history records = %+v, want patch-pinned record", records)
	}
	if !strings.Contains(out.String(), "Pinned cli/tool at patch") {
		t.Fatalf("runPin() output = %q, want patch pin confirmation", out.String())
	}
	if !strings.Contains(out.String(), "locked to exact release") {
		t.Fatalf("runPin() output = %q, want patch allowance", out.String())
	}
}

func TestValidateTargetArgsMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		owner, repo string
		wantErr     string
	}{
		{name: "missing target", wantErr: "specify one installed target as a binary name or owner/repo, or with --owner and --repo"},
		{name: "owner alone", owner: "cli", wantErr: "--owner and --repo must be used together"},
		{name: "both forms", args: []string{"tool"}, owner: "cli", repo: "tool", wantErr: "not both"},
		{name: "target", args: []string{"cli/tool"}},
		{name: "owner and repo", owner: "cli", repo: "tool"},
	}
	for _, tt := range tests {
		cmd := &cobra.Command{}
		addPinTestFlags(cmd)
		if tt.owner != "" {
			if err := cmd.Flags().Set("owner", tt.owner); err != nil {
				t.Fatal(err)
			}
		}
		if tt.repo != "" {
			if err := cmd.Flags().Set("repo", tt.repo); err != nil {
				t.Fatal(err)
			}
		}
		err := validateTargetArgs(cmd, tt.args)
		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("%s: validateTargetArgs() error = %v, want nil", tt.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: validateTargetArgs() error = %v, want %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestRunPinAndUnpinByOwnerAndRepo(t *testing.T) {
	useTestCommandDeps(t, nil)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool", "tool", installPath)})

	pin := &cobra.Command{}
	addPinTestFlags(pin)
	for flag, value := range map[string]string{"owner": "cli", "repo": "tool", "level": "minor"} {
		if err := pin.Flags().Set(flag, value); err != nil {
			t.Fatalf("set %s: %v", flag, err)
		}
	}
	pin.SetOut(&bytes.Buffer{})
	if err := runPin(pin, nil); err != nil {
		t.Fatalf("runPin() with --owner/--repo error: %v", err)
	}
	if records := loadHistoryRecords(t); records[0].PinLevel != history.PinMinor {
		t.Fatalf("pin level = %q, want minor", records[0].PinLevel)
	}

	unpin := &cobra.Command{}
	addUnpinTestFlags(unpin)
	for flag, value := range map[string]string{"owner": "cli", "repo": "tool"} {
		if err := unpin.Flags().Set(flag, value); err != nil {
			t.Fatalf("set %s: %v", flag, err)
		}
	}
	unpin.SetOut(&bytes.Buffer{})
	if err := runUnpin(unpin, nil); err != nil {
		t.Fatalf("runUnpin() with --owner/--repo error: %v", err)
	}
	if records := loadHistoryRecords(t); records[0].PinLevel != history.PinNone {
		t.Fatalf("pin level = %q, want none", records[0].PinLevel)
	}
}

func TestRunPinRejectsNonSemverCurrentTagForMinor(t *testing.T) {
	useTestCommandDeps(t, nil)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "latest", "tool", "tool", installPath)})

	cmd := &cobra.Command{}
	addPinTestFlags(cmd)
	if err := cmd.Flags().Set("level", "minor"); err != nil {
		t.Fatalf("set level: %v", err)
	}

	err := runPin(cmd, []string{"tool"})
	if err == nil || !strings.Contains(err.Error(), "is not valid semver and cannot be pinned") {
		t.Fatalf("runPin() error = %v, want non-semver error", err)
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].PinLevel != history.PinNone {
		t.Fatalf("history records = %+v, want unpinned record", records)
	}
}

func TestRunUnpinClearsPinLevel(t *testing.T) {
	useTestCommandDeps(t, nil)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	record := newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool", "tool", installPath)
	record.PinLevel = history.PinMajor
	writeHistoryRecords(t, []history.Record{record})

	cmd := &cobra.Command{}
	addUnpinTestFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runUnpin(cmd, []string{"tool"}); err != nil {
		t.Fatalf("runUnpin() error: %v", err)
	}

	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].PinLevel != history.PinNone {
		t.Fatalf("history records = %+v, want cleared pin", records)
	}
	if !strings.Contains(out.String(), "Removed pin from cli/tool; future upgrades will track the latest release") {
		t.Fatalf("runUnpin() output = %q, want removal confirmation", out.String())
	}
}

func TestRunUnpinAlreadyUnpinned(t *testing.T) {
	useTestCommandDeps(t, nil)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.2.3", "tool", "tool", installPath)})

	cmd := &cobra.Command{}
	addUnpinTestFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runUnpin(cmd, []string{"tool"}); err != nil {
		t.Fatalf("runUnpin() error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "cli/tool is not pinned" {
		t.Fatalf("runUnpin() output = %q, want already-unpinned notice", out.String())
	}
}
