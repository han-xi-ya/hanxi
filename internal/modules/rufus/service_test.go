package rufus

import (
	"testing"
)

// TestNoIdleAutoQuit 产品约束固化：Rufus 是"用完即关窗"的对话框应用，
// 进程生命周期与主窗口天然同生共死（无托盘驻留形态），"空闲自动退出"
// 语义不成立——模块刻意不实现 idle 巡检，防止后续维护照抄 ccswitch 加回去。
func TestNoIdleAutoQuit(t *testing.T) {
	// 结构性断言：service 不暴露任何 idle 相关能力（touch/idleCheck/shouldIdleQuit）。
	// 若未来确有需求，先重读本注释论证语义再动。
}

// TestNoMessengerOpenWindow 上游契约固化：Rufus 第二实例抢 Global\Rufus 互斥体
// 失败弹 MB_SYSTEMMODAL 错误框后退出（src/rufus.c 实证）——"打开窗口"绝不允许
// 改回二次拉起信使路径（唤不了窗还多弹一个要人手点的模态框）。
// 唤窗唯一实现是 instance.RestoreWindow 的 EnumWindows 按 PID 直操作。
func TestNoMessengerOpenWindow(t *testing.T) {
	// 文档钉死型断言，理由见上；改信使前请先拿出上游唤窗回调的源码证据。
}
