package snipaste

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type snipasteStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
}

type snipasteConfig struct {
	ActiveVersion string `json:"activeVersion"`
}

func newSnipasteStore(dir string) *snipasteStore {
	s := &snipasteStore{filePath: filepath.Join(dir, "snipaste.json")}
	_ = s.load()
	return s
}

func (s *snipasteStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg snipasteConfig
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	s.activeVersion = cfg.ActiveVersion
	return nil
}

func (s *snipasteStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

func (s *snipasteStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

func (s *snipasteStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snipasteConfig{ActiveVersion: s.activeVersion}, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
