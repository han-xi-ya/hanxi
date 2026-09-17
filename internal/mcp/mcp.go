// Package mcp 实现 `hanxi mcp` 无头 stdio MCP server（PLAN_MCP）。
//
// 本包是 ADR，三条核心决策（与 GUI 主程序的行为差异全部收口在这里）：
//
//  1. 只暴露只读工具。工具面 = access.json 授权 ∩ 模块启用，两门全过也只放行查询；
//     提权/写操作（portkill、frpc、配置写入、OpenTarget 执行链等）永不注册为工具，
//     这是编译期事实而非运行期约束——tools/list 里没有的东西，客户端永远调不到。
//     一切工具输出按"会原样进入云端模型上下文"审视：敏感串过 logging.Redact，
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
	"hanxi/internal/modules/memo"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// ModuleGate 是工具调用前的"模块可用"门（真实现 = registryGate；单测注入假件）。
type ModuleGate interface {
	// Check 报告模块当前可服务：已启用且懒初始化就绪。返回的 error 面向模型可读。
	Check(moduleID string) error
}

// registryGate 组合 extapi.Registry（envcheck 等已注册模块）与 settings.Store
// 的启用位（memo 特殊通道），给出统一的中文指引错误。
type registryGate struct {
	registry *extapi.Registry
	store    *settings.Store
}

func (g *registryGate) Check(moduleID string) error {
	// memo 模块构造带文件库迁移写盘副作用，与无头"零落盘"承诺冲突（包注释决策 3）：
	// 不进 registry、不走 EnsureActive，门禁直读 config.json 的 enabled 位保持同语义。
	if moduleID == memo.ID {
		if !g.store.IsModuleEnabled(moduleID, true) {
			return fmt.Errorf("「极客随手记」模块已在 hanxi 中停用，请先在设置中启用该模块")
		}
		return nil
	}
	if err := g.registry.EnsureActive(moduleID); err != nil {
		switch {
		case errors.Is(err, extapi.ErrModuleDisabled):
			return fmt.Errorf("模块「%s」已在 hanxi 中停用，请先在设置中启用", moduleID)
		case errors.Is(err, extapi.ErrUnknownModule):
			return fmt.Errorf("hanxi 无头模式未装配模块「%s」（版本不匹配或该模块不可用）", moduleID)
		default:
			return fmt.Errorf("模块「%s」初始化失败: %v", moduleID, err)
		}
	}
	return nil
}

// Run 是 `hanxi mcp` 的无头主流程（由 cmd/hanxi 在 flag.Parse 之前短路进来）：
// InitPaths → 只读 settings → slog 落文件（stderr 兜底）→ windows.New →
// Registry 注册目标模块（enabled 沿用 config.json）→ 组工具表 → ServeStdio。
// 阻塞至客户端关闭 stdin（会话即进程生命周期），退出前 ShutdownAll 收尾。
func Run() error {
	paths := settings.InitPaths()

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
	if err := registry.Register(mcpModules(plat)...); err != nil {
		return fmt.Errorf("注册无头模块失败: %w", err)
	}
	defer registry.ShutdownAll()

	deps := Deps{
		Access: NewAccess(filepath.Join(paths.DataDir(), accessDirName, AccessFileName)),
		Gate:   &registryGate{registry: registry, store: store},
		// envcheck 后端独立于模块实例直构（plat 仅作 OpenURL，nil 守卫）——
		// 与 registry 中模块共享同一份探测框架（detect 包级注册表），无状态分歧。
		EnvCheck: envcheck.NewEnvCheckService(nil),
		// everything 走独立严格只读通道（不复用 service.Search 的懒启动/下载编排，
		// 决策 3-B）；registry 内仍注册该模块，只取 enabled 门禁与生命周期收口。
		Search: newStrictSearcher(plat),
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
