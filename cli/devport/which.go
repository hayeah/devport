package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var whichCmd = &cobra.Command{
	Use:   "which <target>",
	Short: "Look up a service by key, port, or hash prefix",
	Args:  cobra.ExactArgs(1),
	RunE:  runWhich,
}

func init() {
	rootCmd.AddCommand(whichCmd)
}

func runWhich(cmd *cobra.Command, args []string) error {
	svc, err := store.Resolve(args[0])
	if err != nil {
		return err
	}

	status := probeStatus(svc)
	info := ServiceInfo{
		Hash:    svc.Hash,
		HashID:  svc.HashID,
		Key:     svc.Key,
		Status:  status,
		Port:    svc.Port,
		NoPort:  svc.NoPort,
		Tailnet: svc.Tailnet,
		CWD:     svc.CWD,
		CMD:     svc.CMD,
		LastUp:  svc.LastUp.Format("2006-01-02T15:04:05Z"),
	}
	if svc.Tailnet {
		info.URL = fmt.Sprintf("https://%s.%s", svc.HashID, tailnetDNSSuffix())
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
