package archive

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsArchive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want bool
	}{
		{"foo.tar.gz", true},
		{"foo.tgz", true},
		{"foo.tar.xz", true},
		{"foo.txz", true},
		{"foo.tar.bz2", true},
		{"foo.tbz2", true},
		{"foo.tar", true},
		{"foo.zip", true},
		{"foo.exe", false},
		{"foo", false},
		{"Foo.TAR.GZ", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsArchive(tc.name)
			if got != tc.want {
				t.Fatalf("IsArchive(%q) = %v; want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestExtractTarGz(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	fn := "hello.txt"
	if err := os.WriteFile(filepath.Join(srcDir, fn), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "test.tar.gz")

	// Create a real tar.gz using system tar
	cmd := exec.Command("tar", "czf", archivePath, "-C", srcDir, fn)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create tar.gz: %v output: %s", err, string(out))
	}

	dest := t.TempDir()
	if err := Extract(archivePath, dest); err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	gotPath := filepath.Join(dest, fn)
	b, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", gotPath, err)
	}
	if string(b) != "hello" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestExtractZip(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "test.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("foo.txt")
	if err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			t.Fatalf("close zip writer: %v", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close zip file: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("zipcontent")); err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			t.Fatalf("close zip writer: %v", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close zip file: %v", closeErr)
		}
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	dest := t.TempDir()
	if err := Extract(zipPath, dest); err != nil {
		t.Fatalf("Extract(zip) failed: %v", err)
	}
	got := filepath.Join(dest, "foo.txt")
	b, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(b) != "zipcontent" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestExtractZipSlip(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// create an entry that attempts zip-slip
	w, err := zw.Create("../../evil.txt")
	if err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			t.Fatalf("close zip writer: %v", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close zip file: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("evil")); err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			t.Fatalf("close zip writer: %v", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close zip file: %v", closeErr)
		}
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	dest := t.TempDir()
	if err := Extract(zipPath, dest); err == nil {
		t.Fatalf("expected Extract to fail for zip-slip archive")
	}
}

func TestFindBinaries(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// ELF header
	elfPath := filepath.Join(tmp, "binprog")
	elfData := append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 100)...)
	if err := os.WriteFile(elfPath, elfData, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, "README.md"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "LICENSE"), []byte("l"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bins, err := FindBinaries(tmp)
	if err != nil {
		t.Fatalf("FindBinaries error: %v", err)
	}
	if len(bins) != 1 {
		t.Fatalf("expected 1 binary, got %d: %v", len(bins), bins)
	}
	if bins[0] != "binprog" {
		t.Fatalf("unexpected binary name: %s", bins[0])
	}
}

func TestExtractUnsupportedFormat(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fakeArchive := filepath.Join(tmp, "file.unknown")
	if err := os.WriteFile(fakeArchive, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(fakeArchive, t.TempDir()); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestExtractTarXz(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("xz-test"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "test.tar.xz")
	cmd := exec.Command("tar", "cJf", archivePath, "-C", srcDir, "data.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create tar.xz: %v output: %s", err, string(out))
	}

	dest := t.TempDir()
	if err := Extract(archivePath, dest); err != nil {
		t.Fatalf("Extract(tar.xz) failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "data.txt"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(b) != "xz-test" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestExtractPlainTar(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("tar-test"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "test.tar")
	cmd := exec.Command("tar", "cf", archivePath, "-C", srcDir, "file.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create tar: %v output: %s", err, string(out))
	}

	dest := t.TempDir()
	if err := Extract(archivePath, dest); err != nil {
		t.Fatalf("Extract(tar) failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(b) != "tar-test" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestExtractZipWithDirectory(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "withdir.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// Create a directory entry
	if _, err := zw.Create("subdir/"); err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			t.Fatalf("close zip writer: %v", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close zip file: %v", closeErr)
		}
		t.Fatal(err)
	}
	w, err := zw.Create("subdir/inner.txt")
	if err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			t.Fatalf("close zip writer: %v", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close zip file: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("nested")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	dest := t.TempDir()
	if err := Extract(zipPath, dest); err != nil {
		t.Fatalf("Extract(zip with dir) failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "subdir", "inner.txt"))
	if err != nil {
		t.Fatalf("read nested file: %v", err)
	}
	if string(b) != "nested" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestFindBinaries_MultipleTypes(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// PE binary (MZ header)
	if err := os.WriteFile(filepath.Join(tmp, "app.exe"), append([]byte{'M', 'Z'}, make([]byte, 50)...), 0o755); err != nil {
		t.Fatalf("write PE fixture: %v", err)
	}

	// Mach-O binary
	if err := os.WriteFile(filepath.Join(tmp, "app_mac"), append([]byte{0xfe, 0xed, 0xfa, 0xce}, make([]byte, 50)...), 0o755); err != nil {
		t.Fatalf("write Mach-O fixture: %v", err)
	}

	// Shebang script
	if err := os.WriteFile(filepath.Join(tmp, "script"), []byte("#!/bin/bash\necho hi"), 0o755); err != nil {
		t.Fatalf("write script fixture: %v", err)
	}

	// File with exec bit but no magic bytes
	if err := os.WriteFile(filepath.Join(tmp, "execfile"), []byte("just data"), 0o755); err != nil {
		t.Fatalf("write exec fixture: %v", err)
	}

	// Non-executable, non-magic — should NOT be found
	if err := os.WriteFile(filepath.Join(tmp, "data.dat"), []byte("no exec"), 0o644); err != nil {
		t.Fatalf("write data fixture: %v", err)
	}

	// Config files — should be skipped
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte("k: v"), 0o644); err != nil {
		t.Fatalf("write yaml fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write json fixture: %v", err)
	}

	bins, err := FindBinaries(tmp)
	if err != nil {
		t.Fatalf("FindBinaries error: %v", err)
	}
	if len(bins) != 4 {
		t.Fatalf("expected 4 binaries, got %d: %v", len(bins), bins)
	}
}

func TestFindBinaries_EmptyDir(t *testing.T) {
	t.Parallel()
	bins, err := FindBinaries(t.TempDir())
	if err != nil {
		t.Fatalf("FindBinaries error: %v", err)
	}
	if len(bins) != 0 {
		t.Fatalf("expected 0 binaries, got %d: %v", len(bins), bins)
	}
}
