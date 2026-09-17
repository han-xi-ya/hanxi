package msgboard

import (
	"errors"
	"log/slog"

	"hanxi/internal/hotkey"
)

// 全局热键（R1 收编）：原先本文件是最薄私有封装——直调 Wails application.
// GlobalShortcut 并手写"注销旧键→注册新键→失败回注册旧键"的原子回滚；
// 现已并入 internal/hotkey 通用注册器的槽位语义（先注册新键、成功才注销旧
// 键，冲突保持旧绑定并中文报错，实况以系统为准），系统绑定通路不变（同一个
// Wails GlobalShortcutManager / RegisterHotKey），此处只保留服务侧接线入口。

// hotkeySlot 挂/撤牌热键在通用注册器中的槽位名——与托盘命令键 msgboard/toggle
// 对齐（装配根约定：槽位名与命令键两头一个名字，见 internal/app/hotkeys.go）。
const hotkeySlot = "msgboard/toggle"

// errNoHotkeyRegistry 热键注册器未接线（应用实例未就绪或装配前调用）。
var errNoHotkeyRegistry = errors.New("热键注册器未就绪，无法注册全局热键")

// setHotkeyRegistry 注入全仓通用热键注册器（装配根经 Module.SetHotkeyRegistry
// 交接，见 app.go）。注册器底层是 Wails GlobalShortcut 管理器，Register/
// Unregister 内部为主线程 InvokeSync 包装——热键操作绝不持有 s.mu 调用，防锁反转。
func (s *MsgBoardService) setHotkeyRegistry(r *hotkey.Registry) {
	s.mu.Lock()
	s.hk = r
	s.mu.Unlock()
}

func (s *MsgBoardService) hotkeyRegistry() *hotkey.Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hk
}

// applyHotkey 把槽位绑定迁移到期望键位（newKey 为 ""=停用热键）。
// 语义全部落在注册器上：同键已在位幂等 no-op；换键先注册新键、成功后才注销
// 旧键，新键被占用则旧绑定原样保留并返回中文错误（调用方负责回滚配置与提示，
// 注册器内部无需再回注册旧键）；停用为幂等解绑，OS 注销失败只记日志不挡配置
// 落盘（与旧私有封装行为一致）。
func (s *MsgBoardService) applyHotkey(newKey string) error {
	r := s.hotkeyRegistry()
	if r == nil {
		return errNoHotkeyRegistry
	}
	if newKey == "" {
		if err := r.Unbind(hotkeySlot); err != nil {
			slog.Warn("msgboard: 注销旧热键失败（继续停用热键）", "err", err)
		}
		return nil
	}
	return r.Bind(hotkeySlot, newKey, true, s.onHotkey)
}

// onHotkey 热键回调（manager 保证在独立 goroutine 上派发，不占用任何消息泵线程）。
func (s *MsgBoardService) onHotkey() {
	if err := s.Toggle(); err != nil {
		slog.Warn("msgboard: 热键切换留言牌失败", "err", err)
	}
}
