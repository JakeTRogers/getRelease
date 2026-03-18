package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
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
	// AutoExtract controls whether archives are automatically extracted after download.
	AutoExtract bool `mapstructure:"autoExtract" yaml:"autoExtract"`
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
	v.SetDefault("assetPreferences.os", "")
	v.SetDefault("assetPreferences.arch", "")
	v.SetDefault("assetPreferences.formats", []string{"tar.gz", "zip"})
	v.SetDefault("assetPreferences.excludePatterns", []string{
		"*.deb", "*.rpm", "*.apk", "*.msi", "*.pkg",
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

// Save writes the given config to the config file, creating the directory if needed.
func Save(v *viper.Viper) error {
	cfgPath, err := ConfigFilePath()
	if err != nil {
		return fmt.Errorf("resolving config file path: %w", err)
	}

	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	return v.WriteConfigAs(cfgPath)
}
