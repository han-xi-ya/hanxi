package translucenttb

import (
	"path/filepath"
	"sync"
	"time"

	"hanxi/internal/jsonstore"
)

// translucenttbStore 持久化 TranslucentTB 少量偏好：
// 位置 <stateDir>/translucenttb.json，存 activeVersion（空字符串 = 未指定，冷启动自动回退最新已装）、
// followOnExit 联动开关与最近一笔 AV 崩溃账目（avCrash，从未记账 = 字段缺席）。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type translucenttbStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
	avCrash       *avCrashRecord
}

// avCrashRecord 最近一次 AV(0xC0000005 访问违例) 崩溃账目：Count 跨进程重启
// 累计（复发鉴别的事实底座——"本机已 N 次因此码退出"），Version/At 记最近一笔。
type avCrashRecord struct {
	Code    int       `json:"code"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
	Count   int       `json:"count"`
}

type translucenttbConfig struct {
	ActiveVersion string         `json:"activeVersion"`
	FollowOnExit  *bool          `json:"followOnExit"`
	AVCrash       *avCrashRecord `json:"avCrash,omitempty"`
}

func newTranslucentTBStore(dir string) *translucenttbStore {
	s := &translucenttbStore{filePath: filepath.Join(dir, "translucenttb.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *translucenttbStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg translucenttbConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	s.avCrash = cfg.AVCrash
	return nil
}

func (s *translucenttbStore) saveLocked() error {
	return jsonstore.Save(s.filePath, translucenttbConfig{
		ActiveVersion: s.activeVersion,
		FollowOnExit:  &s.followOnExit,
		AVCrash:       s.avCrash,
	})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *translucenttbStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *translucenttbStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *translucenttbStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *translucenttbStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// RecordAVCrash 记一笔 AV(0xC0000005) 崩溃：Count 在旧账上累计（首次 = 1），
// Version/At/Code 覆盖为最近一笔。落盘失败如实回错（调用侧只降级话术，不断主链）。
func (s *translucenttbStore) RecordAVCrash(code int, version string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := avCrashRecord{Code: code, Version: version, At: at, Count: 1}
	if s.avCrash != nil {
		rec.Count = s.avCrash.Count + 1
	}
	s.avCrash = &rec
	return s.saveLocked()
}

// AVCrashCount 返回本机 AV 崩溃记账累计（无账 = 0；供话术事实引用）。
func (s *translucenttbStore) AVCrashCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.avCrash == nil {
		return 0
	}
	return s.avCrash.Count
}
