package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"local-ai/internal/winsvc"
)

func newInstallServiceCmd() *cobra.Command {
	var startup string
	cmd := &cobra.Command{
		Use:   "install-service",
		Short: "Install local-ai as a Windows Service (requires an elevated shell)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if startup != "auto" && startup != "manual" {
				return fmt.Errorf(`--startup must be "auto" or "manual", got %q`, startup)
			}
			exePath, err := os.Executable()
			if err != nil {
				return err
			}
			auto := startup == "auto"
			if err := winsvc.Install(exePath, auto); err != nil {
				return err
			}
			fmt.Printf("installed service %q (startup: %s)\n", winsvc.ServiceName, startup)
			return nil
		},
	}
	cmd.Flags().StringVar(&startup, "startup", "auto", `service startup type: "auto" or "manual"`)
	return cmd
}

func newUninstallServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-service",
		Short: "Remove the local-ai Windows Service (requires an elevated shell)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := winsvc.Uninstall(); err != nil {
				return err
			}
			fmt.Println("uninstalled service", winsvc.ServiceName)
			return nil
		},
	}
}

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Control the installed local-ai Windows Service (requires an elevated shell)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := winsvc.Start(); err != nil {
				return err
			}
			fmt.Println("started")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := winsvc.Stop(); err != nil {
				return err
			}
			fmt.Println("stopped")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Restart the service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := winsvc.Restart(); err != nil {
				return err
			}
			fmt.Println("restarted")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the service's current SCM state",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := winsvc.QueryStatus()
			if err != nil {
				return err
			}
			fmt.Println(status)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Set the service's startup type to Automatic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := winsvc.SetStartType(true); err != nil {
				return err
			}
			fmt.Println("startup type set to automatic")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Set the service's startup type to Manual",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := winsvc.SetStartType(false); err != nil {
				return err
			}
			fmt.Println("startup type set to manual")
			return nil
		},
	})

	return cmd
}
