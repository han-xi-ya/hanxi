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
//     取色、留言板等热键功能直接复用本注册表（全仓唯一实例，装配根构造后交接，
//     见 internal/app/hotkeys.go），不再各自触碰 Wails。
package hotkey

import (
	"errors"
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
	pending map[string]struct{} // 回滚注销失败的新键；后续同槽操作先重试清理
}

// NewRegistry 以指定底层构造注册表。
func NewRegistry(backend Backend) *Registry {
	return &Registry{backend: backend, slots: make(map[string]slot)}
}

// Bind 为槽位落实期望态。完整事务在 r.mu 内串行，避免同槽并发把底层系统态
// 与槽位账本拆成两套历史：enabled=false 解绑；enabled=true 绑到 accel。
//   - 同槽同键且系统确在绑定 → 幂等 no-op；
//   - 换键先注册新键，再注销旧键；旧键注销失败会反向注销新键并保留旧槽位；
//   - 反向注销也失败时把新键记为 pending，后续同槽操作先清理它；
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
	defer r.mu.Unlock()

	cur, had := r.slots[name]
	if err := r.cleanupPendingLocked(name, &cur); err != nil {
		r.slots[name] = cur
		return err
	}
	if had {
		r.slots[name] = cur
	}
	if had && sameAccel(cur.accel, accel) && r.backend.IsRegistered(accel) {
		cur.handler = handler
		r.slots[name] = cur
		return nil
	}

	if err := r.backend.Register(accel, handler); err != nil {
		return userError(accel, err)
	}
	if !had || sameAccel(cur.accel, accel) {
		r.slots[name] = slot{accel: accel, handler: handler}
		return nil
	}

	if err := r.backend.Unregister(cur.accel); err != nil && r.backend.IsRegistered(cur.accel) {
		primary := fmt.Errorf("热键「%s」旧键 %s 注销失败，换绑未生效：%w", name, cur.accel, err)
		if rollbackErr := r.backend.Unregister(accel); rollbackErr != nil && r.backend.IsRegistered(accel) {
			if cur.pending == nil {
				cur.pending = make(map[string]struct{})
			}
			cur.pending[accel] = struct{}{}
			r.slots[name] = cur
			return errors.Join(primary, fmt.Errorf("热键「%s」补偿注销新键 %s 失败，已保留待重试凭据：%w", name, accel, rollbackErr))
		}
		r.slots[name] = cur
		return primary
	}

	r.slots[name] = slot{accel: accel, handler: handler}
	return nil
}

// cleanupPendingLocked 清理此前补偿失败遗留的新键。调用方持有 r.mu。
func (r *Registry) cleanupPendingLocked(name string, cur *slot) error {
	for accel := range cur.pending {
		if !r.backend.IsRegistered(accel) {
			delete(cur.pending, accel)
			continue
		}
		if err := r.backend.Unregister(accel); err != nil && r.backend.IsRegistered(accel) {
			return fmt.Errorf("热键「%s」清理待回滚键 %s 失败：%w", name, accel, err)
		}
		delete(cur.pending, accel)
	}
	return nil
}

// Unbind 解绑槽位（幂等：未绑定静默成功）。注销失败时不删槽位账本，保留
// accel 作为二次重试凭据；只有系统确认未注册或注销成功后才清账。
func (r *Registry) Unbind(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cur, had := r.slots[name]
	if !had {
		return nil
	}
	if err := r.cleanupPendingLocked(name, &cur); err != nil {
		r.slots[name] = cur
		return err
	}
	if !r.backend.IsRegistered(cur.accel) {
		delete(r.slots, name)
		return nil
	}
	if err := r.backend.Unregister(cur.accel); err != nil && r.backend.IsRegistered(cur.accel) {
		r.slots[name] = cur
		return fmt.Errorf("热键「%s」注销失败：%w", name, err)
	}
	delete(r.slots, name)
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
