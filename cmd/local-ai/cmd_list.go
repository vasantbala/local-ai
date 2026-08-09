package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"local-ai/internal/config"
	"local-ai/internal/llamaclient"
)

func newListCmd() *cobra.Command {
	var litellmConfig bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List downloaded models and their live load state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}

			ids, err := modelIDsOnDisk(cfg.ModelsDir)
			if err != nil {
				return err
			}

			if litellmConfig {
				printLiteLLMConfig(cfg, ids)
				return nil
			}

			if len(ids) == 0 {
				fmt.Println("no models in", cfg.ModelsDir)
				return nil
			}

			statusByID := liveStatus(cmd.Context(), cfg)

			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "MODEL\tSTATUS")
			for _, id := range ids {
				status := "stopped"
				if m, ok := statusByID[id]; ok {
					status = m.Status.Value
				}
				fmt.Fprintf(w, "%s\t%s\n", id, status)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&litellmConfig, "litellm-config", false, "print a LiteLLM model_list: YAML block instead of a table")
	return cmd
}

func modelIDsOnDisk(modelsDir string) ([]string, error) {
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
	}
	sort.Strings(ids)
	return ids, nil
}

func liveStatus(ctx context.Context, cfg *config.Config) map[string]llamaclient.Model {
	out := map[string]llamaclient.Model{}
	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	client := llamaclient.New(cfg.InternalHost, cfg.InternalPort)
	if !client.Healthy(qctx) {
		return out
	}
	models, err := client.ListModels(qctx)
	if err != nil {
		return out
	}
	for _, m := range models {
		out[m.ID] = m
	}
	return out
}

func printLiteLLMConfig(cfg *config.Config, ids []string) {
	host := localReachableHost()
	fmt.Println("model_list:")
	for _, id := range ids {
		fmt.Printf("  - model_name: %s\n", id)
		fmt.Println("    litellm_params:")
		fmt.Printf("      model: openai/%s\n", id)
		fmt.Printf("      api_base: http://%s:%d/v1\n", host, cfg.GatewayPort)
		fmt.Println("      api_key: os.environ/LOCAL_AI_API_KEY")
	}
}

// localReachableHost makes a best-effort guess at this machine's LAN IP so
// the emitted LiteLLM config is usable without hand-editing; falls back to
// the local hostname if detection fails. No packets are actually sent: a UDP
// "dial" just asks the OS to pick the outbound route/interface.
func localReachableHost() string {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return addr.IP.String()
		}
	}
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}
	return "<this-host>"
}
