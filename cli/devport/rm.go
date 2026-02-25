package main

import (
	"fmt"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm <hash-prefix>",
	Short: "Remove a service (stop and delete files)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRM,
}

func init() {
	rootCmd.AddCommand(rmCmd)
}

func runRM(cmd *cobra.Command, args []string) error {
	hash, err := store.ResolvePrefix(args[0])
	if err != nil {
		return err
	}

	svc, err := store.Load(hash)
	if err != nil {
		return fmt.Errorf("load service: %w", err)
	}

	// Stop if running
	pid, err := devport.HolderPID(store.LockPath(hash))
	if err != nil {
		return fmt.Errorf("check lock holder: %w", err)
	}
	if pid != 0 {
		fmt.Printf("stopping pid %d...\n", pid)
		syscall.Kill(pid, syscall.SIGTERM)
	}

	// Tear down Tailscale if enabled
	if svc.Tailnet {
		fmt.Printf("clearing tailscale service svc:%s...\n", svc.HashID)
		if err := devport.TailscaleClear(svc.HashID); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: tailscale clear failed: %v\n", err)
		}
	}

	// Delete files
	if err := store.Delete(hash); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	fmt.Printf("removed service %s (port %d)\n", svc.Hash, svc.Port)
	return nil
}
