package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAccess 在临时目录落一份授权文件并返回指向它的 Access。
func writeAccess(t *testing.T, content string) *Access {
	t.Helper()
	path := filepath.Join(t.TempDir(), AccessFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewAccess(path)
}

// TestAccessFailClosedMatrix 穷举 PLAN_MCP §6 的 fail-closed 判定口径：
// 除"合法且显式 true"外，一切形态（缺失/损坏/类型错/版本错/未知字段/超限/尾随）都必须全拒绝。
func TestAccessFailClosedMatrix(t *testing.T) {
	// 九键全集（AI 接入批：portscan/lan 扫描族两键默认关——未授权连"触发扫描"
	// 都不可能发生；端口查杀批：portkill 键入册但仍默认关，且 destructive.json
	// 总闸另判——access 侧放行只是 A1 门之一；未知键仍整体拒读）
	allModules := []string{"envcheck", "everything", "ocr", "memo", "sysinfo", "logs", "portscan", "lan", portkillAccessKey}

	cases := []struct {
		name    string
		content *string // nil = 不创建文件
		want    map[string]bool
	}{
		{name: "文件缺失-默认全关", content: nil, want: map[string]bool{}},
		{name: "全开", content: strPtr(`{"version":1,"tools":{"envcheck":true,"everything":true,"ocr":true,"memo":true,"sysinfo":true,"logs":true,"portscan":true,"lan":true,"portkill":true}}`),
			want: map[string]bool{"envcheck": true, "everything": true, "ocr": true, "memo": true, "sysinfo": true, "logs": true, "portscan": true, "lan": true, portkillAccessKey: true}},
		{name: "仅扫描族开", content: strPtr(`{"version":1,"tools":{"portscan":true,"lan":true}}`),
			want: map[string]bool{"portscan": true, "lan": true}},
		{name: "仅查杀键开-A1单门放行不代表可杀", content: strPtr(`{"version":1,"tools":{"portkill":true}}`),
			want: map[string]bool{portkillAccessKey: true}},
		{name: "拍板示例文本", content: strPtr(`{"version":1,"tools":{"envcheck":true,"everything":false,"ocr":false,"memo":false}}`),
			want: map[string]bool{"envcheck": true}},
		{name: "缺tools键", content: strPtr(`{"version":1}`), want: map[string]bool{}},
		{name: "tools缺某键按false", content: strPtr(`{"version":1,"tools":{"envcheck":true}}`),
			want: map[string]bool{"envcheck": true}},
		{name: "尾随换行仍合法", content: strPtr("{\"version\":1,\"tools\":{\"memo\":true}}\n"),
			want: map[string]bool{"memo": true}},
		{name: "损坏JSON", content: strPtr(`{"version":1,"tools":`), want: map[string]bool{}},
		{name: "空文件", content: strPtr(``), want: map[string]bool{}},
		{name: "version为字符串", content: strPtr(`{"version":"1","tools":{"envcheck":true}}`), want: map[string]bool{}},
		{name: "version为2-未来版本拒绝", content: strPtr(`{"version":2,"tools":{"envcheck":true}}`), want: map[string]bool{}},
		{name: "version缺失", content: strPtr(`{"tools":{"envcheck":true}}`), want: map[string]bool{}},
		{name: "tools为数组", content: strPtr(`{"version":1,"tools":["envcheck"]}`), want: map[string]bool{}},
		{name: "值为字符串", content: strPtr(`{"version":1,"tools":{"envcheck":"yes"}}`), want: map[string]bool{}},
		{name: "未知顶层字段", content: strPtr(`{"version":1,"tools":{"envcheck":true},"extra":1}`), want: map[string]bool{}},
		// portkill 入册九键后不再是"未知键"样本——改拿 destructive.json 侧同款
		// 假想键 format_disk 当样本（未知键整体拒读的连坐语义不变）。
		{name: "未知tools键", content: strPtr(`{"version":1,"tools":{"envcheck":true,"format_disk":true}}`), want: map[string]bool{}},
		{name: "两个JSON拼接", content: strPtr(`{"version":1,"tools":{"envcheck":true}}{"version":1,"tools":{"ocr":true}}`), want: map[string]bool{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a *Access
			if tc.content == nil {
				a = NewAccess(filepath.Join(t.TempDir(), "mcp", AccessFileName)) // 目录都不存在
			} else {
				a = writeAccess(t, *tc.content)
			}
			for _, mod := range allModules {
				if got := a.Allowed(mod); got != tc.want[mod] {
					t.Errorf("Allowed(%q) = %v, want %v", mod, got, tc.want[mod])
				}
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// TestAccessSizeLimit 超 16KB 的授权文件视为异常：全拒绝（防半截写坏/夹带巨量数据形态）。
func TestAccessSizeLimit(t *testing.T) {
	// 前导空白是合法 JSON 但把体积推爆 16KB 上限：必须触发体积判定而非仅解析判定。
	big := fmt.Sprintf("%s{\"version\":1,\"tools\":{\"envcheck\":true}}", strings.Repeat(" ", maxAccessFileSize))
	a := writeAccess(t, big)
	if a.Allowed("envcheck") {
		t.Fatal("oversized access.json must fail closed")
	}
	if a.LastReason() == "" {
		t.Error("deny reason should be recorded for diagnostics")
	}
}

// TestAccessImmediateRevocation 每次调用重读：GUI（测试模拟）改盘即撤权，无需重建会话。
func TestAccessImmediateRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), AccessFileName)
	if err := os.WriteFile(path, []byte(`{"version":1,"tools":{"envcheck":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewAccess(path)
	if !a.Allowed("envcheck") {
		t.Fatal("should be allowed before revoke")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"tools":{"envcheck":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.Allowed("envcheck") {
		t.Fatal("revocation must take effect on next call, no caching")
	}
}
