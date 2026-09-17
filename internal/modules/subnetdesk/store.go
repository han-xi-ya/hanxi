package subnetdesk

import (
	"path/filepath"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
)

// subnetdeskStore 持久化 SubnetDesk 少量偏好：
// 位置 <stateDir>/subnetdesk.json，存 activeVersion（空字符串 = 未指定，冷启动自动
// 回退最新已装）、activeForm（portable/installed，旧配置无此字段按 portable 兼容
// 读取——两形态版本号可同值，必须成对落盘才无歧义）与 followOnExit。
// 原子写（tmp+rename）与损坏容忍收口至 internal/jsonstore。
type subnetdeskStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	activeForm    string // version 为空时恒为空
	followOnExit  bool   // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type subnetdeskConfig struct {
	ActiveVersion string `json:"activeVersion"`
	ActiveForm    string `json:"activeForm,omitempty"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newSubnetDeskStore(dir string) *subnetdeskStore {
	s := &subnetdeskStore{filePath: filepath.Join(dir, "subnetdesk.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *subnetdeskStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg subnetdeskConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = strings.TrimSpace(cfg.ActiveVersion)
	s.activeForm = strings.TrimSpace(cfg.ActiveForm)
	if s.activeVersion == "" {
		s.activeForm = "" // 成对不变式：无版本则无形态
	} else if s.activeForm == "" {
		s.activeForm = "portable" // 旧配置兼容：形态字段缺省 = 便携
	}
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *subnetdeskStore) saveLocked() error {
	return jsonstore.Save(s.filePath, subnetdeskConfig{ActiveVersion: s.activeVersion, ActiveForm: s.activeForm, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本与形态（version 为空字符串 = 未指定，form 随之为空）。
func (s *subnetdeskStore) GetActive() (version, form string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion, s.activeForm
}

// SetActive 设定使用版本与形态并立即落盘（version 传空即清空设定）。
func (s *subnetdeskStore) SetActive(version, form string) error {
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

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *subnetdeskStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *subnetdeskStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
