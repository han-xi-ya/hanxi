package memo

import (
	"errors"
	"fmt"
	"log/slog"

	"hanxi/internal/hotkey"
)

// 全局速记热键（N16 B 批）：接线形态完全复用 msgboard 先例——本模块只持有
// "键位"配置与回调，系统绑定收口全仓唯一 hotkey.Registry（先注册新键、成功
// 才注销旧键；冲突保持旧绑定并中文报错；实况以系统为准不信任自家记账）。
// 槽位名与装配根"槽位名/命令键两头一个名字"约定对齐（internal/app/hotkeys.go）。

// quickHotkeySlot 悬浮速记卡热键在通用注册器中的槽位名。
const quickHotkeySlot = "memo/quicksheet"

// errNoHotkeyRegistry 热键注册器未接线（应用实例未就绪或装配前调用）。
var errNoHotkeyRegistry = errors.New("热键注册器未就绪，无法注册全局速记热键")

// setHotkeyRegistry 注入全仓通用热键注册器（装配根经 Module.SetHotkeyRegistry
// 交接，见 app.go；注入时序先于 OnInit，与热键回调的并发由 Registry 内部锁兜底，
// 本侧读取走 sheetMu）。
func (s *MemoService) setHotkeyRegistry(r *hotkey.Registry) {
	s.sheetMu.Lock()
	s.hk = r
	s.sheetMu.Unlock()
}

func (s *MemoService) hotkeyRegistry() *hotkey.Registry {
	s.sheetMu.Lock()
	defer s.sheetMu.Unlock()
	return s.hk
}

// applyQuickHotkey 把槽位绑定迁移到期望键位（accel 为 ""=停用热键，幂等解绑）。
// 注册器语义兜底：同键已在位幂等 no-op；换键冲突时旧键原样活着并中文报错上抛。
func (s *MemoService) applyQuickHotkey(accel string) error {
	r := s.hotkeyRegistry()
	if r == nil {
		return errNoHotkeyRegistry
	}
	if accel == "" {
		if err := r.Unbind(quickHotkeySlot); err != nil {
			slog.Warn("memo: 注销速记热键失败（继续停用热键）", "err", err)
		}
		return nil
	}
	return r.Bind(quickHotkeySlot, accel, true, s.onQuickHotkey)
}

// onQuickHotkey 热键回调（manager 保证在独立 goroutine 上派发，不占消息泵）。
// 走无门内部版 toggleQuickSheet——热键绑定/注销随 start/stop 生命周期收口，
// 回调线程不是 RPC 面，不得依赖调用门运行态（msgboard onHotkey 同款纪律）。
func (s *MemoService) onQuickHotkey() {
	if err := s.toggleQuickSheet(); err != nil {
		slog.Warn("memo: 热键唤出悬浮速记卡失败", "err", err)
	}
}

// ---------- 生命周期（module.go 的 OnInit/OnDestroy 驱动） ----------

// start 登记速记卡生命周期在场并按配置绑定全局热键。热键注册失败不致命：
// 只降级为模块页按钮唤出，页面状态区如实显示"热键未在位"（口径同 msgboard）。
func (s *MemoService) start() error {
	s.sheetMu.Lock()
	s.sheetStarted = true
	s.sheetMu.Unlock()

	if accel := s.prefs.Hotkey(); accel != "" {
		if err := s.applyQuickHotkey(accel); err != nil {
			slog.Warn("memo: 速记热键注册失败（已降级为页面按钮唤出）", "hotkey", accel, "err", err)
		}
	}
	return nil
}

// stop 摘热键并释放速记卡窗口：先解绑（不再进新唤出），后销毁（在架窗口与
// 空闲待销毁计时一并收口），不留按键黑洞也不留白烧的 WebView2 视图。
func (s *MemoService) stop() error {
	s.sheetMu.Lock()
	s.sheetStarted = false
	r := s.hk
	s.sheetMu.Unlock()

	if r != nil {
		if err := r.Unbind(quickHotkeySlot); err != nil {
			slog.Warn("memo: 停用速记热键失败", "err", err)
		}
	}
	s.destroySheet(false)
	return nil
}

// ---------- 配置读写（RPC 导出版见 quicksheet.go 尾部） ----------

// setQuickHotkey 改键事务（ocr SetSnipHotkey 同款"先系统后持久化"补偿序）：
//  1. 形态校验（store 内），无效直接报错，系统与配置都不动；
//  2. 落系统（注册器先注册新键成功才注销旧键；停用为幂等解绑），失败上抛——
//     配置保持旧值，旧绑定原样活着；
//  3. 落盘失败反向恢复系统旧键位；补偿也失败 errors.Join 双诊断如实上浮。
//
// 注册器未接线（独立跑测/装配前）时退化为纯配置读写，不报错。
func (s *MemoService) setQuickHotkey(raw string) error {
	prev := s.prefs.Hotkey()
	norm, err := validateHotkey(raw)
	if err != nil {
		return err
	}
	if norm == prev {
		return nil // 幂等：同键重复保存不碰系统也不写盘
	}
	if err := s.applyQuickHotkey(norm); err != nil {
		if errors.Is(err, errNoHotkeyRegistry) {
			slog.Warn("memo: 热键注册器未接线，速记热键仅落配置", "hotkey", norm)
		} else {
			return fmt.Errorf("热键 %q 设置失败（留空表示停用热键）：%v", norm, err)
		}
	}
	if _, serr := s.prefs.SetHotkey(norm); serr != nil {
		if rberr := s.applyQuickHotkey(prev); rberr != nil {
			return errors.Join(serr, fmt.Errorf("恢复速记热键旧键 %s 失败：%v", prev, rberr))
		}
		return serr
	}
	return nil
}
