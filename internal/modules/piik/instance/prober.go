// Package instance 实现 Piik 单实例运行引擎（服务型 frpc/DBX 口径）：
//
// Piik 是屏幕分享 headless 本地服务器（Go 主程序 + 内嵌 Web UI，浏览器开
// http://127.0.0.1:<port>/ 即用）。引擎职责：
//   - 组参拉起：`--local --port <p> --config <托管>/client.json --log-dir
//     <托管>/logs` + env PIIK_CLIENT_GATE_NO_BROWSER=true（机读模式开关），
//     工作目录锁定主 exe 所在目录（兄弟路径 runtime 双 exe 相对解析的兜底
//     保险，家族惯例）；控制台子系统程序 CREATE_NO_WINDOW 无窗拉起（frpc
//     同形）；
//   - 就绪双因子探针：进程名 piik-app.exe ∧ 分配端口 LISTEN（WaitReady，
//     service 层驱动）——上游无单实例 mutex，端口绑定即事实互斥，端口是
//     唯一就绪硬证据；"进程活着"不作成功信号（ddnsgo 同纪律）；
//   - stdout 机读字段逐行解析入快照（machine.go）：Local access /
//     Local access password / LAN invitation origin / Public invitation
//     origin / GATE_NO_BROWSER 命中位。安全面纪律：口令明文在解析处即弃
//     （引擎类型里根本没有承载它的字段，快照与日志事件都只有
//     PasswordSet 布尔/脱敏行——"秘密不上前端"是类型层事实而非运行时
//     承诺）；错误全在 stderr，两路输出都进环形日志与 OnLog 事件；
//   - 优雅退出：Quit = 向 stdin 写一个字节（上游实证收到任意字节优雅停机，
//     内部 5s 预算）→ 宽限期 quitGrace（覆盖 5s 预算 + 余量）等自然收口 →
//     超时 JobObject Terminate 兜底。孙进程 piik-capture.exe/cloudflared.exe
//     必杀：Job Object 创建即 KILL_ON_CLOSE、无 breakaway 许可，整树归 Job
//     管辖（Hanxi 崩溃退出同样连带全树）；Detached 开关解除退出联动
//     （"不随 Hanxi 关闭"，service 层经 !FollowOnExit 决策，随家族默认）；
//   - wait 退出四分类顺序照家族（supervisor 内核纪律，不可调换）：
//     手动停止 → 探针仍见目标存活（外部接管）→ 退出码 0 → 异常退出。
//
// 架构决策如实申明：家族新员（ccswitch/dbx/paseo/ddnsgo）均组合共享内核
// packages/go/supervisor，本包却按 frpc 蓝本自带进程治理——内核
// processHandle 不含 stdin 通道（其优雅退出面只覆盖管道命令/WM_CLOSE/
// HTTP shutdown），Piik 的 stdin 字节停机信令要求引擎在 spawn 前持有
// stdin 写端并跨代管理；为一家需求改共享内核（Spec.Stdin + Engine 取用
// 口 + 全族测试 fake 补件）属跨线架构变更，留待 owner 裁决收敛（届时本包
// 可平移）。其余纪律（startMu/mu 分离、单一 Wait 所有者、上一代未收口
// 拒启动、external 只甄别不强杀、回调锁外广播、日志泵不并入住口链路）
// 与内核逐条对齐。
//
// 上游契约备忘（阶段 0 实锤）：asInvoker 无 manifest（无提权特判通道）；
// 无托盘、无 quit CLI——"唤窗"语义在 Piik 上不存在窗口，等价物是打开本机
// 界面：引擎只提供 URL 组装（LocalURL/URLForPort），浏览器调用归 service。
//
// 本包零框架依赖，便于单元测试（全 seam：假 spawner/假 stdin 管道/假
// JobAPI/假探针，零真网零真 piik）。
package instance

import "time"

// Probe 实例存在性探针（免框架依赖；service 层注入 NewProbe() 平台实现）。
// 双因子判据分两路自持——就绪与外部甄别语义不同，刻意不合并成单一方法：
//   - 就绪（WaitReady）：进程名 ∧ 分配端口 LISTEN（我方端口已知，端口可连
//     即上游服务就绪的硬证据）；
//   - 外部（wait 分类/RefreshExternal）：仅进程名事实（外部实例端口不可知，
//     拨测无从谈起；Piik 无 mutex 允许多实例并存，同名进程在场即
//     "非本引擎托管的 piik 在世"）。
type Probe interface {
	// FindPIDs 返回全部 piik-app.exe 进程 PID（含外部实例；runtime 双 exe
	// 孙进程为异名 piik-capture.exe/cloudflared.exe，不会污染计数）。
	FindPIDs() []uint32
	// ProcessRunning piik-app.exe 进程存在性（进程名快照枚举）。
	ProcessRunning() bool
	// PortListening 127.0.0.1:port TCP 可连通 = 实例已绑定该端口对外服务。
	PortListening(port int) bool
}

// probePollInterval WaitReady 轮询间隔（supervisor 内核 readyPollInterval 同值；
// 包级变量供单测压缩）。
var probePollInterval = 150 * time.Millisecond

// readyFact 就绪双因子合取（纯函数，供引擎轮询与单测钉死判据语义）。
func readyFact(probe Probe, port int) bool {
	return port > 0 && probe.ProcessRunning() && probe.PortListening(port)
}
