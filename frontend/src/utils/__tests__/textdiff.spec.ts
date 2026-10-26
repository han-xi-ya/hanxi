// textdiff 表测（空 old、空 new、截断尾行、CRLF 原样逐条钉死）：
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

describe('diffLines 边界输入无损不变式（纯增/纯删/单行/首尾即差异/尾换行）', () => {
  // 外部事实而非实现镜像：删+ctx 侧拼回逐字等于 oldText、加+ctx 侧拼回逐字等于
  // newText（含尾换行这种"差一个空行"的形态）、stats 与实际行数自洽、
  // 折叠展开流逐行全等于 diff 流。边界集全部来自快照真实可能形态。
  const pairs: [string, string][] = [
    ['a\nb', 'a\nb\nc\nd'], // 纯增：尾部追加
    ['b', 'a\nb'], // 纯增：头部插入（首行即差异）
    ['a\nb\nc\nd', 'a\nb'], // 纯删：尾部砍两行
    ['a\nb\nc\nd', 'b\nc\nd'], // 纯删：头行即差异
    ['only', 'other'], // 单行整改：无公共前缀也无公共后缀
    ['X\na\nY', 'P\na\nQ'], // 首尾行都是差异，只有中间一行公共
    ['a', 'a\n'], // 只多一个尾换行（new 尾行是空行）
    ['a\n', 'a'], // 只少一个尾换行
    ['x', ''], // 删空成零行文件
    ['', 'x'], // 从空行长出一行
  ]
  it('十档边界输入的还原不变式逐一对上', () => {
    for (const [oldT, newT] of pairs) {
      const tag = `${JSON.stringify(oldT)}→${JSON.stringify(newT)}`
      const d = diffLines(oldT, newT)
      expect(d.filter((l) => l.type !== 'add').map((l) => l.text).join('\n'), tag).toBe(oldT)
      expect(d.filter((l) => l.type !== 'del').map((l) => l.text).join('\n'), tag).toBe(newT)
      const st = diffStats(d)
      expect(st.added, tag).toBe(d.filter((l) => l.type === 'add').length)
      expect(st.deleted, tag).toBe(d.filter((l) => l.type === 'del').length)
      expect(foldContext(d).flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines)), tag).toEqual(d)
    }
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

  it('参数护栏：threshold < 2·keep 的自定义档不折锚点盖不住的段——无重复行、无空 fold', () => {
    // foldContext(run, 2, 3)：段长 3~6 ≤ 首尾锚点合计 2·keep，若无护栏按「首 keep
    // 行 + fold + 尾 keep 行」展开会把同行渲染两遍并挂出一个藏 0 行的空 fold；
    // 有护栏则整段直出。断言外部事实：fold 展开流逐行全等于输入（不重不丢）。
    for (const n of [3, 4, 6]) {
      const input = ctxRun(n)
      const segs = foldContext(input, 2, 3)
      expect(segs.some((s) => s.kind === 'fold')).toBe(false)
      expect(segs.flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines))).toEqual(input)
    }
    // 默认参数口径零变化：7 行仍恰好一个 fold 藏 1 行（上方既有例逐字锁定）
    const segs7 = foldContext(ctxRun(7), 2, 3)
    expect(segs7.filter((s) => s.kind === 'fold')).toHaveLength(1)
    expect(segs7.flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines))).toEqual(ctxRun(7))
  })

  it('护栏临界档：段长恰 2·keep 直出，2·keep+1 是最短可折段（藏 1 行）', () => {
    // threshold=1 keep=4：锚点合计 8 行。段长 4~8 全部 ≤ 2·keep 直出（若无护栏，
    // 4 行段会被折成「首4锚+藏0+尾4锚」的同段重复）；9 行起才有 fold 且只藏 1 行。
    for (const n of [2, 4, 8]) {
      const input = ctxRun(n)
      const segs = foldContext(input, 1, 4)
      expect(segs.some((s) => s.kind === 'fold')).toBe(false)
      expect(segs).toHaveLength(n)
    }
    const segs9 = foldContext(ctxRun(9), 1, 4)
    expect(segs9.filter((s) => s.kind === 'fold')).toHaveLength(1)
    expect(segs9.find((s) => s.kind === 'fold')).toMatchObject({ count: 1 })
    expect(segs9).toHaveLength(2 * 4 + 1)
    expect(segs9.flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines))).toEqual(ctxRun(9))
  })

  it('keep=0 极端档：无锚点整段全折进行 fold，展开流仍逐行全等于输入', () => {
    // 2·keep=0，护栏对任何正长度段放行折叠；锚点为零意味着 fold 必须装下全部行，
    // 任何「line 与 fold 内容重叠」或「丢行」都会在此断掉——断的是还原结果。
    const lines = [...ctxRun(10), delLine('d'), ...ctxRun(5, 10)]
    const segs = foldContext(lines, 4, 0)
    const folds = segs.filter((s) => s.kind === 'fold')
    expect(folds).toHaveLength(2)
    expect(folds.map((s) => (s.kind === 'fold' ? s.count : 0))).toEqual([10, 5])
    expect(segs.filter((s) => s.kind === 'line')).toHaveLength(1) // 只剩那条 del
    expect(segs.flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines))).toEqual(lines)
  })

  it('真实 diff 流端到端：大文件多 hunk 折叠展开无损，fold 段内零增删行', () => {
    // 喂 diffLines 的真实产物（非手搓 ctx 段）：头部整改、中部改一行、尾部追加，
    // 公共段跨 fold 边界。外部事实=①展开流逐行等于 diff 流 ②fold 只含 ctx ③
    // line 段 + fold 藏行合计与行数守恒。
    // 702×702 ≈ 49 万格，稳在 1M LCS 预算内（超预算会块状降级、无 ctx 可折）
    const body = lineSeq(700, 'L')
    const d = diffLines(`HEAD1\n${body}\nTAIL`, `HEAD2\n${body.replace('L500', 'X500')}\nTAIL\nEXTRA`)
    const segs = foldContext(d)
    expect(segs.some((s) => s.kind === 'fold')).toBe(true)
    expect(segs.flatMap((s) => (s.kind === 'line' ? [s.line] : s.lines))).toEqual(d)
    for (const s of segs) {
      if (s.kind === 'fold') {
        expect(s.count).toBe(s.lines.length)
        expect(s.lines.every((l) => l.type === 'ctx')).toBe(true)
      }
    }
    expect(diffStats(d)).toEqual({ added: 3, deleted: 2 })
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
