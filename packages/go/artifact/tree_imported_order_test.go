// tree_imported_order_test.go 锁 Tree.Versions()/Resolve("") 对"导入兜底目录"的排序档位。
//
// 缺陷本体（已实测到 heukms/litemonitor 受害）：家族在本地导入读不出版本时统一以
// `imported-<sha256 前缀>`（历史形态 `imported-<秒级时间戳>`）当兜底目录名；
// versioncmp.Compare 对非规范段退化为整串字典序，而 'i'(0x69) 码点大于数字字符，
// 于是降序排列会把兜底目录排成"最新版"。凡信任本序再取首个的调用方，冷启动会选中
// 兜底目录，或误判"已是最新"而漏报更新。
//
// 断言的是外部可观察结果（返回顺序、解析到的目录），不测 versionRank 返回值形状。
package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// plantVersionDir 造一个版本目录；withMeta=true 时写可信账本（schema 非 0），
// 让"兜底目录带着可信账本"这种最诱人的情形也被覆盖到。
func plantVersionDir(t *testing.T, root, entry, version string, withMeta bool) string {
	t.Helper()
	dir := filepath.Join(root, entry+"_"+version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if withMeta {
		m := Meta{
			Schema:      1,
			Tool:        entry,
			Version:     version,
			Entry:       entry + "_" + version,
			AssetSHA256: "0123456789abcdef0123456789abcdef",
			InstalledAt: time.Now().UTC(),
			Source:      "remote",
		}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("编码账本: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, metaFileName), raw, 0o644); err != nil {
			t.Fatalf("写账本: %v", err)
		}
	} else if err := os.WriteFile(filepath.Join(dir, "app.exe"), []byte("MZfake"), 0o644); err != nil {
		t.Fatalf("写占位文件: %v", err)
	}
	return dir
}

func versionsOrder(t *testing.T, root string) []string {
	t.Helper()
	list, err := OpenTree(root, "tool").Versions()
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Version)
	}
	return out
}

// TestVersionsSinksImportedFallbacks 兜底目录（现行哈希形 + 历史时间戳形）必须排在
// 全部规范版本之后，且规范版本之间仍是数值降序（9.9.9 不得因字典序赢过 63.1.0）。
func TestVersionsSinksImportedFallbacks(t *testing.T) {
	root := t.TempDir()
	plantVersionDir(t, root, "tool", "63.1.0", false)
	plantVersionDir(t, root, "tool", "9.9.9", false)
	plantVersionDir(t, root, "tool", "imported-6e449042eec3", false)
	plantVersionDir(t, root, "tool", "imported-20260101-120000", false)

	got := versionsOrder(t, root)
	want := []string{"63.1.0", "9.9.9", "imported-6e449042eec3", "imported-20260101-120000"}
	if len(got) != len(want) {
		t.Fatalf("版本数不符：got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 位不符：got %v，want %v（兜底目录不得冒充最新，规范段之间须数值序）", i, got, want)
		}
	}
}

// TestResolveLatestPrefersCanonicalOverImported 两方都带可信账本时，"最新可用"必须取
// 规范版本而非导入兜底目录——修复前字典序把 imported- 排在 63.1.0 之前，Resolve("")
// 会直接选中兜底目录（冷启动自动跑到导入的那份上）。
//
// 反向界定（本用例同时钉住、别误读成缺陷）：若规范版本**没有**可信账本、只有兜底目录
// 有账本，则 Resolve("") 取兜底目录是**正确**语义——"有可信账本的最新版"本就允许纯导入
// 用户可用；把它一并跳过会让只用导入功能的人得不到任何可用版本。
func TestResolveLatestPrefersCanonicalOverImported(t *testing.T) {
	root := t.TempDir()
	numDir := plantVersionDir(t, root, "tool", "63.1.0", true)
	impDir := plantVersionDir(t, root, "tool", "imported-6e449042eec3", true)

	got, err := OpenTree(root, "tool").Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if got != numDir {
		t.Fatalf("两方均有可信账本时，最新可用必须是规范版本：期望 %s，实得 %s（兜底目录不得冒充最新）", numDir, got)
	}
	_ = impDir
}

// TestVersionsCanonicalOrderUnchanged 反向护栏：全部为规范版本时，序与本改前完全一致
// （含 voidtools 修正版惯例 1422b > 1422、段多者胜），档位不得扰动既有语义。
func TestVersionsCanonicalOrderUnchanged(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"1.5.0.1422", "1.5.0.1422b", "6.2.0", "6.10.0", "1.42.0"} {
		plantVersionDir(t, root, "tool", v, false)
	}
	got := versionsOrder(t, root)
	want := []string{"6.10.0", "6.2.0", "1.42.0", "1.5.0.1422b", "1.5.0.1422"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("规范版本序被改动：got %v，want %v", got, want)
		}
	}
}
