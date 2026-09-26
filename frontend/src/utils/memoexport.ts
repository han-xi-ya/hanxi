// 随手记导出面（N16 C 批）：单条 .md 导出 + 全库 .md 汇总，纯前端零新后端。
// 通道选型实证：bindings 里 DialogManager 是空 typedef（无可调用方法）；
// @wailsio/runtime 的 Dialogs.SaveFile 能弹原生"另存"框但只回路径、beta.10
// 无配套写字节流（无 FileWritingHandler），且现面没有任何可写任意路径的
// Go 绑定——"对话框+落盘"闭环必须等新后端签名（见交付报告清单）。本轮走
// 浏览器原生 Blob 下载（Wails examples/web-apis/blob 证实该 Web API 在
// WebView 窗内可用），下载不可用时调用方降级为复制原文。
// 敏感纪律：本模块不感知过滤决策——masked 条目的排除发生在调用方取数后、
// 进入任何 builder 之前；这里保证的是"给进来的都该进文件"。
import type { MemoItem } from '../../bindings/hanxi/internal/modules/memo/models'

/**
 * sanitizeLine 把值压成单行：连续空白折为单空格并去首尾。
 * 与后端 filestore.sanitizeLine（strings.Fields + Join）同语义，
 * 保证导出的 frontmatter 可被后端 DecodeMemo 原样读回。
 */
export function sanitizeLine(s: string): string {
  return s.trim().replace(/\s+/g, ' ')
}

/**
 * buildMemoFile 生成与 <数据根>/memo/<id>.md 同格式的文档（frontmatter +
 * 正文原样）：导出件即"库里的这一个文件"，复制回数据目录可被后端解析。
 * 时间戳直接透传（后端 JSON 已是 RFC3339Nano）；tags 为 JSON 数组字符串，
 * 转义细节与 Go json.Marshal 不逐字相同（Go 把 <>& 转 \uXXXX）但解码等价。
 */
export function buildMemoFile(item: MemoItem): string {
  const lines = [
    '---',
    `id: ${item.id}`,
    `title: ${sanitizeLine(item.title ?? '')}`,
    `tags: ${JSON.stringify(item.tags ?? null)}`,
    `pinned: ${String(item.isPinned === true)}`,
    `masked: ${String(item.isMasked === true)}`,
    `color: ${sanitizeLine(item.colorTag ?? '')}`,
    `created: ${item.createdAt}`,
    `updated: ${item.updatedAt}`,
    '---',
  ]
  return `${lines.join('\n')}\n${item.content ?? ''}`
}

// fmtLocal ISO 串 → 'YYYY-MM-DD HH:mm'（本地时区）；解析失败原样回退。
function fmtLocal(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/**
 * buildLibraryDigest 全库汇总稿：人读向单文件——篇头统计（含"敏感遮罩条目
 * 已排除"的显式声明，导出文件离库后不再有遮罩语义，纸面要说清）+ 每条一节
 * （标题/标签/时间/置顶/正文）。传入 items 必须已由调用方剔除非导出项。
 */
export function buildLibraryDigest(
  items: MemoItem[],
  opts: { exportedAt?: Date; excludedMasked?: number } = {},
): string {
  const at = fmtLocal((opts.exportedAt ?? new Date()).toISOString())
  const excluded = opts.excludedMasked ?? 0
  const head = [
    '# 随手记 · 全库导出',
    '',
    `- 导出时间：${at}`,
    `- 便签条数：${items.length}`,
    `- 敏感遮罩条目：${excluded > 0 ? `${excluded} 条已排除，明文未写入本文件` : '无'}`,
    '',
    '> 本文件为只读汇总稿；需要可回灌的单条文件请用详情浮层的「导出 .md」。',
  ]
  const sections = items.map((item) => {
    const meta = [
      item.tags?.length ? `- 标签：${item.tags.join(' ')}` : '',
      `- 建 ${fmtLocal(item.createdAt)} · 改 ${fmtLocal(item.updatedAt)}${item.isPinned ? ' · 📌 置顶' : ''}`,
    ].filter(Boolean).join('\n')
    return [`## ${sanitizeLine(item.title || '') || '无标题便签'}`, '', meta, '', item.content ?? '（空便签）'].join('\n')
  })
  return [...head, '', '---', '', sections.join('\n\n---\n\n')].join('\n') + '\n'
}

// Windows 文件名禁用字符与控制符；保留设备名清单（CON/PRN/AUX/NUL/COM1-9/LPT1-9）。
const ILLEGAL_FN_CHARS = /[<>:"/\\|?*\x00-\x1f]/g
const RESERVED_NAMES = /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i

/**
 * sanitizeFileName 下载文件名消毒：非法字符/控制符剔除，首尾点/横杠剥离
 * （NTFS 静默截尾），内部空白折 '-'，限长 60，命中保留设备名时加 '·' 破歧义。
 */
export function sanitizeFileName(name: string): string {
  let s = name
    .replace(ILLEGAL_FN_CHARS, '')
    .trim()
    .replace(/\s+/g, '-')
    .replace(/^[.-]+/, '')
    .replace(/[.-]+$/, '')
  if (s.length > 60) s = s.slice(0, 60).replace(/[.-]+$/, '')
  if (RESERVED_NAMES.test(s.split('.')[0])) s = `·${s}`
  return s || '无标题便签'
}

/** safeExportFileName 单条导出名：`标题-id前缀.md`——id 兜底防撞名（无标题条目标题为空）。 */
export function safeExportFileName(item: MemoItem): string {
  const base = sanitizeFileName(item.title || '')
  const tag = sanitizeFileName(item.id).slice(-8)
  return `${base === '无标题便签' ? '便签' : base}-${tag}.md`
}

/** safeLibraryFileName 全库汇总名：`hanxi-随手记导出-YYYYMMDD-HHmmss.md`（本地时区）。 */
export function safeLibraryFileName(at: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `hanxi-随手记导出-${at.getFullYear()}${p(at.getMonth() + 1)}${p(at.getDate())}-${p(at.getHours())}${p(at.getMinutes())}${p(at.getSeconds())}.md`
}

/**
 * downloadTextFile 浏览器原生下载通道：Blob + a[download] 触发"另存"。
 * 返回 false 表示通道不可用（无 createObjectURL 等），由调用方降级复制。
 * objectURL 延迟释放：Chromium 对 blob: 的取流在 click 后异步发生，立即
 * revoke 会掐死下载。
 */
export function downloadTextFile(fileName: string, text: string): boolean {
  try {
    if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return false
    const blob = new Blob([text], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = fileName
    a.rel = 'noopener'
    a.style.display = 'none'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 4000)
    return true
  } catch {
    return false
  }
}
