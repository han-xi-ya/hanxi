// 特征测试：JsonFmtView（JSON 美化工作台）——粘贴防抖自动 Beautify、入参逐字合契约、
// 空输入不发 RPC、统计行原样用后端字段（前端不得二次计算）、失败横幅行/列/snippet 上屏
// 且输入不清空、开关切换即重发、warnings 逐条可见、防抖定时器卸载清理。
// 后端 jsonfmt 绑定尚未落盘：vitest.config 的 missingBindingSeam 为该导入解析空壳，
// 下方 vi.mock 覆盖空壳（dbx/gonavi spec 同法）。fake timers 下禁 flushPromises
// （微任务不挂在定时器上），排干 await 一律走 flushMicrotasks（PortScanView.spec 同姿势）。
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import JsonFmtView from '../JsonFmtView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  Beautify: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/jsonfmt', () => ({ JsonFmtService: svc }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

// 带日志前缀的原文样例：后端返回什么前端就说什么，本测试只验证"上屏口径"
const LOG_TEXT = '2026-10-26 12:00:01 INFO handle request\n{"a":1,"b":[1,2]}\n2026-10-26 12:00:02 INFO done'

function okResult(patch: Record<string, unknown> = {}) {
  return {
    ok: true,
    output: '{\n  "a": 1,\n  "b": [1, 2]\n}',
    error: '',
    line: 0,
    col: 0,
    snippet: '',
    droppedPrefixLines: 3,
    unwrapped: 2,
    kind: 'object',
    stats: { keys: 12, nodes: 40, maxDepth: 5, bytesIn: 300, bytesOut: 420 },
    warnings: [] as string[],
    ...patch,
  }
}

function errResult(patch: Record<string, unknown> = {}) {
  return {
    ok: false,
    output: '',
    error: 'JSON 语法错误：缺少结束花括号',
    line: 7,
    col: 12,
    snippet: '…{"a":1,"b":[1,2…',
    droppedPrefixLines: 1,
    unwrapped: 0,
    kind: '',
    stats: { keys: 0, nodes: 0, maxDepth: 0, bytesIn: 40, bytesOut: 0 },
    warnings: [] as string[],
    ...patch,
  }
}

function mountView() {
  return mount(JsonFmtView, { attachTo: document.body })
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('JsonFmtView 粘贴即出结果', () => {
  it('输入变化 300ms 防抖自动 Beautify，入参逐字等于契约（含默认开关）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    expect(svc.Beautify).not.toHaveBeenCalled() // 挂载空输入：不发 RPC
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(299)
    expect(svc.Beautify).not.toHaveBeenCalled() // 防抖未到期不触发
    await vi.advanceTimersByTimeAsync(1)
    expect(svc.Beautify).toHaveBeenCalledTimes(1)
    // 精确对象断言（非 objectContaining）：字段名与默认值 indent=2/sortKeys=false/unescape=true 逐字锁死
    expect(svc.Beautify).toHaveBeenCalledWith({
      text: LOG_TEXT,
      indent: 2,
      sortKeys: false,
      unescape: true,
      query: '',
      // 多段抽取恒开（非开关）：一次粘来的终端输出常含多段 JSON，
      // 要求用户手工注释掉别的段是设计失败
      extractAll: true,
    })
    w.unmount()
  })

  it('空输入与纯空白都不发 RPC，结果区显示 .empty-hint', async () => {
    vi.useFakeTimers()
    const w = mountView()
    await flushMicrotasks()
    expect(svc.Beautify).not.toHaveBeenCalled()
    expect(w.find('.empty-hint').exists()).toBe(true)
    await w.findAll('textarea')[0].setValue('   \n  ')
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).not.toHaveBeenCalled()
    expect(w.find('.empty-hint').exists()).toBe(true)
    w.unmount()
  })

  it('手动「美化」立即发请求并吃掉待发防抖（不双发）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT) // 防抖已排队
    await w.find('.jfmt-run-btn').trigger('click')
    await flushMicrotasks()
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 点击即刻发，不待 300ms
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 待发防抖已被点击吃掉
    w.unmount()
  })

  it('卸载后防抖定时器不再触发 RPC', async () => {
    vi.useFakeTimers()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    w.unmount()
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).not.toHaveBeenCalled()
  })
})

