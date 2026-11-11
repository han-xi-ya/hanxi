package airsnip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/airsnip/instance"
	"hanxi/internal/modules/airsnip/version"
	"hanxi/internal/platform"
	appwin "hanxi/internal/platform/windows"
)

// realJobAPI 真 Windows Job Object（与生产同源）：冷启动编排用例需要一个状态
// 可信的引擎，fake job 的快照语义不完整，会把手动构造的 external/running 态
// 当成被测对象而不是被测环境。
func realJobAPI() platform.JobAPI { return appwin.NewJobAPI() }

// ---------- 测试夹具 ----------

type fixture struct {
	svc         *AirsnipService
	versionsDir string
	stateDir    string
	commands    []string // 仅 newEngineFixture 起效：dispatch 投过哪些内部命令
	posted      bool     // command 接缝的投递结果（默认 true）
}

// newFixture 引擎与 command 接缝都留 nil：本文件用例全部走静止语义（版本区、账本、
// 话术、门），而 dispatch 在 `s.engine == nil` 处即回 errNoEngine，不会走到命令投递。
// 需要引擎真实语义的（dispatch 冷启动失败 / 外部实例在场）另用 newEngineFixture，
// 它把探针与命令通道都换成确定性假件。
// 与 snipaste 的 newSnipFixture 同纪律：所有触碰引擎的 RPC 都有显式 nil 防护，
// 拿 nil 引擎跑出 panic 是不合格实现。
func newFixture(t *testing.T) *fixture {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	return &fixture{
		svc: &AirsnipService{
			manager: version.NewManager(versionsDir),
			store:   newAirsnipStore(stateDir),
			holder:  extapi.NewLeaseHolder(ID),
		},
		versionsDir: versionsDir,
		stateDir:    stateDir,
	}
}

// stubProbe 确定性进程探针：默认"这台机器上没有 airsnip.exe"。真实探针会把用例
// 结论交给运气——机主正在用 AirSnip 时同一用例走的是 external 分支（#148 避坑第
// 3 条：判据不许依赖机器现场状态）。在场态由 pids 显式注入。
type stubProbe struct{ pids []uint32 }

func (p *stubProbe) FindPIDs() []uint32              { return p.pids }
func (p *stubProbe) IsRunning() bool                 { return len(p.pids) > 0 }
func (p *stubProbe) WaitForReady(time.Duration) bool { return len(p.pids) > 0 }

// newEngineFixture 挂真 Job + 确定性探针（默认无 airsnip 进程）+ 命令投递记录器，
// 用来验冷启动编排的失败路径——这条路径不 spawn 任何进程，却覆盖了
// "解析版本 → 拒装 → 人话指引"的完整链路。
func newEngineFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.posted = true
	f.svc.command = func(kind string) (bool, bool) {
		f.commands = append(f.commands, kind)
		return f.posted, f.posted
	}
	f.svc.engine = instance.NewEngine(realJobAPI(), &stubProbe{}, instance.Callbacks{})
	return f
}

// newExternalEngineFixture 模拟"机主自己开着 AirSnip、Hanxi 没托管任何版本"：
// 探针报一个非自有 PID → 引擎分类为 external，dispatch 跳过冷启动直接投命令。
func newExternalEngineFixture(t *testing.T) *fixture {
	t.Helper()
	f := newEngineFixture(t)
	f.svc.engine = instance.NewEngine(realJobAPI(), &stubProbe{pids: []uint32{424242}}, instance.Callbacks{})
	return f
}

