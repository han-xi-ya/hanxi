// gate.go 是统一调用门（Wave 3）的注入契约与模块侧复用件。
//
// 裁决语义（blocked → receipt 已安装 → 懒激活 → enabled/stopping → lease 入账）
// 由 Registry.Acquire 单一实现承载；本文件提供把它交到各模块 service 的管道：
//
//   - Gate：调用门视图（不暴露 wrapper）；
//   - GateAware：可选契约，装配根 Register 时对实现的模块注入 Gate；
//   - LeaseHolder：模块 service 的内嵌复用件，业务方法一行 Enter() 接入门。
//
// 未注入 gate（单元测试、无头进程未接线场景）时 LeaseHolder 放行并返回空 release，
// 保持既有行为兼容；接线完成前后可用旁路矩阵测试穷举验证。
package extapi

import (
	"context"
	"sync"
	"sync/atomic"
)

// Gate 统一调用门：与 Registry.Acquire 同一裁决，业务调用方只见租约。
type Gate interface {
	// Acquire 取得 moduleID 的 operation lease；release 幂等，业务结束必须调用
	// （defer 即可）。停用与退出的 drain 以租约归零为门。
	Acquire(moduleID string) (release func(), err error)
}

// BackgroundLease 是可转移给后台 goroutine 的 operation lease。
// release 幂等；调用方必须把 ownership 交给后台任务，并在任务终态调用 Release。
type BackgroundLease struct {
	ctx     context.Context
	cancel  context.CancelFunc
	release func()
	once    sync.Once
}

// Context 返回该后台操作绑定的取消上下文。
func (l *BackgroundLease) Context() context.Context {
	if l == nil || l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}

// Cancel 只发出后台操作取消信号，不释放 Registry lease；goroutine 收口后仍须 Release。
func (l *BackgroundLease) Cancel() {
	if l != nil && l.cancel != nil {
		l.cancel()
	}
}

// Release 释放后台 lease（幂等），并同步取消其 context。
func (l *BackgroundLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.cancel != nil {
			l.cancel()
		}
		if l.release != nil {
			l.release()
		}
	})
}

// BackgroundGate 可选的后台租约扩展。普通 Gate 实现若未实现该接口，
// LeaseHolder 会退化为只提供可取消 context 的内存 lease（单测/无头模式）。
type BackgroundGate interface {
	Gate
	AcquireBackground(moduleID string, parent context.Context) (*BackgroundLease, error)
}

// GateAware 可选契约：希望被注入调用门的模块在 Module 层实现（转发给内部 service）。
type GateAware interface {
	SetGate(g Gate)
}

// gateView Registry 的 Gate 适配视图。
type gateView struct{ registry *Registry }

func (v gateView) Acquire(moduleID string) (func(), error) {
	_, release, err := v.registry.Acquire(moduleID)
	return release, err
}

func (v gateView) AcquireBackground(moduleID string, parent context.Context) (*BackgroundLease, error) {
	if parent == nil {
		parent = context.Background()
	}
	return v.registry.acquireBackground(moduleID, parent)
}

// Gate 返回该注册表的调用门视图（装配根/模块注入用）。
func (r *Registry) Gate() Gate { return gateView{registry: r} }

// LeaseHolder 模块 service 侧的可重用租约持有器（口径详见
// internal/app/rpc_gate_matrix_test.go 头注释）：
//
//	s.holder = extapi.NewLeaseHolder(ID)   // 构造期
//	release, err := s.holder.Enter()       // 每个业务方法入口
//	if err != nil { return ..., err }      // 带 error 通道：拒则如实上抛
//	defer release()
//
// 纯 void 方法改为 `if err != nil { return }`（拒即早退，不改签名）；
// 仅返回单值无 error 的方法必须扩为 (T, error)，禁止"拒则回空值"的静默消化。
type LeaseHolder struct {
	moduleID string
	gate     atomic.Pointer[Gate]
}

// NewLeaseHolder 创建租约持有器；moduleID 为模块注册键。
func NewLeaseHolder(moduleID string) *LeaseHolder {
	return &LeaseHolder{moduleID: moduleID}
}

// SetGate 注入调用门（装配根经 GateAware 转发，幂等）。
func (h *LeaseHolder) SetGate(g Gate) {
	if g == nil {
		return
	}
	h.gate.Store(&g)
}

// Enter 取得本模块的 operation lease。gate 未注入时放行（返回空 release）。
// 业务方法的三种接法（统一口径，禁止第四种"拒则回空值"的静默消化）：
// 带 error 的方法拒则回传；纯 void 方法拒即早退；单值无 error 的方法必须
// 先扩为 (T, error) 再接门。
func (h *LeaseHolder) Enter() (func(), error) {
	gp := h.gate.Load()
	if gp == nil {
		return func() {}, nil
	}
	return (*gp).Acquire(h.moduleID)
}

// EnterBackground 把模块租约转移给后台任务，并绑定父 context。
// 未注入调用门时返回纯内存 lease，保持单测/无头模式可运行。
func (h *LeaseHolder) EnterBackground(parent context.Context) (*BackgroundLease, error) {
	if parent == nil {
		parent = context.Background()
	}
	gp := h.gate.Load()
	if gp == nil {
		ctx, cancel := context.WithCancel(parent)
		return &BackgroundLease{ctx: ctx, cancel: cancel, release: cancel}, nil
	}
	if bg, ok := (*gp).(BackgroundGate); ok {
		return bg.AcquireBackground(h.moduleID, parent)
	}
	ctx, cancel := context.WithCancel(parent)
	release, err := (*gp).Acquire(h.moduleID)
	if err != nil {
		cancel()
		return nil, err
	}
	return &BackgroundLease{ctx: ctx, cancel: cancel, release: func() {
		release()
	}}, nil
}
