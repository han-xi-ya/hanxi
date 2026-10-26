package sshkeys

// 本文件是 sshkeys 的**对外契约面**：所有前端可调的 RPC 入参与返回都在这里。
// 单独成文件的原因不是整洁，是并行施工需要——各实现文件（ledger/export/keyops/deploy）
// 与子包都对着这份类型写，改字段必须先过这里。
//
// **关于 Code 字段的一条如实说明（别把它当已生效的契约）**：Wails 绑定对 `(T, error)`
// 的语义是"error 非空时 reject message、**T 被丢弃**"，所以被拒绝的那条路径上，前端
// 实际拿不到 Code，只能拿到 message 原文。写动作的"按码上色"因此目前只在**成功路径**
// 成立，失败路径前端按原文显示 + 本地预判（已按此现实实现，不是漏做）。
// 要真正兑现"失败也回码"，得把这些 RPC 改成 single-result（业务拒绝算结论不算异常，
// 与 CheckSSH 的"error 通道只装没能开测"同一口径）——那是 Go 与四个对话框的同步改造，
// 记在 docs/plans/PLAN_SSH_KEYS.md §⑧/§⑨ 欠账清单（§⑨-1：未修仍成立），不在这里留一个看着能用其实到不了手的字段。
//
// 三条贯穿全部类型的硬规矩：
//  1. **任何返回值里都不许出现私钥正文、也不许出现口令**。口令只允许出现在
//     *Request 里，用完即弃（对齐 sshauth.go 的"不进结果、不进错误、不落盘"口径）。
//  2. **路径都是字符串路径，不是内容**。删除/覆盖类动作必须带 Confirm 字段，
//     由前端把目标名回显一遍再提交，不接受"布尔一键确认"。
//  3. 结构体字段全部要能在 Info 面板/日志里安全展示——这是审过的隐含契约，
//     新增字段前想清楚它会不会被复制进日志。

// HostEntry 主机清单的一条。别名列就是 ssh 别名，也是唯一键。
type HostEntry struct {
	Alias   string `json:"alias"`   // ssh 别名，唯一键；规则见 sshconf.ValidateAlias
	Host    string `json:"host"`    // IP 或域名
	Port    int    `json:"port"`    // 0 按 22 处理
	User    string `json:"user"`    // 登录用户
	KeyPath string `json:"keyPath"` // 私钥路径，**只存路径**
	Note    string `json:"note"`    // 备注，渲染成配置里的注释行
	Enabled bool   `json:"enabled"` // 关掉即不导出给 ssh（但账本保留）
}

// ExportSummary 导出对账结果：账本与"ssh 实际能读到的那份"之间的关系。
//
// 为什么不是简单返回 error：机主要的是"我改了账本，ssh 那边到底跟上没有"，
// 一个 error 只能表达成败，表达不了"写成功了但 Include 那行没接上"这类半态。
type ExportSummary struct {
	Written    bool   `json:"written"`    // 本次真的写了文件
	ConfPath   string `json:"confPath"`   // 实际落盘的绝对路径（明写给用户看）
	Included   bool   `json:"included"`   // 顶层 ~/.ssh/config 是否已有 Include 行
	Rendered   int    `json:"rendered"`   // 导出的段数
	Drift      bool   `json:"drift"`      // 账本比盘上产物新（有待导出的改动）
	BackupPath string `json:"backupPath"` // 覆盖前留下的备份路径，空表示没备份
	KeyMissing int    `json:"keyMissing"` // 有几条的私钥文件在本机不存在（换机后的常态）
	// MissingKeyAliases 是上述缺失的**具体别名**。为什么不能只给总数让前端自己猜：
	// 前端唯一的线索是台账（只扫 ~/.ssh），而钥匙完全可能放在别处（D:\keys\id_rsa）——
	// 拿"基名不在台账里"当判据会把**真实存在的钥匙标成找不到**，而这顶帽子正挂在
	// "能不能删这把"的判断现场，误标的后果就是误删。故由后端逐条 stat 给确切名单。
	// 密钥路径留空的条目**不算缺失**：那是"没指定钥匙"（走 agent/默认身份），语义不同。
	MissingKeyAliases []string `json:"missingKeyAliases"`
	Message           string   `json:"message"` // 给用户看的一句结论，不装术语
}

// GenerateRequest 生成一把新密钥对。
type GenerateRequest struct {
	Name       string `json:"name"`       // 落盘文件名（id_xxx 形态），不含目录
	Type       string `json:"type"`       // "ed25519" | "rsa"；默认 ed25519
	RSABits    int    `json:"rsaBits"`    // 仅 rsa 生效，0 走 3072 默认
	Passphrase string `json:"passphrase"` // 空串 = 不加口令；口令不进任何返回与日志
	Comment    string `json:"comment"`    // 公钥注释（日后在 authorized_keys 里认钥匙全靠它）
	Overwrite  bool   `json:"overwrite"`  // 目标已存在时必须显式置真，否则拒绝
}

