package cmd

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/history"
	"github.com/JakeTRogers/getRelease/internal/install"
)

var relocateCmd = &cobra.Command{
	Use:          "relocate <dir>",
	Short:        "Move every recorded install to a new install directory",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		format, _ := cmd.Flags().GetString("format")
		format, err := normalizeOutputFormat(format)
		if err != nil {
			return err
		}

		dir, err := config.ExpandPath(args[0])
		if err != nil {
			return fmt.Errorf("expanding %s: %w", args[0], err)
		}
		if dir, err = filepath.Abs(dir); err != nil {
			return fmt.Errorf("resolving %s: %w", args[0], err)
		}
		if err := prepareInstallDir(dir, dryRun); err != nil {
			return err
		}

		cfg, err := config.Load(cfgViper)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		histPath, err := config.HistoryFilePath()
		if err != nil {
			return fmt.Errorf("resolving history path: %w", err)
		}
		store := history.NewStore(histPath)
		if err := store.Load(); err != nil {
			return fmt.Errorf("loading history: %w", err)
		}

		out := cmd.OutOrStdout()
		rel := &relocator{
			store:     store,
			installer: newBinaryInstaller(cfg.InstallCommand),
			dir:       dir,
			dryRun:    dryRun,
			planned:   map[string]bool{},
			moved:     map[string]string{},
		}
		result := relocateResult{DryRun: dryRun, InstallDir: dir, Binaries: []relocateEntry{}}
		var needsAttention, blocked bool
		// Records shares Binaries with the store, and a move repoints the
		// Binaries of later records too; iterate over the paths as loaded.
		records := store.Records()
		for i := range records {
			records[i].Binaries = slices.Clone(records[i].Binaries)
		}
		for _, rec := range records {
			for _, bin := range rec.Binaries {
				entry, err := rel.relocate(rec, bin)
				if err != nil {
					return err
				}
				result.Binaries = append(result.Binaries, entry)
				needsAttention = needsAttention || entry.needsAttention()
				blocked = blocked || entry.blocksInstallDir()
				if format == "text" {
					if err := writeRelocateEntry(out, entry); err != nil {
						return err
					}
				}
			}
		}

		if !dryRun && !blocked {
			cfgViper.Set("installDir", dir)
			if err := config.SetValue("installDir", dir); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			result.InstallDirUpdated = true
		}

		if format == "json" {
			if err := outputJSON(cmd, result); err != nil {
				return fmt.Errorf("writing relocate result: %w", err)
			}
		} else if err := writeRelocateSummary(out, result, blocked); err != nil {
			return err
		}
		if needsAttention {
			return errors.New("some binaries were not relocated")
		}
		return nil
	},
}

// relocateResult is the JSON form of a whole relocation.
type relocateResult struct {
	DryRun            bool            `json:"dryRun"`
	InstallDir        string          `json:"installDir"`
	InstallDirUpdated bool            `json:"installDirUpdated"`
	Binaries          []relocateEntry `json:"binaries"`
}

