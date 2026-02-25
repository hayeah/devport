package devport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// TailscaleServe registers a Tailscale service for the given hashID and port.
func TailscaleServe(hashID string, port int) error {
	svcName := "svc:" + hashID
	target := fmt.Sprintf("http://localhost:%d", port)
	cmd := exec.Command("tailscale", "serve", "--service", svcName, target)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tailscale serve: %w", err)
	}
	return nil
}

// TailscaleClear removes a Tailscale service registration.
func TailscaleClear(hashID string) error {
	svcName := "svc:" + hashID
	cmd := exec.Command("tailscale", "serve", "clear", svcName)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tailscale serve clear: %w", err)
	}
	return nil
}

// tailscaleSelfStatus holds the parsed output of `tailscale status --self --json`.
type tailscaleSelfStatus struct {
	Self struct {
		ID string `json:"ID"`
	} `json:"Self"`
	MagicDNSSuffix string `json:"MagicDNSSuffix"`
	CurrentTailnet struct {
		Name string `json:"Name"`
	} `json:"CurrentTailnet"`
}

func tailscaleStatus() (*tailscaleSelfStatus, error) {
	out, err := exec.Command("tailscale", "status", "--self", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	var status tailscaleSelfStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	return &status, nil
}

// TailscaleDeviceID returns the current device's Tailscale node ID.
func TailscaleDeviceID() (string, error) {
	status, err := tailscaleStatus()
	if err != nil {
		return "", err
	}
	if status.Self.ID == "" {
		return "", fmt.Errorf("tailscale status: empty device ID")
	}
	return status.Self.ID, nil
}

// TailscaleTailnet returns the current tailnet name.
func TailscaleTailnet() (string, error) {
	status, err := tailscaleStatus()
	if err != nil {
		return "", err
	}
	if status.CurrentTailnet.Name == "" {
		return "", fmt.Errorf("tailscale status: empty tailnet name")
	}
	return status.CurrentTailnet.Name, nil
}

// TailscaleApprove approves a Tailscale service via the admin API.
func TailscaleApprove(tailnet, serviceName, deviceID, apiKey string) error {
	endpoint := fmt.Sprintf(
		"https://api.tailscale.com/api/v2/tailnet/%s/services/%s/device/%s/approved",
		url.PathEscape(tailnet),
		url.PathEscape(serviceName),
		url.PathEscape(deviceID),
	)
	body := []byte(`{"approved":true}`)

	resp, err := tailscaleAPIRequest(http.MethodPost, endpoint, apiKey, body)
	if err != nil {
		return fmt.Errorf("tailscale approve API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tailscaleAPIError("tailscale approve API", resp)
	}
	return nil
}

func tailscaleAPIRequest(method, endpoint, apiKey string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return http.DefaultClient.Do(req)
}

func tailscaleAPIError(prefix string, resp *http.Response) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("%s: HTTP %d %s", prefix, resp.StatusCode, resp.Status)
	}
	message := strings.TrimSpace(string(data))
	if message == "" {
		return fmt.Errorf("%s: HTTP %d %s", prefix, resp.StatusCode, resp.Status)
	}
	return fmt.Errorf("%s: HTTP %d %s: %s", prefix, resp.StatusCode, resp.Status, message)
}

func tailscaleServiceURL(tailnet, serviceName string) string {
	return fmt.Sprintf(
		"https://api.tailscale.com/api/v2/tailnet/%s/services/%s",
		url.PathEscape(tailnet),
		url.PathEscape(serviceName),
	)
}

func tailscaleGetService(tailnet, serviceName, apiKey string) (bool, error) {
	endpoint := tailscaleServiceURL(tailnet, serviceName)
	resp, err := tailscaleAPIRequest(http.MethodGet, endpoint, apiKey, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	default:
		return false, tailscaleAPIError("tailscale get service API", resp)
	}
}

func tailscaleCreateService(tailnet, serviceName, apiKey string) error {
	endpoint := tailscaleServiceURL(tailnet, serviceName)
	body, err := json.Marshal(map[string]any{
		"name":  serviceName,
		"ports": []string{"tcp:443"},
	})
	if err != nil {
		return err
	}

	resp, err := tailscaleAPIRequest(http.MethodPut, endpoint, apiKey, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tailscaleAPIError("tailscale create service API", resp)
	}
	return nil
}

func TailscaleEnsureService(tailnet, serviceName, apiKey string) error {
	exists, err := tailscaleGetService(tailnet, serviceName, apiKey)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if err := tailscaleCreateService(tailnet, serviceName, apiKey); err != nil {
		return err
	}
	return nil
}

// LoadTailscaleAPIKey reads TAILSCALE_API_KEY from the environment.
func LoadTailscaleAPIKey() (string, error) {
	key := os.Getenv("TAILSCALE_API_KEY")
	if key == "" {
		return "", fmt.Errorf("TAILSCALE_API_KEY environment variable is not set")
	}
	return key, nil
}

// TailscaleUp registers a Tailscale service and approves it via the API.
// This is the full enable flow: serve → get status → load API key → approve.
func TailscaleUp(hashID string, port int) error {
	// Register the service
	if err := TailscaleServe(hashID, port); err != nil {
		return err
	}

	// Get device info
	status, err := tailscaleStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devport: warning: tailscale status unavailable for auto-approve: %v\n", err)
		fmt.Fprintln(os.Stderr, "devport: tailnet service is registered; approve it manually in the Tailscale admin console if required.")
		return nil
	}
	if status.Self.ID == "" {
		fmt.Fprintln(os.Stderr, "devport: warning: tailscale device ID missing; skipping auto-approve.")
		fmt.Fprintln(os.Stderr, "devport: tailnet service is registered; approve it manually in the Tailscale admin console if required.")
		return nil
	}

	suffix := strings.TrimSuffix(status.MagicDNSSuffix, ".")
	if suffix != "" {
		host := fmt.Sprintf("%s.%s", hashID, suffix)
		fmt.Fprintf(
			os.Stderr,
			"devport: hint: if your dev server validates Host headers, allow %s (or .%s), e.g. Vite server.allowedHosts.\n",
			host,
			suffix,
		)
	}

	// Load API key and approve
	apiKey, err := LoadTailscaleAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devport: warning: %v\n", err)
		fmt.Fprintln(os.Stderr, "devport: tailnet service is registered; approve it manually in the Tailscale admin console if required.")
		return nil
	}

	svcName := "svc:" + hashID
	tailnet := "-"
	displayTailnet := strings.TrimSuffix(status.CurrentTailnet.Name, ".")
	if displayTailnet == "" {
		displayTailnet = tailnet
	}

	fmt.Fprintf(os.Stderr, "devport: ensuring service %s exists on tailnet %s...\n", svcName, displayTailnet)
	if err := TailscaleEnsureService(tailnet, svcName, apiKey); err != nil {
		fmt.Fprintf(os.Stderr, "devport: warning: ensure service failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "devport: tailnet service is registered locally; service definition may need manual creation in the Tailscale admin console.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "devport: approving %s on tailnet %s...\n", svcName, displayTailnet)

	if err := TailscaleApprove(tailnet, svcName, status.Self.ID, apiKey); err != nil {
		fmt.Fprintf(os.Stderr, "devport: warning: auto-approve failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "devport: tailnet service is registered; approve it manually in the Tailscale admin console if required.")
		return nil
	}

	return nil
}
