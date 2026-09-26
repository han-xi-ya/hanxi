package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/modules/envcheck/detect"
)

// ---------- 测试假件 ----------

type fakeEnvChecker struct {
	tools []detect.ToolInfo
	calls int
}

func (f *fakeEnvChecker) DetectAll() ([]detect.ToolInfo, error) {
	f.calls++
	return f.tools, nil
}

type fakeGate struct {
	mu       sync.Mutex
	enabled  map[string]bool
	checkErr map[string]error
	checks   map[string]int
	releases map[string]int
}

func newFakeGate(ids ...string) *fakeGate {
	g := &fakeGate{enabled: map[string]bool{}, checkErr: map[string]error{}, checks: map[string]int{}, releases: map[string]int{}}
	for _, id := range ids {
		g.enabled[id] = true
	}
	return g
}

// Check 模拟带租约的门禁：成功返回计数 release，失败返回 nil release
// （与 ModuleGate 契约一致：middleware 必须在 release != nil 时 defer）。
func (g *fakeGate) Check(moduleID string) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checks[moduleID]++
	if err, ok := g.checkErr[moduleID]; ok {
		return nil, err
	}
	if !g.enabled[moduleID] {
		return nil, fmt.Errorf("模块「%s」已在 hanxi 中停用", moduleID)
	}
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.releases[moduleID]++
	}, nil
}

// newTestServer 组装进程内可测的全链 server：临时 access 文件 + 假件后端。
func newTestServer(t *testing.T) (Deps, *Access, *fakeEnvChecker) {
	t.Helper()
	env := &fakeEnvChecker{tools: []detect.ToolInfo{
		{Name: "git", Display: "Git", Status: detect.StatusInstalled, Version: "2.45.0"},
		{Name: "node", Display: "Node.js", Status: detect.StatusMissing,
			Hint: `未在 PATH 中找到 Node.js（示例敏感串 token=supersecretvalue）`},
	}}
	access := NewAccess(t.TempDir() + "/access.json") // 初始不存在：fail-closed 全拒绝
	deps := Deps{
		Access: access,
		// 门禁默认全开，授权层单独测；portkill 在册=破坏族已过模块启用门（A1 之 A2
		// 侧另由 destructive.json 把关，与本表无关）。
		Gate:     newFakeGate("envcheck", "everything", "ocr", "memo", "sysinfo", "logs", "portscan", "lan", portkillAccessKey),
		EnvCheck: env,
	}
	return deps, access, env
}

func grant(t *testing.T, access *Access, tools map[string]bool) {
	t.Helper()
	parts := make([]string, 0, len(tools))
	for k, v := range tools {
		parts = append(parts, fmt.Sprintf("%q:%v", k, v))
	}
	doc := fmt.Sprintf(`{"version":1,"tools":{%s}}`, strings.Join(parts, ","))
	if err := os.WriteFile(access.Path(), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---------- 进程内往返（initialize → tools/list → tools/call） ----------

func inProcClient(t *testing.T, deps Deps) *client.Client {
	t.Helper()
	srv := NewMCPServer(deps)
	c, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "hanxi-test", Version: "0.0"}
	if _, err := c.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func callText(t *testing.T, c *client.Client, tool string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = args
	res, err := c.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			text = tc.Text
		}
	}
	return res, text
}

