package cmd

import (
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"

	internalconfig "github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/selector"
)

func TestParseConfigValue(t *testing.T) {
	t.Parallel()

	t.Run("bool", func(t *testing.T) {
		t.Parallel()

		got, err := parseConfigValue("autoExtract", "false")
		if err != nil {
			t.Fatalf("parseConfigValue() error: %v", err)
		}
		value, ok := got.(bool)
		if !ok {
			t.Fatalf("parseConfigValue() returned %T, want bool", got)
		}
		if value {
			t.Fatal("parseConfigValue() returned true, want false")
		}
	})

	t.Run("string slice single value", func(t *testing.T) {
		t.Parallel()

		got, err := parseConfigValue("assetPreferences.formats", "zip")
		if err != nil {
			t.Fatalf("parseConfigValue() error: %v", err)
		}
		value, ok := got.([]string)
		if !ok {
			t.Fatalf("parseConfigValue() returned %T, want []string", got)
		}
		want := []string{"zip"}
		if !reflect.DeepEqual(value, want) {
			t.Fatalf("parseConfigValue() = %v, want %v", value, want)
		}
	})

	t.Run("string slice csv", func(t *testing.T) {
		t.Parallel()

		got, err := parseConfigValue("assetPreferences.excludePatterns", "*.deb, *.rpm")
		if err != nil {
			t.Fatalf("parseConfigValue() error: %v", err)
		}
		value, ok := got.([]string)
		if !ok {
			t.Fatalf("parseConfigValue() returned %T, want []string", got)
		}
		want := []string{"*.deb", "*.rpm"}
		if !reflect.DeepEqual(value, want) {
			t.Fatalf("parseConfigValue() = %v, want %v", value, want)
		}
	})

	t.Run("int", func(t *testing.T) {
		t.Parallel()

		got, err := parseConfigValue("cooldown", "5")
		if err != nil {
			t.Fatalf("parseConfigValue() error: %v", err)
		}
		value, ok := got.(int)
		if !ok {
			t.Fatalf("parseConfigValue() returned %T, want int", got)
		}
		if value != 5 {
			t.Fatalf("parseConfigValue() = %d, want 5", value)
		}
	})

	t.Run("int rejects non-numeric", func(t *testing.T) {
		t.Parallel()

		if _, err := parseConfigValue("cooldown", "abc"); err == nil {
			t.Fatal("parseConfigValue() error = nil, want parse error")
		}
	})

	t.Run("int rejects negative", func(t *testing.T) {
		t.Parallel()

		if _, err := parseConfigValue("cooldown", "-1"); err == nil {
			t.Fatal("parseConfigValue() error = nil, want negative-value error")
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		t.Parallel()

		if _, err := parseConfigValue("unknown.key", "value"); err == nil {
			t.Fatal("parseConfigValue() error = nil, want error")
		}
	})
}

func TestConfigKeysMatchSetDefaults(t *testing.T) {
	t.Parallel()

	fresh := viper.New()
	internalconfig.SetDefaults(fresh)

	want := make([]string, 0, len(configKeys))
	for _, k := range configKeys {
		want = append(want, strings.ToLower(k.value))
	}
	sort.Strings(want)

	got := fresh.AllKeys()
	sort.Strings(got)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configKeys drifted from internal/config.SetDefaults: got %v, want %v", got, want)
	}
}

func TestParseStringSliceValue(t *testing.T) {
	t.Parallel()

	got, err := parseStringSliceValue(`["zip","tar.gz"]`)
	if err != nil {
		t.Fatalf("parseStringSliceValue() error: %v", err)
	}
	want := []string{"zip", "tar.gz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStringSliceValue() = %v, want %v", got, want)
	}
}

func TestConfigGetRedactsToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)
	cfgViper.Set("token", "super-secret")

	var out bytes.Buffer
	configGetCmd.SetOut(&out)
	if err := configGetCmd.RunE(configGetCmd, []string{"token"}); err != nil {
		t.Fatalf("configGetCmd.RunE() error: %v", err)
	}

	got := strings.TrimSpace(out.String())
	if got != "<redacted>" {
		t.Errorf("config get token = %q, want <redacted>", got)
	}
}

