package msgboard

import (
	"errors"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// errNoApplication 应用实例未就绪（构造期/装配前调用热键改判）。
var errNoApplication = errors.New("应用实例不可用，无法注册全局热键")

// 全局热键：最薄私有封装，直调 Wails application.GlobalShortcut（beta.10
// Windows 侧即 RegisterHotKey 通路，与 quickmenu 的 WH_MOUSE_LL 钩子零交集）。
//
// 并行纪律：feat/f2-clipboard 正在建全仓集中热键注册管理器，本文件刻意**不**
// 抽公共 manager——只服务"单一 toggle 键"的最小需求；改键采用
// "注销旧键→注册新键→失败回注册旧键"的原子回滚。合并时由协调者把这里收编
// 到集中注册器之下（热键代码落点：本文件 + service.go 的 applyHotkey/stop）。

// applyHotkey 把热键绑定从 oldKey 迁移到 newKey（newKey 为 ""=停用热键）。
// 注意：Register/Unregister 内部是主线程 InvokeSync 包装，绝不持有 s.mu 调用。
// 失败时尽力恢复旧键并返回错误，调用方负责回滚配置与用户提示。
func (s *MsgBoardService) applyHotkey(oldKey, newKey string) error {
	if oldKey == newKey {
		return nil
	}
	a := application.Get()
	if a == nil || a.GlobalShortcut == nil {
		return errNoApplication
	}
	if oldKey != "" {
		if err := a.GlobalShortcut.Unregister(oldKey); err != nil {
			slog.Warn("msgboard: 注销旧热键失败（继续尝试新键）", "hotkey", oldKey, "err", err)
		}
	}
	if newKey == "" {
		s.setBound("")
		return nil
	}
	if err := a.GlobalShortcut.Register(newKey, s.onHotkey); err != nil {
		// 回滚：恢复旧键，恢复结果如实入账（恢复失败则热键通道整体不可用）。
		if oldKey != "" {
			if rerr := a.GlobalShortcut.Register(oldKey, s.onHotkey); rerr != nil {
				slog.Warn("msgboard: 旧热键回注册也失败，热键通道停用", "hotkey", oldKey, "err", rerr)
				s.setBound("")
			} else {
				s.setBound(oldKey)
			}
		} else {
			s.setBound("")
		}
		return err
	}
	s.setBound(newKey)
	return nil
}

// onHotkey 热键回调（manager 保证在独立 goroutine 派发，不占用任何消息泵线程）。
func (s *MsgBoardService) onHotkey() {
	if err := s.Toggle(); err != nil {
		slog.Warn("msgboard: 热键切换留言牌失败", "err", err)
	}
}

func (s *MsgBoardService) setBound(key string) {
	s.mu.Lock()
	s.hotkeyBound = key
	s.mu.Unlock()
}
