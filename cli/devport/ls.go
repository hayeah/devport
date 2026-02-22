package main

import (
	"encoding/json"
	"fmt"

	"github.com/hayeah/devport"
	"github.com/spf13/cobra"
)

var flagActive bool

type ServiceInfo struct {
	Hash   string   `json:"hash"`
	HashID string   `json:"hashid"`
	Key    string   `json:"key,omitempty"`
	Status string   `json:"status"`
	Port   int      `json:"port"`
	CWD    string   `json:"cwd"`
	CMD    []string `json:"cmd"`
	LastUp string   `json:"last_up"`
}

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List registered services",
	RunE:  runLS,
}

func init() {
	lsCmd.Flags().BoolVar(&flagActive, "active", false, "Only show running services")
	rootCmd.AddCommand(lsCmd)
}

func runLS(cmd *cobra.Command, args []string) error {
	services, err := store.All()
	if err != nil {
		return err
	}

	var infos []ServiceInfo
	for _, svc := range services {
		status := probeStatus(svc)
		if flagActive && status != "running" {
			continue
		}
		infos = append(infos, ServiceInfo{
			Hash:   svc.Hash,
			HashID: svc.HashID,
			Key:    svc.Key,
			Status: status,
			Port:   svc.Port,
			CWD:    svc.CWD,
			CMD:    svc.CMD,
			LastUp: svc.LastUp.Format("2006-01-02T15:04:05Z"),
		})
	}

	if infos == nil {
		infos = []ServiceInfo{}
	}

	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func probeStatus(svc *devport.Service) string {
	lock := devport.NewFileLock(store.LockPath(svc.Hash))
	acquired, err := lock.TryLock()
	if err != nil {
		return "unknown"
	}
	if acquired {
		lock.Unlock()
		return "stopped"
	}
	return "running"
}
