package history

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateID(t *testing.T) {
	t.Parallel()
	id := GenerateID()
	if len(id) != 8 {
		t.Fatalf("GenerateID length = %d; want 8", len(id))
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("GenerateID not hex: %v", err)
	}
}

func TestStore_LoadNonExistent(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "history.json")
	s := NewStore(p)
	if err := s.Load(); err != nil {
		t.Fatalf("Load on non-existent file returned error: %v", err)
	}
	if got := s.Records(); len(got) != 0 {
		t.Fatalf("expected no records, got %d", len(got))
	}
}

func TestStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "history.json")
	s := NewStore(p)

	rec := Record{
		Owner:    "alice",
		Repo:     "proj",
		Tag:      "v1",
		Asset:    AssetInfo{Name: "asset.tar.gz", URL: "https://example"},
		Binaries: []Binary{{Name: "prog", InstalledAs: "prog", InstallPath: ""}},
		OS:       "linux",
		Arch:     "amd64",
	}
	if err := s.Add(rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	s2 := NewStore(p)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	rs := s2.Records()
	if len(rs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rs))
	}
	if rs[0].Owner != rec.Owner || rs[0].Repo != rec.Repo || rs[0].Tag != rec.Tag {
		t.Fatalf("record mismatch: got %+v want %+v", rs[0], rec)
	}
}

func TestStore_Add_Deduplication(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), "history.json"))

	r1 := Record{Owner: "o", Repo: "r", Tag: "v1", Binaries: []Binary{{Name: "b1"}}}
	if err := s.Add(r1); err != nil {
		t.Fatalf("Add r1: %v", err)
	}
	r2 := Record{Owner: "o", Repo: "r", Tag: "v2", Binaries: []Binary{{Name: "b2"}}}
	if err := s.Add(r2); err != nil {
		t.Fatalf("Add r2: %v", err)
	}
	rs := s.Records()
	if len(rs) != 1 {
		t.Fatalf("expected 1 record after dedupe, got %d", len(rs))
	}
	if rs[0].Tag != "v2" {
		t.Fatalf("expected tag updated to v2, got %s", rs[0].Tag)
	}
}

func TestStore_Remove(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), "history.json"))
	r := Record{Owner: "x", Repo: "y"}
	if err := s.Add(r); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rs := s.Records()
	if len(rs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rs))
	}
	id := rs[0].ID
	if ok := s.Remove(id); !ok {
		t.Fatalf("expected Remove to return true")
	}
	if len(s.Records()) != 0 {
		t.Fatalf("expected 0 records after remove")
	}
}

func TestStore_FindByBinary(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), "history.json"))
	if err := s.Add(Record{Owner: "a", Repo: "b", Binaries: []Binary{{Name: "bin1", InstalledAs: "bin1"}}}); err != nil {
		t.Fatalf("Add first record: %v", err)
	}
	if err := s.Add(Record{Owner: "c", Repo: "d", Binaries: []Binary{{Name: "other", InstalledAs: "other"}}}); err != nil {
		t.Fatalf("Add second record: %v", err)
	}

	found := s.FindByBinary("bin1")
	if len(found) != 1 {
		t.Fatalf("expected 1 result, got %d", len(found))
	}
	if found[0].Owner != "a" {
		t.Fatalf("unexpected owner: %s", found[0].Owner)
	}
}

func TestStore_FindByRepo(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), "history.json"))
	if err := s.Add(Record{Owner: "o1", Repo: "r1"}); err != nil {
		t.Fatalf("Add first repo record: %v", err)
	}
	if err := s.Add(Record{Owner: "o2", Repo: "r2"}); err != nil {
		t.Fatalf("Add second repo record: %v", err)
	}

	got := s.FindByRepo("o2", "r2")
	if got == nil {
		t.Fatalf("expected to find record for o2/r2")
	}
	if got.Owner != "o2" || got.Repo != "r2" {
		t.Fatalf("found unexpected record: %+v", got)
	}
}

func TestStore_Prune(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), "history.json"))

	// create a real file to represent an installed binary
	p := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := s.Add(Record{Owner: "keep", Repo: "r1", Binaries: []Binary{{InstallPath: p}}}); err != nil {
		t.Fatalf("Add keep record: %v", err)
	}
	if err := s.Add(Record{Owner: "rm", Repo: "r2", Binaries: []Binary{{InstallPath: "/non/existent/path"}}}); err != nil {
		t.Fatalf("Add remove record: %v", err)
	}

	removed, err := s.Prune()
	if err != nil {
		t.Fatalf("Prune error: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed record, got %d", len(removed))
	}
	if len(s.Records()) != 1 {
		t.Fatalf("expected 1 kept record, got %d", len(s.Records()))
	}
}

func TestStore_LoadInvalidVersion(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(p, []byte("{\"version\":99,\"records\":[]}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(p)
	if err := s.Load(); err == nil {
		t.Fatalf("expected Load to error on invalid version")
	}
}

func TestStore_LoadMalformedRecord(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "history.json")
	data := `{"version":1,"records":[{"owner":"ok","repo":"r","id":"1","tag":"v","asset":{"name":"a","url":"u"},"binaries":[]}, 123]}`
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(p)
	if err := s.Load(); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	rs := s.Records()
	if len(rs) != 1 {
		t.Fatalf("expected 1 valid record loaded, got %d", len(rs))
	}
	if rs[0].Owner != "ok" {
		t.Fatalf("unexpected owner: %s", rs[0].Owner)
	}
}