// TestToolSurfaceReadOnly 工具面全量列举与注解审计。
//
// 只读红线（包注释决策 1 升版前形态）：名称白名单、描述非空中文且含"只读"、
// readOnlyHint=true、destructiveHint=false，且绝不出现任何写动词。
//
// 破坏性族显式升版条款（guarded.go 四道闸设计为唯一依据）：豁免**只认
// destructiveFamilies 族登记表**——不在表上、靠散落的字符串匹配放宽一律视为
// 红线漂移。表内成员（每族恰好 prepare+execute 两件）免于上述"正向只读断言 +
// 写动词禁令"（族名自带 kill 语义，正是要如实申报而非藏名字），换得反向强制：
//   - execute 侧：readOnlyHint 必须 false（不得伪装只读）、destructiveHint 必须
//     true（如实申报杀伤面）、描述绝不得含"只读"字样；
//   - prepare 侧：本体确实只读，仍受正向断言（readOnlyHint=true、destructiveHint=false、
//     描述含"只读"）；
//   - 族成对断言：登记表上每族两件都必须在 tools/list 出现，且工具面上不得出现
//     族表之外的破坏性名称（双向钉死，豁免面不可扩大）。
//
// 扫描族既有注解守卫（openWorldHint=true/idempotentHint=false）不回归。
func TestToolSurfaceReadOnly(t *testing.T) {
	deps, _, _ := newTestServer(t)
	c := inProcClient(t, deps)

	list, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != len(toolDefs) {
		t.Fatalf("tool count = %d, want %d", len(list.Tools), len(toolDefs))
	}
	wantNames := map[string]bool{}
	for _, def := range toolDefs {
		wantNames[def.Name] = true
	}
	// 族成对断言（正向）：登记的每族两件必须都已在册——半族注册=二段式被拆，红线。
	for _, f := range destructiveFamilies {
		if !wantNames[f.Prepare] || !wantNames[f.Execute] {
			t.Errorf("破坏族 %v 必须成对注册进 toolDefs", f)
		}
	}
	forbidden := []string{"install", "delete", "remove", "kill", "open", "start",
		"stop", "set", "write", "launch", "quit", "shutdown", "create", "update", "run"}
	for _, tool := range list.Tools {
		if !wantNames[tool.Name] {
			t.Errorf("unexpected tool %q (白名单漂移)", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q description must be non-empty 中文", tool.Name)
		}
		if isDestructiveToolName(tool.Name) {
			// 破坏族成员：豁免只来自族登记表；表外出现即上面 wantNames 已红。
			if isDestructiveExecuteName(tool.Name) {
				if tool.Annotations.ReadOnlyHint == nil || *tool.Annotations.ReadOnlyHint {
					t.Errorf("execute tool %q must carry readOnlyHint=false（不得伪装只读）", tool.Name)
				}
				if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
					t.Errorf("execute tool %q must carry destructiveHint=true（如实申报杀伤面）", tool.Name)
				}
				if strings.Contains(tool.Description, "只读") {
					t.Errorf("execute tool %q description must NOT claim 只读: %q", tool.Name, tool.Description)
				}
			} else {
				// prepare：无杀伤力，正向只读断言照常。
				if !strings.Contains(tool.Description, "只读") {
					t.Errorf("prepare tool %q description must mention 只读: %q", tool.Name, tool.Description)
				}
				if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
					t.Errorf("prepare tool %q must carry readOnlyHint=true", tool.Name)
				}
				if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
					t.Errorf("prepare tool %q must carry destructiveHint=false", tool.Name)
				}
			}
			continue
		}
		lower := strings.ToLower(tool.Name)
		for _, verb := range forbidden {
			if strings.Contains(lower, verb) {
				t.Errorf("tool %q contains forbidden write-verb %q", tool.Name, verb)
			}
		}
		if !strings.Contains(tool.Description, "只读") {
			t.Errorf("tool %q description must be 中文 and mention 只读: %q", tool.Name, tool.Description)
		}
		if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q must carry readOnlyHint=true", tool.Name)
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Errorf("tool %q must carry destructiveHint=false", tool.Name)
		}
		// 扫描族注解必须诚实表达"主动出网探测"：openWorldHint=true +
		// idempotentHint=false（同一请求两次结果可能不同）。查询族维持既有注解
		// 纪律（readOnly/destructive 已由上方断言；openWorld 缺省为库默认值，
		// 既有工具零改动红线内不动它）。
		if strings.HasSuffix(tool.Name, "_scan") {
			if tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
				t.Errorf("scan tool %q must carry openWorldHint=true（主动探测必须诚实标注）", tool.Name)
			}
			if tool.Annotations.IdempotentHint == nil || *tool.Annotations.IdempotentHint {
				t.Errorf("scan tool %q must carry idempotentHint=false", tool.Name)
			}
		}
	}
	// 族成对断言（反向）：未登记的成员绝不得混进豁免逻辑（防豁免面扩大，与
	// guarded_test 的族不变量自检互补）。
	for _, name := range []string{"hanxi_portscan_start", "hanxi_portkill_run", toolEnvCheck} {
		if isDestructiveToolName(name) {
			t.Errorf("%q 不得被认作破坏族成员（族登记表是唯一豁免来源）", name)
		}
	}
}

