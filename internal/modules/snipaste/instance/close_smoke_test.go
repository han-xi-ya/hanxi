package instance

import xsys "golang.org/x/sys/windows"

// closeSmokeTerminate 为 TestQuitCloseRequestSmoke 的关闭替身实现因果面：
// 以**退出码 0** 当场终结目标进程——与旧构造（cmd 挂 ping 2s 后自然退出、
// 退出码 0 落宽限窗）在退出形态上完全同谱，但收口时机由被测动作驱动（毫秒级
// 落 settle），不再依赖 CPU 调度能否让 ping 在全量并行的饥饿下赶完 3s 宽限
// （旧构造饥饿即漂 → forced-job 误归因，2026-10-26 收口门禁红的根因）。
//
// 刻意不用 os.Process.Kill：其为 TerminateProcess(h, 1)，退出码 1 会被内核
// 判为异常退出改 failed——终态快照/文案即偏离旧构造的"自然收口"事实基线。
// OpenProcess 失败（进程已消失）静默返回：收口已由 settle 表达，与旧构造
// "自然退出后再 Quit"分支同语义。
func closeSmokeTerminate(pid uint32) {
	h, err := xsys.OpenProcess(xsys.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return
	}
	_ = xsys.TerminateProcess(h, 0)
	_ = xsys.CloseHandle(h)
}
