// registry_drain_test.go 钉死 ADR-0001 §1.4 的有界 drain 收口纪律（A3 审查）：
//  1. 预算数值冻结：停用 30s、退出 60s（测试可变 seam 只为在 CI 时限内演练超时
//     路径，默认值漂移必须报警——改数须先改 ADR）；
//  2. drain 超时必须落 slog.Error 显式告警，绝不静默：断言用真实日志记录观察面
//     （级别 + 文案 + module 属性），而非私有函数调用；
//  3. 超时后强制收口可观察：SetEnabled/ShutdownAll 在短预算内返回、OnDestroy
//     照常执行、租约最终释放不 panic、投影收敛。
package extapi

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// a3CaptureHandler 最小日志捕获器：记录全部级别的结构化日志供外部事实断言。
type a3CaptureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *a3CaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *a3CaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *a3CaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *a3CaptureHandler) WithGroup(string) slog.Handler      { return h }

// hasError 报告是否出现过级别>=Error、消息含 msgSub、且带 name=value 属性的记录。
func (h *a3CaptureHandler) hasError(msgSub string, attrName, attrValue string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level < slog.LevelError || !strings.Contains(r.Message, msgSub) {
			continue
		}
		if attrName == "" {
			return true
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == attrName && a.Value.String() == attrValue {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// withCapturedLogs 临时接管 slog 默认 logger，返回捕获器与还原函数。
func withCapturedLogs(t *testing.T) (*a3CaptureHandler, func()) {
	t.Helper()
	prev := slog.Default()
	h := &a3CaptureHandler{}
	slog.SetDefault(slog.New(h))
	return h, func() { slog.SetDefault(prev) }
}

// overrideBudget 临时改写 drain 预算（串行单测专用 seam），返回还原函数。
func overrideBudget(t *testing.T, target *time.Duration, short time.Duration) func() {
	t.Helper()
	old := *target
	*target = short
	return func() { *target = old }
}

// TestDrainBudgetFrozen 预算数值即 ADR-0001 §1.4 冻结面：30s/60s。
// 这不是"抄实现"——数值来自 ADR 的外部承诺（停用/退出最长等待），
// 任何改动都必须与 ADR 同步，先由本测试报警。
func TestDrainBudgetFrozen(t *testing.T) {
	if drainBudgetDisable != 30*time.Second {
		t.Errorf("停用 drain 预算 = %v, want 30s（ADR-0001 §1.4 冻结值）", drainBudgetDisable)
	}
	if drainBudgetShutdown != 60*time.Second {
		t.Errorf("退出 drain 预算 = %v, want 60s（ADR-0001 §1.4 冻结值）", drainBudgetShutdown)
	}
}

func TestDisableDrainTimeoutLogsErrorAndForcesConvergence(t *testing.T) {
	defer overrideBudget(t, &drainBudgetDisable, 200*time.Millisecond)()
	logs, restore := withCapturedLogs(t)
	defer restore()

	reg := NewRegistry(nil)
	mod := newRegistryTestModule("demo")
	if err := reg.Register(mod); err != nil {
		t.Fatal(err)
	}
	_, release, err := reg.Acquire("demo")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- reg.SetEnabled("demo", false) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetEnabled(false) = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("停用未按超时预算强制收口（在途 lease 卡死了整条收口路径）")
	}

	// 外部可观察事实 1：超时告警确实落了 Error 级日志且指名模块（绝不静默）。
	if !logs.hasError("停用 drain 超时", "module", "demo") {
		t.Error("drain 超时未见 Error 级告警记录（ADR-0001 §1.4：必须显式告警）")
	}
	// 外部可观察事实 2：强制收口照常完成——OnDestroy 恰好一次、投影已停用。
	if got := mod.destroyCnt.Load(); got != 1 {
		t.Errorf("OnDestroy count = %d, want 1（超时后仍须析构收口）", got)
	}
	st := stateFor(t, reg, "demo")
	if st.Policy != PolicyDisabled {
		t.Errorf("收口后 policy = %s, want disabled", st.Policy)
	}
	// 滞留 lease 归零后不得再触发任何异常。
	release()
	release() // 幂等
}

func TestShutdownAllDrainTimeoutLogsError(t *testing.T) {
	defer overrideBudget(t, &drainBudgetShutdown, 200*time.Millisecond)()
	logs, restore := withCapturedLogs(t)
	defer restore()

	reg := NewRegistry(nil)
	mod := newRegistryTestModule("demo")
	if err := reg.Register(mod); err != nil {
		t.Fatal(err)
	}
	_, release, err := reg.Acquire("demo")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		reg.ShutdownAll()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ShutdownAll 未按超时预算强制收口")
	}

	if !logs.hasError("退出 drain 超时", "", "") {
		t.Error("ShutdownAll drain 超时未见 Error 级告警记录（ADR-0001 §1.4：绝不静默）")
	}
	if got := mod.destroyCnt.Load(); got != 1 {
		t.Errorf("OnDestroy count = %d, want 1", got)
	}
	release()
}
