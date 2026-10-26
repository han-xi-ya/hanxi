package platform

import (
	"errors"
	"fmt"
	"testing"
)

// TestFamilyStringRenderedInWrappedError 断言外部可观察事实：经 %s 与 %v
// 渲染（含错误包装现场，portkill 事故形态复刻）不再出现 %!s(int=N) 乱码。
func TestFamilyStringRenderedInWrappedError(t *testing.T) {
	fam := FamilyIPv4
	err := fmt.Errorf("TCP/%s: %w", fam, errors.New("boom"))
	want := "TCP/IPv4: boom"
	if err.Error() != want {
		t.Fatalf("包装错误渲染失真: got %q want %q", err.Error(), want)
	}
	if got := fmt.Sprintf("%v", FamilyIPv6); got != "IPv6" {
		t.Fatalf("IPv6 渲染失真: %q", got)
	}
	if got := fmt.Sprintf("%s", Family(2)); got != "Family(2)" {
		t.Fatalf("未知族应显式自述而非 panic/乱码: %q", got)
	}
}
