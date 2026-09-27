package wsl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
	// C：127.0.0.1:7000→Debian 启用但发行版未运行 → 跳过；且现态里它有旧转发
	// → 跳过必须豁免差集摘除（"这次没应用成"≠"该拆掉"），不得拼 delete。
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
127.0.0.1       7000        172.25.4.50     7000
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
		t.Fatalf("未运行发行版不应产生 netsh 动作，其现有转发更不得因跳过被摘除: %s", script)
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

// 运行名单读取失败（`wsl -l -q --running` 抖动）必须失败中止：丢弃 ok 三态会把
// 全部发行版误判"未运行"，差集删除循环随之把账本内所有已生效转发整体摘除，
// 且空 cmds 分支还会误清 pending——回执看着正常，系统却被搬空。
func TestApplyPortRulesAbortsOnRunningSetFailure(t *testing.T) {
	svc, stub := ppFixture(t)
	stub.resp = func(args []string) (string, error) {
		switch joined(args) {
		case "-l -q":
			return "Ubuntu\x00", nil
		case "-l -q --running":
			return "", fmt.Errorf("wsl 抖动")
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}
	if _, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.0", false, ""); err != nil {
		t.Fatal(err)
	}
	// 现态里 8080 已生效且指向旧 IP：若误判未运行，差集删除会把它摘掉。
	stubPPShow(t, `
Address         Port        Address         Port
--------------- ----------  --------------- ----------
0.0.0.0         8080        172.25.4.136    8080
`)
	svc.elevProc = func(_ context.Context, _ string, _ ...string) (OperationOutcome, error) {
		t.Fatal("运行名单读不到时不得进入提权批次")
		return OperationOutcome{}, nil
	}
	if _, err := svc.ApplyPortRules(); err == nil {
		t.Fatal("quietSet 失败必须报错中止，而非按'全未运行'继续差集摘除")
	}
	svc.mu.Lock()
	pending := svc.ppPending
	svc.mu.Unlock()
	if !pending {
		t.Fatal("中止路径不得清待应用标记")
	}
}

// 备注截断必须落在 rune 边界：字节口径直切 [:120] 会把三字节汉字切成残码乱码。
func TestAddPortRuleNoteRuneSafeTruncate(t *testing.T) {
	svc, _ := ppFixture(t)
	// 1 个 ASCII + 50 个三字节汉字：第 121 字节恰落在第 40 个汉字中间。
	note := "a" + strings.Repeat("汉", 50)
	r, err := svc.AddPortRule("Ubuntu", 8080, 0, "0.0.0.0", false, note)
	if err != nil {
		t.Fatal(err)
	}
	want := "a" + strings.Repeat("汉", 39) // 回退到最近的完整汉字边界（118 字节）
	if r.Note != want {
		t.Fatalf("备注未按 rune 边界截断: got %q (%d 字节, valid=%v)", r.Note, len(r.Note), utf8.ValidString(r.Note))
	}
	if !utf8.ValidString(r.Note) || len(r.Note) > 120 {
		t.Fatalf("截断结果必须 ≤120 字节且为合法 UTF-8: %d 字节", len(r.Note))
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
