// Package hotkey 提供全局快捷键的集中封装（跨模块平台能力）：维护"命名槽位 →
// 键位 → 回调"的注册表，底层系统绑定委托给 Wails v3 的 GlobalShortcutManager
// （Windows 侧即 RegisterHotKey，无需手写键盘钩子；回调由 Wails 派发在独立
// goroutine 上，不会阻塞消息泵）。
//
// 设计边界（克制）：
//   - 槽位化：每个功能一个 name（如 "ocr/snip-clipboard"），改键 = 先注册新键、
//     成功后才注销旧键——注册失败保持旧绑定原样（冲突降级，绝不 panic/静默丢键）；
//   - 不吞错误：OS 拒绝（典型为组合键已被其他程序抢占）转中文可读 error 上抛，
//     由设置页红字呈现；
//   - 不做监听/轮询/队列：一次性触发即回调，语义与 RegisterHotKey 对齐。
//     取色、留言板等后续热键功能直接复用本注册表，不再各自触碰 Wails。
package hotkey

import (
	"fmt"
	"strings"
	"sync"
)

// Backend 底层全局热键能力。*application.GlobalShortcutManager 天然满足；
// 单测注入桩件验证注册表语义（无需真实系统热键）。
type Backend interface {
	Register(accelerator string, callback func()) error
	Unregister(accelerator string) error
	IsRegistered(accelerator string) bool
}

// Registry 命名槽位的热键注册表（并发安全）。
type Registry struct {
	backend Backend

	mu    sync.Mutex
	slots map[string]slot
}

type slot struct {
	accel   string
	handler func()
}

// NewRegistry 以指定底层构造注册表。
func NewRegistry(backend Backend) *Registry {
	return &Registry{backend: backend, slots: make(map[string]slot)}
}

// Bind 为槽位落实期望态：enabled=false 解绑；enabled=true 绑到 accel。
//   - 同槽同键且系统确在绑定 → 幂等 no-op（启动期 pending 回滚产生的"记账在、
//     系统没绑"状态会被视为需要重试，走下方重绑路径）；
//   - 换键先注册新键，成功后才注销旧键；新键被占用则保持旧绑定、返回中文错误；
//   - handler 必须非 nil（解绑除外）。
func (r *Registry) Bind(name, accel string, enabled bool, handler func()) error {
	if name == "" {
		return fmt.Errorf("热键槽位名不能为空")
	}
	if !enabled {
		return r.Unbind(name)
	}
	accel = strings.TrimSpace(accel)
	if accel == "" {
		return fmt.Errorf("热键「%s」尚未设置键位", name)
	}
	if handler == nil {
		return fmt.Errorf("热键「%s」缺少回调", name)
	}

	r.mu.Lock()
	cur, had := r.slots[name]
	r.mu.Unlock()
	if had && sameAccel(cur.accel, accel) && r.backend.IsRegistered(accel) {
		return nil // 已是期望态
	}

	if err := r.backend.Register(accel, handler); err != nil {
		return userError(accel, err) // 失败保旧：slots 原样，旧键仍在
	}
	r.mu.Lock()
	r.slots[name] = slot{accel: accel, handler: handler}
	r.mu.Unlock()
	if had && !sameAccel(cur.accel, accel) {
		// 新键到手后才释放旧键；记账已落新键，注销失败如实报错但不再回退
		//（OS 侧旧键即使用户再按也只触同一 handler，无越权风险，重试可清）
		if err := r.backend.Unregister(cur.accel); err != nil && r.backend.IsRegistered(cur.accel) {
			return fmt.Errorf("热键「%s」已换到 %s，但旧键 %s 注销失败：%w", name, accel, cur.accel, err)
		}
	}
	return nil
}

// Unbind 解绑槽位（幂等：未绑定静默成功）。启动期 OS 绑定回滚（记账在、系统
// 没绑）的残账同样直接抹除。
func (r *Registry) Unbind(name string) error {
	r.mu.Lock()
	cur, had := r.slots[name]
	if had {
		delete(r.slots, name)
	}
	r.mu.Unlock()
	if !had {
		return nil
	}
	if !r.backend.IsRegistered(cur.accel) {
		return nil // 启动回滚/已被注销：记账抹除即达成
	}
	if err := r.backend.Unregister(cur.accel); err != nil {
		return fmt.Errorf("热键「%s」注销失败：%w", name, err)
	}
	return nil
}

// Registered 槽位是否真实绑定在系统上（以底层为准，不信任自家记账——
// 启动期 flushPending 冲突回滚后此处如实报 false）。未绑定槽位报 false。
func (r *Registry) Registered(name string) bool {
	r.mu.Lock()
	cur, had := r.slots[name]
	r.mu.Unlock()
	return had && r.backend.IsRegistered(cur.accel)
}

// Accel 槽位当前记账户上的键位（未绑定返回 ""；供设置页回显）。
func (r *Registry) Accel(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.slots[name].accel
}

func sameAccel(a, b string) bool { return strings.EqualFold(a, b) }

// userError 把底层注册失败翻译成用户可动作的中文话术（PLAN_CLIPBOARD 风险 1：
// 抢键不静默、可改键）。Wails/Win32 的冲突错误串为 "already registered (possibly
// by another application)"。
func userError(accel string, err error) error {
	if strings.Contains(err.Error(), "already registered") {
		return fmt.Errorf("组合键 %s 已被占用（可能被其他软件抢注），请到设置页改键", accel)
	}
	return fmt.Errorf("全局热键 %s 注册失败：%w", accel, err)
}
