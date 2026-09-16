package quicklook

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// quicklookStore 持久化 QuickLook 少量偏好：
// 位置 <dataDir>/quicklook.json，仅存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）
// 与 followOnExit。原子写（tmp+rename）与损坏容忍
// （解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
// 注意与 QuickLook 自身的用户配置（便携目录内的 *.config，随 portable.lock 走）无关——
// 托管侧只存"用哪个版本、是否随 Hanxi 退出"两件事。
type quicklookStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立常驻运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type quicklookConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newQuicklookStore(dir string) *quicklookStore {
	s := &quicklookStore{filePath: filepath.Join(dir, "quicklook.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *quicklookStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg quicklookConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *quicklookStore) saveLocked() error {
	return jsonstore.Save(s.filePath, quicklookConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *quicklookStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *quicklookStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *quicklookStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *quicklookStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
