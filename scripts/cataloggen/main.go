// main.go 是 Wave 0「模块目录清点」生成器，确定性重生成两份产物（幂等，可重复执行）：
//
//   - internal/app/catalog.go：装配根 Catalog 静态表（catalogItems）与 CatalogItems/CatalogItemOf 查询函数；
//   - scripts/fixture/module_catalog.json：冻结基线（schema + items，encoding/json 序列化，与 Go 表逐字段一致）。
//
// 证据来源：
//  1. scripts/fixture/composition_contract.json：模块身份（id/route/group → Category）与
//     wiring.preactivatedModules（background 入口证据）；
//  2. internal/modules/<pkg>/module.go：正则提取 ID（const ID）、展示名（Name:）
//     与描述（Description:，CatalogItem.Description 与 ModuleInfo.Description 同源）；
//  3. 源码可判定证据（扫描模块目录全部非 _test.go 文件，与 catalog_contract_test.go 同口径）：
//     TrayCommands() []extapi.TrayCommand → tray 入口；Window.NewWithOptions → window 入口；
//     net.Listen( → embedded-server 能力；version/ 与 instance/ 子目录存在性 →
//     hosted-versions / managed-process 能力；
//  4. 源码判不了的走显式 allowlist（hotkeyAllowlist / mcpAllowlist，证据文件见各自注释）。
//
// 用法：在仓库内任意子目录执行 `go run ./scripts/cataloggen`。
// 输出按模块 ID 排序、无时间戳，重复执行字节级一致；未知模块 ID（fixture 有目录无、
// 或目录有 fixture 无）fail loud 拒绝生成。
package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"hanxi/internal/extapi"
)

// ---------------------------------------------------------------------------
// 契约输入结构（composition_contract.json 的最小子集）
// ---------------------------------------------------------------------------

type moduleFixture struct {
	ID    string `json:"id"`
	Route string `json:"route"`
	Group string `json:"group"`
}

type contractFixture struct {
	Modules []moduleFixture `json:"modules"`
	Wiring  struct {
		PreactivatedModules []string `json:"preactivatedModules"`
	} `json:"wiring"`
}

// catalogFixtureFile 是 scripts/fixture/module_catalog.json 的外层信封。
type catalogFixtureFile struct {
	Schema int                        `json:"schema"`
	Items  []extapi.ModuleCatalogItem `json:"items"`
}

// ---------------------------------------------------------------------------
// 能力标记词汇（catalog 契约的 capabilities 取值全集）
// ---------------------------------------------------------------------------

const (
	capTrayCommands     = "tray-commands"       // 实现 extapi.TrayCommandsProvider
	capHostedVersions   = "hosted-versions"     // 自带 version/ 版本管理子系统
	capManagedProcess   = "managed-process"     // 自带 instance/ JobObject 托管启停子系统
	capDedicatedWindow  = "dedicated-window"    // 直接创建独立应用窗口
	capEmbeddedServer   = "embedded-server"     // 服务监听（net.Listen）
	capHotkeySlot       = "hotkey-slot"         // 占用全局热键槽位
	capMcpTools         = "mcp-tools"           // 经 `hanxi mcp` 无头进程暴露工具
	capBackgroundListen = "background-listener" // 启动预激活的常驻后台任务
)

// entrypointOrder 是 entrypoints 数组内的固定排序（契约输出口径，不按字母序）。
var entrypointOrder = []struct {
	value     extapi.Entrypoint
	constName string
}{
	{extapi.EntryRPC, "extapi.EntryRPC"},
	{extapi.EntryNavigation, "extapi.EntryNavigation"},
	{extapi.EntrySearch, "extapi.EntrySearch"},
	{extapi.EntryTray, "extapi.EntryTray"},
	{extapi.EntryHotkey, "extapi.EntryHotkey"},
	{extapi.EntryMCP, "extapi.EntryMCP"},
	{extapi.EntryWindow, "extapi.EntryWindow"},
	{extapi.EntryBackground, "extapi.EntryBackground"},
}

// entrypointConst 把入口值映射为 extapi 常量名，供生成的 Go 表引用常量而非裸串。
var entrypointConst = func() map[extapi.Entrypoint]string {
	out := make(map[extapi.Entrypoint]string, len(entrypointOrder))
	for _, e := range entrypointOrder {
		out[e.value] = e.constName
	}
	return out
}()

