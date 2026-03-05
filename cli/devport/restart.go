package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <target>",
	Short: "Stop a service and re-launch it in tmux from stored state",
	Args:  cobra.ExactArgs(1),
	RunE:  runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
	svc, err := store.Resolve(args[0])
	if err != nil {
		return err
	}
	hash := svc.Hash

	// Stop if running
	pid, err := devport.HolderPID(store.LockPath(hash))
	if err != nil {
		return fmt.Errorf("check lock holder: %w", err)
	}
	if pid != 0 {
		fmt.Fprintf(os.Stderr, "devport: stopping pid %d...\n", pid)
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("kill %d: %w", pid, err)
		}
		// Wait for lock to be released (up to 10s)
		if err := waitForLock(hash, 10*time.Second); err != nil {
			return err
		}
	}

	// Kill any dead tmux window
	exec.Command("tmux", "kill-window", "-t", "devport:"+svc.TmuxWindow()).Run() //nolint:errcheck

	// Build devport run args from stored state
	devportBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	runArgs := []string{"run"}
	if svc.Key != "" {
		runArgs = append(runArgs, "--key", svc.Key)
	}
	if svc.NoPort {
		runArgs = append(runArgs, "--no-port")
	}
	if svc.Tailnet {
		runArgs = append(runArgs, "--tailnet")
	}
	runArgs = append(runArgs, "--")
	runArgs = append(runArgs, svc.CMD...)

	var tmuxArgs []string
	sessionExists := exec.Command("tmux", "has-session", "-t", "devport").Run() == nil
	if sessionExists {
		tmuxArgs = []string{"new-window", "-t", "devport", "-n", svc.TmuxWindow(), "-c", svc.CWD, "--", devportBin}
	} else {
		tmuxArgs = []string{"new-session", "-d", "-s", "devport", "-n", svc.TmuxWindow(), "-c", svc.CWD, "--", devportBin}
	}
	tmuxArgs = append(tmuxArgs, runArgs...)

	out, err := exec.Command("tmux", tmuxArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux: %w\n%s", err, out)
	}

	// Poll for the service to be live again
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc, err := store.Load(hash)
		if err == nil {
			return printServiceJSON(svc)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for service %s to restart", svc.HashID)
}

// waitForLock polls until the identity lock for hash is free, or timeout elapses.
func waitForLock(hash string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		lock := devport.NewFileLock(store.LockPath(hash))
		acquired, err := lock.TryLock()
		if err != nil {
			return fmt.Errorf("probe lock: %w", err)
		}
		if acquired {
			lock.Unlock()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for service to stop")
}
