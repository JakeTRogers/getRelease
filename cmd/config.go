package cmd

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"go.yaml.in/yaml/v3"

	internalconfig "github.com/JakeTRogers/getRelease/internal/config"
	"github.com/JakeTRogers/getRelease/internal/platform"
)

// configCmd is the parent for configuration management subcommands.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

// configKeys lists every configuration key known to getRelease, used to power
// `config get`/`config set`/`config reset` shell completion. Keep this in sync
// with internal/config/config.go's SetDefaults — TestConfigKeysMatchSetDefaults
// in config_test.go cross-checks the two, so a drift here fails CI instead of
// silently breaking completion.
var configKeys = []struct {
	value       string
	description string
}{
	{value: "downloadDir", description: "directory for downloaded release assets"},
	{value: "installDir", description: "directory for installed binaries"},
	{value: "installCommand", description: "command template for installing binaries"},
	{value: "autoExtract", description: "extract archives fetched with --download-only (installs and upgrades always extract)"},
	{value: "keepDownloads", description: "keep downloaded assets after a successful install or upgrade"},
	{value: "token", description: "GitHub API token (prefer GETRELEASE_TOKEN/GH_TOKEN/GITHUB_TOKEN env vars over storing here)"},
	{value: "cooldown", description: "minimum release age in days before install (0 disables)"},
	{value: "trustedOwners", description: "GitHub owners exempt from cooldown (case-insensitive)"},
	{value: "assetPreferences.os", description: "override detected OS for asset matching"},
	{value: "assetPreferences.arch", description: "override detected architecture for asset matching"},
	{value: "assetPreferences.formats", description: "preferred archive formats in priority order"},
	{value: "assetPreferences.excludePatterns", description: "glob patterns for asset names to exclude"},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display effective configuration",
	RunE: func(cmd *cobra.Command, _ []string) error {
		format, _ := cmd.Flags().GetString("format")
		format, err := normalizeConfigShowFormat(format)
		if err != nil {
			return err
		}

		settings := cfgViper.AllSettings()
		if tok, ok := settings["token"].(string); ok && tok != "" {
			settings["token"] = "<redacted>"
		}

		switch format {
		case "json":
			out, err := json.MarshalIndent(settings, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling config to json: %w", err)
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(out)); err != nil {
				return fmt.Errorf("writing json config output: %w", err)
			}
		default:
			out, err := yaml.Marshal(settings)
			if err != nil {
				return fmt.Errorf("marshaling config to yaml: %w", err)
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(out)); err != nil {
				return fmt.Errorf("writing yaml config output: %w", err)
			}
		}

		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a specific config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := canonicalConfigKey(args[0])
		if !cfgViper.IsSet(key) {
			return fmt.Errorf("unknown config key: %s", key)
		}
		val := cfgViper.Get(key)
		if key == "token" {
			if tok, _ := val.(string); tok != "" {
				val = "<redacted>"
			}
		}
		// Maps and lists print as YAML, like `config show`, rather than in
		// Go's map[...] and [...] syntax.
		if kind := reflect.ValueOf(val).Kind(); kind == reflect.Map || kind == reflect.Slice {
			out, err := yaml.Marshal(val)
			if err != nil {
				return fmt.Errorf("marshaling config value to yaml: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(out); err != nil {
				return fmt.Errorf("writing config value: %w", err)
			}
			return nil
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), val); err != nil {
			return fmt.Errorf("writing config value: %w", err)
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := canonicalConfigKey(args[0])
		value := args[1]

		parsedValue, err := parseConfigValue(key, value)
		if err != nil {
			return err
		}
		displayValue := value
		// Store Go's names for OS and architecture so asset matching
		// recognizes aliases such as x86_64 or macos.
		var normalized string
		switch key {
		case "assetPreferences.os":
			normalized = platform.NormalizeOS(value)
		case "assetPreferences.arch":
			normalized = platform.NormalizeArch(value)
		}
		if normalized != "" {
			parsedValue = normalized
			if normalized != value {
				displayValue = fmt.Sprintf("%s (normalized from %q)", normalized, value)
			}
		}

		cfgViper.Set(key, parsedValue)
		if err := internalconfig.SetValue(key, parsedValue); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		if key == "token" && value != "" {
			displayValue = "<redacted>"
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", key, displayValue); err != nil {
			return fmt.Errorf("writing config set confirmation: %w", err)
		}
		return nil
	},
}

// normalizeConfigShowFormat validates the config show format. "text" is
// accepted as an alias for yaml, the format it has always printed.
func normalizeConfigShowFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "", "yaml", "text":
		return "yaml", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported output format %q: use yaml or json", format)
	}
}

