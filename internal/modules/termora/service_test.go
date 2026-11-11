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

// applyDataMode 的三条可确定路径（2026-11-11 数据模式开关）：
//   - 独立模式：补齐 exe 同级 data\ 激活器；
//   - 共享模式（默认）+ 遗留 data\：改名停用留档，内容一字节不动；
//   - 共享模式 + 无 data\：零动作零噪音（新装版本本来就共享）。
//
// 改名失败（实例在跑、目录被占用）走"如实上抛并中止启动"，由调用链的 error 传播
// 承载——Windows 上无法在不引入独占句柄接缝的前提下确定性构造，故不在此伪装覆盖。
func TestApplyDataModeBySwitch(t *testing.T) {
	exeOf := func(versionsDir, token string) string {
		return filepath.Join(versionsDir, "termora_"+token, "Termora", "Termora.exe")
	}

	t.Run("独立模式=补齐 data 激活器", func(t *testing.T) {
		svc, versionsDir := newTestTermoraService(t)
		seedInstalled(t, versionsDir, "2.16.1")
		exe := exeOf(versionsDir, "2.16.1")
		if err := svc.store.SetPortableData(true); err != nil {
			t.Fatal(err)
		}
		note, err := svc.applyDataMode(exe)
		if err != nil || note != "" {
			t.Fatalf("独立模式应静默建目录: note=%q err=%v", note, err)
		}
		if fi, serr := os.Stat(filepath.Join(filepath.Dir(exe), termoraDataDirName)); serr != nil || !fi.IsDir() {
			t.Fatalf("独立模式必须建 data\\: %v", serr)
		}
	})

	t.Run("共享模式=遗留 data 改名停用且内容保留", func(t *testing.T) {
		svc, versionsDir := newTestTermoraService(t)
		seedInstalled(t, versionsDir, "2.16.2")
		exe := exeOf(versionsDir, "2.16.2")
		dataDir := filepath.Join(filepath.Dir(exe), termoraDataDirName)
		if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "config", "termora.db"), []byte("隔离期数据"), 0o644); err != nil {
			t.Fatal(err)
		}
		note, err := svc.applyDataMode(exe) // 默认共享
		if err != nil {
			t.Fatalf("applyDataMode: %v", err)
		}
		if !strings.Contains(note, "已停用") || !strings.Contains(note, "data.disabled-") {
			t.Errorf("启动回执必须说明停用与回退办法: %q", note)
		}
		if _, serr := os.Stat(dataDir); !os.IsNotExist(serr) {
			t.Fatal("共享模式下 data\\ 必须不再在场（否则 Termora 仍走隔离）")
		}
		matches, gerr := filepath.Glob(filepath.Join(filepath.Dir(exe), "data.disabled-*"))
		if gerr != nil || len(matches) != 1 {
			t.Fatalf("应留下恰好一个停用档: %v err=%v", matches, gerr)
		}
		raw, rerr := os.ReadFile(filepath.Join(matches[0], "config", "termora.db"))
		if rerr != nil || string(raw) != "隔离期数据" {
			t.Fatalf("停用档内容必须原样保留: %q err=%v", raw, rerr)
		}
	})

	t.Run("共享模式=无 data 时零动作", func(t *testing.T) {
		svc, versionsDir := newTestTermoraService(t)
		seedInstalled(t, versionsDir, "2.16.3")
		note, err := svc.applyDataMode(exeOf(versionsDir, "2.16.3"))
		if err != nil || note != "" {
			t.Fatalf("无遗留目录应静默通过: note=%q err=%v", note, err)
		}
	})

	t.Run("模式开关落盘可复现", func(t *testing.T) {
		st := newTermoraStore(t.TempDir())
		if st.GetPortableData() {
			t.Fatal("数据模式默认必须为共享（false）")
		}
		if err := st.SetPortableData(true); err != nil {
			t.Fatal(err)
		}
		if !newTermoraStore(dirOf(st)).GetPortableData() {
			t.Fatal("重载后模式应复现为独立")
		}
	})
}

// dirOf 反射出 store 的所在目录（复用 jsonstore 落盘位置断言重载语义）。
func dirOf(s *termoraStore) string { return filepath.Dir(s.filePath) }

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
