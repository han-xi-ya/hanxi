package ddnsgo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// defaultListenPort 与上游 -l 默认端口一致，接管既有使用习惯；
// 实际绑定地址恒加 127.0.0.1 前缀（仅回环，面板不外露局域网）。
const defaultListenPort = 9876

// ddnsgoStore 持久化 ddns-go 托管偏好：
// 位置 <dataDir>/ddnsgo.json，存 activeVersion（空 = 未指定，冷启动回退最新已装）、
// listenPort（web 监听端口）、followOnExit（随 Hanxi 退出开关联动）。
// 与 ccswitchStore 同款原子写（tmp+rename），损坏容忍（解析失败按默认值继续）。
type ddnsgoStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 true：随 Hanxi 退出一起关闭；false：独立运行
	listenPort    int
}

type ddnsgoConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
	ListenPort    *int   `json:"listenPort"`
}

func newDdnsgoStore(dir string) *ddnsgoStore {
	s := &ddnsgoStore{
		filePath:     filepath.Join(dir, "ddnsgo.json"),
		followOnExit: true,
		listenPort:   defaultListenPort,
	}
	_ = s.load()
	return s
}

func (s *ddnsgoStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg ddnsgoConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，字段自然兜底默认值
		return nil
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit == nil || *cfg.FollowOnExit
	if cfg.ListenPort != nil && *cfg.ListenPort >= 1024 && *cfg.ListenPort <= 65535 {
		s.listenPort = *cfg.ListenPort
	}
	return nil
}

func (s *ddnsgoStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(ddnsgoConfig{
		ActiveVersion: s.activeVersion,
		FollowOnExit:  &s.followOnExit,
		ListenPort:    &s.listenPort,
	}, "", "  ")
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
func (s *ddnsgoStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *ddnsgoStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *ddnsgoStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *ddnsgoStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// GetListenPort 返回 web 监听端口（默认 9876）。
func (s *ddnsgoStore) GetListenPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listenPort
}

// SetListenPort 设定端口并立即落盘（校验范围 1024~65535）。
func (s *ddnsgoStore) SetListenPort(port int) error {
	if err := validateListenPort(port); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenPort = port
	return s.saveLocked()
}

// validateListenPort 端口合法性：1024~65535（避开特权端口段，上游首次
// 设置页同规则），非法即拒。
func validateListenPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("端口需在 1024~65535 范围内，当前 %d", port)
	}
	return nil
}
