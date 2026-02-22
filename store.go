package devport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	BaseDir string
}

func NewStore(baseDir string) *Store {
	return &Store{BaseDir: baseDir}
}

func (s *Store) servicesDir() string {
	return filepath.Join(s.BaseDir, "services")
}

func (s *Store) locksDir() string {
	return filepath.Join(s.BaseDir, "locks")
}

func (s *Store) EnsureDirs() error {
	if err := os.MkdirAll(s.servicesDir(), 0755); err != nil {
		return err
	}
	return os.MkdirAll(s.locksDir(), 0755)
}

func (s *Store) ServicePath(hash string) string {
	return filepath.Join(s.servicesDir(), hash+".json")
}

func (s *Store) LockPath(hash string) string {
	return filepath.Join(s.locksDir(), hash+".lock")
}

func (s *Store) RegisterLockPath() string {
	return filepath.Join(s.locksDir(), "register.lock")
}

func (s *Store) Load(hash string) (*Service, error) {
	data, err := os.ReadFile(s.ServicePath(hash))
	if err != nil {
		return nil, err
	}
	var svc Service
	if err := json.Unmarshal(data, &svc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", hash, err)
	}
	return &svc, nil
}

func (s *Store) Save(svc *Service) error {
	data, err := json.MarshalIndent(svc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.ServicePath(svc.Hash), data, 0644)
}

func (s *Store) Delete(hash string) error {
	os.Remove(s.LockPath(hash))
	return os.Remove(s.ServicePath(hash))
}

func (s *Store) All() ([]*Service, error) {
	entries, err := os.ReadDir(s.servicesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var services []*Service
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		hash := strings.TrimSuffix(e.Name(), ".json")
		svc, err := s.Load(hash)
		if err != nil {
			continue // skip corrupt files
		}
		services = append(services, svc)
	}
	return services, nil
}

func (s *Store) UsedPorts() (map[int]bool, error) {
	services, err := s.All()
	if err != nil {
		return nil, err
	}
	ports := make(map[int]bool, len(services))
	for _, svc := range services {
		ports[svc.Port] = true
	}
	return ports, nil
}
