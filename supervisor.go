package devport

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	GracefulTimeout   = 5 * time.Second
	LastUpInterval    = 30 * time.Second
	CrashBackoffMax   = 30 * time.Second
	CrashBackoffReset = 5 * time.Second // if child runs longer than this, reset backoff
)

type SupervisorConfig struct {
	CMD        []string
	CWD        string
	Env        []string
	Port       int    // port to check for binding after start
	TmuxTarget string // tmux target (e.g. "devport:myservice") for pane capture
	OnLastUp   func()
	OnError    func(errMsg string) // called when child exits non-zero
	OnStarted  func()             // called when port is confirmed bound
}

type Supervisor struct {
	config    SupervisorConfig
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan error
	startedAt time.Time
}

func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	return &Supervisor{
		config: cfg,
		done:   make(chan error),
	}
}

// Run starts the child and blocks until SIGINT/SIGTERM.
func (s *Supervisor) Run() error {
	if err := s.startChild(); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGTSTP, syscall.SIGHUP)

	ticker := time.NewTicker(LastUpInterval)
	defer ticker.Stop()

	// Initial last_up update
	if s.config.OnLastUp != nil {
		s.config.OnLastUp()
	}

	// Check port binding after initial start
	s.checkPortStarted()

	backoff := time.Second

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				s.killChild()
				return nil
			case syscall.SIGTSTP, syscall.SIGHUP:
				backoff = time.Second
				s.restartChild()
				s.checkPortStarted()
			}
		case waitErr := <-s.done:
			// Child exited on its own — restart with backoff
			s.mu.Lock()
			uptime := time.Since(s.startedAt)
			s.mu.Unlock()

			if waitErr != nil {
				s.reportError(waitErr)
			}

			if uptime > CrashBackoffReset {
				backoff = time.Second
			}

			fmt.Fprintf(os.Stderr, "devport: child exited (uptime %s), restarting in %s...\n", uptime.Round(time.Millisecond), backoff)
			time.Sleep(backoff)

			if backoff < CrashBackoffMax {
				backoff *= 2
				if backoff > CrashBackoffMax {
					backoff = CrashBackoffMax
				}
			}

			if err := s.startChild(); err != nil {
				return fmt.Errorf("restart failed: %w", err)
			}
			s.checkPortStarted()
		case <-ticker.C:
			if s.config.OnLastUp != nil {
				s.config.OnLastUp()
			}
		}
	}
}

// ExpandEnv replaces $VAR and ${VAR} references in args using the given env slice.
func ExpandEnv(args []string, env []string) []string {
	// Build a map from the env slice
	envMap := make(map[string]string, len(env))
	for _, e := range env {
		for i := range e {
			if e[i] == '=' {
				envMap[e[:i]] = e[i+1:]
				break
			}
		}
	}

	expanded := make([]string, len(args))
	for i, arg := range args {
		expanded[i] = os.Expand(arg, func(key string) string {
			if v, ok := envMap[key]; ok {
				return v
			}
			return ""
		})
	}
	return expanded
}

func (s *Supervisor) startChild() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Expand env vars in command args
	expandedCMD := ExpandEnv(s.config.CMD, s.config.Env)

	cmd := exec.Command(expandedCMD[0], expandedCMD[1:]...)
	cmd.Dir = s.config.CWD
	cmd.Env = s.config.Env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %v: %w", expandedCMD, err)
	}

	s.cmd = cmd
	s.startedAt = time.Now()
	s.done = make(chan error)
	done := s.done // capture for goroutine — ensures it sends to THIS generation's channel

	go func() {
		waitErr := cmd.Wait()
		select {
		case done <- waitErr:
		default:
		}
	}()

	return nil
}

func (s *Supervisor) killChild() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd == nil || s.cmd.Process == nil {
		return
	}

	// Kill the entire process group
	pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGTERM)
	}

	// Drain the done channel so we don't trigger a restart
	go func() {
		select {
		case <-s.done: //nolint:errcheck
		case <-time.After(GracefulTimeout):
		}
	}()

	// Wait for graceful shutdown, then force kill
	timer := time.AfterFunc(GracefulTimeout, func() {
		if pgid > 0 {
			syscall.Kill(-pgid, syscall.SIGKILL)
		}
	})
	defer timer.Stop()

	s.cmd.Wait()
	s.cmd = nil
}

func (s *Supervisor) restartChild() {
	fmt.Fprintf(os.Stderr, "devport: restarting child...\n")
	s.killChild()
	if err := s.startChild(); err != nil {
		fmt.Fprintf(os.Stderr, "devport: restart failed: %v\n", err)
	}
}

// reportError captures the tmux pane content and calls OnError.
func (s *Supervisor) reportError(waitErr error) {
	if s.config.OnError == nil {
		return
	}

	var paneOutput string
	if s.config.TmuxTarget != "" {
		out, err := exec.Command("tmux", "capture-pane", "-p", "-S", "-", "-t", s.config.TmuxTarget).Output()
		if err == nil {
			paneOutput = StripANSI(strings.TrimSpace(string(out)))
		}
	}

	errMsg := waitErr.Error()
	if paneOutput != "" {
		errMsg = paneOutput
	}
	s.config.OnError(errMsg)
}

const (
	PortCheckTimeout  = 30 * time.Second
	PortCheckInterval = 250 * time.Millisecond
)

// checkPortStarted polls until the configured port is bound, then calls OnStarted.
func (s *Supervisor) checkPortStarted() {
	if s.config.Port == 0 || s.config.OnStarted == nil {
		return
	}

	go func() {
		addr := fmt.Sprintf("127.0.0.1:%d", s.config.Port)
		deadline := time.Now().Add(PortCheckTimeout)
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				fmt.Fprintf(os.Stderr, "devport: port %d is up\n", s.config.Port)
				s.config.OnStarted()
				return
			}
			time.Sleep(PortCheckInterval)
		}
		fmt.Fprintf(os.Stderr, "devport: port %d did not bind within %s\n", s.config.Port, PortCheckTimeout)
	}()
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[.*?[mGKHJP]`)

// StripANSI removes ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}
