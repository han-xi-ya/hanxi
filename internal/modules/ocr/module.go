// Package ocr 内置模块：hanxi-ocr 本地识别服务（微信 4.0 OCR 引擎封装）的
// 消费方集成——状态探测、JobObject 一键启停托管与图片识别转发。
//
// 定位边界（决策记录）：hanxi-ocr.exe 及其引擎资产含腾讯专有组件，属用户自备的
// 私有分发件，**永不进 Hanxi 公开包**；本模块只做探活、转发与生命周期托管，
// 不做版本管理/下载（区别于 integrate-github-tool 托管模式）。
// 服务默认自动发现 Hanxi 同级目录 ../hanxi-ocr/hanxi-ocr.exe，也可在设置中指定。
package ocr

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "ocr"

type Module struct {
	svc *OcrService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewOcrService(plat)}
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "文字识别",
		Version:     "0.1.0",
		Description: "本地 OCR 服务托管与图片文字识别：探测/一键启停 hanxi-ocr，拖图即识别（离线免费，需自备服务组件）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "ocr-main", Title: "文字识别", Route: "/ext/ocr", Icon: "i:scan-text",
			Section: extapi.SectionExt, Order: 88, Group: extapi.GroupEfficiency},
	}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

// Service 暴露服务实例供 app.go 接线主窗原生文件拖放（组件导入通道）。
func (m *Module) Service() *OcrService { return m.svc }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向轮盘/托盘命令候选
// 目录暴露「框选截屏识别」（Key=ocr/snip，宿主独立 goroutine 调用，内部防重入）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{{
		ID:    "snip",
		Label: "框选截屏识别（OCR）",
		Run: func(ctx context.Context) error {
			_, err := m.svc.SnipAndRecognize()
			return err
		},
	}}
}

// Permissions 声明回环 HTTP 探测与转发（网络出站最小口径）。
func (m *Module) Permissions() []extapi.Permission { return []extapi.Permission{extapi.PermNetwork} }

func (m *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动状态感知轮询（懒加载，与 ddnsgo 同策略）。
func (m *Module) OnInit(ctx context.Context) error {
	m.svc.activate()
	return nil
}

func (m *Module) OnDestroy() error {
	m.svc.Shutdown()
	return nil
}

func (m *Module) IsInitialized() bool { return true }
