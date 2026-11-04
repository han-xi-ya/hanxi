package omp

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// ompStore 持久化 omp 模块偏好：位置 <stateDir>/omp.json，仅存 activeVersion
// （空字符串 = 未指定，展示与 ResolveExe 自动回退最新已装）。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至
// internal/jsonstore。本模块不做进程托管，无 followOnExit 开关（形态即 douzy
// 的"仅版本管理"家族，多一层进程生命周期反而误导）。
type ompStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
}

type ompConfig struct {
	ActiveVersion string `json:"activeVersion"`
}

func newOmpStore(dir string) *ompStore {
	s := &ompStore{filePath: filepath.Join(dir, "omp.json")}
	_ = s.load()
	return s
}

func (s *ompStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg ompConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	return nil
}

func (s *ompStore) saveLocked() error {
	return jsonstore.Save(s.filePath, ompConfig{ActiveVersion: s.activeVersion})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *ompStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定激活版本并立即落盘。
func (s *ompStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}
