// ============================================================================
// 剪贴板内置模块 · 前端类型层（多 agent 并行 kernel，所有实现 agent 只读）
//
// 与 docs/plans/2026-09-26-clipboard-contract.md §3 的 Go Entry/Status 逐字
// 对位（JSON tag = 此处字段名）。真实接线经 @/adapters/clipboard 的 transport
// 注入完成——本文件与生成绑定零静态耦合，vue-tsc/vitest 在合入前即可全绿。
// ============================================================================

/** 条目类别：文本 / 图片 / 文件列表（CF_HDROP）。 */
export type ClipKind = 'text' | 'image' | 'file'

/** 一条剪贴板历史（wire 形态；落盘 Text 为 DPAPI 密由后端处理，前端不见）。 */
export interface ClipEntry {
  id: string
  hash: string
  kind: ClipKind
  /** 明文正文（仅 Get 回填；List/事件不携带）。 */
  text?: string
  /** ≤120 rune 首行摘要，列表页渲染用。 */
  preview: string
  /** CF_HDROP 文件路径列表。 */
  files?: string[]
  /** 图片 blob 相对路径（"blobs/<sha>.png" 语义，展示走 Get）。 */
  blob?: string
  /** image 条目的 64px 内联缩略图 dataURL（v1.7 契约 §12 R-G2；List 即带，前端零额外请求）。 */
  thumb?: string
  width?: number
  height?: number
  byteSize: number
  /** 复制时前台窗口标题。 */
  sourceApp?: string
  /** 复制来源进程名（小写无路径）。 */
  sourceExe?: string
  /** 入库嗅探标签：url/email/phone/color/code/cjk。 */
  autoTags?: string[]
  /** 敏感位：MCP 通道整条不下发（前端可见但仅作警示渲染）。 */
  sensitive?: boolean
  pinned?: boolean
  /** 手建固定片段（不被容量淘汰）。 */
  manual?: boolean
  /** epoch 毫秒。 */
  createdAt: number
  lastUsedAt?: number
  useCount?: number
  /** 仅 Get 回填的 PNG 原始字节（Wails 侧 base64 → 此处 string）。 */
  blobData?: string
}

/** 模块运行状态（GetStatus）。 */
export interface ClipStatus {
  paused: boolean
  entryCount: number
  blobBytes: number
  maxEntries: number
  maxBlobBytes: number
  excludedExes: string[]
}

/** `clipboard:removed` 载荷。 */
export interface ClipRemoved {
  id: string
}

/** `clipboard:paused` 载荷。 */
export interface ClipPaused {
  paused: boolean
}
