package quickmenu

// MenuItem 弹窗菜单的一个条目：复用托盘 TrayMenu 配置（同一份条目、同一套分发），
// Index 为展示序下标，前端点击经 Launch(index) 回传。
type MenuItem struct {
	Index int    `json:"index"`
	Label string `json:"label"` // 已解析显示名（自定义名缺省回退）
	Type  string `json:"type"`  // exe | command | route
	Hint  string `json:"hint"`  // 辅助说明：exe 路径 / 命令与页面的引用键
}

// Status 快捷菜单运行态（模块页只读展示）。
type Status struct {
	TrapActive bool `json:"trapActive"` // 全局鼠标钩子是否在位
	HoldMs     int  `json:"holdMs"`     // 触发所需按住时长（ms）
	MoveTol    int  `json:"moveTol"`    // 抬手前允许的光标位移（物理像素）
	ItemCount  int  `json:"itemCount"`  // 当前可用条目数（来自托盘配置中启用的条目）
}
