package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var (
	startFlagKey     string
	startFlagPortEnv string
	startFlagTailnet bool
	startFlagNoPort  bool
	startFlagFile    string
)

var startCmd = &cobra.Command{
	Use:   "start [flags] -- <cmd> [args...]",
	Short: "Start a supervised dev service in a detached tmux session",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().StringVar(&startFlagKey, "key", "", "Named key for the service (otherwise derived from cwd+cmd)")
	startCmd.Flags().StringVar(&startFlagPortEnv, "port-env", "PORT", "Environment variable name for the port")
	startCmd.Flags().BoolVar(&startFlagTailnet, "tailnet", false, "Expose service via Tailscale")
	startCmd.Flags().BoolVar(&startFlagNoPort, "no-port", false, "Do not allocate a port for this service")
	startCmd.Flags().StringVarP(&startFlagFile, "file", "f", "", "Start services from a devport.yaml file")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	if startFlagFile != "" {
		if len(args) > 0 {
			return fmt.Errorf("cannot use -f with positional arguments")
		}
		return runStartFile(startFlagFile)
	}

	if len(args) == 0 {
		return fmt.Errorf("requires at least 1 arg(s), or use -f <file>")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	p := startParams{
		Key:     startFlagKey,
		Cmd:     args,
		CWD:     cwd,
		PortEnv: startFlagPortEnv,
		NoPort:  startFlagNoPort,
		Tailnet: startFlagTailnet,
	}

	svc, err := startService(p)
	if err != nil {
		return err
	}
	return printServiceJSON(svc)
}

func runStartFile(path string) error {
	specs, err := devport.ParseConfig(path)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	type result struct {
		Key    string
		Port   int
		Hash   string
		NoPort bool
		Err    error
	}

	var results []result

	for _, spec := range specs {
		cmdArgs := splitCommand(spec.Exec)

		portEnv := spec.PortEnv
		if portEnv == "" {
			portEnv = "PORT"
		}

		p := startParams{
			Key:     spec.Key,
			Cmd:     cmdArgs,
			CWD:     cwd,
			PortEnv: portEnv,
			NoPort:  spec.NoPort,
			Tailnet: spec.Tailnet,
		}

		// Load env files if specified
		if len(spec.Env) > 0 {
			envMap, err := devport.LoadEnvFiles(spec.Env)
			if err != nil {
				name := spec.Key
				if name == "" {
					name = spec.Exec
				}
				fmt.Fprintf(os.Stderr, "devport: %s: %v\n", name, err)
				results = append(results, result{Key: name, Err: err})
				continue
			}
			p.ExtraEnv = envMapToSlice(envMap)
		}

		name := spec.Key
		if name == "" {
			name = spec.Exec
		}

		svc, err := startService(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "devport: %s: %v\n", name, err)
			results = append(results, result{Key: name, Err: err})
			continue
		}

		results = append(results, result{
			Key:    name,
			Port:   svc.Port,
			Hash:   svc.Hash,
			NoPort: svc.NoPort,
		})
	}

	// Print summary
	fmt.Println()
	fmt.Printf("%-20s %-6s %-12s %s\n", "SERVICE", "PORT", "HASH", "STATUS")
	for _, r := range results {
		port := fmt.Sprintf("%d", r.Port)
		if r.NoPort {
			port = "—"
		}
		status := "started"
		if r.Err != nil {
			port = "—"
			status = fmt.Sprintf("error: %v", r.Err)
		}
		fmt.Printf("%-20s %-6s %-12s %s\n", r.Key, port, r.Hash, status)
	}

	// Return error if any failed
	for _, r := range results {
		if r.Err != nil {
			return fmt.Errorf("some services failed to start")
		}
	}

	return nil
}

type startParams struct {
	Key      string
	Cmd      []string
	CWD      string
	PortEnv  string
	NoPort   bool
	Tailnet  bool
	ExtraEnv []string // additional KEY=VALUE pairs from env files
}

func startService(p startParams) (*devport.Service, error) {
	hash := devport.ComputeHash(p.Key, p.CWD, p.Cmd)

	windowName := p.Key
	if windowName == "" {
		windowName = hash
	}

	// Check if already running
	identityLock := devport.NewFileLock(store.LockPath(hash))
	acquired, err := identityLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("identity lock: %w", err)
	}
	if !acquired {
		svc, err := store.Load(hash)
		if err != nil {
			return nil, fmt.Errorf("service running but can't load metadata: %w", err)
		}
		return svc, nil
	}
	identityLock.Unlock()

	// Kill any dead window with this name
	exec.Command("tmux", "kill-window", "-t", "devport:"+windowName).Run() //nolint:errcheck

	devportBin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}

	runArgs := []string{"run"}
	if p.Key != "" {
		runArgs = append(runArgs, "--key", p.Key)
	}
	if p.PortEnv != "PORT" {
		runArgs = append(runArgs, "--port-env", p.PortEnv)
	}
	if p.Tailnet {
		runArgs = append(runArgs, "--tailnet")
	}
	if p.NoPort {
		runArgs = append(runArgs, "--no-port")
	}
	runArgs = append(runArgs, "--")
	runArgs = append(runArgs, p.Cmd...)

	var tmuxArgs []string
	sessionExists := exec.Command("tmux", "has-session", "-t", "devport").Run() == nil
	if sessionExists {
		tmuxArgs = []string{"new-window", "-t", "devport", "-n", windowName, "-c", p.CWD, "--", devportBin}
	} else {
		tmuxArgs = []string{"new-session", "-d", "-s", "devport", "-n", windowName, "-c", p.CWD, "--", devportBin}
	}
	tmuxArgs = append(tmuxArgs, runArgs...)

	// Set extra env vars in the tmux environment
	cmd := exec.Command("tmux", tmuxArgs...)
	if len(p.ExtraEnv) > 0 {
		cmd.Env = append(os.Environ(), p.ExtraEnv...)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux: %w\n%s", err, out)
	}

	// Poll for service to register (up to 2s)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc, err := store.Load(hash)
		if err == nil {
			return svc, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("timed out waiting for service %s to start", hash[:8])
}

// splitCommand splits a command string into args, respecting quoted strings.
func splitCommand(cmd string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == ' ' && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func envMapToSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}
