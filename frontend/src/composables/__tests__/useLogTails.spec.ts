// useLogTails 外部事实断言：ANSI 剥离结果、贴底阈值 39/40/41 边界、满仓裁头保尾不保头、
// 着色词表映射（frpc 单正表 / ddns-go 正表+负表豁免）、自动滚动闩的读写与落底行为。
// 机构事实与三处视图既有 spec（FrpcProjectsView/DdnsGoView/LogsView）互证：公共件改的
// 是出处，不是呈现——视图 spec 零改动全绿即"收编等价"的另一半证明。
import { describe, expect, it } from 'vitest'
import {
  createWarnMatcher,
  DDNS_LOG_WARN_EXCLUDE_RE,
  DDNS_LOG_WARN_RE,
  FRPC_LOG_WARN_RE,
  isNearBottom,
  keepLastLines,
  LOG_BOTTOM_THRESHOLD,
  pinToBottom,
  stripAnsi,
  trimToCap,
  useLogTails,
} from '../useLogTails'

describe('stripAnsi：剥离结果断外部事实', () => {
  it('SGR 色码全剥离，可见文本逐字保留', () => {
    expect(stripAnsi('\x1b[32m[I] frps connected\x1b[0m')).toBe('[I] frps connected')
    expect(stripAnsi('\x1b[1;34m多参数序\x1b[0m')).toBe('多参数序')
    expect(stripAnsi('\x1b[m无参序列\x1b[0m')).toBe('无参序列')
  })

  it('行中段多段色码同样剥离', () => {
    expect(stripAnsi('前\x1b[31m中\x1b[0m后')).toBe('前中后')
  })

  it('非 SGR 序列与裸文本不误伤', () => {
    expect(stripAnsi('plain text 无码')).toBe('plain text 无码')
    expect(stripAnsi('[32m 缺 ESC 前缀不动')).toBe('[32m 缺 ESC 前缀不动')
    expect(stripAnsi('\x1b[2K清行码不属 SGR，保留')).toBe('\x1b[2K清行码不属 SGR，保留')
    expect(stripAnsi('')).toBe('')
  })
})

describe('isNearBottom：贴底阈值边界（39/40/41）', () => {
  // scrollHeight=1000、clientHeight=100：可视区底缘贴文档底缘时 scrollTop=900，
  // 距底 = scrollHeight - clientHeight - scrollTop。
  const metrics = (scrollTop: number) => ({ scrollTop, clientHeight: 100, scrollHeight: 1000 })

  it('默认阈值为 40（两处视图逐字内联表达式的档位单源）', () => {
    expect(LOG_BOTTOM_THRESHOLD).toBe(40)
  })

  it('距底 41px 失咬（阈外），40px 恰等含边界为真，39px 真', () => {
    expect(isNearBottom(metrics(859))).toBe(false) // 859+100=959 < 960
    expect(isNearBottom(metrics(860))).toBe(true) // 恰等 scrollHeight-40，`>=` 含边界
    expect(isNearBottom(metrics(861))).toBe(true) // 距底 39
  })

  it('阈值可覆写：按给定档判边界', () => {
    expect(isNearBottom(metrics(889), 10)).toBe(false) // 距底 11
    expect(isNearBottom(metrics(890), 10)).toBe(true) // 距底 10
    expect(isNearBottom(metrics(860), 10)).toBe(false) // 默认档真、覆写档假
  })
})

describe('pinToBottom：无条件落底（LogsView 形制）', () => {
  it('scrollTop 直接写至 scrollHeight，不问当前位置', () => {
    const el = { scrollTop: 0, scrollHeight: 500 }
    pinToBottom(el)
    expect(el.scrollTop).toBe(500)
    const el2 = { scrollTop: 123, scrollHeight: 500 }
    pinToBottom(el2)
    expect(el2.scrollTop).toBe(500)
  })
})

describe('满仓裁头：保尾不保头', () => {
  it('trimToCap 原地裁头并返回同一引用（ddns-go ref 原位变异依赖此契约）', () => {
    const arr = [1, 2, 3, 4, 5]
    const out = trimToCap(arr, 3)
    expect(out).toBe(arr)
    expect(arr).toEqual([3, 4, 5]) // 丢最旧、留最近
    expect(out).toEqual([3, 4, 5])
  })

  it('trimToCap 未超容不动数组（含恰等边界）', () => {
    const a = [1, 2]
    expect(trimToCap(a, 5)).toEqual([1, 2])
    const b = [1, 2, 3]
    expect(trimToCap(b, 3)).toEqual([1, 2, 3])
  })

  it('keepLastLines 切尾且不改源（wire 数组与视图缓冲不别名互污）', () => {
    const src = ['a', 'b', 'c', 'd', 'e']
    const out = keepLastLines(src, 3)
    expect(out).toEqual(['c', 'd', 'e'])
    expect(src).toEqual(['a', 'b', 'c', 'd', 'e'])
    expect(out).not.toBe(src)
  })

  it('keepLastLines 短于上限时返回等值新副本', () => {
    const src = [1, 2]
    const out = keepLastLines(src, 400)
    expect(out).toEqual([1, 2])
    expect(out).not.toBe(src)
  })
})

