package devport

import (
	"fmt"
	"sort"
	"time"
)

const (
	PortMin        = 19000
	PortMax        = 19999
	ReclaimAfter   = 30 * 24 * time.Hour
)

// AllocatePort picks the lowest unused port in [PortMin, PortMax].
// If all ports are taken, it reclaims the stalest port past ReclaimAfter.
func AllocatePort(services []*Service) (int, error) {
	used := make(map[int]bool, len(services))
	for _, svc := range services {
		used[svc.Port] = true
	}

	// Prefer the lowest fresh port
	for p := PortMin; p <= PortMax; p++ {
		if !used[p] {
			return p, nil
		}
	}

	// All ports taken — try reclaiming stale ones
	now := time.Now()
	var stale []*Service
	for _, svc := range services {
		if now.Sub(svc.LastUp) > ReclaimAfter {
			stale = append(stale, svc)
		}
	}
	if len(stale) == 0 {
		return 0, fmt.Errorf("no available ports in range %d-%d", PortMin, PortMax)
	}

	// Reclaim the stalest
	sort.Slice(stale, func(i, j int) bool {
		return stale[i].LastUp.Before(stale[j].LastUp)
	})
	return stale[0].Port, nil
}
