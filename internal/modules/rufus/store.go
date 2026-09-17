package rufus

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// rufusStore 持久化 Rufus 少量偏好：
// 位置 <stateDir>/rufus.json，仅存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）
// 与 followOnExit 开关。原子写（tmp+rename）与
// 损坏容忍（解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type rufusStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type rufusConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newRufusStore(dir string) *rufusStore {
	s := &rufusStore{filePath: filepath.Join(dir, "rufus.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *rufusStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg rufusConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *rufusStore) saveLocked() error {
	return jsonstore.Save(s.filePath, rufusConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *rufusStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *rufusStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *rufusStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *rufusStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
