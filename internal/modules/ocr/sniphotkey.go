package ocr

import (
	"errors"
	"fmt"
)

// ---------- 剪贴板识图全局热键（F2-③）：服务接缝与前端 API ----------
//
// 热键的系统绑定归装配根（internal/hotkey 通用注册器包装 Wails GlobalShortcut），
// 本模块只持有"开关 + 键位"配置与业务回调：SetSnipHotkeyBinding 由 app.go 注入
// 一个把槽位 ocr/snip-clipboard 落实到底层注册器的适配器；未注入（独立跑测/模块
// 未接线）时相关方法退化为纯配置读写，不报错。
//
// 派发链：热键触发 → handler（Wails 已在独立 goroutine 上派发）→
// registry.RunTrayCommand("ocr/snip-clipboard") → RecognizeClipboardImage。

// SnipHotkeyBinding 宿主注入的热键落实通道。Apply 把"键位 + 开关"期望态落到系统
// 全局热键（失败返回中文错误，配置端须自行回滚）；Registered 如实回报槽位是否
// 真实绑定在系统上（启动期抢键失败 → false，设置页据此给"已被占用"提示）。
type SnipHotkeyBinding interface {
	Apply(accel string, enabled bool) error
	Registered() bool
}

// SnipHotkeyState 热键配置的对外视图（设置页一次拉全）。
type SnipHotkeyState struct {
	Enabled    bool   `json:"enabled"`
	Accel      string `json:"accel"`      // 规范化键位（如 Ctrl+Alt+T）
	Registered bool   `json:"registered"` // 系统侧真实绑定状态（禁用中恒 false）
}

// SetSnipHotkeyBinding 注入热键落实通道（app/hotkeys.go 装配根接线）。
// 装配布线：Go 直调路径，不得依赖运行态（见 ADR-0001 Wave 3 注记）
func (s *OcrService) SetSnipHotkeyBinding(b SnipHotkeyBinding) {
	s.hotkeyMu.Lock()
	defer s.hotkeyMu.Unlock()
	s.hotkeyBinding = b
}

func (s *OcrService) snipHotkeyBinding() SnipHotkeyBinding {
	s.hotkeyMu.Lock()
	defer s.hotkeyMu.Unlock()
	return s.hotkeyBinding
}

// GetSnipHotkey 拉取热键开关、键位与系统注册实况（前端设置页回显用）。
// Wave 3 口径：单值绑定签名扩为 (SnipHotkeyState, error)，门拒绝如实上抛，
// 禁止回零值态造成"热键未启用"的假象。
func (s *OcrService) GetSnipHotkey() (SnipHotkeyState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return SnipHotkeyState{}, gateErr
	}
	defer release()
	enabled, accel := s.store.GetSnipHotkey()
	b := s.snipHotkeyBinding()
	return SnipHotkeyState{Enabled: enabled, Accel: accel, Registered: enabled && b != nil && b.Registered()}, nil
}

// SetSnipHotkeyEnabled 以“系统态 + 持久化态”补偿事务切换开关：先落系统，保存
// 失败则把系统恢复到旧开关；补偿也失败时 errors.Join 同时保留两段诊断。
func (s *OcrService) SetSnipHotkeyEnabled(v bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.hotkeyMu.Lock()
	defer s.hotkeyMu.Unlock()

	prevEnabled, accel := s.store.GetSnipHotkey()
	b := s.hotkeyBinding
	if b != nil {
		if err := b.Apply(accel, v); err != nil {
			return err
		}
	}
	if err := s.store.SetSnipHotkeyEnabled(v); err != nil {
		if b == nil {
			return err
		}
		if rollbackErr := b.Apply(accel, prevEnabled); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("恢复全局热键旧开关失败：%w", rollbackErr))
		}
		return err
	}
	return nil
}

// SetSnipHotkey 改键采用先系统换绑、后持久化的新值事务。保存失败时先把系统
// 换回旧键；store setter 自身保证保存失败不污染内存态，因此成功/失败后三态一致。
func (s *OcrService) SetSnipHotkey(raw string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	norm, err := NormalizeSnipHotkey(raw)
	if err != nil {
		return err
	}

	s.hotkeyMu.Lock()
	defer s.hotkeyMu.Unlock()

	enabled, prev := s.store.GetSnipHotkey()
	b := s.hotkeyBinding
	if b != nil {
		if err := b.Apply(norm, enabled); err != nil {
			return err
		}
	}
	if _, err := s.store.SetSnipHotkey(norm); err != nil {
		if b == nil {
			return err
		}
		if rollbackErr := b.Apply(prev, enabled); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("恢复全局热键旧键 %s 失败：%w", prev, rollbackErr))
		}
		return err
	}
	return nil
}
