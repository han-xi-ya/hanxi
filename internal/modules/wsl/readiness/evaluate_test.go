package readiness

import (
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

func baseProbe() ProbeResult {
	return ProbeResult{
		OSBuild: 26200, OSUBR: 9168, ReleaseID: "25H2", Arch: "AMD64",
		Hypervisor: true, VTFirmware: boolPtr(false), VBS: intPtr(2), Store: true,
		FeatureVM: true,
		APIGitHub: float64(200), GitHub: float64(200),
	}
}

func findCheck(t *testing.T, r Report, key string) CheckItem {
	t.Helper()
	for _, c := range r.Checks {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("报告缺少体检项 %s", key)
	return CheckItem{}
}

// 骨架互锁：key 与顺序是前端 WSLView CHECK_SKELETON 的镜像，
// 任何重排/增删必须两侧同步（流式落位按 key 合并，顺序变化影响渲染骨架）。
func TestBuildItemsKeyOrderLocked(t *testing.T) {
	want := []string{"build", "arch", "virt", "hypervisor", "feature", "vbs", "store", "form", "net", "wsl"}
	got := make([]string, 0, len(want))
	for _, c := range BuildItems(baseProbe(), "2.7.13") {
		got = append(got, c.Key)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("体检项顺序漂移: %v != %v（须与前端 CHECK_SKELETON 同步）", got, want)
	}
}

func TestEvaluateFeatureOffIsWarn(t *testing.T) {
	// 实机校准画像：内核隔离把监控程序跑着（Hypervisor=true），但虚拟机平台已禁用
	// ——WSL2 实际跑不了，必须 warn 点名而非被"监控程序运行中"麻痹成静默通过。
	p := baseProbe()
	p.FeatureVM = false
	r := Evaluate(p, "", nil, "")
	feat := findCheck(t, r, "feature")
	if feat.State != StateWarn {
		t.Fatalf("虚拟机平台禁用应 warn: %+v", feat)
	}
	if r.Verdict != VerdictAttention {
		t.Fatalf("结论应进注意项: %s", r.Verdict)
	}
}

func TestEvaluateReady(t *testing.T) {
	r := Evaluate(baseProbe(), "2.7.13", []Distro{{Name: "Ubuntu", State: "Running", Version: "2", Default: true}}, "2026-09-06 12:00:00")
	if r.Verdict != VerdictReady {
		t.Fatalf("期望 ready，得 %s（%s）", r.Verdict, r.VerdictTitle)
	}
	if r.WslVersion != "2.7.13" || len(r.Distros) != 1 {
		t.Fatalf("WSL 现状未透传: %q %v", r.WslVersion, r.Distros)
	}
	for _, c := range r.Checks {
		if c.State == StateBad || c.State == StateWarn {
			t.Fatalf("ready 结论下不应有阻塞/注意项: %+v", c)
		}
	}
}

func TestEvaluateBlockedByBuild(t *testing.T) {
	p := baseProbe()
	p.OSBuild = 18362
	r := Evaluate(p, "", nil, "")
	if r.Verdict != VerdictBlocked {
		t.Fatalf("老系统应判 blocked，得 %s", r.Verdict)
	}
	if findCheck(t, r, "build").State != StateBad {
		t.Fatal("build 项应为 bad")
	}
}

func TestEvaluateBlockedByVirtualization(t *testing.T) {
	p := baseProbe()
	p.Hypervisor = false
	p.VTFirmware = boolPtr(false)
	r := Evaluate(p, "", nil, "")
	if r.Verdict != VerdictBlocked || findCheck(t, r, "virt").State != StateBad {
		t.Fatalf("BIOS 未开虚拟化必须阻塞: %s / %+v", r.Verdict, r.Checks)
	}
}

func TestEvaluateAttentionOn403(t *testing.T) {
	p := baseProbe()
	p.APIGitHub = "403"
	r := Evaluate(p, "", nil, "")
	if r.Verdict != VerdictAttention {
		t.Fatalf("403 应判 attention（硬件满足、通道受限），得 %s", r.Verdict)
	}
	net := findCheck(t, r, "net")
	if net.State != StateWarn || !strings.Contains(net.Detail, "403") {
		t.Fatalf("net 项应给出 403 根因解释: %+v", net)
	}
}

func TestEvaluateNetFailIsWarnNotBlock(t *testing.T) {
	p := baseProbe()
	p.APIGitHub = "net-fail"
	p.GitHub = "net-fail"
	r := Evaluate(p, "", nil, "")
	if findCheck(t, r, "net").State != StateWarn {
		t.Fatal("GitHub 不可达应降为 warn：存在离线导入方案，不算硬阻塞")
	}
}

func TestEvaluateFormCoexistRealMachine(t *testing.T) {
	// 用户实机画像：MSIX 与 MSI 双形态共存且同版（"设置只显一个、卸载卸不干净"）。
	p := baseProbe()
	p.WslMSIX, p.WslMSI, p.WslExeFile = "2.7.13.0", "2.7.13.0", "10.0.26100.8972"
	r := Evaluate(p, "2.7.13", nil, "")
	form := findCheck(t, r, "form")
	if form.State != StateOK || !strings.Contains(form.Detail, "共存") || !strings.Contains(form.Value, "MSIX") {
		t.Fatalf("双形态同版应 ok + 共存提醒: %+v", form)
	}
}

func TestEvaluateFormLeftover(t *testing.T) {
	p := baseProbe()
	p.WslMSIX = "2.7.13.0"
	r := Evaluate(p, "", nil, "") // 运行时不可用 → 跨源互证升级 wsl 项
	wslItem := findCheck(t, r, "wsl")
	if wslItem.State != StateWarn || !strings.Contains(wslItem.Detail, "残留") {
		t.Fatalf("运行时失联但形态残留应升级 warn: %+v", wslItem)
	}
	if r.Verdict != VerdictAttention {
		t.Fatalf("残留应进注意项而非静默: %s", r.Verdict)
	}
}

func TestEvaluateRebootPendingLedger(t *testing.T) {
	// 已启用 + CBS 台账在案：feature 项必须点名"待重启生效"，且 Report 透出该事实。
	p := baseProbe()
	p.RebootPending = true
	r := Evaluate(p, "2.7.13", nil, "")
	if !r.RebootPending {
		t.Fatal("Report.rebootPending 未透传")
	}
	feat := findCheck(t, r, "feature")
	if !strings.Contains(feat.Value, "待重启生效") || !strings.Contains(feat.Detail, "重启") {
		t.Fatalf("feature 项未反映待重启台账: %+v", feat)
	}
	// 引导条的判据来自检测报告本身，而非前端操作记忆。
}

func TestEvaluateLauncherOnlyIsInfo(t *testing.T) {
	p := baseProbe()
	p.WslExeFile = "10.0.26100.8875 (WinBuild.160101.0800)" // 系统自带启动器，任何 Win10+ 都在
	r := Evaluate(p, "", nil, "")
	form := findCheck(t, r, "form")
	if form.State != StateInfo || !strings.Contains(form.Detail, "未安装 WSL 主体") {
		t.Fatalf("仅启动器存在应 info 提示未安装主体: %+v", form)
	}
}

func TestEvaluateFormMismatch(t *testing.T) {
	p := baseProbe()
	p.WslMSIX, p.WslMSI = "2.7.13.0", "2.5.10.0"
	r := Evaluate(p, "2.7.13", nil, "")
	if findCheck(t, r, "form").State != StateWarn {
		t.Fatal("MSIX/MSI 版本不一致应 warn")
	}
}

func TestEvaluateUnknownFirmwareIsInfo(t *testing.T) {
	p := baseProbe()
	p.Hypervisor = false
	p.VTFirmware = nil
	r := Evaluate(p, "", nil, "")
	if findCheck(t, r, "virt").State != StateInfo {
		t.Fatal("读不到固件位应判 info，不误伤为未开启")
	}
}

func TestEvaluateReadyNotInstalledWording(t *testing.T) {
	r := Evaluate(baseProbe(), "", nil, "")
	if r.Verdict != VerdictReady || !strings.Contains(r.VerdictTitle, "一键开启") {
		t.Fatalf("未安装但全通过应引导一键开启: %+v", r)
	}
}
