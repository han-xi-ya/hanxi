package dbx

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// dbxStore 持久化 DBX 少量偏好：
// 位置 <stateDir>/dbx.json，存三字段——
//   - activeVersion：设定使用版本（空字符串 = 未指定，冷启动自动回退最新已装）；
//   - followOnExit：随 Hanxi 退出联动开关（默认 false，家族口径）；
//   - dataDirInjected：自有实例是否至少完成过一次带 DBX_DATA_DIR 注入的托管
//     启动（GetStatus 投影与 metaHints 披露依据——true 即"数据已在 Hanxi
//     数据根落位"的事实账，卸载任何托管版本都不该动它）。
//
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type dbxStore struct {
	filePath        string
	mu              sync.RWMutex
	activeVersion   string
	followOnExit    bool // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
	dataDirInjected bool // 注入式托管启动成功发生过的事实账（一次即恒真）
}

type dbxConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
	// DataDirInjected 用 *bool 区分"从未落键"与"显式 false"：语义等价取
	// "非 nil 且为 true"，落键 true 后不因重载丢步。
	DataDirInjected *bool `json:"dataDirInjected"`
}

func newDBXStore(dir string) *dbxStore {
	s := &dbxStore{filePath: filepath.Join(dir, "dbx.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *dbxStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg dbxConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	s.dataDirInjected = cfg.DataDirInjected != nil && *cfg.DataDirInjected
	return nil
}

func (s *dbxStore) saveLocked() error {
	return jsonstore.Save(s.filePath, dbxConfig{
		ActiveVersion:   s.activeVersion,
		FollowOnExit:    &s.followOnExit,
		DataDirInjected: &s.dataDirInjected,
	})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *dbxStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *dbxStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *dbxStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *dbxStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// GetDataDirInjected 返回注入式托管启动发生过的事实账。
func (s *dbxStore) GetDataDirInjected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dataDirInjected
}

// SetDataDirInjected 记入事实账并落盘（幂等：恒真后重复置真不再写盘）。
func (s *dbxStore) SetDataDirInjected(b bool) error {
	s.mu.Lock()
	if s.dataDirInjected == b {
		s.mu.Unlock()
		return nil
	}
	s.dataDirInjected = b
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}
