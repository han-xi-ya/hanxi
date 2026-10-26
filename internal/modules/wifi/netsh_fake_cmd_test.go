// netsh_fake_cmd_test.go 通过 PATH 前置的 netsh.cmd 假命令驱动真实生产链路
// （runCommandSilent → getSavedSSIDs → getPasswordForSSID → GetAllWiFiPasswords），
// 断言"给定 netsh 实况输出 → 实际解析结果"，而非在测试里复刻解析逻辑。
// 中文标记行经 `type` 原样字节透传（cmd echo 走系统 ANSI 代码页会破坏 UTF-8，
// type 是二进制复制，不受控制台代码页影响），假脚本内仅按纯 ASCII 段做分支。
package wifi_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/wifi"
)

// stageFakeNetsh 在临时目录落一个 netsh.cmd（含数据片段文件）并前置到 PATH，
// 使 exec.Command("netsh", ...) 经 PATHEXT 命中假脚本而非 System32 的真 netsh。
// t.Setenv 在测试结束后自动还原 PATH；本包用例因此不得并行。
func stageFakeNetsh(t *testing.T, script string, data map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range data {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("写假 netsh 数据片失败: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "netsh.cmd"), []byte(script), 0o644); err != nil {
		t.Fatalf("写假 netsh.cmd 失败: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stripNetsh 把 PATH 换成只含空目录，使 "netsh" 根本解析不到——驱动错误分支。
func stripNetsh(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// fakeNetshScript 复刻 netsh wlan 的两种调用形态：
// show profiles → 输出名单；show profile name=X key=clear → 按 X 分支给密码/缺键/失败。
// 注意批处理参数以 '=' 分词：name=NoKey 会拆成 %4=name %5=NoKey。
const fakeNetshScript = `@echo off
if /I "%~3"=="profiles" (
  type "%~dp0profiles.txt"
  exit /b 0
)
if /I "%~3"=="profile" (
  if /I "%~5"=="NoKey" (
    type "%~dp0nokey.txt"
    exit /b 0
  )
  if /I "%~5"=="EnKey" (
    type "%~dp0key_en.txt"
    exit /b 0
  )
  if /I "%~5"=="Fails" (
    echo Profile "Fails" query failed 1>&2
    exit /b 1
  )
  type "%~dp0key_cn.txt"
  exit /b 0
)
echo unexpected args %* 1>&2
exit /b 9
`

var fakeNetshData = map[string]string{
	"profiles.txt": "    所有用户配置文件 : 家用WiFi\r\n" +
		"    All User Profile     : Cafe:Special\r\n" +
		"    All User Profile     : EnKey\r\n" +
		"    All User Profile     : NoKey\r\n" +
		"    All User Profile     : Fails\r\n",
	"key_cn.txt": "        关键内容            : pw:123\r\n",
	"key_en.txt": "        Key Content            : secret-456\r\n",
	"nokey.txt":  "    安全设置            : WPA2-PSK\r\n    身份验证            : WPA2-PSK\r\n",
}

const placeholder = "未设置密码或无法读取"

// TestGetAllWiFiPasswordsParsesFakeNetsh 钉死整条真实链路的对外返回值：
// 中/英名单标记、SSID 内冒号保留、中/英密码标记、缺键与查询失败两种兜底。
func TestGetAllWiFiPasswordsParsesFakeNetsh(t *testing.T) {
	stageFakeNetsh(t, fakeNetshScript, fakeNetshData)

	got, err := wifi.GetAllWiFiPasswords()
	if err != nil {
		t.Fatalf("GetAllWiFiPasswords 出错: %v", err)
	}
	want := []wifi.Profile{
		{SSID: "家用WiFi", Password: "pw:123"},
		{SSID: "Cafe:Special", Password: "pw:123"},
		{SSID: "EnKey", Password: "secret-456"},
		{SSID: "NoKey", Password: placeholder},
		{SSID: "Fails", Password: placeholder},
	}
	if len(got) != len(want) {
		t.Fatalf("配置文件数 = %d (%+v), want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("第 %d 条 = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestGetAllWiFiPasswordsNoProfiles 名单里无可识别行/空名行 → 空列表而非报错。
func TestGetAllWiFiPasswordsNoProfiles(t *testing.T) {
	stageFakeNetsh(t, fakeNetshScript, map[string]string{
		"profiles.txt": "Group policy profiles (read only)\r\n" +
			"---------------------------------\r\n" +
			"    <None>\r\n" +
			"    All User Profile     :\r\n" +
			"    所有用户配置文件 : \t \r\n",
	})
	got, err := wifi.GetAllWiFiPasswords()
	if err != nil {
		t.Fatalf("无配置不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("应解析为空列表，实际 %+v", got)
	}
}

// TestGetAllWiFiPasswordsNetshFailure 枚举命令非零退出 → 如实上抛错误。
func TestGetAllWiFiPasswordsNetshFailure(t *testing.T) {
	stageFakeNetsh(t, `@echo off
echo boom 1>&2
exit /b 3
`, nil)
	if _, err := wifi.GetAllWiFiPasswords(); err == nil {
		t.Fatal("netsh 非零退出应返回错误")
	} else {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Errorf("错误应包装 exit status 3，实际: %v", err)
		}
	}
}

// --- 调用门（Wave 3）与 WifiService 语义 ---

type countGate struct {
	deny     error
	acquired atomic.Int64
	released atomic.Int64
}

func (g *countGate) Acquire(string) (func(), error) {
	if g.deny != nil {
		g.acquired.Add(1)
		return nil, g.deny
	}
	g.acquired.Add(1)
	return func() { g.released.Add(1) }, nil
}

// TestListProfilesGateDeniedUpstreamsError 门拒绝必须如实上抛（Wave 3 口径），
// 绝不允许被"枚举失败回空表"的既有兜底静默吞掉。sentinel 错误身份 +
// 无 netsh 环境（若门失效会走到 netsh 枚举并把错误消化成空表）双重钉死。
func TestListProfilesGateDeniedUpstreamsError(t *testing.T) {
	sentinel := errors.New("gate denied: module stopped")
	holder := extapi.NewLeaseHolder("wifi")
	holder.SetGate(&countGate{deny: sentinel})
	svc := wifi.NewWifiService(holder)
	stripNetsh(t) // 若门检查被绕过，这里只可能得到"消化后的空表"

	got, err := svc.ListProfiles()
	if !errors.Is(err, sentinel) {
		t.Fatalf("门拒绝应原样上抛 sentinel，实际 err=%v", err)
	}
	if got != nil {
		t.Errorf("拒绝时不应带回数据，实际 %+v", got)
	}
}

// TestListProfilesReleasesLease 成功调用必须归还 operation lease（drain 依赖归零）。
func TestListProfilesReleasesLease(t *testing.T) {
	stageFakeNetsh(t, fakeNetshScript, fakeNetshData)
	g := &countGate{}
	holder := extapi.NewLeaseHolder("wifi")
	holder.SetGate(g)
	svc := wifi.NewWifiService(holder)

	if _, err := svc.ListProfiles(); err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if g.acquired.Load() != 1 || g.released.Load() != 1 {
		t.Errorf("租约未收口: acquired=%d released=%d, want 1/1", g.acquired.Load(), g.released.Load())
	}
}

// TestListProfilesNetshFailureSwallowedToEmpty 既有约定：netsh 枚举失败落日志回
// 空表（nil error）——前端以"空列表 + 日志"呈现，本用例钉死该不回 nil 的语义。
func TestListProfilesNetshFailureSwallowedToEmpty(t *testing.T) {
	svc := wifi.NewWifiService(extapi.NewLeaseHolder("wifi"))
	stripNetsh(t)

	got, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("枚举失败按既有约定不应报错: %v", err)
	}
	if got == nil {
		t.Fatal("回空表必须是空切片而非 nil（前端绑定侧契约）")
	}
	if len(got) != 0 {
		t.Errorf("netsh 不可用应回空表，实际 %+v", got)
	}
}
