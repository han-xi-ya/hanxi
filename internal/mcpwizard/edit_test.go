package mcpwizard

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDetectClientPaths(t *testing.T) {
	const home = `/home/u`
	noop := func(string) string { return "" }

	cases := []struct {
		id      string
		wantDir string
		wantRel string // 相对目录的文件名（filepath.Join 组合避免分隔符口径纠缠）
		env     map[string]string
	}{
		{"claude", home, ".claude.json", nil},
		{"claude", "/custom/claude", ".claude.json", map[string]string{"CLAUDE_CONFIG_DIR": "/custom/claude"}},
		{"codex", filepath.Join(home, ".codex"), "config.toml", nil},
		{"codex", "/ox", "config.toml", map[string]string{"CODEX_HOME": "/ox"}},
		{"cursor", filepath.Join(home, ".cursor"), "mcp.json", nil},
	}
	for _, c := range cases {
		spec, ok := clientSpecByID(c.id)
		if !ok {
			t.Fatalf("客户端未建档: %s", c.id)
		}
		dir, path := spec.resolve(home, func(k string) string { return c.env[k] })
		if dir != c.wantDir || path != filepath.Join(c.wantDir, c.wantRel) {
			t.Errorf("%s env=%v 解析为 %s/%s，期望 %s/%s", c.id, c.env, dir, path, c.wantDir, filepath.Join(c.wantDir, c.wantRel))
		}
	}
	if _, p := (clientSpec{}).resolve("", noop); p != "" {
		t.Error("home 为空应得到空路径")
	}
}

func TestMergeJSONPreservesForeignKeys(t *testing.T) {
	src := "{\n    \"theme\": \"dark\",\n    \"mcpServers\": {\n        \"other\": {\"command\": \"x\"}\n    },\n    \"count\": 3\n}"
	out, changed, fp, err := mergeJSONEntry([]byte(src), `C:\bin\hanxi.exe`, serverArgs)
	if err != nil || !changed {
		t.Fatalf("merge 失败: changed=%t err=%v", changed, err)
	}
	if fp == "" {
		t.Fatal("指纹为空")
	}
	var top map[string]any
	if err := json.Unmarshal(out, &top); err != nil {
		t.Fatalf("产出非法 JSON: %v", err)
	}
	if top["theme"] != "dark" {
		t.Error("顶层 theme 丢失")
	}
	if top["count"].(float64) != 3 {
		t.Error("顶层 count 丢失")
	}
	servers := top["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("同僚条目 other 丢失")
	}
	entry := servers["hanxi"].(map[string]any)
	if entry["command"] != `C:\bin\hanxi.exe` {
		t.Errorf("command 错: %v", entry["command"])
	}
	args, _ := entry["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Errorf("args 错: %v", entry["args"])
	}
	if !strings.Contains(string(out), "\n    \"") {
		t.Error("缩进未跟随 4 空格")
	}
	// 幂等：再 merge 一次零改动
	out2, changed2, _, err := mergeJSONEntry(out, `C:\bin\hanxi.exe`, serverArgs)
	if err != nil || changed2 || string(out2) != string(out) {
		t.Errorf("重复 merge 非幂等: changed=%t err=%v", changed2, err)
	}
}

func TestMergeJSONIdempotentDespiteKeyOrder(t *testing.T) {
	// 键序不同的语义一致条目：应判零改动
	src := `{"mcpServers":{"hanxi":{"args":["mcp"],"command":"X"}}}`
	out, changed, _, err := mergeJSONEntry([]byte(src), "X", serverArgs)
	if err != nil || changed || string(out) != src {
		t.Errorf("键序归一失败: changed=%t out=%s err=%v", changed, out, err)
	}
}

