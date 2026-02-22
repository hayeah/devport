package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var store *devport.Store

var rootCmd = &cobra.Command{
	Use:   "devport",
	Short: "Manage dev services with stable ports and Tailscale exposure",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		baseDir := filepath.Join(home, ".local", "share", "devport")
		store = devport.NewStore(baseDir)
		return store.EnsureDirs()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
