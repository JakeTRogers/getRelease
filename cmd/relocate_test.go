package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/install"
)

// setupRelocate isolates History and config in temp directories and returns
// the old install directory (where recorded binaries live) and a new one that
// does not exist yet.
func setupRelocate(t *testing.T) (oldDir, newDir string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)

	root := t.TempDir()
	oldDir = filepath.Join(root, "old")
	newDir = filepath.Join(root, "new")
	setTestConfig(t.TempDir(), oldDir)
	return oldDir, newDir
}

func runRelocate(t *testing.T, dir string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	relocateCmd.SetOut(&out)
	err := relocateCmd.RunE(relocateCmd, []string{dir})
	return out.String(), err
}

func configFileContents(t *testing.T) string {
	t.Helper()
	path, err := config.ConfigFilePath()
	if err != nil {
		t.Fatalf("config file path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read config file: %v", err)
	}
	return string(data)
}

func assertExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if got := err == nil; got != want {
		t.Fatalf("%s exists = %v, want %v", path, got, want)
	}
}

func TestRelocateMovesRecordedBinaries(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	newPath := filepath.Join(newDir, "tool")
	assertExists(t, newPath, true)
	assertExists(t, oldPath, false)
	records := loadHistoryRecords(t)
	if got := records[0].Binaries[0].InstallPath; got != newPath {
		t.Fatalf("recorded install path = %q, want %q", got, newPath)
	}
	if !strings.Contains(configFileContents(t), "installDir: "+newDir) {
		t.Fatalf("config file does not set installDir to %s:\n%s", newDir, configFileContents(t))
	}
	if !strings.Contains(out, oldPath) || !strings.Contains(out, newPath) {
		t.Fatalf("output does not mention the move %s -> %s:\n%s", oldPath, newPath, out)
	}
}

func TestRelocateSkipsMissingBinaries(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	presentPath := filepath.Join(oldDir, "present")
	missingPath := filepath.Join(oldDir, "missing")
	writeExecutableFile(t, presentPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("a1", "acme", "present", "v1.0.0", "present", "present", presentPath),
		newHistoryRecord("b2", "acme", "missing", "v1.0.0", "missing", "missing", missingPath),
	})

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	records := loadHistoryRecords(t)
	if got, want := records[0].Binaries[0].InstallPath, filepath.Join(newDir, "present"); got != want {
		t.Fatalf("present binary path = %q, want %q", got, want)
	}
	if got := records[1].Binaries[0].InstallPath; got != missingPath {
		t.Fatalf("missing binary path = %q, want it untouched at %q", got, missingPath)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("output does not report the missing binary:\n%s", out)
	}
}

func TestRelocateTreatsUnreadableBinaryAsFailureNotMissing(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	// A name longer than any filesystem allows makes Lstat fail with an error
	// other than not-exist, even when the tests run as root.
	unreadable := filepath.Join(oldDir, strings.Repeat("x", 300))
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", unreadable)})
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for the unreadable binary\noutput: %s", out)
	}

	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if entry := result.Binaries[0]; entry.Status != relocateInstallFailed || entry.Error == "" {
		t.Fatalf("entry = %+v, want status %s with the lstat error", entry, relocateInstallFailed)
	}
	if result.InstallDirUpdated || strings.Contains(configFileContents(t), "installDir") {
		t.Fatalf("installDir was written despite an unreadable binary:\n%s", configFileContents(t))
	}
}

func TestRelocateRejectsInstallNameOutsideTheNewDirectory(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "../escaped", oldPath)})
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for the invalid name\noutput: %s", out)
	}

	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if entry := result.Binaries[0]; entry.Status != relocateInvalidName || entry.To != "" {
		t.Fatalf("entry = %+v, want status %s with no destination", entry, relocateInvalidName)
	}
	assertExists(t, filepath.Join(filepath.Dir(newDir), "escaped"), false)
	assertExists(t, oldPath, true)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != oldPath {
		t.Fatalf("recorded path = %q, want it untouched at %q", got, oldPath)
	}
	if result.InstallDirUpdated || strings.Contains(configFileContents(t), "installDir") {
		t.Fatalf("installDir was written despite an invalid name:\n%s", configFileContents(t))
	}
}

