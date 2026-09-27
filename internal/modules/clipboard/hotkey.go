package clipboard

import (
	"errors"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/hotkey"
)

// 全局浮层热键与生命周期收口（契约 §6；接线形态完全复用 memo 速记卡/msgboard
// 先例）：本模块只持有键位与回调，系统绑定收口全仓唯一 hotkey.Registry
// （先注册新键、成功才注销旧键；冲突保持旧绑定并中文报错；实况以系统为准）。
// 槽位名与装配根"槽位名/命令键两头一个名字"约定对齐（收口阶段进
// internal/app/hotkeys.go，本线不碰）。

const (
	// overlayHotkeySlot 浮层热键在通用注册器中的槽位名（契约 §6 冻结）。
	overlayHotkeySlot = "clipboard/overlay"
	// defaultOverlayHotkey 默认键位 Ctrl+Alt+V（契约 §6 冻结；键位配置面
	// 首版不做——无 prefs 文件所有权，改键需求登记收口清单）。
	defaultOverlayHotkey = "Ctrl+Alt+V"
)

// errNoHotkeyRegistry 热键注册器未接线（应用实例未就绪或装配前调用）。
var errNoHotkeyRegistry = errors.New("热键注册器未就绪，无法注册剪贴板全局热键")

// setHotkeyRegistry 注入全仓通用热键注册器（装配根交接，注入时序先于 OnInit；
// 与热键回调的并发由 Registry 内部锁兜底，本侧读取走 ovMu）。
// 对位 memo：模块面导出 Module.SetHotkeyRegistry（module.go），svc 侧保持
// 不导出，冻结绑定面零增员。
func (s *ClipboardService) setHotkeyRegistry(r *hotkey.Registry) {
	s.ovMu.Lock()
	s.hk = r
	s.ovMu.Unlock()
}

func (s *ClipboardService) hotkeyRegistry() *hotkey.Registry {
	s.ovMu.Lock()
	defer s.ovMu.Unlock()
	return s.hk
}

// applyOverlayHotkey 把槽位绑定落到期望键位（accel ""=停用热键，幂等解绑）。
func (s *ClipboardService) applyOverlayHotkey(accel string) error {
	r := s.hotkeyRegistry()
	if r == nil {
		return errNoHotkeyRegistry
	}
	if accel == "" {
		if err := r.Unbind(overlayHotkeySlot); err != nil {
			slog.Warn("clipboard: 注销浮层热键失败（继续停用热键）", "err", err)
		}
		return nil
	}
	return r.Bind(overlayHotkeySlot, accel, true, s.onOverlayHotkey)
}

// onOverlayHotkey 热键回调（manager 保证在独立 goroutine 上派发，不占消息泵）。
// 走无门内部版 toggleOverlay——回调线程不是 RPC 面，不得依赖调用门运行态。
func (s *ClipboardService) onOverlayHotkey() {
	if err := s.toggleOverlay(); err != nil {
		slog.Warn("clipboard: 热键唤出剪贴板浮层失败", "err", err)
	}
}

// ---------- 生命周期（module.go 的 OnInit/OnDestroy 驱动） ----------

// start 登记生命周期在场：起剪贴板监听 + 绑定全局浮层热键。两脉皆降级不致命：
//   - 应用未运行（无头单测/装配前）监听不启动——message-only 窗口与消息泵
//     依赖交互式窗口站，且测试期真监听会污染开发机剪贴板，一律跳过只记 debug；
//   - 热键注册失败（抢键冲突/注册器未接线）只降级为无热键（浮层待托盘/按钮
//     通路收口后仍可用），warn 留痕不阻塞启用（口径同 memo/msgboard）。
func (s *ClipboardService) start() error {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()

	if application.Get() != nil {
		l, err := startClipboardListener(s.handleCapture)
		if err != nil {
			slog.Warn("clipboard: 剪贴板监听启动失败，历史采集已暂停（其余功能不受影响）", "err", err)
		} else {
			s.mu.Lock()
			// stop 可能恰在建窗期间完成：刚起即废的监听当场收口，不留孤儿泵线程
			if s.started {
				s.listener = l
			}
			s.mu.Unlock()
			if !s.isStarted() {
				l.stop()
			}
		}
	} else {
		slog.Debug("clipboard: 应用未运行（无头态），跳过剪贴板监听启动")
	}

	if err := s.applyOverlayHotkey(defaultOverlayHotkey); err != nil {
		if errors.Is(err, errNoHotkeyRegistry) {
			slog.Debug("clipboard: 热键注册器未接线，浮层热键本次不绑定")
		} else {
			slog.Warn("clipboard: 浮层热键注册失败（已降级为无热键）", "hotkey", defaultOverlayHotkey, "err", err)
		}
	}
	return nil
}

// stop 收口全部在途副作用：先灭 started（监听回调与浮层唤出即刻静默早退），
// 再摘监听（等消息泵线程退出）、摘热键（不再进新唤出）、真销毁浮层
// （在架窗口与空闲待销毁计时一并收口）——不留孤儿线程、按键黑洞与白烧的
// WebView2 视图。
func (s *ClipboardService) stop() error {
	s.mu.Lock()
	s.started = false
	l := s.listener
	s.listener = nil
	s.mu.Unlock()

	if l != nil {
		l.stop()
	}
	r := s.hotkeyRegistry()
	if r != nil {
		if err := r.Unbind(overlayHotkeySlot); err != nil {
			slog.Warn("clipboard: 停用浮层热键失败", "err", err)
		}
	}
	s.destroyOverlay(false)
	return nil
}
