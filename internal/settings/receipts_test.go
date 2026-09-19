package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

// readReceiptFileForTest 读取指定模块的凭据文件原始内容（不存在直接 Fatal）。
func readReceiptFileForTest(t *testing.T, dir, moduleID string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, moduleID+".json"))
	if err != nil {
		t.Fatalf("读取凭据 %s 失败: %v", moduleID, err)
	}
	return raw
}

// TestReceiptStoreRoundTrip 基本回环：登记→落盘→查询→投影→卸载→幂等卸载。
func TestReceiptStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)

	if store.IsInstalled("markeron") {
		t.Fatal("空目录应判未安装")
	}
	if err := store.MarkInstalled("markeron", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if !store.IsInstalled("markeron") {
		t.Fatal("MarkInstalled 后应判已安装")
	}

	var rec extapi.Receipt
	if err := json.Unmarshal(readReceiptFileForTest(t, dir, "markeron"), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Schema != extapi.ModuleContractSchema || rec.ModuleID != "markeron" || rec.Kind != extapi.ReceiptBuiltinLogical {
		t.Fatalf("落盘凭据内容错误: %+v", rec)
	}
	if rec.InstalledAt.IsZero() {
		t.Fatal("InstalledAt 不应为零值")
	}
	if got := store.Installed(); len(got) != 1 || !got["markeron"] {
		t.Fatalf("投影集合错误: %v", got)
	}

	if err := store.MarkAbsent("markeron"); err != nil {
		t.Fatal(err)
	}
	if store.IsInstalled("markeron") {
		t.Fatal("MarkAbsent 后应判未安装")
	}
	// 卸载幂等：重复移除不存在的凭据视为成功
	if err := store.MarkAbsent("markeron"); err != nil {
		t.Fatalf("移除不存在凭据应幂等成功: %v", err)
	}
}

// TestReceiptStoreMarkInstalledIdempotent 同 ID 同 kind 重复登记：磁盘内容
// 一字不改（InstalledAt 保留首次值）。
func TestReceiptStoreMarkInstalledIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)
	if err := store.MarkInstalled("envcheck", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	first := readReceiptFileForTest(t, dir, "envcheck")

	if err := store.MarkInstalled("envcheck", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if second := readReceiptFileForTest(t, dir, "envcheck"); string(first) != string(second) {
		t.Fatal("幂等重登不应改写凭据（InstalledAt 必须保留首次值）")
	}
}

// TestReceiptStoreKindDriftRejected 同 ID 不同 kind：交付形态漂移必须拒绝，
// 且旧凭据原样保留。
func TestReceiptStoreKindDriftRejected(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)
	if err := store.MarkInstalled("everything", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	before := readReceiptFileForTest(t, dir, "everything")

	err := store.MarkInstalled("everything", extapi.ReceiptOfficialSidecar)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("形态漂移必须报错并说明原因, got %v", err)
	}
	if after := readReceiptFileForTest(t, dir, "everything"); string(before) != string(after) {
		t.Fatal("漂移拒绝后旧凭据必须原样保留")
	}
	if !store.IsInstalled("everything") {
		t.Fatal("被拒的漂移登记不得破坏既有安装态")
	}
}

