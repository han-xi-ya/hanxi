// ============================================================================
// 托管适配器家族共享骨架（B 区质量审查落点）
//
// 24 个 ManagedModuleAdapter 工厂里逐字同形的订阅接线与回执模板收编于本件单源
// （组件契约见 components/managed/adapter.ts 三条归一化铁律）：事件名、模块文案
// 方言仍留在各 adapter，本件只承载**结构**——迁移前后 adapter 对外导出签名与
// 回执形状逐字节不变，破坏面为零。
//
// 收编判据：仅与逐字同形的块入表；任何带模块方言的转换（dbx 账目合并、
// bili23 回读聚合、piik in-progress 分支、rammap/mangodisk 已装词形等）
// 保持原样留在各 adapter，不塞进共享件当特例开关。
// ============================================================================

import { useWailsEvent } from '../composables/useWailsEvent'
import type { ManagedActionResult, NormalizedProgress } from '../components/managed/adapter'

/**
 * 实例态事件归一接线（铁律③）：订阅 `<module>:instance-state`，空载荷跳过，
 * 其余原样交付。语义与迁出前各 adapter 内联体逐字一致。
 */
export function bindInstanceState<S>(event: string, cb: (snap: S) => void): void {
  useWailsEvent<S>(event, (s) => {
    if (!s) return
    cb(s)
  })
}

/** 各模块生成 DownloadProgress 的公共必备形状（bindings @typedef 五字段恒在）。 */
export interface VersionProgressPayload {
  version: string
  stage: string
  done: number
  total: number
  message: string
}

/**
 * 版本下载进度归一接线（铁律②单键形）：订阅进度事件，空载荷/缺 version 丢弃，
 * 其余映射为 NormalizedProgress（key=version）。复合键模块（bcu 的 version|variant）
 * 经 keyOf 覆写键形——票据与面板行反查（versions.progressKey）必须同源构造。
 */
export function bindVersionProgress<T extends VersionProgressPayload>(
  event: string,
  cb: (progress: NormalizedProgress) => void,
  keyOf?: (payload: T) => string,
): void {
  useWailsEvent<T>(event, (t) => {
    if (!t || !t.version) return
    cb({
      key: keyOf ? keyOf(t) : t.version,
      stage: t.stage,
      done: t.done,
      total: t.total,
      message: t.message,
    })
  })
}

/**
 * setActive 统一回执：共享件消费 message（toast）与 activeVersion（高亮切换）。
 * 词面即原各 adapter 内联模板 `已将 ${ver} 设为使用版本`（逐字保留）。
 */
export function setActiveResult(version: string): ManagedActionResult {
  return { message: `已将 ${version} 设为使用版本`, activeVersion: version }
}

/**
 * download 统一回执：后端词 'already-installed' → 提示 + 要求重拉版本区，
 * 其余词（启动下载/已受理等）回空静默。词面 `版本 ${version} 已安装` 逐字保留。
 * 含额外回执词的模块（piik 的 'in-progress' 等）不走本件，保留内联分支。
 */
export function downloadOutcome(res: string | null | undefined, version: string): ManagedActionResult {
  return res === 'already-installed' ? { message: `版本 ${version} 已安装`, reloadVersions: true } : {}
}

/**
 * remove（确认闸已过、RPC 已删）统一回执：词面 `已卸载 ${version}` + 重拉版本区。
 */
export function uninstalledResult(version: string): ManagedActionResult {
  return { message: `已卸载 ${version}`, reloadVersions: true }
}
