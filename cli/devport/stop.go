package main

import (
	"fmt"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <target>",
	Short: "Stop a running service",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	svc, err := store.Resolve(args[0])
	if err != nil {
		return err
	}

	pid, err := devport.HolderPID(store.LockPath(svc.Hash))
	if err != nil {
		return fmt.Errorf("check lock holder: %w", err)
	}
	if pid == 0 {
		fmt.Println("service is not running")
		return nil
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("kill %d: %w", pid, err)
	}
	fmt.Printf("sent SIGTERM to pid %d\n", pid)
	return nil
}
