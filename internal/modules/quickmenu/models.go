package quickmenu

// MenuItem 轮盘的一个扇区条目：复用托盘 TrayMenu 配置（同一份条目、同一套分发），
// Index 为当前层展示序下标，前端点击经 Launch(path) 回传完整路径。
// Type 为 group 且 Children 非空时该扇区可展开二级盘（二级轮盘关闭时后端已把
// 子条目拍平到主盘，前端看到的数据形态天然只随开关变化，无需感知配置）。
type MenuItem struct {
	Index    int        `json:"index"`
	Label    string     `json:"label"`    // 已解析显示名（自定义名缺省回退）
	Type     string     `json:"type"`     // exe | command | route | group
	Hint     string     `json:"hint"`     // 辅助说明：exe 路径 / 命令与页面的引用键；group 留空
	Icon     string     `json:"icon"`     // 前端图标名（AppIcon 注册表，按类型解析，未知名前端回退）
	Children []MenuItem `json:"children"` // group 的二级条目（非组为空切片，绑定侧恒在）
}

// Status 快捷菜单运行态（模块页展示 + 开关回显）。
type Status struct {
	TrapActive bool `json:"trapActive"` // 全局鼠标钩子是否在位
	HoldMs     int  `json:"holdMs"`     // 触发所需按住时长（ms）
	MoveTol    int  `json:"moveTol"`    // 抬手前允许的光标位移（物理像素）
	ItemCount  int  `json:"itemCount"`  // 当前主盘扇区数（含分组节点；二级轮盘关闭时为拍平后的叶子数）
	TwoTier    bool `json:"twoTier"`    // 二级轮盘是否启用（分组展开为子盘，否则拍平）
}
