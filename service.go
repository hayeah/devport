package devport

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	Hash    string    `json:"hash"`
	HashID  string    `json:"hashid"`
	Key     string    `json:"key,omitempty"`
	Port    int       `json:"port"`
	Tailnet bool      `json:"tailnet"`
	CWD     string    `json:"cwd"`
	CMD     []string  `json:"cmd"`
	LastUp  time.Time `json:"last_up"`
}

// ComputeHash produces a 10-char hex hash.
// If key is non-empty, hash the key alone.
// Otherwise hash cwd + "\x00" + join(cmd, "\x00").
func ComputeHash(key, cwd string, cmd []string) string {
	var input string
	if key != "" {
		input = key
	} else {
		input = cwd + "\x00" + strings.Join(cmd, "\x00")
	}
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum[:5]) // 5 bytes = 10 hex chars
}

func (s *Service) MarshalJSON() ([]byte, error) {
	type Alias Service
	return json.Marshal((*Alias)(s))
}

func (s *Service) UnmarshalJSON(data []byte) error {
	type Alias Service
	return json.Unmarshal(data, (*Alias)(s))
}
