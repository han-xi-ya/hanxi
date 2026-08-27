package bcu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// bcuStore 持久化 BCU 少量偏好：
// 位置 <dataDir>/bcu.json，仅存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）。
// 与各模块 store 同款原子写（tmp+rename），损坏容忍（解析失败按空配置继续，不阻断模块）。
type bcuStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
}

type bcuConfig struct {
	ActiveVersion string `json:"activeVersion"`
}

func newBCUStore(dir string) *bcuStore {
	s := &bcuStore{filePath: filepath.Join(dir, "bcu.json")}
	_ = s.load()
	return s
}

func (s *bcuStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg bcuConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return nil
	}
	s.activeVersion = cfg.ActiveVersion
	return nil
}

func (s *bcuStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(bcuConfig{ActiveVersion: s.activeVersion}, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmp, bytes, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *bcuStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *bcuStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}
