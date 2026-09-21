package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

// newLedgerPath 账本注入位：独立 TempDir，绝不落在被测 receipts 目录内
// （否则 Installed 扫描与落盘文件数断言会被账本文件污染）。
func newLedgerPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "modules-ledger.json")
}

// readLedgerForTest 直读账本原始 JSON（断言 seen/uninstalled 归位）。
func readLedgerForTest(t *testing.T, path string) moduleLedger {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取账本失败: %v", err)
	}
	var led moduleLedger
	if err := json.Unmarshal(raw, &led); err != nil {
		t.Fatalf("账本 JSON 解析失败: %v raw=%s", err, raw)
	}
	return led
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

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
	store := NewReceiptStore(dir, newLedgerPath(t))

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
	store := NewReceiptStore(dir, newLedgerPath(t))
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
	store := NewReceiptStore(dir, newLedgerPath(t))
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
	store := NewReceiptStore(dir, newLedgerPath(t))

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
	store := NewReceiptStore(dir, newLedgerPath(t))
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
	store := NewReceiptStore(dir, newLedgerPath(t))
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
	missing := NewReceiptStore(filepath.Join(dir, "no-such-dir"), newLedgerPath(t))
	if len(missing.Installed()) != 0 {
		t.Fatal("目录缺失投影应为空集")
	}
	if missing.IsInstalled("good-a") {
		t.Fatal("目录缺失应判未安装")
	}
}

