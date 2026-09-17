package msgboard

// Config 留言板偏好（模块页读写的完整快照）：Text 为挂牌正文（留空回落第一条预设）。
type Config struct {
	Text     string `json:"text"`
	FontSize int    `json:"fontSize"` // 正文字号（DIP px，钳位区间见 store）
	Screen   string `json:"screen"`   // 目标显示器设备名（如 \\.\DISPLAY2）；空=主屏
	Hotkey   string `json:"hotkey"`   // 全局热键加速器串（如 Ctrl+Alt+B）；空=停用热键
}

// Status 留言板运行态（模块页状态区）。HotkeyActive 如实反映热键是否注册在位
// （开机期抢键失败时只降级不报错，页面据此提示去托盘/轮盘唤起或改键）。
type Status struct {
	Shown        bool   `json:"shown"`
	Hotkey       string `json:"hotkey"`
	HotkeyActive bool   `json:"hotkeyActive"`
	KeepAwake    bool   `json:"keepAwake"` // 挂牌期间防休眠诉求是否已成功挂账
}

// BoardContent 挂牌弹窗视图模型（/#msgboard 挂载与热更新时拉取）。
type BoardContent struct {
	Text     string `json:"text"`
	FontSize int    `json:"fontSize"`
}

// ScreenInfo 显示器候选（模块页下拉；设备名即 store 的 Screen 值）。
type ScreenInfo struct {
	Device    string `json:"device"`    // 设备名 \\.\DISPLAYn
	Width     int    `json:"width"`     // 物理宽（px）
	Height    int    `json:"height"`    // 物理高（px）
	IsPrimary bool   `json:"isPrimary"` // 是否主屏
}
