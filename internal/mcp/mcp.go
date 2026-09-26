// Package mcp 实现 `hanxi mcp` 无头 stdio MCP server（PLAN_MCP）。
//
// 本包是 ADR，三条核心决策（与 GUI 主程序的行为差异全部收口在这里）：
//
//  1. 只暴露"不改状态"的工具：纯查询族（envcheck/everything/ocr/memo/sysinfo/logs）
//     严格只读；扫描族（portscan/lan）是**主动网络探测**——不修改本机或任何设备
//     状态，但会出网、结果含网络拓扑，因此各立授权键（access.json 八键，默认全关），
//     参数面收口为有界小扫描（见 tools_scan.go 包注），与纯查询工具同等门禁。
//     提权/写操作（portkill 杀进程、frpc、配置写入、OpenTarget 执行链等）永不注册
//     为工具，这是编译期事实而非运行期约束——tools/list 里没有的东西，客户端永远调不到。
//     一切工具输出按"会原样进入云端模型上下文"审视：敏感串过 logging.Redact 家族，
//     memo 遮罩条目（IsMasked）整条不下发，载荷上限 1MB（截断显式置 truncated 标志）。
//
//  2. stdout 即协议通道。MCP 走 stdio JSON-RPC，本包任何代码（含日志）不得写
//     os.Stdout——日志经 logging.InitLogger 落文件+stderr；该纪律由
//     guards_test.go 的静态扫描与 stdio 帧级测试双重守卫。
//
//  3. 与 GUI 主程序并存、对 hanxidata 严格只读。不装配 WebView/托盘/全局钩子，
//     不进单实例锁（mcp 分支在 application.New 之前短路）；与主程序共读同一数据根，
//     状态文件写盘均为原子 rename，读侧永远看到完整旧版或完整新版。MCP 进程自身
//     零落盘承诺：不迁移、不隔离、不创建任何数据文件（因此 memo 模块不进无头
//     registry——其构造携带旧库迁移写盘副作用，改走零写盘直读通道，门禁口径不变）。
package mcp

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/extapi"
	"hanxi/internal/logging"
	"hanxi/internal/modules/envcheck"
	"hanxi/internal/modules/everything"
	"hanxi/internal/modules/lan"
	"hanxi/internal/modules/memo"
	"hanxi/internal/modules/ocr"
	"hanxi/internal/modules/portscan"
	"hanxi/internal/modules/sysinfo"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// ModuleGate 是工具调用前的"模块可用"门（真实现 = registryGate；单测注入假件）。
type ModuleGate interface {
	// Check 裁决模块当前可服务（安装/启用/就绪）并取得 operation lease：
	// 返回的 release 必须在工具调用结束后 defer 调用（停用 drain 覆盖无头在途）；
	// 返回的 error 面向模型可读。
	Check(moduleID string) (release func(), err error)
}

// registryGate 组合 extapi.Registry（envcheck 等已注册模块）与 settings.Store
// 的启用位（memo 特殊通道），给出统一的中文指引错误。
type registryGate struct {
	registry *extapi.Registry
	store    *settings.Store
	// receipts 承载 memo 特殊通道的逻辑安装态（其余模块经 registry receipt 门）；
	// nil 时按已安装处理，与 Registry 未注入时的回退语义一致。
	receipts extapi.ReceiptStorage
}