// TestReceiptStoreIllegalModuleIDs moduleId 消毒：非法 ID 写入侧拒绝、
// 查询侧静默 false、卸载侧拒绝，且绝不在凭据目录外拼出任何文件。
func TestReceiptStoreIllegalModuleIDs(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)

	illegal := []string{"", "Foo", "foo bar", "a/b", `a\b`, "..", ".", "a.b", "a.json",
		"../../outside", "模块", "a\x00b", "A-1", "a_b"}
	for _, id := range illegal {
		if err := store.MarkInstalled(id, extapi.ReceiptBuiltinLogical); err == nil {
			t.Errorf("MarkInstalled(%q) 应拒绝非法 ID", id)
		}
		if err := store.MarkAbsent(id); err == nil {
			t.Errorf("MarkAbsent(%q) 应拒绝非法 ID", id)
		}
		if store.IsInstalled(id) {
			t.Errorf("IsInstalled(%q) 对非法 ID 应返回 false", id)
		}
	}

	legal := []string{"a", "markeron", "ccswitch", "z-9", "a1-b2"}
	for _, id := range legal {
		if err := store.MarkInstalled(id, extapi.ReceiptBuiltinLogical); err != nil {
			t.Errorf("MarkInstalled(%q) 应接受合法 ID: %v", id, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(legal) {
		t.Fatalf("非法 ID 不得留下任何落盘文件: got %d, want %d", len(entries), len(legal))
	}
}

// TestReceiptStoreEnsureInstalled 迁移补建：只为缺失者新建，已有同 kind
// 保留原时刻不改写；坏 ID 聚合报错但不波及其他补建；重跑幂等。
func TestReceiptStoreEnsureInstalled(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)
	if err := store.MarkInstalled("legacy-a", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	beforeA := readReceiptFileForTest(t, dir, "legacy-a")

	err := store.EnsureInstalled([]string{"legacy-a", "legacy-b", "Bad ID"}, extapi.ReceiptBuiltinLogical)
	if err == nil || !strings.Contains(err.Error(), "Bad ID") {
		t.Fatalf("坏 ID 应聚合报错并点名, got %v", err)
	}
	if afterA := readReceiptFileForTest(t, dir, "legacy-a"); string(beforeA) != string(afterA) {
		t.Fatal("EnsureInstalled 不得改写既有同 kind 凭据（只补缺）")
	}
	if !store.IsInstalled("legacy-b") {
		t.Fatal("缺失模块应被补建")
	}
	if err := store.EnsureInstalled([]string{"legacy-a", "legacy-b"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatalf("全部已存在时重跑应零错误: %v", err)
	}
	if err := store.EnsureInstalled([]string{"legacy-a"}, extapi.ReceiptOfficialSidecar); err == nil {
		t.Fatal("补建同样受形态漂移闸门约束")
	}
}

// TestReceiptStoreInstalledScan 投影扫描：有效凭据入集合；损坏 JSON、
// 形态不对的 JSON、内容漂移、非法文件名、非 json 杂项与同名目录一律
// 跳过不阻断。目录缺失按空集处理。
func TestReceiptStoreInstalledScan(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)
	if err := store.MarkInstalled("good-a", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkInstalled("good-b", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}

	drift, err := json.Marshal(extapi.Receipt{Schema: extapi.ModuleContractSchema, ModuleID: "elsewhere", Kind: extapi.ReceiptBuiltinLogical})
	if err != nil {
		t.Fatal(err)
	}
	blockers := map[string][]byte{
		"broken.json":   []byte("{ 这不是 JSON"),
		"array.json":    []byte("[1,2]"),
		"drift.json":    drift,
		"Bad Name.json": []byte("{}"),
		"README.txt":    []byte("非凭据文件"),
	}
	for name, raw := range blockers {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.json"), 0755); err != nil {
		t.Fatal(err)
	}

	got := store.Installed()
	if len(got) != 2 || !got["good-a"] || !got["good-b"] {
		t.Fatalf("投影应只含两份有效凭据, got %v", got)
	}
	// 损坏文件按未安装处理（读取 fail soft，但已留痕 Warn）
	for _, id := range []string{"broken", "array", "drift"} {
		if store.IsInstalled(id) {
			t.Errorf("损坏凭据 %s 应按未安装处理", id)
		}
	}

	// 目录缺失（全新安装常态）：投影空集、查询静默 false，不得 panic
	missing := NewReceiptStore(filepath.Join(dir, "no-such-dir"))
	if len(missing.Installed()) != 0 {
		t.Fatal("目录缺失投影应为空集")
	}
	if missing.IsInstalled("good-a") {
		t.Fatal("目录缺失应判未安装")
	}
}

// TestEnsureSeenUninstallPersists 名单账本核心行为：卸载过的已知模块重启不复活；
// 名单外新模块自动安装；名单缺失时按空名单重认全（现存凭据不丢）。
func TestEnsureSeenUninstallPersists(t *testing.T) {
	dir := t.TempDir()
	store := NewReceiptStore(dir)

	added, err := store.EnsureSeen([]string{"alpha", "beta"}, extapi.ReceiptBuiltinLogical)
	if err != nil || len(added) != 2 {
		t.Fatalf("首轮应全量迁移: added=%v err=%v", added, err)
	}
	// 用户卸载 beta → 再次启动（同一目录重开 store）不得复活。
	if err := store.MarkAbsent("beta"); err != nil {
		t.Fatal(err)
	}
	reopened := NewReceiptStore(dir)
	added, err = reopened.EnsureSeen([]string{"alpha", "beta", "gamma"}, extapi.ReceiptBuiltinLogical)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.IsInstalled("beta") {
		t.Error("已卸载的已知模块不得被启动迁移复活")
	}
	if !reopened.IsInstalled("gamma") {
		t.Error("名单外新模块应自动安装")
	}
	if len(added) != 1 || added[0] != "gamma" {
		t.Errorf("added = %v, want [gamma]", added)
	}
	// 名单损坏 → 空名单起步：现存模块重新认全，但不覆盖既有凭据时间戳由幂等保证。
	if err := os.WriteFile(filepath.Join(dir, knownLedgerFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := NewReceiptStore(dir)
	if _, err := fresh.EnsureSeen([]string{"alpha", "beta", "gamma"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if !fresh.IsInstalled("alpha") || !fresh.IsInstalled("gamma") {
		t.Error("名单丢失重认后现存模块应仍为已安装")
	}
}
