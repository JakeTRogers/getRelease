package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestHistoryListEmpty(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

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
