// Package config provides application configuration management backed by Viper.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const appName = "getRelease"

// ConfigDir returns the platform-appropriate configuration directory.
// Linux: $XDG_CONFIG_HOME/getRelease (defaults to ~/.config/getRelease)
// macOS: ~/Library/Application Support/getRelease
// Windows: %APPDATA%\getRelease
func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName), nil
}

// DataDir returns the platform-appropriate data directory for history and other state.
// Linux: $XDG_DATA_HOME/getRelease (defaults to ~/.local/share/getRelease)
// macOS/Windows: same as ConfigDir (platform convention)
func DataDir() (string, error) {
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, appName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", appName), nil
	}
	return ConfigDir()
}

// ConfigFilePath returns the full path to the config file.
func ConfigFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// HistoryFilePath returns the full path to the history file.
func HistoryFilePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}

// DefaultDownloadDir returns the default download directory (~/install).
func DefaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "install"), nil
}

// DefaultInstallDir returns the platform-appropriate default install directory.
func DefaultInstallDir() string {
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, appName, "bin")
		}
		return filepath.Join("C:\\", "Users", appName, "bin")
	default:
		return "/usr/local/bin"
	}
}

// ExpandPath expands ~ to the user's home directory and cleans the path.
func ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Clean(path), nil
}
