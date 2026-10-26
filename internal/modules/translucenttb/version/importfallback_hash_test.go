// importfallback_hash_test.go 锁 TranslucentTB 本地导入的兜底版本形态（主会话补齐的
// 两处病灶之二；B1 已在 22 个模块落同型测试，translucenttb 因归属误判被跳过）。
//
// 断言落在外部事实：versions/ 目录计数、错误文案、与 GNU coreutils sha256sum 独立
// 算得的摘要对账；不依赖调度落在整秒哪一侧。另验存量 imported-<旧时间戳> 目录
// 照常可列出/可解析/可卸载（向下兼容红线）。
package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 摘要由 printf 'MZhanxi-b1-hash-fixture-[AB]' | sha256sum 独立算得（主会话本机复算）。
const (
	b1FixA      = "MZhanxi-b1-hash-fixture-A"
	b1FixAPref  = "5a0f07b12bd9"
	b1FixB      = "MZhanxi-b1-hash-fixture-B"
	b1FixBPref  = "481b029e3c8c"
	b1LegacyTag = "imported-20260101-120000"
)

// b1WriteSource 造一个完整便携版目录：主程序 + 全部伴生文件（缺伴生文件会被
// ImportLocal 的前置校验拒绝，走不到版本兜底分支）。假 PE 无版本资源 → 必落兜底。
func b1WriteSource(t *testing.T, content string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, exeName), []byte(content), 0o644); err != nil {
		t.Fatalf("写入主程序: %v", err)
	}
	for _, c := range companionNames {
		if err := os.WriteFile(filepath.Join(src, c), []byte("companion"), 0o644); err != nil {
			t.Fatalf("写入伴生文件 %s: %v", c, err)
		}
	}
	return src
}

// b1CountVersionDirs 数 versions/ 下的真实版本目录（外部事实，不看返回值形状）。
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

	info, err := m.ImportLocal(b1WriteSource(t, b1FixA))
	if err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	// TranslucentTB 展示口径：兜底版本不带 v 前缀。
	if want := "imported-" + b1FixAPref; info.Version != want {
		t.Errorf("兜底版本必须等于外部独立计算的源摘要（同内容=同版本）: want %s got %s", want, info.Version)
	}

	raw, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil {
		t.Fatalf("读 meta.json: %v", err)
	}
	if !strings.Contains(string(raw), `"installedAt"`) {
		t.Error("meta.json 缺 installedAt 时间账（版本串去时钟后时间必须由 meta 承载）")
	}
}

// TestImportLocalDuplicateContentRejectedAcrossSources 同字节换个路径再导必拒、目录数仍为 1；
// 不同内容才允许第二目录。旧时间戳兜底正是在这里跨秒静默装成两份。
func TestImportLocalDuplicateContentRejectedAcrossSources(t *testing.T) {
	m := NewManager(t.TempDir())
	srcA := b1WriteSource(t, b1FixA)
	if _, err := m.ImportLocal(srcA); err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if _, err := m.ImportLocal(srcA); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("同源重复导入应报「已安装」，got %v", err)
	}

	srcA2 := b1WriteSource(t, b1FixA)
	if _, err := m.ImportLocal(srcA2); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("同内容另一源重复导入应报「已安装」，got %v", err)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 1 {
		t.Errorf("三次导入后应恰有 1 个版本目录，实得 %d", n)
	}

	infoB, err := m.ImportLocal(b1WriteSource(t, b1FixB))
	if err != nil {
		t.Fatalf("导入不同内容应成功: %v", err)
	}
	if want := "imported-" + b1FixBPref; infoB.Version != want {
		t.Errorf("不同内容应落外部计算的摘要 %s，got %s", want, infoB.Version)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 2 {
		t.Errorf("导入不同内容后应新增第二目录（共 2），实得 %d", n)
	}
}

func TestLegacyTimestampFallbackDirRemainsUsable(t *testing.T) {
	// 向下兼容红线：历史 imported-<秒级时间戳> 目录必须照常可列出、可解析、可卸载。
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+b1LegacyTag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 忠实还原"历史导入目录"的形态：checkPortableLayout 要求主程序 + 全套伴生文件，
	// 导入账 meta.json 携带 installedAt/isImport/source（时间账由它承载）。
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(b1FixA), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range companionNames {
		if err := os.WriteFile(filepath.Join(dir, c), []byte("companion"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"),
		[]byte(`{"installedAt":"2026-01-01 12:00:00","isImport":true,"source":"C:\\old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(versionsDir)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	var found *TBVersionInfo
	for i := range list {
		if strings.Contains(list[i].Version, b1LegacyTag) {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("存量时间戳兜底目录未被列出（形状闸或解析链不认它）: %+v", list)
	}
	if _, err := m.ResolveExe(found.Version); err != nil {
		t.Errorf("存量目录应可解析主程序路径: %v", err)
	}
	if err := m.Remove(found.Version); err != nil {
		t.Errorf("存量目录应可卸载: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("Remove 后目录仍在，卸载未真正生效")
	}
}
