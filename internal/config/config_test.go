package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/viper"
)

func TestSetDefaults(t *testing.T) {
	t.Parallel()
	v := viper.New()
	SetDefaults(v)

	if v.GetString("installCommand") != "sudo install -m 755 {source} {target}" {
		t.Errorf("installCommand default = %q, want template with {source} {target}", v.GetString("installCommand"))
	}
	if !v.GetBool("autoExtract") {
		t.Error("autoExtract default should be true")
	}
	formats := v.GetStringSlice("assetPreferences.formats")
	if len(formats) != 2 || formats[0] != "tar.gz" || formats[1] != "zip" {
		t.Errorf("assetPreferences.formats default = %v, want [tar.gz zip]", formats)
	}
}

func TestInit_NoConfigFile(t *testing.T) {
	v := viper.New()

	// Intentionally point to a non-existent directory
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Init(v); err != nil {
		t.Fatalf("Init() error: %v (missing config file should not be an error)", err)
	}

	// Defaults should be set
	if v.GetString("downloadDir") == "" {
		t.Error("downloadDir should have a default value after Init()")
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	v := viper.New()
	SetDefaults(v)

	cfg, err := Load(v)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.AutoExtract != true {
		t.Error("AutoExtract should be true by default")
	}
	if cfg.DownloadDir == "" {
		t.Error("DownloadDir should not be empty after Load()")
	}
	if cfg.InstallDir == "" {
		t.Error("InstallDir should not be empty after Load()")
	}
	if cfg.InstallCommand == "" {
		t.Error("InstallCommand should not be empty after Load()")
	}
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, appName)

	// Point XDG_CONFIG_HOME to our temp dir so ConfigFilePath uses it
	t.Setenv("XDG_CONFIG_HOME", dir)

	v := viper.New()
	SetDefaults(v)
	v.Set("autoExtract", false)

	if err := Save(v); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	cfgFile := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(cfgFile); err != nil {
		t.Fatalf("config file not created at %s: %v", cfgFile, err)
	}

	// Read it back with a fresh Viper
	v2 := viper.New()
	v2.SetConfigFile(cfgFile)
	if err := v2.ReadInConfig(); err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	if v2.GetBool("autoExtract") {
		t.Error("autoExtract should be false in saved config")
	}
}

func TestDefaultInstallDir_NonWindows(t *testing.T) {
	t.Parallel()
	dir := DefaultInstallDir()
	if runtime.GOOS == "windows" {
		if dir == "" {
			t.Error("DefaultInstallDir() should not be empty on windows")
		}
	} else {
		if dir != "/usr/local/bin" {
			t.Errorf("DefaultInstallDir() = %q, want /usr/local/bin", dir)
		}
	}
}

func TestLoad_UnmarshalError(t *testing.T) {
	t.Parallel()
	v := viper.New()
	// Set a value that can't unmarshal into bool
	v.Set("autoExtract", "not-a-bool-slice-map")
	_, err := Load(v)
	if err == nil {
		t.Fatal("Load() should return an error for invalid types")
	}
}

func TestInit_WithConfigFile(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, appName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgContent := []byte("autoExtract: false\ndownloadDir: /tmp/downloads\n")
	if err := os.WriteFile(filepath.Join(appDir, "config.yaml"), cfgContent, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", dir)

	v := viper.New()
	if err := Init(v); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if v.GetBool("autoExtract") {
		t.Error("autoExtract should be false from config file")
	}
	if v.GetString("downloadDir") != "/tmp/downloads" {
		t.Errorf("downloadDir = %q, want /tmp/downloads", v.GetString("downloadDir"))
	}
}

func TestExpandPath_Absolute(t *testing.T) {
	t.Parallel()
	result, err := ExpandPath("/usr/local/bin")
	if err != nil {
		t.Fatalf("ExpandPath() error: %v", err)
	}
	if result != "/usr/local/bin" {
		t.Errorf("ExpandPath() = %q, want /usr/local/bin", result)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	t.Parallel()
	result, err := ExpandPath("~/mydir")
	if err != nil {
		t.Fatalf("ExpandPath() error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "mydir")
	if result != want {
		t.Errorf("ExpandPath(~/mydir) = %q, want %q", result, want)
	}
}

func TestExpandPath_TildeOnly(t *testing.T) {
	t.Parallel()
	result, err := ExpandPath("~")
	if err != nil {
		t.Fatalf("ExpandPath() error: %v", err)
	}
	home, _ := os.UserHomeDir()
	if result != home {
		t.Errorf("ExpandPath(~) = %q, want %q", result, home)
	}
}
