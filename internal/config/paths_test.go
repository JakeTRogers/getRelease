package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigDir(t *testing.T) {
	t.Parallel()
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	if dir == "" {
		t.Fatal("ConfigDir() returned empty string")
	}
	if filepath.Base(dir) != appName {
		t.Errorf("ConfigDir() = %q, want base dir %q", dir, appName)
	}
}

func TestDataDir_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	// Test with custom XDG_DATA_HOME
	t.Run("custom XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/tmp/testdata")
		dir, err := DataDir()
		if err != nil {
			t.Fatalf("DataDir() error: %v", err)
		}
		want := filepath.Join("/tmp/testdata", appName)
		if dir != want {
			t.Errorf("DataDir() = %q, want %q", dir, want)
		}
	})

	// Test default fallback
	t.Run("default fallback", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		dir, err := DataDir()
		if err != nil {
			t.Fatalf("DataDir() error: %v", err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".local", "share", appName)
		if dir != want {
			t.Errorf("DataDir() = %q, want %q", dir, want)
		}
	})
}

func TestConfigFilePath(t *testing.T) {
	t.Parallel()
	path, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath() error: %v", err)
	}
	if filepath.Base(path) != "config.yaml" {
		t.Errorf("ConfigFilePath() = %q, want base name config.yaml", path)
	}
}

func TestHistoryFilePath(t *testing.T) {
	t.Parallel()
	path, err := HistoryFilePath()
	if err != nil {
		t.Fatalf("HistoryFilePath() error: %v", err)
	}
	if filepath.Base(path) != "history.json" {
		t.Errorf("HistoryFilePath() = %q, want base name history.json", path)
	}
}

func TestDefaultDownloadDir(t *testing.T) {
	t.Parallel()
	dir, err := DefaultDownloadDir()
	if err != nil {
		t.Fatalf("DefaultDownloadDir() error: %v", err)
	}
	if filepath.Base(dir) != "install" {
		t.Errorf("DefaultDownloadDir() = %q, want base dir 'install'", dir)
	}
}

func TestDefaultInstallDir(t *testing.T) {
	t.Parallel()
	dir := DefaultInstallDir()
	if dir == "" {
		t.Fatal("DefaultInstallDir() returned empty string")
	}
	if runtime.GOOS != "windows" && dir != "/usr/local/bin" {
		t.Errorf("DefaultInstallDir() = %q, want /usr/local/bin", dir)
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "tilde prefix", input: "~/install", want: filepath.Join(home, "install")},
		{name: "tilde alone", input: "~", want: home},
		{name: "absolute path", input: "/usr/local/bin", want: "/usr/local/bin"},
		{name: "relative path", input: "relative/path", want: "relative/path"},
		{name: "dot prefix", input: "./local", want: "local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExpandPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExpandPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
