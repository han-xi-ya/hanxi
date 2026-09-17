package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	evinstance "hanxi/internal/modules/everything/instance"
	evsearch "hanxi/internal/modules/everything/search"
)

// ---------- strictSearcher 前置闸门（决策 3-B 的零副作用面单测） ----------

// TestStrictSearcherGuards 三类前置形态：无实例/缺组件/齐备。
// 全程不触碰真实数据根与进程（snapshotFn/runFn 切点 + 不存在的 esExe 路径）。
func TestStrictSearcherGuards(t *testing.T) {
	missingExe := t.TempDir() + "\\no-such-es.exe"
	ran := false
	mk := func(state evinstance.State, esExe string) *strictSearcher {
		return &strictSearcher{
			esExe: esExe,
			snapshotFn: func() evinstance.State {
				return state
			},
			runFn: func(esExe, query string, limit int) ([]evsearch.Result, error) {
				ran = true
				return []evsearch.Result{{Name: "报告.pdf", Path: `C:\临时目录`}}, nil
			},
		}
	}

	if _, err := mk(evinstance.StateStopped, missingExe).Search("x", 10); !errors.Is(err, errNoInstance) {
		t.Errorf("stopped instance: got %v, want errNoInstance", err)
	}
	if _, err := mk(evinstance.StateFailed, missingExe).Search("x", 10); !errors.Is(err, errNoInstance) {
		t.Errorf("failed instance: got %v, want errNoInstance", err)
	}
	if _, err := mk(evinstance.StateRunning, missingExe).Search("x", 10); !errors.Is(err, errNoSearchTool) {
		t.Errorf("missing es.exe: got %v, want errNoSearchTool", err)
	}
	ran = false
	if _, err := mk(evinstance.StateExternal, t.TempDir()+"/exists-not-check-here").Search("x", 10); !errors.Is(err, errNoSearchTool) {
		t.Errorf("external state without component still blocked: got %v", err)
	}
	// 组件在场（用测试可执行文件冒充正文件）+ 实例在场 → 放行到 runFn
	if _, err := mk(evinstance.StateExternal, selfPath(t)).Search("x", 10); err != nil {
		t.Errorf("ready precondition must reach executor, got err: %v", err)
	}
	if !ran {
		t.Error("runFn not invoked on ready precondition")
	}
}

func selfPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// ---------- 工具面行为（假 Searcher 驱动全链） ----------

type fakeSearcher struct {
	results []evsearch.Result
	err     error
	lastQ   string
	lastLim int
}

func (f *fakeSearcher) Search(query string, limit int) ([]evsearch.Result, error) {
	f.lastQ, f.lastLim = query, limit
	if f.err != nil {
		return nil, f.err
	}
	if len(f.results) < limit {
		return f.results, nil
	}
	return f.results[:limit], nil
}

// TestFileSearchToolRoundTrip 授权后的往返：结果 JSON 信封、默认/钳制 limit、
// 拿满 limit 时的 truncated 语义。
func TestFileSearchToolRoundTrip(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"everything": true})
	fs := &fakeSearcher{results: []evsearch.Result{
		{Name: "中文报告.docx", Path: `D:\项目文档`, Size: 1234, IsDir: false, Modified: "2026-09-01 10:00"},
		{Name: `C:\`, Path: "", IsDir: true},
	}}
	deps.Search = fs
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolSearch, map[string]any{"query": "报告 ext:docx"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", text)
	}
	if fs.lastQ != "报告 ext:docx" || fs.lastLim != 50 {
		t.Errorf("backend got q=%q limit=%d, want 报告 ext:docx/50", fs.lastQ, fs.lastLim)
	}
	var payload struct {
		Count     int `json:"count"`
		Truncated bool
		Results   []evsearch.Result
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	if payload.Count != 2 || len(payload.Results) != 2 {
		t.Fatalf("payload = %s", text)
	}
	// 结果数 2 < 默认 limit 50 → 无截断
	if payload.Truncated {
		t.Errorf("truncated should be false when under limit: %s", text)
	}

	// limit=2 拿满 → truncated=true
	_, text2 := callText(t, c, toolSearch, map[string]any{"query": "x", "limit": 2.0})
	var p2 struct {
		Count     int  `json:"count"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text2), &p2); err != nil {
		t.Fatal(err)
	}
	if p2.Count != 2 || !p2.Truncated {
		t.Errorf("capped-by-limit result must carry truncated=true: %s", text2)
	}
	if fs.lastLim != 2 {
		t.Errorf("explicit limit not forwarded: %d", fs.lastLim)
	}

	// limit 越界钳到 300
	callText(t, c, toolSearch, map[string]any{"query": "x", "limit": 9999.0})
	if fs.lastLim != maxSearchLimit {
		t.Errorf("limit clamp = %d, want %d", fs.lastLim, maxSearchLimit)
	}
}

// TestFileSearchGuidance 缺实例/缺组件的前置错误必须原样透传指引文案（S2 验收面），
// 且缺 query 参数报错不发后端。
func TestFileSearchGuidance(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"everything": true})
	fs := &fakeSearcher{err: errNoInstance}
	deps.Search = fs
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolSearch, map[string]any{"query": "abc"})
	if !res.IsError || !strings.Contains(text, "不会代为启动") {
		t.Fatalf("no-instance guidance must reach client, got: %s", text)
	}

	fs.err = errNoSearchTool
	res, text = callText(t, c, toolSearch, map[string]any{"query": "abc"})
	if !res.IsError || !strings.Contains(text, "不会代为下载") {
		t.Fatalf("no-component guidance must reach client, got: %s", text)
	}

	fs.err = nil
	res, text = callText(t, c, toolSearch, map[string]any{})
	if !res.IsError || !strings.Contains(text, "query") {
		t.Fatalf("missing query must be rejected: %s", text)
	}
}

// TestFileSearchDescriptionHonesty S2 红线：description 文本必须如实声明两个
// "绝不"（不代启动/不代下载）与只读性，防止超前承诺。
func TestFileSearchDescriptionHonesty(t *testing.T) {
	tool, _ := buildEverythingTool(Deps{})
	for _, phrase := range []string{"绝不代为启动", "绝不代为下载", "严格只读"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q (S2 如实文案红线), got: %s", phrase, tool.Description)
		}
	}
}
