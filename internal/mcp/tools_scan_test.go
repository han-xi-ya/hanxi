package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/lan"
	"hanxi/internal/modules/portscan"
)

// ---------- 扫描族假件 ----------

type fakePortProber struct {
	calls           int
	out             portScanOutcome
	err             error
	lastTarget      string
	lastPorts       []int
	lastTimeout     time.Duration
	lastDeep        bool
	lastHadDeadline bool
}

func (f *fakePortProber) Probe(ctx context.Context, target string, ports []int, perPort time.Duration, deep bool) (portScanOutcome, error) {
	f.calls++
	f.lastTarget, f.lastPorts, f.lastTimeout, f.lastDeep = target, ports, perPort, deep
	_, f.lastHadDeadline = ctx.Deadline()
	if f.err != nil {
		return portScanOutcome{}, f.err
	}
	return f.out, nil
}

type fakeLanProber struct {
	countCalls int
	scanCalls  int
	count      int
	countErr   error
	devices    []lan.DeviceInfo
	scanErr    error
	lastTarget string
}

func (f *fakeLanProber) CountTargets(target string) (int, error) {
	f.countCalls++
	f.lastTarget = target
	return f.count, f.countErr
}

func (f *fakeLanProber) ScanDevices(target string) ([]lan.DeviceInfo, error) {
	f.scanCalls++
	f.lastTarget = target
	return f.devices, f.scanErr
}

func scanSetup(t *testing.T, port PortProber, ln LanProber, grants ...string) Deps {
	t.Helper()
	d, access, _ := newTestServer(t)
	g := map[string]bool{}
	for _, k := range grants {
		g[k] = true
	}
	grant(t, access, g)
	d.PortScan, d.Lan = port, ln
	return d
}

// ---------- hanxi_portscan_scan ----------

func TestPortScanPayloadRedactsBannerAndEchoesBounded(t *testing.T) {
	fp := &fakePortProber{out: portScanOutcome{
		Target:     "192.168.10.7",
		TotalPorts: 3,
		DurationMs: 421,
		Completed:  true,
		OpenPorts: []portscan.PortResult{
			{Port: 22, Status: portscan.PortOpen, Service: "ssh", Banner: "SSH-2.0-OpenSSH_8.9 password=hunter2", LatencyMs: 4},
			{Port: 80, Status: portscan.PortOpen, Service: "http", Banner: "nginx/1.24.0 admin token: abcdefghijklmnop1234 host 192.168.9.9", LatencyMs: 6},
			{Port: 443, Status: portscan.PortOpen, Service: "https", Banner: "Title: NAS登录 user boss@example.com", LatencyMs: 7},
		},
	}}
	c := inProcClient(t, scanSetup(t, fp, nil, "portscan"))
	res, text := callText(t, c, toolPortScan, map[string]any{
		"target": "192.168.10.7", "ports": "22,80,443", "timeout_ms": 800, "deep_detect": true,
	})
	if res.IsError {
		t.Fatalf("授权后的有界扫描应成功: %s", text)
	}
	var p struct {
		Target     string `json:"target"`
		TotalPorts int    `json:"totalPorts"`
		OpenCount  int    `json:"openCount"`
		Completed  bool   `json:"completed"`
		OpenPorts  []struct {
			Port    int    `json:"port"`
			Service string `json:"service"`
			Banner  string `json:"banner"`
		} `json:"openPorts"`
	}
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatalf("输出须为合法 JSON: %v\n%s", err, text)
	}
	if p.Target != "192.168.10.7" || p.TotalPorts != 3 || p.OpenCount != 3 || !p.Completed {
		t.Fatalf("载荷字段错: %s", text)
	}
	// 自由文本红线：banner 里的密钥/密码/邮箱/第三方 IPv4 绝不出机（RedactPII 家族口径）
	for _, banned := range []string{"hunter2", "abcdefghijklmn", "boss@example.com", "192.168.9.9"} {
		if strings.Contains(text, banned) {
			t.Errorf("敏感串 %q 泄露进模型可见输出: %s", banned, text)
		}
	}
	// 结构化结论保留：端口号本身不销毁（工具核心价值）
	if p.OpenPorts[0].Port != 22 || p.OpenPorts[1].Port != 80 {
		t.Errorf("开放端口明细丢失: %s", text)
	}
	// 透传口径：端口集去重升序、超时原值、deep 开关、墙钟预算已挂
	if len(fp.lastPorts) != 3 || fp.lastTimeout != 800*time.Millisecond || !fp.lastDeep || !fp.lastHadDeadline {
		t.Errorf("后端参数透传错: ports=%v timeout=%v deep=%v deadline=%v", fp.lastPorts, fp.lastTimeout, fp.lastDeep, fp.lastHadDeadline)
	}
}

