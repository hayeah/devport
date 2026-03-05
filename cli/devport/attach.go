package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach [target]",
	Short: "Attach to a running service's tmux session",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAttach,
}

func init() {
	rootCmd.AddCommand(attachCmd)
}

func runAttach(cmd *cobra.Command, args []string) error {
	var target string // tmux target: "devport" or "devport:<window>"

	if len(args) == 0 {
		// No target given — pick via fzf
		window, err := fzfSelectWindow()
		if err != nil {
			return err
		}
		target = "devport:" + window
	} else {
		svc, err := store.Resolve(args[0])
		if err != nil {
			return err
		}
		target = "devport:" + svc.TmuxWindow()
	}

	// Check if target exists
	if err := exec.Command("tmux", "has-session", "-t", target).Run(); err != nil {
		return fmt.Errorf("no tmux window %q (is it running via 'devport start'?)", target)
	}

	// If already inside tmux, switch client to the target; otherwise attach
	var tmuxArgs []string
	if os.Getenv("TMUX") != "" {
		tmuxArgs = []string{"switch-client", "-t", target}
	} else {
		tmuxArgs = []string{"attach-session", "-t", target}
	}

	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}

	// Replace current process with tmux (no subprocess overhead)
	return syscall.Exec(tmux, append([]string{"tmux"}, tmuxArgs...), os.Environ())
}

// fzfSelectWindow lists running devport service windows, pipes them to fzf,
// and returns the selected window name.
func fzfSelectWindow() (string, error) {
	if exec.Command("tmux", "has-session", "-t", "devport").Run() != nil {
		return "", fmt.Errorf("no devport tmux session (start a service with 'devport start' first)")
	}

	services, err := store.All()
	if err != nil {
		return "", err
	}

	// Index services by window name for label lookup
	byWindow := make(map[string]*devport.Service)
	for _, svc := range services {
		byWindow[svc.TmuxWindow()] = svc
	}

	// List actual windows in the devport session
	out, err := exec.Command("tmux", "list-windows", "-t", "devport", "-F", "#{window_name}").Output()
	if err != nil {
		return "", fmt.Errorf("list windows: %w", err)
	}

	var lines []string
	for _, window := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		window = strings.TrimSpace(window)
		if window == "" {
			continue
		}
		label := window
		if svc, ok := byWindow[window]; ok && svc.Key != "" {
			label = svc.Key
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", label, window))
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("no windows in devport session")
	}

	fzf := exec.Command("fzf", "--with-nth=1", "--delimiter=\t", "--prompt=attach> ")
	fzf.Stdin = bytes.NewBufferString(strings.Join(lines, "\n"))
	fzf.Stderr = os.Stderr
	selected, err := fzf.Output()
	if err != nil {
		return "", fmt.Errorf("fzf: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(selected)), "\t", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected fzf output: %q", selected)
	}
	return parts[1], nil
}