func TestConfigSetRedactsTokenInConfirmation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)

	var out bytes.Buffer
	configSetCmd.SetOut(&out)
	if err := configSetCmd.RunE(configSetCmd, []string{"token", "super-secret"}); err != nil {
		t.Fatalf("configSetCmd.RunE() error: %v", err)
	}

	got := strings.TrimSpace(out.String())
	if got != "Set token = <redacted>" {
		t.Errorf("config set token confirmation = %q, want %q", got, "Set token = <redacted>")
	}
	if cfgViper.GetString("token") != "super-secret" {
		t.Errorf("cfgViper token = %q, want %q", cfgViper.GetString("token"), "super-secret")
	}
}

func TestCanonicalConfigKey(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"token":                            "token",
		"TOKEN":                            "token",
		"installdir":                       "installDir",
		"ASSETPREFERENCES.FORMATS":         "assetPreferences.formats",
		"assetpreferences":                 "assetPreferences",
		"assetPreferences.excludepatterns": "assetPreferences.excludePatterns",
		"unknown.key":                      "unknown.key",
		"asset":                            "asset",
	}
	for in, want := range tests {
		if got := canonicalConfigKey(in); got != want {
			t.Errorf("canonicalConfigKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigGetRedactsTokenInAnyCase(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)
	cfgViper.Set("token", "super-secret")

	var out bytes.Buffer
	configGetCmd.SetOut(&out)
	if err := configGetCmd.RunE(configGetCmd, []string{"TOKEN"}); err != nil {
		t.Fatalf("configGetCmd.RunE() error: %v", err)
	}

	if got := strings.TrimSpace(out.String()); got != "<redacted>" {
		t.Errorf("config get TOKEN = %q, want <redacted>", got)
	}
}

func TestConfigSetPersistsOnlyTheKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	t.Setenv("GETRELEASE_TOKEN", "env-secret")
	t.Setenv("GETRELEASE_INSTALLDIR", "/from/env")
	useTestCommandDeps(t, nil)
	if err := internalconfig.Init(cfgViper); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	configSetCmd.SetOut(&bytes.Buffer{})
	if err := configSetCmd.RunE(configSetCmd, []string{"COOLDOWN", "5"}); err != nil {
		t.Fatalf("configSetCmd.RunE() error: %v", err)
	}

	cfgPath, err := internalconfig.ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if got, want := string(data), "cooldown: 5\n"; got != want {
		t.Errorf("config file = %q, want %q (no env values or defaults)", got, want)
	}
}

func TestConfigResetKeyRemovesItFromFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)

	cfgPath, err := internalconfig.ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("cooldown: 3\ninstallDir: /opt/bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	configResetCmd.SetOut(&bytes.Buffer{})
	if err := configResetCmd.RunE(configResetCmd, []string{"cooldown"}); err != nil {
		t.Fatalf("configResetCmd.RunE() error: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if got, want := string(data), "installDir: /opt/bin\n"; got != want {
		t.Errorf("config file = %q, want %q", got, want)
	}
}

func TestConfigShowFormats(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)
	t.Cleanup(func() {
		if err := configShowCmd.Flags().Set("format", "yaml"); err != nil {
			t.Errorf("reset format: %v", err)
		}
	})

	tests := []struct {
		format     string
		wantPrefix string
		wantErr    string
	}{
		{format: "yaml", wantPrefix: "assetpreferences:"},
		{format: "text", wantPrefix: "assetpreferences:"}, // legacy alias for yaml
		{format: "JSON", wantPrefix: "{"},
		{format: "xml", wantErr: `unsupported output format "xml": use yaml or json`},
	}
	for _, tt := range tests {
		if err := configShowCmd.Flags().Set("format", tt.format); err != nil {
			t.Fatalf("set format: %v", err)
		}
		var out bytes.Buffer
		configShowCmd.SetOut(&out)

		err := configShowCmd.RunE(configShowCmd, nil)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("config show --format %s error = %v, want %q", tt.format, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("config show --format %s error: %v", tt.format, err)
		}
		if !strings.HasPrefix(out.String(), tt.wantPrefix) {
			t.Errorf("config show --format %s output = %q, want prefix %q", tt.format, out.String(), tt.wantPrefix)
		}
	}
}

func TestConfigResetAllConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		noFile     bool
		force      string
		confirm    func(string, bool) (bool, error)
		wantErr    string
		wantOutput string
		wantFile   bool
	}{
		{
			name:       "missing file does not prompt",
			noFile:     true,
			wantOutput: "Config file does not exist",
		},
		{
			name:       "declined",
			confirm:    func(string, bool) (bool, error) { return false, nil },
			wantOutput: "Aborted.",
			wantFile:   true,
		},
		{
			name:     "no terminal",
			confirm:  func(string, bool) (bool, error) { return false, selector.ErrNotInteractive },
			wantErr:  "use --force to proceed without the prompt",
			wantFile: true,
		},
		{
			name:       "confirmed",
			confirm:    func(string, bool) (bool, error) { return true, nil },
			wantOutput: "Removed config file",
		},
		{
			name:       "force skips the prompt",
			force:      "true",
			wantOutput: "Removed config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
			useTestCommandDeps(t, nil)
			setCommandFlag(t, configResetCmd, "force", cmp.Or(tt.force, "false"))

			cfgPath, err := internalconfig.ConfigFilePath()
			if err != nil {
				t.Fatal(err)
			}
			if !tt.noFile {
				if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfgPath, []byte("cooldown: 3\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			confirmAction = func(prompt string, defaultYes bool) (bool, error) {
				if tt.confirm == nil {
					t.Fatalf("unexpected confirmation prompt %q", prompt)
				}
				if defaultYes {
					t.Errorf("confirmation prompt defaults to yes, want no")
				}
				return tt.confirm(prompt, defaultYes)
			}
			var out bytes.Buffer
			configResetCmd.SetOut(&out)

			err = configResetCmd.RunE(configResetCmd, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("config reset error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("config reset error: %v", err)
			}
			if !strings.Contains(out.String(), tt.wantOutput) {
				t.Errorf("config reset output = %q, want %q", out.String(), tt.wantOutput)
			}
			if _, statErr := os.Stat(cfgPath); (statErr == nil) != tt.wantFile {
				t.Errorf("config file exists = %v, want %v", statErr == nil, tt.wantFile)
			}
		})
	}
}

func TestConfigGetFormatsValues(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)

	tests := []struct {
		key  string
		want string
	}{
		{key: "cooldown", want: "10\n"},
		{key: "assetPreferences.formats", want: "- tar.gz\n- zip\n"},
		{key: "assetPreferences.excludePatterns", want: "- '*.deb'\n- '*.rpm'\n- '*.apk'\n- '*.msi'\n- '*.pkg'\n- '*.pkg.tar.zst'\n"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		configGetCmd.SetOut(&out)
		if err := configGetCmd.RunE(configGetCmd, []string{tt.key}); err != nil {
			t.Fatalf("config get %s error: %v", tt.key, err)
		}
		if out.String() != tt.want {
			t.Errorf("config get %s = %q, want %q", tt.key, out.String(), tt.want)
		}
	}

	var out bytes.Buffer
	configGetCmd.SetOut(&out)
	if err := configGetCmd.RunE(configGetCmd, []string{"assetPreferences"}); err != nil {
		t.Fatalf("config get assetPreferences error: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("config get assetPreferences output is not YAML: %v\n%s", err, out.String())
	}
	if _, ok := parsed["formats"]; !ok || strings.Contains(out.String(), "map[") {
		t.Errorf("config get assetPreferences = %q, want a YAML mapping with formats", out.String())
	}
}

func TestConfigSetNormalizesPlatform(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	useTestCommandDeps(t, nil)

	tests := []struct {
		key, value, stored, output string
	}{
		{key: "assetPreferences.arch", value: "x86_64", stored: "amd64", output: `Set assetPreferences.arch = amd64 (normalized from "x86_64")`},
		{key: "assetPreferences.os", value: "macOS", stored: "darwin", output: `Set assetPreferences.os = darwin (normalized from "macOS")`},
		{key: "assetPreferences.arch", value: "arm64", stored: "arm64", output: "Set assetPreferences.arch = arm64"},
		{key: "assetPreferences.os", value: "", stored: "", output: "Set assetPreferences.os = "},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		configSetCmd.SetOut(&out)
		if err := configSetCmd.RunE(configSetCmd, []string{tt.key, tt.value}); err != nil {
			t.Fatalf("config set %s %q error: %v", tt.key, tt.value, err)
		}
		if got := strings.TrimRight(out.String(), "\n"); got != tt.output {
			t.Errorf("config set %s %q output = %q, want %q", tt.key, tt.value, got, tt.output)
		}
		if got := cfgViper.GetString(tt.key); got != tt.stored {
			t.Errorf("config set %s %q stored %q, want %q", tt.key, tt.value, got, tt.stored)
		}
	}
}
