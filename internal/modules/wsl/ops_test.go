package wsl

import (
	"strings"
	"testing"
)

// 提权脚本形状锁：必须 -PassThru 拿进程对象并传播子进程退出码。
// Start-Process 从不设置 $LASTEXITCODE，裸 -Wait 下目标程序失败也返回 0——
// 实机事故（wsl --install -d 失败却回执"操作已执行完毕"）的总闸回归。
func TestBuildElevatedPSPropagatesChildExitCode(t *testing.T) {
	ps := buildElevatedPS("wsl.exe", "--install", "-d", "Ubuntu")
	for _, want := range []string{
		"$p = Start-Process",
		"-Verb RunAs",
		"-Wait",
		"-PassThru",
		"if ($null -ne $p -and $p.ExitCode -ne 0) { exit $p.ExitCode }",
	} {
		if !strings.Contains(ps, want) {
			t.Fatalf("提权脚本缺少 %q:\n%s", want, ps)
		}
	}
	if !strings.Contains(ps, "'wsl.exe'") || !strings.Contains(ps, "'--install','-d','Ubuntu'") {
		t.Fatalf("参数拼装错误:\n%s", ps)
	}
}

// 单引号转义覆盖（白名单值理论上无单引号，防线不拆）。
func TestBuildElevatedPSEscapesQuotes(t *testing.T) {
	ps := buildElevatedPS("tool.exe", "it's")
	if !strings.Contains(ps, "'it''s'") {
		t.Fatalf("单引号未按 PowerShell 规则双写: %s", ps)
	}
}
