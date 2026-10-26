// ============================================================================
// 剪贴板内置模块 · 前端类型层（多 agent 并行 kernel，所有实现 agent 只读）
//
// 与 docs/plans/2026-09-26-clipboard-contract.md §3 的 Go Entry/Status 逐字
// 对位（JSON tag = 此处字段名）。真实接线经 @/adapters/clipboard 的 transport
// 注入完成——本文件与生成绑定零静态耦合，vue-tsc/vitest 在合入前即可全绿。
// ============================================================================

/** 条目类别：文本 / 图片 / 文件列表（CF_HDROP）。 */
export type ClipKind = 'text' | 'image' | 'file'

/** 一条剪贴板历史（wire 形态；落盘 Text 为明文——契约 §12 v1.6 机主裁决取消
 *  DPAPI，明文只随本机数据根携带——List/事件不带正文，前端明文仅 Get 回填可见）。 */
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
  /** 顺序粘贴队列余数（v1.8）。 */
  queueLen: number
}

/** `clipboard:queue:changed` 载荷：队列 id 全量快照（数组序=贴出序）。 */
export interface ClipQueueChanged {
  ids: string[]
}

/** 队列热键实况（Get/SetQueueHotkey；registered 为 Registry 在位实况，
 *  accel ""=停用）。 */
export interface ClipQueueHotkeyState {
  accel: string
  registered: boolean
}

/** `clipboard:removed` 载荷。 */
export interface ClipRemoved {
  id: string
}

/** `clipboard:paused` 载荷。 */
export interface ClipPaused {
  paused: boolean
}

/**
 * 用户偏好（GetPrefs/SetPrefs 往返面；契约 §12 v1.8）。
 * 整表替换语义：SetPrefs 必须发全量——漏发域=回落默认，前端本地持全表，
 * 改一项发全表。maxEntries/maxBlobMB 回显的是**钳制后的生效值**。
 * queueHotkey 只读回显（SetPrefs 忽略该字段保留旧值，改键只走 SetQueueHotkey）。
 */
export interface ClipPrefs {
  maxEntries: number
  maxBlobMB: number
  deleteAfterPaste: boolean
  /** 出厂排除表（只读，结构上删不掉）。 */
  excludedFactory: string[]
  /** 用户追加排除项（小写进程名，后端已补 .exe 去重）。 */
  excludedCustom: string[]
  /** 按应用保留天数覆盖表：exe（小写）→ 天数；不在表内的应用不做按日清扫。 */
  appRetentionDays: Record<string, number>
  queueHotkey: string
  /** 预览遮蔽内置类（phone/idcard/email/secret；显式空表=全关）。 */
  maskKinds: string[]
  /** 自定义遮蔽正则（RE2；坏正则整表拒绝保存）。 */
  maskCustomPatterns: string[]
}
