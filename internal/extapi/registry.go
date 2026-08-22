package extapi

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
)

// StateStorage 状态持久化抽象接口
type StateStorage interface {
	IsModuleEnabled(moduleId string, defaultEnabled bool) bool
	SetModuleEnabled(moduleId string, enabled bool) error
}

// ModuleWrapper 包装具体模块与其运行时状态
type ModuleWrapper struct {
	Module      Module
	Enabled     bool
	initialized bool
	mu          sync.Mutex
}

// Registry 管理内建扩展的注册、生命周期与启用状态。
type Registry struct {
	mu      sync.RWMutex
	modules map[string]*ModuleWrapper
	store   StateStorage
}

func NewRegistry(store StateStorage) *Registry {
	return &Registry{
		modules: make(map[string]*ModuleWrapper),
		store:   store,
	}
}

// Register 注册一个或多个扩展（此时仅注册元数据，不分配运行时重资源）。
func (r *Registry) Register(exts ...Module) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range exts {
		info := e.Info()
		id := info.ID
		if id == "" {
			return fmt.Errorf("registry: extension with empty id")
		}
		if _, dup := r.modules[id]; dup {
			return fmt.Errorf("registry: duplicate extension id %q", id)
		}

		// 优先从持久化 store 读取状态，默认 true
		enabled := true
		if r.store != nil {
			enabled = r.store.IsModuleEnabled(id, true)
		}

		r.modules[id] = &ModuleWrapper{
			Module:      e,
			Enabled:     enabled,
			initialized: false,
		}
	}
	return nil
}

// EnsureActive 确保指定模块已完成懒初始化。在模块页面进入或业务接口调用前统一调用。
func (r *Registry) EnsureActive(moduleID string) error {
	r.mu.RLock()
	wrapper, ok := r.modules[moduleID]
	r.mu.RUnlock()

	if !ok || !wrapper.Enabled {
		return nil
	}

	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()

	if !wrapper.initialized {
		if err := wrapper.Module.OnInit(context.Background()); err != nil {
			return fmt.Errorf("registry: init module %q failed: %w", moduleID, err)
		}
		wrapper.initialized = true
	}
	return nil
}

// List 返回全部扩展元信息（含启用状态与初始化状态）。
func (r *Registry) List() []ModuleInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ModuleInfo, 0, len(r.modules))
	for _, w := range r.modules {
		info := w.Module.Info()
		info.Enabled = w.Enabled
		info.Initialized = w.initialized
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetEnabledNavs 返回所有已启用模块的导航条目，按 Order 全局排序。
func (r *Registry) GetEnabledNavs() []NavEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []NavEntry
	for _, w := range r.modules {
		if !w.Enabled {
			continue
		}
		out = append(out, w.Module.Nav()...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// EnabledServices 返回所有模块的 wails service 用于静态注册与路由绑定。
func (r *Registry) EnabledServices() []Service {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []Service
	for _, w := range r.modules {
		out = append(out, w.Module.Services()...)
	}
	return out
}

// IsEnabled 查询扩展启用状态。
func (r *Registry) IsEnabled(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if w, ok := r.modules[id]; ok {
		return w.Enabled
	}
	return false
}

// IsActive 查询模块是否已被懒加载激活。
func (r *Registry) IsActive(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if w, ok := r.modules[id]; ok {
		return w.initialized
	}
	return false
}

// SetEnabled 启停扩展。停用时立即触发 OnDestroy 销毁内部资源并向操作系统归还内存。
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	wrapper, ok := r.modules[id]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("registry: unknown extension %q", id)
	}

	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()

	if !enabled && wrapper.initialized {
		// 1. 调用模块自身析构
		_ = wrapper.Module.OnDestroy()
		wrapper.initialized = false

		// 2. 主动触发 Go 垃圾回收并将空闲虚拟内存立即交还操作系统
		go func() {
			runtime.GC()
			debug.FreeOSMemory()
		}()
	}
	wrapper.Enabled = enabled

	if r.store != nil {
		return r.store.SetModuleEnabled(id, enabled)
	}
	return nil
}

// ShutdownAll 应用退出时清理所有已初始化的模块
func (r *Registry) ShutdownAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, w := range r.modules {
		w.mu.Lock()
		if w.initialized {
			_ = w.Module.OnDestroy()
			w.initialized = false
		}
		w.mu.Unlock()
	}
}
