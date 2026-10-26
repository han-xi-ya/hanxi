// netsh_test.go 直接测生产解析函数（getSavedSSIDs / getPasswordForSSID）。
//
// 本文件的历史形态是"假测试"：它在 `package wifi_test` 里用 strings.Contains/Split
// **把生产解析逻辑在测试体内重写一遍再自己跟自己比**，一次都没调用被测函数——
// 于是该包"测试全绿"而覆盖率为 0%（2026-10-26 审查批次实测：旧文件对生产函数调用数=0）。
// 现改为在包内直调未导出的解析函数，走真实命令链路（假 netsh.cmd 前置 PATH），
// 断言"实际解析出的列表/值/错误"。
//
// 端到端面（GetAllWiFiPasswords 组合两函数 + 门/租约）由 netsh_fake_cmd_test.go 覆盖，
// 两处不重复：这里钉单元解析，那里钉组合与降级。
package wifi

import (
	"os"
	"path/filepath"
	"testing"
)

// uNXStage 落一个假 netsh.cmd 与其数据片段，并前置到 PATH 使 exec "netsh" 命中它。
// 用 t.Setenv 自动还原，故本包用例不可并行。
func uNXStage(t *testing.T, script string, data map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range data {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("写数据片 %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "netsh.cmd"), []byte(script), 0o644); err != nil {
		t.Fatalf("写 netsh.cmd: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// uNXScript 覆盖 profiles 名单与 profile 查询两条分支；批处理按 '=' 分词，
// 故 name=XXX 的参数落在 %5。
const uNXScript = `@echo off
if /I "%~3"=="profiles" (
  type "%~dp0profiles.txt"
  exit /b 0
)
if /I "%~3"=="profile" (
  if /I "%~5"=="CnKey" (
    type "%~dp0key_cn.txt"
    exit /b 0
  )
  if /I "%~5"=="EnKey" (
    type "%~dp0key_en.txt"
    exit /b 0
  )
  if /I "%~5"=="NoKeyLine" (
    type "%~dp0nokeyline.txt"
    exit /b 0
  )
  if /I "%~5"=="Boom" (
    echo query failed 1>&2
    exit /b 3
  )
  exit /b 7
)
exit /b 9
`

var uNXData = map[string]string{
	// 名单：中文名行、英文冒号含值内冒号、空名行（应被丢弃）、无关行
	"profiles.txt": "    所有用户配置文件 : 家用WiFi\r\n" +
		"    All User Profile     : Cafe:Corner\r\n" +
		"    All User Profile     :    \r\n" +
		"    接口名称 : Wi-Fi\r\n",
	// 中文密码标记，值本身含冒号
	"key_cn.txt": "    关键内容 : p:a:s:s123\r\n",
	// 英文密码标记
	"key_en.txt": "    Key Content  : en-secret-42\r\n",
	// 有 profile 但无密码行（未设密钥 / 权限不足形态）
	"nokeyline.txt": "    安全设置  : WPA2-个人\r\n",
}

func TestGetSavedSSIDsParsesRealOutput(t *testing.T) {
	uNXStage(t, uNXScript, uNXData)

	got, err := getSavedSSIDs()
	if err != nil {
		t.Fatalf("getSavedSSIDs: %v", err)
	}
	want := []string{"家用WiFi", "Cafe:Corner"}
	if len(got) != len(want) {
		t.Fatalf("名单条数不符：got %q want %q（空名行与无关行必须被丢弃）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条不符：got %q want %q", i, got[i], want[i])
		}
	}
}

func TestGetSavedSSIDsPropagatesCommandError(t *testing.T) {
	// PATH 里没有任何 netsh：错误必须原样上抛，不能静默返回"无已保存网络"。
	t.Setenv("PATH", t.TempDir())
	if got, err := getSavedSSIDs(); err == nil {
		t.Fatalf("命令不可用时应上抛错误，实际返回 %q 且 err=nil", got)
	}
}

func TestGetPasswordForSSIDBothLanguages(t *testing.T) {
	uNXStage(t, uNXScript, uNXData)

	cases := []struct {
		ssid string
		want string
	}{
		{"CnKey", "p:a:s:s123"},   // 中文标记，值内冒号需完整保留
		{"EnKey", "en-secret-42"}, // 英文标记
	}
	for _, c := range cases {
		got, err := getPasswordForSSID(c.ssid)
		if err != nil {
			t.Fatalf("%s: %v", c.ssid, err)
		}
		if got != c.want {
			t.Errorf("getPasswordForSSID(%q) = %q, want %q", c.ssid, got, c.want)
		}
	}
}

func TestGetPasswordForSSIDErrorPaths(t *testing.T) {
	uNXStage(t, uNXScript, uNXData)

	// 有输出但无密码行 → 如实报"未找到密码"，不得回空串当成功
	if pwd, err := getPasswordForSSID("NoKeyLine"); err == nil {
		t.Fatalf("缺密码行应报错，实际返回 pwd=%q err=nil", pwd)
	}
	// 命令非零退出 → 错误上抛（与上一条区分：这是查询失败，不是无密码）
	if pwd, err := getPasswordForSSID("Boom"); err == nil {
		t.Fatalf("查询失败应上抛错误，实际返回 pwd=%q err=nil", pwd)
	}
}
