package devport

import (
	"fmt"
	"strconv"
	"strings"
)

// Resolve looks up a service by target, trying in order: key, port, hash prefix.
func (s *Store) Resolve(target string) (*Service, error) {
	services, err := s.All()
	if err != nil {
		return nil, err
	}

	// Try key match
	for _, svc := range services {
		if svc.Key != "" && svc.Key == target {
			return svc, nil
		}
	}

	// Try hash prefix
	hash, prefixErr := s.resolvePrefix(target, services)
	if prefixErr == nil {
		return s.Load(hash)
	}

	// Try port match
	if port, err := strconv.Atoi(target); err == nil {
		for _, svc := range services {
			if svc.Port == port {
				return svc, nil
			}
		}
	}

	return nil, prefixErr
}

// ResolvePrefix resolves a hash prefix to a full hash.
// Returns an error if the prefix is ambiguous or not found.
func (s *Store) ResolvePrefix(prefix string) (string, error) {
	services, err := s.All()
	if err != nil {
		return "", err
	}
	return s.resolvePrefix(prefix, services)
}

func (s *Store) resolvePrefix(prefix string, services []*Service) (string, error) {
	if len(prefix) < 3 {
		return "", fmt.Errorf("prefix too short (minimum 3 characters)")
	}

	var matches []string
	for _, svc := range services {
		if strings.HasPrefix(svc.Hash, prefix) {
			matches = append(matches, svc.Hash)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no service matching %q", prefix)
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
