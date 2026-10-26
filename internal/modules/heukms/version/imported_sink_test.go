package version

// F1 回归测试：imported- 兜底目录绝不当"最新已装"。versioncmp 对非规范形状
// 退化字典序且 'i'(0x69) > 数字字符，Tree 扫描排序会把兜底目录排到最前——
// 冷启动自动选版与 CheckUpdate 曾因此选中兜底目录并误判"已是最新"。
// 现由 importedRank 闸显式沉底；断言落外部可观察事实（ListInstalled 首位与
// 完整前缀分区、CheckUpdate 的 local/remote/hasUpdate 结论），不断 rank 返回值。

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// 兜底目录两形态同测：现行 imported-<sha256 前 12 hex> 与历史 imported-<时间戳>
// （存量用户目录不改名不迁移，只要求在排序里沉底、照常可列可解可卸——后者
// 由 TestLegacyTimestampFallbackDirRemainsUsable 把关）。
const (
	sinkImportedHash   = "imported-6e449042eec3"
	sinkImportedLegacy = "imported-20260101-120000"
)

// writeSinkFixtures 造"数字版本 + 兜底目录"同场：63.1.0/9.9.9 一对刻意让
// 字典序与数值序相反（字典序 '9' > '6'），两枚兜底目录 VER 留空走豁免核账分支。
func writeSinkFixtures(t *testing.T, versions string) {
	t.Helper()
	writeFakeExe(t, filepath.Join(versions, "heukms_63.1.0"), "HEU_KMS_Activator_v63.1.0.exe", "63.1.0.0", productHint)
	writeFakeExe(t, filepath.Join(versions, "heukms_9.9.9"), "HEU_KMS_Activator_v9.9.9.exe", "9.9.9", productHint)
	writeFakeExe(t, filepath.Join(versions, "heukms_"+sinkImportedHash), "HEU_KMS_Activator.exe", "", productHint)
	writeFakeExe(t, filepath.Join(versions, "heukms_"+sinkImportedLegacy), "HEU_KMS_Activator.exe", "", productHint)
}

func TestListInstalledSinksImportedFallback(t *testing.T) {
	m, versions := newManagerAt(t)
	writeSinkFixtures(t, versions)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("四个目录应全数列出，实得 %d: %+v", len(list), list)
	}
	if list[0].Version != "63.1.0" {
		t.Errorf("首位应为最新数字版本 63.1.0（无闸时 'imported-…' 借 'i'>数字 的字典序退化抢到最前），实得 %q", list[0].Version)
	}
	if list[1].Version != "9.9.9" {
		t.Errorf("次位应按数值分段为 9.9.9（63>9；若整体退化成字典序则 9.9.9 抢先），实得 %q", list[1].Version)
	}
	for _, v := range list[2:] {
		if !strings.HasPrefix(v.Version, "imported-") {
			t.Errorf("末两席应均为兜底目录，实得 %+v", list)
		}
	}
}

func TestCheckUpdateLocalIgnoresImportedFallback(t *testing.T) {
	seedRemoteCache(t, []byte(realReleaseBody)) // 稳定通道最新条目：64.0.0
	m, versions := newManagerAt(t)
	writeFakeExe(t, filepath.Join(versions, "heukms_63.1.0"), "HEU_KMS_Activator_v63.1.0.exe", "63.1.0", productHint)
	writeFakeExe(t, filepath.Join(versions, "heukms_"+sinkImportedHash), "HEU_KMS_Activator.exe", "", productHint)

	local, remote, hasUpdate, err := m.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if local != "63.1.0" {
		t.Errorf("本地「最新已装」应为数字版本 63.1.0 而非兜底目录，实得 %q", local)
	}
	if remote != "64.0.0" {
		t.Errorf("远程稳定通道应取 64.0.0，实得 %q", remote)
	}
	if !hasUpdate {
		t.Errorf(`63.1.0 对 64.0.0 应报"有更新"（无闸时 local=兜底目录，字典序误判"已是最新"），实得 hasUpdate=false`)
	}
}
