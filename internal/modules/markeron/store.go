package markeron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// markeronStore 持久化 MarkerOn 少量偏好：
// 位置 <dataDir>/markeron.json，仅存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）。
// 与 frpcStore 同款原子写（tmp+rename），损坏容忍（解析失败按空配置继续，不阻断模块）。
type markeronStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 true：随 Hanxi 退出一起关闭；false：独立运行
}

type markeronConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newMarkeronStore(dir string) *markeronStore {
	s := &markeronStore{filePath: filepath.Join(dir, "markeron.json"), followOnExit: true}
	_ = s.load()
	return s
}

func (s *markeronStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg markeronConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return nil
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit == nil || *cfg.FollowOnExit
	return nil
}

func (s *markeronStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(markeronConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit}, "", "  ")
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

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *markeronStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *markeronStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *markeronStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *markeronStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
