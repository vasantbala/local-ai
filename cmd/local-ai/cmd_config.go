package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"local-ai/internal/config"
)

var settableKeys = map[string]bool{
	"gateway_host":         true,
	"gateway_port":         true,
	"internal_host":        true,
	"internal_port":        true,
	"models_dir":           true,
	"models_max":           true,
	"idle_timeout_seconds": true,
	"llama_server_path":    true,
	"log_level":            true,
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and edit local-ai's configuration",
	}
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigModelCmd())
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Print the current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}
			if !reveal {
				redacted := *cfg
				redacted.InternalAPIKey = "***"
				cfg = &redacted
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			fmt.Print(string(data))
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal-secrets", false, "include the internal llama-server API key in the output")
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a top-level config value: " + strings.Join(sortedKeys(settableKeys), ", "),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			if !settableKeys[key] {
				return fmt.Errorf("unknown config key %q (settable: %s)", key, strings.Join(sortedKeys(settableKeys), ", "))
			}

			cfg, paths, err := loadConfig()
			if err != nil {
				return err
			}

			var perr error
			switch key {
			case "gateway_host":
				cfg.GatewayHost = value
			case "internal_host":
				cfg.InternalHost = value
			case "models_dir":
				cfg.ModelsDir = value
			case "llama_server_path":
				cfg.LlamaServerPath = value
			case "log_level":
				cfg.LogLevel = value
			case "gateway_port":
				cfg.GatewayPort, perr = strconv.Atoi(value)
			case "internal_port":
				cfg.InternalPort, perr = strconv.Atoi(value)
			case "models_max":
				cfg.ModelsMax, perr = strconv.Atoi(value)
			case "idle_timeout_seconds":
				cfg.IdleTimeoutSeconds, perr = strconv.Atoi(value)
			}
			if perr != nil {
				return fmt.Errorf("invalid value %q for %s: %w", value, key, perr)
			}

			if err := config.Save(paths, cfg); err != nil {
				return err
			}
			fmt.Printf("%s = %s\n", key, value)
			fmt.Println("restart local-ai (local-ai service restart, or re-run serve) to apply")
			return nil
		},
	}
}

func newConfigModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage per-model llama-server flag overrides (written into presets.ini)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set <model-id> <flag> <value>",
		Short: "Set a per-model llama-server flag override, e.g. ctx-size, gpu-layers",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, err := loadConfig()
			if err != nil {
				return err
			}
			id, flag, value := args[0], args[1], args[2]
			if cfg.ModelOverrides == nil {
				cfg.ModelOverrides = map[string]map[string]string{}
			}
			if cfg.ModelOverrides[id] == nil {
				cfg.ModelOverrides[id] = map[string]string{}
			}
			cfg.ModelOverrides[id][flag] = value

			if err := config.Save(paths, cfg); err != nil {
				return err
			}
			fmt.Printf("%s: %s = %s\n", id, flag, value)
			fmt.Println("restart local-ai (local-ai service restart, or re-run serve) to apply")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "unset <model-id> [flag]",
		Short: "Remove one override, or all overrides for the model if flag is omitted",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, err := loadConfig()
			if err != nil {
				return err
			}
			id := args[0]
			if len(args) == 2 {
				delete(cfg.ModelOverrides[id], args[1])
				if len(cfg.ModelOverrides[id]) == 0 {
					delete(cfg.ModelOverrides, id)
				}
			} else {
				delete(cfg.ModelOverrides, id)
			}
			if err := config.Save(paths, cfg); err != nil {
				return err
			}
			fmt.Println("updated")
			fmt.Println("restart local-ai (local-ai service restart, or re-run serve) to apply")
			return nil
		},
	})
	return cmd
}
