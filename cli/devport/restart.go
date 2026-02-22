package main

import (
	"fmt"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <hash-prefix>",
	Short: "Restart a running service's child process",
	Args:  cobra.ExactArgs(1),
	RunE:  runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
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
	fmt.Printf("sent SIGHUP to pid %d (restart)\n", pid)
	return nil
}
