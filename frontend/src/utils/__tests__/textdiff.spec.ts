// textdiff 表测（N33 §4 验收：空 old、空 new、截断尾行、CRLF 原样逐条钉死）：
// LCS 行对比的正确性、公共前后缀裁剪、超预算块状降级、折叠只藏 ctx、
// 渲染截断与统计口径。纯函数件，无 DOM 依赖。
import { describe, expect, it } from 'vitest'
import { FOLD_KEEP, FOLD_THRESHOLD, MAX_LCS_CELLS, MAX_RENDER_LINES, capRender, diffLines, diffStats, foldContext, type DiffLine } from '../textdiff'

/** 生成 n 行 ctx 行（文本 c0..c[n-1]，可带起点偏移）。 */
function ctxRun(n: number, from = 0): DiffLine[] {
  return Array.from({ length: n }, (_, i) => ({ type: 'ctx' as const, text: `c${from + i}` }))
}
const addLine = (t: string): DiffLine => ({ type: 'add', text: t })
const delLine = (t: string): DiffLine => ({ type: 'del', text: t })
const lineSeq = (n: number, prefix: string) =>
  Array.from({ length: n }, (_, i) => `${prefix}${i}`).join('\n')

describe('diffLines 基础形态', () => {
  it('双侧空串 → 零行', () => {
    expect(diffLines('', '')).toEqual([])
  })

  it('空 old = 全部新增', () => {
    expect(diffLines('', 'a\nb')).toEqual([addLine('a'), addLine('b')])
  })

  it('空 new = 全部删除', () => {
    expect(diffLines('a\nb', '')).toEqual([delLine('a'), delLine('b')])
  })

  it('完全一致 → 全 ctx 且无增删', () => {
    const d = diffLines('a\nb\nc', 'a\nb\nc')
    expect(d.every((l) => l.type === 'ctx')).toBe(true)
    expect(diffStats(d)).toEqual({ added: 0, deleted: 0 })
  })

  it('单行纯新增：插在中部且不动其余行', () => {
    expect(diffLines('a\nc', 'a\nb\nc')).toEqual([
      { type: 'ctx', text: 'a' },
      addLine('b'),
      { type: 'ctx', text: 'c' },
    ])
  })

  it('单行纯删除', () => {
    expect(diffLines('a\nb\nc', 'a\nc')).toEqual([
      { type: 'ctx', text: 'a' },
      delLine('b'),
      { type: 'ctx', text: 'c' },
    ])
  })

  it('修改=删+加且删行在前（GitHub 口径）', () => {
    expect(diffLines('a\nX\nc', 'a\nY\nc')).toEqual([
      { type: 'ctx', text: 'a' },
      delLine('X'),
      addLine('Y'),
      { type: 'ctx', text: 'c' },
    ])
  })

  it('内容无损：删+ctx 侧还原 old，加+ctx 侧还原 new；统计正确', () => {
    const oldT = 'one\ntwo\nthree\nfour\nfive'
    const newT = 'one\n2\nthree\nfour\n5\nsix'
    const d = diffLines(oldT, newT)
    expect(d.filter((l) => l.type !== 'add').map((l) => l.text).join('\n')).toBe(oldT)
    expect(d.filter((l) => l.type !== 'del').map((l) => l.text).join('\n')).toBe(newT)
    // two→2（1 删 1 加）+ five→5、six（1 删 2 加）
    expect(diffStats(d)).toEqual({ added: 3, deleted: 2 })
  })

  it('CRLF 原样：行内 \\r 不吞不显式改写（尾行无 \\r 也不虚构）', () => {
    expect(diffLines('a\r\nb', 'a\r\nc')).toEqual([
      { type: 'ctx', text: 'a\r' },
      delLine('b'),
      addLine('c'),
    ])
    // 完整 CRLF 文本（每行行尾都带 \r）逐行保留
    expect(diffLines('a\r\nb\r\n', 'a\r\nc\r\n').map((l) => `${l.type}:${JSON.stringify(l.text)}`))
      .toEqual(['ctx:"a\\r"', 'del:"b\\r"', 'add:"c\\r"', 'ctx:""'])
  })

  it('截断尾行（无尾换行的半行）照常参与对比，不虚构换行', () => {
    // 后端 512KB 截断常留"半行"尾巴：old 尾行是截断残留，new 完整
    const d = diffLines('a\nb\ntruncated-tail', 'a\nb\nfull-content')
    expect(d.slice(-2)).toEqual([delLine('truncated-tail'), addLine('full-content')])
    expect(d.filter((l) => l.type === 'ctx').map((l) => l.text)).toEqual(['a', 'b'])
  })

  it('公共前后缀大段相同时只解中段（1 万行头尾相同 + 中部 1 行改）', () => {
    const head = lineSeq(10000, 'H')
    const d = diffLines(`${head}\nX\ntail`, `${head}\nY\ntail`)
    expect(diffStats(d)).toEqual({ added: 1, deleted: 1 })
    expect(d.filter((l) => l.type === 'ctx')).toHaveLength(10001)
  })
})

