package app

// ---------- 全局热键装配（F2-③） ----------
//
// 热键是跨模块的全局输入能力：底层走 Wails GlobalShortcut（RegisterHotKey，
// 免手写键盘钩子），注册表语义收口在 internal/hotkey 通用封装；派发只认
// TrayCommand 键（registry.RunTrayCommand），热键层不持有任何模块业务——
// 取色、留言板等后续功能各加一条"键位配置 → 命令键"即可复用，零触碰底层。
//
// 槽位命名与托盘命令键对齐（"ocr/snip-clipboard"），排查时两头一个名字。

import (
	"context"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	"hanxi/internal/modules/ocr"
	"hanxi/internal/notify"
)

// snipHotkeyName OCR 剪贴板识图热键的注册槽位名。
const snipHotkeyName = "ocr/snip-clipboard"

// setupSnipHotkey 接线 OCR 剪贴板识图全局热键：
//   - 启动即按持久化配置绑定（application.Run 前注册由 Wails 排入 pending 待
//     启动绑定；启动期抢键失败只记日志，设置页实况可见、可改键重试）；
//   - 注入服务绑定通道：设置页改键/开关即时落实系统并如实回滚配置；
//   - 回调复用命令派发链（模块门禁 + 懒初始化 + 防重入，与轮盘/托盘同路）。
func setupSnipHotkey(a *application.App, reg *extapi.Registry, svc *ocr.OcrService) {
	hk := hotkey.NewRegistry(a.GlobalShortcut)
	handler := func() {
		// Wails 已在独立 goroutine 上派发热键回调，耗时识别可安全直跑不卡消息泵；
		// 业务失败（无图/服务未就绪/正在进行中）进通知 Hub——窗口隐藏时自动落原生 Toast。
		if err := reg.RunTrayCommand(context.Background(), snipHotkeyName); err != nil {
			slog.Warn("hotkey: ocr/snip-clipboard 执行失败", "err", err)
			notify.GetHub().Emit(&notify.Notification{
				ModuleID: "ocr",
				Title:    "剪贴板识图失败",
				Message:  err.Error(),
				Level:    notify.LevelError,
				Route:    "/ext/ocr",
			})
		}
	}
	svc.SetSnipHotkeyBinding(snipHotkeyBinding{hk: hk, handler: handler})

	st := svc.GetSnipHotkey()
	if err := hk.Bind(snipHotkeyName, st.Accel, st.Enabled, handler); err != nil {
		slog.Warn("剪贴板识图热键启动绑定未成功（不影响其他功能，可到文字识别页改键）", "err", err)
	}
}

// snipHotkeyBinding ocr.SnipHotkeyBinding 适配器：把 ocr 模块的期望态
// （键位+开关）落到通用热键注册表的具体槽位上。
type snipHotkeyBinding struct {
	hk      *hotkey.Registry
	handler func()
}

func (b snipHotkeyBinding) Apply(accel string, enabled bool) error {
	return b.hk.Bind(snipHotkeyName, accel, enabled, b.handler)
}

func (b snipHotkeyBinding) Registered() bool {
	return b.hk.Registered(snipHotkeyName)
}