// TestEnsureSeenUninstallPersists 账本核心行为（批 1 项二强化版）：
// 卸载过的已知模块重启不复活；账本在场时新模块自动安装；
// 账本主备俱损时按凭据事实保守重建——beta 依然不得复活
// （旧版"空名单起步重认全"正是本批要根除的复活通道）。
func TestEnsureSeenUninstallPersists(t *testing.T) {
	dir := t.TempDir()
	ledger := newLedgerPath(t)

	store := NewReceiptStore(dir, ledger)
	added, err := store.EnsureSeen([]string{"alpha", "beta"}, extapi.ReceiptBuiltinLogical)
	if err != nil || len(added) != 2 {
		t.Fatalf("首轮应全量认全: added=%v err=%v", added, err)
	}
	// 用户卸载 beta（tombstone fail-closed 先行）。
	if err := store.MarkAbsent("beta"); err != nil {
		t.Fatal(err)
	}
	reopened := NewReceiptStore(dir, ledger)
	added, err = reopened.EnsureSeen([]string{"alpha", "beta", "gamma"}, extapi.ReceiptBuiltinLogical)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.IsInstalled("beta") {
		t.Error("tombstone 在册的已卸载模块不得复活")
	}
	if !reopened.IsInstalled("gamma") || len(added) != 1 || added[0] != "gamma" {
		t.Errorf("正常账本下新模块应自动安装: added=%v gamma=%v", added, reopened.IsInstalled("gamma"))
	}
	led := readLedgerForTest(t, ledger)
	if !containsString(led.Uninstalled, "beta") || !containsString(led.Seen, "gamma") {
		t.Fatalf("账本归位异常: %+v", led)
	}

	// 主备俱损 → "缺席=已卸载"保守重建：现存凭据认全，beta 不复活。
	for _, p := range []string{ledger, ledger + ".bak"} {
		if err := os.WriteFile(p, []byte("{ broken json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fresh := NewReceiptStore(dir, ledger)
	added, err = fresh.EnsureSeen([]string{"alpha", "beta", "gamma"}, extapi.ReceiptBuiltinLogical)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.IsInstalled("alpha") || !fresh.IsInstalled("gamma") {
		t.Error("重建后现存凭据模块应仍为已安装")
	}
	if fresh.IsInstalled("beta") || len(added) != 0 {
		t.Errorf("全损重建必须把无凭据的注册模块判已卸载（不补建不复活）: added=%v", added)
	}
	if led := readLedgerForTest(t, ledger); !containsString(led.Uninstalled, "beta") {
		t.Fatalf("重建的保守裁决应落 tombstone 固化: %+v", led)
	}
}

// TestLedgerLegacyMigration 旧版 receipts/known-modules.json 一次性迁移：
// 旧名单 ∩ 凭据事实归位 seen/uninstalled，旧文件改名 .migrated 离场。
func TestLedgerLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	ledger := newLedgerPath(t)
	// 手工铺旧世界：alpha 在册且已装；beta 在册但已卸载（无凭据）。
	if err := os.WriteFile(filepath.Join(dir, "alpha.json"),
		[]byte(`{"schema":1,"moduleId":"alpha","kind":"builtin-logical","installedAt":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyLedgerFile),
		[]byte(`{"schema":1,"moduleIds":["alpha","beta"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewReceiptStore(dir, ledger)
	added, err := store.EnsureSeen([]string{"alpha", "beta", "gamma"}, extapi.ReceiptBuiltinLogical)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Errorf("迁移轮不应有新补建（gamma 缺席=已卸载）: %v", added)
	}
	led := readLedgerForTest(t, ledger)
	if !containsString(led.Seen, "alpha") || !containsString(led.Uninstalled, "beta") || !containsString(led.Uninstalled, "gamma") {
		t.Fatalf("迁移归位异常: %+v", led)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyLedgerFile)); !os.IsNotExist(err) {
		t.Error("旧账本应改名离场")
	}
	if _, err := os.Stat(filepath.Join(dir, legacyLedgerFile+".migrated")); err != nil {
		t.Errorf("迁移残留未就位: %v", err)
	}
	if store.IsInstalled("beta") {
		t.Error("迁移判定的已卸载模块不得被补建")
	}
}

// TestLedgerBakRescue 主文件损坏由 .bak 救回，tombstone 记忆无损。
func TestLedgerBakRescue(t *testing.T) {
	dir := t.TempDir()
	ledger := newLedgerPath(t)
	store := NewReceiptStore(dir, ledger)
	if _, err := store.EnsureSeen([]string{"alpha", "beta"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkAbsent("beta"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, []byte("{ corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened := NewReceiptStore(dir, ledger)
	if _, err := reopened.EnsureSeen([]string{"alpha", "beta"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if reopened.IsInstalled("beta") {
		t.Error("主文件损坏应由 .bak 保住 tombstone，beta 不得复活")
	}
	if led := readLedgerForTest(t, ledger); !containsString(led.Uninstalled, "beta") {
		t.Errorf("救回后主文件应被修复回写: %+v", led)
	}
}

// TestLedgerReadOnlyFutureSchema 更高未知 schema：全场只读——不补建、
// 不改写文件、卸载显式报错（摧毁未来字段比拒绝服务恶劣）。
func TestLedgerReadOnlyFutureSchema(t *testing.T) {
	dir := t.TempDir()
	ledger := newLedgerPath(t)
	future := `{"schema":99,"seen":["alpha"],"uninstalled":[]}`
	if err := os.MkdirAll(filepath.Dir(ledger), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewReceiptStore(dir, ledger)
	added, err := store.EnsureSeen([]string{"alpha", "beta"}, extapi.ReceiptBuiltinLogical)
	if err != nil || len(added) != 0 {
		t.Fatalf("只读模式不得补建: added=%v err=%v", added, err)
	}
	if err := store.MarkAbsent("alpha"); err == nil {
		t.Error("只读模式卸载必须显式失败（fail-closed）")
	}
	if raw, _ := os.ReadFile(ledger); string(raw) != future {
		t.Error("只读模式不得改写未来版本账本")
	}
}

// TestMarkAbsentFailClosed 账本写失败（账本父路径被文件占位）→ 卸载报错、
// 凭据原样保留——"半卸载"与"重启复活"两头都堵死。
func TestMarkAbsentFailClosed(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(t.TempDir(), "blocker") // 占位成普通文件，MkdirAll 必败
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(blocker, "modules-ledger.json")
	store := NewReceiptStore(dir, ledger)
	if err := store.MarkInstalled("alpha", extapi.ReceiptBuiltinLogical); err == nil {
		t.Fatal("账本不可写时 MarkInstalled 必须上抛（卸载记忆状态是否可靠属用户可见语义）")
	}
	// 但凭据本身已落盘（安装动作与记账分离），安装对用户成立。
	if !store.IsInstalled("alpha") {
		t.Fatal("账本不可写不应阻止凭据落盘")
	}
	if err := store.MarkAbsent("alpha"); err == nil {
		t.Fatal("账本写失败时卸载必须报错")
	}
	if !store.IsInstalled("alpha") {
		t.Fatal("tombstone 未落账前绝不删凭据（fail-closed 顺序）")
	}
}

// TestReservedModuleIDAndTombstoneLift 保留名拒绝 + 显式重装注销 tombstone。
func TestReservedModuleIDAndTombstoneLift(t *testing.T) {
	dir := t.TempDir()
	ledger := newLedgerPath(t)
	store := NewReceiptStore(dir, ledger)
	if err := store.MarkInstalled("known-modules", extapi.ReceiptBuiltinLogical); err == nil {
		t.Fatal("known-modules 应被保留名闸门拒绝")
	}

	if _, err := store.EnsureSeen([]string{"alpha"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkAbsent("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkInstalled("alpha", extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	led := readLedgerForTest(t, ledger)
	if containsString(led.Uninstalled, "alpha") || !containsString(led.Seen, "alpha") {
		t.Fatalf("显式安装应注销 tombstone 并归位 seen: %+v", led)
	}
	// 重装后重启不失踪也不"被卸载"：EnsureSeen 正常跳过。
	if _, err := store.EnsureSeen([]string{"alpha"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	if !store.IsInstalled("alpha") {
		t.Error("重装后的模块应稳定保持已安装")
	}
}
