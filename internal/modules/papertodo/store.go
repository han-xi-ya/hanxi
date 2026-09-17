package papertodo

import (
	"fmt"
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// papertodoStore 持久化 PaperTodo 少量偏好：
// 位置 <stateDir>/papertodo.json，存 variant（下载运行库变体）与 followOnExit。
// 单版本覆盖布局无 activeVersion 概念（托管目录至多一版）。
// 原子写（tmp+rename）与损坏容忍（解析失败按默认配置继续，不阻断模块）收口至 internal/jsonstore。
type papertodoStore struct {
	filePath     string
	mu           sync.RWMutex
	variant      string // version.VariantSelfContained / version.VariantNoRuntime（默认前者）
	followOnExit bool   // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type papertodoConfig struct {
	Variant      string `json:"variant"`
	FollowOnExit *bool  `json:"followOnExit"`
}

func newPapertodoStore(dir string) *papertodoStore {
	s := &papertodoStore{filePath: filepath.Join(dir, "papertodo.json"), variant: defaultVariant, followOnExit: false}
	_ = s.load()
	return s
}

func (s *papertodoStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg papertodoConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，variant 自然回退默认 self-contained
		return err
	}
	if validVariant(cfg.Variant) {
		s.variant = cfg.Variant
	}
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *papertodoStore) saveLocked() error {
	return jsonstore.Save(s.filePath, papertodoConfig{Variant: s.variant, FollowOnExit: &s.followOnExit})
}

// GetVariant 返回下载变体偏好。
func (s *papertodoStore) GetVariant() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.variant
}

// SetVariant 设定变体并立即落盘（下次下载生效；不追溯已装版本）。
func (s *papertodoStore) SetVariant(variant string) error {
	if !validVariant(variant) {
		return fmt.Errorf("未知运行库变体: %q", variant)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.variant = variant
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *papertodoStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘（下次启动生效）。
func (s *papertodoStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
