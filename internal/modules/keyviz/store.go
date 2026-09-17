package keyviz

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// keyvizStore 持久化 Keyviz 少量偏好：
// 位置 <stateDir>/keyviz.json，仅存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）
// 与 followOnExit。原子写（tmp+rename）与损坏容忍
// （解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
// 注意与 Keyviz 自身的用户配置（%APPDATA%\org.keyviz\store.json）无关——
// 托管侧只存"用哪个版本、是否随 Hanxi 退出"两件事。
type keyvizStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type keyvizConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newKeyvizStore(dir string) *keyvizStore {
	s := &keyvizStore{filePath: filepath.Join(dir, "keyviz.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *keyvizStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg keyvizConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *keyvizStore) saveLocked() error {
	return jsonstore.Save(s.filePath, keyvizConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *keyvizStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *keyvizStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *keyvizStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *keyvizStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
