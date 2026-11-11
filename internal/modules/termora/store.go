package termora

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// termoraStore 持久化 Termora 少量偏好：位置 <stateDir>/termora.json，
// 仅存 activeVersion（空串 = 未指定，冷启动回退最新已装）与 followOnExit。
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）
// 收口至 internal/jsonstore（guoheview 同型）。
type termoraStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：独立运行，不随 Hanxi 退出一起关闭托管实例
	// portableData 数据模式（2026-11-11 机主拍板做成开关）：
	//   false（默认）=共享：不建 data\ 激活器，Termora 用上游默认数据根 ~/.termora，
	//     与机主自己装的 Termora 同一份——升级/多版本不再"打开是空的"；
	//   true=独立：启动前幂等补齐 exe 同级 data\，该版本自带一份隔离数据（要干净试验场
	//     或"导入件接着用它自带数据"时用）。
	// 默认值刻意取"共享"：旧实现无条件建 data\，与 gonavi 包注释声称的"与 Termora/
	// WindTerm 同口径：不动用户数据"相反，实测造成"从 Hanxi 打开没有数据、删版本连带
	// 删数据"（TROUBLESHOOTING #151）。
	portableData bool
}

type termoraConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
	PortableData  *bool  `json:"portableData"` // 缺省（含老配置无此字段）= 共享
}

func newTermoraStore(dir string) *termoraStore {
	s := &termoraStore{filePath: filepath.Join(dir, "termora.json"), followOnExit: false, portableData: false}
	_ = s.load()
	return s
}

func (s *termoraStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg termoraConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	s.portableData = cfg.PortableData != nil && *cfg.PortableData
	return nil
}

func (s *termoraStore) saveLocked() error {
	return jsonstore.Save(s.filePath, termoraConfig{
		ActiveVersion: s.activeVersion,
		FollowOnExit:  &s.followOnExit,
		PortableData:  &s.portableData,
	})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *termoraStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *termoraStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *termoraStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *termoraStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}

// GetPortableData 返回数据模式开关（true=独立随版本 data\；false=共享 ~/.termora）。
func (s *termoraStore) GetPortableData() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.portableData
}

// SetPortableData 设定数据模式并立即落盘（下一次启动时由 applyDataMode 兑现）。
func (s *termoraStore) SetPortableData(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.portableData = b
	return s.saveLocked()
}
