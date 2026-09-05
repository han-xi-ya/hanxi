package rustdesk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// rustdeskStore 持久化 RustDesk 少量偏好：
// 位置 <dataDir>/rustdesk.json，存 activeVersion（空字符串 = 未指定，冷启动自动
// 回退最新已装）、activeForm（portable/installed，旧配置无此字段按 portable 兼容
// 读取——两形态版本号可同值，必须成对落盘才无歧义）与 followOnExit。
// 与 ccswitchStore 同款原子写（tmp+rename），损坏容忍。
type rustdeskStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	activeForm    string // version 为空时恒为空
	followOnExit  bool   // 默认 true：随 Hanxi 退出一起关闭；false：独立运行
}

type rustdeskConfig struct {
	ActiveVersion string `json:"activeVersion"`
	ActiveForm    string `json:"activeForm,omitempty"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newRustDeskStore(dir string) *rustdeskStore {
	s := &rustdeskStore{filePath: filepath.Join(dir, "rustdesk.json"), followOnExit: true}
	_ = s.load()
	return s
}

func (s *rustdeskStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg rustdeskConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return nil
	}
	s.activeVersion = strings.TrimSpace(cfg.ActiveVersion)
	s.activeForm = strings.TrimSpace(cfg.ActiveForm)
	if s.activeVersion == "" {
		s.activeForm = "" // 成对不变式：无版本则无形态
	} else if s.activeForm == "" {
		s.activeForm = "portable" // 旧配置兼容：形态字段缺省 = 便携
	}
	s.followOnExit = cfg.FollowOnExit == nil || *cfg.FollowOnExit
	return nil
}

func (s *rustdeskStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(rustdeskConfig{ActiveVersion: s.activeVersion, ActiveForm: s.activeForm, FollowOnExit: &s.followOnExit}, "", "  ")
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

// GetActive 返回当前设定版本与形态（version 为空字符串 = 未指定，form 随之为空）。
func (s *rustdeskStore) GetActive() (version, form string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion, s.activeForm
}

// SetActive 设定使用版本与形态并立即落盘（version 传空即清空设定）。
func (s *rustdeskStore) SetActive(version, form string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	if version == "" {
		s.activeForm = ""
	} else {
		s.activeForm = form
	}
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *rustdeskStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *rustdeskStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
