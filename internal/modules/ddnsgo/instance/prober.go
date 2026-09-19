// Package instance 实现 ddns-go 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 端口就绪/退出分类 → 手动终止收口）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 ddns-go 领域适配：
//   - 双通道探针形状适配（readiness=TCP 端口 / external=进程名扫描，见 supProbe）
//     与启动前置端口预检（甄别 external/他人占用，内核 Spec 无启动前挂点，留本层）；
//   - 控制台子系统无窗拉起经 Spec.HideWindow、DDNS_GO_DAEMON=1 环境注入经
//     Spec.Env（均交内核落地，本包不再自带 child_windows）；
//   - 日志环形缓冲与 DNS 服务商凭据脱敏（内核 OnLog 逐行透传，策略在本层回调）；
//   - 状态词表/错误文案映射与退出码、停止时刻账目（前端事件契约零漂移）。
//
// ddns-go（Go 编写的后台 DDNS 更新器，Web 管理界面）由内核启动后绑定
// Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），Hanxi 以何种方式
// 退出内核都会连带终止进程，杜绝孤儿占端口。
//
// 上游契约（jeessy2/ddns-go v6.17.6 main.go / util/user.go 源码实证）：
//   - 控制台子系统 CLI 程序，无窗口、无单实例锁、无优雅退出通道；
//   - 配置恒存 %USERPROFILE%\.ddns_go_config.yaml（-c 可覆盖，本引擎不改——
//     与用户自行运行的实例共享同一份配置，托管即无缝接管）；
//   - Web 服务经 -l 指定地址监听（上游默认 :9876 绑全网卡；本引擎固定
//     127.0.0.1:port 仅回环，杜绝 DNS 服务商凭据面板暴露到局域网）；
//   - 关键后门：启动时若检测到已安装同名 Windows 服务（kardianos/service
//     Status != Unknown），裸执行 exe 会被劫持走 SCM 服务路径直接失败退出。
//     环境变量 DDNS_GO_DAEMON=1 让上游跳过服务检测直跑 run()——本引擎恒注入；
//   - 端口绑定失败的进程不会立即退出：web 协程打印"监听端口发生异常"后
//     sleep 1 分钟才 os.Exit(1)。因此就绪判定必须走 TCP 端口探测而非
//     "进程活着 = 启动成功"，且启动前做端口预检。
//
// 存活探测无互斥体可用（与 ccswitch 不同），采用 flclash 的进程名快照枚举。
//
// 本包零框架依赖，便于单元测试。
package instance

// Probe 实例存活与端口探测（免框架依赖；service 层注入平台实现）。
type Probe interface {
	// FindPIDs 返回全部 ddns-go.exe 进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning ddns-go.exe 进程存在性探测（进程名扫描，覆盖任意端口的实例）。
	IsRunning() bool
	// PortOpen 指定监听地址 TCP 可连通 = ddns-go web 服务就绪。
	PortOpen(listenAddr string) bool
}
