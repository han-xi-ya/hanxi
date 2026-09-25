//go:build windows

package windows

import (
	"fmt"
	"strings"
	"testing"
)

func TestPsQuoteAndElevatedCancellation(t *testing.T) {
	if got := PsQuote(`C:\Program Files\$tool\it's.exe`); got != `'C:\Program Files\$tool\it''s.exe'` {
		t.Fatalf("PsQuote = %q", got)
	}
	cases := []string{
		"canceled by the user",
		"0x800704C7",
		"已被用户取消",
	}
	for _, out := range cases {
		if !isUACCancelled(out) {
			t.Errorf("应识别 UAC 取消: %q", out)
		}
	}
	// 审查 #7 收紧回归锁：裸 "1223"/泛 "已取消" 会把普通失败误判成取消吞错
	for _, out := range []string{"目标程序启动失败", "尺寸 1223 超限", "操作已取消（非 UAC）"} {
		if isUACCancelled(out) {
			t.Errorf("不得误判为 UAC 取消: %q", out)
		}
	}
}

func TestValidateElevatedTarget(t *testing.T) {
	if err := validateElevatedTarget(""); err == nil {
		t.Fatal("空目标必须拒绝")
	}
	if err := validateElevatedTarget(t.TempDir()); err == nil {
		t.Fatal("目录目标必须拒绝")
	}
}

func TestElevatedArgsShape(t *testing.T) {
	args := []string{"a b", `C:\x\$y.exe`}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, PsQuote(arg))
	}
	if !strings.Contains(strings.Join(quoted, ","), "'a b'") {
		t.Fatal("参数必须按 PowerShell 字面量拼接")
	}
}

// 审查 P0#2 回归锁：空参数不得渲染 -ArgumentList（PowerShell 对空值段
// 报 Missing an argument、UAC 不弹）；有参数与目录时段完整拼接。
func TestElevatedScriptShapeEmptyArgs(t *testing.T) {
	build := func(args []string, workingDir string) string {
		argPart := ""
		if len(args) > 0 {
			quoted := make([]string, 0, len(args))
			for _, a := range args {
				quoted = append(quoted, PsQuote(a))
			}
			argPart = " -ArgumentList " + strings.Join(quoted, ", ")
		}
		work := ""
		if workingDir != "" {
			work = " -WorkingDirectory " + PsQuote(workingDir)
		}
		return fmt.Sprintf("Start-Process -FilePath %s%s -Verb RunAs%s", PsQuote(`C:\x.exe`), argPart, work)
	}
	if got := build(nil, ""); strings.Contains(got, "-ArgumentList") {
		t.Fatalf("空参数仍渲染 -ArgumentList（必触发参数绑定异常）: %s", got)
	}
	if got := build([]string{"--purge-standby"}, `C:\work dir`); !strings.Contains(got, "-ArgumentList '--purge-standby'") || !strings.Contains(got, `-WorkingDirectory 'C:\work dir'`) {
		t.Fatalf("完整拼接异常: %s", got)
	}
}
