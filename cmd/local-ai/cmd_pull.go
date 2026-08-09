package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"local-ai/internal/hf"
)

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <owner>/<repo>[:quant]",
		Short: "Download a GGUF model from Hugging Face into the models directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}

			ref, err := hf.ParseRef(args[0])
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			fmt.Printf("resolving %s ...\n", args[0])
			filename, url, err := hf.Resolve(ctx, ref)
			if err != nil {
				return err
			}

			dest := filepath.Join(cfg.ModelsDir, filename)
			if _, err := os.Stat(dest); err == nil {
				fmt.Printf("%s already present at %s\n", filename, dest)
				return nil
			}

			fmt.Printf("downloading %s -> %s\n", filename, dest)
			lastPct := -1
			err = hf.Download(ctx, url, dest, func(downloaded, total int64) {
				if total <= 0 {
					return
				}
				pct := int(downloaded * 100 / total)
				if pct != lastPct {
					lastPct = pct
					fmt.Printf("\r%3d%% (%d/%d MB)", pct, downloaded/(1<<20), total/(1<<20))
				}
			})
			fmt.Println()
			if err != nil {
				return err
			}
			fmt.Printf("done: %s\n", dest)
			return nil
		},
	}
}
