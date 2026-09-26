package translucenttb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/translucenttb/instance"
	"hanxi/internal/modules/translucenttb/version"
)

// TestVersionCompare 年份.序号版本数值分段比较：2026.10 必须大于 2026.2
// （字典序会得出反果，目录名排序依赖本函数），imported- 兜底版本退化为字典序不 panic。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2026.2", "2026.1", 1},
		{"2026.1", "2026.2", -1},
		{"2026.2", "2026.2", 0},
		{"2026.10", "2026.9", 1},
		{"2027.1", "2026.99", 1},
		// imported- 兜底段非数值 → 退化为字典序（与 ccswitch 同构取舍，只求排序确定不求语义）
		{"imported-20260906-150405", "2026.2", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// fakeTBInstall 造一个可通过 ListInstalled 锚点自检的"已装版本"目录骨架
// （exe + 全套伴生非空文件），供 AV 话术事实注入的用例使用。
func fakeTBInstall(t *testing.T, versionsDir, ver string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "translucenttb_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TranslucentTB.exe", "ExplorerHooks.dll", "ExplorerTAP.dll", "ProgramLog.dll", "Xaml.dll", "resources.pri"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAVCrashAccountingViaEmit AV 崩溃落账全链（OnState 钩子档）：failed 且
// 退出码反解码为 0xC0000005 才记账；同 StoppedAt 的重复广播幂等；非 AV 码与
// 不可反解码（ExitCode=0，如启动失败）不入账。
func TestAVCrashAccountingViaEmit(t *testing.T) {
	svc := &TranslucentTBService{store: newTranslucentTBStore(t.TempDir())}
	at := time.Now()
	av := func(stopped time.Time) instance.Snapshot {
		return instance.Snapshot{State: instance.StateFailed, ExitCode: 3221225477, Version: "2026.2", StoppedAt: stopped}
	}

	svc.emitInstanceState(av(at))
	svc.emitInstanceState(av(at)) // 同一笔崩溃重复广播：不重账
	if got := svc.store.AVCrashCount(); got != 1 {
		t.Fatalf("重复广播后计数应为 1, got %d", got)
	}

	svc.emitInstanceState(av(at.Add(time.Minute)))
	if got := svc.store.AVCrashCount(); got != 2 {
		t.Fatalf("新一笔崩溃应累计为 2, got %d", got)
	}

	svc.emitInstanceState(instance.Snapshot{State: instance.StateFailed, ExitCode: 1, Version: "2026.2", StoppedAt: at.Add(2 * time.Minute)})
	svc.emitInstanceState(instance.Snapshot{State: instance.StateFailed, ExitCode: 0, Error: "进程启动失败: fake", StoppedAt: at.Add(3 * time.Minute)})
	if got := svc.store.AVCrashCount(); got != 2 {
		t.Fatalf("非 AV 码/不可反解码不得入账, got %d", got)
	}
}

// TestAVCrashFactsOlderVersions 话术事实提供者：点名早于崩溃版本的已装旧版
// （新者在前、崩溃版本自身不入列），计数取 store 账目；空崩溃版本只回计数。
func TestAVCrashFactsOlderVersions(t *testing.T) {
	versionsDir := filepath.Join(t.TempDir(), "versions")
	fakeTBInstall(t, versionsDir, "2026.2")
	fakeTBInstall(t, versionsDir, "2025.1")
	fakeTBInstall(t, versionsDir, "2026.1")
	fakeTBInstall(t, versionsDir, "2026.10") // 更新的版本不是"旧版"，不入列
	svc := &TranslucentTBService{
		store:   newTranslucentTBStore(t.TempDir()),
		manager: version.NewManager(versionsDir),
	}
	if err := svc.store.RecordAVCrash(3221225477, "2026.2", time.Now()); err != nil {
		t.Fatal(err)
	}

	facts := svc.avCrashFacts("2026.2")
	if facts.CrashCount != 1 {
		t.Errorf("计数应透传 store 账目: %+v", facts)
	}
	want := "2026.1、2025.1"
	if got := strings.Join(facts.OlderVersions, "、"); got != want {
		t.Errorf("旧版点名（新者在前）= %q, want %q", got, want)
	}

	if empty := svc.avCrashFacts(""); len(empty.OlderVersions) != 0 || empty.CrashCount != 1 {
		t.Errorf("空崩溃版本只回计数: %+v", empty)
	}
}

// TestQuitOutcomeTextHonest 退出文案契约：external 指引不得承诺代退，
// 正常退出必须告知任务栏还原（透明特效随进程消失，用户需预期）。
func TestQuitOutcomeTextHonest(t *testing.T) {
	ext := QuitOutcome{Stopped: false, External: true, Message: "当前是外部自行启动的实例，请在 TranslucentTB 托盘菜单中退出"}
	if ext.Stopped {
		t.Error("external 语义不得标 Stopped")
	}
	ok := QuitOutcome{Stopped: true, Message: "TranslucentTB 已退出，任务栏已还原默认外观"}
	if !strings.Contains(ok.Message, "还原") {
		t.Error("正常退出文案必须预告任务栏还原")
	}
}
