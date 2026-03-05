package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var (
	startFlagKey     string
	startFlagPortEnv string
	startFlagTailnet bool
	startFlagNoPort  bool
)

var startCmd = &cobra.Command{
	Use:   "start [flags] -- <cmd> [args...]",
	Short: "Start a supervised dev service in a detached tmux session",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runStart,
}

func init() {
	startCmd.Flags().StringVar(&startFlagKey, "key", "", "Named key for the service (otherwise derived from cwd+cmd)")
	startCmd.Flags().StringVar(&startFlagPortEnv, "port-env", "PORT", "Environment variable name for the port")
	startCmd.Flags().BoolVar(&startFlagTailnet, "tailnet", false, "Expose service via Tailscale")
	startCmd.Flags().BoolVar(&startFlagNoPort, "no-port", false, "Do not allocate a port for this service")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	hash := devport.ComputeHash(startFlagKey, cwd, args)

	// Window name: key if provided, else full hash
	windowName := startFlagKey
	if windowName == "" {
		windowName = hash
	}

	// Check if already running
	identityLock := devport.NewFileLock(store.LockPath(hash))
	acquired, err := identityLock.TryLock()
	if err != nil {
		return fmt.Errorf("identity lock: %w", err)
	}
	if !acquired {
		// Already running
		svc, err := store.Load(hash)
		if err != nil {
			return fmt.Errorf("service running but can't load metadata: %w", err)
		}
		return printServiceJSON(svc)
	}
	// Release probe lock — devport run will re-acquire it inside tmux
	identityLock.Unlock()

	// Kill any dead window with this name in the devport session
	exec.Command("tmux", "kill-window", "-t", "devport:"+windowName).Run() //nolint:errcheck

	// Build the `devport run` invocation
	devportBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	runArgs := []string{"run"}
	if startFlagKey != "" {
		runArgs = append(runArgs, "--key", startFlagKey)
	}
	if startFlagPortEnv != "PORT" {
		runArgs = append(runArgs, "--port-env", startFlagPortEnv)
	}
	if startFlagTailnet {
		runArgs = append(runArgs, "--tailnet")
	}
	if startFlagNoPort {
		runArgs = append(runArgs, "--no-port")
	}
	runArgs = append(runArgs, "--")
	runArgs = append(runArgs, args...)

	// Create session if it doesn't exist, otherwise add a new window
	var tmuxArgs []string
	sessionExists := exec.Command("tmux", "has-session", "-t", "devport").Run() == nil
	if sessionExists {
		tmuxArgs = []string{"new-window", "-t", "devport", "-n", windowName, "-c", cwd, "--", devportBin}
	} else {
		tmuxArgs = []string{"new-session", "-d", "-s", "devport", "-n", windowName, "-c", cwd, "--", devportBin}
	}
	tmuxArgs = append(tmuxArgs, runArgs...)

	out, err := exec.Command("tmux", tmuxArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux: %w\n%s", err, out)
	}

	// Poll for devport run to register the service (up to 2s)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc, err := store.Load(hash)
		if err == nil {
			return printServiceJSON(svc)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for service %s to start", hash[:8])
}
