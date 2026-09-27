//go:build windows

package windows

import (
	"strings"
	"testing"
)

func TestPsQuote(t *testing.T) {
	if got := PsQuote(`C:\Program Files\$tool\it's.exe`); got != `'C:\Program Files\$tool\it''s.exe'` {
		t.Fatalf("PsQuote = %q", got)
	}
}

// IsUACCancelled 是全仓唯一取消判定实现（portkill/RestartElevated 均已委托），
// 边界必须钉死在强特征上。
func TestIsUACCancelled(t *testing.T) {
	// 正例：ERROR_CANCELLED 在各语言/拼写形态下的真实报错文本都要命中
	yes := []string{
		"Start-Process : This operation was canceled by the user.",
		"The user cancelled the operation.",     // en-GB 双写 l
		"此操作已被用户取消。",                            // zh-CN ShellMsg
		"无法完成操作，因为用户取消了授权 (HRESULT 0x800704C7)", // 仅 HRESULT 强特征
		"操作已被用户取消 (ERROR_CANCELLED 1223)",       // 特征与 1223 并存仍判取消
		"CANCELED BY THE USER",                  // 大小写不敏感
	}
	for _, out := range yes {
		if !IsUACCancelled(out) {
			t.Errorf("应识别 UAC 取消: %q", out)
		}
	}
	// 审查 #7 收紧回归锁：裸 "1223"/泛 "已取消" 会把普通失败误判成取消吞错——
	// 路径不存在/执行策略/ConstrainedLanguage/带 1223 数字字样的真实失败必须走原始错误通道
	no := []string{
		"目标程序启动失败",
		"尺寸 1223 超限",
		"操作已取消（非 UAC）",
		"helper 退出码 1223，目标进程拒绝访问",
		"提权目标不存在: CreateFile C:\\no\\such.exe: The system cannot find the file specified.",
		"无法加载文件，因为在此系统上禁止运行脚本（执行策略）",
		"ConstrainedLanguage 模式下不支持该操作",
		"timeout after 122300ms",
	}
	for _, out := range no {
		if IsUACCancelled(out) {
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

// 提权脚本拼接断言（RestartElevated 委托 RunElevatedDetached 后包内唯一脚本件）：
// 含 $ 与反引号的安装目录、含单引号的工作目录必须原样落进单引号字面量——
// 双引号形态会被 PowerShell 展开 $env 并把反引号当转义符（旧 RestartElevated
// 手拼 `"`+a+`"` 踩过的教训），且整个脚本不得出现任何双引号。
func TestElevatedStartScriptQuoting(t *testing.T) {
	trickyExe := `C:\tools\$env` + "`" + `\hanxi.exe`
	script := elevatedStartScript(trickyExe, `D:\it's work`, []string{`-takeover=4321`, `-route=/ext/bcu`})
	want := `Start-Process -FilePath 'C:\tools\$env` + "`" + `\hanxi.exe'` +
		` -ArgumentList '-takeover=4321', '-route=/ext/bcu'` +
		` -Verb RunAs -WorkingDirectory 'D:\it''s work'`
	if script != want {
		t.Fatalf("脚本拼接失真:\n got %s\nwant %s", script, want)
	}
	if strings.Contains(script, `"`) {
		t.Fatalf("脚本混入双引号字面量（$ 会被 PowerShell 插值）: %s", script)
	}
}

// 审查 P0#2 回归锁：空参数不得渲染 -ArgumentList（PowerShell 对空值段
// 报 Missing an argument、UAC 不弹）；有参数与目录时段完整拼接。
func TestElevatedScriptShapeEmptyArgs(t *testing.T) {
	if got := elevatedStartScript(`C:\x.exe`, "", nil); strings.Contains(got, "-ArgumentList") {
		t.Fatalf("空参数仍渲染 -ArgumentList（必触发参数绑定异常）: %s", got)
	}
	if got := elevatedStartScript(`C:\x.exe`, "", nil); strings.Contains(got, "-WorkingDirectory") {
		t.Fatalf("空目录仍渲染 -WorkingDirectory: %s", got)
	}
	got := elevatedStartScript(`C:\x.exe`, `C:\work dir`, []string{"--purge-standby"})
	if !strings.Contains(got, "-ArgumentList '--purge-standby'") || !strings.Contains(got, `-WorkingDirectory 'C:\work dir'`) {
		t.Fatalf("完整拼接异常: %s", got)
	}
}