func TestRemoveJSONOnlyOwnsEntry(t *testing.T) {
	src := "{\n  \"mcpServers\": {\n    \"hanxi\": {\"command\": \"X\", \"args\": [\"mcp\"]},\n    \"other\": {}\n  },\n  \"z\": 1\n}"
	out, changed, err := removeJSONEntry([]byte(src))
	if err != nil || !changed {
		t.Fatalf("remove 失败: changed=%t err=%v", changed, err)
	}
	if strings.Contains(string(out), `"hanxi"`) {
		t.Error("hanxi 条目残留")
	}
	var top map[string]any
	if err := json.Unmarshal(out, &top); err != nil {
		t.Fatal(err)
	}
	servers := top["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("other 被误删")
	}
	// 摘空保留空对象（顶层键不动）
	out2, changed2, err := removeJSONEntry([]byte(`{"mcpServers":{"hanxi":{"command":"X","args":["mcp"]}}}`))
	if err != nil || !changed2 {
		t.Fatalf("摘除最后条目失败: %v", err)
	}
	if !strings.Contains(string(out2), `"mcpServers"`) || strings.Contains(string(out2), "hanxi") {
		t.Errorf("mcpServers 应保留为空对象: %s", out2)
	}
	// 无条目零改动
	if _, ch, _ := removeJSONEntry([]byte(`{"mcpServers":{"a":{}}}`)); ch {
		t.Error("无 hanxi 条目却报改动")
	}
}

