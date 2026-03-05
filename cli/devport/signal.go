package main

import (
	"fmt"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var flagSignal int

var signalCmd = &cobra.Command{
	Use:   "signal <target>",
	Short: "Send a signal to a running service's supervisor (default: SIGHUP)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSignal,
}

func init() {
	signalCmd.Flags().IntVarP(&flagSignal, "signal", "s", int(syscall.SIGHUP), "Signal number to send")
	rootCmd.AddCommand(signalCmd)
}

func runSignal(cmd *cobra.Command, args []string) error {
	svc, err := store.Resolve(args[0])
	if err != nil {
		return err
	}

	supervisorPID, err := devport.HolderPID(store.LockPath(svc.Hash))
	if err != nil {
		return fmt.Errorf("check lock holder: %w", err)
	}
	if supervisorPID == 0 {
		return fmt.Errorf("service %s is not running", args[0])
	}

	childPID, err := devport.ChildPID(supervisorPID)
	if err != nil {
		return fmt.Errorf("find child process: %w", err)
	}
	if childPID == 0 {
		return fmt.Errorf("service %s has no child process (still starting?)", args[0])
	}

	sig := syscall.Signal(flagSignal)
	if err := syscall.Kill(childPID, sig); err != nil {
		return fmt.Errorf("kill %d: %w", childPID, err)
	}
	fmt.Printf("sent signal %d to pid %d\n", flagSignal, childPID)
	return nil
}
