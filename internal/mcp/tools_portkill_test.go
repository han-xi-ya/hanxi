package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/modules/portkill"
	"hanxi/internal/platform"
)

// tools_portkill_test.go 用假 PortkillBackend 驱动二段式全链路，钉五条硬断言：
// token 过期、token 复用、TOCTOU 身份变化、黑名单拒杀、审计必落（每结局一条），
// 外加默认关（总闸关时零后端触发且 token 不烧）与注解如实申报。

// ---------- 假后端 ----------

type killCall struct {
	pid   uint32
	exe   string
	start int64
}

type fakePortkillBackend struct {
	occupants []portkill.PortOccupant
	queryErr  error
	queries   int

	proc    platform.ProcInfo // execute 复查看到的"实况"
	procErr error
	isProt  bool

	killResult portkill.KillResult
	killErr    error
	kills      []killCall
}

func (f *fakePortkillBackend) QueryPort(int) ([]portkill.PortOccupant, error) {
	f.queries++
	return f.occupants, f.queryErr
}

func (f *fakePortkillBackend) QueryProcess(uint32) (platform.ProcInfo, error) {
	return f.proc, f.procErr
}

func (f *fakePortkillBackend) IsProtected(pid uint32, info platform.ProcInfo) bool {
	return f.isProt
}

func (f *fakePortkillBackend) KillProcess(pid uint32, exePath string, startedAtUnix int64) (portkill.KillResult, error) {
	f.kills = append(f.kills, killCall{pid, exePath, startedAtUnix})
	if f.killErr != nil {
		return portkill.KillResult{}, f.killErr
	}
	return f.killResult, nil
}

// ---------- 工具件（不经 toolDefs 注册，直调 handler 全链） ----------

func mcpCallReq(tool string, args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = args
	return req
}

// portkillSetup 接线一套可控环境（假时钟+假后端+假总闸+审计捕获），
// t.Cleanup 复位 hook，绝不影响包内其他测试。
func portkillSetup(t *testing.T, be *fakePortkillBackend, policyOn bool) (*captureHandler, *TokenStore, *testClock) {
	t.Helper()
	h := installAuditCapture(t)
	clk := &testClock{t: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)}
	store := NewKillTokenStore(portkillTokenTTL, clk.Time)
	SetPortkillWiring(NewPortkillWiring(be, &fakeDestructivePolicy{enabled: policyOn}, store))
	t.Cleanup(func() { SetPortkillWiring(nil) })
	return h, store, clk
}

func callHandler(t *testing.T, build func(Deps) (mcp.Tool, server.ToolHandlerFunc), args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	tool, handler := build(Deps{})
	res, err := handler(context.Background(), mcpCallReq(tool.Name, args))
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			text = tc.Text
		}
	}
	return res, text
}

func callPrepare(t *testing.T, args map[string]any) (*mcp.CallToolResult, string) {
	return callHandler(t, buildPortkillPrepareTool, args)
}

// prepareOK 走一遍成功侦察并回解 JSON 载荷。
func prepareOK(t *testing.T, port int) (resultPayload, string) {
	t.Helper()
	res, text := callPrepare(t, map[string]any{"port": float64(port)})
	if res.IsError {
		t.Fatalf("prepare must succeed: %s", text)
	}
	var p resultPayload
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatalf("prepare payload must be JSON: %v\n%s", err, text)
	}
	return p, text
}

// mustPrepare 成功侦察并返回载荷（与 prepareOK 同体，语义化别名供 TOCTOU 用例直读）。
func mustPrepare(t *testing.T, port int) resultPayload {
	t.Helper()
	p, _ := prepareOK(t, port)
	return p
}

func firstToken(t *testing.T, p resultPayload) string {
	t.Helper()
	items, _ := p["occupants"].([]any)
	for _, raw := range items {
		o, _ := raw.(map[string]any)
		if tok, _ := o["confirmToken"].(string); tok != "" {
			return tok
		}
	}
	t.Fatalf("payload 无可用 token: %v", p)
	return ""
}

// ---------- 注解如实申报（红线升版的声明面） ----------

