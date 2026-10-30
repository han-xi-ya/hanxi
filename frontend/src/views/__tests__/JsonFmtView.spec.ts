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
  ExtractByAI: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/jsonfmt', () => ({ JsonFmtService: svc }))

// 外发确认：视图用全局 useConfirm 单例，而 ConfirmDialog 由 App.vue 顶层挂载——
// 本测试只挂视图，对话框不存在，不 mock 就会等一个永远不会来的 settle。
const confirmBox = vi.hoisted(() => ({ confirm: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => confirmBox }))

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

// 三块内容合进一条工具栏（默认停在「查找」）：要用替换/统计得先切过去
async function openTool(w: any, label: string) {
  const btn = w.findAll('.jfmt-switch button').find((b: any) => b.text().includes(label))
  expect(btn).toBeTruthy()
  await btn!.trigger('click')
  await flushMicrotasks()
}

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
    // 输出区已换成着色渲染的 <pre>：只读 textarea 既不能上色，也标不出命中位置
    const out = w.find('.jfmt-out')
    expect(out.text()).toContain('"a": 1')
    // 这份 mock 的值全是数字与数组，所以只断键/数字/标点三档；
    // 字符串色档由下面那条含字符串值的用例覆盖（别在这份 mock 上断不存在的东西）
    expect(out.find('.jf-k').exists()).toBe(true)
    expect(out.find('.jf-n').exists()).toBe(true)
    expect(out.find('.jf-p').exists()).toBe(true)
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
    // 每段各自一块着色输出；textarea 只剩输入那一个
    expect(w.findAll('.jfmt-out')).toHaveLength(2)
    expect(w.findAll('textarea')).toHaveLength(1)
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
    await openTool(w, '统计')
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

    await openTool(w, '替换')
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
    await openTool(w, '替换')
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
    await openTool(w, '替换')
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
    // 填进的是替换框，而替换框属「替换」档：切过去才看得见
    await openTool(w, '替换')
    expect((w.findAll('.jfmt-path-input')[0].element as HTMLInputElement).value).toBe('data.items[].traceId')
    w.unmount()
  })


  it('查找词在输出里原地高亮，并给出命中数与上下跳转', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue({
      ...okResult(),
      output: '{"traceId":"abc","span":{"traceId":"abc"}}',
      matches: [{ path: 'traceId', value: 'abc', kind: 'string' }],
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    await w.find('.jfmt-find-input').setValue('traceId')
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    expect(w.findAll('.jfmt-out')[0].find('.jf-s').exists()).toBe(true) // 值字符串有色档
    const hits = w.findAll('.jf-hit')
    expect(hits.length).toBeGreaterThanOrEqual(2) // 键名出现两次都要标出来，不是只标第一个
    expect(w.text()).toContain('输出内命中')
    const next = w.findAll('button').find((b) => b.text().includes('下一个'))
    expect(next).toBeTruthy()
    await next!.trigger('click')
    await next!.trigger('click')
    expect(w.find('.jf-hit-cur').exists()).toBe(true)
    w.unmount()
  })
})

