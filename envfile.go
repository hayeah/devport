package devport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// ExpandTilde replaces a leading ~ with the user's home directory.
func ExpandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}

// LoadEnvFiles reads dotenv files in order and returns merged KEY=VALUE pairs.
// Later files override earlier ones. Paths are tilde-expanded.
func LoadEnvFiles(paths []string) (map[string]string, error) {
	merged := make(map[string]string)
	for _, p := range paths {
		expanded, err := ExpandTilde(p)
		if err != nil {
			return nil, fmt.Errorf("expand %s: %w", p, err)
		}
		m, err := godotenv.Read(expanded)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged, nil
}
