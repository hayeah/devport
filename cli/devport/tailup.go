package main

import (
	"fmt"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var tailupCmd = &cobra.Command{
	Use:   "tailup <hash-prefix>",
	Short: "Enable Tailscale for a service",
	Args:  cobra.ExactArgs(1),
	RunE:  runTailup,
}

func init() {
	rootCmd.AddCommand(tailupCmd)
}

func runTailup(cmd *cobra.Command, args []string) error {
	hash, err := store.ResolvePrefix(args[0])
	if err != nil {
		return err
	}

	svc, err := store.Load(hash)
	if err != nil {
		return fmt.Errorf("load service: %w", err)
	}

	if svc.Tailnet {
		fmt.Fprintf(cmd.ErrOrStderr(), "tailnet already enabled for %s\n", svc.HashID)
		return printServiceJSON(svc)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "enabling tailnet for %s...\n", svc.HashID)
	if err := devport.TailscaleUp(svc.HashID, svc.Port); err != nil {
		return fmt.Errorf("tailscale up: %w", err)
	}

	svc.Tailnet = true
	if err := store.Save(svc); err != nil {
		return err
	}

	return printServiceJSON(svc)
}