// mkVersion 造一个形状合法的已装版本目录（本模块 ListInstalled 不读 PE，
// 目录令牌 + 非空 exe 即可入列）。
func mkVersion(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "airsnip_"+ver)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "airsnip.exe"), []byte("MZ载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ---------- 版本入参归一与比较 ----------

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v0.7.0":                 "v0.7.0",
		"0.7.0":                  "v0.7.0",
		"  V0.7.0  ":             "v0.7.0",
		"vvimported-abcdef":      "vvimported-abcdef", // 上游不产出该形，但绝不吞成 vimported
		"imported-abcdef123456":  "vimported-abcdef123456",
		"vimported-abcdef123456": "vimported-abcdef123456",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestVersionCompareNumeric 1.10.0 vs 1.9.0 的字典序误判必须被数值分段闸挡住；
// imported- 兜底目录必须沉底（否则冷启动"最新已装"会选中它）。
func TestVersionCompareNumeric(t *testing.T) {
	if versionCompare("v1.10.0", "v1.9.0") <= 0 {
		t.Error("1.10.0 应大于 1.9.0")
	}
	if versionCompare("v0.7.0", "vimported-abcdef123456") <= 0 {
		t.Error("语义版本应大于 imported- 兜底目录")
	}
	if versionCompare("v0.7.0", "v0.7.0") != 0 {
		t.Error("同版本应相等")
	}
	if importedRank("imported-x") != 1 || importedRank("0.7.0") != 0 {
		t.Error("importedRank 权重错")
	}
}

// ---------- 命令话术 ----------

// TestCommandResultVocabulary 三个动作的词表与话术（前端按钮回执与通知文案的
// 唯一来源）：投错 ID 上游只会静默忽略，所以这里也是"动作没做错"的最后一道纸面锁。
func TestCommandResultVocabulary(t *testing.T) {
	action, msg := commandResult("shot", false)
	if action != "shot" || !strings.Contains(msg, "截图") {
		t.Errorf("shot 词表不符: %q %q", action, msg)
	}
	action, msg = commandResult("pin", false)
	if action != "pin" || !strings.Contains(msg, "贴") {
		t.Errorf("pin 词表不符: %q %q", action, msg)
	}
	action, msg = commandResult("settings", true)
	if action != "opened" || msg != "已打开 AirSnip 设置面板" {
		t.Errorf("settings(focused) 词表不符: %q %q", action, msg)
	}
	action, msg = commandResult("settings", false)
	if action != "opened" || !strings.Contains(msg, "未能置前") {
		t.Errorf("settings(未置前) 应如实分说，不该冒充成功: %q %q", action, msg)
	}
}

// TestUIPIAdviceContainsElevationKeyword 投递失败话术必须含「管理员」——
// 与本模块 740 文案同一关键词口径（前端提权重启钮据此匹配）。
func TestUIPIAdviceContainsElevationKeyword(t *testing.T) {
	if !strings.Contains(uipiAdvice, "管理员") {
		t.Errorf("命令失败指引应含「管理员」，got %q", uipiAdvice)
	}
	if !strings.Contains(uipiAdvice, "热键") {
		t.Errorf("指引应给出可用的替代路径（上游热键/托盘），got %q", uipiAdvice)
	}
}

// ---------- 版本区账本 ----------

// TestSetActiveValidatesBeforePersisting 设定使用版本先校验后落账：未装版本被拒
// 且盘上账本原样保留（判据 = 同目录重载 store）。
func TestSetActiveValidatesBeforePersisting(t *testing.T) {
	f := newFixture(t)
	mkVersion(t, f.versionsDir, "0.7.0")
	if err := f.svc.store.SetActive("v0.6.0"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SetActiveVersion("v9.9.9"); err == nil {
		t.Fatal("未安装版本应被拒绝")
	}
	if got := newAirsnipStore(f.stateDir).GetActive(); got != "v0.6.0" {
		t.Fatalf("拒绝后账本应保持 v0.6.0，实得 %q", got)
	}

	got, err := f.svc.SetActiveVersion("0.7.0")
	if err != nil || got != "v0.7.0" {
		t.Fatalf("合法版本应设定成功: %v %v", got, err)
	}
	if reloaded := newAirsnipStore(f.stateDir).GetActive(); reloaded != "v0.7.0" {
		t.Errorf("设定未落盘: %q", reloaded)
	}
}

// TestRemoveVersionClearsActiveSelfHeal 卸载当前使用版本后清空 active，
// 下次冷启动自动回退最新已装（不留一个指向空气的版本号骗过 resolve）。
func TestRemoveVersionClearsActiveSelfHeal(t *testing.T) {
	f := newFixture(t)
	mkVersion(t, f.versionsDir, "0.7.0")
	if _, err := f.svc.SetActiveVersion("v0.7.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveVersion("v0.7.0"); err != nil {
		t.Fatal(err)
	}
	if got := newAirsnipStore(f.stateDir).GetActive(); got != "" {
		t.Errorf("卸载 active 版本后应清空设定，实得 %q", got)
	}
}

// TestImportLocalFirstVersionBecomesActive N24 契约：没有任何版本时，
// 第一个到手的版本默认=使用版本（导入链与下载链同源）。
func TestImportLocalFirstVersionBecomesActive(t *testing.T) {
	f := newFixture(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "airsnip.exe"), []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := f.svc.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("无名可嗅的导入应落哈希兜底，got %q", info.Version)
	}
	if got := newAirsnipStore(f.stateDir).GetActive(); got != info.Version {
		t.Errorf("首个导入版本应自动设为使用，实得 %q want %q", got, info.Version)
	}
}

// ---------- 下载事务 ----------

// TestDownloadAlreadyInstalledShortCircuit 已装版本重复点下载直接回
// already-installed，不发起后台事务（避免 journal 里出现空转事务）。
func TestDownloadAlreadyInstalledShortCircuit(t *testing.T) {
	f := newFixture(t)
	mkVersion(t, f.versionsDir, "0.7.0")
	f.svc.download = func(_ context.Context, _, _ string, _ func(version.DownloadProgress)) error {
		t.Error("已装版本不应再走下载链")
		return nil
	}
	out, err := f.svc.DownloadVersion("0.7.0")
	if err != nil {
		t.Fatal(err)
	}
	if out != "already-installed" {
		t.Errorf("应返回 already-installed，实得 %q", out)
	}
}

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步先落账，而非等 DownloadContext
// 返回之后——否则前端共享 store 收到 done 即复刷版本区读 GetActiveVersion，
// 瞬时读到空值。判据取在 done 送达后、下载函数尚未返回的时点。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	f := newFixture(t)
	activeAtDone := make(chan string, 1)
	f.svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: ver, Stage: "downloading", Done: 10, Total: 100})
		emit(version.DownloadProgress{Version: ver, Stage: "extract"})
		emit(version.DownloadProgress{Version: ver, Stage: "done", Done: 100, Total: 100})
		activeAtDone <- f.svc.store.GetActive()
		return nil
	}
	if _, err := f.svc.DownloadVersion("v0.7.0"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-activeAtDone:
		if got != "v0.7.0" {
			t.Fatalf("done 送达时 active 应已落账为 v0.7.0，实得 %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("后台下载收口未在时限内送达 done")
	}
}

// TestDownloadKeepsUserChosenActive 已有手选 active 时，后续版本下载不得覆盖它。
func TestDownloadKeepsUserChosenActive(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.store.SetActive("v0.6.0"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	f.svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: ver, Stage: "done"})
		close(done)
		return nil
	}
	if _, err := f.svc.DownloadVersion("v0.7.0"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("下载收口超时")
	}
	if got := f.svc.store.GetActive(); got != "v0.6.0" {
		t.Errorf("用户手选 active 被下载链覆盖: %q", got)
	}
}