func TestRelocateReportsConflictWithoutOverwriting(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	conflictPath := filepath.Join(newDir, "tool")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("something else"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for the conflict\noutput: %s", out)
	}

	if data, _ := os.ReadFile(conflictPath); string(data) != "something else" {
		t.Fatalf("conflicting file was overwritten: %q", data)
	}
	assertExists(t, oldPath, true)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != oldPath {
		t.Fatalf("recorded path = %q, want it untouched at %q", got, oldPath)
	}
	if strings.Contains(configFileContents(t), "installDir") {
		t.Fatalf("installDir was written despite a skipped binary:\n%s", configFileContents(t))
	}
	if !strings.Contains(out, "conflict") {
		t.Fatalf("output does not report the conflict:\n%s", out)
	}
}

func TestRelocateLeavesBinariesAlreadyInPlace(t *testing.T) {
	_, newDir := setupRelocate(t)
	inPlace := filepath.Join(newDir, "tool")
	writeExecutableFile(t, inPlace)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", inPlace)})

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	assertExists(t, inPlace, true)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != inPlace {
		t.Fatalf("recorded path = %q, want %q", got, inPlace)
	}
	if !strings.Contains(configFileContents(t), "installDir: "+newDir) {
		t.Fatalf("config file does not set installDir to %s:\n%s", newDir, configFileContents(t))
	}
}

type installerFunc func(source, target string) error

func (f installerFunc) Install(source, target string) error { return f(source, target) }

func TestRelocateInstallFailureKeepsRecordAndContinues(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	badPath := filepath.Join(oldDir, "bad")
	goodPath := filepath.Join(oldDir, "good")
	writeExecutableFile(t, badPath)
	writeExecutableFile(t, goodPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("a1", "acme", "bad", "v1.0.0", "bad", "bad", badPath),
		newHistoryRecord("b2", "acme", "good", "v1.0.0", "good", "good", goodPath),
	})
	real := newBinaryInstaller("")
	newBinaryInstaller = func(string) install.Installer {
		return installerFunc(func(source, target string) error {
			if filepath.Base(target) == "bad" {
				return errors.New("permission denied")
			}
			return real.Install(source, target)
		})
	}

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for the failed install\noutput: %s", out)
	}

	assertExists(t, badPath, true)
	assertExists(t, filepath.Join(newDir, "good"), true)
	assertExists(t, goodPath, false)
	records := loadHistoryRecords(t)
	if got := records[0].Binaries[0].InstallPath; got != badPath {
		t.Fatalf("failed binary path = %q, want it untouched at %q", got, badPath)
	}
	if got, want := records[1].Binaries[0].InstallPath, filepath.Join(newDir, "good"); got != want {
		t.Fatalf("other binary path = %q, want %q", got, want)
	}
	if strings.Contains(configFileContents(t), "installDir") {
		t.Fatalf("installDir was written despite a failed install:\n%s", configFileContents(t))
	}
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("output does not report the install failure:\n%s", out)
	}
}

func TestRelocateRejectsLinkToTheOldCopy(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	newBinaryInstaller = func(string) install.Installer { return installerFunc(os.Symlink) }

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for a link to the old copy\noutput: %s", out)
	}

	assertExists(t, oldPath, true)
	assertExists(t, filepath.Join(newDir, "tool"), false)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != oldPath {
		t.Fatalf("recorded path = %q, want it untouched at %q", got, oldPath)
	}
	if strings.Contains(configFileContents(t), "installDir") {
		t.Fatalf("installDir was written despite the failed move:\n%s", configFileContents(t))
	}
	if !strings.Contains(out, "instead of copying it") {
		t.Fatalf("output does not explain the rejected link:\n%s", out)
	}
}

func TestRelocateRelinksALinkedBinaryToWhatItPointsAt(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	download := filepath.Join(t.TempDir(), "download", "tool")
	writeExecutableFile(t, download)
	oldPath := filepath.Join(oldDir, "tool")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(download, oldPath); err != nil {
		t.Fatal(err)
	}
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	newBinaryInstaller = func(string) install.Installer { return installerFunc(os.Symlink) }

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	newPath := filepath.Join(newDir, "tool")
	assertExists(t, oldPath, false)
	want, err := filepath.EvalSymlinks(download)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(newPath); err != nil || got != want {
		t.Fatalf("new link points at %q (err %v), want %q", got, err, want)
	}
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != newPath {
		t.Fatalf("recorded path = %q, want %q", got, newPath)
	}
}

