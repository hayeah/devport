package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var (
	flagKey     string
	flagPortEnv string
	flagTailnet bool
	flagNoPort  bool
)

var runCmd = &cobra.Command{
	Use:   "run [flags] -- <cmd> [args...]",
	Short: "Start a supervised dev service",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRun,
}

func init() {
	runCmd.Flags().StringVar(&flagKey, "key", "", "Named key for the service (otherwise derived from cwd+cmd)")
	runCmd.Flags().StringVar(&flagPortEnv, "port-env", "PORT", "Environment variable name for the port")
	runCmd.Flags().BoolVar(&flagTailnet, "tailnet", false, "Expose service via Tailscale")
	runCmd.Flags().BoolVar(&flagNoPort, "no-port", false, "Do not allocate a port for this service")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	hash := devport.ComputeHash(flagKey, cwd, args)

	// Step 1: Try to acquire identity lock
	identityLock := devport.NewFileLock(store.LockPath(hash))
	acquired, err := identityLock.TryLock()
	if err != nil {
		return fmt.Errorf("identity lock: %w", err)
	}
	if !acquired {
		// Already running — print info and exit
		svc, err := store.Load(hash)
		if err != nil {
			return fmt.Errorf("service running but can't load metadata: %w", err)
		}
		return printServiceJSON(svc)
	}
	// identityLock held for the lifetime of this process

	// Step 2: Load or register
	svc, err := store.Load(hash)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// New service — register under global lock
		svc, err = registerService(hash, cwd, args)
		if err != nil {
			return err
		}
	} else if flagTailnet && !svc.Tailnet {
		// Existing service, --tailnet requested but not yet enabled
		fmt.Fprintf(os.Stderr, "devport: enabling tailnet for %s...\n", svc.HashID)
		if err := devport.TailscaleUp(svc.HashID, svc.Port); err != nil {
			return fmt.Errorf("tailscale up: %w", err)
		}
		svc.Tailnet = true
		if err := store.Save(svc); err != nil {
			return err
		}
	}

	// Print service info
	if err := printServiceJSON(svc); err != nil {
		return err
	}

	// Step 3: Run supervisor
	env := os.Environ()
	if !flagNoPort {
		env = append(env, fmt.Sprintf("%s=%d", flagPortEnv, svc.Port))
	}

	supervisor := devport.NewSupervisor(devport.SupervisorConfig{
		CMD: args,
		CWD: cwd,
		Env: env,
		OnLastUp: func() {
			svc.LastUp = time.Now()
			store.Save(svc)
		},
	})

	return supervisor.Run()
}

func registerService(hash, cwd string, args []string) (*devport.Service, error) {
	regLock := devport.NewFileLock(store.RegisterLockPath())
	if err := regLock.Lock(); err != nil {
		return nil, fmt.Errorf("registration lock: %w", err)
	}
	defer regLock.Unlock()

	all, err := store.All()
	if err != nil {
		return nil, err
	}

	var port int
	if !flagNoPort {
		port, err = devport.AllocatePort(all)
		if err != nil {
			return nil, err
		}
	}

	// Compute shortest unique prefix
	var allHashes []string
	for _, s := range all {
		allHashes = append(allHashes, s.Hash)
	}
	hashID := devport.ShortestUniquePrefix(hash, allHashes)

	svc := &devport.Service{
		Hash:    hash,
		HashID:  hashID,
		Key:     flagKey,
		Port:    port,
		NoPort:  flagNoPort,
		Tailnet: flagTailnet,
		CWD:     cwd,
		CMD:     args,
		Env:     os.Environ(),
		LastUp:  time.Now(),
	}

	if flagTailnet {
		fmt.Fprintf(os.Stderr, "devport: enabling tailnet for %s...\n", svc.HashID)
		if err := devport.TailscaleUp(svc.HashID, port); err != nil {
			return nil, fmt.Errorf("tailscale up: %w", err)
		}
	}

	if err := store.Save(svc); err != nil {
		return nil, err
	}

	return svc, nil
}

func printServiceJSON(svc *devport.Service) error {
	data, err := json.MarshalIndent(svc, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