// GenerateResult 生成结果。没有私钥内容，只有路径与指纹。
//
// Code 是给前端上色的枚举（见 manage.go），Message 是人读结论；**前端不得解析
// Message 来决定行为**——文案会改，码不会。
type GenerateResult struct {
	PrivatePath string `json:"privatePath"`
	PublicPath  string `json:"publicPath"`
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"`
	Guarded     bool   `json:"guarded"` // 生成结果是否受口令保护（直接决定下一步建议）
	Code        string `json:"code"`
	Message     string `json:"message"`
}

// SetPassphraseRequest 给已有私钥加/换/去口令。
//
// 老口令允许为空（= 当前无口令，要加上）。**新口令为空且当前有口令 = 去掉保护**，
// 这是降级动作，实现侧必须把它当危险操作对待并要求 Confirm。
type SetPassphraseRequest struct {
	Path          string `json:"path"`
	OldPassphrase string `json:"oldPassphrase"`
	NewPassphrase string `json:"newPassphrase"`
	Confirm       string `json:"confirm"` // 必须回显文件名，去掉口令时必填非空
}

// KeyOpResult 密钥写操作的统一返回：路径 + 新指纹 + 一句话结论。
// Path 是**解析后的真身路径**（可能与传入字面不同形），前端要回写台账就用这个值，
// 不要拿它和用户当初输入的字符串做相等比较。
type KeyOpResult struct {
	Path string `json:"path"`
	// PublicPath 本次动作**实际新建或删除**的那份 .pub。
	//
	// 为什么单独要有这个字段：补公钥时 Path 的语义是"这把钥匙"（私钥真身路径），
	// 新写出的 .pub 若只在 Message 散文里，界面上就没有可核对、可复制的机器值——
	// 用户看不到"到底生出个什么文件"。删除同理：删掉的公钥文件名应当是结构化字段。
	// 空值有意义："配套公钥本就不存在"。
	PublicPath  string `json:"publicPath"`
	Fingerprint string `json:"fingerprint"`
	Guarded     bool   `json:"guarded"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

// ExportPubRequest 从私钥补出配套 .pub（公钥段本就明文可得，**带口令的私钥也能用**，
// 且全程不需要口令）。已存在 .pub 时后端拒绝，不覆盖。
type ExportPubRequest struct {
	Path    string `json:"path"`    // 私钥路径
	Comment string `json:"comment"` // 公钥行尾注释；留空就真空着，不拿文件名编一个
}

// DeletePubRequest 删除**孤儿** .pub（配套私钥已不在）。有私钥时后端坚决拒——
// 那种情况该走成对删除，从这儿绕过去就会留下"半把钥匙"。
type DeletePubRequest struct {
	Path    string `json:"path"`
	Confirm string `json:"confirm"` // 必须等于文件名
}

// DeleteKeyRequest 删除本机密钥文件。私钥与配套公钥**成对删**，
// 只删一半会留下"有公钥没私钥"的孤儿，比留着更难判断。
type DeleteKeyRequest struct {
	Path    string `json:"path"`
	Confirm string `json:"confirm"` // 必须等于文件名，不等就拒绝——防手滑
}

// PinInfo 一条主机密钥钉定（TOFU 记录）。只含地址与指纹，不含任何密钥材料。
type PinInfo struct {
	Addr        string `json:"addr"`
	Fingerprint string `json:"fingerprint"`
}

// DeployRequest 把某把公钥装到目标机器的 authorized_keys 里。
//
// 这是本模块唯一"对外部机器产生写影响"的动作：它会用给定凭据登录一次，
// 并可能在对端 sshd 日志留下记录（口令错误时是 Failed password）。
// 因此与 CheckSSHAuth 一样：**只能是显式动作，绝不被其它 RPC 顺带调用**。
type DeployRequest struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`   // 一次性，不进结果、不进日志、不落盘
	PubKeyPath string `json:"pubKeyPath"` // 装哪把公钥（读 .pub 文件，不读私钥）
	TimeoutMs  int    `json:"timeoutMs"`
}

// DeployResult 部署结论。verdict 是有限枚举，前端按值上色，不解析文本。
type DeployResult struct {
	Verdict            string `json:"verdict"` // appended | already-present | auth-rejected | unreachable | timeout | not-ssh | refused
	OK                 bool   `json:"ok"`      // appended 与 already-present 为真
	Reason             string `json:"reason"`  // 给用户看的一句话，已过口令打码
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
	AuthorizedKeysPath string `json:"authorizedKeysPath"` // 远端写入位置，如实回显
	PubKeyFingerprint  string `json:"pubKeyFingerprint"`  // 装上去的那把的指纹
	Code               string `json:"code"`               // 失败归类的枚举码，供前端上色
}
