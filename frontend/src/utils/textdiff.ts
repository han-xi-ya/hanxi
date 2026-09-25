// 行级文本 diff 公共件（N33 §4 / 批 B）：后端 DiffFile 只回 old/new 原样文本
// （≤512KB 各截断），行对比在前端算——git 模式后端已有 show 双读，diff 属观感
// 计算，不进 RPC 超时预算，也不给双引擎各写一份。零第三方依赖。
//
// 算法选型：经典 LCS（最长公共子序列）动态规划，O(n·m) 时间/空间，输出统一式
// （unified）行序列 ctx/add/del，同 GitHub 单栏口径。前置做公共前后缀裁剪
// （多数快照只改局部，裁剪后中段常常为空，LCS 只兜住真正的变化区）。
// 超过 MAX_LCS_CELLS 预算时降级为「整段删 + 整段加」的块状呈现：如实表达
// 「这些行变了」，只是不做行对齐——宁缺德对齐，不假装精确（防 LCS 卡顿）。

export type DiffLineType = 'add' | 'del' | 'ctx'

export interface DiffLine {
  type: DiffLineType
  text: string
}

/** diff 折叠分段：line 直出；fold 是可展开的连续未变段（纯呈现层，不藏增删行）。 */
export type DiffSegment =
  | { kind: 'line'; line: DiffLine }
  | { kind: 'fold'; count: number; lines: DiffLine[] }

/** LCS DP 单元预算：约 1M 格（Uint32 上限 4MB），超走块状降级（见文件头）。导出供测试钉口径。 */
export const MAX_LCS_CELLS = 1_000_000

/** 连续未变 ctx 行 > 该值时折叠中段（保留首尾各 FOLD_KEEP 行做上下文锚点）。 */
export const FOLD_THRESHOLD = 6

/** 折叠段前后各保留的 ctx 锚点行数。 */
export const FOLD_KEEP = 3

/** 单次渲染行数上限：超过只出前 N 行 + 「展开完整对比」（§4 大文件保护）。 */
export const MAX_RENDER_LINES = 2000

/** 按行切分：空串视为无行；CRLF 原样保留在行内（不悄悄改写用户文本）。 */
function splitLines(text: string): string[] {
  if (text === '') return []
  return text.split('\n')
}

/**
 * old → new 的行级 diff（统一式单栏序）。
 * 约定：修改区先列删行后列加行（GitHub 口径）；两侧全等时全 ctx。
 */
export function diffLines(oldText: string, newText: string): DiffLine[] {
  const a = splitLines(oldText)
  const b = splitLines(newText)
  const out: DiffLine[] = []

  // 公共前缀
  let p = 0
  while (p < a.length && p < b.length && a[p] === b[p]) p++
  // 公共后缀（不与前缀重叠）
  let s = 0
  while (s < a.length - p && s < b.length - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++

  for (let i = 0; i < p; i++) out.push({ type: 'ctx', text: a[i] })
  out.push(...diffMiddle(a.slice(p, a.length - s), b.slice(p, b.length - s)))
  for (let i = a.length - s; i < a.length; i++) out.push({ type: 'ctx', text: a[i] })
  return out
}

/** 裁剪后的中段求解：单侧空直出；规模在预算内走 LCS，超预算块状降级。 */
function diffMiddle(a: string[], b: string[]): DiffLine[] {
  if (!a.length && !b.length) return []
  if (!a.length) return b.map((text) => ({ type: 'add' as const, text }))
  if (!b.length) return a.map((text) => ({ type: 'del' as const, text }))
  if (a.length * b.length > MAX_LCS_CELLS) {
    return [
      ...a.map((text) => ({ type: 'del' as const, text })),
      ...b.map((text) => ({ type: 'add' as const, text })),
    ]
  }

  const n = a.length
  const m = b.length
  const w = m + 1
  // dp[i*w+j] = a[i..] 与 b[j..] 的 LCS 长度（后缀式填表，回溯单向扫过）
  const dp = new Uint32Array((n + 1) * w)
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i * w + j] =
        a[i] === b[j]
          ? dp[(i + 1) * w + j + 1] + 1
          : Math.max(dp[(i + 1) * w + j], dp[i * w + j + 1])
    }
  }

  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ type: 'ctx', text: a[i] })
      i++
      j++
    } else if (dp[(i + 1) * w + j] >= dp[i * w + j + 1]) {
      out.push({ type: 'del', text: a[i] })
      i++
    } else {
      out.push({ type: 'add', text: b[j] })
      j++
    }
  }
  while (i < n) out.push({ type: 'del', text: a[i++] })
  while (j < m) out.push({ type: 'add', text: b[j++] })
  return out
}

/** 行列出统计（「+3 −1 行」摘要用）。 */
export function diffStats(lines: DiffLine[]): { added: number; deleted: number } {
  let added = 0
  let deleted = 0
  for (const l of lines) {
    if (l.type === 'add') added++
    else if (l.type === 'del') deleted++
  }
  return { added, deleted }
}

/**
 * 折叠呈现：连续 ctx 行 > threshold 时，保留首尾各 keep 行、中段并为 fold 段
 * （fold 自带被藏行，展开即原样回插——只藏未变行，绝不吞增删行）。
 */
export function foldContext(lines: DiffLine[], threshold = FOLD_THRESHOLD, keep = FOLD_KEEP): DiffSegment[] {
  const out: DiffSegment[] = []
  let i = 0
  while (i < lines.length) {
    if (lines[i].type !== 'ctx') {
      out.push({ kind: 'line', line: lines[i] })
      i++
      continue
    }
    let j = i
    while (j < lines.length && lines[j].type === 'ctx') j++
    const runLen = j - i
    if (runLen <= threshold) {
      for (let k = i; k < j; k++) out.push({ kind: 'line', line: lines[k] })
    } else {
      for (let k = 0; k < keep; k++) out.push({ kind: 'line', line: lines[i + k] })
      const hidden = lines.slice(i + keep, j - keep)
      out.push({ kind: 'fold', count: hidden.length, lines: hidden })
      for (let k = j - keep; k < j; k++) out.push({ kind: 'line', line: lines[k] })
    }
    i = j
  }
  return out
}

/** 大文件渲染截断：超上限只留前 max 行，返回被藏行数供「展开完整对比」。 */
export function capRender(lines: DiffLine[], max = MAX_RENDER_LINES): { visible: DiffLine[]; hidden: number } {
  if (lines.length <= max) return { visible: lines, hidden: 0 }
  return { visible: lines.slice(0, max), hidden: lines.length - max }
}
