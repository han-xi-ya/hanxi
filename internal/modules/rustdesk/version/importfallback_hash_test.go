// importfallback_hash_test.go 锁 rustdesk 本地导入的兜底版本形态（主会话补齐的两处
// 病灶之一；B1 已在其 22 个模块落同型测试，rustdesk 因归属误判被跳过）。
//
// 断言全部落在外部事实：versions/ 目录计数、错误文案、与独立工具（GNU coreutils
// sha256sum）算出的摘要对账——不依赖用例调度落在整秒的哪一侧；并验存量
// imported-<旧时间戳> 目录照常可列出/可卸载（向下兼容红线）。
package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 摘要由 printf 'MZhanxi-b1-hash-fixture-[AB]' | sha256sum 独立算得（主会话本机复算
// 5a0f07b12bd9 / 481b029e3c8c，与被测 Go 实现无同源关系）。
const (
	b1FixA      = "MZhanxi-b1-hash-fixture-A"
	b1FixAPref  = "5a0f07b12bd9"
	b1FixB      = "MZhanxi-b1-hash-fixture-B"
	b1FixBPref  = "481b029e3c8c"
	b1LegacyTag = "imported-20260101-120000" // 历史秒级时间戳形态
)

var b1HashVerRe = regexp.MustCompile(`^vimported-[0-9a-f]{12}$`)

// b1WriteSource 造一个"文件名取不出版本"的便携版源文件：rustdesk-custom.exe 既过
// importCandidateRe（^rustdesk-.*\.exe$），又不被 assetVerRe 命中 → 必走兜底分支。
// 内容以 MZ 起头，过 verifyPEMagic 断言。
func b1WriteSource(t *testing.T, content string) string {
	t.Helper()
	src := t.TempDir()
	p := filepath.Join(src, "rustdesk-custom.exe")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写入源 exe: %v", err)
	}
	return p
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

// TestImportLocalHashFallbackIsDeterministic 兜底版本形状 + 与外部摘要对账 +
// 时间语义仍在 meta.json（版本串去时钟后不得丢时间账）。
func TestImportLocalHashFallbackIsDeterministic(t *testing.T) {
	m := NewManager(t.TempDir())

	info, err := m.ImportLocal(b1WriteSource(t, b1FixA))
	if err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if !b1HashVerRe.MatchString(info.Version) {
		t.Errorf("兜底版本应为 vimported-<sha256 前 12 hex>，got %q", info.Version)
	}
	if want := "vimported-" + b1FixAPref; info.Version != want {
		t.Errorf("兜底版本必须等于外部独立计算的源摘要（同内容=同版本）: want %s got %s", want, info.Version)
	}

	raw, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil {
		t.Fatalf("读 meta.json: %v", err)
	}
	if !strings.Contains(string(raw), `"installedAt"`) {
		t.Error("meta.json 缺 installedAt 时间账（版本串不再携带时刻后，时间必须由 meta 承载）")
	}
}

// TestImportLocalDuplicateContentRejectedAcrossSources 同一字节的源换个路径再导，
// 仍必拒且目录数仍为 1——旧时间戳兜底跨秒就是在这里静默装成第二份。
func TestImportLocalDuplicateContentRejectedAcrossSources(t *testing.T) {
	m := NewManager(t.TempDir())
	srcA := b1WriteSource(t, b1FixA)
	if _, err := m.ImportLocal(srcA); err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if _, err := m.ImportLocal(srcA); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("同源重复导入应报「已安装」，got %v", err)
	}

	srcA2 := b1WriteSource(t, b1FixA) // 同内容、另一路径
	if _, err := m.ImportLocal(srcA2); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("同内容另一源重复导入应报「已安装」，got %v", err)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 1 {
		t.Errorf("三次导入（同源两次+同内容另一源一次）后应恰有 1 个版本目录，实得 %d", n)
	}

	// 不同内容 → 不同兜底版本，照常第二目录。
	infoB, err := m.ImportLocal(b1WriteSource(t, b1FixB))
	if err != nil {
		t.Fatalf("导入不同内容应成功: %v", err)
	}
	if want := "vimported-" + b1FixBPref; infoB.Version != want {
		t.Errorf("不同内容应落外部计算的摘要 %s，got %s", want, infoB.Version)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 2 {
		t.Errorf("导入不同内容后应新增第二目录（共 2），实得 %d", n)
	}
}

func TestLegacyTimestampFallbackDirRemainsUsable(t *testing.T) {
	// 向下兼容红线：已存在的 imported-<旧秒级时间戳> 目录必须照常可列出、可卸载。
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, dirPrefix+b1LegacyTag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(b1FixA), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(versionsDir)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	var found *RDVersionInfo
	for i := range list {
		if strings.Contains(list[i].Version, b1LegacyTag) {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("存量时间戳兜底目录未被列出（形状闸或解析链不认它）: %+v", list)
	}
	if _, err := m.ResolveExe(found.Version); err != nil {
		t.Errorf("存量目录应可解析可执行体路径: %v", err)
	}
	if err := m.Remove(found.Version); err != nil {
		t.Errorf("存量目录应可卸载: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("Remove 后目录仍在，卸载未真正生效")
	}
}
