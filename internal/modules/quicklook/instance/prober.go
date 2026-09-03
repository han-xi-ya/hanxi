// Package instance 实现 QuickLook 单实例运行引擎：
//
// QuickLook 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 以何种方式退出（正常关窗/托盘退出/崩溃/强杀）内核都会连带终止 QuickLook，
// 杜绝孤儿驻留。
//
// 上游运行契约（源码实证，决定本引擎形态，勿回退照抄 ccswitch）：
//   - 全局键盘钩子是进程内低级钩子，非注入：App.xaml.cs → KeystrokeDispatcher 用
//     GlobalKeyboardHook.SetWindowsHookEx(WH_KEYBOARD_LL) 与 SetWinEventHook
//     (WINEVENT_OUTOFCONTEXT)。回调全部跑在 Manager 自身进程内，不向 explorer.exe
//     或任何第三方窗口注入 DLL；进程一旦终止，钩子由操作系统自动摘除，系统零残渣。
//     这是 QuickLook 可被 JobObject 干净托管的根本依据（初判"退出留钩子残渣"经此推翻）；
//   - 单实例：命名互斥体 "QuickLook.App.Mutex"（EnsureFirstInstance）。二次无参拉起只
//     弹"已在运行"框后自退，不会 show+focus 任何窗口——故本引擎刻意不做 OpenWindow
//     信使路径，托管侧"打开设置"只能引导用户点托盘图标（上游无唤窗契约）；
//   - 便携免安装：官方便携 zip 根含 portable.lock（SettingHelper.IsPortableVersion），
//     解压跑 QuickLook.exe 即生效，配置随 exe 走，无需 MSI/注册表；
//   - 优雅退出：命名管道 "QuickLook.App.Pipe.<用户SID>"（PipeServerManager）接受 "Quit"
//     行消息 → App.OnExit 释放互斥体并正常收尾（exit 0）。故 Quit 优先投递管道，
//     宽限等待自然退出，JobObject 强杀仅作兜底（管道不可达/卡死时）。
//   - 许可合规：上游 GPL-3.0，托管模式仅启动上游官方二进制、不链接不分发其代码，
//     无传染性；QuickLook 自有配置在便携目录（随 exe），不属 Hanxi 读写范围。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// QuickLookProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type QuickLookProbe interface {
	// IsRunning 命名互斥体存在性探测（主实例持有 = 存活）。
	IsRunning() bool
	// WaitForReady 轮询等待互斥体出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
