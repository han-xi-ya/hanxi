// Package piik 内置模块装配面：piik（上游 TNTcraftHIM/Piik，headless 屏幕分享
// 服务器）托管的 extapi.Module 契约载体。服务型骨架的全部负裁决账——无窗口、
// 无托盘、无提权、数据改道恒注入、空闲不退服——记于 service.go 头部包注释，
// 本文件只落装配面的唯一一条负裁决：
//
// **刻意不实现 extapi.UpdateChecker（仓内首例：不是契约欠账，是主动不挂雷达）**。
// 上游是日更级风暴发布节奏（22 版/14 天实登账，见 metaHints 第 1 条），挂上雷达
// 等于模块中心"可用更新"红点每天重亮——轰炸；且与本模块"钉版本手动追"的设计
// 自相矛盾（远程表只如实列版，追不追由机主在版本管理页逐条手点，不存在自动
// 跟版通道）。踩坑 #90 教训的镜像面：softver 当年雷达缺席是"忘了实现"——
// 不实现=静默缺席、无编译期提醒；piik 的缺席是裁决，本段注释即对静默缺席的
// 显式记账——后人勿把它当遗漏顺手补上 CheckUpdate；version 包比较链
// （check_update.go）保留，雷达接线拒绝。
package piik

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "piik"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *PiikService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewPiikService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Piik 屏幕分享",
		Version:     "0.1.0",
		Description: "托管 headless 屏幕分享服务器 Piik：版本管理、JobObject 启停与端口试绑分配，界面即系统浏览器打开的本机 HTTP 页面（服务型骨架：无窗口无托盘无提权，钉版本手动追更不挂更新红点）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
// Icon i:cast——图标池无 i:share（仅 share-2，已被 fileshare 占用），cast 系
// 在册且全仓 Nav/展示面零占用的投屏语义矢量（装配线一轮裁定）；Order 27 为
// network 组 portscan(25)~lan(30) 邻域空位（全仓 Order 实扫登账）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "piik-manager", Title: "Piik 屏幕分享", Route: "/ext/piik", Icon: "i:cast", Section: extapi.SectionExt, Order: 27, Group: extapi.GroupNetwork},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动后台外部实例感知轮询（懒加载，与托管族其余模块同策略）。
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (e *Module) OnDestroy() error {
	// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)
	e.svc.shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
