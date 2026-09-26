// catalog_contract_test.go 守护 Wave 0「模块目录清点」产物三件套的一致性：
// 装配根静态表（catalog.go，生成物）↔ 冻结基线（scripts/fixture/module_catalog.json）
// ↔ 源码证据（module.go / 目录结构 / mcp.go / composition contract）。
// 与 composition_contract_test.go 同一套静态扫描风格：不启动 app，纯文件系统证据比对，
// 漂移以 diffLines（-want +got）呈现。辅助函数 repositoryRoot/readText/extractMatches/
// firstMatch/diffLines/loadCompositionContract 复用自同包 composition_contract_test.go。
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

// moduleCatalogFixture 对应 scripts/fixture/module_catalog.json 的外层信封。
type moduleCatalogFixture struct {
	Schema int                        `json:"schema"`
	Items  []extapi.ModuleCatalogItem `json:"items"`
}

func TestModuleCatalogMatchesFixture(t *testing.T) {
	root := repositoryRoot(t)
	fixture := loadModuleCatalogFixture(t, root)

	if diff := diffLines(formatCatalogItems(t, fixture.Items), formatCatalogItems(t, CatalogItems())); diff != "" {
		t.Fatalf("装配根 Catalog 表与冻结基线不一致（-fixture +table，逐 ID JSON 深比对）:\n%s", diff)
	}
}

func TestModuleCatalogCoversComposition(t *testing.T) {
	root := repositoryRoot(t)
	contract := loadCompositionContract(t, root)

	want := make([]string, 0, len(contract.Modules))
	for _, module := range contract.Modules {
		want = append(want, module.ID)
	}
	sort.Strings(want)

	items := CatalogItems()
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.ID)
	}
	sort.Strings(got)

	if diff := diffLines(want, got); diff != "" {
		t.Fatalf("Catalog ID 集合与 composition contract modules 不一致 (-want +got):\n%s", diff)
	}
	if len(got) != contract.Counts.Modules || contract.Counts.Modules != 46 {
		t.Fatalf("Catalog 覆盖数量辅助断言: got %d, contract.counts.modules %d（期望 45）", len(got), contract.Counts.Modules)
	}
}

var (
	trayCommandRe = regexp.MustCompile(`TrayCommands\(\) \[\]extapi\.TrayCommand`)
	windowOpenRe  = regexp.MustCompile(`Window\.NewWithOptions`)
	mcpBodyRe     = regexp.MustCompile(`(?s)func mcpModules\([^)]*\)\s*\[\]extapi\.Module\s*\{(.*?)\n\}`)
	mcpPkgNewRe   = regexp.MustCompile(`([a-z][a-z0-9]*)\.New\(`)
)

func TestCatalogEntrypointsGrounded(t *testing.T) {
	root := repositoryRoot(t)
	items := CatalogItems()

	// tray 入口必须与模块源码中的 extapi.TrayCommandsProvider 实现互为充要。
	wantTray := catalogEntrypointIDs(items, extapi.EntryTray)
	if diff := diffLines(wantTray, catalogEvidenceModules(t, root, trayCommandRe)); diff != "" {
		t.Fatalf("tray 入口与 TrayCommands() []extapi.TrayCommand 证据不一致 (-catalog +源码证据):\n%s", diff)
	}

	// window 入口必须与模块源码中的 Window.NewWithOptions 独立建窗证据互为充要。
	wantWindow := catalogEntrypointIDs(items, extapi.EntryWindow)
	if diff := diffLines(wantWindow, catalogEvidenceModules(t, root, windowOpenRe)); diff != "" {
		t.Fatalf("window 入口与 Window.NewWithOptions 证据不一致 (-catalog +源码证据):\n%s", diff)
	}

	// mcp 入口必须钉住 internal/mcp/mcp.go 的无头装配面：
	// mcpModules() 函数体内的 pkg.New() + ocr 追加注册 + memo 零落盘特例通道。
	if diff := diffLines(catalogEntrypointIDs(items, extapi.EntryMCP), mcpGroundedModules(t, root)); diff != "" {
		t.Fatalf("mcp 入口与 internal/mcp/mcp.go 装配证据不一致 (-catalog +mcp.go):\n%s", diff)
	}

	// background 入口以 composition contract 的启动预激活集合为唯一锚点。
	wantBackground := catalogContractPreactivated(loadCompositionContract(t, root))
	sort.Strings(wantBackground)
	if diff := diffLines(wantBackground, catalogEntrypointIDs(items, extapi.EntryBackground)); diff != "" {
		t.Fatalf("background 入口与 composition contract preactivatedModules 不一致 (-fixture +catalog):\n%s", diff)
	}
}

