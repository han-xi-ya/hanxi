package everything

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// everythingStore 持久化 Everything 模块少量偏好：
// 位置 <dataDir>/everything.json，存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）
// 与 followOnExit（是否随 Hanxi 退出一起关闭自有实例）。
// 与 markeronStore 同款原子写（tmp+rename），损坏容忍（解析失败按空配置继续，不阻断模块）。
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

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg everythingConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return nil
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *everythingStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(everythingConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit}, "", "  ")
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
