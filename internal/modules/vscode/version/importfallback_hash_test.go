package version

// B1 回归测试：本地导入兜底版本由"秒级时间戳"改为"被导入 Code.exe 的 sha256
// 前 12 hex"。断言落在外部事实上（落盘目录数、错误文案、与 GNU coreutils
// sha256sum 独立计算的摘要对账），同秒/跨秒调度结果一致；并验证历史
// imported-<时间戳> 存量目录照常可列出/可解析/可卸载。
//
// 注：本包既有 version_test.go 在并行会话在途（不得触碰），新断言全部落在
// 本独立文件；与在途改动无重名（b1 前缀）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 夹具摘要由 printf '%s' <内容> | sha256sum 独立算得（与被测 Go 实现无同源关系）。
const (
	b1FixA        = "hanxi-b1-hash-fixture-A"
	b1FixAPref    = "6e449042eec3"
	b1FixB        = "hanxi-b1-hash-fixture-B"
	b1FixBPref    = "840782cb5fca"
	b1LegacyTag   = "imported-20260101-120000" // 历史秒级时间戳形态
	b1ImportError = "已存在"                      // vscode 既有话术（不回归）
)

// vscode 展示口径：兜底版本不带 v 前缀。
var b1HashVerRe = regexp.MustCompile(`^imported-[0-9a-f]{12}$`)

func b1WritePortableSource(t *testing.T, content string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, exeName), []byte(content), 0644); err != nil {
		t.Fatalf("写入 Code.exe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, filepath.FromSlash(codeCmdRel)), []byte("@echo off\r\n"), 0644); err != nil {
		t.Fatalf("写入 code.cmd: %v", err)
	}
	return src
}

func b1CountVersionDirs(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("读 versions 目录: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), dirPrefix) {
			n++
		}
	}
	return n
}

func TestImportLocalHashFallbackIsDeterministic(t *testing.T) {
	m := NewManager(t.TempDir())
	srcA := b1WritePortableSource(t, b1FixA)

	info, err := m.ImportLocal(srcA)
	if err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if !b1HashVerRe.MatchString(info.Version) {
		t.Errorf("兜底版本应为 imported-<sha256 前 12 hex> 形状，got %q", info.Version)
	}
	if want := "imported-" + b1FixAPref; info.Version != want {
		t.Errorf("兜底版本必须等于外部独立计算的 Code.exe 摘要: want %s got %s", want, info.Version)
	}

	if _, err := m.ImportLocal(srcA); err == nil || !strings.Contains(err.Error(), b1ImportError) {
		t.Errorf("重复导入同一源应报「%s」，got %v", b1ImportError, err)
	}
	srcA2 := b1WritePortableSource(t, b1FixA)
	if _, err := m.ImportLocal(srcA2); err == nil || !strings.Contains(err.Error(), b1ImportError) {
		t.Errorf("同内容另一源目录重复导入应报「%s」，got %v", b1ImportError, err)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 1 {
		t.Errorf("三次导入（同一源两次+同内容另一源一次）后 versions/ 应恰有 1 个版本目录，实得 %d", n)
	}

	srcB := b1WritePortableSource(t, b1FixB)
	infoB, err := m.ImportLocal(srcB)
	if err != nil {
		t.Fatalf("导入不同内容源应成功: %v", err)
	}
	if want := "imported-" + b1FixBPref; infoB.Version != want {
		t.Errorf("不同内容应落外部计算的摘要 %s，got %s", want, infoB.Version)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 2 {
		t.Errorf("导入不同内容后应新增第二目录（共 2），实得 %d", n)
	}
}

func TestImportLocalHashFallbackKeepsInstalledAtInMeta(t *testing.T) {
	m := NewManager(t.TempDir())
	info, err := m.ImportLocal(b1WritePortableSource(t, b1FixA))
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil {
		t.Fatalf("读 meta.json: %v", err)
	}
	var mm map[string]any
	if err := json.Unmarshal(raw, &mm); err != nil {
		t.Fatalf("解析 meta.json: %v", err)
	}
	if at, _ := mm["installedAt"].(string); at == "" {
		t.Error("meta.json 缺 installedAt 时间账（时间信息须由 meta 承载）")
	}
}

func TestLegacyTimestampFallbackDirRemainsUsable(t *testing.T) {
	// 向下兼容红线：用户已存在的 imported-<旧秒级时间戳> 目录必须照常可用。
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+b1LegacyTag)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(b1FixA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(codeCmdRel)), []byte("@echo off\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(versionsDir)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	// 本机可能装有 VS Code 安装版（注册表感知），只断言兜底目录在列即可，
	// 不锁列表总数——锁总数会把用例绑死在测试机的环境上。
	found := false
	for _, v := range list {
		if v.Version == b1LegacyTag {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("历史形态目录应被列出，实得 %+v", list)
	}
	if _, err := m.ResolveExe(b1LegacyTag); err != nil {
		t.Errorf("历史形态目录应可解析 exe: %v", err)
	}
	if err := m.Remove(b1LegacyTag); err != nil {
		t.Fatalf("历史形态目录应可卸载: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("卸载后目录应消失: %v", err)
	}
}
