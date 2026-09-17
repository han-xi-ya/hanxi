package bili23

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// bili23Store 持久化 Bili23 少量偏好：
// 位置 <stateDir>/bili23.json，仅存 activeVersion（空串 = 未指定，冷启动自动回退最新已装）
// 与 followOnExit（是否随 Hanxi 退出一起关闭自有实例）。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
// 注意：本文件只存 Hanxi 侧的托管偏好，Bili23 自身配置恒在 %APPDATA%\Bili23 Downloader\，
// 由上游管理，Hanxi 不碰。
type bili23Store struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行（解除 Job 联动），不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type bili23Config struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newBili23Store(dir string) *bili23Store {
	s := &bili23Store{filePath: filepath.Join(dir, "bili23.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *bili23Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg bili23Config
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *bili23Store) saveLocked() error {
	return jsonstore.Save(s.filePath, bili23Config{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *bili23Store) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *bili23Store) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *bili23Store) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘（下次启动生效）。
func (s *bili23Store) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
