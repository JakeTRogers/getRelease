package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestHistoryListEmpty(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	resetHistoryListFlags(t)

	if err := historyListCmd.Flags().Set("format", "text"); err != nil {
		t.Fatalf("set format: %v", err)
	}

	var out bytes.Buffer
	historyListCmd.SetOut(&out)

	if err := historyListCmd.RunE(historyListCmd, nil); err != nil {
		t.Fatalf("history list error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "No history records found." {
		t.Fatalf("history list output = %q, want empty message", out.String())
	}
}

func TestHistoryListJSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	resetHistoryListFlags(t)
	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool", "tool", installPath)})

	if err := historyListCmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}

	var out bytes.Buffer
	historyListCmd.SetOut(&out)

	if err := historyListCmd.RunE(historyListCmd, nil); err != nil {
		t.Fatalf("history list error: %v", err)
	}

	var got []history.Record
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "rec1" || got[0].Owner != "cli" {
		t.Fatalf("history list json = %+v, want saved record", got)
	}
}

func TestHistoryListSortText(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	resetHistoryListFlags(t)

	records := historyListSortRecordsFixture(t)
	writeHistoryRecords(t, records)

	tests := []struct {
		name   string
		sortBy string
		want   []string
	}{
		{name: "default binary sort", sortBy: "", want: []string{"rec3", "rec2", "rec1"}},
		{name: "owner sort", sortBy: historyListSortOwner, want: []string{"rec2", "rec3", "rec1"}},
		{name: "repo sort", sortBy: historyListSortRepo, want: []string{"rec3", "rec2", "rec1"}},
		{name: "installed sort", sortBy: historyListSortInstalled, want: []string{"rec2", "rec3", "rec1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetHistoryListFlags(t)

			if tt.sortBy != "" {
				if err := historyListCmd.Flags().Set("sort", tt.sortBy); err != nil {
					t.Fatalf("set sort: %v", err)
				}
			}

			var out bytes.Buffer
			historyListCmd.SetOut(&out)

			if err := historyListCmd.RunE(historyListCmd, nil); err != nil {
				t.Fatalf("history list error: %v", err)
			}

			got := historyListOutputIDs(t, out.String())
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("history list ids = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHistoryListSortJSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	resetHistoryListFlags(t)
	writeHistoryRecords(t, historyListSortRecordsFixture(t))

	if err := historyListCmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}
	if err := historyListCmd.Flags().Set("sort", historyListSortInstalled); err != nil {
		t.Fatalf("set sort: %v", err)
	}

	var out bytes.Buffer
	historyListCmd.SetOut(&out)

	if err := historyListCmd.RunE(historyListCmd, nil); err != nil {
		t.Fatalf("history list error: %v", err)
	}

	var got []history.Record
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	gotIDs := make([]string, 0, len(got))
	for _, rec := range got {
		gotIDs = append(gotIDs, rec.ID)
	}

	wantIDs := []string{"rec2", "rec3", "rec1"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("history list json ids = %v, want %v", gotIDs, wantIDs)
	}
}

func TestHistoryListSortInvalid(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	resetHistoryListFlags(t)
	writeHistoryRecords(t, historyListSortRecordsFixture(t))

	if err := historyListCmd.Flags().Set("sort", "invalid"); err != nil {
		t.Fatalf("set sort: %v", err)
	}

	var out bytes.Buffer
	historyListCmd.SetOut(&out)

	err := historyListCmd.RunE(historyListCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid sort value") {
		t.Fatalf("history list error = %v, want invalid sort value", err)
	}
}

func TestHistoryRemoveByID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool", "tool", installPath)})

	var out bytes.Buffer
	historyRemoveCmd.SetOut(&out)

	if err := historyRemoveCmd.RunE(historyRemoveCmd, []string{"rec1"}); err != nil {
		t.Fatalf("history remove error: %v", err)
	}
	if !strings.Contains(out.String(), "Removed history record rec1") {
		t.Fatalf("history remove output = %q, want removal confirmation", out.String())
	}
	if records := loadHistoryRecords(t); len(records) != 0 {
		t.Fatalf("history records after remove = %d, want 0", len(records))
	}
}

func TestHistoryRemoveAmbiguousBinary(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	firstPath := filepath.Join(t.TempDir(), "bin", "tool-a")
	secondPath := filepath.Join(t.TempDir(), "bin", "tool-b")
	writeExecutableFile(t, firstPath)
	writeExecutableFile(t, secondPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("rec1", "cli", "tool-a", "v1.0.0", "tool", "tool", firstPath),
		newHistoryRecord("rec2", "ops", "tool-b", "v1.0.0", "tool", "tool", secondPath),
	})

	var out bytes.Buffer
	historyRemoveCmd.SetOut(&out)

	err := historyRemoveCmd.RunE(historyRemoveCmd, []string{"tool"})
	if err == nil || !strings.Contains(err.Error(), "multiple matches") {
		t.Fatalf("history remove error = %v, want multiple matches", err)
	}
	if !strings.Contains(out.String(), "Multiple history records match 'tool':") {
		t.Fatalf("history remove output = %q, want ambiguity listing", out.String())
	}
}

func TestHistoryClearForce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	installPath := filepath.Join(t.TempDir(), "bin", "tool")
	writeExecutableFile(t, installPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("rec1", "cli", "tool", "v1.0.0", "tool", "tool", installPath)})

	if err := historyClearCmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set force: %v", err)
	}

	var out bytes.Buffer
	historyClearCmd.SetOut(&out)

	if err := historyClearCmd.RunE(historyClearCmd, nil); err != nil {
		t.Fatalf("history clear error: %v", err)
	}
	if !strings.Contains(out.String(), "Cleared 1 history records.") {
		t.Fatalf("history clear output = %q, want clear confirmation", out.String())
	}
	if records := loadHistoryRecords(t); len(records) != 0 {
		t.Fatalf("history records after clear = %d, want 0", len(records))
	}
}