func TestPortkillToolAnnotationsAreHonest(t *testing.T) {
	pTool, _ := buildPortkillPrepareTool(Deps{})
	eTool, _ := buildPortkillExecuteTool(Deps{})

	if pTool.Annotations.ReadOnlyHint == nil || !*pTool.Annotations.ReadOnlyHint {
		t.Error("prepare 必须申报 readOnlyHint=true（它确实只读）")
	}
	if pTool.Annotations.DestructiveHint == nil || *pTool.Annotations.DestructiveHint {
		t.Error("prepare 必须申报 destructiveHint=false（无杀伤面）")
	}
	if !strings.Contains(pTool.Description, "只读") {
		t.Error("prepare 描述须含『只读』字样")
	}
	if eTool.Annotations.ReadOnlyHint == nil || *eTool.Annotations.ReadOnlyHint {
		t.Error("execute 不得伪装只读（readOnlyHint 必须为 false）")
	}
	if eTool.Annotations.DestructiveHint == nil || !*eTool.Annotations.DestructiveHint {
		t.Error("execute 必须申报 destructiveHint=true（如实申报破坏性）")
	}
	if strings.Contains(eTool.Description, "只读") {
		t.Error("execute 描述不得含『只读』字样")
	}
	for _, tool := range []mcp.Tool{pTool, eTool} {
		if !strings.Contains(tool.Description, "destructive.json") && !strings.Contains(tool.Description, "总闸") {
			t.Errorf("%s 描述须向模型交代机主总闸的存在", tool.Name)
		}
	}
}

// ---------- 默认关：总闸未开时零后端触发、token 不烧 ----------

func TestPortkillPolicyOffDeniesEverything(t *testing.T) {
	started := time.Now()
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{{Port: 8080, PID: 100, ProcessName: "myapp.exe", ExePath: `C:\a\myapp.exe`, StartedAt: started}},
		proc:      platform.ProcInfo{PID: 100, ExePath: `C:\a\myapp.exe`, StartedAt: started},
	}
	h, store, _ := portkillSetup(t, be, false)

	res, text := callPrepare(t, map[string]any{"port": float64(8080)})
	if !res.IsError || !strings.Contains(text, "destructive.json") {
		t.Fatalf("总闸关时 prepare 必须给指引错误: %s", text)
	}
	if be.queries != 0 {
		t.Errorf("总闸关时不得触碰后端，queries=%d", be.queries)
	}
	// 先塞一枚 token 再试 execute：闸关不消耗（撤闸是机主即时权利，不留副作用）。
	tok, _, err := store.Issue(8080, 100, `C:\a\myapp.exe`, started)
	if err != nil {
		t.Fatal(err)
	}
	res, text = callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "令牌未被消耗") {
		t.Fatalf("总闸关时 execute 必须拒绝且不烧 token: %s", text)
	}
	if len(be.kills) != 0 {
		t.Error("总闸关时绝不可查杀")
	}
	if store.Pending() != 1 {
		t.Errorf("被拒执行不得消耗 token, pending=%d", store.Pending())
	}
	outs := h.auditOutcomes("")
	if got := len(h.auditOutcomes("prepare")); got != 1 || h.auditRecord("prepare", "denied_policy") == nil {
		t.Errorf("prepare 拒绝必落审计, outcomes=%v", outs)
	}
	if got := len(h.auditOutcomes("execute")); got != 1 || h.auditRecord("execute", "denied_policy") == nil {
		t.Errorf("execute 拒绝必落审计, outcomes=%v", outs)
	}
}

// ---------- 成功链路：prepare 发 token → execute 复查通过 → 查杀 + 审计 ----------

