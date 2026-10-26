package version

// F1 回归测试：vimported- 兜底目录绝不当"最新已装"。versioncmp 对非规范形状
// 退化字典序且 'i'(0x69) > 数字字符，Tree 扫描排序会把兜底目录排到最前——
// 冷启动自动选版与 CheckUpdate 曾因此选中兜底目录并误判"已是最新"。
// 现由 importedRank 闸显式沉底；断言落外部可观察事实（ListInstalled 首位与
// 完整前缀分区、CheckUpdate 的 local/remote/hasUpdate 结论），不断 rank 返回值。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 兜底目录两形态同测：现行 imported-<sha256 前 12 hex> 与历史 imported-<时间戳>
// （存量用户目录不改名不迁移，只要求在排序里沉底；可列可解可卸由
// importfallback_hash_test.go 既有用例把关）。
const (
	sinkLMImportedHash   = "imported-6e449042eec3"
	sinkLMImportedLegacy = "imported-20260101-120000"
)

// newLMSinkManager 注入假 PE 账目接缝（exe 内容即裸版本号，四段 .0 归一）：
// 数字目录全平台可过核账列出，排序断言不依赖宿主平台的真 PE 资源读取。
func newLMSinkManager(versionsDir string) *Manager {
	m := NewManager(versionsDir)
	m.fileVersion = func(path string) (string, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)) + ".0", nil
	}
	return m
}

// mkLMSuiteDir 造布局齐备的版本目录（exe + 语言包锚点，官方 zip 恒含二者）。
func mkLMSuiteDir(t *testing.T, versionsDir, dir, exeBody string) {
	t.Helper()
	full := filepath.Join(versionsDir, dir)
	if err := os.MkdirAll(filepath.Join(full, "resources", "lang"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(full, exeName), []byte(exeBody), 0644); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(full, "resources", "lang", "zh.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write lang: %v", err)
	}
}

// writeLMSinkFixtures 造"数字版本 + 兜底目录"同场：10.0.0/9.9.9 一对刻意让
// 字典序与数值序相反（字典序 '9' > '1'），兜底目录免核账、exe 内容随意。
func writeLMSinkFixtures(t *testing.T, versionsDir string) {
	t.Helper()
	mkLMSuiteDir(t, versionsDir, "litemonitor_10.0.0", "10.0.0")
	mkLMSuiteDir(t, versionsDir, "litemonitor_9.9.9", "9.9.9")
	mkLMSuiteDir(t, versionsDir, "litemonitor_"+sinkLMImportedHash, "junk-exe-body")
	mkLMSuiteDir(t, versionsDir, "litemonitor_"+sinkLMImportedLegacy, "junk-exe-body")
}

func TestLMListInstalledSinksImportedFallback(t *testing.T) {
	versionsDir := t.TempDir()
	m := newLMSinkManager(versionsDir)
	writeLMSinkFixtures(t, versionsDir)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("四个目录应全数列出，实得 %d: %+v", len(list), list)
	}
	if list[0].Version != "v10.0.0" {
		t.Errorf("首位应为最新数字版本 v10.0.0（无闸时 'vimported-…' 借 'i'>数字 的字典序退化抢到最前），实得 %q", list[0].Version)
	}
	if list[1].Version != "v9.9.9" {
		t.Errorf("次位应按数值分段为 v9.9.9（10>9；若整体退化成字典序则 9.9.9 抢先），实得 %q", list[1].Version)
	}
	for _, v := range list[2:] {
		if !strings.HasPrefix(v.Version, "vimported-") {
			t.Errorf("末两席应均为兜底目录，实得 %+v", list)
		}
	}
}

func TestLMCheckUpdateLocalIgnoresImportedFallback(t *testing.T) {
	versionsDir := t.TempDir()
	m := newLMSinkManager(versionsDir)
	mkLMSuiteDir(t, versionsDir, "litemonitor_9.9.9", "9.9.9")
	mkLMSuiteDir(t, versionsDir, "litemonitor_"+sinkLMImportedHash, "junk-exe-body")

	// 远程稳定通道注入 v10.0.0（remoteCache 为包级单例，测后复原不污染他用例）。
	remoteCache.mu.Lock()
	oldData, oldAt := remoteCache.data, remoteCache.fetchedAt
	remoteCache.data = []LMRelease{{
		Version:   "v10.0.0",
		AssetName: "LiteMonitor_v10.0.0-win-x64.zip",
		SHA256:    strings.Repeat("a", 64),
		Size:      1,
	}}
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()
	t.Cleanup(func() {
		remoteCache.mu.Lock()
		remoteCache.data, remoteCache.fetchedAt = oldData, oldAt
		remoteCache.mu.Unlock()
	})

	local, remote, hasUpdate, err := m.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if local != "v9.9.9" {
		t.Errorf("本地「最新已装」应为数字版本 v9.9.9 而非兜底目录，实得 %q", local)
	}
	if remote != "v10.0.0" {
		t.Errorf("远程稳定通道应取 v10.0.0，实得 %q", remote)
	}
	if !hasUpdate {
		t.Errorf(`v9.9.9 对 v10.0.0 应报"有更新"（无闸时 local=兜底目录，字典序误判"已是最新"），实得 hasUpdate=false`)
	}
}
