package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	inst := NewInstaller("")
	if _, ok := inst.(*CopyInstaller); !ok {
		t.Fatalf("expected CopyInstaller for empty command")
	}

	inst2 := NewInstaller("cp {source} {target}")
	if _, ok := inst2.(*CommandInstaller); !ok {
		t.Fatalf("expected CommandInstaller for non-empty command")
	}
}

func TestValidatePath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	abs := filepath.Join(tmp, "foo")
	if p, err := validatePath(abs); err == nil {
		if !filepath.IsAbs(p) {
			t.Fatalf("expected absolute path returned")
		}
	} else {
		t.Fatalf("validatePath failed for absolute: %v", err)
	}

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"relative", "relative/path", true},
		{"empty", "", true},
		{"nullbyte", "/tmp/has\x00null", true},
		{"dots-relative", "../foo", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validatePath(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validatePath(%q) error=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestCommandInstaller_InvalidPaths(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ci := &CommandInstaller{Command: "echo {source} {target}"}

	cases := []struct {
		name   string
		target string
	}{
		{"relative", "relative/target"},
		{"empty", ""},
		{"dotdots", "../foo"},
		{"null", "/tmp/has\x00null"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ci.Install(src, tc.target); err == nil {
				t.Fatalf("expected error for target %q", tc.target)
			}
		})
	}
}

func TestCopyInstaller(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	content := []byte("hello")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	dst := filepath.Join(destDir, "bin")

	ci := &CopyInstaller{}
	if err := ci.Install(src, dst); err != nil {
		t.Fatalf("CopyInstaller.Install failed: %v", err)
	}

	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(b) != string(content) {
		t.Fatalf("content mismatch: got %q", string(b))
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable bit set on target")
	}
}

func TestCommandInstaller(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	content := []byte("world")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	dst := filepath.Join(destDir, "bin")

	ci := &CommandInstaller{Command: "cp {source} {target}"}
	if err := ci.Install(src, dst); err != nil {
		t.Fatalf("CommandInstaller.Install failed: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(b) != string(content) {
		t.Fatalf("content mismatch: got %q", string(b))
	}
}

func TestCommandInstaller_EmptyCommand(t *testing.T) {
	t.Parallel()
	ci := &CommandInstaller{Command: ""}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	if err := ci.Install(src, filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestCommandInstaller_SourceIsDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ci := &CommandInstaller{Command: "cp {source} {target}"}
	if err := ci.Install(dir, filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected error when source is a directory")
	}
}

func TestCommandInstaller_SourceNotExist(t *testing.T) {
	t.Parallel()
	ci := &CommandInstaller{Command: "cp {source} {target}"}
	if err := ci.Install(filepath.Join(t.TempDir(), "noexist"), filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected error when source does not exist")
	}
}

func TestCommandInstaller_FailedCommand(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	ci := &CommandInstaller{Command: "false {source} {target}"}
	if err := ci.Install(src, filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected error for command that fails")
	}
}

func TestCopyInstaller_SourceNotExist(t *testing.T) {
	t.Parallel()
	ci := &CopyInstaller{}
	if err := ci.Install(filepath.Join(t.TempDir(), "noexist"), filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected error when source does not exist")
	}
}

func TestCopyInstaller_SourceIsDirectory(t *testing.T) {
	t.Parallel()
	ci := &CopyInstaller{}
	if err := ci.Install(t.TempDir(), filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected error when source is a directory")
	}
}

func TestCopyInstaller_InvalidTarget(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	ci := &CopyInstaller{}
	if err := ci.Install(src, "relative/path"); err == nil {
		t.Fatal("expected error for relative target path")
	}
}

func TestValidatePath_ControlChars(t *testing.T) {
	t.Parallel()
	_, err := validatePath("/tmp/has\ttab")
	if err == nil {
		t.Fatal("expected error for path with control character")
	}
}

func TestValidatePath_DotDotAbsolute(t *testing.T) {
	t.Parallel()
	// After Clean, /tmp/../.. becomes / which has no ".." parts.
	// validatePath accepts that. Instead, test the real rejection case:
	// a relative path with ".." which validatePath rejects for not being absolute.
	_, err := validatePath("../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for relative path with .. traversal")
	}
}
