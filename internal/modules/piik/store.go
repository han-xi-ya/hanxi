package piik

import (
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// piikStore 持久化 piik 托管的少量偏好：位置 <stateDir>/piik.json，两字段——
//   - activeVersion：设定使用版本（空字符串 = 未指定，冷启动自动回退最新已装；
//     上游日更风暴下"钉版本"就是靠它钉住，见 metaHints 第 1 条）；
//   - followOnExit：随 Hanxi 退出联动开关（默认 false，家族口径；关态下
//     Hanxi 退出后服务与 8787 监听原样留着继续开播）。
//
// 刻意**不**持久化端口：端口是每次托管启动现场试绑分配的事实值（8787 起被占
// 则 +1），落盘反而制造"账上有值但现场已被他人占走"的假账。运行期那一笔也
// 不在本层——A 线引擎把它记进 Snapshot.Port（本代/最近一代分配端口），本层
// 读那一处，绝不另存副本（两处端口账必漂移，一处为准才诚实）。
//
// 原子写（tmp+rename）与损坏容忍（解析失败按空配置继续，不阻断模块）收口至
// internal/jsonstore，与托管族其余 store 同源。
type piikStore struct {
	filePath      string
	mu            sync.RWMutex
	activeVersion string
	followOnExit  bool // 默认 false：服务独立运行，不随 Hanxi 退出
}

type piikConfig struct {
	ActiveVersion string `json:"activeVersion"`
	FollowOnExit  *bool  `json:"followOnExit"`
}

func newPiikStore(dir string) *piikStore {
	s := &piikStore{filePath: filepath.Join(dir, "piik.json"), followOnExit: false}
	_ = s.load()
	return s
}

func (s *piikStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg piikConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *piikStore) saveLocked() error {
	return jsonstore.Save(s.filePath, piikConfig{ActiveVersion: s.activeVersion, FollowOnExit: &s.followOnExit})
}

// GetActive 返回当前设定版本（空字符串 = 未指定）。
func (s *piikStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘。
func (s *piikStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *piikStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘（生效时机见 service 侧注释：下次启动）。
func (s *piikStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
