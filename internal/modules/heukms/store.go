package heukms

import (
	"path/filepath"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
)

// heukmsStore 持久化 HEU 托管的少量偏好：位置 <stateDir>/heukms.json，
// 存 activeVersion（空 = 未指定，冷启动自动回退最新已装）与 followOnExit 开关。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口 internal/jsonstore。
//
// activeVersion 一律以**无前缀形态**（64.0.0）入库：上游 tag 本就不带 v，
// version.Manager 的令牌解析也按无前缀形状白名单；RPC 入参容忍前端回传的
// "v64.0.0" 形态，入口即归一，避免账本里两种写法并存导致比较失配。
type heukmsStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 一起关闭
}

type heukmsConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newHeuStore(dir string) *heukmsStore {
	s := &heukmsStore{filePath: filepath.Join(dir, "heukms.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *heukmsStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg heukmsConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = normalizeToken(cfg.ActiveVersion)
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *heukmsStore) saveLocked() error {
	return jsonstore.Save(s.filePath, heukmsConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（无前缀，空字符串 = 未指定）。
func (s *heukmsStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本（入口归一为无前缀形态）并立即落盘。
func (s *heukmsStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = normalizeToken(version)
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *heukmsStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘（下次启动生效）。
func (s *heukmsStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// normalizeToken 版本令牌归一：去首尾空白与可选 v 前缀（"v64.0.0" → "64.0.0"）。
// 本模块的版本串一律用无前缀形态（与上游 tag 同形）。
func normalizeToken(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}
