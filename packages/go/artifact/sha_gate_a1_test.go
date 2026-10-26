package artifact

// A1 审查批次：Fetch 摘要形状闸并入包级 isSHA256Hex 后的等价性外部锁。
//
// 断言的是可观察事实：坏形状一律在触碰网络前被拒（下载目录零产物 + 报错
// 即形状闸文案）；含大写字母的合法 64 hex 摘要必须放行到网络层（回环死靶
// 连接失败），不得被形状闸误杀。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchSHA256ShapeGate(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.zip")

	// 回环死端口：放行过形状闸后请求立即失败，无需真实外网。
	const deadURL = "http://127.0.0.1:1/pkg.zip"
	const hex64 = "0123456789abcdef0123456789ABCDEF0123456789abcdef0123456789ABCDEF"

	cases := []struct{ name, sha string }{
		{"63位", hex64[:63]},
		{"65位", hex64 + "0"},
		{"非hex字符", strings.Repeat("g", 64)},
		{"内部空白", "0123456789abcdef " + hex64[:63]},
		{"嵌入换行", hex64[:32] + "\n" + hex64[32:]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Fetch(context.Background(), Source{URL: deadURL, SHA256: tc.sha, MaxBytes: 1024}, dest, nil, 5*time.Second)
			if err == nil || !strings.Contains(err.Error(), "十六进制") {
				t.Fatalf("坏形状必须止步于形状闸: %v", err)
			}
			ents, rerr := os.ReadDir(dir)
			if rerr != nil || len(ents) != 0 {
				t.Fatalf("形状闸拒收应零副作用（目录应为空）: entries=%v err=%v", ents, rerr)
			}
		})
	}

	t.Run("大写合法摘要放行至网络层", func(t *testing.T) {
		err := Fetch(context.Background(), Source{URL: deadURL, SHA256: strings.ToUpper(hex64), MaxBytes: 1024}, dest, nil, 3*time.Second)
		if err == nil {
			t.Fatal("死端口应造成网络层失败")
		}
		if strings.Contains(err.Error(), "十六进制") {
			t.Fatalf("大写形态属合法摘要形状，不得被形状闸误杀: %v", err)
		}
	})
}
