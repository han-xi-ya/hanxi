package ddnsgo

import (
	"reflect"
	"testing"
)

// TestConsoleURLOf 监听地址 → 面板 URL 拼接。
func TestConsoleURLOf(t *testing.T) {
	if got := consoleURLOf("127.0.0.1:9876"); got != "http://127.0.0.1:9876/" {
		t.Fatalf("consoleURLOf = %q", got)
	}
}

// TestExternalConsoleCandidates 外部面板候选：设定端口优先、默认 9876 兜底、去重。
func TestExternalConsoleCandidates(t *testing.T) {
	// 设定端口与默认不同 → 两个候选，设定在前
	if got := externalConsoleCandidates(8080); !reflect.DeepEqual(got,
		[]string{"127.0.0.1:8080", "127.0.0.1:9876"}) {
		t.Errorf("candidates(8080) = %v", got)
	}
	// 设定端口即默认端口 → 去重只剩一个
	if got := externalConsoleCandidates(defaultListenPort); !reflect.DeepEqual(got,
		[]string{"127.0.0.1:9876"}) {
		t.Errorf("candidates(9876) = %v, want 单元素", got)
	}
}

// TestValidateListenPort 端口合法区间：1024~65535 通过，越界拒绝。
func TestValidateListenPort(t *testing.T) {
	for _, p := range []int{1024, 9876, 65535} {
		if err := validateListenPort(p); err != nil {
			t.Errorf("端口 %d 应合法，却报错 %v", p, err)
		}
	}
	for _, p := range []int{0, 80, 1023, 65536, -1} {
		if err := validateListenPort(p); err == nil {
			t.Errorf("端口 %d 应被拒绝", p)
		}
	}
}

// TestVersionCompare 数值分段比较：6.9.0 < 6.10.0（字典序会反），
// 主/次/补丁位逐级生效。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v6.10.0", "v6.9.0", 1},
		{"v6.9.0", "v6.10.0", -1},
		{"v6.17.6", "v6.17.6", 0},
		{"v7.0.0", "v6.99.99", 1},
		{"v6.17.7", "v6.17.6", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