func (g *registryGate) Check(moduleID string) (func(), error) {
	noop := func() {}
	// logs 工具（N34）背后没有业务模块——读的是 hanxi 自己的落盘日志，无启用位、
	// 无生命周期可查：access.json 的 logs 键即唯一授权门（脱敏在 handler 出机前
	// 强制执行），这里放行不构成旁路，也不存在可被占用的租约。
	if moduleID == accessKeyLogs {
		return noop, nil
	}
	// memo 模块构造带文件库迁移写盘副作用，与无头"零落盘"承诺冲突（包注释决策 3）：
	// 不进 registry、不走 Acquire，门禁直读 config.json 的 enabled 位 + receipt
	// 保持同语义（未安装同样拒绝，杜绝卸载旁路）；无租约可占用，release 为空操作。
	if moduleID == memo.ID {
		if !g.store.IsModuleEnabled(moduleID, true) {
			return noop, fmt.Errorf("「随手记」模块已在 hanxi 中停用，请先在设置中启用该模块")
		}
		if g.receipts != nil && !g.receipts.IsInstalled(moduleID) {
			return noop, fmt.Errorf("「随手记」模块尚未安装，请先在 hanxi 模块中心安装该模块")
		}
		return noop, nil
	}
	// 经统一 Acquire 取租约：无头在途调用同样纳入停用/退出的 drain 门。
	_, release, err := g.registry.Acquire(moduleID)
	if err != nil {
		switch {
		case errors.Is(err, extapi.ErrModuleDisabled):
			return nil, fmt.Errorf("模块「%s」已在 hanxi 中停用，请先在设置中启用", moduleID)
		case errors.Is(err, extapi.ErrModuleNotInstalled):
			return nil, fmt.Errorf("模块「%s」尚未安装，请先在 hanxi 模块中心安装", moduleID)
		case errors.Is(err, extapi.ErrUnknownModule):
			return nil, fmt.Errorf("hanxi 无头模式未装配模块「%s」（版本不匹配或该模块不可用）", moduleID)
		default:
			return nil, fmt.Errorf("模块「%s」初始化失败: %v", moduleID, err)
		}
	}
	return release, nil
}

