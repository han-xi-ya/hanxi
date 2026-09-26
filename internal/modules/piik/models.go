package piik

import (
	piikinstance "hanxi/internal/modules/piik/instance"
	piikversion "hanxi/internal/modules/piik/version"
)

// 冻结契约类型锚点（A 线 version/instance 子包公开面的本包别名）：本包业务
// 代码只经这几个别名引用 A 面类型，收口时如需改名，改动集中在本块。
// 现状：两侧均已落盘证实（version 侧 PiikRelease/PiikVersionInfo/DownloadProgress
// + Manager 全套面；instance 侧 Snapshot/Options/Engine + DefaultPort/URLForPort），
// 其中 DownloadProgress 与 Snapshot 另由装配根 app.go 的 RegisterEvent 实锁。
type (
	Release          = piikversion.PiikRelease
	VersionInfo      = piikversion.PiikVersionInfo
	DownloadProgress = piikversion.DownloadProgress
	Snapshot         = piikinstance.Snapshot
	LaunchOptions    = piikinstance.Options
)

// 机读字段的取数口径（本层不做二次投影，直出 A 线快照展平）：上游机读模式
// （env PIIK_CLIENT_GATE_NO_BROWSER=true，A 线恒注入）stdout 四条打点已由 A 线
// 解析入账并随快照广播——localAccessOpen（本地访问免口令可用）、passwordSet
// （口令**有无**布尔：明文在解析处即弃，A 线类型层就没有可承载值的字段，
// "口令值绝不上前端"由此结构性成立，不靠本层自觉）、lanInvitation /
// publicInvitation（LAN 与公网 Cloudflare 隧道邀请来源，上游自拼含本机内网 IP
// 与隧道域名，本层原样转呈不二次拼装——空串 = 该通道当前不可用，前端不装
// 样子，PLAN ④风险 2"知情不阻断"裁决）、noBrowser（GATE_NO_BROWSER 命中位，
// 界面需手动打开，前端据此亮兜底链接）。
//
// 前端 adapters/piik.ts 现按本线提议名 lanUrl/publicUrl/accessPasswordSet 消费：
// 名字与本处事实源不一致，**收口点定在前端那一处投影接口**（其文件注释已自授
// "若 B 线生成物字段名有更宽/异名事实，经此一处收口对齐"），本层不加同值别名
// 字段——同一份事实在一个 JSON 里出现两个键名，是漂移的开始而不是结束。

// ControlOutcome 控制操作（Start 起服务 / OpenWindow 打开界面）的执行结果说明。
// action 词表照托管族（ccswitch/dbx 同名）：started（冷启动，仅 Start 出口）/
// already-running（Start 幂等直返）/ opened（补开自有实例界面）/
// external-opened（打开外部实例界面，恒带"非 Hanxi 托管"如实标注）/
// starting（启动临界区，不重复动作）/ external（外部实例在场，托管不接管）。
// 两个动词严格分工（C 线前端同口径锁定）：**OpenWindow 绝不冷启动**——起服务
// 等于在局域网敞开分享端口，只在机主点「启动」时发生；界面没自动弹出时
// OpenWindow 只负责补开。
// 状态词表如实声明：A 线为五态（stopped/starting/running/failed/external），
// 优雅停的收口相在引擎内部（stopping）对前端折并入 running，**没有 quitting 档**
// ——前端 adapters/piik.ts 现按六态写词表，其 quitting 分支属永不到来的档，
// 已列联编收口清单（要么前端删档，要么 A 线补档，本层不造幻影状态）。
type ControlOutcome struct {
	Action   string `json:"action"`
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
	Port     int    `json:"port"`    // 本条回执相关的监听端口（0 = 不适用）
	URL      string `json:"url"`     // 界面地址（打开界面/已运行回执携带）
}

// QuitOutcome 退出执行结果。
// 如实口径：piik 无自有窗口，优雅停走**上游官方通道**（stdin 写入/关闭即
// 优雅退出），宽限后 JobObject 兜底并连带回收 cloudflared / piik-capture 孙
// 进程。external 态（机主自行双击裸 exe 起的实例，占住 8787）绝不越权终止。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`
	External bool   `json:"external"`
	Message  string `json:"message"`
	Port     int    `json:"port"` // 退出后本端口已释放（0 = 不适用）
}

// PiikStatus GetStatus 复合投影：A 线实例快照 + 端口账 + 机读字段投影 +
// 托管数据目录账 + 使用版本漂移复查。
//
// 内嵌 Snapshot 经 JSON 展平，前端消费形状 = "piik:instance-state" 事件载荷
// 再加本层投影位（两路同形，前端不必分两套读法——dbx/ccswitch 同纪律）。
// 前提纪律如实声明：内嵌意味着 A 线快照字段会原样进前端，因此
// **piikinstance.Snapshot 不得携带口令明文等任何秘密值**（口令只得以
// PasswordSet 布尔形态出场）；该纪律是联编硬要求，见交付收口清单。
type PiikStatus struct {
	// Snapshot 内嵌经 JSON 展平：state/pid/exitCode/error/external/startedAt/
	// stoppedAt/version 与 **port**（A 线单点记账的本代分配端口，8787 起被占则
	// 上移）+ 机读四字段原样在位。本层不再另存端口副本——两处账必漂移。
	Snapshot

	// ListenPort 界面访问端口：running = 引擎记账的本代分配端口（8787 起被占
	// 则由本层分配器上移）；external = 上游默认口（外部实例端口不可知，前端
	// 话术"通常占着 8787"同源取此值）；stopped/failed = 最近一代端口，无账给 0。
	ListenPort int `json:"listenPort"`
	// ConsoleURL 本机界面地址（http://127.0.0.1:<port>/，值由 A 线
	// instance.URLForPort 单点组装）：**仅自有实例在跑时给值**，其余状态空串——
	// 前端"界面恒在浏览器"话术与 GATE_NO_BROWSER 兜底链接共用这一个字符串，
	// 杜绝前后端两处各拼一遍。external 态不填（那个实例的端口不归本层记账，
	// 谎报一个可点链接比不给更糟），由 OpenWindow 现场拨测候选口后代开浏览器。
	ConsoleURL string `json:"consoleUrl"`

	// DataDir / ConfigPath / LogDir 托管数据目录三账（跨版本共享、删版本不
	// 删数据；前端"打开数据目录"与 metaHints 披露同源于此）。
	DataDir    string `json:"dataDir"`
	ConfigPath string `json:"configPath"`
	LogDir     string `json:"logDir"`

	Drifted   bool   `json:"drifted"`   // 使用版本 exe 发生账外漂移（只报告不处置）
	DriftNote string `json:"driftNote"` // 漂移/无法比对的如实明细（空 = 未复查）
}
