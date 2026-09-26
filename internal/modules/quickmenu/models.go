package quickmenu

// MenuItem 轮盘的一个扇区条目：取自轮盘独立账 WheelMenu（分发语义与托盘同源复用
// internal/launcher，两账互不连带），Index 为当前层展示序下标，前端点击经
// Launch(path) 回传完整路径。
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

// Skin 轮盘皮肤账（机主拍板 2026-09-26"轮盘皮肤做"）：纯视觉偏好入后端账，
// 皮肤随 hanxidata 数据目录 NAS 同步随身。前端呈现侧归一语义与此同源
// （wheelSkin.ts：预设白名单/数值钳域，坏值归正不报错）：Preset ∈ frost|veil|ink；
// FaceAlpha 为盘纱不透明度整数百分比（有效域 35–100，再透桌面噪点透出、
// 盘面读不出"一块盘"）；Stroke 为扇区描边强度 0–100；FollowModuleColor 开启时
// 描边改用条目真图标主色（灰图标/矢量轨回落类型色）。
type Skin struct {
	Preset            string `json:"preset"`
	FaceAlpha         int    `json:"faceAlpha"`
	Stroke            int    `json:"stroke"`
	FollowModuleColor bool   `json:"followModuleColor"`
}

// Status 快捷菜单运行态（模块页展示 + 开关回显）。
type Status struct {
	TrapActive bool `json:"trapActive"` // 全局鼠标钩子是否在位
	HoldMs     int  `json:"holdMs"`     // 触发所需按住时长（ms）
	MoveTol    int  `json:"moveTol"`    // 抬手前允许的光标位移（物理像素）
	ItemCount  int  `json:"itemCount"`  // 当前主盘扇区数（含分组节点；二级轮盘关闭时为拍平后的叶子数）
	TwoTier    bool `json:"twoTier"`    // 二级轮盘是否启用（分组展开为子盘，否则拍平）
}
