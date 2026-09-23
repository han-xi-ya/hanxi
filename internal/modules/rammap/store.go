package rammap

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// rammapStore 持久化 RAMMap 少量偏好：<stateDir>/rammap.json，仅存
// activeVersion（空串 = 未指定，冷启动回退最新已装日期版）与 followOnExit。
// 原子写与损坏容忍收口 internal/jsonstore（家族同型）。
type rammapStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：观察工具随开随关，联动退出仅作可选项
}

type rammapConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newRammapStore(dir string) *rammapStore {
	s := &rammapStore{filePath: filepath.Join(dir, "rammap.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *rammapStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg rammapConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *rammapStore) saveLocked() error {
	return jsonstore.Save(s.filePath, rammapConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空串 = 未指定）。
func (s *rammapStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *rammapStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *rammapStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *rammapStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