// ---------- 引擎缺失与冷启动编排 ----------

// TestNilEnginePathsFailLoudly 引擎未装配时各控制 RPC 必须返回错误而不是 panic
// （snipaste 的显式防护同纪律）。
func TestNilEnginePathsFailLoudly(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.GetStatus(); !errors.Is(err, errNoEngine) {
		t.Errorf("GetStatus 应报引擎未装配，got %v", err)
	}
	if _, err := f.svc.Shot(); !errors.Is(err, errNoEngine) {
		t.Errorf("Shot 应报引擎未装配，got %v", err)
	}
	if _, err := f.svc.Quit(); !errors.Is(err, errNoEngine) {
		t.Errorf("Quit 应报引擎未装配，got %v", err)
	}
	// 卸载/导入在 nil 引擎下应正常走版本区逻辑（不因引擎缺席而失败）
	mkVersion(t, f.versionsDir, "0.7.0")
	if err := f.svc.RemoveVersion("v0.7.0"); err != nil {
		t.Errorf("nil 引擎不应阻断卸载: %v", err)
	}
	f.svc.shutdown() // 不应 panic
}

// TestDispatchColdStartWithoutInstall 未装任何版本时点「截图」：错误指引必须把用户
// 带到"先下载/导入"，而不是含糊的启动失败。
func TestDispatchColdStartWithoutInstall(t *testing.T) {
	f := newEngineFixture(t)
	for name, call := range map[string]func() error{
		"OpenWindow": func() error { _, err := f.svc.OpenWindow(); return err },
		"Shot":       func() error { _, err := f.svc.Shot(); return err },
		"Pin":        func() error { _, err := f.svc.Pin(); return err },
	} {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "下载") {
			t.Errorf("%s 应给出\"先下载/导入\"指引，got %v", name, err)
		}
	}
}