// TestUnauthorizedFailClosed access.json 缺失（默认全关）时：工具仍在列表中（可发现性），
// 但调用必须返回指引错误且后端零触发。
func TestUnauthorizedFailClosed(t *testing.T) {
	deps, _, env := newTestServer(t)
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolEnvCheck, nil)
	if !res.IsError {
		t.Fatalf("call without grant must be error, got: %s", text)
	}
	if !strings.Contains(text, "未获授权") || !strings.Contains(text, "AI 接入") {
		t.Errorf("error should guide to 设置→AI 接入, got: %s", text)
	}
	if env.calls != 0 {
		t.Errorf("denied call must not touch backend, calls=%d", env.calls)
	}
}

// TestAllToolsUnauthorizedMatrix 全工具 fail-closed 矩阵：无授权文件时全部工具
// 逐一调用都必须指引错误且各自后端零触发（撤权=即时生效已由 access 层单测保证）。
// 便签族两件同键——memo 未授权时检索与统计都必须拦住；portkill 族两件同键——
// A1 未放行时连 handler 都不进（审计零落=handler 未执行的物证），且指引文案必须
// 如实指向两文件手动开启路径（刻意不提"面板开关"——portkill 键不进面板是既定决策）。
func TestAllToolsUnauthorizedMatrix(t *testing.T) {
	deps, _, env := newTestServer(t)
	fs := &fakeSearcher{}
	fr := &fakeRecognizer{}
	fm := &fakeMemo{}
	fi := &fakeReportSource{}
	fl := &fakeLogTailer{}
	fp := &fakePortProber{}
	fl2 := &fakeLanProber{}
	deps.Search, deps.OCR, deps.Memo, deps.SysInfo, deps.Logs = fs, fr, fm, fi, fl
	deps.PortScan, deps.Lan = fp, fl2
	SetPortkillWiring(nil) // 显式未接线态：若有任何路径溜进 handler，审计会留下 unwired 物证
	h := installAuditCapture(t)
	c := inProcClient(t, deps)

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{toolEnvCheck, nil},
		{toolSearch, map[string]any{"query": "x"}},
		{toolOCR, map[string]any{"path": `C:\a.png`}},
		{toolMemo, nil},
		{toolMemoStats, nil},
		{toolSysInfo, nil},
		{toolLogs, nil},
		{toolPortScan, map[string]any{"target": "192.168.1.1", "ports": "22,80"}},
		{toolLanScan, map[string]any{"target": "192.168.1.0/24"}},
		{toolPortkillPrepare, map[string]any{"port": float64(8080)}},
		{toolPortkillExecute, map[string]any{"token": "irrelevant-without-grant"}},
	} {
		res, text := callText(t, c, tc.tool, tc.args)
		if !res.IsError || !strings.Contains(text, "未获授权") {
			t.Errorf("%s: must deny without grant: %s", tc.tool, text)
		}
		if isDestructiveToolName(tc.tool) {
			if !strings.Contains(text, "destructive.json") || !strings.Contains(text, "access.json") {
				t.Errorf("%s: 破坏族 A1 拒绝必须指向两文件手动路径: %s", tc.tool, text)
			}
			if strings.Contains(text, "「设置 → AI 接入」中开启") {
				t.Errorf("%s: 破坏族指引不得误导向无该开关的面板: %s", tc.tool, text)
			}
		}
	}
	// 扫描族被拒时后端（含 CountTargets 预检）必须零触发——"默认不放开触发扫描"
	// 的回归锚：未授权连"解析目标规模"都不发生，更不碰网络。
	if env.calls != 0 || fs.lastQ != "" || fr.calls != 0 || len(fm.items) != 0 ||
		fi.calls != 0 || fl.calls != 0 || fp.calls != 0 || fl2.countCalls != 0 || fl2.scanCalls != 0 {
		t.Errorf("denied tools must not touch backends")
	}
	// 破坏族被 A1 拒时 handler 一步不进：审计零落（连 unwired 都不该有）——
	// "默认关闸零杀伤"的最硬物证。
	if got := h.auditOutcomes(""); len(got) != 0 {
		t.Errorf("未授权 portkill 调用不得进入 handler（审计应零落），got=%v", got)
	}
}

