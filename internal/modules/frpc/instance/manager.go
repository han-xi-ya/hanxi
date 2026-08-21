package instance

import (
	"sync"

	"hubkit/internal/platform"
)

// Manager 多实例管理器：projectID → Instance。
// 负责实例的创建/复用、启停、状态与日志查询。实例在项目删除前始终保留
// （含停止后的历史日志，便于回看）。
type Manager struct {
	mu        sync.Mutex
	instances map[string]*Instance
	jobAPI    platform.JobAPI
	onState   func(snapshot Snapshot)
	onLog     func(projectID string, line string)
}

// NewManager 创建实例管理器。
// cb 可省略（nil 时静默丢弃）；runDir 由调用方提供并保证存在。
func NewManager(jobAPI platform.JobAPI, cb Callbacks) *Manager {
	if cb.OnState == nil {
		cb.OnState = func(Snapshot) {}
	}
	if cb.OnLog == nil {
		cb.OnLog = func(string, string) {}
	}
	return &Manager{
		instances: make(map[string]*Instance),
		jobAPI:    jobAPI,
		onState:   cb.OnState,
		onLog:     cb.OnLog,
	}
}

// Start 启动项目实例；若该实例已存在则以新参数重启（复用日志历史）。
func (m *Manager) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	m.mu.Lock()
	in, ok := m.instances[opts.ProjectID]
	if !ok {
		in = newInstance(opts, m.jobAPI, Callbacks{OnState: m.onState, OnLog: m.onLog})
		m.instances[opts.ProjectID] = in
	}
	m.mu.Unlock()
	return in.Start(opts)
}

// Stop 停止项目实例（幂等；未启动时直接返回 nil）。
func (m *Manager) Stop(projectID string) error {
	m.mu.Lock()
	in := m.instances[projectID]
	m.mu.Unlock()
	if in == nil {
		return nil
	}
	return in.Stop()
}

// StopAll 停止全部运行中的实例（应用退出前兜底；正常情况下 JobObject 已覆盖）。
func (m *Manager) StopAll() {
	m.mu.Lock()
	list := make([]*Instance, 0, len(m.instances))
	for _, in := range m.instances {
		list = append(list, in)
	}
	m.mu.Unlock()
	for _, in := range list {
		_ = in.Stop()
	}
}

// Remove 删除实例（项目删除时调用；调用前应先 Stop）。
func (m *Manager) Remove(projectID string) {
	m.mu.Lock()
	delete(m.instances, projectID)
	m.mu.Unlock()
}

// Snapshot 查询实例状态。
func (m *Manager) Snapshot(projectID string) (Snapshot, bool) {
	m.mu.Lock()
	in := m.instances[projectID]
	m.mu.Unlock()
	if in == nil {
		return Snapshot{}, false
	}
	return in.Snapshot(), true
}

// AllSnapshots 返回全部实例状态（含已停止的历史实例）。
func (m *Manager) AllSnapshots() []Snapshot {
	m.mu.Lock()
	list := make([]*Instance, 0, len(m.instances))
	for _, in := range m.instances {
		list = append(list, in)
	}
	m.mu.Unlock()

	snaps := make([]Snapshot, 0, len(list))
	for _, in := range list {
		snaps = append(snaps, in.Snapshot())
	}
	return snaps
}

// Logs 拉取实例最近 n 行日志（n <= 0 时全部）。
func (m *Manager) Logs(projectID string, n int) ([]string, error) {
	m.mu.Lock()
	in := m.instances[projectID]
	m.mu.Unlock()
	if in == nil {
		return nil, nil
	}
	return in.Logs(n), nil
}