// prepareInstallDir makes sure dir is a usable directory, creating it unless
// this is a dry run.
func prepareInstallDir(dir string, dryRun bool) error {
	info, err := os.Stat(dir)
	switch {
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case err == nil || dryRun:
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	return nil
}

type relocateStatus string

const (
	relocateWouldMove     relocateStatus = "would-move"
	relocateMoved         relocateStatus = "moved"
	relocateInPlace       relocateStatus = "in-place"
	relocateMissing       relocateStatus = "missing"
	relocateConflict      relocateStatus = "conflict"
	relocateInvalidName   relocateStatus = "invalid-name"
	relocateInstallFailed relocateStatus = "install-failed"
	relocateRemoveFailed  relocateStatus = "remove-failed"
)

// relocateEntry is the outcome of relocating one Binary.
type relocateEntry struct {
	Package string         `json:"package"`
	Binary  string         `json:"binary"`
	From    string         `json:"from"`
	To      string         `json:"to"`
	Status  relocateStatus `json:"status"`
	Error   string         `json:"error,omitempty"`
	// Hint is a command the user can run to finish a step relocate could not.
	Hint string `json:"hint,omitempty"`
}

// needsAttention reports whether the entry needs the user to act.
func (e relocateEntry) needsAttention() bool {
	switch e.Status {
	case relocateConflict, relocateInvalidName, relocateInstallFailed, relocateRemoveFailed:
		return true
	default:
		return false
	}
}

// blocksInstallDir reports whether the Binary was left outside the new
// Install directory because of a problem, so future installs must not move
// there yet. A failed removal does not: the Binary is already in place.
func (e relocateEntry) blocksInstallDir() bool {
	return e.Status == relocateConflict || e.Status == relocateInvalidName || e.Status == relocateInstallFailed
}

// relocator moves Binaries into one Install directory.
type relocator struct {
	store     *history.Store
	installer install.Installer
	dir       string
	dryRun    bool
	// planned holds the targets a dry run would have created, so Binaries
	// that share a target are reported as the conflict a real run would hit.
	planned map[string]bool
	// moved maps each old path already moved (or, in a dry run, planned) to
	// its new path. Packages that installed the same name share one file, so
	// later Binaries recorded at that path follow it instead of reporting it
	// missing.
	moved map[string]string
}

// relocate moves one Binary: it installs a new copy from the old file, records
// the new path in History, then removes the old copy. The only error returned
// is one that makes continuing unsafe.
func (r *relocator) relocate(rec history.Record, bin history.Binary) (relocateEntry, error) {
	from := bin.InstallPath
	recordedName := cmp.Or(bin.InstalledAs, filepath.Base(from))
	entry := relocateEntry{
		Package: rec.Owner + "/" + rec.Repo,
		Binary:  bin.InstalledAs,
		From:    from,
		To:      filepath.Join(r.dir, recordedName),
	}

	if from == "" {
		entry.Status = relocateMissing
		return entry, nil
	}
	if to, ok := r.moved[from]; ok {
		entry.To = to
		entry.Status = relocateMoved
		if r.dryRun {
			entry.Status = relocateWouldMove
		}
		return entry, nil
	}
	// History is a plain file, so a name such as "../tool" must not be
	// allowed to place the Binary outside the new Install directory.
	name, err := validateInstallName(recordedName)
	if err != nil {
		entry.To = ""
		entry.Status = relocateInvalidName
		entry.Error = fmt.Sprintf("invalid install name %q: %v", recordedName, err)
		return entry, nil
	}
	entry.To = filepath.Join(r.dir, name)
	fromInfo, err := os.Lstat(from)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			entry.Status = relocateMissing
			return entry, nil
		}
		entry.Status = relocateInstallFailed
		entry.Error = err.Error()
		return entry, nil
	}
	if from == entry.To {
		entry.Status = relocateInPlace
		return entry, nil
	}
	if _, err := os.Lstat(entry.To); err == nil || r.planned[entry.To] {
		entry.Status = relocateConflict
		return entry, nil
	}
	if r.dryRun {
		r.planned[entry.To] = true
		r.moved[from] = entry.To
		entry.Status = relocateWouldMove
		return entry, nil
	}

	// Install from the file a linked Binary points at, so an installCommand
	// that links, such as `ln -s {source} {target}`, does not link to the
	// old link that is about to be removed.
	source, err := filepath.EvalSymlinks(from)
	if err != nil {
		entry.Status = relocateInstallFailed
		entry.Error = fmt.Sprintf("resolving %s: %v", from, err)
		return entry, nil
	}
	if err := r.installer.Install(source, entry.To); err != nil {
		entry.Status = relocateInstallFailed
		entry.Error = err.Error()
		return entry, nil
	}
	if err := checkIndependentCopy(entry.To, fromInfo); err != nil {
		return r.undoInstall(entry, err), nil
	}
	ids := r.repoint(from, entry.To)
	if len(ids) == 0 {
		return r.undoInstall(entry, errors.New("history record not found")), nil
	}
	if err := r.store.Save(); err != nil {
		for _, id := range ids {
			r.store.SetInstallPath(id, entry.To, from)
		}
		return r.undoInstall(entry, fmt.Errorf("saving history: %w", err)), nil
	}
	r.moved[from] = entry.To
	if err := removeFile(from); err != nil {
		entry.Status = relocateRemoveFailed
		entry.Error = err.Error()
		entry.Hint = "sudo rm " + shellQuote(from)
		return entry, nil
	}
	entry.Status = relocateMoved
	return entry, nil
}