func TestRelocateRemovalFailureKeepsOldCopyAndHintsSudo(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	removeFile = func(string) error { return fs.ErrPermission }

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for the failed removal\noutput: %s", out)
	}

	newPath := filepath.Join(newDir, "tool")
	assertExists(t, oldPath, true)
	assertExists(t, newPath, true)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != newPath {
		t.Fatalf("recorded path = %q, want the new path %q", got, newPath)
	}
	if !strings.Contains(configFileContents(t), "installDir: "+newDir) {
		t.Fatalf("a removal failure must not block writing installDir:\n%s", configFileContents(t))
	}
	if !strings.Contains(out, "sudo rm "+oldPath) {
		t.Fatalf("output does not hint how to remove the old copy:\n%s", out)
	}
}

func TestRelocateDryRunChangesNothing(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	setCommandFlag(t, relocateCmd, "dry-run", "true")

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	assertExists(t, newDir, false)
	assertExists(t, oldPath, true)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != oldPath {
		t.Fatalf("recorded path = %q, want it untouched at %q", got, oldPath)
	}
	if configFileContents(t) != "" {
		t.Fatalf("dry run wrote config:\n%s", configFileContents(t))
	}
	if !strings.Contains(out, oldPath) || !strings.Contains(out, filepath.Join(newDir, "tool")) {
		t.Fatalf("dry run output does not list the planned move:\n%s", out)
	}
}

func TestRelocateJSONOutput(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath),
		newHistoryRecord("b2", "acme", "gone", "v1.0.0", "gone", "gone", filepath.Join(oldDir, "gone")),
	})
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if result.DryRun || !result.InstallDirUpdated || result.InstallDir != newDir {
		t.Fatalf("result = %+v, want dryRun=false installDirUpdated=true installDir=%s", result, newDir)
	}
	want := []relocateEntry{
		{Package: "acme/tool", Binary: "tool", From: oldPath, To: filepath.Join(newDir, "tool"), Status: relocateMoved},
		{Package: "acme/gone", Binary: "gone", From: filepath.Join(oldDir, "gone"), To: filepath.Join(newDir, "gone"), Status: relocateMissing},
	}
	if !reflect.DeepEqual(result.Binaries, want) {
		t.Fatalf("binaries = %+v, want %+v", result.Binaries, want)
	}
}

func TestRelocateRerunFinishesAfterPartialFailure(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	badPath := filepath.Join(oldDir, "bad")
	goodPath := filepath.Join(oldDir, "good")
	writeExecutableFile(t, badPath)
	writeExecutableFile(t, goodPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("a1", "acme", "bad", "v1.0.0", "bad", "bad", badPath),
		newHistoryRecord("b2", "acme", "good", "v1.0.0", "good", "good", goodPath),
	})
	real := newBinaryInstaller("")
	newBinaryInstaller = func(string) install.Installer {
		return installerFunc(func(source, target string) error {
			if filepath.Base(target) == "bad" {
				return errors.New("permission denied")
			}
			return real.Install(source, target)
		})
	}
	if _, err := runRelocate(t, newDir); err == nil {
		t.Fatal("first relocate succeeded, want an error")
	}
	newBinaryInstaller = func(string) install.Installer { return real }

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("second relocate error: %v\noutput: %s", err, out)
	}

	for _, rec := range loadHistoryRecords(t) {
		if dir := filepath.Dir(rec.Binaries[0].InstallPath); dir != newDir {
			t.Fatalf("%s is recorded in %s, want %s", rec.Repo, dir, newDir)
		}
	}
	assertExists(t, badPath, false)
	assertExists(t, goodPath, false)
	if !strings.Contains(configFileContents(t), "installDir: "+newDir) {
		t.Fatalf("config file does not set installDir to %s:\n%s", newDir, configFileContents(t))
	}
}

func TestRelocateRejectsFileAsDirectory(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	writeExecutableFile(t, newDir)

	if out, err := runRelocate(t, newDir); err == nil {
		t.Fatalf("relocate succeeded, want an error for a file target\noutput: %s", out)
	}

	assertExists(t, oldPath, true)
	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != oldPath {
		t.Fatalf("recorded path = %q, want it untouched at %q", got, oldPath)
	}
}