describe('diffLines 超预算块状降级', () => {
  it('中段乘积超 MAX_LCS_CELLS → 整段删+整段加，不做行对齐', () => {
    const n = Math.floor(Math.sqrt(MAX_LCS_CELLS)) + 10 // n·n 必超预算
    const a = lineSeq(n, 'old-')
    const b = lineSeq(n, 'new-')
    const d = diffLines(a, b)
    expect(d).toHaveLength(n * 2)
    expect(d.slice(0, n).every((l) => l.type === 'del')).toBe(true)
    expect(d.slice(n).every((l) => l.type === 'add')).toBe(true)
  })

  it('预算内的大而不同文件仍走 LCS（有公共行则出 ctx）', () => {
    const m = 400 // 400×400 = 160k < 1M
    const a = lineSeq(m, 'A')
    const b = Array.from({ length: m }, (_, i) => (i % 2 === 0 ? `A${i}` : `B${i}`)).join('\n')
    const d = diffLines(a, b)
    expect(d.some((l) => l.type === 'ctx')).toBe(true)
    expect(diffStats(d)).toEqual({ added: m / 2, deleted: m / 2 })
  })
})

describe('foldContext hunk 折叠', () => {
  it('≤ 阈值的连续 ctx 不折叠', () => {
    const lines = [...ctxRun(FOLD_THRESHOLD), addLine('x')]
    expect(foldContext(lines).every((s) => s.kind === 'line')).toBe(true)
  })

  it('> 阈值折叠：首尾各留 FOLD_KEEP 锚点，fold 自带被藏行且计数如实', () => {
    const lines = [...ctxRun(10), addLine('x'), ...ctxRun(20, 10)]
    const segs = foldContext(lines)
    // 10 段 → 3 line + 1 fold(藏 4) + 3 line；20 段 → 3 line + 1 fold(藏 14) + 3 line
    const folds = segs.filter((s) => s.kind === 'fold')
    expect(folds.map((s) => (s.kind === 'fold' ? s.count : 0))).toEqual([10 - 2 * FOLD_KEEP, 20 - 2 * FOLD_KEEP])
    expect(segs[0]).toEqual({ kind: 'line', line: { type: 'ctx', text: 'c0' } })
    expect(segs[FOLD_KEEP]).toMatchObject({ kind: 'fold' })
    expect(segs[FOLD_KEEP + 1]).toEqual({ kind: 'line', line: { type: 'ctx', text: 'c7' } })
  })

  it('默认阈值口径：连续 7 行 ctx（>6）恰好产生一个 fold，藏 1 行', () => {
    const segs = foldContext(ctxRun(FOLD_THRESHOLD + 1))
    expect(segs.filter((s) => s.kind === 'fold')).toHaveLength(1)
    expect(segs.find((s) => s.kind === 'fold')).toMatchObject({ count: 1 })
    expect(segs).toHaveLength(2 * FOLD_KEEP + 1)
  })

  it('折叠只藏未变行：fold 展开后行数与内容零回退，增删行永不入 fold', () => {
    const lines = [...ctxRun(20), delLine('d'), ...ctxRun(30, 20)]
    const segs = foldContext(lines)
    const restored = segs.flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines))
    expect(restored).toEqual(lines)
    for (const s of segs) {
      if (s.kind === 'fold') expect(s.lines.every((l) => l.type === 'ctx')).toBe(true)
    }
  })

  it('全增删行输入 → 无 fold', () => {
    const segs = foldContext([addLine('a'), delLine('b'), addLine('c')])
    expect(segs.every((s) => s.kind === 'line')).toBe(true)
  })
})

describe('capRender 大文件渲染保护', () => {
  it('不超上限原样返回', () => {
    const lines = ctxRun(5)
    expect(capRender(lines)).toEqual({ visible: lines, hidden: 0 })
  })

  it('超上限截前段并报 hidden（§4 大文件保护：>2000 行）', () => {
    const lines = ctxRun(MAX_RENDER_LINES + 37)
    const r = capRender(lines)
    expect(r.visible).toHaveLength(MAX_RENDER_LINES)
    expect(r.hidden).toBe(37)
  })
})