// deliveryConst 把交付形态映射为 extapi 常量名（Phase 1-2 全部 builtin-logical）。
var deliveryConst = map[extapi.DeliveryKind]string{
	extapi.DeliveryBuiltinLogical:     "extapi.DeliveryBuiltinLogical",
	extapi.DeliveryManagedDeclarative: "extapi.DeliveryManagedDeclarative",
	extapi.DeliveryOfficialSidecar:    "extapi.DeliveryOfficialSidecar",
}

// ---------------------------------------------------------------------------
// 显式 allowlist（源码结构判不了入口归属，证据钉死在注释里的文件）
// ---------------------------------------------------------------------------

// hotkeyAllowlist 全局热键入口/能力白名单。
// 证据：internal/app/hotkeys.go 由装配根为 ocr 绑定剪贴板识图热键（槽位
// ocr/snip-clipboard）；internal/modules/msgboard/hotkey.go 为 msgboard 模块
// 自绑显隐热键（槽位 msgboard/toggle，随 OnInit/OnDestroy 驱动）。
var hotkeyAllowlist = map[string]bool{
	"ocr":      true,
	"msgboard": true,
}

// mcpAllowlist `hanxi mcp` 无头进程暴露工具的模块白名单。
// 证据：internal/mcp/mcp.go —— mcpModules() 挂载 envcheck、everything；
// Run 额外注册 ocr（ocrModule := ocr.New 并注入识图后端）；memo 为特例通道
// （构造带迁移写盘副作用不进 registry，registryGate 直读 config.json enabled 位）。
var mcpAllowlist = map[string]bool{
	"envcheck":   true,
	"everything": true,
	"ocr":        true,
	"memo":       true,
}

// ---------------------------------------------------------------------------
// 证据正则（口径与 internal/app/catalog_contract_test.go 的抽查锚定一致）
// ---------------------------------------------------------------------------

var (
	moduleIDRe   = regexp.MustCompile(`const ID = "([^"]+)"`)
	moduleNameRe = regexp.MustCompile(`Name:\s*"([^"]+)"`)
	// moduleDescRe 捕获 Info() 的 Description 带引号字面量（含转义序列），
	// 再用 strconv.Unquote 还原真实文本，防止描述里出现 \" 时提取错位。
	moduleDescRe  = regexp.MustCompile(`Description:\s*("(?:[^"\\]|\\.)*")`)
	trayCommandRe = regexp.MustCompile(`TrayCommands\(\) \[\]extapi\.TrayCommand`)
	windowOpenRe  = regexp.MustCompile(`Window\.NewWithOptions`)
	listenerRe    = regexp.MustCompile(`net\.Listen\(`)
)

// moduleEvidence 汇总单个模块目录的全部判定输入。
type moduleEvidence struct {
	pkg            string // 目录名
	id             string // module.go 的 const ID
	name           string // module.go Info() 的展示名
	description    string // module.go Info() 的描述（ModuleInfo.Description 同源）
	hasTray        bool
	hasWindow      bool
	hasListener    bool
	hasVersionDir  bool
	hasInstanceDir bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cataloggen:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	contract, err := loadContract(root)
	if err != nil {
		return err
	}

	evidences, err := scanModules(filepath.Join(root, "internal", "modules"))
	if err != nil {
		return err
	}

	items, err := buildItems(contract, evidences)
	if err != nil {
		return err
	}

	goSource, err := renderGoSource(items)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "app", "catalog.go"), goSource, 0o644); err != nil {
		return fmt.Errorf("写 internal/app/catalog.go: %w", err)
	}

	fixtureBytes, err := renderFixture(items)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "fixture", "module_catalog.json"), fixtureBytes, 0o644); err != nil {
		return fmt.Errorf("写 scripts/fixture/module_catalog.json: %w", err)
	}

	report(items)
	return nil
}

// repoRoot 从当前工作目录向上定位 go.mod 所在仓库根，保证在仓库任意子目录运行结果一致。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("取工作目录: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod（当前目录不在 hanxi 仓库内）")
		}
		dir = parent
	}
}

func loadContract(root string) (*contractFixture, error) {
	data, err := os.ReadFile(filepath.Join(root, "scripts", "fixture", "composition_contract.json"))
	if err != nil {
		return nil, fmt.Errorf("读取 composition contract: %w", err)
	}
	var contract contractFixture
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil, fmt.Errorf("解析 composition contract: %w", err)
	}
	return &contract, nil
}

