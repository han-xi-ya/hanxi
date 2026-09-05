package bili23

import (
	"time"

	"hanxi/internal/modules/bili23/instance"
)

// ControlOutcome 打开窗口操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（自有实例唤窗）/ external-opened（外部实例唤窗）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。Bili23 特有：上游"关闭窗口"行为用户可配，
// 退出按钮可能只换来"收入托盘"或"弹窗询问"，必须如实分叉上报而非假装已退出。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Hidden   bool   `json:"hidden"`   // true = 进程收入 Bili23 托盘（仍托管）
	Asked    bool   `json:"asked"`    // true = Bili23 弹出退出询问对话框，等待用户选择
	Message  string `json:"message"`  // 面向用户的执行说明
}

// Status 控制台聚合视图：引擎快照全字段平铺 + 窗口可见性 + 运行时长。
// 刻意平铺而非内嵌 instance.Snapshot——规避 Wails v3 对内嵌结构的 TS 绑定生成歧义，
// 前端拿到的就是单一扁平对象，一次 GetStatus 得到渲染所需的一切。
type Status struct {
	Version       string    `json:"version"`
	State         string    `json:"state"`
	PID           uint32    `json:"pid"`
	ExitCode      int       `json:"exitCode"`
	Error         string    `json:"error"`
	External      bool      `json:"external"`
	StartedAt     time.Time `json:"startedAt"`
	StoppedAt     time.Time `json:"stoppedAt"`
	WindowVisible bool      `json:"windowVisible"` // 自有实例是否有可见窗口（false=收入托盘/尚未开窗）
	UptimeSeconds int64     `json:"uptimeSeconds"` // 自有实例已运行秒数（非 running 为 0）
}

// statusFrom 由引擎快照与实时窗口探测合成状态视图。
func statusFrom(snap instance.Snapshot, windowVisible bool) Status {
	var uptime int64
	if snap.State == instance.StateRunning && !snap.StartedAt.IsZero() {
		uptime = int64(time.Since(snap.StartedAt).Seconds())
	}
	return Status{
		Version:       snap.Version,
		State:         string(snap.State),
		PID:           snap.PID,
		ExitCode:      snap.ExitCode,
		Error:         snap.Error,
		External:      snap.External,
		StartedAt:     snap.StartedAt,
		StoppedAt:     snap.StoppedAt,
		WindowVisible: snap.State == instance.StateRunning && windowVisible,
		UptimeSeconds: uptime,
	}
}
