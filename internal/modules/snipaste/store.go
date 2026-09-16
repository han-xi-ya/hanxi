package snipaste

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// snipasteStore 模块私有小配置（data/snipaste.json）：目前仅持久化"当前使用版本"。
// 读走 RLock；写经 SetActive 串行化并原子落盘（临时文件+Rename，公共核 internal/jsonstore）。
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
	var cfg snipasteConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 保持默认空串
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	return nil
}

// GetActive 返回当前使用版本，未设置返回空串。
func (s *snipasteStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 更新内存中的使用版本并立即原子落盘；传空串表示清除选择。
func (s *snipasteStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

func (s *snipasteStore) saveLocked() error {
	return jsonstore.Save(s.filePath, snipasteConfig{ActiveVersion: s.activeVersion})
}
