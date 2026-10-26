// lifecycle_quiet_test.go 钉死无 Wails 应用（钩子从未装成）时的生命周期诚实性：
// start 必须如实失败而非假成功；stop/重启在无钩子、无窗的静默态幂等且零副作用。
// 有头环境下的真实钩子安装不在本包默认档职责（见 mousetrap 包分档纪律）。
package quickmenu

import (
	"testing"

	"hanxi/internal/extapi"
)

func TestStartFailsWithoutApplication(t *testing.T) {
	svc := NewQuickMenuService(nil, nil, nil)
	if err := svc.start(); err == nil {
		t.Fatal("application.Get()==nil 时 start 不得谎报成功")
	}
	if svc.trapActive() {
		t.Error("start 失败后不得虚报钩子在位")
	}
	if svc.started {
		t.Error("start 失败后 started 账目不得置位（注册表须可重试）")
	}
	// 失败后 stop 幂等收尾：零错误、零 panic
	for i := 0; i < 2; i++ {
		if err := svc.stop(); err != nil {
			t.Fatalf("未启动服务第 %d 次 stop: %v", i+1, err)
		}
	}
}

func TestRestartTrapNoOpWhenNotStarted(t *testing.T) {
	svc := NewQuickMenuService(nil, nil, nil)
	if err := svc.restartTrap(); err != nil {
		t.Fatalf("未启动的热重启应零操作，got %v", err)
	}
	if svc.trapActive() {
		t.Error("restartTrap 零操作路径不得装出钩子")
	}
}

// TestDismissQuietNoWindow 从未建窗：Dismiss/hidePopup/destroyPopup 全链静默，
// 不得 panic、不得凭空出现弹窗引用（空闲计时器到点的销毁回调也必须空转安全）。
func TestDismissQuietNoWindow(t *testing.T) {
	svc := NewQuickMenuService(nil, nil, extapi.NewLeaseHolder(ID))
	if err := svc.Dismiss(); err != nil {
		t.Fatalf("静默态 Dismiss: %v", err)
	}
	svc.hidePopup() // 窗体事件路径同款：popup==nil 必须空转
	svc.destroyPopup(true)
	svc.destroyPopup(false)
	if svc.popup != nil || svc.popupShown {
		t.Errorf("静默态不得残留弹窗账: popup=%v shown=%v", svc.popup, svc.popupShown)
	}
}
