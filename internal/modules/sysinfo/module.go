// Package sysinfo 提供"系统信息"静态软硬件档案模块（借鉴 MooTool 机制，
// N10 排期项）：CPU/内存/主板/显卡/显示器/磁盘卷/网络接口/OS 一屏总览。
// 实现与边界说明见 report.go 包注释。
package sysinfo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
)

// ID 是模块注册键。
const ID = "sysinfo"

// Module extapi.Module 契约载体。
type Module struct {
	svc *SysInfoService
}

// New 装配模块（无 IO 副作用；采集全部延迟到 RPC 调用）。
func New() extapi.Module {
	return &Module{svc: NewSysInfoService(extapi.NewLeaseHolder(ID))}
}

// Info 模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "系统信息",
		Version:     "0.1.0",
		Description: "本机软硬件静态档案一览：CPU/内存/主板/显卡/显示器/磁盘/网络/OS（纯只读采集）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 注入统一调用门。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Nav 侧边栏入口（系统组，紧邻开发环境检测）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      "sysinfo-main",
		Title:   "系统信息",
		Route:   "/ext/sysinfo",
		Icon:    "i:cpu",
		Section: extapi.SectionExt,
		Order:   64,
		Group:   extapi.GroupSystem,
	}}
}

// Services RPC 面。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) OnInit(context.Context) error { return nil }
func (m *Module) OnDestroy() error             { return nil }

// IsInitialized 无初始化失败路径。
func (m *Module) IsInitialized() bool { return true }
