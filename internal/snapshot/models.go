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

// TrackedFile 受保文件清单行（N33 批 A，文件为轴左栏）。
// Display 回落文件名——便签标题经装配根注入的 resolver 映射，config/state 的
// 中文名表在批 C 落地；Revisions 口径受 maxListRevisions 观察窗约束，如实标注。
type TrackedFile struct {
	Path       string `json:"path"`       // 白名单相对路径（RPC 往返用）
	Display    string `json:"display"`    // 中文名（映射不到回落文件名）
	Group      string `json:"group"`      // "memo" | "config" | "state"
	Revisions  int    `json:"revisions"`  // 观察窗内该文件的历史条数（0=从未入版）
	LastChange string `json:"lastChange"` // RFC3339；观察窗内该文件最近一次变化
	Alive      bool   `json:"alive"`      // 磁盘上当前是否还在（false=已删除，仍可见历史）
}

// FileRevision 文件时间线的一行（该文件在此版本发生变化）。
type FileRevision struct {
	RevisionID string `json:"revisionId"`
	Time       string `json:"time"`    // RFC3339
	Status     string `json:"status"`  // A/M/D/R（备份模式由相邻 manifest 差集读时算出，N33 §3）
	Summary    string `json:"summary"` // 该版整体摘要，可空→前端回落 status 词表
}

// FileDiff 单文件新旧对照（行 diff 在前端算，后端只回原样文本，N33 §4）。
// Old/New 各按 maxPreviewBytes 截断，对应 Truncated 标记如实置位。
type FileDiff struct {
	Path         string `json:"path"`
	Status       string `json:"status"` // 该版本对此文件的变化类型
	Old          string `json:"old"`
	New          string `json:"new"`
	OldTruncated bool   `json:"oldTruncated"`
	NewTruncated bool   `json:"newTruncated"`
	Summary      string `json:"summary"` // 该版整体摘要
}

// fileChange 某版本中单文件的变化事件（含改名双侧：R 时 Orig=旧名、Path=新名）。
type fileChange struct {
	Status string // 首字母 A/M/D/R/C/T
	Path   string // 变化后（该版本中）的路径
	Orig   string // 仅 R/C：变化前路径
}

// fileDiffRaw 引擎侧新旧文本原样（不截断；512KB 截断归服务层统一执行）。
type fileDiffRaw struct {
	Status  string
	Old     string
	New     string
	Summary string
}

// versionChanges 一个版本的 id、时间与全部变化事件。两引擎读历史共用素材：
// git 侧为 log -z --name-status 解析产物；备份侧为相邻 manifest 差集读时算结果
// （备份模式 A/M/D 是真事件非假信号，N33 §0.2-③ / §3）。Time RFC3339，
// Summary 已剥 "checkpoint: " 前缀。实现方按"指定版本 → 更老版本"排序。
type versionChanges struct {
	ID      string
	Time    string
	Summary string
	Changes []fileChange
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
	// fileChanges 读最近 window 个版本的全部文件变化事件（新→旧），
	// ListFiles 聚合与 DiffFile 定位改名链的共同底座。
	fileChanges(ctx context.Context, window int) ([]versionChanges, error)
	// fileHistory 查询名的单文件时间线（新→旧，≤window）。git 侧沿改名链追溯
	// （--follow），备份侧为 fileChanges 差集按路径过滤；两模式事件形态一致。
	fileHistory(ctx context.Context, relPath string, window int) ([]FileRevision, error)
	// fileDiff 取该版本此文件的新旧文本原样（改名沿链取旧名；A 无旧、
	// D 无新、首版无父 Old 给空）。relPath 已过服务层闸门，引擎侧仍需自检。
	fileDiff(ctx context.Context, id, relPath string) (fileDiffRaw, error)
	// heal 连续失败后的自救（git 模式重 init 仓库；备份模式 no-op）。
	heal(ctx context.Context)
}

// aggregateFileEvents 把逐版本变化事件摊平为"文件 → 变化时间线（新→旧）"。
// 改名规则与 git --follow 实探一致：改名到达算新名的 R 事件；改名离开算旧名
// 的 D 事件（旧名时间线上"恢复被删内容"据此回取最后存在版本）。
// 两引擎的 fileChanges 素材共用，ListFiles 计数与 FileHistory 时间线同源。
func aggregateFileEvents(recs []versionChanges) map[string][]FileRevision {
	out := make(map[string][]FileRevision)
	for _, rec := range recs {
		for _, ch := range rec.Changes {
			if ch.Orig != "" {
				out[ch.Orig] = append(out[ch.Orig], FileRevision{
					RevisionID: rec.ID, Time: rec.Time, Status: "D", Summary: rec.Summary,
				})
			}
			st := ch.Status
			if st == "" {
				st = "M"
			}
			out[ch.Path] = append(out[ch.Path], FileRevision{
				RevisionID: rec.ID, Time: rec.Time, Status: st, Summary: rec.Summary,
			})
		}
	}
	return out
}