describe('createWarnMatcher：着色词表映射（分叉保留为参数）', () => {
  const frpcWarn = createWarnMatcher(FRPC_LOG_WARN_RE)
  const ddnsWarn = createWarnMatcher(DDNS_LOG_WARN_RE, DDNS_LOG_WARN_EXCLUDE_RE)

  it('frpc 单正表：[W]/WARN/error/fail 命中，大小写不敏感，[I] 正常行不涂', () => {
    expect(frpcWarn('[W] token warning')).toBe(true)
    expect(frpcWarn('WARN low')).toBe(true)
    expect(frpcWarn('[E] login fail: error')).toBe(true)
    expect(frpcWarn('[I] frps connected')).toBe(false)
    expect(frpcWarn('')).toBe(false)
  })

  it('ddns-go 中英成败词命中；负表豁免压过正表（error 行内 no change 不算异常）', () => {
    expect(ddnsWarn('更新失败: AliDNS refused')).toBe(true)
    expect(ddnsWarn('timeout')).toBe(true)
    expect(ddnsWarn('ERROR')).toBe(true)
    expect(ddnsWarn('新 IP: 1.2.3.4')).toBe(false)
    // 原实现首条历史行夹具 'IP 未变化' 与事件行 'IP 未变化，跳过更新' 均不涂
    expect(ddnsWarn('IP 未变化，跳过更新')).toBe(false)
    // 正反表同命中时负表豁免（断豁免真的在场而非恰好两表互斥）
    expect(ddnsWarn('error: no change')).toBe(false)
    expect(ddnsWarn('同步错误（未变化兜底）')).toBe(false)
  })

  it('两视图词表分叉逐字锚定（收编改出处不改词面）', () => {
    expect(FRPC_LOG_WARN_RE.source).toBe('\\[W\\]|WARN|error|fail')
    expect(FRPC_LOG_WARN_RE.flags).toBe('i')
    expect(DDNS_LOG_WARN_RE.source).toBe('失败|错误|异常|error|fail|refused|timeout')
    expect(DDNS_LOG_WARN_EXCLUDE_RE.source).toBe('未变化|no change')
  })
})

describe('useLogTails：闩/容器/裁头装配', () => {
  it('默认闩开、容器空', () => {
    const tails = useLogTails({ maxLines: 10 })
    expect(tails.autoScroll.value).toBe(true)
    expect(tails.bodyRef.value).toBeNull()
  })

  it('onBodyScroll 按阈值回写闩：上翻失咬、回底复咬', () => {
    const tails = useLogTails({ maxLines: 10 })
    const scrollEvent = (scrollTop: number) =>
      ({ target: { scrollTop, clientHeight: 100, scrollHeight: 1000 } }) as unknown as Event
    tails.onBodyScroll(scrollEvent(859)) // 距底 41
    expect(tails.autoScroll.value).toBe(false)
    tails.onBodyScroll(scrollEvent(860)) // 距底恰 40，含边界
    expect(tails.autoScroll.value).toBe(true)
    tails.onBodyScroll(scrollEvent(500))
    expect(tails.autoScroll.value).toBe(false)
  })

  it('自定义阈值档生效', () => {
    const tails = useLogTails({ maxLines: 10, bottomThreshold: 10 })
    tails.onBodyScroll({ target: { scrollTop: 889, clientHeight: 100, scrollHeight: 1000 } } as unknown as Event)
    expect(tails.autoScroll.value).toBe(false)
    tails.onBodyScroll({ target: { scrollTop: 890, clientHeight: 100, scrollHeight: 1000 } } as unknown as Event)
    expect(tails.autoScroll.value).toBe(true)
  })

  it('scrollToBottom 闩开时 nextTick 后落底；闩关保持原位；容器未挂载不炸', async () => {
    const tails = useLogTails({ maxLines: 10 })
    const el = { scrollTop: 42, clientHeight: 100, scrollHeight: 500 }
    tails.bodyRef.value = el as unknown as HTMLElement
    await tails.scrollToBottom()
    expect(el.scrollTop).toBe(500)

    tails.autoScroll.value = false
    el.scrollTop = 77
    await tails.scrollToBottom()
    expect(el.scrollTop).toBe(77)

    tails.bodyRef.value = null
    await expect(tails.scrollToBottom()).resolves.toBeUndefined()
  })

  it('bound trimHead/keepLast 吃本实例 maxLines，两形制结果同为保尾', () => {
    const tails = useLogTails({ maxLines: 3 })
    const arr = [1, 2, 3, 4, 5]
    expect(tails.trimHead(arr)).toEqual([3, 4, 5])
    expect(arr).toEqual([3, 4, 5]) // 原地
    const src = ['a', 'b', 'c', 'd', 'e']
    expect(tails.keepLast(src)).toEqual(['c', 'd', 'e'])
    expect(src).toEqual(['a', 'b', 'c', 'd', 'e']) // 不改源
  })

  it('各实例独立：闩与容器互不串写', async () => {
    const a = useLogTails({ maxLines: 10 })
    const b = useLogTails({ maxLines: 10 })
    a.autoScroll.value = false
    expect(b.autoScroll.value).toBe(true)
    a.bodyRef.value = { scrollTop: 0, clientHeight: 0, scrollHeight: 999 } as unknown as HTMLElement
    await b.scrollToBottom()
    expect((a.bodyRef.value as unknown as { scrollTop: number }).scrollTop).toBe(0)
  })
})
