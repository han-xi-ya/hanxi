package ocr

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
func (s *OcrService) GetSnipHotkey() SnipHotkeyState {
	enabled, accel := s.store.GetSnipHotkey()
	b := s.snipHotkeyBinding()
	return SnipHotkeyState{Enabled: enabled, Accel: accel, Registered: enabled && b != nil && b.Registered()}
}

// SetSnipHotkeyEnabled 开关热键：先落系统（开=注册/关=注销），成功才持久化——
// 注册失败（键位被占用）直接报错返回，配置保持原样，UI 不留"显示开了实际没绑"。
func (s *OcrService) SetSnipHotkeyEnabled(v bool) error {
	_, accel := s.store.GetSnipHotkey()
	if b := s.snipHotkeyBinding(); b != nil {
		if err := b.Apply(accel, v); err != nil {
			return err
		}
	}
	return s.store.SetSnipHotkeyEnabled(v)
}

// SetSnipHotkey 改键：规范化校验 → 落系统重绑 → 失败回滚持久化。
// 键位非法（纯键位不开/未知修饰键）在第一步即中文拒绝，不触系统。
func (s *OcrService) SetSnipHotkey(raw string) error {
	enabled, prev := s.store.GetSnipHotkey()
	norm, err := s.store.SetSnipHotkey(raw)
	if err != nil {
		return err
	}
	if b := s.snipHotkeyBinding(); b != nil {
		if err := b.Apply(norm, enabled); err != nil {
			_, _ = s.store.SetSnipHotkey(prev) // 回滚配置，保持"配置=系统态"不变式
			return err
		}
	}
	return nil
}
