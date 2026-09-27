package clipboard

import (
	"testing"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

// TestClipUtf16BytesToString CF_UNICODETEXT 解码纯函数面：UTF-16LE 字节 →
// Go 字符串（截断到首个 NUL，含尾 NUL 缓冲原样兼容）、空/奇数字节防御。
func TestClipUtf16BytesToString(t *testing.T) {
	units, err := xw.UTF16FromString("hello 你好\r\nnext")
	if err != nil {
		t.Fatal(err)
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&units[0])), len(units)*2)
	if got := utf16BytesToString(raw); got != "hello 你好\r\nnext" {
		t.Fatalf("往返失真: %q", got)
	}

	// 中段 NUL 之后内容按 Win32 文本语义截断（UTF16FromString 拒绝嵌入 NUL，手工构造缓冲）
	unitsWithGap := []uint16{'a', 0, 'b', 0}
	rawGap := unsafe.Slice((*byte)(unsafe.Pointer(&unitsWithGap[0])), len(unitsWithGap)*2)
	if got := utf16BytesToString(rawGap); got != "a" {
		t.Fatalf("NUL 截断语义异常: %q", got)
	}

	if utf16BytesToString(nil) != "" || utf16BytesToString([]byte{1}) != "" {
		t.Fatal("空/奇数字节输入应得空串")
	}
}

// TestClipCaptureShape 捕获载荷与格式常量对位契约 §5 读序（纯常量核对，
// 裸 Win32 消息泵/句柄路径不在单测覆盖面内，由真机冒烟验收）。
func TestClipCaptureShape(t *testing.T) {
	if cfUnicodeText != 13 || cfDIB != 8 || cfHDROP != 15 {
		t.Fatalf("剪贴板格式码位漂移: %d/%d/%d", cfUnicodeText, cfDIB, cfHDROP)
	}
	if wmClipboardUpdate != 0x031D {
		t.Fatal("WM_CLIPBOARDUPDATE 码位漂移")
	}
	if clipOpenAttempts != 6 {
		t.Fatal("开板重试口径漂移（1 正试 + ≤5 重试）")
	}
}
