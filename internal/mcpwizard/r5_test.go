package mcpwizard

// r5_test.go —— R5 销案核销（见 TestR5ServerGuidanceNowTrue）。
// internal/mcp 为只读领地：本测试仅读其源码文本，不 import、不改写。

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestR5ServerGuidanceNowTrue R5 销案：F4a server.go 的拒权指引承诺
// "请到 hanxi 主窗口「设置 → AI 接入」中开启、授权即时生效无需重启"。
// R6 前该承诺悬空（分区只读）；本测从三方钉死它重新为真：
//  1. server.go 文案在场（只读核验，不改其代码）；
//  2. 「AI 接入」分区确实存在写入口（本包 SetToolAccess 可用，
//     access_readmatch_test.go 已证读者逐键采信其落盘字节）；
//  3. 分区标题字面与指引对得上（读 AiSection.vue 源文本，含真接的写调用）。
func TestR5ServerGuidanceNowTrue(t *testing.T) {
	src := readFile(t, filepath.Join("..", "mcp", "server.go"))
	for _, want := range []string{"设置 → AI 接入", "授权即时生效，无需重启本服务"} {
		if !strings.Contains(src, want) {
			t.Errorf("server.go 指引应含 %q（R5 销案前提）", want)
		}
	}
	view := readFile(t, filepath.Join("..", "..", "frontend", "src", "views", "settings", "AiSection.vue"))
	if !strings.Contains(view, `title="AI 接入"`) {
		t.Error(`分区标题须为「AI 接入」，与 server.go 指引口径对齐`)
	}
	if !strings.Contains(view, "SetToolAccess") {
		t.Error("分区须真接了写入口（SetToolAccess），否则 R5 指引仍是假承诺")
	}
}