// TestDispatchWithExternalInstance 机主自己开着 AirSnip、Hanxi 一侧没装任何版本：
// 这条 external 分支在真机上随时可达（本机 airsnip.exe 在场即命中）。旧夹具用**真进程
// 探针**，于是"机主没开 AirSnip"时用例走冷启动失败路径、开着时直落命令投递——同一
// 用例两种结论（#148 避坑第 3 条点名的正是这种"依赖机器现场状态的前提"）。现在两条
// 分支各自钉死：external 态不许报"先下载/导入"，而要把命令投出去，判据落在
// "投了哪个 kind"上；投递失败必须给送达指引，不含糊成启动失败。
func TestDispatchWithExternalInstance(t *testing.T) {
	f := newExternalEngineFixture(t)
	out, err := f.svc.Shot()
	if err != nil {
		t.Fatalf("外部实例在场应直接投命令，got %v", err)
	}
	if out.Action != "shot" || !out.External || !strings.Contains(out.Message, "冻结") {
		t.Errorf("外部态回执不符: %+v", out)
	}
	if got := strings.Join(f.commands, ","); got != "shot" {
		t.Errorf("投递记录 = %q, want shot（且不得先冷启动拉起自有实例）", got)
	}

	f.posted = false
	if _, err := f.svc.Pin(); err == nil || !strings.Contains(err.Error(), "未能把命令送达") {
		t.Fatalf("投递失败应给送达指引: %v", err)
	}
}

// TestQuitStoppedIdempotent 静止态 Quit 走引擎幂等路径应报成功并给出人话回执。
func TestQuitStoppedIdempotent(t *testing.T) {
	f := newEngineFixture(t)
	out, err := f.svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || out.External || !strings.Contains(out.Message, "AirSnip") {
		t.Errorf("静止态 Quit 回执不符: %+v", out)
	}
}

// TestFollowOnExitDefaultOffAndPersist 默认"不随 Hanxi 关闭"（全托管统一口径），
// 设定后同目录重载可见。
func TestFollowOnExitDefaultOffAndPersist(t *testing.T) {
	f := newFixture(t)
	if on, err := f.svc.GetFollowOnExit(); err != nil || on {
		t.Fatalf("默认应为 false（独立运行）: %v %v", on, err)
	}
	if err := f.svc.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	reloaded := newAirsnipStore(f.stateDir)
	if !reloaded.GetFollowOnExit() {
		t.Error("开关未落盘")
	}
	if reloaded.GetActive() != "" {
		t.Error("写 followOnExit 不应顺带污染 activeVersion")
	}
}

// TestRepositoryURLAndShortcutGuide 上游地址是页面「打开仓库」的唯一事实源；
// 未装版本时建快捷方式应给出下载指引而非 panic。
func TestRepositoryURLAndShortcutGuide(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.RepositoryURL()
	if err != nil || got != "https://github.com/bd-dxg/airsnip" {
		t.Errorf("RepositoryURL = %q err=%v", got, err)
	}
	if err := f.svc.CreateDesktopShortcut(); err == nil || !strings.Contains(err.Error(), "下载") {
		t.Errorf("未装版本建快捷方式应给下载指引，got %v", err)
	}
}