// Run 是 `hanxi mcp` 的无头主流程（由 cmd/hanxi 在 flag.Parse 之前短路进来）：
// InitPaths → 只读 settings → slog 落文件（stderr 兜底）→ windows.New →
// Registry 注册目标模块（enabled 沿用 config.json）→ 组工具表 → ServeStdio。
// 阻塞至客户端关闭 stdin（会话即进程生命周期），退出前 ShutdownAll 收尾。
func Run() error {
	paths := settings.InitPaths()
	if err := paths.InitError(); err != nil {
		// 数据根不可用 fail loud（F6）：无头模式经 stderr 报引导文案退出，不弹 GUI 对话框。
		return fmt.Errorf("数据目录不可用: %w", err)
	}

	// 配置读取失败 = fail-closed 直接退出：损坏 config.json 的隔离降级是 GUI 的
	// 修复通道（有用户可见的提示），无头模式不抢这个动作，也不静默带病服务。
	store, err := settings.NewStore(paths.ConfigFile())
	if err != nil {
		return fmt.Errorf("读取 hanxi 配置失败（请在 hanxi 主程序中修复 config.json 后重试）: %w", err)
	}

	// 日志必须早于一切模块构造就位；InitLogger 失败不致命（slog 回落 stderr）。
	if _, logCleanup, lerr := logging.InitLogger(paths.LogsDir(), store.Get().LogRetainDays); lerr == nil {
		defer logCleanup()
	}
	slog.Info("hanxi mcp: headless server starting",
		"mode", paths.Mode(), "baseDir", paths.BaseDir(), "version", product.Version)

	plat, err := windows.New()
	if err != nil {
		return fmt.Errorf("初始化 Windows 平台原语失败: %w", err)
	}

	registry := extapi.NewRegistry(store)
	// 逻辑安装态与 GUI 同账（只读）：未安装模块经 EnsureActive 的 receipt 门
	// 拒绝无头调用，杜绝"GUI 卸载、MCP 仍可用"旁路；无头零落盘承诺不受影响。
	headlessReceipts := settings.NewReceiptStore(paths.ModulesReceiptsDir(), paths.ModulesLedgerFile())
	registry.SetReceiptStorage(headlessReceipts)
	ocrModule := ocr.New(plat) // 类型断言取 service 作识图后端（与 GUI 同一 service 契约）
	sysModule := sysinfo.New() // 同上：N32 系统档案后端取同一 service 实例
	// 扫描族（AI 接入批）：两模块构造均零副作用（无落盘/无常驻资源，OnInit nil），
	// 进无头 registry 取"与 GUI 同谱的启用位门禁 + lease drain 收口"；lan 的
	// service 实例被 MCP 后端复用（单飞闸与动态超时同谱），portscan 只进门禁、
	// 后端直构 Scanner（规避 StartScan 的 GUI last-wins 顶任务语义，见 tools_scan.go）。
	lanModule := lan.New(plat, store)
	portscanModule := portscan.New()
	if err := registry.Register(append(mcpModules(plat), ocrModule, sysModule, lanModule, portscanModule)...); err != nil {
		return fmt.Errorf("注册无头模块失败: %w", err)
	}
	defer registry.ShutdownAll()

	deps := Deps{
		Access: NewAccess(filepath.Join(paths.DataDir(), accessDirName, AccessFileName)),
		Gate:   &registryGate{registry: registry, store: store, receipts: headlessReceipts},
		// envcheck 后端独立于模块实例直构（plat 仅作 OpenURL，nil 守卫）——
		// 与 registry 中模块共享同一份探测框架（detect 包级注册表），无状态分歧。
		// holder 不注门：本直构实例不属无头 registry 的租约账（无 Init/Destroy 生命周期），
		// 工具面门禁已由 registryGate.Acquire 承担，此处放行不构成旁路。
		EnvCheck: envcheck.NewEnvCheckService(nil, extapi.NewLeaseHolder("envcheck")),
		// everything 走独立严格只读通道（不复用 service.Search 的懒启动/下载编排，
		// 决策 3-B）；registry 内仍注册该模块，只取 enabled 门禁与生命周期收口。
		Search: newStrictSearcher(plat),
		// memo 走零落盘直读通道（绕开携带迁移/隔离写盘副作用的 MemoService 构造链，
		// 包注释决策 3）；门禁仍按 config.json enabled 位（registryGate）。
		Memo: newMemoDiskReader(),
		// sysinfo（N32）取表内模块同一 service（纯采集零副作用，直构无收益分歧）；
		// logs（N34）是 hanxi 自身日志的只读 tail，无模块后端，直读 <DataDir>/logs。
		Logs: newLogDiskReader(),
		// 扫描族（AI 接入批）：portscan 直构引擎实例（MCP 面单飞闸在 backend 内，
		// 门禁由 registryGate.Acquire("portscan") 承担——envcheck 直构同款口径）；
		// lan 复用表内模块 service（单飞/超时/ARP 补全与 GUI 同谱）。
		PortScan: newPortScanBackend(),
	}
	if lanMod, ok := lanModule.(*lan.Module); ok && lanMod != nil {
		deps.Lan = lanProbe{svc: lanMod.Service()}
	}
	if ocrMod, ok := ocrModule.(*ocr.Module); ok && ocrMod != nil {
		// service 无头可用：client 已显式 Proxy:nil（回环 HTTP 纪律）、构造零落盘、
		// 事件出口 application.Get() nil 守卫；服务离线时 RecognizeImage 返回
		// "请先启动服务"业务指引而非拉起（§2.2 ocr 行）。
		deps.OCR = ocrMod.Service()
	}
	if sysMod, ok := sysModule.(*sysinfo.Module); ok && sysMod != nil {
		// GetReport 内 holder.Enter 走统一调用门：门禁已由 registryGate.Acquire
		// 在中间件层完成（懒激活+租约），此处只是接上同一实例。
		deps.SysInfo = sysMod.Service()
	}

	srv := NewMCPServer(deps)
	// 协议错误日志显式钉死 stderr（stdout 是 JSON-RPC 通道，包注释纪律 2）。
	return server.ServeStdio(srv,
		server.WithErrorLogger(log.New(os.Stderr, "hanxi-mcp: ", log.LstdFlags)))
}

// mcpModules 无头进程挂载的模块集合（enabled 门禁与 EnsureActive 懒激活的来源）。
// 注意两条例外：memo 因构造带迁移写盘不进此表（见 registryGate 注释）；各工具后端
// 也未必复用表内实例（envcheck 直构 service、everything 走独立严格只读通道），
// 表内模块的价值是"与 GUI 同谱的启用状态与生命周期收口（ShutdownAll）"。
func mcpModules(plat platform.Platform) []extapi.Module {
	return []extapi.Module{
		envcheck.New(plat),
		everything.New(plat),
	}
}