func TestHistoryPruneDryRunAndApply(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	presentPath := filepath.Join(t.TempDir(), "bin", "present")
	writeExecutableFile(t, presentPath)
	missingPath := filepath.Join(t.TempDir(), "bin", "missing")
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("rec1", "cli", "present", "v1.0.0", "present", "present", presentPath),
		newHistoryRecord("rec2", "cli", "missing", "v1.0.0", "missing", "missing", missingPath),
	})

	if err := historyPruneCmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}

	var dryRunOut bytes.Buffer
	historyPruneCmd.SetOut(&dryRunOut)

	if err := historyPruneCmd.RunE(historyPruneCmd, nil); err != nil {
		t.Fatalf("history prune dry-run error: %v", err)
	}
	if !strings.Contains(dryRunOut.String(), "The following records would be removed:") || !strings.Contains(dryRunOut.String(), "rec2") {
		t.Fatalf("history prune dry-run output = %q, want removed record preview", dryRunOut.String())
	}

	if err := historyPruneCmd.Flags().Set("dry-run", "false"); err != nil {
		t.Fatalf("reset dry-run: %v", err)
	}

	var applyOut bytes.Buffer
	historyPruneCmd.SetOut(&applyOut)

	if err := historyPruneCmd.RunE(historyPruneCmd, nil); err != nil {
		t.Fatalf("history prune error: %v", err)
	}
	if !strings.Contains(applyOut.String(), "Removed 1 history record(s).") {
		t.Fatalf("history prune output = %q, want prune summary", applyOut.String())
	}
	records := loadHistoryRecords(t)
	if len(records) != 1 || records[0].ID != "rec1" {
		t.Fatalf("history records after prune = %+v, want only present record", records)
	}
}

func TestHistoryEditAndPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	t.Setenv("EDITOR", "true")

	if err := historyEditCmd.RunE(historyEditCmd, nil); err != nil {
		t.Fatalf("history edit error: %v", err)
	}
	path := historyPathForTest(t)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("history file missing after edit: %v", err)
	}

	var out bytes.Buffer
	historyPathCmd.SetOut(&out)

	if err := historyPathCmd.RunE(historyPathCmd, nil); err != nil {
		t.Fatalf("history path error: %v", err)
	}
	if strings.TrimSpace(out.String()) != path {
		t.Fatalf("history path output = %q, want %q", out.String(), path)
	}
}

func resetHistoryListFlags(t *testing.T) {
	t.Helper()

	for _, name := range []string{"format", "sort"} {
		flag := historyListCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("history list flag %q not found", name)
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset history list flag %q: %v", name, err)
		}
		flag.Changed = false
	}
}

func historyListSortRecordsFixture(t *testing.T) []history.Record {
	t.Helper()

	firstPath := filepath.Join(t.TempDir(), "bin", "gamma")
	secondPath := filepath.Join(t.TempDir(), "bin", "beta")
	thirdPath := filepath.Join(t.TempDir(), "bin", "alpha")

	writeExecutableFile(t, firstPath)
	writeExecutableFile(t, secondPath)
	writeExecutableFile(t, thirdPath)

	rec1 := newHistoryRecord("rec1", "charlie", "zulu", "v1.0.0", "gamma", "gamma", firstPath)
	rec1.InstalledAt = time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC)

	rec2 := newHistoryRecord("rec2", "alpha", "yankee", "v1.0.0", "beta", "beta", secondPath)
	rec2.InstalledAt = time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

	rec3 := newHistoryRecord("rec3", "bravo", "xray", "v1.0.0", "alpha", "alpha", thirdPath)
	rec3.InstalledAt = time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)

	return []history.Record{rec1, rec2, rec3}
}

func historyListOutputIDs(t *testing.T, output string) []string {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("history list output = %q, want header and rows", output)
	}

	ids := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		ids = append(ids, fields[0])
	}

	return ids
}
