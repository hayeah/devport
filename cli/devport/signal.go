package main

import (
	"fmt"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var flagSignal int

var signalCmd = &cobra.Command{
	Use:   "signal <hash-prefix>",
	Short: "Send a signal to a running service's supervisor (default: SIGHUP)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSignal,
}

func init() {
	signalCmd.Flags().IntVarP(&flagSignal, "signal", "s", int(syscall.SIGHUP), "Signal number to send")
	rootCmd.AddCommand(signalCmd)
}

func runSignal(cmd *cobra.Command, args []string) error {
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

	sig := syscall.Signal(flagSignal)
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("kill %d: %w", pid, err)
	}
	fmt.Printf("sent signal %d to pid %d\n", flagSignal, pid)
	return nil
}