func TestRelocateDryRunReportsBinariesSharingATarget(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	firstPath := filepath.Join(oldDir, "a", "tool")
	secondPath := filepath.Join(oldDir, "b", "tool")
	writeExecutableFile(t, firstPath)
	writeExecutableFile(t, secondPath)
	writeHistoryRecords(t, []history.Record{
		newHistoryRecord("a1", "acme", "first", "v1.0.0", "tool", "tool", firstPath),
		newHistoryRecord("b2", "acme", "second", "v1.0.0", "tool", "tool", secondPath),
	})
	setCommandFlag(t, relocateCmd, "dry-run", "true")
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("dry run succeeded, want an error for the shared target\noutput: %s", out)
	}

	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got := []relocateStatus{result.Binaries[0].Status, result.Binaries[1].Status}; !reflect.DeepEqual(got, []relocateStatus{relocateWouldMove, relocateConflict}) {
		t.Fatalf("statuses = %v, want the second binary reported as a conflict", got)
	}
}

// sharedInstallRecords returns two Packages, the same repo on two hosts, that
// installed the same name and so share one file.
func sharedInstallRecords(path string) []history.Record {
	public := newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", path)
	enterprise := newHistoryRecord("b2", "acme", "tool", "v1.0.0", "tool", "tool", path)
	enterprise.Host = "ghe.example.com"
	return []history.Record{public, enterprise}
}

func TestRelocateRepointsEveryRecordSharingAnInstallPath(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, sharedInstallRecords(oldPath))
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	newPath := filepath.Join(newDir, "tool")
	assertExists(t, newPath, true)
	assertExists(t, oldPath, false)
	for _, rec := range loadHistoryRecords(t) {
		if got := rec.Binaries[0].InstallPath; got != newPath {
			t.Fatalf("%s record path = %q, want %q", rec.EffectiveHost(), got, newPath)
		}
	}
	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	for _, entry := range result.Binaries {
		if entry.Status != relocateMoved || entry.To != newPath {
			t.Fatalf("entry = %+v, want status %s to %s", entry, relocateMoved, newPath)
		}
	}
	if !result.InstallDirUpdated {
		t.Fatal("installDir was not updated")
	}
}

func TestRelocateDryRunPlansSharedInstallPathOnce(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, sharedInstallRecords(oldPath))
	setCommandFlag(t, relocateCmd, "dry-run", "true")
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("dry run error: %v\noutput: %s", err, out)
	}

	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	for _, entry := range result.Binaries {
		if entry.Status != relocateWouldMove {
			t.Fatalf("entry = %+v, want status %s, not a conflict with itself", entry, relocateWouldMove)
		}
	}
}

func TestRelocateWithEmptyHistorySetsInstallDir(t *testing.T) {
	_, newDir := setupRelocate(t)

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	if !strings.Contains(configFileContents(t), "installDir: "+newDir) {
		t.Fatalf("config file does not set installDir to %s:\n%s", newDir, configFileContents(t))
	}
}

func TestRelocateSkipsRecordsWithoutInstallPath(t *testing.T) {
	_, newDir := setupRelocate(t)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", "")})

	out, err := runRelocate(t, newDir)
	if err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}

	if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != "" {
		t.Fatalf("recorded path = %q, want it untouched", got)
	}
	entries, err := os.ReadDir(newDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("new directory contents = %v (err %v), want it empty", entries, err)
	}
}

func TestRelocateSavesHistoryBeforeRemovingOldCopy(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	removeFile = func(path string) error {
		if got := loadHistoryRecords(t)[0].Binaries[0].InstallPath; got != filepath.Join(newDir, "tool") {
			t.Errorf("recorded path = %q when removing %s, want the new path already saved", got, path)
		}
		return os.Remove(path)
	}

	if out, err := runRelocate(t, newDir); err != nil {
		t.Fatalf("relocate error: %v\noutput: %s", err, out)
	}
}

func TestRelocateQuotesRemovalHintAndExposesItInJSON(t *testing.T) {
	oldDir, newDir := setupRelocate(t)
	oldPath := filepath.Join(oldDir, "my tools", "tool")
	writeExecutableFile(t, oldPath)
	writeHistoryRecords(t, []history.Record{newHistoryRecord("a1", "acme", "tool", "v1.0.0", "tool", "tool", oldPath)})
	removeFile = func(string) error { return fs.ErrPermission }
	setCommandFlag(t, relocateCmd, "format", "json")

	out, err := runRelocate(t, newDir)
	if err == nil {
		t.Fatalf("relocate succeeded, want an error for the failed removal\noutput: %s", out)
	}

	var result relocateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	entry := result.Binaries[0]
	if want := "sudo rm '" + oldPath + "'"; entry.Status != relocateRemoveFailed || entry.Hint != want {
		t.Fatalf("entry = %+v, want status %s with hint %q", entry, relocateRemoveFailed, want)
	}
}
