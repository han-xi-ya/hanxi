package termora

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/termora/instance"
	"hanxi/internal/modules/termora/version"
)

// 机主实跑反馈（2026-09-26）：唯一已装版本恰为"当前使用版本"时，卸载被
// guard 死锁（"请先选择其他版本"但根本没有其他版本可选）。修复语义：
// 唯一版本放行 + 卸载成功后清空 active；多版本时维持拦截。

// seedInstalled 造一个 ListInstalled 可识别的最小假版本目录
// （jpackage 嵌套布局 Termora/Termora.exe 非空字节即可）。
func seedInstalled(t *testing.T, versionsDir, token string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "termora_"+token, "Termora")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Termora.exe"), []byte("MZ fake payload"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestTermoraService(t *testing.T) (*TermoraService, string) {
	t.Helper()
	versionsDir := t.TempDir()
	svc := &TermoraService{
		manager:   version.NewManager(versionsDir),
		store:     newTermoraStore(t.TempDir()),
		engine:    instance.NewEngine(nil, nil, nil, instance.Callbacks{}),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}
	return svc, versionsDir
}

func TestRemoveOnlyActiveVersionAllowed(t *testing.T) {
	svc, versionsDir := newTestTermoraService(t)
	seedInstalled(t, versionsDir, "2.16.1")
	if err := svc.store.SetActive("v2.16.1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveVersion("2.16.1"); err != nil {
		t.Fatalf("唯一已装版本为使用中时应放行卸载: %v", err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("卸载唯一使用中版本后 active 应清空，实得 %q", got)
	}
	if installed, err := svc.manager.ListInstalled(); err != nil || len(installed) != 0 {
		t.Fatalf("版本应已卸载，实得 %+v (err=%v)", installed, err)
	}
}

func TestRemoveActiveVersionRejectedWhenOthersInstalled(t *testing.T) {
	svc, versionsDir := newTestTermoraService(t)
	seedInstalled(t, versionsDir, "2.16.1")
	seedInstalled(t, versionsDir, "2.17.0")
	if err := svc.store.SetActive("v2.16.1"); err != nil {
		t.Fatal(err)
	}
	err := svc.RemoveVersion("2.16.1")
	if err == nil || !strings.Contains(err.Error(), "请先选择其他版本") {
		t.Fatalf("多版本时使用中版本卸载应被拒，实得 %v", err)
	}
	if got := svc.store.GetActive(); got != "v2.16.1" {
		t.Fatalf("卸载被拒后 active 不应变动，实得 %q", got)
	}
}

// 非使用中版本不受 guard 约束，多版本在场也直接放行（回归护栏）。
func TestRemoveInactiveVersionAlwaysAllowed(t *testing.T) {
	svc, versionsDir := newTestTermoraService(t)
	seedInstalled(t, versionsDir, "2.16.1")
	seedInstalled(t, versionsDir, "2.17.0")
	if err := svc.store.SetActive("v2.17.0"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveVersion("2.16.1"); err != nil {
		t.Fatalf("非使用中版本卸载应直接放行: %v", err)
	}
	if got := svc.store.GetActive(); got != "v2.17.0" {
		t.Fatalf("active 不应因卸载他版本变动，实得 %q", got)
	}
}
