package version

// B1 回归测试：本地导入兜底版本由"秒级时间戳"改为"被导入 exe 的 sha256 前
// 12 hex"。同时补上原实现缺失的重复导入闸（此前同令牌二次导入会穿透到
// Kernel Commit 的同版本幂等合并，静默盖旧账）。断言落在外部事实上（落盘
// 目录数、错误文案、与 GNU coreutils sha256sum 独立计算的摘要对账），同秒/
// 跨秒调度结果一致；并验证历史 imported-<时间戳> 存量目录照常可用。
//
// 夹具为不可执行假件：PE FileVersion 读不出 → 兜底路径确定性触发。

import (
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
	b1LegacyTag   = "imported-20260101120000" // 历史形态（本家族时间戳不带分隔符）
	b1ImportError = "已安装"                     // 对外文案口径与家族一致
)

// windterm 展示口径：normalizeVersion 对 imported- 令牌不加 v。
var b1HashVerRe = regexp.MustCompile(`^imported-[0-9a-f]{12}$`)

func b1WriteSource(t *testing.T, content string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, exeName), []byte(content), 0644); err != nil {
		t.Fatalf("写入源 exe: %v", err)
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
		if e.IsDir() && strings.HasPrefix(e.Name(), treeEntryName+"_") {
			n++
		}
	}
	return n
}

func TestImportLocalHashFallbackIsDeterministic(t *testing.T) {
	m := NewManager(t.TempDir())
	srcA := b1WriteSource(t, b1FixA)

	info, err := m.ImportLocal(srcA)
	if err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if !b1HashVerRe.MatchString(info.Version) {
		t.Errorf("兜底版本应为 imported-<sha256 前 12 hex> 形状，got %q", info.Version)
	}
	if want := "imported-" + b1FixAPref; info.Version != want {
		t.Errorf("兜底版本必须等于外部独立计算的源摘要: want %s got %s", want, info.Version)
	}

	if _, err := m.ImportLocal(srcA); err == nil || !strings.Contains(err.Error(), b1ImportError) {
		t.Errorf("重复导入同一源应报「%s」，got %v", b1ImportError, err)
	}
	srcA2 := b1WriteSource(t, b1FixA)
	if _, err := m.ImportLocal(srcA2); err == nil || !strings.Contains(err.Error(), b1ImportError) {
		t.Errorf("同内容另一源目录重复导入应报「%s」，got %v", b1ImportError, err)
	}
	if n := b1CountVersionDirs(t, m.versionsDir); n != 1 {
		t.Errorf("三次导入（同一源两次+同内容另一源一次）后 versions/ 应恰有 1 个版本目录，实得 %d", n)
	}

	srcB := b1WriteSource(t, b1FixB)
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
	// 时间语义迁移处置：Commit 族导入账本（artifact.Meta）现记 installedAt。
	m := NewManager(t.TempDir())
	info, err := m.ImportLocal(b1WriteSource(t, b1FixA))
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil {
		t.Fatalf("读 meta.json: %v", err)
	}
	if !strings.Contains(string(raw), `"installedAt"`) {
		t.Errorf("meta.json 缺 installedAt 时间账: %s", raw)
	}
}

func TestLegacyTimestampFallbackDirRemainsUsable(t *testing.T) {
	// 向下兼容红线：用户已存在的 imported-<旧时间戳> 目录必须照常可用。
	versionsDir := t.TempDir()
	dir := filepath.Join(versionsDir, treeEntryName+"_"+b1LegacyTag)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(b1FixA), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(versionsDir)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	found := false
	for _, v := range list {
		if v.Version == b1LegacyTag {
			found = true
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
