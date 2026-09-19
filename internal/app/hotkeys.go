package app

// ---------- 全局热键装配（F2-③，留言板已于 R1 收编） ----------
//
// 热键是跨模块的全局输入能力：底层走 Wails GlobalShortcut（RegisterHotKey，
// 免手写键盘钩子），全仓唯一一张 hotkey.Registry 注册表由装配根构造后按需交接
// ——注册表语义（槽位记账、改键原子回滚、冲突中文报错、实况查询）收口在
// internal/hotkey 通用封装，各功能零触碰底层。
//
// 接线形态两种：ocr 剪贴板识图走命令派发链（本文件包装配启动绑定）；留言板
// 只注入注册表，绑定/解绑随模块 OnInit/OnDestroy 驱动（见 msgboard/hotkey.go）。
//
// 槽位命名与托盘命令键对齐（"ocr/snip-clipboard"、"msgboard/toggle"），
// 排查时两头一个名字。

import (
	"context"
	"log/slog"
	"sync"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	"hanxi/internal/modules/ocr"
	"hanxi/internal/notify"
)

// snipHotkeyName OCR 剪贴板识图热键的注册槽位名。
const snipHotkeyName = "ocr/snip-clipboard"

// ocrModuleID snip 热键所属模块 ID（与槽位名前缀一致）。
const ocrModuleID = "ocr"

// setupSnipHotkey 接线 OCR 剪贴板识图全局热键：
//   - 启动即按"模块启停 × 用户开关"双因子落定绑定（application.Run 前注册由
//     Wails 排入 pending 待启动绑定；启动期抢键失败只记日志，设置页实况可见、
//     可改键重试）；
//   - 注入服务绑定通道：设置页改键/开关即时落实系统并如实回滚配置；
//   - 回调复用命令派发链（模块门禁 + 懒初始化 + 防重入，与轮盘/托盘同路）；
//   - 挂 Registry 启停钩子：ocr 模块被停用时注销 OS 级热键（不留按键黑洞），
//     重新启用时仅当用户热键开关仍为开才重绑（见 snipHotkeyGate）。
func setupSnipHotkey(hk *hotkey.Registry, reg *extapi.Registry, svc *ocr.OcrService) {
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

	gate := newSnipHotkeyGate(hk, func() (accel string, userEnabled bool) {
		st, err := svc.GetSnipHotkey()
		if err != nil {
			// 调用门拒绝（模块未启用/停用中）：按"期望未绑定"处理，apply 会落解绑。
			slog.Warn("hotkey: snip 热键实况读取被拒（按未绑定处理）", "err", err)
			return "", false
		}
		return st.Accel, st.Enabled
	}, handler)
	gate.apply(reg.IsEnabled(ocrModuleID))
	reg.OnLifecycle(gate.onLifecycle)
}

// snipHotkeyGate 把 snip 热键的 Bind/Unbind 收拢成可重入的小状态机：
// 期望绑定态 = 模块启用 ∧ 用户开关开启，任一因子变化都经 apply 重算并落到
// 通用注册表。幂等语义由 hotkey.Registry 兜底（重复解绑静默成功、同键重绑
// no-op），重复启停不报错也不重复占用槽位；mu 只串行化本侧决策与日志，
// 不与设置页的 Apply 通道争抢（后者经同一 Registry 的内部锁天然串行）。
type snipHotkeyGate struct {
	mu      sync.Mutex
	hk      *hotkey.Registry
	config  func() (accel string, userEnabled bool)
	handler func()
}

func newSnipHotkeyGate(hk *hotkey.Registry, config func() (accel string, userEnabled bool), handler func()) *snipHotkeyGate {
	return &snipHotkeyGate{hk: hk, config: config, handler: handler}
}

// apply 按"模块启用 × 用户开关"重算期望态并落系统；失败只记日志
// （与启动期抢键失败同策：实况回显可查，不阻断启停流程）。
func (g *snipHotkeyGate) apply(moduleEnabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	accel, userEnabled := g.config()
	if err := g.hk.Bind(snipHotkeyName, accel, moduleEnabled && userEnabled, g.handler); err != nil {
		slog.Warn("剪贴板识图热键绑定调整未成功（不影响其他功能，可到文字识别页改键）", "err", err)
	}
}

// onLifecycle 挂接 Registry.OnLifecycle：只关心 ocr 模块的启停事件，
// 停用即注销热键释放 OS 键位，启用则交回 apply 按用户开关裁决。
func (g *snipHotkeyGate) onLifecycle(moduleID string, enabled bool) {
	if moduleID != ocrModuleID {
		return
	}
	g.apply(enabled)
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
