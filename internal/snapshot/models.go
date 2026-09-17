package snapshot

import (
	"context"
	"regexp"
	"time"
)

// 生命周期与闸门常量（ccswitch ticker 范式：stop-channel + 装配根 Start/Stop 驱动；
// tick 只读 Stat/git，永不阻塞 UI）。
const (
	tickInterval       = 5 * time.Second
	shutdownFlushBound = 3 * time.Second // 退出前最后一发的同步闸门：卡死不拖退出
	commandTimeout     = 15 * time.Second
	maxListRevisions   = 50
	repoRebuildFails   = 3 // git 连续失败 N 次→重 init 仓库（旧仓库改名保留），不烧 tick
)

// mode 标识（对外 status.mode 字段）：git 检查点 / 影子拷贝降级。
const (
	ModeGit    = "git"
	ModeBackup = "backup"
)

// snapshotsDirName 快照驻留目录名（数据根下唯一落点；git-dir 与影子备份都住这里）。
// 隐藏目录、不在白名单内——git 侧靠 pathspec 隔离，白名单式拷贝天然规避"备份目录
// 被自己备份"的自嵌套坑（MooTool backupService 曾踩）。
const snapshotsDirName = ".snapshots"

// gitRepoDirName / backupDirName 驻留目录内两种模式的子落点。
const (
	gitRepoDirName = "repo.git"
	backupDirName  = "backup"
)

// maxPreviewBytes 预览上限：白名单 JSON 均 KB 级，512KB 已属异常大文件保护
// （截断规则照 MooTool vaultGitService）。
const maxPreviewBytes = 512 * 1024

// 版本标识形态：git commit hash（hex）或影子备份时间戳目录名。
var (
	gitHexRe   = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	backupIDRe = regexp.MustCompile(`^\d{8}-\d{6}(-\d+)?$`)
)

// backupTimeLayout 影子备份目录名时间格式（字典序即时间序，滚动删依赖此性质）。
const backupTimeLayout = "20060102-150405"

// StatusInfo "历史版本"分区顶部状态行。
type StatusInfo struct {
	Enabled         bool   `json:"enabled"`
	Mode            string `json:"mode"`            // "git" | "backup"（引擎实际生效模式；未启动为空）
	GitAvailable    bool   `json:"gitAvailable"`    // 探测结论（false 时恒为 backup）
	LastCommitAt    string `json:"lastCommitAt"`    // 最近一次成功提交 RFC3339；空=本进程从未提交
	RevisionCount   int    `json:"revisionCount"`   // 历史版本份数
	IdleSeconds     int    `json:"idleSeconds"`     // 生效的空闲阈值
	IntervalMinutes int    `json:"intervalMinutes"` // 生效的最小提交间隔
	SnapshotDir     string `json:"snapshotDir"`     // .snapshots 目录绝对路径（展示/打开用）
}

// Preferences 分区内可调偏好（落 settings.Store 持久化）。
type Preferences struct {
	Enabled         bool `json:"enabled"`
	IdleSeconds     int  `json:"idleSeconds"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

// Revision 历史版本列表行。ID 为 git commit hash 或影子备份时间戳目录名，
// 对 PreviewFile/RestoreFile 不透明回传。
type Revision struct {
	ID      string `json:"id"`
	Time    string `json:"time"`    // RFC3339
	Summary string `json:"summary"` // "config.json, memo/xxx.md 等 N 个文件"
}

// RevisionFile 某个版本包含的文件及其变化类型（git 语义首字母 M/A/D/R；备份模式恒 M）。
type RevisionFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// FilePreview 单文件内容预览（≤512KB 文本，超出截断）。
type FilePreview struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

// engine 两种模式统一提交/历史/恢复内核（PLAN §3.3："两种模式对外统一
// History()/Restore() 接口"）。实现者必须全部并发安全。
type engine interface {
	// mode 返回 ModeGit / ModeBackup。
	mode() string
	// changes 列出白名单内当前未落版本的变更文件（相对路径，斜杠分隔）；
	// 空切片 = 无变更，天然去重（"无变更不 commit"是 MooTool
	// "同一活动只一次"的等价实现）。
	changes(ctx context.Context) ([]string, error)
	// commit 把白名单当前全部变更固化为一个版本，files 用于生成人话摘要。
	commit(ctx context.Context, files []string) error
	revisions(ctx context.Context, limit int) ([]Revision, error)
	revisionFiles(ctx context.Context, id string) ([]RevisionFile, error)
	// file 读取指定版本中某文件的内容（字节原样，预览与恢复共用）。
	file(ctx context.Context, id, relPath string) ([]byte, error)
	// heal 连续失败后的自救（git 模式重 init 仓库；备份模式 no-op）。
	heal(ctx context.Context)
}
