package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/spf13/cobra"

	"local-ai/internal/gateway"
	"local-ai/internal/keys"
	"local-ai/internal/supervisor"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the supervisor and gateway in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, err := loadConfig()
			if err != nil {
				return err
			}

			store, err := keys.Load(paths.KeysPath)
			if err != nil {
				return err
			}
			gw, err := gateway.New(cfg, store)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			fmt.Printf("local-ai: data dir       %s\n", paths.DataDir)
			fmt.Printf("local-ai: models dir     %s\n", cfg.ModelsDir)
			fmt.Printf("local-ai: internal (llama-server) http://%s:%d\n", cfg.InternalHost, cfg.InternalPort)
			fmt.Printf("local-ai: gateway        http://%s:%d\n", cfg.GatewayHost, cfg.GatewayPort)

			sup := supervisor.New(cfg, paths)
			go func() {
				<-ctx.Done()
				sup.Stop()
			}()

			var wg sync.WaitGroup
			errCh := make(chan error, 2)

			wg.Add(2)
			go func() {
				defer wg.Done()
				errCh <- sup.Run(ctx, os.Stdout)
			}()
			go func() {
				defer wg.Done()
				errCh <- gw.Run(ctx)
			}()

			wg.Wait()
			close(errCh)
			for e := range errCh {
				if e != nil {
					return e
				}
			}
			return nil
		},
	}
}
