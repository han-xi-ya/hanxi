package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/modules/sysinfo"
)

// tools_sysinfo.go 是 N32 系统信息只读工具（hanxi_sysinfo_report）：GUI「系统
// 信息」模块同一 service 直供，零新采集逻辑。档位化（学 ocr 全文档先例）：
// 整份 report 体量偏大（八段全量 + 每卷/每接口逐项），默认 overview 摘要档，
// 需要 MAC/BIOS/卷标级细节时显式要 full。
//
// 敏感面口径（N32 ③ 如实评估）：report 含计算机名/产品 ID/IP/MAC/盘符与卷标，
// 经本地 stdio 喂给用户自己的 AI 客户端属"自机信息自取"，不新增外泄面；
// 但工具描述讲清返回内容，让模型与用户都知道这一档会露出什么。

// ReportSource 是 hanxi_sysinfo_report 的后端能力面（真 = *sysinfo.SysInfoService，
// 与 GUI 同一实例契约；error 为调用门拒绝通道，Wave 3 统一门）。
type ReportSource interface {
	GetReport() (sysinfo.Report, error)
}

// buildSysInfoTool 软硬件档案只读查询：纯本机注册表/WinAPI 采集，零出网。
func buildSysInfoTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolSysInfo,
		mcp.WithDescription("采集本机软硬件静态档案快照（只读，纯注册表/WinAPI/标准库调用，不联网），"+
			"两档：overview（默认）给整机/系统/CPU/内存/显卡/显示器/磁盘/联网接口的摘要视图；"+
			"full 追加 BIOS 版本、Windows 产品 ID、网卡 MAC 与环回接口、卷文件系统明细、内存提交量等逐项细节。"+
			"返回内容含计算机名与本机 IP/MAC（full 档）等机器指纹信息——仅经本地 stdio 进入你自己的 AI 会话。"+
			"个别字段可能因采集降级为空，errors 数组如实列出失败段。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("level", mcp.Description("档位：overview（默认，摘要）或 full（全量逐项）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.SysInfo == nil {
			return mcp.NewToolResultError("sysinfo 后端未装配：请在 hanxi 主程序中确认「系统信息」模块可用"), nil
		}
		level := req.GetString("level", "overview")
		rep, err := deps.SysInfo.GetReport()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("系统档案采集被拒绝: %v", err)), nil
		}
		switch level {
		case "full":
			return textResult(rep)
		case "", "overview":
			return textResult(overviewPayload(rep))
		default:
			return mcp.NewToolResultError(fmt.Sprintf("未知档位 %q：只支持 overview（默认）与 full", level)), nil
		}
	}
	return tool, handler
}

// overviewPayload 摘要档：保留"这台机器是什么"的主干问答字段，剥掉
// BIOS/产品 ID/MAC/环回接口/内存提交量等 full 档细节与逐字段冗余。
// 段级 errors 两档都透传——降级不可见比信息不全更糟。
func overviewPayload(rep sysinfo.Report) resultPayload {
	nics := make([]any, 0, len(rep.Network))
	for _, n := range rep.Network {
		if n.Loopback {
			continue
		}
		nics = append(nics, resultPayload{
			"name": n.Name, "up": n.Up, "addresses": n.Addresses,
		})
	}
	displays := make([]any, 0, len(rep.Displays))
	for _, d := range rep.Displays {
		displays = append(displays, resultPayload{
			"width": d.Width, "height": d.Height, "refreshHz": d.RefreshHz, "primary": d.Primary,
		})
	}
	gpus := make([]any, 0, len(rep.GPUs))
	for _, g := range rep.GPUs {
		gpus = append(gpus, resultPayload{"desc": g.Desc, "driverVersion": g.DriverVersion})
	}
	vols := make([]any, 0, len(rep.Volumes))
	for _, v := range rep.Volumes {
		vols = append(vols, resultPayload{
			"letter": v.Letter, "type": v.Type, "totalBytes": v.TotalBytes, "freeBytes": v.FreeBytes,
		})
	}
	return resultPayload{
		"machine": resultPayload{
			"hostname": rep.Machine.Hostname, "manufacturer": rep.Machine.Manufacturer, "model": rep.Machine.Model,
		},
		"os": resultPayload{
			"productName": rep.OS.ProductName, "version": rep.OS.Version,
			"build": rep.OS.Build, "uptime": rep.OS.Uptime, "is64Bit": rep.OS.Is64Bit,
		},
		"cpu": resultPayload{
			"name": rep.CPU.Name, "vendor": rep.CPU.Vendor,
			"speedMHz": rep.CPU.SpeedMHz, "cores": rep.CPU.Cores, "logical": rep.CPU.Logical,
		},
		"memory": resultPayload{
			"totalBytes": rep.Memory.TotalBytes, "availableBytes": rep.Memory.AvailableBytes,
			"loadPercent": rep.Memory.LoadPercent,
		},
		"gpus":     gpus,
		"displays": displays,
		"volumes":  vols,
		"network":  nics,
		"errors":   rep.Errors,
	}
}
