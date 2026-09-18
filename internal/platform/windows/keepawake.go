//go:build windows

package windows

import (
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"sync"

	"hanxi/internal/platform"
)

// SetThreadExecutionState 标志位（winbase.h）。ES_CONTINUOUS 让本次诉求进入
// "粘滞"形态：设置后持续到同一线程下一次调用改判或线程退出为止。
const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
)

var procSetThreadExecutionState = modKernel32.NewProc("SetThreadExecutionState")

// setThreadExecutionState Win32 裸调用层：scope 位并集 → ES 标志。
func setThreadExecutionState(scope platform.KeepAwakeScope) error {
	flags := uintptr(esContinuous)
	if scope&platform.KeepAwakeSystem != 0 {
		flags |= esSystemRequired
	}
	if scope&platform.KeepAwakeDisplay != 0 {
		flags |= esDisplayRequired
	}
	r1, _, err := procSetThreadExecutionState.Call(flags)
	if r1 == 0 {
		return fmt.Errorf("SetThreadExecutionState(0x%x) 调用失败: %w", flags, err)
	}
	return nil
}

// keepAwakeAPI 是 platform.KeepAwakeAPI 的 Windows 实现（引用计数聚合器）。
//
// 为什么必须收拢到唯一 owner 线程：SetThreadExecutionState 的粘滞诉求按
// "调用发生的 OS 线程"记账——线程 A 置位的 ES_DISPLAY_REQUIRED 不会被线程 B
// 后续的 ES_CONTINUOUS 清除（清除只对本线程生效），Go 的 goroutine 又会在
// 任意 M 上漂移。若聚合器直接"谁调用谁 syscall"，多持有人交叠时先 Acquire
// 的线程诉求将永久残留（防休眠泄露），最后持有者撤牌也无法真正放行休眠。
// 因此所有 ES 调用一律投递给一个 LockOSThread 的常驻 owner 协程执行；
// 归零清场后 owner 退出（locked 线程随协程结束被 runtime 回收，系统顺带
// 自动擦除该线程全部残留诉求——双保险），下次 Acquire 再懒起新 owner。
type keepAwakeAPI struct {
	mu        sync.Mutex
	holders   map[string]platform.KeepAwakeScope  // 活跃持有人 → 诉求 scope
	applied   platform.KeepAwakeScope             // owner 线程当前生效的并集
	owner     chan platform.KeepAwakeScope        // 非 nil 表示 owner 在跑
	ack       chan error                          // 与 owner 一问一答（全程持 mu，恒无并发请求）
	ownerDone chan struct{}                       // owner 已 UnlockOSThread 并退出的完成信号
	apply     func(platform.KeepAwakeScope) error // 落点缝：单测注入假实现
}

// NewKeepAwake 返回进程级 Windows 防休眠聚合器（真实 SetThreadExecutionState 落点）。
func NewKeepAwake() platform.KeepAwakeAPI {
	return newKeepAwake(setThreadExecutionState)
}

func newKeepAwake(apply func(platform.KeepAwakeScope) error) *keepAwakeAPI {
	return &keepAwakeAPI{
		holders: make(map[string]platform.KeepAwakeScope),
		apply:   apply,
	}
}

// Acquire 登记/改判持有人诉求并同步系统状态；scope 为 0 视为调用方错误。
func (k *keepAwakeAPI) Acquire(holder string, scope platform.KeepAwakeScope) error {
	if holder == "" {
		return fmt.Errorf("keepawake: 持有人名字不能为空")
	}
	if scope == 0 {
		return fmt.Errorf("keepawake: 持有人 %q 的防休眠范围不能为 0", holder)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	prev, existed := k.holders[holder]
	k.holders[holder] = scope
	if err := k.syncLocked(); err != nil {
		// 落点失败视为"未获得租约"：回滚登记，不留下兑现不了的系统级诉求。
		if existed {
			k.holders[holder] = prev
		} else {
			delete(k.holders, holder)
		}
		return err
	}
	return nil
}

// Release 撤销持有人诉求（未在场幂等）；最后一个持有人离场归零即清系统状态。
// 注意不因"持有人不在场"提前返回：上一次归零清场失败时 applied 仍挂着旧诉求，
// 此处走 syncLocked 补做清场，防止"表已空但系统防休眠泄露"无人兜底。
func (k *keepAwakeAPI) Release(holder string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.holders, holder)
	return k.syncLocked()
}

// Holders 返回当前活跃持有人（字典序，诊断与测试用）。
func (k *keepAwakeAPI) Holders() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]string, 0, len(k.holders))
	for h := range k.holders {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// syncLocked 计算持有人 scope 并集并与 owner 线程对账（调用方必须持 k.mu）。
// 与 owner 的通道往返也在锁内完成：owner 协程从不取 k.mu，无死锁环；
// 一次 syscall 的微秒级持锁可接受，换来"任何时刻至多一个在途改判"的简单性。
func (k *keepAwakeAPI) syncLocked() error {
	desired := platform.KeepAwakeScope(0)
	for _, scope := range k.holders {
		desired |= scope
	}
	if desired == k.applied {
		return nil
	}
	if k.owner == nil {
		req := make(chan platform.KeepAwakeScope)
		ack := make(chan error)
		done := make(chan struct{})
		k.owner, k.ack, k.ownerDone = req, ack, done
		go keepAwakeOwner(k.apply, req, ack, done)
	}
	k.owner <- desired
	err := <-k.ack
	if err != nil {
		slog.Warn("keepawake: 防休眠状态更新失败", "scope", desired, "err", err)
		// owner 线程上一次失败调用的系统后果不可证明；立即关停 owner，让
		// UnlockOSThread/线程退场替本周期兜底清理。尤其首次置位失败时，若仍
		// 留着 owner，下一次 Acquire 会复用一条可能残留半成功状态的线程。
		k.shutdownOwnerLocked()
		return err // applied 保持旧值，下次状态变化会启动全新 owner 重试
	}
	k.applied = desired
	if desired == 0 {
		// owner 收到归零诉求后自行退场；等它完成 UnlockOSThread 再清引用，
		// 保证下一周期不会与旧 owner 的线程清场交叠。
		<-k.ownerDone
		k.owner, k.ack, k.ownerDone = nil, nil, nil
	}
	return nil
}

// shutdownOwnerLocked 关闭当前 owner 并等待其 UnlockOSThread 退场（调用方持 k.mu）。
// owner 从不获取 k.mu，因此等待不形成锁环。
func (k *keepAwakeAPI) shutdownOwnerLocked() {
	if k.owner == nil {
		return
	}
	close(k.owner)
	<-k.ownerDone
	k.owner, k.ack, k.ownerDone = nil, nil, nil
}

// keepAwakeOwner 独占 OS 线程执行全部 ES 调用；收到归零诉求并清偿，或请求通道
// 被关闭时退场。defer 确保所有路径都 UnlockOSThread 并通知等待者。
func keepAwakeOwner(apply func(platform.KeepAwakeScope) error, req <-chan platform.KeepAwakeScope, ack chan<- error, done chan<- struct{}) {
	runtime.LockOSThread()
	defer func() {
		runtime.UnlockOSThread()
		close(done)
	}()
	for scope := range req {
		err := apply(scope)
		ack <- err
		if err == nil && scope == 0 {
			return
		}
	}
}
