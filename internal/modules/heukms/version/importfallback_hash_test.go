package version

// B1 回归测试：本地导入兜底版本由"秒级时间戳"改为"主程序 sha256 前 12 hex"
// （摘要先行计算、双处复用）。断言落在外部事实上（落盘目录数、错误文案、
// 与 GNU coreutils sha256sum 独立计算的摘要对账），同秒/跨秒调度结果一致；
// 并验证历史 imported-<时间戳> 存量目录照常可列出/可解析/可卸载。
//
// 夹具走包内既有假件接缝（newManagerAt + writeFakeExe：VER/DESC 标签由
// fakeFileVersion/fakeFileDescription 读取）；version 传空 → 读不出版本，
// 必走哈希兜底；DESC 保持 productHint 前缀过身份闸。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 摘要由 printf 'MZ\x90\x00\x03\x00DESC=HEU KMS Activator[ Beta]\x00' | sha256sum
// 独立算得（与被测 Go 实现无同源关系；writeFakeExe 的固定字节头 + DESC 标签）。
const (
	b1HeuAPref  = "5685ceb858ba" // DESC=HEU KMS Activator
	b1HeuBPref  = "9e24a0b913b9" // DESC=HEU KMS Activator Beta
	b1LegacyTag = "imported-20260101-120000"
)

var b1HashVerRe = regexp.MustCompile(`^imported-[0-9a-f]{12}$`)

// b1WriteHeuSource 造一个"读不出版本但过 MZ/身份闸"的源目录。
func b1WriteHeuSource(t *testing.T, desc string) string {
	t.Helper()
	src := t.TempDir()
	writeFakeExe(t, src, "HEU_KMS_Activator_portable.exe", "", desc)
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
	m, versions := newManagerAt(t)
	srcA := b1WriteHeuSource(t, productHint)

	info, err := m.ImportLocal(srcA)
	if err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	if !b1HashVerRe.MatchString(info.Version) {
		t.Errorf("兜底版本应为 imported-<sha256 前 12 hex> 形状，got %q", info.Version)
	}
	if want := "imported-" + b1HeuAPref; info.Version != want {
		t.Errorf("兜底版本必须等于外部独立计算的主程序摘要: want %s got %s", want, info.Version)
	}

	if _, err := m.ImportLocal(srcA); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("重复导入同一源应报「已安装」，got %v", err)
	}
	srcA2 := b1WriteHeuSource(t, productHint)
	if _, err := m.ImportLocal(srcA2); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("同内容另一源目录重复导入应报「已安装」，got %v", err)
	}
	if n := b1CountVersionDirs(t, versions); n != 1 {
		t.Errorf("三次导入（同一源两次+同内容另一源一次）后 versions/ 应恰有 1 个版本目录，实得 %d", n)
	}

	// 不同内容 → 不同兜底版本，第二目录照常落位
	srcB := b1WriteHeuSource(t, productHint+" Beta")
	infoB, err := m.ImportLocal(srcB)
	if err != nil {
		t.Fatalf("导入不同内容源应成功: %v", err)
	}
	if want := "imported-" + b1HeuBPref; infoB.Version != want {
		t.Errorf("不同内容应落外部计算的摘要 %s，got %s", want, infoB.Version)
	}
	if n := b1CountVersionDirs(t, versions); n != 2 {
		t.Errorf("导入不同内容后应新增第二目录（共 2），实得 %d", n)
	}
}

func TestImportLocalHashFallbackKeepsInstalledAtInMeta(t *testing.T) {
	// 时间语义迁移处置：Commit 族导入账本（artifact.Meta）现记 installedAt。
	m, _ := newManagerAt(t)
	info, err := m.ImportLocal(b1WriteHeuSource(t, productHint))
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
	// 向下兼容红线：用户已存在的 imported-<旧秒级时间戳> 目录必须照常可用。
	m, versions := newManagerAt(t)
	dir := filepath.Join(versions, dirPrefix+b1LegacyTag)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFakeExe(t, dir, "HEU_KMS_Activator_99.9.9.exe", "99.9.9.0", productHint)

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
