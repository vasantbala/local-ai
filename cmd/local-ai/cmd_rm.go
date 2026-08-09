package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"local-ai/internal/config"
)

func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <model-id>",
		Short: "Delete a downloaded model file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, err := loadConfig()
			if err != nil {
				return err
			}
			id := args[0]

			path := filepath.Join(cfg.ModelsDir, id+".gguf")
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("no model file found for %q in %s", id, cfg.ModelsDir)
			}
			if err := os.Remove(path); err != nil {
				return err
			}

			if _, ok := cfg.ModelOverrides[id]; ok {
				delete(cfg.ModelOverrides, id)
				if err := config.Save(paths, cfg); err != nil {
					return err
				}
			}

			fmt.Printf("removed %s\n", path)
			return nil
		},
	}
}
