package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
)

// ── ReceiptStore 读取面的开销量化（跨区转派 A2，回应 A3 热路径放大问题）──
//
// 现状语义：ReceiptStore "无内存缓存、磁盘为单一事实源"（receipts.go 类型
// 注释），IsInstalled 每次调用真读盘。装配根 Registry 的 Acquire/nav 投影/
// Installed 汇总等路径逐模块调用，51 模块 × 高频 RPC 时被放大为每调用数十
// 次文件 IO；机主数据根常挂 NAS/网盘同步盘，冷读放大更甚。
//
// 本文件把量级钉成可复跑的数字（本地 NTFS 为口径，NAS 冷读另计）：
// 修前/修后是否加缓存，由机主按这些数字裁"新鲜度 vs IO 成本"，
// 缓存方案未裁前本测试只测量、不改语义。

func newBenchReceiptStore(b *testing.B, n int) (*ReceiptStore, string) {
	b.Helper()
	dir := b.TempDir()
	ledger := filepath.Join(dir, "..", "state", "modules-ledger.json")
	s := NewReceiptStore(dir, ledger)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("mod-%03d", i)
		if err := s.MarkInstalled(id, extapi.ReceiptBuiltinLogical); err != nil {
			b.Fatalf("预置凭据失败: %v", err)
		}
	}
	return s, dir
}

// BenchmarkIsInstalledHit 热路径主体：已安装模块的单次判定（一次
// ReadFile+Unmarshal）。装配根每个业务 RPC 至少触发一次。
func BenchmarkIsInstalledHit(b *testing.B) {
	s, _ := newBenchReceiptStore(b, 1)
	b.ResetTimer()
	for b.Loop() {
		if !s.IsInstalled("mod-000") {
			b.Fatal("预置凭据后应判已装")
		}
	}
}

// BenchmarkIsInstalledMiss 未安装判定：一次 stat-open 失败（ENOENT 快路径）。
func BenchmarkIsInstalledMiss(b *testing.B) {
	s, _ := newBenchReceiptStore(b, 1)
	b.ResetTimer()
	for b.Loop() {
		if s.IsInstalled("ghost-module") {
			b.Fatal("幽灵模块不应判已装")
		}
	}
}

// BenchmarkInstalledScan51 nav 投影全扫：ReadDir + 51 次逐文件读解析
// （registry.go 的 List/Nav 收集路径同款成本）。
func BenchmarkInstalledScan51(b *testing.B) {
	s, _ := newBenchReceiptStore(b, 51)
	b.ResetTimer()
	for b.Loop() {
		if got := s.Installed(); len(got) != 51 {
			b.Fatalf("投影应含 51 模块，得 %d", len(got))
		}
	}
}

// TestIsInstalledReadsDiskEveryCall 钉住**当前**语义（缓存方案裁决前不许漂）：
// 绕过 Store 直接在盘上增删 receipt 文件，下一次 IsInstalled 必须如实反映。
// 若未来引入缓存，本测试即"失效点必须覆盖此处"的验收锚。
func TestIsInstalledReadsDiskEveryCall(t *testing.T) {
	dir := t.TempDir()
	s := NewReceiptStore(dir, filepath.Join(t.TempDir(), "modules-ledger.json"))

	if s.IsInstalled("drift-mod") {
		t.Fatal("初始应判未装")
	}
	raw, err := json.MarshalIndent(extapi.Receipt{
		Schema: extapi.ModuleContractSchema, ModuleID: "drift-mod", Kind: extapi.ReceiptBuiltinLogical,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "drift-mod.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.IsInstalled("drift-mod") {
		t.Fatal("盘上直写 receipt 后判定未反映——读盘语义被缓存破坏且无失效通道")
	}
	if err := os.Remove(filepath.Join(dir, "drift-mod.json")); err != nil {
		t.Fatal(err)
	}
	if s.IsInstalled("drift-mod") {
		t.Fatal("盘上删除 receipt 后判定未反映（同上）")
	}
}