func TestPortkillHappyPath(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{
			{Port: 8080, Protocol: "TCP", PID: 100, ProcessName: "myapp.exe", ExePath: `C:\dev\myapp.exe`, StartedAt: started, State: "LISTENING"},
		},
		proc:       platform.ProcInfo{PID: 100, Name: "myapp.exe", ExePath: `C:\dev\myapp.exe`, StartedAt: started},
		killResult: portkill.KillResult{Success: true},
	}
	h, store, _ := portkillSetup(t, be, true)

	p, _ := prepareOK(t, 8080)
	if p["port"] != float64(8080) || p["count"] != float64(1) || p["tokensIssued"] != float64(1) {
		t.Fatalf("prepare 载荷字段错: %v", p)
	}
	tok := firstToken(t, p)
	if store.Pending() != 1 {
		t.Fatalf("token 应入库, pending=%d", store.Pending())
	}

	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if res.IsError {
		t.Fatalf("execute 应成功: %s", text)
	}
	var out resultPayload
	if err := json.Unmarshal([]byte(text), &out); err != nil || out["ok"] != true || out["outcome"] != "success" {
		t.Fatalf("execute 载荷错: %s", text)
	}
	if len(be.kills) != 1 {
		t.Fatalf("必须且只查杀一次, kills=%v", be.kills)
	}
	k := be.kills[0]
	if k.pid != 100 || k.exe != `C:\dev\myapp.exe` || k.start != started.Unix() {
		t.Errorf("查杀指纹透传错: %+v (want pid=100 exe=C:\\dev\\myapp.exe start=%d)", k, started.Unix())
	}
	rec := h.auditRecord("execute", "success")
	if rec == nil {
		t.Fatal("成功查杀必落审计")
	}
	if attrString(*rec, "channel") != "mcp" || attrString(*rec, "pid") != "100" {
		t.Errorf("审计字段错: channel=%s pid=%s", attrString(*rec, "channel"), attrString(*rec, "pid"))
	}
	// 审计绝不含明文 token（只记录短哈希）。
	if got := attrString(*rec, "token"); got == "" || got == tok || len(got) >= len(tok) {
		t.Errorf("审计只可记录 token 哈希, got %q", got)
	}
}

// ---------- TOCTOU：身份变化/进程消失 → 中止且 kill 零调用、token 已烧 ----------

func TestPortkillTOCTOUIdentityChangeAborts(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{{Port: 8080, PID: 100, ProcessName: "myapp.exe", ExePath: `C:\dev\myapp.exe`, StartedAt: started}},
		proc:      platform.ProcInfo{PID: 100, ExePath: `C:\other\hijack.exe`, StartedAt: started}, // PID 已被复用
	}
	h, _, _ := portkillSetup(t, be, true)
	tok := firstToken(t, mustPrepare(t, 8080))

	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "可执行路径已变化") {
		t.Fatalf("exe 指纹变化必须中止并告知: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("TOCTOU 中止后绝不可调用查杀")
	}
	if h.auditRecord("execute", "aborted_identity") == nil {
		t.Fatal("TOCTOU 中止必落审计")
	}
	// token 已烧：拿同一枚重试只会得到"已被使用"，杜绝重放。
	res, text = callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "已被使用") {
		t.Fatalf("中止后 token 不得复用: %s", text)
	}
}

func TestPortkillTOCTOUProcessGoneAborts(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{{Port: 8080, PID: 100, ProcessName: "myapp.exe", ExePath: `C:\dev\myapp.exe`, StartedAt: started}},
		procErr:   platform.ErrProcessNotFound, // 目标已退出
	}
	h, _, _ := portkillSetup(t, be, true)
	tok := firstToken(t, mustPrepare(t, 8080))

	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "现已不存在") {
		t.Fatalf("目标消失必须如实中止: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("目标消失不得尝试查杀")
	}
	if h.auditRecord("execute", "aborted_identity") == nil {
		t.Fatal("目标消失必落审计")
	}
}

func TestPortkillTOCTOUStartTimeChangeAborts(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{{Port: 8080, PID: 100, ExePath: `C:\dev\myapp.exe`, StartedAt: started}},
		proc:      platform.ProcInfo{PID: 100, ExePath: `C:\dev\myapp.exe`, StartedAt: started.Add(2 * time.Minute)}, // 超 1s 容差
	}
	portkillSetup(t, be, true)
	tok := firstToken(t, mustPrepare(t, 8080))

	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "启动时间已变化") {
		t.Fatalf("启动时间漂移必须中止: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("启动时间漂移不得查杀")
	}
}

// ---------- token 过期 ----------

func TestPortkillTokenExpiryOnExecute(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{{Port: 8080, PID: 100, ExePath: `C:\dev\myapp.exe`, StartedAt: started}},
		proc:      platform.ProcInfo{PID: 100, ExePath: `C:\dev\myapp.exe`, StartedAt: started},
	}
	h, _, clk := portkillSetup(t, be, true)
	tok := firstToken(t, mustPrepare(t, 8080))
	clk.Advance(portkillTokenTTL + time.Second)

	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "过期") {
		t.Fatalf("过期 token 必须拒绝: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("过期 token 绝不可触发查杀")
	}
	if h.auditRecord("execute", "expired") == nil {
		t.Fatal("过期拒绝必落审计")
	}
}

// ---------- 黑名单：拿合法 token 也杀不掉（独立于授权态） ----------