func TestJSONCFailClosed(t *testing.T) {
	jsonc := "{\n  // 我的 MCP 配置\n  \"mcpServers\": {}\n}"
	if !hasJSONComments([]byte(jsonc)) {
		t.Fatal("注释未检出")
	}
	if _, err := jsonEntryFingerprint([]byte(jsonc)); !errors.Is(err, errUnsafeJSON) {
		t.Errorf("JSONC 应判 unsafe: %v", err)
	}
	if _, _, _, err := mergeJSONEntry([]byte(jsonc), "X", serverArgs); !errors.Is(err, errUnsafeJSON) {
		t.Errorf("JSONC merge 应拒绝: %v", err)
	}
	// 注释样式出现在字符串值内不算注释
	if hasJSONComments([]byte(`{"url":"http://a//b", "n": "/*x*/"}`)) {
		t.Error("字符串内斜杠误判为注释")
	}
	// BOM 同判 unsafe
	if _, err := jsonEntryFingerprint(append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{}`)...)); !errors.Is(err, errUnsafeJSON) {
		t.Errorf("BOM 应判 unsafe: %v", err)
	}
	// 纯损坏（非注释）也 unsafe
	if _, err := jsonEntryFingerprint([]byte(`{"a":`)); !errors.Is(err, errUnsafeJSON) {
		t.Errorf("损坏应判 unsafe: %v", err)
	}
	// mcpServers 非对象 → unsafe
	if _, err := jsonEntryFingerprint([]byte(`{"mcpServers":[]}`)); !errors.Is(err, errUnsafeJSON) {
		t.Errorf("形状异常应判 unsafe: %v", err)
	}
}

func TestDetectIndentVariants(t *testing.T) {
	if got := detectIndent([]byte("{\n\t\"a\": 1\n}")); got != "\t" {
		t.Errorf("Tab 缩进探测为 %q", got)
	}
	if got := detectIndent([]byte("{\n      \"a\": 1\n}")); got != "      " {
		t.Errorf("6 空格缩进探测为 %q", got)
	}
	if got := detectIndent([]byte(`{"a":1}`)); got != "  " {
		t.Errorf("探测失败应回落两空格，得 %q", got)
	}
}

func TestTomlBlockRoundTrip(t *testing.T) {
	const user = "sandbox_mode = \"workspace-write\"\n"
	out, changed, fp, err := appendTomlBlock([]byte(user), `C:\Program Files\hanxi\hanxi.exe`, serverArgs)
	if err != nil || !changed || fp == "" {
		t.Fatalf("append 失败: %t %v", changed, err)
	}
	text := string(out)
	if !strings.HasPrefix(text, user) {
		t.Error("用户原内容未被原样保留在前部")
	}
	if strings.Count(text, tomlBegin) != 1 || strings.Count(text, tomlEnd) != 1 {
		t.Error("哨兵应各出现一次")
	}
	// 无尾换行的用户内容也要正确分隔
	out2, _, _, err := appendTomlBlock([]byte("x = 1"), "X", serverArgs)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if _, err := toml.Decode(string(out2), &probe); err != nil {
		t.Fatalf("追加后 TOML 解析失败: %v", err)
	}
	// 幂等重放
	out3, changed3, _, err := appendTomlBlock(out, `C:\Program Files\hanxi\hanxi.exe`, serverArgs)
	if err != nil || changed3 || string(out3) != string(out) {
		t.Errorf("重复 append 非幂等: %t %v", changed3, err)
	}
	// 指纹一致提取
	gotFp, err := tomlEntryFingerprint(out)
	if err != nil || gotFp != fp {
		t.Errorf("区块指纹不一致: %v %v", gotFp == fp, err)
	}
	// 精确摘除回到原文件
	back, changed4, err := removeTomlBlock(out)
	if err != nil || !changed4 {
		t.Fatalf("remove 失败: %t %v", changed4, err)
	}
	if string(back) != user {
		t.Errorf("摘除未回到原字节:\n%q\n期望\n%q", back, user)
	}
	// 无区块零改动
	if _, ch, err := removeTomlBlock([]byte(user)); ch || err != nil {
		t.Errorf("无区块应零改动: %t %v", ch, err)
	}
}

func TestTomlBackslashCommandParses(t *testing.T) {
	out, _, _, err := appendTomlBlock(nil, `C:\Users\漢\hanxi.exe`, serverArgs)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if _, err := toml.Decode(string(out), &probe); err != nil {
		t.Fatalf("Windows 反斜杠路径复验失败: %v", err)
	}
	ms, _ := probe["mcp_servers"].(map[string]any)
	h, _ := ms["hanxi"].(map[string]any)
	if h["command"] != `C:\Users\漢\hanxi.exe` {
		t.Errorf("command 回读错: %v", h["command"])
	}
}

func TestTomlSentinelAnomaliesRefused(t *testing.T) {
	// begin 出现两次：卸载与覆盖都必须拒绝
	twice := tomlBegin + "\nx\n" + tomlBegin + "\ny\n" + tomlEnd + "\n"
	if _, err := tomlEntryFingerprint([]byte(twice)); !errors.Is(err, errTomlSentinels) {
		t.Errorf("双 begin 应拒绝: %v", err)
	}
	if _, _, err := removeTomlBlock([]byte(twice)); !errors.Is(err, errTomlSentinels) {
		t.Errorf("双 begin 卸载应拒绝: %v", err)
	}
	// 只有 end 无 begin
	if _, err := tomlEntryFingerprint([]byte("y\n" + tomlEnd + "\n")); !errors.Is(err, errTomlSentinels) {
		t.Errorf("孤立 end 应拒绝: %v", err)
	}
}

func TestTomlDifferentBlockRefused(t *testing.T) {
	data, _, _, err := appendTomlBlock(nil, "OTHER", serverArgs)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := appendTomlBlock(data, "MINE", serverArgs); !errors.Is(err, errTomlSentinels) {
		t.Errorf("已存在不同内容区块时再 append 应拒绝: %v", err)
	}
}

func TestLineDiffBasics(t *testing.T) {
	d := lineDiff("a\nb\nc\n", "a\nB\nc\n")
	var kinds []string
	for _, l := range d {
		kinds = append(kinds, l.Kind+l.Text)
	}
	joined := strings.Join(kinds, "|")
	if !strings.Contains(joined, "delb") || !strings.Contains(joined, "addB") || !strings.Contains(joined, "keepa") {
		t.Errorf("diff 结果异常: %s", joined)
	}
	if len(lineDiff("", "x\n")) != 1 {
		t.Error("空 before 应全为 add")
	}
}
