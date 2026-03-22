package cmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/spf13/cobra"
)

func TestCompleteOwnersFromRecords(t *testing.T) {
	t.Parallel()

	records := []history.Record{
		{Owner: "cli", Repo: "alpha"},
		{Owner: "cli", Repo: "beta"},
		{Owner: "ops", Repo: "gamma"},
	}

	got := completeOwnersFromRecords(records, "cl")
	want := []string{"cli\t2 installs"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeOwnersFromRecords() = %v, want %v", got, want)
	}
}

func TestCompleteReposFromRecords(t *testing.T) {
	t.Parallel()

	records := []history.Record{
		{Owner: "cli", Repo: "alpha"},
		{Owner: "cli", Repo: "beta"},
		{Owner: "ops", Repo: "beta"},
	}

	got := completeReposFromRecords(records, "cli", "b")
	want := []string{"beta\tcli"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeReposFromRecords() = %v, want %v", got, want)
	}

	got = completeReposFromRecords(records, "", "b")
	want = []string{"beta\tcli, ops"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeReposFromRecords() without owner = %v, want %v", got, want)
	}
}

func TestCompleteUpgradeTargetsFromRecords(t *testing.T) {
	t.Parallel()

	records := []history.Record{
		{
			Owner: "cli",
			Repo:  "alpha",
			Binaries: []history.Binary{
				{Name: "alpha", InstalledAs: "alpha"},
				{Name: "helper", InstalledAs: "helper"},
			},
		},
		{
			Owner: "ops",
			Repo:  "alpha",
			Binaries: []history.Binary{
				{Name: "alpha", InstalledAs: "alpha"},
			},
		},
	}

	got := completeUpgradeTargetsFromRecords(records, "")
	want := []string{
		"alpha\t2 installs",
		"cli/alpha\talpha, helper",
		"ops/alpha\talpha",
	}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeUpgradeTargetsFromRecords() = %v, want %v", got, want)
	}

	got = completeUpgradeTargetsFromRecords(records, "ops/")
	want = []string{"ops/alpha\talpha"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeUpgradeTargetsFromRecords() owner/repo = %v, want %v", got, want)
	}
}

func TestCompleteHistoryListSortValues(t *testing.T) {
	t.Parallel()

	got, directive := completeHistoryListSortValues(&cobra.Command{}, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("completeHistoryListSortValues() directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}

	want := []string{
		"binary\tsort by installed binary name",
		"owner\tsort by repository owner",
		"repo\tsort by repository name",
		"installed\tsort by installed date, oldest first",
	}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeHistoryListSortValues() = %v, want %v", got, want)
	}

	got, directive = completeHistoryListSortValues(&cobra.Command{}, nil, "re")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("completeHistoryListSortValues() prefix directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}

	want = []string{"repo\tsort by repository name"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeHistoryListSortValues() prefix = %v, want %v", got, want)
	}
}

func TestRootHasCompletionCommand(t *testing.T) {
	t.Parallel()

	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" {
			return
		}
	}

	t.Fatal("root command is missing Cobra completion command")
}

func TestLoadHistoryRecordsForCompletionAndInstalledTargets(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	presentPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, presentPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool", "tool", presentPath),
		newHistoryRecord("rec2", "cli", "missing", "v1.0.0", "missing", "missing", filepath.Join(t.TempDir(), "bin", "missing")),
	})

	present := loadHistoryRecordsForCompletion(true)
	if len(present) != 1 || present[0].Repo != "tool" {
		t.Fatalf("loadHistoryRecordsForCompletion(true) = %+v, want only installed record", present)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("all", false, "")

	got, directive := completeInstalledUpgradeTargets(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("completeInstalledUpgradeTargets() directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	want := []string{"cli/tool\ttool"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("completeInstalledUpgradeTargets() = %v, want %v", got, want)
	}

	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("set all flag: %v", err)
	}
	got, _ = completeInstalledUpgradeTargets(cmd, nil, "")
	if len(got) != 0 {
		t.Fatalf("completeInstalledUpgradeTargets() with --all = %v, want no completions", got)
	}
}
