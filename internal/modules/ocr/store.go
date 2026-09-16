package ocr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// defaultListenPort 与上游 hanxi-ocr v0.2.0 内置默认端口一致。
const defaultListenPort = 53120

// ocrStore 持久化 hanxi-ocr 服务托管偏好：
// 位置 <dataDir>/ocr.json，存 exePath（空 = 自动发现同级 ../hanxi-ocr/）、
// listenPort（服务端口）、followOnExit（随 Hanxi 退出联动，默认 true——
// 托管拉起的服务不该在宿主退出后变孤儿）。
// 与 ddnsgoStore 同款原子写（tmp+rename），损坏容忍（解析失败按默认值继续）。
type ocrStore struct {
	filePath     string
	mu           sync.RWMutex
	exePath      string
	followOnExit bool
	autoCopy     bool
	listenPort   int
}

type ocrConfig struct {
	ExePath      string `json:"exePath"`
	FollowOnExit *bool  `json:"followOnExit"`
	AutoCopy     *bool  `json:"autoCopy"`
	ListenPort   *int   `json:"listenPort"`
}

func newOcrStore(dir string) *ocrStore {
	s := &ocrStore{
		filePath:     filepath.Join(dir, "ocr.json"),
		followOnExit: true,
		autoCopy:     true, // 截屏识别后自动把文字复制进剪贴板（悬浮卡仍可手动复制）
		listenPort:   defaultListenPort,
	}
	_ = s.load()
	return s
}

func (s *ocrStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg ocrConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，字段自然兜底默认值
		return nil
	}
	s.exePath = strings.TrimSpace(cfg.ExePath)
	if cfg.FollowOnExit != nil {
		s.followOnExit = *cfg.FollowOnExit
	}
	if cfg.AutoCopy != nil {
		s.autoCopy = *cfg.AutoCopy
	}
	if cfg.ListenPort != nil && *cfg.ListenPort >= 1024 && *cfg.ListenPort <= 65535 {
		s.listenPort = *cfg.ListenPort
	}
	return nil
}

func (s *ocrStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(ocrConfig{
		ExePath:      s.exePath,
		FollowOnExit: &s.followOnExit,
		AutoCopy:     &s.autoCopy,
		ListenPort:   &s.listenPort,
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

// GetExePath 返回设定路径（空字符串 = 自动发现）。
func (s *ocrStore) GetExePath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exePath
}

// SetExePath 设定 hanxi-ocr.exe 路径并落盘；""=恢复自动发现；
// 非空必须是存在的 .exe 文件（校验中文报错，仿 ddnsgo validateListenPort）。
func (s *ocrStore) SetExePath(path string) error {
	path = strings.TrimSpace(path)
	if path != "" {
		if !strings.EqualFold(filepath.Ext(path), ".exe") {
			return fmt.Errorf("服务路径必须以 .exe 结尾，当前选择的是 %q", filepath.Base(path))
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return fmt.Errorf("服务程序不存在：%s", path)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exePath = path
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *ocrStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘（下次启动/停止托管实例时生效）。
func (s *ocrStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// GetAutoCopy 返回「截屏识别后自动复制文字」开关（默认 true）。
func (s *ocrStore) GetAutoCopy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoCopy
}

// SetAutoCopy 设定自动复制开关并落盘（下一次截屏识别生效）。
func (s *ocrStore) SetAutoCopy(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoCopy = v
	return s.saveLocked()
}

// GetListenPort 返回服务端口（默认 53120）。
func (s *ocrStore) GetListenPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listenPort
}

// SetListenPort 设定端口并立即落盘（校验范围 1024~65535）。
func (s *ocrStore) SetListenPort(port int) error {
	if err := validateListenPort(port); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenPort = port
	return s.saveLocked()
}

// validateListenPort 端口合法性：1024~65535（避开特权端口段），非法即拒。
func validateListenPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("端口需在 1024~65535 范围内，当前 %d", port)
	}
	return nil
}