// canonicalConfigKey maps a key typed in any case (Viper ignores case) to its
// documented camelCase spelling, so token redaction checks match "TOKEN" too
// and the config file is written with consistent key names. A parent key such
// as "assetpreferences" maps to its prefix; unknown keys are returned as-is.
func canonicalConfigKey(key string) string {
	for _, k := range configKeys {
		if strings.EqualFold(k.value, key) {
			return k.value
		}
		if len(k.value) > len(key) && k.value[len(key)] == '.' && strings.EqualFold(k.value[:len(key)], key) {
			return k.value[:len(key)]
		}
	}
	return key
}

func parseConfigValue(key, raw string) (any, error) {
	fresh := viper.New()
	internalconfig.SetDefaults(fresh)
	if !fresh.IsSet(key) {
		return nil, fmt.Errorf("unknown config key: %s", key)
	}

	switch fresh.Get(key).(type) {
	case bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing %s as bool: %w", key, err)
		}
		return parsed, nil
	case string:
		return raw, nil
	case int:
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing %s as int: %w", key, err)
		}
		if parsed < 0 {
			return nil, fmt.Errorf("%s must be >= 0", key)
		}
		return parsed, nil
	case []string:
		return parseStringSliceValue(raw)
	default:
		return nil, fmt.Errorf("unsupported config key type: %s", key)
	}
}

func parseStringSliceValue(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var values []string
		if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
			return nil, fmt.Errorf("parsing list value: %w", err)
		}
		return values, nil
	}

	parts := strings.Split(trimmed, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}

	return values, nil
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open config file in $VISUAL or $EDITOR",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfgPath, err := internalconfig.ConfigFilePath()
		if err != nil {
			return fmt.Errorf("resolving config file path: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			if err := os.WriteFile(cfgPath, []byte{}, 0o600); err != nil {
				return fmt.Errorf("creating config file: %w", err)
			}
		}

		return openInEditor(cfgPath)
	},
}

// openInEditor opens path in the user's editor: $VISUAL, then $EDITOR, then
// vi (notepad on Windows). Editor arguments use shell quoting on Unix and
// native command-line quoting on Windows, as in EDITOR="code --wait".
func openInEditor(path string) error {
	editor := strings.TrimSpace(cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR")))
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}

	editorCmd, err := editorCommand(editor, path)
	if err != nil {
		return fmt.Errorf("preparing editor %q: %w", editor, err)
	}
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("running editor %q: %w", editor, err)
	}
	return nil
}

var configResetCmd = &cobra.Command{
	Use:   "reset [key]",
	Short: "Reset all or a specific key to defaults",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			key := canonicalConfigKey(args[0])

			fresh := viper.New()
			internalconfig.SetDefaults(fresh)

			def := fresh.Get(key)
			if def == nil {
				return fmt.Errorf("unknown config key: %s", key)
			}

			cfgViper.Set(key, def)
			if err := internalconfig.UnsetValue(key); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Reset %s to default\n", key); err != nil {
				return fmt.Errorf("writing config reset confirmation: %w", err)
			}
			return nil
		}

		// No key provided: remove config file
		cfgPath, err := internalconfig.ConfigFilePath()
		if err != nil {
			return fmt.Errorf("resolving config file path: %w", err)
		}

		if _, err := os.Stat(cfgPath); errors.Is(err, fs.ErrNotExist) {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Config file does not exist: %s\n", cfgPath); err != nil {
				return fmt.Errorf("writing missing config message: %w", err)
			}
			return nil
		}

		force, _ := cmd.Flags().GetBool("force")
		ok, err := confirmDestructive(fmt.Sprintf("Delete the config file at %s?", cfgPath), force)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted."); err != nil {
				return fmt.Errorf("writing reset abort message: %w", err)
			}
			return nil
		}

		if err := os.Remove(cfgPath); err != nil {
			return fmt.Errorf("removing config file: %w", err)
		}

		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed config file: %s\n", cfgPath); err != nil {
			return fmt.Errorf("writing removed config message: %w", err)
		}
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print config file path",
	RunE: func(cmd *cobra.Command, _ []string) error {
		p, err := internalconfig.ConfigFilePath()
		if err != nil {
			return fmt.Errorf("resolving config file path: %w", err)
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), p); err != nil {
			return fmt.Errorf("writing config path: %w", err)
		}
		return nil
	},
}

func init() {
	configShowCmd.Flags().String("format", "yaml", "output format: yaml, json")
	mustRegisterFlagCompletion(configShowCmd, "format", completeConfigShowFormatValues)

	configGetCmd.ValidArgsFunction = completeConfigKeyArg
	configSetCmd.ValidArgsFunction = completeConfigKeyArg
	configResetCmd.ValidArgsFunction = completeConfigKeyArg
	configResetCmd.Flags().Bool("force", false, "skip confirmation prompt when deleting the config file")

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configPathCmd)

	rootCmd.AddCommand(configCmd)
}
