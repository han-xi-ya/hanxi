package wsl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePortproxyShow(t *testing.T) {
	sample := `

Listen on ipv4:             Connect to ipv4:

Address         Port        Address         Port
--------------- ----------  --------------- ----------
0.0.0.0         8080        172.25.4.136    8080
127.0.0.1       9000        172.25.4.136    9001

`
	got := parsePortproxyShow(sample)
	if len(got) != 2 {
		t.Fatalf("应解析出 2 条: %+v", got)
	}
	if got[0].ListenPort != 8080 || got[0].ConnectAddr != "172.25.4.136" || got[1].ConnectPort != 9001 {
		t.Fatalf("列解析错位: %+v", got)
	}
	// 中文表头/坏行/越界端口全部被结构约束过滤
	zh := "侦听于 IPv4...\n地址 端口\n--------------- ----------\nnot-an-ip 80 1.2.3.4 80\n0.0.0.0 70000 1.2.3.4 80\n0.0.0.0 80 1.2.3.4 8080"
	if list := parsePortproxyShow(zh); len(list) != 1 || list[0].ListenPort != 80 {
		t.Fatalf("结构约束过滤失效: %+v", list)
	}
}

// ppFixture：Ubuntu 在册且运行、guest IP=172.25.9.99、规则文件落临时目录。
func ppFixture(t *testing.T) (*WslService, *wslStub) {
	t.Helper()
	svc, _, _ := newTestService()
	svc.ppPath = filepath.Join(t.TempDir(), "wsl-portproxy.json")
	stub := &wslStub{resp: func(args []string) (string, error) {
		switch joined(args) {
		case "-l -q":
			return "Ubuntu\x00", nil
		case "-l -q --running":
			return "Ubuntu\x00", nil
		case "-d Ubuntu -- hostname -I":
			return "172.25.9.99", nil
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}}
	svc.runWsl = stub.run
	return svc, stub
}

// stubPPShow 注入 netsh show all 输出。
func stubPPShow(t *testing.T, out string) {
	t.Helper()
	old := netshShowAll
	netshShowAll = func(context.Context) (string, error) { return out, nil }
	t.Cleanup(func() { netshShowAll = old })
}

func TestAddPortRuleValidation(t *testing.T) {
	svc, _ := ppFixture(t)
	if _, err := svc.AddPortRule("Ubuntu", 70000, 0, "0.0.0.0", false, ""); err == nil {
		t.Fatal("端口越界必须拒绝")
	}
	if _, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.999", false, ""); err == nil {
		t.Fatal("非法 IPv4 必须拒绝")
	}
	if _, err := svc.AddPortRule("evil", 8080, 0, "0.0.0.0", false, ""); err == nil {
		t.Fatal("名单外发行版必须拒绝")
	}
	r, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.0", true, "web")
	if err != nil {
		t.Fatal(err)
	}
	if r.Guest != 8080 || !r.Enabled {
		t.Fatalf("guest 默认=端口、默认启用: %+v", r)
	}
	if _, err := svc.AddPortRule("Ubuntu", 8080, 80, "0.0.0.0", false, ""); err == nil {
		t.Fatal("同监听口重复规则必须拒绝")
	}
	svc.mu.Lock()
	pending := svc.ppPending
	svc.mu.Unlock()
	if !pending {
		t.Fatal("增删改后必须置待应用标记")
	}
}

func TestListPortRulesSeparatesForeign(t *testing.T) {
	svc, _ := ppFixture(t)
	if _, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.0", false, ""); err != nil {
		t.Fatal(err)
	}
	stubPPShow(t, `
Address         Port        Address         Port
--------------- ----------  --------------- ----------
0.0.0.0         8080        172.25.4.136    8080
0.0.0.0         3389        172.25.4.200    3389
`)
	view, err := svc.ListPortRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Rules) != 1 || !view.Rules[0].Applied || view.Rules[0].ActiveIP != "172.25.4.136" {
		t.Fatalf("规则应用态错误: %+v", view.Rules)
	}
	if view.Rules[0].TargetIP != "172.25.9.99" {
		t.Fatalf("漂移对比 IP 未解析: %+v", view.Rules[0])
	}
	if len(view.Foreign) != 1 || view.Foreign[0].ListenPort != 3389 {
		t.Fatalf("外部转发应单列不接管: %+v", view.Foreign)
	}
}

