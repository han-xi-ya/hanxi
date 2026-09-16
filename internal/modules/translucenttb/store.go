package translucenttb

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// translucenttbStore 持久化 TranslucentTB 少量偏好：
// 位置 <dataDir>/translucenttb.json，仅存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type translucenttbStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type translucenttbConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newTranslucentTBStore(dir string) *translucenttbStore {
	s := &translucenttbStore{filePath: filepath.Join(dir, "translucenttb.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *translucenttbStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg translucenttbConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *translucenttbStore) saveLocked() error {
	return jsonstore.Save(s.filePath, translucenttbConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *translucenttbStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *translucenttbStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *translucenttbStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *translucenttbStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
