// Command local-ai is a lightweight wrapper/supervisor around llama-server
// that turns a Windows box into a multi-model local LLM host, reachable over
// the network behind local-ai's own API-key gateway.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"local-ai/internal/config"
	"local-ai/internal/winsvc"
)

var dataDirFlag string

func main() {
	// When the SCM launches us as a service, skip cobra entirely and run
	// the service handler (which shares the same supervisor+gateway logic
	// as `serve`) for the lifetime of the process.
	if isService, err := winsvc.IsWindowsService(); err == nil && isService {
		if err := winsvc.Run(); err != nil {
			os.Exit(1)
		}
		return
	}

	root := &cobra.Command{
		Use:   "local-ai",
		Short: "Supervise llama-server and expose it as a networked, multi-model LLM host",
	}
	root.PersistentFlags().StringVar(&dataDirFlag, "data-dir", "", "override data directory (default: $LOCAL_AI_DATA_DIR or %PROGRAMDATA%\\local-ai)")

	root.AddCommand(newServeCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newPullCmd())
	root.AddCommand(newRmCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newKeysCmd())
	root.AddCommand(newInstallServiceCmd())
	root.AddCommand(newUninstallServiceCmd())
	root.AddCommand(newServiceCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newLogsCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// loadConfig is the shared entrypoint every subcommand uses to resolve the
// data directory and load (or initialize) config.yaml.
func loadConfig() (*config.Config, config.Paths, error) {
	dir := config.DataDir(dataDirFlag)
	return config.Load(dir)
}
