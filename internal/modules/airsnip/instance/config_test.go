// 上游内置更新检查接管的改写规则测试。
//
// 被测对象是纯文本变换 + 一次原子落盘：config.toml 是**用户的全部设置**所在，
// 越界改写（重排、丢键、把顶层键写进表里）都是不可接受的数据破坏，
// 故每个用例都同时断言"目标键已关闭"与"其余字节一字未动"。
package instance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDisableAutoUpdateKeyPresentAndTrue 现行最常见形态：上游写满全量默认值，
// auto_check_update = true 在顶层键区。只改等号右侧，其余原样。
func TestDisableAutoUpdateKeyPresentAndTrue(t *testing.T) {
	in := "auto_start = false\nlanguage = \"zh-CN\"\nauto_check_update = true\nskipped_version = \"0.6.0\"\n\n[hotkey]\nscreenshot_key = 113\n"
	out, changed := disableAutoUpdate(in)
	if !changed {
		t.Fatal("true → false 应报已变更")
	}
	if strings.Contains(out, "auto_check_update = true") {
		t.Errorf("目标键未关闭:\n%s", out)
	}
	if !strings.Contains(out, "auto_check_update = false") {
		t.Errorf("未按 TOML 规范形态写入:\n%s", out)
	}
	// 其余每一行都必须原样在场
	for _, line := range []string{"auto_start = false", `language = "zh-CN"`, `skipped_version = "0.6.0"`, "[hotkey]", "screenshot_key = 113"} {
		if !strings.Contains(out, line) {
			t.Errorf("越界丢失内容 %q:\n%s", line, out)
		}
	}
	// 行数不变（就地替换，不是追加）
	if strings.Count(out, "\n") != strings.Count(in, "\n") {
		t.Errorf("行数被改动: in=%d out=%d", strings.Count(in, "\n"), strings.Count(out, "\n"))
	}
}

// TestDisableAutoUpdateAlreadyFalse 已关闭时一字节不动、不报变更（避免每次启动
// 都 mtime 抖动，也避免与上游写盘抢同一文件）。
func TestDisableAutoUpdateAlreadyFalse(t *testing.T) {
	in := "auto_check_update=false\n[hotkey]\n"
	out, changed := disableAutoUpdate(in)
	if changed {
		t.Error("已为 false 不应报变更")
	}
	if out != in {
		t.Errorf("不应重排字节: %q", out)
	}
}

// TestDisableAutoUpdateKeyAbsentButTablesExist 键缺席且文件已有表头：必须插在
// **文件最前**——写在 [hotkey] 之后会变成该表的成员键（上游 load 读不到，
// 等价于没改，且给用户的 config 里埋一个畸形键）。
func TestDisableAutoUpdateKeyAbsentButTablesExist(t *testing.T) {
	in := "[hotkey]\nscreenshot_key = 113\n\n[tool_keys]\nrect = 49\n"
	out, changed := disableAutoUpdate(in)
	if !changed {
		t.Fatal("键缺席应补写并报变更")
	}
	if !strings.HasPrefix(out, "auto_check_update = false\n") {
		t.Errorf("顶层键必须落在文件最前（表头之前）:\n%s", out)
	}
	if strings.Contains(out, "[hotkey]\nauto_check_update") {
		t.Error("键被写进了表内")
	}
	if !strings.Contains(out, "screenshot_key = 113") || !strings.Contains(out, "rect = 49") {
		t.Errorf("原内容丢失:\n%s", out)
	}
}

// TestDisableAutoUpdateEmptyFile 空文件/新建场景。
func TestDisableAutoUpdateEmptyFile(t *testing.T) {
	out, changed := disableAutoUpdate("")
	if !changed || out != "auto_check_update = false\n" {
		t.Errorf("空输入应落最小内容，got %q changed=%v", out, changed)
	}
}

