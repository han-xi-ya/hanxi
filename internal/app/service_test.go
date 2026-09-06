package app

import (
	"strings"
	"testing"
)

// TestSanitizeRoute 交接路由门卫：cmd 入口与 RestartElevated RPC 共用同一
// 校验，非法/脏值必须静默丢弃（返回空串退回首页），合法前端路由原样通过。
func TestSanitizeRoute(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/ext/bcu", "/ext/bcu"}, // 扩展模块路由
		{"/ext/litemonitor", "/ext/litemonitor"},
		{"/settings", "/settings"},          // 核心页
		{"  /ext/rufus  ", "/ext/rufus"},    // 前后空白容忍
		{"", ""},                            // 空 → 保持空（默认首页）
		{"/", ""},                           // 裸斜杠不是有效路由
		{"bcu", ""},                         // 缺前导斜杠
		{"/Ext/Bcu", ""},                    // 大写非法（路由全小写约定）
		{"/../../windows", ""},              // 路径逃逸字符
		{"/ext/bcu;calc", ""},               // 命令注入脏字符
		{`/ext/bcu\..\x`, ""},               // 反斜杠
		{"/" + strings.Repeat("a", 70), ""}, // 超长
	}
	for _, c := range cases {
		if got := SanitizeRoute(c.in); got != c.want {
			t.Errorf("SanitizeRoute(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}
