// Package history manages the installation history for tracking and upgrades.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// historyFile is the top-level JSON structure.
type historyFile struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// Store manages a set of history records stored on disk.
type Store struct {
	path    string
	records []Record
}

// NewStore constructs a new Store for the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads and parses the JSON history file. If the file does not exist,
// the store is initialized with an empty record set (not an error).
// Unknown file versions return an error. Malformed individual records are
// logged and skipped.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.records = nil
			return nil
		}
		return fmt.Errorf("read history file: %w", err)
	}

	// Decode into raw messages so we can log/skip malformed records.
	var raw struct {
		Version int               `json:"version"`
		Records []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse history file: %w", err)
	}
	if raw.Version != 1 {
		return fmt.Errorf("unknown history version: %d", raw.Version)
	}

	var parsed []Record
	for i, rj := range raw.Records {
		var rec Record
		if err := json.Unmarshal(rj, &rec); err != nil {
			slog.Warn("skipping malformed history record", "index", i, "err", err)
			continue
		}
		parsed = append(parsed, rec)
	}
	s.records = parsed
	return nil
}

// Save writes the current records to the configured path. It writes to a
// temporary file and renames it into place for atomicity.
func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	out := historyFile{Version: 1, Records: s.records}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}

	tmpf, err := os.CreateTemp(dir, "history-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp history file: %w", err)
	}
	tmpPath := tmpf.Name()

	if _, err := tmpf.Write(data); err != nil {
		return fmt.Errorf("write temp history file: %w", errors.Join(err, tmpf.Close(), os.Remove(tmpPath)))
	}
	if err := tmpf.Close(); err != nil {
		return fmt.Errorf("close temp history file: %w", errors.Join(err, os.Remove(tmpPath)))
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename history file into place: %w", errors.Join(err, os.Remove(tmpPath)))
	}
	return nil
}

// Add inserts or updates a record in the store. If a record with the same
// owner+repo exists it will be updated (tag, asset, binaries, updatedAt).
// New records will receive an ID if empty and InstalledAt will be set if zero.
func (s *Store) Add(r Record) error {
	now := time.Now()
	if r.ID == "" {
		r.ID = GenerateID()
	}
	if r.InstalledAt.IsZero() {
		r.InstalledAt = now
	}
	r.UpdatedAt = now

	for i := range s.records {
		if s.records[i].Owner == r.Owner && s.records[i].Repo == r.Repo {
			// update existing
			s.records[i].Tag = r.Tag
			s.records[i].Asset = r.Asset
			s.records[i].Binaries = r.Binaries
			s.records[i].UpdatedAt = r.UpdatedAt
			return nil
		}
	}

	// append new
	s.records = append(s.records, r)
	return nil
}

// Remove deletes a record by ID. Returns true if a record was removed.
func (s *Store) Remove(id string) bool {
	for i := range s.records {
		if s.records[i].ID == id {
			s.records = append(s.records[:i], s.records[i+1:]...)
			return true
		}
	}
	return false
}

// Records returns a copy of all stored records.
func (s *Store) Records() []Record {
	out := make([]Record, len(s.records))
	copy(out, s.records)
	return out
}

// FindByBinary returns records where any binary's Name or InstalledAs
// matches the given name.
func (s *Store) FindByBinary(name string) []Record {
	var out []Record
	for _, rec := range s.records {
		for _, b := range rec.Binaries {
			if b.Name == name || b.InstalledAs == name {
				out = append(out, rec)
				break
			}
		}
	}
	return out
}

// FindByRepo returns the record matching owner+repo or nil if not found.
func (s *Store) FindByRepo(owner, repo string) *Record {
	for _, rec := range s.records {
		if rec.Owner == owner && rec.Repo == repo {
			r := rec
			return &r
		}
	}
	return nil
}

// Prune removes records where all binaries' InstallPaths do not exist on disk.
// It returns the removed records.
func (s *Store) Prune() ([]Record, error) {
	var kept []Record
	var removed []Record

	for _, rec := range s.records {
		// consider a record missing if it has no binaries
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
		if keep {
			kept = append(kept, rec)
		} else {
			removed = append(removed, rec)
		}
	}

	s.records = kept
	return removed, nil
}