func TestPortScanBoundsAndValidation(t *testing.T) {
	fp := &fakePortProber{out: portScanOutcome{Completed: true}}
	c := inProcClient(t, scanSetup(t, fp, nil, "portscan"))

	// 端口数超限：整体拒绝并给拆分指引，后端零触发（不静默截断——截断会伪装"探完"）
	res, text := callText(t, c, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "1-300"})
	if !res.IsError || !strings.Contains(text, "256") {
		t.Errorf("超限端口集必须拒绝并给上限指引: %s", text)
	}
	if fp.calls != 0 {
		t.Errorf("参数被拒时不得触发扫描后端")
	}

	// 目标形态：CIDR/网段/空白一律拒（目标维度是规模爆炸之源，只放行单目标）
	for _, bad := range []string{"192.168.1.0/24", "10.0.0.1-10.0.0.9", "nas host", " "} {
		res, _ = callText(t, c, toolPortScan, map[string]any{"target": bad, "ports": "80"})
		if !res.IsError {
			t.Errorf("非单目标 %q 必须拒绝", bad)
		}
	}
	if fp.calls != 0 {
		t.Errorf("非法 target 不得触发后端, calls=%d", fp.calls)
	}

	// 端口表达式非法：透传引擎错误并拒绝
	res, text = callText(t, c, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "abc"})
	if !res.IsError || !strings.Contains(text, "端口表达式无效") {
		t.Errorf("非法端口表达式必须拒绝: %s", text)
	}

	// 超时钳制：下界 100 / 上界 3000 / 默认 600
	callText(t, c, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "80", "timeout_ms": 1})
	if fp.lastTimeout != minPortTimeout {
		t.Errorf("超时下界钳制失败: %v", fp.lastTimeout)
	}
	callText(t, c, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "80", "timeout_ms": 999999})
	if fp.lastTimeout != maxPortTimeout {
		t.Errorf("超时上界钳制失败: %v", fp.lastTimeout)
	}
	callText(t, c, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "80"})
	if fp.lastTimeout != defaultPortTimeout {
		t.Errorf("默认超时口径错: %v", fp.lastTimeout)
	}
}

