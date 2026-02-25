package main

import (
	"fmt"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var taildownCmd = &cobra.Command{
	Use:   "taildown <hash-prefix>",
	Short: "Disable Tailscale for a service",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaildown,
}

func init() {
	rootCmd.AddCommand(taildownCmd)
}

func runTaildown(cmd *cobra.Command, args []string) error {
	hash, err := store.ResolvePrefix(args[0])
	if err != nil {
		return err
	}

	svc, err := store.Load(hash)
	if err != nil {
		return fmt.Errorf("load service: %w", err)
	}

	if !svc.Tailnet {
		fmt.Fprintf(cmd.ErrOrStderr(), "tailnet not enabled for %s\n", svc.HashID)
		return printServiceJSON(svc)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "disabling tailnet for %s...\n", svc.HashID)
	if err := devport.TailscaleClear(svc.HashID); err != nil {
		return fmt.Errorf("tailscale clear: %w", err)
	}

	svc.Tailnet = false
	if err := store.Save(svc); err != nil {
		return err
	}

	return printServiceJSON(svc)
}
