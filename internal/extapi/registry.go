package extapi

import (
	"fmt"
	"sort"
	"sync"
)

// StateStorage 状态持久化抽象接口
type StateStorage interface {
	IsModuleEnabled(moduleId string, defaultEnabled bool) bool
	SetModuleEnabled(moduleId string, enabled bool) error
}

// Registry 管理内建扩展的注册与启用状态。
type Registry struct {
	mu      sync.Mutex
	byID    map[string]Module
	enabled map[string]bool
	store   StateStorage
}

func NewRegistry(store StateStorage) *Registry {
	return &Registry{
		byID:    map[string]Module{},
		enabled: map[string]bool{},
		store:   store,
	}
}

// Register 注册一个或多个扩展；从 store 读取历史启用状态（若有）。
func (r *Registry) Register(exts ...Module) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range exts {
		id := e.Info().ID
		if id == "" {
			return fmt.Errorf("registry: extension with empty id")
		}
		if _, dup := r.byID[id]; dup {
			return fmt.Errorf("registry: duplicate extension id %q", id)
		}
		r.byID[id] = e

		// 优先从持久化 store 读取状态，默认 true
		enabled := true
		if r.store != nil {
			enabled = r.store.IsModuleEnabled(id, true)
		}
		r.enabled[id] = enabled
	}
	return nil
}

// List 返回全部扩展元信息（含启用状态）。
func (r *Registry) List() []ModuleInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ModuleInfo, 0, len(r.byID))
	for _, e := range r.byID {
		info := e.Info()
		info.Enabled = r.enabled[info.ID]
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetEnabledNavs 返回所有已启用模块的导航条目，按 Order 全局排序。
func (r *Registry) GetEnabledNavs() []NavEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []NavEntry
	for _, e := range r.byID {
		info := e.Info()
		if !r.enabled[info.ID] {
			continue
		}
		out = append(out, e.Nav()...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// EnabledServices 返回所有已启用扩展的 wails service。
func (r *Registry) EnabledServices() []Service {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Service
	for _, e := range r.byID {
		info := e.Info()
		if !r.enabled[info.ID] {
			continue
		}
		out = append(out, e.Services()...)
	}
	return out
}

// IsEnabled 查询扩展启用状态。
func (r *Registry) IsEnabled(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled[id]
}

// SetEnabled 启停扩展并同步持久化；未知 ID 返回错误。
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[id]; !ok {
		return fmt.Errorf("registry: unknown extension %q", id)
	}
	r.enabled[id] = enabled

	if r.store != nil {
		return r.store.SetModuleEnabled(id, enabled)
	}
	return nil
}
