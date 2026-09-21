package externalquit

import (
	"context"
	"errors"
	"testing"
	"time"

	"hanxi/internal/platform"
)

// fakeProc platform.ProcessAPI 假实现：进程世务由脚本控制。
type fakeProc struct {
	alive    bool              // Query 是否成功
	info     platform.ProcInfo // Query 返回体
	killErr  error             // KillVerified 结果脚本
	killed   bool
	killTok  platform.VerifyToken
	queryErr error // 可注入非"不存在"类查询失败（当前实现一律按消失处理）
}

func (f *fakeProc) Query(pid uint32) (platform.ProcInfo, error) {
	if !f.alive || f.queryErr != nil {
		return platform.ProcInfo{}, errors.New("process not found")
	}
	return f.info, nil
}

func (f *fakeProc) KillVerified(_ context.Context, token platform.VerifyToken, force bool) error {
	f.killed = true
	f.killTok = token
	if !force {
		return errors.New("fake: 执行器应以 force=true 调用")
	}
	return f.killErr
}

func (f *fakeProc) IsProtected(uint32, platform.ProcInfo) bool { return false }

var _ platform.ProcessAPI = (*fakeProc)(nil)

func aliveToken() (platform.VerifyToken, *fakeProc) {
	start := time.Now()
	tok := platform.VerifyToken{PID: 4242, ExePath: `C:\tools\app.exe`, StartedAt: start}
	return tok, &fakeProc{alive: true, info: platform.ProcInfo{PID: 4242, ExePath: `C:\tools\app.exe`, StartedAt: start}}
}

// fastDeps 压缩等待窗口的公共依赖。
func fastDeps(p *fakeProc) Deps {
	return Deps{Proc: p, Grace: 30 * time.Millisecond, PollInterval: 5 * time.Millisecond}
}

func TestGracefulWins(t *testing.T) {
	tok, p := aliveToken()
	deps := fastDeps(p)
	deps.Graceful = func(ctx context.Context) error {
		p.alive = false // 模拟优雅信号后进程在宽限期内退出
		return nil
	}
	r, err := Quit(context.Background(), tok, PolicyConfirmForce, deps)
	if err != nil || !r.Stopped || r.Forced || r.Method != MethodGraceful {
		t.Fatalf("期望 graceful-request，得 %+v err=%v", r, err)
	}
	if p.killed {
		t.Fatalf("graceful 收口不应触碰强杀: killed=%v", p.killed)
	}
}

func TestForceFreeKillsWithoutConfirm(t *testing.T) {
	tok, p := aliveToken()
	deps := fastDeps(p)
	deps.Graceful = func(context.Context) error { return nil } // 信号发了但进程不走
	deps.Confirm = func(string) bool {
		t.Fatal("force-free 档不得调用 Confirm")
		return false
	}
	r, err := Quit(context.Background(), tok, PolicyForceFree, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stopped || !r.Forced || r.Method != MethodForced || !p.killed {
		t.Fatalf("期望 force-free 直接强杀，得 %+v killed=%v", r, p.killed)
	}
	if p.killTok.PID != tok.PID {
		t.Fatalf("强杀令牌透传不符: %+v", p.killTok)
	}
}

func TestConfirmForceDeclinesAndConsents(t *testing.T) {
	tok, p := aliveToken()
	deps := fastDeps(p)
	deps.Risk = "正在同步"
	// 无 Confirm 通道 → 拒绝（保守边界）。
	r, err := Quit(context.Background(), tok, PolicyConfirmForce, deps)
	if err != nil || r.Method != MethodDeclined || p.killed {
		t.Fatalf("缺 Confirm 必须按拒绝: %+v err=%v killed=%v", r, err, p.killed)
	}
	// Confirm=false → 拒绝，风险文案需送达。
	deps.Confirm = func(risk string) bool {
		if risk != "正在同步" {
			t.Errorf("风险文案未送达: %q", risk)
		}
		return false
	}
	if r, _ := Quit(context.Background(), tok, PolicyConfirmForce, deps); r.Method != MethodDeclined || p.killed {
		t.Fatalf("用户拒绝后不得强杀: %+v", r)
	}
	// Confirm=true → 强杀。
	deps.Confirm = func(string) bool { return true }
	if r, err := Quit(context.Background(), tok, PolicyConfirmForce, deps); err != nil || !r.Stopped || !r.Forced || r.Method != MethodForced {
		t.Fatalf("确认后应强杀收口: %+v err=%v", r, err)
	}
}

func TestOwnershipLostRefusesKill(t *testing.T) {
	tok, p := aliveToken()
	p.info.ExePath = `C:\windows\system32\other.exe` // 同 PID 换了主体（复用）
	r, err := Quit(context.Background(), tok, PolicyForceFree, fastDeps(p))
	if err == nil || r.Method != MethodOwnership || p.killed {
		t.Fatalf("身份不符必须拒杀上报: %+v err=%v killed=%v", r, err, p.killed)
	}
}

func TestVanishedBeforeKill(t *testing.T) {
	tok, p := aliveToken()
	p.alive = false // 动手前已消失
	r, err := Quit(context.Background(), tok, PolicyForceFree, fastDeps(p))
	if err != nil || !r.Stopped || r.Method != MethodAlreadyGone || p.killed {
		t.Fatalf("已消失应幂等收口: %+v err=%v", r, err)
	}
}

func TestBlockedOnElevatedTarget(t *testing.T) {
	tok, p := aliveToken()
	p.killErr = platform.ErrAccessDenied // UIPI：目标提权，杀不动
	r, err := Quit(context.Background(), tok, PolicyForceFree, fastDeps(p))
	if err != nil || r.Stopped || r.Method != MethodBlocked {
		t.Fatalf("权限拦截应为业务结果非错误: %+v err=%v", r, err)
	}
	p2 := &fakeProc{alive: true, info: p.info, killErr: platform.ErrProtectedProcess}
	deps := fastDeps(p2)
	if r, err := Quit(context.Background(), tok, PolicyForceFree, deps); err != nil || r.Method != MethodBlocked {
		t.Fatalf("红线拦截同归 blocked: %+v err=%v", r, err)
	}
}

func TestGracefulErrorDoesNotAbort(t *testing.T) {
	tok, p := aliveToken()
	deps := fastDeps(p)
	deps.Graceful = func(context.Context) error { return errors.New("投递失败") }
	r, err := Quit(context.Background(), tok, PolicyForceFree, deps)
	if err != nil || r.Method != MethodForced {
		t.Fatalf("优雅信号尽力而为，失败也要走分档处置: %+v err=%v", r, err)
	}
}

func TestNoGracefulGoesStraightToTier(t *testing.T) {
	tok, p := aliveToken()
	r, err := Quit(context.Background(), tok, PolicyForceFree, fastDeps(p))
	if err != nil || r.Method != MethodForced || !p.killed {
		t.Fatalf("无优雅通道应直达按档强杀: %+v err=%v", r, err)
	}
}

func TestRequireProc(t *testing.T) {
	if _, err := Quit(context.Background(), platform.VerifyToken{PID: 1}, PolicyForceFree, Deps{}); err == nil {
		t.Fatal("未注入 Proc 必须报错")
	}
}