// scanModules 遍历模块目录：无 module.go 的子目录按辅助包（如 modpath）处理不计入模块；
// 其余目录提取 ID/展示名并按规则收集源码证据。
func scanModules(modulesDir string) ([]moduleEvidence, error) {
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", modulesDir, err)
	}
	var out []moduleEvidence
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(modulesDir, entry.Name())
		moduleFile := filepath.Join(dir, "module.go")
		if _, statErr := os.Stat(moduleFile); statErr != nil {
			continue // 非模块辅助包
		}
		moduleSource, err := os.ReadFile(moduleFile)
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", moduleFile, err)
		}
		id := firstGroup(moduleIDRe, string(moduleSource))
		if id == "" {
			return nil, fmt.Errorf("%s 未找到 `const ID = \"...\"`", moduleFile)
		}
		name := firstGroup(moduleNameRe, string(moduleSource))
		if name == "" {
			return nil, fmt.Errorf("%s 未找到 Info() 的 `Name:` 展示名", moduleFile)
		}
		descLiteral := firstGroup(moduleDescRe, string(moduleSource))
		if descLiteral == "" {
			return nil, fmt.Errorf("%s 未找到 Info() 的 `Description:` 描述", moduleFile)
		}
		description, err := strconv.Unquote(descLiteral)
		if err != nil {
			return nil, fmt.Errorf("%s Info() 的 `Description:` 字面量解析失败: %w", moduleFile, err)
		}
		if description == "" {
			return nil, fmt.Errorf("%s Info() 的 `Description:` 为空", moduleFile)
		}
		packageSource, err := collectNonTestSources(dir)
		if err != nil {
			return nil, err
		}
		out = append(out, moduleEvidence{
			pkg:            entry.Name(),
			id:             id,
			name:           name,
			description:    description,
			hasTray:        trayCommandRe.MatchString(packageSource),
			hasWindow:      windowOpenRe.MatchString(packageSource),
			hasListener:    listenerRe.MatchString(packageSource),
			hasVersionDir:  dirExists(filepath.Join(dir, "version")),
			hasInstanceDir: dirExists(filepath.Join(dir, "instance")),
		})
	}
	return out, nil
}

// collectNonTestSources 拼接模块目录内全部非 _test.go 的 Go 源码（证据扫描口径）。
func collectNonTestSources(dir string) (string, error) {
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
		return "", fmt.Errorf("扫描 %s: %w", dir, err)
	}
	return sb.String(), nil
}

