package papertodo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// papertodoStore 持久化 PaperTodo 少量偏好：
// 位置 <dataDir>/papertodo.json，存 variant（下载运行库变体）与 followOnExit。
// 单版本覆盖布局无 activeVersion 概念（托管目录至多一版）。
// 与 ccswitchStore 同款原子写（tmp+rename），损坏容忍（解析失败按默认配置继续，不阻断模块）。
type papertodoStore struct {
	filePath     string
	mu           sync.RWMutex
	variant      string // version.VariantSelfContained / version.VariantNoRuntime（默认前者）
	followOnExit bool   // 默认 true：随 Hanxi 退出一起关闭；false：独立运行
}

type papertodoConfig struct {
	Variant      string `json:"variant"`
	FollowOnExit *bool  `json:"followOnExit"`
}

func newPapertodoStore(dir string) *papertodoStore {
	s := &papertodoStore{filePath: filepath.Join(dir, "papertodo.json"), variant: defaultVariant, followOnExit: true}
	_ = s.load()
	return s
}

func (s *papertodoStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg papertodoConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，variant 自然回退默认 self-contained
		return nil
	}
	if validVariant(cfg.Variant) {
		s.variant = cfg.Variant
	}
	s.followOnExit = cfg.FollowOnExit == nil || *cfg.FollowOnExit
	return nil
}

func (s *papertodoStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(papertodoConfig{Variant: s.variant, FollowOnExit: &s.followOnExit}, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmp, bytes, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
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
