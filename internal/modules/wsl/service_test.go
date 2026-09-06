package wsl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/wsl/readiness"
	"hanxi/internal/modules/wsl/releases"
)

// fakeOpener / fakeElevated / fakePS 捕获外呼参数，验证白名单与地址拼装。
type fakeOpener struct{ urls []string }

func (f *fakeOpener) OpenURL(url string) error { f.urls = append(f.urls, url); return nil }

type elevatedCall struct {
	file string
	args []string
}

type fakeElevated struct {
	calls []elevatedCall
	out   OperationOutcome
	err   error
}

func (f *fakeElevated) run(_ context.Context, file string, args ...string) (OperationOutcome, error) {
	f.calls = append(f.calls, elevatedCall{file: file, args: args})
	return f.out, f.err
}

func newTestService() (*WslService, *fakeElevated, *fakeOpener) {
	ev := &fakeElevated{out: OperationOutcome{Success: true, Message: "ok"}}
	op := &fakeOpener{}
	svc := &WslService{
		opener: op,
		probe: func(context.Context) (readiness.ProbeResult, error) {
			return readiness.ProbeResult{OSBuild: 26200, Arch: "AMD64", Hypervisor: true, Store: true, FeatureVM: true}, nil
		},
		netProbe:   func(context.Context) (string, string) { return "200", "200" },
		wslVersion: func(context.Context) string { return "2.7.13" },
		wslDistros: func(context.Context) []readiness.Distro { return nil },
		onlineDistros: func(context.Context) ([]readiness.DistroOption, error) {
			return []readiness.DistroOption{{ID: "Ubuntu", Label: "Ubuntu"}, {ID: "Debian", Label: "Debian"}}, nil
		},
		overview: func(local string) (releases.Overview, error) {
			return releases.Overview{LocalVersion: local, Relation: "update"}, nil
		},
		elevProc: ev.run,
		localPS: func(_ context.Context, script string) (string, error) {
			switch {
			case strings.HasPrefix(script, "$ProgressPreference"): // 卸载脚本
				return "none|0", nil
			default: // ProductCode 补查
				return "", nil
			}
		},
		emit: func(string, any) {},
	}
	return svc, ev, op
}

func TestGetReadinessWiresAll(t *testing.T) {
	svc, _, _ := newTestService()
	rpt, err := svc.GetReadiness()
	if err != nil {
		t.Fatal(err)
	}
	if rpt.Verdict != readiness.VerdictReady || rpt.WslVersion != "2.7.13" {
		t.Fatalf("报告装配错误: %+v", rpt)
	}
}

func TestGetReadinessProbeFailureIsError(t *testing.T) {
	svc, _, _ := newTestService()
	svc.probe = func(context.Context) (readiness.ProbeResult, error) {
		return readiness.ProbeResult{}, errors.New("ps blocked")
	}
	if _, err := svc.GetReadiness(); err == nil {
		t.Fatal("探针失败必须整体报错（缺门槛项的报告没有结论意义）")
	}
}