// TestAuthorizedCallAndRedact 授权后 envcheck 正常返回；输出过 Redact 口径
// （敏感串绝不出网——这是"会进云端模型上下文"红线的回归锚点）。
func TestAuthorizedCallAndRedact(t *testing.T) {
	deps, access, env := newTestServer(t)
	grant(t, access, map[string]bool{"envcheck": true})
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolEnvCheck, nil)
	if res.IsError {
		t.Fatalf("authorized call failed: %s", text)
	}
	if env.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", env.calls)
	}
	if strings.Contains(text, "supersecretvalue") {
		t.Errorf("redaction must scrub secret from model-visible payload: %s", text)
	}
	var payload struct {
		Count     int `json:"count"`
		Truncated bool
		Results   []detect.ToolInfo
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("result must be JSON: %v\n%s", err, text)
	}
	if payload.Count != 2 || len(payload.Results) != 2 {
		t.Fatalf("count = %d, want 2 (%s)", payload.Count, text)
	}
}

// TestGateModuleDisabled 授权通过但模块停用：报错给"启用模块"指引，后端零触发。
func TestGateModuleDisabled(t *testing.T) {
	deps, access, env := newTestServer(t)
	grant(t, access, map[string]bool{"envcheck": true})
	deps.Gate = newFakeGate() // 全停用
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolEnvCheck, nil)
	if !res.IsError || !strings.Contains(text, "停用") {
		t.Fatalf("disabled module must error with guidance, got: %s", text)
	}
	if env.calls != 0 {
		t.Errorf("gate-blocked call must not touch backend")
	}
}

