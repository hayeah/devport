package main

import (
	"fmt"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var sigupCmd = &cobra.Command{
	Use:   "sigup <hash-prefix>",
	Short: "Send SIGHUP to a running service's supervisor (restarts child in-place)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSigup,
}

func init() {
	rootCmd.AddCommand(sigupCmd)
}

func runSigup(cmd *cobra.Command, args []string) error {
	hash, err := store.ResolvePrefix(args[0])
	if err != nil {
		return err
	}

	pid, err := devport.HolderPID(store.LockPath(hash))
	if err != nil {
		return fmt.Errorf("check lock holder: %w", err)
	}
	if pid == 0 {
		return fmt.Errorf("service %s is not running", args[0])
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("kill %d: %w", pid, err)
	}
	fmt.Printf("sent SIGHUP to pid %d\n", pid)
	return nil
}