func TestStartReadinessStreamsAllStages(t *testing.T) {
	svc, _, _ := newTestService()
	stages := make(chan string, 8)
	svc.emit = func(_ string, payload any) {
		if u, ok := payload.(ReadinessUpdate); ok && u.Stage != "error" {
			stages <- u.Stage
		}
	}
	if err := svc.StartReadiness(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for !seen["done"] {
		select {
		case s := <-stages:
			seen[s] = true
		case <-deadline:
			t.Fatalf("流式体检超时，已收阶段: %v", seen)
		}
	}
	for _, want := range []string{"system", "wsl", "net", "done"} {
		if !seen[want] {
			t.Fatalf("缺少阶段 %s（收到 %v）", want, seen)
		}
	}
}

func TestInstallDistroWhitelist(t *testing.T) {
	svc, ev, _ := newTestService()

	// 白名单内的 ID：固定 wsl.exe + 参数原样进入提权通道。
	if _, err := svc.InstallDistro(" Ubuntu "); err != nil {
		t.Fatal(err)
	}
	if len(ev.calls) != 1 || ev.calls[0].file != "wsl.exe" || strings.Join(ev.calls[0].args, " ") != "--install -d Ubuntu" {
		t.Fatalf("白名单内安装参数错误: %+v", ev.calls)
	}

	// 白名单外（含注入尝试）：拒绝执行。
	if _, err := svc.InstallDistro("Ubuntu; rm -rf /"); err == nil {
		t.Fatal("清单外 ID 必须拒绝")
	}
	if len(ev.calls) != 1 {
		t.Fatalf("被拒绝的调用不应触达提权通道: %+v", ev.calls)
	}

	// 清单获取失败时保守中止，而非放行。
	svc.onlineDistros = func(context.Context) ([]readiness.DistroOption, error) { return nil, errors.New("net down") }
	if _, err := svc.InstallDistro("Ubuntu"); err == nil {
		t.Fatal("清单不可得时必须中止安装")
	}
}

// 虚拟化装载预检（#36 家族的新证词）：虚拟机平台未生效时装发行版注定失败，
// 必须在提权前拦下——实机事故是"待重启期间连点安装全部假成功"。
func TestInstallDistroGatedByVirtualization(t *testing.T) {
	// 已启用但 CBS 台账欠重启 → 拦截，且绝不触达提权通道。
	svc, ev, _ := newTestService()
	svc.probe = func(context.Context) (readiness.ProbeResult, error) {
		return readiness.ProbeResult{FeatureVM: true, RebootPending: true}, nil
	}
	out, err := svc.InstallDistro("Ubuntu")
	if err != nil || out.Success {
		t.Fatalf("待重启时应拦截而非执行: %+v %v", out, err)
	}
	if len(ev.calls) != 0 {
		t.Fatalf("被拦截不得触达提权通道: %+v", ev.calls)
	}
	if !strings.Contains(out.Message, "重启") {
		t.Fatalf("拦截回执应指路重启: %s", out.Message)
	}

	// 虚拟机平台压根没启用 → 同样拦截。
	svc2, ev2, _ := newTestService()
	svc2.probe = func(context.Context) (readiness.ProbeResult, error) {
		return readiness.ProbeResult{FeatureVM: false}, nil
	}
	out2, _ := svc2.InstallDistro("Ubuntu")
	if out2.Success || len(ev2.calls) != 0 {
		t.Fatalf("平台未启用应拦截: %+v", out2)
	}

	// 探针自身失败 → 保守放行（不拿读不到的状态误拦正常操作），退出码通道兜底。
	svc3, ev3, _ := newTestService()
	svc3.probe = func(context.Context) (readiness.ProbeResult, error) {
		return readiness.ProbeResult{}, errors.New("ps blocked")
	}
	if _, err := svc3.InstallDistro("Ubuntu"); err != nil {
		t.Fatal(err)
	}
	if len(ev3.calls) != 1 {
		t.Fatalf("探针不可得时应放行到提权通道: %+v", ev3.calls)
	}
}

func TestElevatedOperationsUseFixedArgs(t *testing.T) {
	svc, ev, _ := newTestService()
	calls := []struct {
		invoke func() (OperationOutcome, error)
		want   string
	}{
		// 一键开启固定无发行版语义：绝不自动捆绑默认 Linux（用户手动挑选原则）。
		{svc.InstallWsl, "--install --no-distribution"},
		{svc.UpdateWsl, "--update"},
		{svc.UpdateWslWebDownload, "--update --web-download"},
		{svc.SetDefaultVersion2, "--set-default-version 2"},
	}
	for i, c := range calls {
		if _, err := c.invoke(); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if ev.calls[i].file != "wsl.exe" {
			t.Fatalf("case %d 提权目标应为 wsl.exe，得 %s", i, ev.calls[i].file)
		}
		if got := strings.Join(ev.calls[i].args, " "); got != c.want {
			t.Fatalf("case %d args = %q, want %q", i, got, c.want)
		}
	}
}

func TestUninstallWslHappyPath(t *testing.T) {
	svc, ev, _ := newTestService()
	// 探针缓存已含 ProductCode：直接复用、不再补查。
	code := "{104E16F9-C929-46FA-8CC3-A874FC01F91E}"
	svc.setLastProbe(readiness.ProbeResult{WslMSICode: code, WslMSI: "2.7.13.0"})
	var psCalls int
	svc.localPS = func(_ context.Context, script string) (string, error) {
		psCalls++
		if strings.HasPrefix(script, "$ProgressPreference") {
			return "removed|2", nil
		}
		return code, nil
	}

	out, err := svc.UninstallWsl()
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success {
		t.Fatalf("happy path 应成功: %+v", out)
	}
	if ev.calls[0].file != "MsiExec.exe" || ev.calls[0].args[0] != "/X"+code {
		t.Fatalf("MSI 卸载必须走官方 msiexec /X ProductCode: %+v", ev.calls)
	}
	for _, want := range []string{"MSI 系统版", "MSIX 用户包已移除", "2 个历史发行版注册残留", "可选功能"} {
		if !strings.Contains(out.Message, want) {
			t.Fatalf("回执缺少 %q: %s", want, out.Message)
		}
	}
	if psCalls != 1 {
		t.Fatalf("有缓存 ProductCode 时不应补查注册表，实际 PS 调用 %d 次", psCalls)
	}
}

func TestUninstallWslAbortsOnUACCancel(t *testing.T) {
	svc, ev, _ := newTestService()
	svc.setLastProbe(readiness.ProbeResult{WslMSICode: "{104E16F9-C929-46FA-8CC3-A874FC01F91E}"})
	psCalled := false
	svc.localPS = func(context.Context, string) (string, error) { psCalled = true; return "removed|0", nil }
	ev.out = OperationOutcome{Success: false, Message: "已取消 UAC 授权，操作未执行"}

	out, err := svc.UninstallWsl()
	if err != nil {
		t.Fatal(err)
	}
	if out.Success || psCalled {
		t.Fatalf("UAC 取消应整体中止（MSIX 步骤不得继续）: %+v", out)
	}
}

func TestUninstallWslRejectsMalformedProductCode(t *testing.T) {
	svc, ev, _ := newTestService()
	svc.setLastProbe(readiness.ProbeResult{WslMSICode: "not-a-guid"})
	svc.localPS = func(context.Context, string) (string, error) { return "none|0", nil }

	if _, err := svc.UninstallWsl(); err != nil {
		t.Fatal(err)
	}
	for _, c := range ev.calls {
		if c.file == "MsiExec.exe" {
			t.Fatal("非法 ProductCode 绝不能触达 msiexec")
		}
	}
}

func TestLastErrorLine(t *testing.T) {
	out := "\n部署映像服务和管理工具\n[===   10%  ]\n[=========== 100.0%]\n错误: 0x800f0950\n未找到要完成操作所需的文件。\n"
	if got := lastErrorLine(out); !strings.HasPrefix(got, "错误: 0x800f0950") {
		t.Fatalf("未从进度噪音里提出错误行: %q", got)
	}
	if got := lastErrorLine("[== 50%]\n操作成功完成。"); got != "" {
		t.Fatalf("成功输出不应提出错误行: %q", got)
	}
}

// 功能开关命令链必须传播 $LASTEXITCODE（`;` 链吞退出码的实机事故回归锁）。
func TestFeatureCommandsPropagateExitCode(t *testing.T) {
	svc, ev, _ := newTestService()
	for _, invoke := range []func() (OperationOutcome, error){svc.EnableWslFeatures, svc.DisableWslFeatures} {
		if _, err := invoke(); err != nil {
			t.Fatal(err)
		}
		last := ev.calls[len(ev.calls)-1]
		if last.file != "powershell.exe" {
			t.Fatalf("提权目标错误: %s", last.file)
		}
		full := strings.Join(last.args, " ")
		if !strings.Contains(full, "if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }") ||
			!strings.Contains(full, "/norestart; exit $LASTEXITCODE") {
			t.Fatalf("dism 链缺少退出码传播: %s", full)
		}
	}
}

func TestParseUninstallReport(t *testing.T) {
	cases := []struct {
		in      string
		verdict string
		count   int
	}{
		{"removed|3", "removed", 3},
		{"none|0", "none", 0},
		{"failed|", "failed", -1},
		{"garbage", "failed", -1},
	}
	for _, c := range cases {
		v, n := parseUninstallReport(c.in)
		if v != c.verdict || n != c.count {
			t.Fatalf("parse(%q) = (%s,%d), want (%s,%d)", c.in, v, n, c.verdict, c.count)
		}
	}
}

func TestAssetDownloadURLValidation(t *testing.T) {
	want := "https://github.com/microsoft/WSL/releases/download/2.9.10/wsl.2.9.10.0.x64.msi"
	got, err := assetDownloadURL("v2.9.10", "wsl.2.9.10.0.x64.msi") // v 前缀归一
	if err != nil || got != want {
		t.Fatalf("合法直链拼装失败: %q %v", got, err)
	}
	if _, err := assetDownloadURL("../../evil", "wsl.2.9.10.0.x64.msi"); err == nil {
		t.Fatal("tag 路径穿越必须拒绝")
	}
	if _, err := assetDownloadURL("2.9.10", "evil.exe"); err == nil {
		t.Fatal("非白名单文件名必须拒绝")
	}
}

func TestOpenReleaseTagValidation(t *testing.T) {
	svc, _, op := newTestService()
	if err := svc.OpenReleaseTag("v2.9.10"); err != nil {
		t.Fatalf("v 前缀 tag 应被归一接受: %v", err)
	}
	if err := svc.OpenReleaseTag("tag?x=/"); err == nil {
		t.Fatal("非法 tag 必须拒绝")
	}
	_ = op
}

func TestUniquePathNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	p1 := uniquePath(dir, "wsl.2.9.10.0.x64.msi")
	if p1 != filepath.Join(dir, "wsl.2.9.10.0.x64.msi") {
		t.Fatalf("首份路径异常: %s", p1)
	}
	if err := os.WriteFile(p1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2 := uniquePath(dir, "wsl.2.9.10.0.x64.msi")
	if p2 != filepath.Join(dir, "wsl.2.9.10.0.x64 (1).msi") {
		t.Fatalf("重名应递增不覆盖: %s", p2)
	}
}