// checkIndependentCopy reports an error unless the new copy at to is usable
// and will stay usable once the old file described by from is removed: a
// symbolic link that resolves to that file would be left dangling.
func checkIndependentCopy(to string, from fs.FileInfo) error {
	target, err := os.Stat(to)
	if err != nil {
		return fmt.Errorf("the new copy is not usable: %w", err)
	}
	link, err := os.Lstat(to)
	if err != nil {
		return fmt.Errorf("the new copy is not usable: %w", err)
	}
	if link.Mode()&fs.ModeSymlink != 0 && os.SameFile(target, from) {
		return errors.New("installCommand linked the new copy to the old one instead of copying it, so removing the old copy would break it; use an installCommand that copies")
	}
	return nil
}

// repoint points every Binary recorded at from to to, so no record is left
// at the old path once it is removed. It returns the IDs of the records it
// changed.
func (r *relocator) repoint(from, to string) []string {
	var ids []string
	for _, rec := range r.store.Records() {
		if r.store.SetInstallPath(rec.ID, from, to) {
			ids = append(ids, rec.ID)
		}
	}
	return ids
}

// undoInstall removes the new copy of a Binary whose move could not be
// recorded, so a rerun starts clean instead of finding a conflict.
func (r *relocator) undoInstall(entry relocateEntry, cause error) relocateEntry {
	entry.Status = relocateInstallFailed
	entry.Error = cause.Error()
	if err := removeFile(entry.To); err != nil {
		entry.Error += fmt.Sprintf("; the new copy at %s could not be removed: %v", entry.To, err)
	}
	return entry
}

// shellQuote quotes s for a POSIX shell unless it is already safe.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeRelocateEntry(w io.Writer, e relocateEntry) error {
	var err error
	switch e.Status {
	case relocateWouldMove:
		_, err = fmt.Fprintf(w, "Would move %s: %s -> %s\n", e.Package, e.From, e.To)
	case relocateMoved:
		_, err = fmt.Fprintf(w, "Moved %s: %s -> %s\n", e.Package, e.From, e.To)
	case relocateInPlace:
		_, err = fmt.Fprintf(w, "Already in place %s: %s\n", e.Package, e.From)
	case relocateMissing:
		_, err = fmt.Fprintf(w, "Skipped %s: %s is missing\n", e.Package, cmp.Or(e.From, "the recorded path"))
	case relocateConflict:
		_, err = fmt.Fprintf(w, "Skipped %s: conflict, %s already exists\n", e.Package, e.To)
	case relocateInvalidName:
		_, err = fmt.Fprintf(w, "Skipped %s: %s\n", e.Package, e.Error)
	case relocateInstallFailed:
		_, err = fmt.Fprintf(w, "Failed %s: installing %s: %s\n", e.Package, e.To, e.Error)
	case relocateRemoveFailed:
		_, err = fmt.Fprintf(w, "Moved %s: %s -> %s, but the old copy could not be removed: %s\n  remove it yourself: %s\n", e.Package, e.From, e.To, e.Error, e.Hint)
	}
	if err != nil {
		return fmt.Errorf("writing relocate result: %w", err)
	}
	return nil
}

func writeRelocateSummary(w io.Writer, r relocateResult, blocked bool) error {
	var err error
	switch {
	case r.DryRun:
		_, err = fmt.Fprintf(w, "Would set installDir to %s\n", r.InstallDir)
	case blocked:
		_, err = fmt.Fprintln(w, "installDir was not changed because some binaries could not be moved; fix them and run relocate again")
	default:
		_, err = fmt.Fprintf(w, "Set installDir to %s\n", r.InstallDir)
	}
	if err != nil {
		return fmt.Errorf("writing relocate summary: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(relocateCmd)
	relocateCmd.Flags().Bool("dry-run", false, "show what would be moved without changing anything")
	relocateCmd.Flags().String("format", "text", "output format: text, json")
	mustRegisterFlagCompletion(relocateCmd, "format", completeOutputFormatValues)
}
