// Package mousetrap 提供"右键长按"全局识别原语（Windows WH_MOUSE_LL 低级钩子）。
//
// 识别策略（真机实证修正版，核心是"吞按不放、短按回放、阈值即弹"）：
//   - WM_RBUTTONDOWN 一律吞掉（返回 1）并记录时刻/位置进入待判态——应用的异步按键状态
//     随"按下"更新，若放行按下而吞掉抬起，系统与前台应用会永远认为右键还按着，
//     导致其它窗口输入卡死、弹窗抢不到焦点（初版踩坑，见 TROUBLESHOOTING #29）；
//   - 按下同时启动阈值定时（time.AfterFunc，MinHold；到期仅 PostThreadMessage 回泵线程，
//     刻意不用 CreateThreadTimer——内核 APC 定时器在真机上直接把进程打崩）：按住到达
//     阈值即弹窗（Quicker 式，松手前就出现），手势标记 suppressed——抬手吞净，
//     应用全程没看见这次右键，上下文菜单不弹、按键状态干净；
//   - 阈值前抬手（普通右键）：吞掉真实抬起，经消息泵用 SendInput 回放一组
//     合成 按下+抬起，应用照常弹菜单；自家回放盖 replayMagic extraInfo、回调据此放行，
//     外部自动化的注入右键则与真实按键同权参与判定；
//   - 期间位移超过 MaxMove（右键拖拽，如资源管理器右拖文件）：立刻补回放一个"按下"，
//     之后的移动与抬起原样放行，让应用自己完成整个拖拽；
//   - 阈值前点击左/中键：视同放弃长按，按短按路径补回放（阈值后不打断——抬手仍吞净）；
//   - 按下位置落在任务栏/托盘溢出区窗口矩形内（FindWindow+GetWindowRect 相交判定；
//     Win11 XAML 任务栏上 WindowFromPoint 整带返回 0 不可用）则整体旁路不拦截。
//
// 低级钩子回调运行在安装它的线程上且必须尽快返回（超过 LowLevelHooksTimeout
// 会被系统跳过甚至静默摘钩），因此回调内只做时间/坐标比较、非阻塞投递与
// PostThreadMessage；SendInput 回放、阈值判定与日志全部在消息泵线程处理。
//
// 全进程仅支持一个活动 Trap（快捷菜单场景足够）；重复 Start 会先停旧再装新。
package mousetrap

import "time"

// Trigger 表示一次右键长按触发：抬手瞬间的光标物理像素坐标 + 实际按住时长。
type Trigger struct {
	X, Y int32
	Hold time.Duration
}

// ButtonEvent 表示观察到的一次真实（非合成）鼠标键按下事件（物理像素坐标）。
// 供消费侧实现"点击弹窗外部区域即收起"——弹窗未获焦时 Wails 的 LostFocus 不会触发，
// 必须依赖全局观察兜底。
type ButtonEvent struct {
	X, Y int32
}

// Config 识别阈值。
type Config struct {
	MinHold time.Duration // 按住至少这么久才算长按（<=0 取默认 450ms）
	MaxMove int32         // 按下到抬手允许的光标位移（物理像素，<=0 取默认 16）
}

// 默认阈值：450ms 长按 + 16px 位移容差（经验值，够快且不与传统右键误触相争）。
const (
	defaultMinHold = 450 * time.Millisecond
	defaultMaxMove = 16
)

func (c Config) normalized() Config {
	if c.MinHold <= 0 {
		c.MinHold = defaultMinHold
	}
	if c.MaxMove <= 0 {
		c.MaxMove = defaultMaxMove
	}
	return c
}
