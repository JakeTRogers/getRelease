package cmd

import (
	"reflect"
	"testing"
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

	t.Run("unknown key", func(t *testing.T) {
		t.Parallel()

		if _, err := parseConfigValue("unknown.key", "value"); err == nil {
			t.Fatal("parseConfigValue() error = nil, want error")
		}
	})
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