func TestApplyPortRulesPlanAndScript(t *testing.T) {
	svc, _ := ppFixture(t)
	// A：0.0.0.0:8080→Ubuntu（防火墙开）；现态指旧 IP → 漂移需重指。
	// B：0.0.0.0:9000→Ubuntu 已禁用；现态仍在 → 摘除。
	// C：127.0.0.1:7000→Debian 启用但发行版未运行 → 跳过。
	// 另有一条外部 3389 转发 → 绝不触碰。
	rA, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.0", true, "")
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := svc.ppLoad()
	rB := rA.PortRule
	rB.ID, rB.Port, rB.Firewall, rB.Enabled = "pp-b", 9000, false, false
	rC := rA.PortRule
	rC.ID, rC.Port, rC.Distro, rC.Listen, rC.Firewall = "pp-c", 7000, "Debian", "127.0.0.1", false
	svc.ppSave(append(rules, rB, rC))
	stubPPShow(t, `
Address         Port        Address         Port
--------------- ----------  --------------- ----------
0.0.0.0         8080        172.25.4.136    8080
0.0.0.0         9000        172.25.4.136    9000
0.0.0.0         3389        172.25.4.200    3389
`)
	var script string
	svc.elevProc = func(_ context.Context, file string, args ...string) (OperationOutcome, error) {
		if file != "powershell.exe" || len(args) < 4 {
			return OperationOutcome{}, fmt.Errorf("unexpected elev %s %v", file, args)
		}
		script = args[len(args)-1]
		return OperationOutcome{Success: true}, nil
	}
	out, err := svc.ApplyPortRules()
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success {
		t.Fatalf("应用回执失败: %+v", out)
	}
	if !strings.Contains(script, "portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=8080") ||
		!strings.Contains(script, "portproxy add v4tov4 listenaddress=0.0.0.0 listenport=8080 connectaddress=172.25.9.99 connectport=8080") {
		t.Fatalf("漂移规则未先删后加重指: %s", script)
	}
	if !strings.Contains(script, "delete v4tov4 listenaddress=0.0.0.0 listenport=9000") {
		t.Fatalf("禁用规则的系统转发未摘除: %s", script)
	}
	if strings.Contains(script, "3389") {
		t.Fatalf("外部转发绝不触碰: %s", script)
	}
	if strings.Contains(script, "7000") {
		t.Fatalf("未运行发行版不应产生 netsh 动作: %s", script)
	}
	if !strings.Contains(script, `if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }`) {
		t.Fatalf("逐条退出码传播红线缺失: %s", script)
	}
	if !strings.Contains(script, "'Hanxi WSL 8080'") {
		t.Fatalf("防火墙规则应随 A 先删后加: %s", script)
	}
	if !strings.Contains(out.Message, "跳过") || !strings.Contains(out.Message, "Debian 未运行") {
		t.Fatalf("跳过项必须点名: %+v", out)
	}
	svc.mu.Lock()
	pending := svc.ppPending
	svc.mu.Unlock()
	if pending {
		t.Fatal("应用成功后待应用标记必须清除")
	}
}

func TestApplyNoChangesNoElevation(t *testing.T) {
	svc, _ := ppFixture(t)
	if _, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.0", false, ""); err != nil {
		t.Fatal(err)
	}
	svc.clearPending()
	stubPPShow(t, `
Address         Port        Address         Port
--------------- ----------  --------------- ----------
0.0.0.0         8080        172.25.9.99     8080
`)
	svc.elevProc = func(context.Context, string, ...string) (OperationOutcome, error) {
		t.Fatal("现态一致时不得弹 UAC")
		return OperationOutcome{}, nil
	}
	out, err := svc.ApplyPortRules()
	if err != nil || !out.Success || !strings.Contains(out.Message, "无需变更") {
		t.Fatalf("一致即空操作: %v %+v", err, out)
	}
}
