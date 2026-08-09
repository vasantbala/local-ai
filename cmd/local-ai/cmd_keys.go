package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"local-ai/internal/keys"
)

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage gateway API keys",
	}
	cmd.AddCommand(newKeysCreateCmd())
	cmd.AddCommand(newKeysListCmd())
	cmd.AddCommand(newKeysRevokeCmd())
	return cmd
}

func loadKeyStore() (*keys.Store, error) {
	_, paths, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return keys.Load(paths.KeysPath)
}

func newKeysCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Issue a new gateway API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadKeyStore()
			if err != nil {
				return err
			}
			raw, key, err := store.Create(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("created key %q (id %s)\n", key.Name, key.ID)
			fmt.Println(raw)
			fmt.Println("Save this key now — it will not be shown again.")
			return nil
		},
	}
}

func newKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List issued gateway API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadKeyStore()
			if err != nil {
				return err
			}
			list := store.List()
			if len(list) == 0 {
				fmt.Println("no keys")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tPREFIX\tCREATED")
			for _, k := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", k.ID, k.Name, k.Prefix, k.CreatedAt.Format(time.RFC3339))
			}
			return w.Flush()
		},
	}
}

func newKeysRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id-or-name>",
		Short: "Revoke a gateway API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadKeyStore()
			if err != nil {
				return err
			}
			if !store.Revoke(args[0]) {
				return fmt.Errorf("no key found matching %q", args[0])
			}
			fmt.Println("revoked", args[0])
			return nil
		},
	}
}