describe('JsonFmtView 结果如实上屏', () => {
  it('统计行原样展示后端字段：droppedPrefixLines=3 显示「3 行」，输出只读区回填 output', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const meta = w.find('.jfmt-meta').text()
    // 以下数字全部来自 mock（后端）返回值：3/2/12/40/5/300/420，前端不得二次计算
    expect(meta).toContain('3 行日志前缀')
    expect(meta).toContain('2 层转义')
    expect(meta).toContain('12')
    expect(meta).toContain('节点 40')
    expect(meta).toContain('深度 5')
    expect(meta).toContain('300')
    expect(meta).toContain('420')
    expect(meta).toContain('对象') // kind 走中文标签；后端原值由 mock 侧保证
    // 输出区 = UiClipboardField 只读态，内容原样为后端 output
    const out = w.findAll('textarea')[1]
    expect(out.attributes('readonly')).toBeDefined()
    expect(out.element.value).toBe('{\n  "a": 1,\n  "b": [1, 2]\n}')
    w.unmount()
  })

  it('ok=false：error/第 line 行 第 col 列/snippet 上屏，原始输入不被清空', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(errResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const banner = w.find('.banner.banner-error')
    expect(banner.text()).toContain('JSON 语法错误：缺少结束花括号')
    expect(banner.text()).toContain('第 7 行')
    expect(banner.text()).toContain('第 12 列')
    expect(banner.text()).toContain('…{"a":1,"b":[1,2…')
    expect(w.findAll('textarea')[0].element.value).toBe(LOG_TEXT) // 输入原样保留
    w.unmount()
  })

  it('warnings 非空逐条可见，一条不吞', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(
      okResult({ warnings: ['跳过 2 行无法识别的噪声内容', '输入含 BOM，已剔除'] }),
    )
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const warns = w.findAll('.banner.banner-warn')
    expect(warns).toHaveLength(2)
    expect(warns[0].text()).toBe('跳过 2 行无法识别的噪声内容')
    expect(warns[1].text()).toBe('输入含 BOM，已剔除')
    w.unmount()
  })

  it('切缩进开关立即重发请求（不待防抖），且不留第二次', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT) // 300ms 防抖在排队
    await w.find('select').setValue('4')
    await flushMicrotasks()
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 开关变更即刻发，没等定时器
    expect(svc.Beautify).toHaveBeenCalledWith(expect.objectContaining({ indent: 4, text: LOG_TEXT }))
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 待发的输入防抖已被重算吃掉
    w.unmount()
  })
  it('多段：逐段呈现并带原文行号，「复制全部」用顶层 output', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({
      ...okResult(),
      kind: 'multi',
      output: '{"a":1}\n\n[1,2]',
      segments: [
        { index: 1, line: 2, kind: 'object', output: '{"a":1}', unwrapped: 0, stats: { keys: 1, nodes: 2, maxDepth: 1, bytesIn: 0, bytesOut: 7, linesOut: 1 } },
        { index: 2, line: 7, kind: 'array', output: '[1,2]', unwrapped: 0, stats: { keys: 0, nodes: 2, maxDepth: 1, bytesIn: 0, bytesOut: 5, linesOut: 1 } },
      ],
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    const segs = w.findAll('.jfmt-seg')
    expect(segs).toHaveLength(2)
    // 行号是用户回对日志的唯一抓手，必须逐段在
    expect(segs[0].text()).toContain('第 1 段')
    expect(segs[0].text()).toContain('原第 2 行')
    expect(segs[1].text()).toContain('原第 7 行')
    expect(segs[1].text()).toContain('数组')
    expect(w.text()).toContain('共 2 段')
    // 每段有自己的只读输出区（textarea 数 = 1 输入 + 2 段）
    expect(w.findAll('textarea')).toHaveLength(3)
    w.unmount()
  })
  it('自动修复无需勾选：入参不带 repairTail，修复处数与明细都上屏', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({
      ...okResult(),
      repairs: [{ kind: '单引号改双引号', count: 1 }, { kind: '无引号的键补上引号', count: 2 }],
      repaired: 3,
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue("{'a':1,b:2,c:[3")
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    // 修复恒自动（前提是原样读不了），所以入参里压根没有这个字段了。
    // 断言"不该有的字段"而不是整个键清单：后者只是在抄实现，加个字段就红。
    const req = svc.Beautify.mock.calls[0][0]
    expect(req.repairTail).toBeUndefined()
    expect(req.extractAll).toBe(true)
    const chip = w.find('.chip-warning')
    expect(chip.text()).toContain('已自动修复 6 处')
    expect(w.find('.jfmt-repair-detail').text()).toContain('单引号改双引号 1 处')
    expect(w.find('.jfmt-repair-detail').text()).toContain('补了 3 个闭合符')
    w.unmount()
  })
  it('查找词进请求并渲染命中：路径可复制、多段命中带段号与行号', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({
      ...okResult(),
      matches: [
        { path: 'data.items[].traceId', value: 'abc-123', kind: 'string', segment: 2, line: 7 },
      ],
      matchTruncated: false,
    })
    const w = mountView()
    await flushMicrotasks()
    // 查找/统计要"有结果之后才在"（没解析出东西时没东西可查）——先让粘贴跑出一版
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    await w.find('.jfmt-find-input').setValue('traceid')
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const last = svc.Beautify.mock.calls.at(-1)![0]
    expect(last.query).toBe('traceid')
    const hits = w.findAll('.jfmt-find tbody tr')
    expect(hits).toHaveLength(1)
    expect(hits[0].text()).toContain('data.items[].traceId')
    expect(hits[0].text()).toContain('abc-123')
    expect(hits[0].text()).toContain('第2段 第7行')
    expect(w.text()).toContain('命中')
    w.unmount()
  })

  it('键路径统计：表头与次数/类型都来自后端字段，前端不自己数', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({
      ...okResult(),
      keyStats: [
        { path: 'data.items[]', count: 12, types: [{ type: 'object', count: 12 }] },
        { path: 'data.items[].id', count: 12, types: [{ type: 'number', count: 12 }] },
      ],
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const tables = w.findAll('.jfmt-find table')
    const statsRows = tables[tables.length - 1].findAll('tbody tr')
    expect(statsRows).toHaveLength(2)
    expect(statsRows[0].text()).toContain('data.items[]')
    expect(statsRows[0].text()).toContain('12')
    expect(statsRows[0].text()).toContain('object')
    expect(w.text()).toContain('共 2 条路径')
    w.unmount()
  })
  it('替换只走显式按钮：粘贴与开关变化都不带 replace，点击后才带上', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({ ...okResult(), replaced: 2 })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    // 自动那次绝不能带 replace：改数据必须是用户按下去的
    expect(svc.Beautify.mock.calls.at(-1)![0].replace).toBeUndefined()

    const pathBox = w.findAll('.jfmt-path-input')
    expect(pathBox.length).toBeGreaterThanOrEqual(2)
    await pathBox[0].setValue('data.items[].ok')
    await pathBox[1].setValue('true')
    const btn = w.findAll('button').find((b) => b.text().includes('执行替换'))
    await btn!.trigger('click')
    await vi.advanceTimersByTimeAsync(50)
    await flushMicrotasks()
    expect(svc.Beautify.mock.calls.at(-1)![0].replace).toEqual({
      path: 'data.items[].ok', newValue: 'true', newKey: '',
    })
    expect(w.text()).toContain('已替换 2 处')
    w.unmount()
  })

  it('撤销与改缩进：撤销后不再带 replace，而改缩进仍带着它（不偷偷丢替换）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({ ...okResult(), replaced: 1 })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    await w.findAll('.jfmt-path-input')[0].setValue('a.b')
    await w.findAll('.jfmt-path-input')[1].setValue('1')
    await w.findAll('button').find((b) => b.text().includes('执行替换'))!.trigger('click')
    await vi.advanceTimersByTimeAsync(50)
    await flushMicrotasks()
    expect(svc.Beautify.mock.calls.at(-1)![0].replace).toBeTruthy()

    // 换个缩进重算：替换条件必须还在
    await w.find('.jfmt-select').setValue('4')
    await vi.advanceTimersByTimeAsync(50)
    await flushMicrotasks()
    expect(svc.Beautify.mock.calls.at(-1)![0].indent).toBe(4)
    expect(svc.Beautify.mock.calls.at(-1)![0].replace?.path).toBe('a.b')

    const undo = w.findAll('button').find((b) => b.text().includes('撤销'))
    expect(undo).toBeTruthy()
    await undo!.trigger('click')
    await vi.advanceTimersByTimeAsync(50)
    await flushMicrotasks()
    expect(svc.Beautify.mock.calls.at(-1)![0].replace).toBeUndefined()
    w.unmount()
  })

  it('缺路径或缺新值时不发请求，只给提示', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const before = svc.Beautify.mock.calls.length
    const run = w.findAll('button').find((b) => b.text().includes('执行替换'))
    await run!.trigger('click')
    await vi.advanceTimersByTimeAsync(50)
    await flushMicrotasks()
    expect(svc.Beautify.mock.calls.length).toBe(before)
    expect(w.text()).toContain('替换需要路径')
    w.unmount()
  })

  it('查找命中里的「→ 替换」把路径填进替换框（两处共用同一套路径写法）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({
      ...okResult(),
      matches: [{ path: 'data.items[].traceId', value: 'abc', kind: 'string', segment: 1, line: 2 }],
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const use = w.findAll('button').find((b) => b.text().includes('→ 替换'))
    expect(use).toBeTruthy()
    await use!.trigger('click')
    await flushMicrotasks()
    expect((w.findAll('.jfmt-path-input')[0].element as HTMLInputElement).value).toBe('data.items[].traceId')
    w.unmount()
  })

})

