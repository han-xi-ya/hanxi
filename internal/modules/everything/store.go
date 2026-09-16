package everything

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// everythingStore 持久化 Everything 模块少量偏好：
// 位置 <dataDir>/everything.json，存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）
// 与 followOnExit（是否随 Hanxi 退出一起关闭自有实例）。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type everythingStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type everythingConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newEverythingStore(dir string) *everythingStore {
	s := &everythingStore{filePath: filepath.Join(dir, "everything.json")}
	_ = s.load()
	return s
}

func (s *everythingStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg everythingConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *everythingStore) saveLocked() error {
	return jsonstore.Save(s.filePath, everythingConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *everythingStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *everythingStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *everythingStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *everythingStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