// TestDisableAutoUpdateSkipsComments 注释行里提到该键不算在场（否则注释会被改写，
// 而真键仍在下方保持 true——最坏情形是"看着关了其实没关"）。
func TestDisableAutoUpdateSkipsComments(t *testing.T) {
	in := "# auto_check_update = true 是默认值\nauto_check_update = true\n"
	out, changed := disableAutoUpdate(in)
	if !changed {
		t.Fatal("应报变更")
	}
	if !strings.HasPrefix(out, "# auto_check_update = true 是默认值") {
		t.Errorf("注释行被改写:\n%s", out)
	}
	if strings.Count(out, "auto_check_update = false") != 1 {
		t.Errorf("真键应恰好一条:\n%s", out)
	}
}

// TestDisableAutoUpdatePreservesCRLF 上游用 toml::to_string_pretty 写 LF，但用户
// 可能手工编辑（Windows 记事本落 CRLF）：命中行按原换行形态写回，不混排。
func TestDisableAutoUpdatePreservesCRLF(t *testing.T) {
	in := "auto_start = true\r\nauto_check_update = true\r\n[hotkey]\r\n"
	out, changed := disableAutoUpdate(in)
	if !changed {
		t.Fatal("应报变更")
	}
	if strings.Contains(out, "auto_check_update = false\n[hotkey]") {
		t.Error("CRLF 文件里混进了 LF 行")
	}
	if !strings.Contains(out, "auto_check_update = false\r\n") {
		t.Errorf("未按原 CRLF 形态写回:\n%q", out)
	}
}

// TestDisableAutoUpdateIndentedKey 缩进形态（用户手工排版）保持缩进。
func TestDisableAutoUpdateIndentedKey(t *testing.T) {
	in := "   auto_check_update = true\n"
	out, changed := disableAutoUpdate(in)
	if !changed || !strings.HasPrefix(out, "   auto_check_update = false") {
		t.Errorf("缩进未保留: %q changed=%v", out, changed)
	}
}

// ---------- 落盘侧 ----------

// TestTakeoverUpdateCheckCreatesMissingFile 上游尚未首启（文件不存在）：
// 目录与最小文件都要建起来，且内容能被上游 serde(default) 接受（只含该键）。
func TestTakeoverUpdateCheckCreatesMissingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), configDirName)
	cfg := filepath.Join(dir, configFileName)
	if err := takeoverUpdateCheck(cfg); err != nil {
		t.Fatalf("新建接管失败: %v", err)
	}
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "auto_check_update = false" {
		t.Errorf("新建内容不符: %q", string(raw))
	}
	// tmp 不留残骸
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("目录残留 %+v", ents)
	}
}

// TestTakeoverUpdateCheckIdempotent 第二次调用不得再改内容（也不得因"已存在"跳过写入逻辑而报错）。
func TestTakeoverUpdateCheckIdempotent(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), configDirName, configFileName)
	if err := takeoverUpdateCheck(cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(cfg)
	if err := takeoverUpdateCheck(cfg); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(cfg)
	if string(before) != string(after) {
		t.Errorf("重复接管改写了内容: %q → %q", before, after)
	}
}

// TestTakeoverUpdateCheckEmptyPathNoop APPDATA 缺失环境（configPath 返空串）：
// 不猜路径、不写盘、不报错——调用方（Engine.Start）本就按"失败不阻断"处理。
func TestTakeoverUpdateCheckEmptyPathNoop(t *testing.T) {
	if err := takeoverUpdateCheck(""); err != nil {
		t.Errorf("空路径应静默跳过，got %v", err)
	}
}

// TestConfigPathFollowsAppdata 路径口径与上游 Config::config_dir 一致
// （%APPDATA%\airsnip\config.toml）——写错地方等于什么都没接管。
func TestConfigPathFollowsAppdata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	if got := configPath(); got != filepath.Join(dir, "airsnip", "config.toml") {
		t.Errorf("configPath = %q", got)
	}
	if got := ConfigDir(); got != filepath.Join(dir, "airsnip") {
		t.Errorf("ConfigDir = %q", got)
	}
	t.Setenv("APPDATA", "")
	if got := configPath(); got != "" {
		t.Errorf("无 APPDATA 应返空串，got %q", got)
	}
	if got := ConfigDir(); got != "" {
		t.Errorf("无 APPDATA 时 ConfigDir 应返空串（service 据此报错，不拿空串撞 explorer），got %q", got)
	}
}
