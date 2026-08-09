package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"local-ai/internal/llamaclient"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether local-ai is running and each model's load state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			client := llamaclient.New(cfg.InternalHost, cfg.InternalPort)
			if !client.Healthy(ctx) {
				fmt.Println("local-ai: not running (llama-server unreachable at",
					fmt.Sprintf("%s:%d", cfg.InternalHost, cfg.InternalPort), ")")
				return nil
			}

			models, err := client.ListModels(ctx)
			if err != nil {
				return err
			}
			if len(models) == 0 {
				fmt.Println("local-ai: running, no models found in", cfg.ModelsDir)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "MODEL\tSTATUS\tCTX\tSIZE (GB)")
			for _, m := range models {
				ctxSize, sizeGB := "-", "-"
				if m.Meta != nil {
					ctxSize = fmt.Sprintf("%d", m.Meta.NCtx)
					sizeGB = fmt.Sprintf("%.2f", float64(m.Meta.Size)/(1<<30))
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", m.ID, m.Status.Value, ctxSize, sizeGB)
			}
			return w.Flush()
		},
	}
}