func TestCatalogEnumValidity(t *testing.T) {
	root := repositoryRoot(t)
	fixture := loadModuleCatalogFixture(t, root)

	if fixture.Schema != extapi.ModuleContractSchema {
		t.Fatalf("fixture schema = %d，期望 extapi.ModuleContractSchema = %d", fixture.Schema, extapi.ModuleContractSchema)
	}

	validDeliveries := []extapi.DeliveryKind{
		extapi.DeliveryBuiltinLogical,
		extapi.DeliveryManagedDeclarative,
		extapi.DeliveryOfficialSidecar,
	}
	validEntrypoints := []extapi.Entrypoint{
		extapi.EntryRPC, extapi.EntryNavigation, extapi.EntrySearch, extapi.EntryTray,
		extapi.EntryHotkey, extapi.EntryMCP, extapi.EntryWindow, extapi.EntryBackground,
	}

	// 静态表与冻结基线两个口径都必须只用 extapi 契约常量表达 delivery/entrypoint。
	for _, batch := range [][]extapi.ModuleCatalogItem{CatalogItems(), fixture.Items} {
		for _, item := range batch {
			if !slices.Contains(validDeliveries, item.DeliveryKind) {
				t.Errorf("模块 %s deliveryKind=%q 不在 extapi 交付常量内", item.ID, item.DeliveryKind)
			}
			for _, entry := range item.Entrypoints {
				if !slices.Contains(validEntrypoints, entry) {
					t.Errorf("模块 %s entrypoint=%q 不在 extapi 入口常量内", item.ID, entry)
				}
			}
			if item.ID == "" || item.Name == "" || item.Owner == "" {
				t.Errorf("模块目录项身份字段不完整: %+v", item)
			}
			// rpc/navigation/search 是内建模块恒含的基础入口，任何缺失都是规则漂移。
			for _, base := range []extapi.Entrypoint{extapi.EntryRPC, extapi.EntryNavigation, extapi.EntrySearch} {
				if !slices.Contains(item.Entrypoints, base) {
					t.Errorf("模块 %s 缺基础入口 %q", item.ID, base)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 证据扫描辅助（口径与 scripts/cataloggen 保持一致：模块目录全部非 _test.go 源码）
// ---------------------------------------------------------------------------

func loadModuleCatalogFixture(t *testing.T, root string) moduleCatalogFixture {
	t.Helper()
	data := readText(t, filepath.Join(root, "scripts", "fixture", "module_catalog.json"))
	var fixture moduleCatalogFixture
	if err := json.Unmarshal([]byte(data), &fixture); err != nil {
		t.Fatalf("解析 module_catalog.json: %v", err)
	}
	return fixture
}

// formatCatalogItems 把目录项逐条压成 "id|<json>" 行，供 diffLines 输出可定位诊断。
func formatCatalogItems(t *testing.T, items []extapi.ModuleCatalogItem) []string {
	t.Helper()
	out := make([]string, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("序列化目录项 %s: %v", item.ID, err)
		}
		out = append(out, item.ID+"|"+string(encoded))
	}
	return out
}

func catalogEntrypointIDs(items []extapi.ModuleCatalogItem, entry extapi.Entrypoint) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.Entrypoints, entry) {
			out = append(out, item.ID)
		}
	}
	sort.Strings(out)
	return out
}

// catalogEvidenceModules 扫描 internal/modules 下全部模块包（含 module.go 的目录），
// 返回源码命中给定证据正则的目录名升序集合。目录名即模块 ID 的前提由
// TestModuleCatalogCoversComposition 与生成器 fail loud 共同钉死。
func catalogEvidenceModules(t *testing.T, root string, evidence *regexp.Regexp) []string {
	t.Helper()
	modulesDir := filepath.Join(root, "internal", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		t.Fatalf("读取 %s: %v", modulesDir, err)
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(modulesDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(dir, "module.go")); statErr != nil {
			continue // 非模块辅助包（如 modpath）
		}
		if evidence.MatchString(catalogModuleSources(t, dir)) {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// catalogModuleSources 拼接模块目录内全部非 _test.go Go 源码（与生成器同证据口径）。
func catalogModuleSources(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sb.Write(data)
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 %s: %v", dir, err)
	}
	return sb.String()
}

// mcpGroundedModules 从 internal/mcp/mcp.go 推导无头工具挂载的模块集合：
// mcpModules() 函数体内的 pkg.New()（envcheck、everything），外加 Run 中追加注册的
// ocr（ocr.New 证据）与 registryGate 特例通道 memo（memo.ID 证据）。
// 证据被删除时直接 fail，防止 allowlist 与 mcp 装配面静默脱钩。
func mcpGroundedModules(t *testing.T, root string) []string {
	t.Helper()
	source := readText(t, filepath.Join(root, "internal", "mcp", "mcp.go"))
	body := mcpBodyRe.FindStringSubmatch(source)
	if body == nil {
		t.Fatal("未能在 internal/mcp/mcp.go 锚定 mcpModules() 函数体")
	}
	out := extractMatches(body[1], mcpPkgNewRe)
	if !regexp.MustCompile(`ocr\.New\(`).MatchString(source) {
		t.Fatal(`internal/mcp/mcp.go 不再含 ocr.New( 追加注册证据`)
	}
	out = append(out, "ocr")
	if !regexp.MustCompile(`memo\.ID`).MatchString(source) {
		t.Fatal("internal/mcp/mcp.go 不再含 memo.ID 特例通道证据")
	}
	out = append(out, "memo")
	sort.Strings(out)
	return out
}

func catalogContractPreactivated(contract compositionContract) []string {
	return append([]string(nil), contract.Wiring.PreactivatedModules...)
}
