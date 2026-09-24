//go:build windows

package windows

import (
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
		"1223",
		"已被用户取消",
	}
	for _, out := range cases {
		if !isUACCancelled(out) {
			t.Errorf("应识别 UAC 取消: %q", out)
		}
	}
	if isUACCancelled("目标程序启动失败") {
		t.Error("普通启动失败不得误判为 UAC 取消")
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
