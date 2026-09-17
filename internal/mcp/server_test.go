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

func (f *fakeEnvChecker) DetectAll() []detect.ToolInfo {
	f.calls++
	return f.tools
}

type fakeGate struct {
	mu       sync.Mutex
	enabled  map[string]bool
	checkErr map[string]error
	checks   map[string]int
}

func newFakeGate(ids ...string) *fakeGate {
	g := &fakeGate{enabled: map[string]bool{}, checkErr: map[string]error{}, checks: map[string]int{}}
	for _, id := range ids {
		g.enabled[id] = true
	}
	return g
}

func (g *fakeGate) Check(moduleID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checks[moduleID]++
	if err, ok := g.checkErr[moduleID]; ok {
		return err
	}
	if !g.enabled[moduleID] {
		return fmt.Errorf("模块「%s」已在 hanxi 中停用", moduleID)
	}
	return nil
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
		Access:   access,
		Gate:     newFakeGate("envcheck", "everything", "ocr", "memo"), // 门禁默认全开，授权层单独测
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

// TestToolSurfaceReadOnly 工具面全量列举与只读注解：名称白名单、描述中文、
// readOnlyHint=true、destructiveHint=false，且绝不出现任何写动词（红线静态断言）。
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
	forbidden := []string{"install", "delete", "remove", "kill", "open", "start",
		"stop", "set", "write", "launch", "quit", "shutdown", "create", "update", "run"}
	for _, tool := range list.Tools {
		if !wantNames[tool.Name] {
			t.Errorf("unexpected tool %q (白名单漂移)", tool.Name)
		}
		lower := strings.ToLower(tool.Name)
		for _, verb := range forbidden {
			if strings.Contains(lower, verb) {
				t.Errorf("tool %q contains forbidden write-verb %q", tool.Name, verb)
			}
		}
		if tool.Description == "" || !strings.Contains(tool.Description, "只读") {
			t.Errorf("tool %q description must be non-empty 中文 and mention 只读: %q", tool.Name, tool.Description)
		}
		if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q must carry readOnlyHint=true", tool.Name)
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Errorf("tool %q must carry destructiveHint=false", tool.Name)
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

// TestAllToolsUnauthorizedMatrix 全工具 fail-closed 矩阵：无授权文件时四件工具
// 逐一调用都必须指引错误且各自后端零触发（撤权=即时生效已由 access 层单测保证）。
func TestAllToolsUnauthorizedMatrix(t *testing.T) {
	deps, _, env := newTestServer(t)
	fs := &fakeSearcher{}
	fr := &fakeRecognizer{}
	fm := &fakeMemo{}
	deps.Search, deps.OCR, deps.Memo = fs, fr, fm
	c := inProcClient(t, deps)

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{toolEnvCheck, nil},
		{toolSearch, map[string]any{"query": "x"}},
		{toolOCR, map[string]any{"path": `C:\a.png`}},
		{toolMemo, nil},
	} {
		res, text := callText(t, c, tc.tool, tc.args)
		if !res.IsError || !strings.Contains(text, "未获授权") {
			t.Errorf("%s: must deny without grant: %s", tc.tool, text)
		}
	}
	if env.calls != 0 || fs.lastQ != "" || fr.calls != 0 || len(fm.items) != 0 {
		t.Errorf("denied tools must not touch backends")
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
