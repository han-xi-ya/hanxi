//go:build windows

package version

import (
	"strings"
	"testing"
)

// TestDecodeInstallerExit 三类语义必须各归各位：0xC0000005 崩溃不得再被
// 兜底文案（"文件占用/退出实例"）误报；小码走 Win32 语义；未知大码至少
// 给出十六进制 NTSTATUS 便于搜索定位。
func TestDecodeInstallerExit(t *testing.T) {
	crash := decodeInstallerExit(3221225477) // 0xC0000005，真机实录的裸码
	if !strings.Contains(crash, "0xC0000005") || !strings.Contains(crash, "崩溃") {
		t.Fatalf("0xC0000005 应识别为访问违例崩溃，got %q", crash)
	}
	if strings.Contains(crash, "正在运行") {
		t.Fatalf("崩溃语义不得混入文件占用猜测，got %q", crash)
	}

	if got := decodeInstallerExit(1223); !strings.Contains(got, "UAC") {
		t.Fatalf("1223 应指向提权取消，got %q", got)
	}
	if got := decodeInstallerExit(2); !strings.Contains(got, "正在运行的 Recordly") {
		t.Fatalf("小码应保留文件占用兜底指引，got %q", got)
	}
	if got := decodeInstallerExit(0xC0000409); !strings.Contains(got, "0xc0000409") && !strings.Contains(got, "0xC0000409") {
		t.Fatalf("未知 NTSTATUS 应带十六进制码，got %q", got)
	}
}