func TestPortScanBusyAndPartialResultsHonest(t *testing.T) {
	// 单飞忙：如实报忙（指引错误），不排队不顶任务
	fp := &fakePortProber{err: errScanBusy}
	c := inProcClient(t, scanSetup(t, fp, nil, "portscan"))
	res, text := callText(t, c, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "80"})
	if !res.IsError || !strings.Contains(text, "在途") {
		t.Errorf("忙时必须返回中文指引错误: %s", text)
	}

	// 预算打断的部分结果：completed=false + note 明示"结论不完整"，绝不伪装探完
	fp2 := &fakePortProber{out: portScanOutcome{Target: "10.0.0.1", TotalPorts: 256, Completed: false,
		OpenPorts: []portscan.PortResult{{Port: 22, Status: portscan.PortOpen, Service: "ssh"}}}}
	c2 := inProcClient(t, scanSetup(t, fp2, nil, "portscan"))
	res, text = callText(t, c2, toolPortScan, map[string]any{"target": "10.0.0.1", "ports": "22,80"})
	if res.IsError {
		t.Fatalf("部分结果也应合法返回: %s", text)
	}
	var p struct {
		Completed bool   `json:"completed"`
		Note      string `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatal(err)
	}
	if p.Completed || !strings.Contains(p.Note, "不完整") {
		t.Errorf("超时预算打断必须如实标注不完整结论: %s", text)
	}
}

// TestPortScanBackendSingleFlight 真后端的 MCP 面单飞闸：在途时后来者拿忙错误，
// 不触引擎（busy 标志 CAS 先行，ExecuteScan 根本不被调用，无网络副作用）。
func TestPortScanBackendSingleFlight(t *testing.T) {
	b := newPortScanBackend()
	if !b.busy.CompareAndSwap(false, true) {
		t.Fatal("前置失败：应能占位")
	}
	_, err := b.Probe(context.Background(), "10.0.0.1", []int{80}, time.Second, false)
	if !errors.Is(err, errScanBusy) {
		t.Fatalf("占用期间再扫描必须回忙错误, got %v", err)
	}
	b.busy.Store(false)
	// ctx 已取消 + 空端口集：ExecuteScan 无网络动作即收尾，Completed 必须如实为 false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := b.Probe(ctx, "10.0.0.1", []int{}, time.Second, false)
	if err != nil {
		t.Fatalf("取消路径应合法收尾: %v", err)
	}
	if out.Completed {
		t.Error("ctx 已死时不得报告 completed=true（半截结论诚实口径）")
	}
}

// ---------- hanxi_lan_scan ----------

func TestLanScanPayloadRowsAndRedacts(t *testing.T) {
	// 备注里的赋值型密钥（Redact 家族的 password= 词形）与邮箱必须被打码；
	// "pwd=" 不属既有口径词形（Redact 只认 token/secret/password/passwd/sk/auth 族），
	// 测试按真实口径取样，避免虚构脱敏能力。
	ln := &fakeLanProber{count: 254, devices: []lan.DeviceInfo{
		{IP: "192.168.1.1", MAC: "AA:BB:CC:DD:EE:FF", Remark: "主路由 admin@example.com password=hunter2", RTTMs: 2, IsGateway: true},
		{IP: "192.168.1.7", MAC: "11:22:33:44:55:66", Hostname: "DESKTOP-ABC", RTTMs: 1, IsSelf: true},
	}}
	c := inProcClient(t, scanSetup(t, nil, ln, "lan"))
	res, text := callText(t, c, toolLanScan, map[string]any{"target": "192.168.1.0/24"})
	if res.IsError {
		t.Fatalf("授权后的局域网扫描应成功: %s", text)
	}
	var env struct {
		Count     int `json:"count"`
		Truncated bool
		Results   []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("须为 listResult JSON 信封: %v\n%s", err, text)
	}
	if env.Count != 2 || env.Truncated {
		t.Fatalf("信封字段错: %s", text)
	}
	// 网络拓扑核心结论原样保留（授权本键即知情放行，描述已声明 IP/MAC 下发）
	if env.Results[0]["ip"] != "192.168.1.1" || env.Results[0]["mac"] != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("IP/MAC 结论字段丢失: %s", text)
	}
	// 自由文本（备注/主机名）过 RedactPII 家族：邮箱/密码串不出机
	for _, banned := range []string{"hunter2", "admin@example.com"} {
		if strings.Contains(text, banned) {
			t.Errorf("敏感串 %q 泄露进模型可见输出: %s", banned, text)
		}
	}
	if env.Results[0]["isGateway"] != true || env.Results[1]["isSelf"] != true {
		t.Errorf("isSelf/isGateway 标记丢失: %s", text)
	}
}

func TestLanScanBoundsBusyAndRowCap(t *testing.T) {
	// 地址数超限（MCP 面 1024，严于引擎 /20 硬限）：拒绝并指引，扫描零触发
	ln := &fakeLanProber{count: 2048}
	c := inProcClient(t, scanSetup(t, nil, ln, "lan"))
	res, text := callText(t, c, toolLanScan, map[string]any{"target": "10.0.0.0/21"})
	if !res.IsError || !strings.Contains(text, "1024") {
		t.Errorf("超限网段必须拒绝并给上限指引: %s", text)
	}
	if ln.scanCalls != 0 {
		t.Error("预检拒绝后不得触发扫描")
	}

	// 解析错误透传（复用 lan 同一解析面）
	ln.countErr = errors.New("invalid CIDR")
	res, text = callText(t, c, toolLanScan, map[string]any{"target": "garbage"})
	if !res.IsError || !strings.Contains(text, "invalid CIDR") {
		t.Errorf("范围无效必须报指引: %s", text)
	}

	// 单飞忙（真 lanProbe 把 lan.ErrScanInProgress 映射为 errScanBusy；
	// 假件直接回忙错误验证 handler 侧口径）
	ln2 := &fakeLanProber{count: 254, scanErr: errScanBusy}
	c2 := inProcClient(t, scanSetup(t, nil, ln2, "lan"))
	res, text = callText(t, c2, toolLanScan, map[string]any{"target": "192.168.1.0/24"})
	if !res.IsError || !strings.Contains(text, "在途") {
		t.Errorf("忙时必须返回中文指引错误: %s", text)
	}

	// 行数硬顶 256：拿满上限置 truncated（截断 ≠ 无更多设备）
	devs := make([]lan.DeviceInfo, 0, maxLanDeviceRows+40)
	for i := 0; i < maxLanDeviceRows+40; i++ {
		devs = append(devs, lan.DeviceInfo{IP: "192.168.9.9", RTTMs: 1})
	}
	ln3 := &fakeLanProber{count: 1024, devices: devs}
	c3 := inProcClient(t, scanSetup(t, nil, ln3, "lan"))
	res, text = callText(t, c3, toolLanScan, map[string]any{"target": "192.168.8.0/22"})
	if res.IsError {
		t.Fatalf("满载扫描应成功: %s", text)
	}
	var env struct {
		Count     int `json:"count"`
		Truncated bool
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatal(err)
	}
	if env.Count != maxLanDeviceRows || !env.Truncated {
		t.Errorf("行数硬顶/截断标志错: %+v", env)
	}
}

// TestLanBusyErrorContract lan 引擎忙错误哨兵文案稳定（无头映射中文指引的前提；
// 若引擎改文案本测试第一时间红，提醒同步 lanProbe 的 errors.Is 映射）。
func TestLanBusyErrorContract(t *testing.T) {
	if lan.ErrScanInProgress.Error() != "scan already in progress" {
		t.Errorf("lan.ErrScanInProgress 文案漂移: %q", lan.ErrScanInProgress.Error())
	}
}
