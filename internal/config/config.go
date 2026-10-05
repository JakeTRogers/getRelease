package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// AssetPreferences controls how release assets are filtered and ranked.
type AssetPreferences struct {
	// OS overrides the auto-detected operating system for asset matching.
	OS string `mapstructure:"os" yaml:"os"`
	// Arch overrides the auto-detected architecture for asset matching.
	Arch string `mapstructure:"arch" yaml:"arch"`
	// Formats lists preferred archive formats in priority order.
	Formats []string `mapstructure:"formats" yaml:"formats"`
	// ExcludePatterns lists glob patterns for asset names to exclude.
	ExcludePatterns []string `mapstructure:"excludePatterns" yaml:"excludePatterns"`
}

// AppConfig holds all configuration values for the application.
type AppConfig struct {
	// DownloadDir is the base directory for downloading release assets.
	DownloadDir string `mapstructure:"downloadDir" yaml:"downloadDir"`
	// InstallDir is the target directory for installed binaries.
	InstallDir string `mapstructure:"installDir" yaml:"installDir"`
	// InstallCommand is the command template for installing binaries.
	// {source} and {target} are substituted with actual paths.
	InstallCommand string `mapstructure:"installCommand" yaml:"installCommand"`
	// AutoExtract controls whether --download-only extracts archives.
	// Installs and upgrades always extract archives to find their binaries.
	AutoExtract bool `mapstructure:"autoExtract" yaml:"autoExtract"`
	// Token authenticates GitHub API requests. Prefer the GETRELEASE_TOKEN,
	// GH_TOKEN, or GITHUB_TOKEN environment variables over storing it here.
	Token string `mapstructure:"token" yaml:"token"`
	// Cooldown is the minimum age in days a release must be before it is
	// eligible for install, guarding against freshly published malicious
	// releases. 0 disables the cooldown.
	Cooldown int `mapstructure:"cooldown" yaml:"cooldown"`
	// TrustedOwners lists GitHub owners (case-insensitive) whose releases are
	// exempt from Cooldown.
	TrustedOwners []string `mapstructure:"trustedOwners" yaml:"trustedOwners"`
	// AssetPreferences controls asset filtering and ranking.
	AssetPreferences AssetPreferences `mapstructure:"assetPreferences" yaml:"assetPreferences"`
}

// SetDefaults configures Viper with built-in default values.
func SetDefaults(v *viper.Viper) {
	dlDir, err := DefaultDownloadDir()
	if err != nil {
		dlDir = "~/install"
	}

	v.SetDefault("downloadDir", dlDir)
	v.SetDefault("installDir", DefaultInstallDir())
	v.SetDefault("installCommand", "sudo install -m 755 {source} {target}")
	v.SetDefault("autoExtract", true)
	v.SetDefault("token", "")
	v.SetDefault("cooldown", 10)
	v.SetDefault("trustedOwners", []string{})
	v.SetDefault("assetPreferences.os", "")
	v.SetDefault("assetPreferences.arch", "")
	v.SetDefault("assetPreferences.formats", []string{"tar.gz", "zip"})
	v.SetDefault("assetPreferences.excludePatterns", []string{
		"*.deb", "*.rpm", "*.apk", "*.msi", "*.pkg", "*.pkg.tar.zst",
	})
}

// Init initializes Viper with project defaults, config file, and env var support.
// It returns the Viper instance and any error from reading the config file.
// A missing config file is not treated as an error.
func Init(v *viper.Viper) error {
	SetDefaults(v)

	// Environment variable support
	v.SetEnvPrefix("GETRELEASE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Config file discovery
	cfgDir, err := ConfigDir()
	if err != nil {
		return fmt.Errorf("resolving config directory: %w", err)
	}

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(cfgDir)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			// Only return an error if the file exists but can't be read
			if _, statErr := os.Stat(v.ConfigFileUsed()); statErr == nil {
				return fmt.Errorf("reading config file: %w", err)
			}
		}
	}

	return nil
}

// Load unmarshals the effective Viper configuration into an AppConfig struct.
// Paths containing ~ are expanded to the user's home directory.
func Load(v *viper.Viper) (*AppConfig, error) {
	var cfg AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	expanded, err := ExpandPath(cfg.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("expanding download dir path: %w", err)
	}
	cfg.DownloadDir = expanded

	expanded, err = ExpandPath(cfg.InstallDir)
	if err != nil {
		return nil, fmt.Errorf("expanding install dir path: %w", err)
	}
	cfg.InstallDir = expanded

	return &cfg, nil
}

// SetValue persists a single key to the config file, leaving the file's other
// entries untouched. Defaults and environment variables are never written:
// GETRELEASE_TOKEN would otherwise leak into the file, and persisted defaults
// would shadow future default changes. Keys are matched case-insensitively,
// as Viper does, so a lowercased key written by an older version is replaced
// rather than duplicated.
func SetValue(key string, value any) error {
	return updateConfigFile(func(settings map[string]any) {
		setNestedKey(settings, strings.Split(key, "."), value)
	})
}

// UnsetValue removes a key from the config file so its default applies again.
func UnsetValue(key string) error {
	return updateConfigFile(func(settings map[string]any) {
		unsetNestedKey(settings, strings.Split(key, "."))
	})
}

func updateConfigFile(mutate func(map[string]any)) error {
	cfgPath, err := ConfigFilePath()
	if err != nil {
		return fmt.Errorf("resolving config file path: %w", err)
	}

	settings, err := readConfigFile(cfgPath)
	if err != nil {
		return err
	}
	mutate(settings)
	return writeConfigFile(cfgPath, settings)
}

func readConfigFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var settings map[string]any
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

// writeConfigFile writes settings via a temporary file renamed into place.
// os.CreateTemp creates the file with mode 0600, so the config file, which
// may hold a token, is only readable by its owner.
func writeConfigFile(path string, settings map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	tmpf, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp config file: %w", err)
	}
	tmpPath := tmpf.Name()

	if _, err := tmpf.Write(data); err != nil {
		return fmt.Errorf("writing temp config file: %w", errors.Join(err, tmpf.Close(), os.Remove(tmpPath)))
	}
	if err := tmpf.Close(); err != nil {
		return fmt.Errorf("closing temp config file: %w", errors.Join(err, os.Remove(tmpPath)))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming config file into place: %w", errors.Join(err, os.Remove(tmpPath)))
	}
	return nil
}

// takeKey removes every key in m equal to name ignoring case and returns the
// value of the last one removed.
func takeKey(m map[string]any, name string) any {
	var existing any
	for k, v := range m {
		if strings.EqualFold(k, name) {
			existing = v
			delete(m, k)
		}
	}
	return existing
}

func setNestedKey(m map[string]any, parts []string, value any) {
	existing := takeKey(m, parts[0])
	if len(parts) == 1 {
		m[parts[0]] = value
		return
	}
	child, ok := existing.(map[string]any)
	if !ok {
		child = map[string]any{}
	}
	setNestedKey(child, parts[1:], value)
	m[parts[0]] = child
}

func unsetNestedKey(m map[string]any, parts []string) {
	if len(parts) == 1 {
		takeKey(m, parts[0])
		return
	}
	for k, v := range m {
		if !strings.EqualFold(k, parts[0]) {
			continue
		}
		child, ok := v.(map[string]any)
		if !ok {
			continue
		}
		unsetNestedKey(child, parts[1:])
		if len(child) == 0 {
			delete(m, k)
		}
	}
}