// ---------- AI 提取（外发路径） ----------
// 这一族的要点不在"能不能调通"，而在**什么时候不许调**：粘贴、改开关、点美化都不许
// 把用户内容送出机器；只有按下那个钮、并且确认过，才允许外发一次。
describe('JsonFmtView AI 提取只在按下钮时外发', () => {
  function aiResult() {
    return okResult({
      warnings: ['由本机 AI 提取：外发 1234 字节、模型返回 567 字节、耗时 3210ms（内容已经过本地 JSON 校验）'],
    })
  }

  it('粘贴与开关变化只走本地 Beautify，ExtractByAI 一次都不调', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    // 改缩进会立刻重算——那也是"自动"路径，同样不许外发
    await w.find('select').setValue('4')
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    expect(svc.Beautify.mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(svc.ExtractByAI).not.toHaveBeenCalled()
    expect(confirmBox.confirm).not.toHaveBeenCalled()
    w.unmount()
  })

  it('点「让 AI 提取」先要过一次外发确认，确认后才调用并把返回的交代上屏', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    svc.ExtractByAI.mockResolvedValue(aiResult())
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    const btn = w.findAll('button').find((b) => b.text().includes('让 AI 提取'))
    expect(btn).toBeTruthy()
    await btn!.trigger('click')
    await flushMicrotasks()

    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    // 确认文案必须把"会离开这台机器"说出来，而不是含糊的"使用 AI 处理"
    const desc = String(confirmBox.confirm.mock.calls[0][0].description)
    expect(desc).toContain('离开这台机器')
    expect(desc).toContain('87 字节') // LOG_TEXT 的 UTF-8 长度；字节数要说得出具体量，不是"部分内容"
    expect(svc.ExtractByAI).toHaveBeenCalledTimes(1)
    expect(svc.ExtractByAI.mock.calls[0][0]).toMatchObject({
      text: LOG_TEXT, indent: 2, sortKeys: false, unescape: true, extractAll: true, query: '',
    })
    // 后端那句"外发多少字节 / 耗时多久"要原样可见，一条不吞
    expect(w.find('.jfmt-warns').text()).toContain('由本机 AI 提取')
    expect(w.find('.jfmt-warns').text()).toContain('外发 1234 字节')
    w.unmount()
  })

  it('取消确认就一次都不外发（也不留任何在途态）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(false)
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    const btn = w.findAll('button').find((b) => b.text().includes('让 AI 提取'))
    await btn!.trigger('click')
    await flushMicrotasks()

    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    expect(svc.ExtractByAI).not.toHaveBeenCalled()
    expect(w.text()).not.toContain('AI 提取中')
    w.unmount()
  })

  it('AI 在途期间改了原文：成果不采用，但要把"为什么屏上没动静"说出来', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    // 手动掌控返回时机：几十秒的在途，"什么时候回"本身就是被测行为
    let release: (v: unknown) => void = () => {}
    svc.ExtractByAI.mockImplementation(() => new Promise((resolve) => { release = resolve }))
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    const btn = w.findAll('button').find((b) => b.text().includes('让 AI 提取'))
    await btn!.trigger('click')
    await flushMicrotasks()
    expect(svc.ExtractByAI).toHaveBeenCalledTimes(1)

    // 用户等不及改了原文（这会另起一发本地 Beautify）
    svc.Beautify.mockResolvedValue(okResult())
    await w.findAll('textarea')[0].setValue('{"changed":1}')
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    release(okResult({ output: '{"STALE":1}' }))
    await flushMicrotasks()

    expect(w.text()).toContain('未采用')
    expect(w.text()).not.toContain('STALE') // 过期成果不得倒灌上屏
    expect(w.text()).not.toContain('AI 提取中') // 在途帽照样要摘，否则界面看着还在等
    w.unmount()
  })

  it('按下 AI 不吃掉待发的防抖：到点本地那一发照常发出', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    svc.ExtractByAI.mockImplementation(() => new Promise(() => {})) // 永不返回，只看这一发有没有被清掉
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()

    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(100)
    expect(svc.Beautify).not.toHaveBeenCalled() // 防抖未到期

    await w.findAll('button').find((b) => b.text().includes('让 AI 提取'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.ExtractByAI).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(200)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // AI 无权清防抖
    expect(svc.Beautify.mock.calls[0][0]).toMatchObject({ text: LOG_TEXT })
    w.unmount()
  })

  it('本地那一发在飞时按下 AI：它先回来照样上屏，AI 的成果随后也照样采用', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    let releaseAI: (v: unknown) => void = () => {}
    let releaseLocal: (v: unknown) => void = () => {}
    svc.ExtractByAI.mockImplementation(() => new Promise((resolve) => { releaseAI = resolve }))
    svc.Beautify.mockImplementation(() => new Promise((resolve) => { releaseLocal = resolve }))
    const w = mountView()
    await flushMicrotasks()

    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 已在飞，尚未返回
    await flushMicrotasks()

    await w.findAll('button').find((b) => b.text().includes('让 AI 提取'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.ExtractByAI).toHaveBeenCalledTimes(1)

    // 在飞的本地发不得因为按下 AI 而作废（旧写法借用 ++reqSeq 会把它判成过期）。
    // AI 的成果按「原文还对不对」判，两件事不能捆在一起。
    releaseLocal(okResult())
    await flushMicrotasks()
    expect(w.find('.jfmt-out').text()).toContain('"a": 1')
    expect(w.text()).not.toContain('正在格式化')

    releaseAI(okResult({ output: '{"kept":1}' }))
    await flushMicrotasks()
    expect(w.text()).toContain('kept')
    expect(w.text()).not.toContain('未采用')
    w.unmount()
  })

  it('确认文案要按实测说清耗时量级（写"几秒"会让人以为卡死）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(false)
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    await w.findAll('button').find((b) => b.text().includes('让 AI 提取'))!.trigger('click')
    await flushMicrotasks()
    expect(String(confirmBox.confirm.mock.calls[0][0].description)).toContain('几十秒')
    w.unmount()
  })

  it('AI 失败只说 AI 这条路，上一轮本地结果与输入都不砸掉', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    svc.Beautify.mockResolvedValue(okResult())
    svc.ExtractByAI.mockRejectedValue(new Error('本机未找到 Claude Code 命令行（claude）'))
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    expect(w.findAll('.jfmt-out').length).toBe(1) // 本地结果已在屏

    const btn = w.findAll('button').find((b) => b.text().includes('让 AI 提取'))
    await btn!.trigger('click')
    await flushMicrotasks()

    expect(w.text()).toContain('本机未找到 Claude Code 命令行')
    expect(w.text()).toContain('这只影响 AI 这条路')
    expect(w.findAll('.jfmt-out').length).toBe(1) // 结果没被清空
    expect(w.findAll('textarea')[0].element.value).toBe(LOG_TEXT) // 输入原样保留
    expect(w.text()).not.toContain('AI 提取中') // 在途帽已摘
    w.unmount()
  })

  it('确认框开着时连点两下，也只许外发一次', async () => {
    vi.useFakeTimers()
    // 故意让确认悬着不返回：弹窗那几十秒就是双击的窗口期
    confirmBox.confirm.mockImplementation(() => new Promise(() => {}))
    svc.ExtractByAI.mockImplementation(() => new Promise(() => {}))
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()

    const btn = w.findAll('button').find((b) => b.text().includes('让 AI 提取'))!
    await btn.trigger('click')
    await flushMicrotasks()
    await btn.trigger('click') // 第二次：确认框还悬着，按钮此时必须已禁用
    await flushMicrotasks()

    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    expect(svc.ExtractByAI).not.toHaveBeenCalled() // 还没确认，一发都不该外发
    w.unmount()
  })

  it('AI 成果上屏后，晚到的本地旧发不得把它顶掉', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    let releaseLocal: (v: unknown) => void = () => {}
    let releaseAI: (v: unknown) => void = () => {}
    svc.Beautify.mockImplementation(() => new Promise((resolve) => { releaseLocal = resolve }))
    svc.ExtractByAI.mockImplementation(() => new Promise((resolve) => { releaseAI = resolve }))
    const w = mountView()
    await flushMicrotasks()

    await w.findAll('textarea')[0].setValue(LOG_TEXT)
    await vi.advanceTimersByTimeAsync(300)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 本地发起，尚未返回
    await flushMicrotasks()

    await w.findAll('button').find((b) => b.text().includes('让 AI 提取'))!.trigger('click')
    await flushMicrotasks()
    releaseAI(okResult({ output: '{"fromAI":1}' }))
    await flushMicrotasks()
    expect(w.text()).toContain('fromAI')

    // 几十毫秒前发出的那一发现在才回来（内容是"解析失败"）：不能顶掉已到手的成绩
    releaseLocal(okResult({ ok: false, output: '', error: 'JSON 语法错误：本地这发才回来' }))
    await flushMicrotasks()
    expect(w.text()).toContain('fromAI')
    expect(w.text()).not.toContain('本地这发才回来')

    // 用户主动重算是**新意图**，不受"旧发优先"约束：这一发返回就该顶掉 AI 成果。
    // （releaseLocal 这个变量在每次调用时被重新赋值，此刻指向第二发的放行口）
    await w.find('select').setValue('4')
    await flushMicrotasks()
    expect(svc.Beautify).toHaveBeenCalledTimes(2)
    releaseLocal(okResult({ output: '{"rerun":1}' }))
    await flushMicrotasks()
    expect(w.text()).toContain('rerun')
    expect(w.text()).not.toContain('fromAI')
    w.unmount()
  })
})