// TestUnknownToolRejectedAtProtocol 列表外工具名请求：协议层直接 not-found
// （闸门之前即拒——"tools/list 里没有的东西永远调不到"是编译期事实）。
func TestUnknownToolRejectedAtProtocol(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"envcheck": true})
	c := inProcClient(t, deps)

	req := mcp.CallToolRequest{}
	req.Params.Name = "hanxi_run_any_command"
	if _, err := c.CallTool(context.Background(), req); err == nil {
		t.Fatal("unknown tool call must fail at protocol level")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------- stdio 帧级往返：stdout 只允许 JSON-RPC 帧（污染守卫的实弹版） ----------

// TestStdioFrameLoop 走真实帧通道（newline-delimited JSON-RPC）：
// 服务端输出逐行必须是合法 JSON-RPC 帧，处理途中后端乱写 stderr 不受影响；
// 这是对"stdout 即协议"纪律的端到端断言（mcp-go 走 io 注入，不碰 os.Stdout）。
func TestStdioFrameLoop(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"envcheck": true})

	srv := NewMCPServer(deps)
	stdio := server.NewStdioServer(srv)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0)) // 协议帧流是断言对象，噪声日志丢弃

	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	listenErr := make(chan error, 1)
	go func() {
		err := stdio.Listen(context.Background(), inReader, outWriter)
		_ = outWriter.Close() // Listen 退出即关写端，readAll 才能收到 EOF（否则 Scanner 永久阻塞）
		listenErr <- err
	}()

	go func() {
		writeFrame(inWriter, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+mcp.LATEST_PROTOCOL_VERSION+`","capabilities":{},"clientInfo":{"name":"framing-test","version":"0"}}}`)
		writeFrame(inWriter, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		writeFrame(inWriter, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
		writeFrame(inWriter, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"`+toolEnvCheck+`","arguments":{}}}`)
		_ = inWriter.Close()
	}()

	// 兜底超时：管道任一环节卡死时给出明确失败而非 10 分钟包级 panic。
	type frameRead struct {
		lines []string
		err   error
	}
	linesCh := make(chan frameRead, 1)
	go func() {
		ls, rerr := readAll(outReader)
		linesCh <- frameRead{ls, rerr}
	}()
	var lines []string
	select {
	case got := <-linesCh:
		if got.err != nil {
			t.Fatalf("read stdout stream: %v", got.err)
		}
		lines = got.lines
	case <-time.After(60 * time.Second):
		t.Fatal("stdio 帧通道未在 60s 内收尾（Listen/管道死锁？）")
	}
	var responses []map[string]any
	for _, line := range lines {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("stdout pollution: non-JSON-RPC line %q: %v", line, err)
		}
		if frame["jsonrpc"] != "2.0" {
			t.Fatalf("stdout pollution: frame without jsonrpc 2.0: %q", line)
		}
		if frame["id"] != nil {
			responses = append(responses, frame)
		}
	}
	if len(responses) != 3 {
		t.Fatalf("responses = %d (want 3: initialize, tools/list, tools/call); lines=%v", len(responses), lines)
	}
	// id1 initialize → serverInfo.name=hanxi
	si, _ := responses[0]["result"].(map[string]any)
	info, _ := si["serverInfo"].(map[string]any)
	if info["name"] != "hanxi" {
		t.Errorf("initialize serverInfo = %v", si["serverInfo"])
	}
	// id3 tools/call → 非 error、文本 JSON 含 git
	call := responses[2]
	res, _ := call["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("framed tools/call errored: %v", res)
	}
	content, _ := res["content"].([]any)
	tc, _ := content[0].(map[string]any)
	text, _ := tc["text"].(string)
	if !strings.Contains(text, `"git"`) {
		t.Errorf("framed result missing payload: %s", text)
	}
	if err := <-listenErr; err != nil && !strings.Contains(err.Error(), "EOF") {
		t.Logf("stdio Listen exit: %v", err) // 正常收尾形态差异不作失败断言
	}
}

func TestTextResultOversizeAlwaysReturnsBoundedJSON(t *testing.T) {
	payload := resultPayload{
		"ok":   true,
		"text": strings.Repeat("你\x00\n\t\\\"", maxPayloadBytes),
	}
	res, err := textResult(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(res.Content))
	}
	content, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want mcp.TextContent", res.Content[0])
	}
	data := []byte(content.Text)
	if len(data) > maxPayloadBytes {
		t.Fatalf("payload size = %d, want <= %d", len(data), maxPayloadBytes)
	}
	if !utf8.Valid(data) {
		t.Fatal("payload must be valid UTF-8")
	}
	if !json.Valid(data) {
		t.Fatalf("payload must remain valid JSON: %q", content.Text)
	}
	var envelope struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error == "" || !envelope.Truncated {
		t.Fatalf("oversize envelope = %+v", envelope)
	}
}

func writeFrame(w io.Writer, frame string) {
	_, _ = io.WriteString(w, frame+"\n")
}

// readAll 读干协议输出流（EOF 或显式失败）。跑在独立 goroutine，故返回 error 交主测试协程断言。
func readAll(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines = append(lines, sc.Text())
		}
	}
	if err := sc.Err(); err != nil && !strings.Contains(err.Error(), "io: read/write on closed pipe") {
		return lines, err
	}
	return lines, nil
}
