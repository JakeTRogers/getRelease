package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
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
	if v.GetBool("keepDownloads") {
		t.Error("keepDownloads default should be false")
	}
	formats := v.GetStringSlice("assetPreferences.formats")
	if len(formats) != 2 || formats[0] != "tar.gz" || formats[1] != "zip" {
		t.Errorf("assetPreferences.formats default = %v, want [tar.gz zip]", formats)
	}
	if v.GetInt("cooldown") != 10 {
		t.Errorf("cooldown default = %d, want 10", v.GetInt("cooldown"))
	}
	if owners := v.GetStringSlice("trustedOwners"); len(owners) != 0 {
		t.Errorf("trustedOwners default = %v, want empty", owners)
	}
	excludes := v.GetStringSlice("assetPreferences.excludePatterns")
	for _, pattern := range []string{"*.deb", "*.rpm", "*.pkg.tar.zst"} {
		if !slices.Contains(excludes, pattern) {
			t.Errorf("assetPreferences.excludePatterns default = %v, want it to contain %q", excludes, pattern)
		}
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

// writeTestConfigFile points XDG_CONFIG_HOME at a temp dir, writes content as
// its config file (unless empty), and returns the config file path.
func writeTestConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgFile := filepath.Join(dir, appName, "config.yaml")
	if content == "" {
		return cfgFile
	}
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgFile
}

func readTestConfigFile(t *testing.T, cfgFile string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var settings map[string]any
	if err := yaml.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing config file: %v", err)
	}
	return settings
}

func TestSetValue_CreatesFileWithOnlyKey(t *testing.T) {
	cfgFile := writeTestConfigFile(t, "")

	if err := SetValue("autoExtract", false); err != nil {
		t.Fatalf("SetValue() error: %v", err)
	}

	got := readTestConfigFile(t, cfgFile)
	want := map[string]any{"autoExtract": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config file = %v, want %v", got, want)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfgFile)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("config file mode = %o, want 600", perm)
		}
	}
}

func TestSetValue_PreservesOtherKeys(t *testing.T) {
	cfgFile := writeTestConfigFile(t, "downloadDir: /tmp/downloads\nassetPreferences:\n  os: linux\n")

	if err := SetValue("assetPreferences.arch", "arm64"); err != nil {
		t.Fatalf("SetValue() error: %v", err)
	}

	got := readTestConfigFile(t, cfgFile)
	want := map[string]any{
		"downloadDir":      "/tmp/downloads",
		"assetPreferences": map[string]any{"os": "linux", "arch": "arm64"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config file = %v, want %v", got, want)
	}
}

func TestSetValue_ReplacesKeyInAnyCase(t *testing.T) {
	// Older versions wrote every key lowercased.
	cfgFile := writeTestConfigFile(t, "installcommand: old\nassetpreferences:\n  os: linux\n")

	if err := SetValue("installCommand", "new"); err != nil {
		t.Fatalf("SetValue() error: %v", err)
	}
	if err := SetValue("assetPreferences.OS", "darwin"); err != nil {
		t.Fatalf("SetValue() error: %v", err)
	}

	got := readTestConfigFile(t, cfgFile)
	want := map[string]any{
		"installCommand":   "new",
		"assetPreferences": map[string]any{"OS": "darwin"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config file = %v, want %v", got, want)
	}
}

func TestUnsetValue(t *testing.T) {
	cfgFile := writeTestConfigFile(t, "cooldown: 3\ndownloadDir: /tmp/downloads\nassetpreferences:\n  os: linux\n")

	if err := UnsetValue("cooldown"); err != nil {
		t.Fatalf("UnsetValue() error: %v", err)
	}
	// Removing the last child also removes the now-empty parent.
	if err := UnsetValue("assetPreferences.os"); err != nil {
		t.Fatalf("UnsetValue() error: %v", err)
	}

	got := readTestConfigFile(t, cfgFile)
	want := map[string]any{"downloadDir": "/tmp/downloads"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config file = %v, want %v", got, want)
	}
}

func TestSetValue_MalformedFile(t *testing.T) {
	writeTestConfigFile(t, "cooldown: [unclosed\n")

	if err := SetValue("cooldown", 5); err == nil {
		t.Fatal("SetValue() error = nil, want parse error")
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