func TestPortkillBlacklistRefusesDespiteValidToken(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		proc:       platform.ProcInfo{PID: 4, Name: "System", StartedAt: started},
		killResult: portkill.KillResult{Success: true},
	}
	h, store, _ := portkillSetup(t, be, true)
	// 绕过 prepare 直接塞 token（模拟签发面失守的极端场景）：黑名单仍必须兜底。
	tok, _, err := store.Issue(445, 4, ``, started)
	if err != nil {
		t.Fatal(err)
	}
	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "拒绝查杀") {
		t.Fatalf("PID 4 必须永久拒杀: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("黑名单命中绝不可调用查杀")
	}
	if h.auditRecord("execute", "denied_protected") == nil {
		t.Fatal("黑名单拒杀必落审计")
	}
	// svchost（平台红线经 IsProtected）同样拒。
	tok2, _, err := store.Issue(445, 900, `C:\Windows\System32\svchost.exe`, started)
	if err != nil {
		t.Fatal(err)
	}
	be.proc = platform.ProcInfo{PID: 900, ExePath: `C:\Windows\System32\svchost.exe`, StartedAt: started}
	be.isProt = true
	res, text = callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok2})
	if !res.IsError || !strings.Contains(text, "红线") {
		t.Fatalf("平台红线进程必须拒杀: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("平台红线命中绝不可调用查杀")
	}
}

// ---------- 审计必落：execute 每种结局一条（五态矩阵） ----------

func TestPortkillAuditMandatoryForEveryOutcome(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		args    map[string]any
		mutate  func(t *testing.T, be *fakePortkillBackend, store *TokenStore, clk *testClock) string
		outcome string
	}{
		{name: "success", mutate: func(t *testing.T, be *fakePortkillBackend, s *TokenStore, _ *testClock) string {
			tok, _, err := s.Issue(8080, 100, `C:\dev\myapp.exe`, started)
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}, outcome: "success"},
		{name: "need_elevate", mutate: func(t *testing.T, be *fakePortkillBackend, s *TokenStore, _ *testClock) string {
			be.killResult = portkill.KillResult{NeedElevate: true, ErrorMessage: "权限不足"}
			tok, _, err := s.Issue(8080, 100, `C:\dev\myapp.exe`, started)
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}, outcome: "need_elevate"},
		{name: "fail", mutate: func(t *testing.T, be *fakePortkillBackend, s *TokenStore, _ *testClock) string {
			be.killResult = portkill.KillResult{ErrorMessage: "终止失败: boom"}
			tok, _, err := s.Issue(8080, 100, `C:\dev\myapp.exe`, started)
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}, outcome: "fail"},
		{name: "unknown token", args: map[string]any{"token": "nope"}, outcome: "unknown_token"},
		{name: "expired", mutate: func(t *testing.T, be *fakePortkillBackend, s *TokenStore, clk *testClock) string {
			tok, _, err := s.Issue(8080, 100, `C:\dev\myapp.exe`, started)
			if err != nil {
				t.Fatal(err)
			}
			clk.Advance(portkillTokenTTL + time.Second)
			return tok
		}, outcome: "expired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			be := &fakePortkillBackend{
				proc:       platform.ProcInfo{PID: 100, Name: "myapp.exe", ExePath: `C:\dev\myapp.exe`, StartedAt: started},
				killResult: portkill.KillResult{Success: true},
			}
			h, store, clk := portkillSetup(t, be, true)
			args := tc.args
			if args == nil {
				args = map[string]any{"token": tc.mutate(t, be, store, clk)}
			}
			res, text := callHandler(t, buildPortkillExecuteTool, args)
			if tc.outcome == "success" && res.IsError {
				t.Fatalf("success case errored: %s", text)
			}
			rec := h.auditRecord("execute", tc.outcome)
			if rec == nil {
				t.Fatalf("结局 %s 必落一条审计（无论成败），got=%v", tc.outcome, h.auditOutcomes("execute"))
			}
			if attrString(*rec, "channel") != "mcp" {
				t.Errorf("审计必须可经 channel=mcp 单独 grep, got %q", attrString(*rec, "channel"))
			}
		})
	}
}

// ---------- prepare 细节：多占用/受保护不发 token/空占用 ----------

