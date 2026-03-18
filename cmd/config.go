package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"go.yaml.in/yaml/v3"

	internalconfig "github.com/JakeTRogers/getRelease/internal/config"
)

// configCmd is the parent for configuration management subcommands.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display effective configuration",
	RunE: func(cmd *cobra.Command, _ []string) error {
		format, _ := cmd.Flags().GetString("format")

		settings := cfgViper.AllSettings()

		switch strings.ToLower(format) {
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
		key := args[0]
		if !cfgViper.IsSet(key) {
			return fmt.Errorf("unknown config key: %s", key)
		}
		val := cfgViper.Get(key)
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
		key := args[0]
		value := args[1]

		parsedValue, err := parseConfigValue(key, value)
		if err != nil {
			return err
		}

		cfgViper.Set(key, parsedValue)
		if err := internalconfig.Save(cfgViper); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", key, value); err != nil {
			return fmt.Errorf("writing config set confirmation: %w", err)
		}
		return nil
	},
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
	Short: "Open config file in $EDITOR",
	RunE: func(cmd *cobra.Command, _ []string) error {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}

		cfgPath, err := internalconfig.ConfigFilePath()
		if err != nil {
			return fmt.Errorf("resolving config file path: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			if err := os.WriteFile(cfgPath, []byte{}, 0o644); err != nil {
				return fmt.Errorf("creating config file: %w", err)
			}
		}

		editorCmd := exec.Command(editor, cfgPath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			return fmt.Errorf("running editor: %w", err)
		}

		return nil
	},
}

var configResetCmd = &cobra.Command{
	Use:   "reset [key]",
	Short: "Reset all or a specific key to defaults",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			key := args[0]

			fresh := viper.New()
			internalconfig.SetDefaults(fresh)

			def := fresh.Get(key)
			if def == nil {
				return fmt.Errorf("unknown config key: %s", key)
			}

			cfgViper.Set(key, def)
			if err := internalconfig.Save(cfgViper); err != nil {
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

		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Are you sure you want to delete the config file at %s? [y/N]: ", cfgPath); err != nil {
			return fmt.Errorf("writing reset prompt: %w", err)
		}
		reader := bufio.NewReader(os.Stdin)
		resp, _ := reader.ReadString('\n')
		resp = strings.TrimSpace(strings.ToLower(resp))
		proceed := resp == "y" || resp == "yes"

		if !proceed {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted"); err != nil {
				return fmt.Errorf("writing reset abort message: %w", err)
			}
			return nil
		}

		if err := os.Remove(cfgPath); err != nil {
			if os.IsNotExist(err) {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Config file does not exist: %s\n", cfgPath); err != nil {
					return fmt.Errorf("writing missing config message: %w", err)
				}
				return nil
			}
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
	configShowCmd.Flags().String("format", "text", "output format: text, json")

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configPathCmd)

	rootCmd.AddCommand(configCmd)
}
