package app

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"空串", "", nil},
		{"仅空白", "   ", nil},
		{"普通多参数", "--port 8080 -v", []string{"--port", "8080", "-v"}},
		{"引号包裹含空格路径", `"C:\Program Files\app.exe" -min`, []string{"C:\\Program Files\\app.exe", "-min"}},
		{"引号内空白保留", `"a  b"`, []string{"a  b"}},
		{"连续空白与制表符", "a   b\tc", []string{"a", "b", "c"}},
		{"尾部无闭合引号宽容处理", `"unclosed tail`, []string{"unclosed tail"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitArgs(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("splitArgs(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}