func firstGroup(re *regexp.Regexp, source string) string {
	match := re.FindStringSubmatch(source)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// buildItems 按证据规则装配目录项，并对未知模块 ID fail loud；结果按 ID 排序。
func buildItems(contract *contractFixture, evidences []moduleEvidence) ([]extapi.ModuleCatalogItem, error) {
	byID := make(map[string]moduleEvidence, len(evidences))
	for _, ev := range evidences {
		if _, dup := byID[ev.id]; dup {
			return nil, fmt.Errorf("模块 ID %q 重复（目录 %s）", ev.id, ev.pkg)
		}
		byID[ev.id] = ev
	}
	fixtureIDs := make(map[string]bool, len(contract.Modules))
	for _, module := range contract.Modules {
		if fixtureIDs[module.ID] {
			return nil, fmt.Errorf("composition contract 模块 ID %q 重复", module.ID)
		}
		fixtureIDs[module.ID] = true
		if _, ok := byID[module.ID]; !ok {
			return nil, fmt.Errorf("fixture 模块 %q 在 internal/modules 无对应 module.go", module.ID)
		}
	}
	// 反向清点：目录有而 fixture 无（新模块未登记 composition contract）同样 fail loud。
	var unknown []string
	for id := range byID {
		if !fixtureIDs[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("模块目录 %v 未出现在 composition contract 中", unknown)
	}

	preactivated := make(map[string]bool, len(contract.Wiring.PreactivatedModules))
	for _, id := range contract.Wiring.PreactivatedModules {
		preactivated[id] = true
	}

	items := make([]extapi.ModuleCatalogItem, 0, len(contract.Modules))
	for _, module := range contract.Modules {
		ev := byID[module.ID]
		items = append(items, extapi.ModuleCatalogItem{
			ID:           ev.id,
			Name:         ev.name,
			Description:  ev.description,
			Category:     module.Group,
			Delivery:     extapi.DeliveryBuiltinLogical,
			Capabilities: capabilitiesOf(ev, preactivated),
			Entrypoints:  entrypointsOf(ev, preactivated),
			Compatibility: extapi.Compatibility{
				HostRange: "*",
				Platform:  []string{"windows"},
			},
			Permissions: []extapi.Permission{},
			Owner:       "hanxi",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// entrypointsOf 按固定顺序输出模块入口集合：rpc/navigation/search 恒含，其余按证据追加。
func entrypointsOf(ev moduleEvidence, preactivated map[string]bool) []extapi.Entrypoint {
	has := map[extapi.Entrypoint]bool{
		extapi.EntryRPC:        true,
		extapi.EntryNavigation: true,
		extapi.EntrySearch:     true,
		extapi.EntryTray:       ev.hasTray,
		extapi.EntryHotkey:     hotkeyAllowlist[ev.id],
		extapi.EntryMCP:        mcpAllowlist[ev.id],
		extapi.EntryWindow:     ev.hasWindow,
		extapi.EntryBackground: preactivated[ev.id],
	}
	out := make([]extapi.Entrypoint, 0, len(has))
	for _, e := range entrypointOrder {
		if has[e.value] {
			out = append(out, e.value)
		}
	}
	return out
}

// capabilitiesOf 输出按字母序排列的能力标记集合。
func capabilitiesOf(ev moduleEvidence, preactivated map[string]bool) []string {
	var caps []string
	add := func(name string, ok bool) {
		if ok {
			caps = append(caps, name)
		}
	}
	add(capTrayCommands, ev.hasTray)
	add(capHostedVersions, ev.hasVersionDir)
	add(capManagedProcess, ev.hasInstanceDir)
	add(capDedicatedWindow, ev.hasWindow)
	add(capEmbeddedServer, ev.hasListener)
	add(capHotkeySlot, hotkeyAllowlist[ev.id])
	add(capMcpTools, mcpAllowlist[ev.id])
	add(capBackgroundListen, preactivated[ev.id])
	sort.Strings(caps)
	// 空能力必须序列化为 []（非 null），与 fixture 深比对才稳定。
	if caps == nil {
		caps = []string{}
	}
	return caps
}

// ---------------------------------------------------------------------------
// 产物渲染（Go 表 + JSON 基线，均确定性输出）
// ---------------------------------------------------------------------------

const generatedHeader = `// Code generated by "scripts/cataloggen"; DO NOT EDIT.
//
// catalog.go 是装配根模块目录静态表（Wave 0「模块目录清点」产物）：模块身份、交付形态、
// 入口与能力标记全部由 scripts/cataloggen 从源码证据与 composition 契约推导。
// 本文件为生成物：修改请调整 scripts/cataloggen/main.go 的规则后重新运行
//
//	go run ./scripts/cataloggen
//
// 重生成，勿手改；基线冻结于 scripts/fixture/module_catalog.json，
// 漂移由 TestModuleCatalogMatchesFixture（internal/app/catalog_contract_test.go）守护。

package app

import "hanxi/internal/extapi"

// catalogItems 是模块目录静态表，按 ID 升序生成。
var catalogItems = []extapi.ModuleCatalogItem{
`

const generatedFooter = `}

// CatalogItems 返回按 ID 排序的全部目录项（深拷贝，调用方可自由改动不回污静态表）。
func CatalogItems() []extapi.ModuleCatalogItem {
	out := make([]extapi.ModuleCatalogItem, len(catalogItems))
	for i, item := range catalogItems {
		out[i] = cloneCatalogItem(item)
	}
	return out
}

// CatalogItemOf 按模块 ID 查找目录项（深拷贝）；未知 ID 返回 false。
func CatalogItemOf(id string) (extapi.ModuleCatalogItem, bool) {
	for _, item := range catalogItems {
		if item.ID == id {
			return cloneCatalogItem(item), true
		}
	}
	return extapi.ModuleCatalogItem{}, false
}

// cloneCatalogItem 深拷贝全部切片字段，保持空切片的非 nil 语义（JSON 序列化为 []）。
func cloneCatalogItem(item extapi.ModuleCatalogItem) extapi.ModuleCatalogItem {
	clone := item
	clone.Capabilities = make([]string, len(item.Capabilities))
	copy(clone.Capabilities, item.Capabilities)
	clone.Entrypoints = make([]extapi.Entrypoint, len(item.Entrypoints))
	copy(clone.Entrypoints, item.Entrypoints)
	clone.Compatibility.Platform = make([]string, len(item.Compatibility.Platform))
	copy(clone.Compatibility.Platform, item.Compatibility.Platform)
	clone.Permissions = make([]extapi.Permission, len(item.Permissions))
	copy(clone.Permissions, item.Permissions)
	return clone
}
`

// renderGoSource 生成 internal/app/catalog.go 的完整内容，经 gofmt 规范化后返回。
func renderGoSource(items []extapi.ModuleCatalogItem) ([]byte, error) {
	var b strings.Builder
	b.WriteString(generatedHeader)
	for _, item := range items {
		b.WriteString("\t{\n")
		fmt.Fprintf(&b, "\t\tID: %s,\n", strconv.Quote(item.ID))
		fmt.Fprintf(&b, "\t\tName: %s,\n", strconv.Quote(item.Name))
		fmt.Fprintf(&b, "\t\tDescription: %s,\n", strconv.Quote(item.Description))
		fmt.Fprintf(&b, "\t\tCategory: %s,\n", strconv.Quote(item.Category))
		fmt.Fprintf(&b, "\t\tDelivery: %s,\n", deliveryConst[item.Delivery])
		fmt.Fprintf(&b, "\t\tCapabilities: %s,\n", quoteStrings(item.Capabilities, "string"))
		fmt.Fprintf(&b, "\t\tEntrypoints: %s,\n", quoteEntrypoints(item.Entrypoints))
		fmt.Fprintf(&b, "\t\tCompatibility: extapi.Compatibility{HostRange: %s, Platform: %s},\n",
			strconv.Quote(item.Compatibility.HostRange), quoteStrings(item.Compatibility.Platform, "string"))
		fmt.Fprintf(&b, "\t\tPermissions: %s,\n", quoteStrings(nil, "extapi.Permission"))
		fmt.Fprintf(&b, "\t\tOwner: %s,\n", strconv.Quote(item.Owner))
		b.WriteString("\t},\n")
	}
	b.WriteString(generatedFooter)

	// go/format 与 gofmt 同规则：对齐字段、规范空白，保证输出字节级确定。
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("规范化 catalog.go 失败: %w", err)
	}
	return formatted, nil
}

func quoteStrings(values []string, elemType string) string {
	if len(values) == 0 {
		return "[]" + elemType + "{}"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[]" + elemType + "{" + strings.Join(quoted, ", ") + "}"
}

func quoteEntrypoints(values []extapi.Entrypoint) string {
	if len(values) == 0 {
		return "[]extapi.Entrypoint{}"
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		constName, ok := entrypointConst[value]
		if !ok {
			// 生成期兜底：入口值必须是契约常量，出现未知值说明装配规则写错。
			constName = "extapi.Entrypoint(" + strconv.Quote(string(value)) + ")"
		}
		out = append(out, constName)
	}
	return "[]extapi.Entrypoint{" + strings.Join(out, ", ") + "}"
}

// renderFixture 生成 scripts/fixture/module_catalog.json（缩进 2 空格 + 换行收尾）。
func renderFixture(items []extapi.ModuleCatalogItem) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalogFixtureFile{
		Schema: extapi.ModuleContractSchema,
		Items:  items,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 module_catalog.json: %w", err)
	}
	return append(encoded, '\n'), nil
}

// report 打印清点摘要，便于人工核对证据规则命中面。
func report(items []extapi.ModuleCatalogItem) {
	counts := map[extapi.Entrypoint]int{}
	caps := map[string]int{}
	for _, item := range items {
		for _, entry := range item.Entrypoints {
			counts[entry]++
		}
		for _, capability := range item.Capabilities {
			caps[capability]++
		}
	}
	fmt.Printf("cataloggen: %d 个模块目录项；入口 tray=%d hotkey=%d mcp=%d window=%d background=%d；"+
		"能力 hosted-versions=%d managed-process=%d embedded-server=%d\n",
		len(items),
		counts[extapi.EntryTray], counts[extapi.EntryHotkey], counts[extapi.EntryMCP],
		counts[extapi.EntryWindow], counts[extapi.EntryBackground],
		caps[capHostedVersions], caps[capManagedProcess], caps[capEmbeddedServer])
}