func TestPortkillPrepareTokenization(t *testing.T) {
	started := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	be := &fakePortkillBackend{
		occupants: []portkill.PortOccupant{
			{Port: 8080, PID: 100, ProcessName: "myapp.exe", ExePath: `C:\dev\myapp.exe`, StartedAt: started},
			{Port: 8080, PID: 4, ProcessName: "System", StartedAt: started},
			{Port: 8080, PID: 200, ProcessName: "guard.exe", ExePath: `C:\Windows\System32\guard.exe`, StartedAt: started, IsProtected: true},
			{Port: 8080, PID: 300, ProcessName: "hanxi.exe", ExePath: `D:\tools\hanxi.exe`, StartedAt: started},
		},
	}
	_, store, _ := portkillSetup(t, be, true)
	p, text := prepareOK(t, 8080)
	if p["count"] != float64(4) || p["tokensIssued"] != float64(1) {
		t.Fatalf("只对可杀目标发 token: %s", text)
	}
	items := p["occupants"].([]any)
	byPid := map[float64]map[string]any{}
	for _, raw := range items {
		o := raw.(map[string]any)
		byPid[o["pid"].(float64)] = o
	}
	if tok, _ := byPid[100]["confirmToken"].(string); tok == "" || byPid[100]["killable"] != true {
		t.Error("正常目标应携带 token 且 killable=true")
	}
	for pid, whyWant := range map[float64]string{4: "系统核心", 200: "红线", 300: "黑名单"} {
		if k, _ := byPid[pid]["confirmToken"].(string); k != "" {
			t.Errorf("pid %v 不得发 token", pid)
		}
		if br, _ := byPid[pid]["blockReason"].(string); !strings.Contains(br, whyWant) {
			t.Errorf("pid %v 拒杀归因缺失: %q (want 含 %q)", pid, br, whyWant)
		}
	}
	if store.Pending() != 1 {
		t.Errorf("在途 token 数=1, got %d", store.Pending())
	}

	// 空占用：合法成功态，零 token + 明确 message。
	be.occupants = nil
	p2, _ := prepareOK(t, 9999)
	if p2["count"] != float64(0) {
		t.Fatalf("空占用应为 count=0 成功态: %v", p2)
	}

	// 查询出错 → 指引错误。
	be.queryErr = platform.ErrAccessDenied
	res, text := callPrepare(t, map[string]any{"port": float64(1)})
	if !res.IsError || !strings.Contains(text, "查询失败") {
		t.Fatalf("后端错误必须上抛为指引错误: %s", text)
	}
	// 非法端口。
	res, text = callPrepare(t, map[string]any{"port": float64(70000)})
	if !res.IsError || !strings.Contains(text, "非法") {
		t.Fatalf("端口越界必须拒: %s", text)
	}
	res, _ = callPrepare(t, map[string]any{})
	if !res.IsError {
		t.Fatal("缺 port 参数必须拒")
	}
}

func TestPortkillExecuteArgumentGuards(t *testing.T) {
	be := &fakePortkillBackend{}
	h, _, _ := portkillSetup(t, be, true)
	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{})
	if !res.IsError || !strings.Contains(text, "缺少 token") {
		t.Fatalf("缺参必须给指引: %s", text)
	}
	if h.auditRecord("execute", "error") == nil {
		t.Fatal("缺参拒绝同样必落审计")
	}
	if len(be.kills) != 0 {
		t.Fatal("参数拒绝不得触发查杀")
	}
}

// ---------- 零指纹签发防线（只有 pid 的 token 一律拒杀） ----------

func TestPortkillZeroFingerprintTokenRefused(t *testing.T) {
	be := &fakePortkillBackend{
		proc:       platform.ProcInfo{PID: 777, ExePath: `C:\x\y.exe`},
		killResult: portkill.KillResult{Success: true},
	}
	h, store, _ := portkillSetup(t, be, true)
	tok, _, err := store.Issue(8080, 777, "", time.Time{}) // 签发侧全空
	if err != nil {
		t.Fatal(err)
	}
	res, text := callHandler(t, buildPortkillExecuteTool, map[string]any{"token": tok})
	if !res.IsError || !strings.Contains(text, "拒绝仅凭 PID 查杀") {
		t.Fatalf("零指纹 token 必须拒: %s", text)
	}
	if len(be.kills) != 0 {
		t.Fatal("零指纹不得查杀")
	}
	if h.auditRecord("execute", "aborted_identity") == nil {
		t.Fatal("零指纹拒绝必落审计")
	}
}
