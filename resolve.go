package devport

import (
	"fmt"
	"strings"
)

// ResolvePrefix resolves a hash prefix to a full hash.
// Returns an error if the prefix is ambiguous or not found.
func (s *Store) ResolvePrefix(prefix string) (string, error) {
	if len(prefix) < 3 {
		return "", fmt.Errorf("prefix too short (minimum 3 characters)")
	}

	services, err := s.All()
	if err != nil {
		return "", err
	}

	var matches []string
	for _, svc := range services {
		if strings.HasPrefix(svc.Hash, prefix) {
			matches = append(matches, svc.Hash)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no service matching prefix %q", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous prefix %q matches: %s", prefix, strings.Join(matches, ", "))
	}
}

// ShortestUniquePrefix computes the shortest unique prefix for hash
// among all existing service hashes, with a minimum of 3 characters.
func ShortestUniquePrefix(hash string, allHashes []string) string {
	minLen := 3
	for l := minLen; l < len(hash); l++ {
		prefix := hash[:l]
		unique := true
		for _, h := range allHashes {
			if h == hash {
				continue
			}
			if strings.HasPrefix(h, prefix) {
				unique = false
				break
			}
		}
		if unique {
			return prefix
		}
	}
	return hash
}
