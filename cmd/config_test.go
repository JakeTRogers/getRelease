package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/viper"

	internalconfig "github.com/JakeTRogers/getRelease/internal/config"
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
