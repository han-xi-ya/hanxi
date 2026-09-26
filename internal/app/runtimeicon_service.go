// runtimeicon_service.go 是"N27 红线图标运行期本机提取通道"的集中服务面：
// 平台级直挂服务（snapSvc/mcpwizard 同位置先例），一枚 RPC 打通三家——
// 模块只经 extapi.IconSourceProvider 供路径，解析/缓存/门语义全部收口本文件。
//
// 许可红线口径（docs/THIRD_PARTY_NOTICES.md「运行期本机提取」节）：
// rammap/recordly/vscode 的厂商图标永不入库、不随发布物分发；本服务只在
// 运行期读机主本机已装的官方 exe，就地提取最大画幅 PNG 供本机渲染，
// 结果仅存进程内存（不落盘、不入前端构建产物），关进程即消失。
//
// 门语义（与模块 RPC 的 LeaseHolder.Enter 口径刻意有别，详见
// extapi/iconsource.go 注释）：图标是纯展示元数据，只要求目标模块
// "已安装 + 未停用"，绝不触发懒激活——为绘一枚徽标叫醒睡眠模块
// （OnInit 起轮询/建资源）是不可接受的副作用。停用/卸载/未知模块三类
// 拒绝都如实回 error（复用 extapi 哨兵，前端据此回落矢量，不打扰用户）。
package app

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"hanxi/internal/extapi"
	"hanxi/packages/go/peicon"
)

// RuntimeIconService 运行期真图标提取服务：进程内缓存 + 源文件指纹失效。
type RuntimeIconService struct {
	registry *extapi.Registry

	// cache moduleID → 提取账目。正缓存（png≠nil）供渲染直读；负缓存
	// （png=nil）连同源指纹记录失败事实，防每次渲染都撞一遍解析。
	// 源文件 size/mtime 任一变化（重装/升级/换 active 版本）即作废重取。
	cache sync.Map
}

// runtimeIconEntry 单模块提取账目：png=nil 即负缓存。
type runtimeIconEntry struct {
	png     []byte
	exe     string    // 提取源路径（诊断用）
	size    int64     // 源文件字节数指纹
	modTime time.Time // 源文件修改时间指纹
	err     string    // 负缓存失败原因（透传给诊断日志）
	at      time.Time // 记账时刻（日志用）
}

// NewRuntimeIconService 装配集中图标服务；构造无 IO。
func NewRuntimeIconService(registry *extapi.Registry) *RuntimeIconService {
	return &RuntimeIconService{registry: registry}
}

// IconPNG 返回模块厂商图标的 PNG 字节（最大真实画幅；Vista+ PNG 帧原样
// 透传、经典 DIB 帧转码）。任何不可用态（载荷不在位/解析失败/停用）都
// 如实回 error——前端 resolveIcon 轨自动回落 `rt:<id>|i:<name>` 声明的
// 矢量徽标，零观感损失零打扰。
//
// Wails 序列化口径：[]byte 经 JSON 出货为 base64 字符串，前端
// （constants/runtimeIcons.ts）拼 data URL 交 <img> 消费。
func (s *RuntimeIconService) IconPNG(moduleID string) ([]byte, error) {
	if !s.registry.HasModule(moduleID) {
		return nil, fmt.Errorf("%w %q", extapi.ErrUnknownModule, moduleID)
	}
	provider, ok := s.registry.IconSourceProviderOf(moduleID)
	if !ok {
		// 模块存在但未挂提取源（非红线三家 / 未来新模块未实现契约）。
		return nil, fmt.Errorf("模块 %q 未提供运行期图标提取源", moduleID)
	}
	if !s.registry.IsInstalled(moduleID) {
		return nil, fmt.Errorf("%w %q", extapi.ErrModuleNotInstalled, moduleID)
	}
	if !s.registry.IsEnabled(moduleID) {
		return nil, fmt.Errorf("%w %q", extapi.ErrModuleDisabled, moduleID)
	}

	exe, err := provider.IconSourceExe()
	if err != nil {
		return nil, fmt.Errorf("解析 %s 图标提取源失败: %w", moduleID, err)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return nil, fmt.Errorf("图标提取源不可读: %w", err)
	}

	cached := s.cached(moduleID, exe, fi)
	if cached != nil {
		if cached.png == nil {
			return nil, fmt.Errorf("%s 图标提取此前已失败（%s），源未变化不重试", moduleID, cached.err)
		}
		return cached.png, nil
	}

	icon, err := peicon.Extract(exe)
	if err != nil {
		s.cache.Store(moduleID, &runtimeIconEntry{exe: exe, size: fi.Size(), modTime: fi.ModTime(), err: err.Error(), at: time.Now()})
		slog.Info("运行期图标提取失败（已落负缓存）", "module", moduleID, "exe", exe, "err", err)
		return nil, err
	}
	s.cache.Store(moduleID, &runtimeIconEntry{png: icon.PNG, exe: exe, size: fi.Size(), modTime: fi.ModTime(), at: time.Now()})
	slog.Info("运行期图标提取成功", "module", moduleID, "exe", exe, "canvas", fmt.Sprintf("%dx%d", icon.Width, icon.Height), "format", icon.Format, "bytes", len(icon.PNG))
	return icon.PNG, nil
}

// cached 按"路径 + 字节数 + 修改时间"三指纹命中账目；任一不符视为失效。
func (s *RuntimeIconService) cached(moduleID, exe string, fi os.FileInfo) *runtimeIconEntry {
	v, ok := s.cache.Load(moduleID)
	if !ok {
		return nil
	}
	entry := v.(*runtimeIconEntry)
	if entry.exe != exe || entry.size != fi.Size() || !entry.modTime.Equal(fi.ModTime()) {
		return nil
	}
	return entry
}
