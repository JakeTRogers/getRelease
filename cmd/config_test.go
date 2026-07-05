package cmd

import (
	"bytes"
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
