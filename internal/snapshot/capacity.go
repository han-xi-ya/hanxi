package snapshot

// 容量口径常量收口处：历史版本"能看多少"的两个数字全包唯一落点（值零变更，
// 分别自 models.go 生命周期 const 块与 backup.go 迁入——工程卫生批双写对账）。
//
//   - maxListRevisions：观察窗上限——ListRevisions/ListFiles 截窗、FileHistory
//     limit 回落、DiffFile 超窗报错文案（git.go）全经此一处；
//   - backupKeepCount：备份降级链滚动保留份数（Q3 拍板），prune 依此修剪。
//
// 前后端双写对账（本文件是跨语言互指的正主）：前端展示端以
// frontend/src/constants/snapshotLabels.ts 的 REVISION_WINDOW / BACKUP_KEEP 为
// 全前端唯一数字源（modeChip 徽标、windowCapNote/revisionCapNote 容量整句、
// FileHistory 拉取 limit 均经此插值）。绑定现面（bindings checkpointservice.js
// 的 GetStatus→StatusInfo）没有容量出口，且裁决不为这两个数新开 Wails 导出；
// 两侧漂移由 frontend/src/constants/__tests__/snapshotCapacity.contract.spec.ts
// 焊死——Go 源文本提值与 TS 常量逐字对账，任何一侧单独改动直接红。
// 改值流程：动本文件 → 同步 snapshotLabels.ts → 复核 N33 批 D 容量文案
// （SnapshotSection/SnapshotTimeline 的"最多展示最近 N 版/份"由视图 spec 锁字面）。
const (
	// maxListRevisions git 模式观察窗上限（列表/时间线请求超窗按此截断，≤0 回落此值）。
	maxListRevisions = 50
	// backupKeepCount 备份降级链滚动保留份数（Q3 拍板）：字典序倒排修剪后只剩最近 N 份。
	backupKeepCount = 30
)
