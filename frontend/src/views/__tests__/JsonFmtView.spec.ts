// 特征测试：JsonFmtView（JSON 基础美化器，2026-10-31 收纳裁决后的主干三件套 +
// 同日「AI 一键美化」就地回归后的失败块主动作）。
// 钉住的口径：
//  - 入参逐字等于 Wave 3 三字段契约 {text, indent, sortKeys}——曾经长出来的
//    unescape/query/replace/extractAll 已随规则族退役，不得回潮（精确对象断言）；
//    ExtractByAI 吃同一份三字段契约（req 同 Beautify，开关跟工具条现值）；
//  - 失败块四件齐（error/行列/snippet/hint）一件不丢、输入不清空；
//  - warnings 逐条上屏（成功与失败路径都不吞）；
//  - 脏文本的主出路是就地「✨ AI 一键美化」（.jfmt-ai-btn）：按下先弹外发确认
//    （取消则零 RPC 零外发），在途/待决双闸防双击并发，失败与"过期未采用"各走
//    独立通道且不砸已在屏上的结果，采用即摘掉更早本地发的覆盖权（reqSeq 门禁）；
//  - 「或带到 AI 聊天细聊」降级为次要 ghost 链接（.jfmt-chat-link）：只写桥
//    （direction=toChat）+ emit navigate，本地零确认零外发——确认归聊天侧，
//    这里弹第二次确认就是两处各问一遍的反面教材；
//  - 回程桥消费：toFmt 有货即填入并立即美化（不等防抖、不双发），读后即清，
//    方向不互串（toChat 的货美化器不接）。
// 后端绑定经 vi.mock 打桩（missingBindingSeam 兜底在册）；useConfirm hoisted
// mock（AiChatView.spec 同法）——confirm 可控才谈得上钉"待决期双击"与"取消零发"；
// fake timers 下排干 await 一律走 flushMicrotasks（JsonFmtView.spec 头注纪律自旧版
// 沿用）；桥是模块级单例，每条用例前后 take* 双向排空，防跨用例弹入。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import JsonFmtView from '../JsonFmtView.vue'
import { useToast } from '../../composables/useToast'
import { handOff, handToFmt, peekForChat, takeForChat, takeForFmt } from '../../composables/useJsonChatBridge'

const svc = vi.hoisted(() => ({
  Beautify: vi.fn(),
  ExtractByAI: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/jsonfmt', () => ({ JsonFmtService: svc }))

// confirm 桩（AiChatView.spec 形制）：mock 成可控——返回 Promise 让用例能停在
// 「弹窗待决」这一拍上，双击与取消两条分支才钉得住。
const confirmBox = vi.hoisted(() => ({ confirm: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => confirmBox }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

// 可控挂起：把 RPC/confirm 停在在途态，由用例决定何时放行（慢发倒灌、待决双击
// 这类时序只有拿到 resolve 手柄才测得成）。
function deferred<T = unknown>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

// Wave 3 后输入必须本就是合法 JSON（后端不猜不修）；样例就用最 plain 的一份
const RAW_JSON = '{"a":1,"b":[1,2]}'

function okResult(patch: Record<string, unknown> = {}) {
  return {
    ok: true,
    output: '{\n  "a": 1,\n  "b": [1, 2]\n}',
    error: '',
    hint: '',
    line: 0,
    col: 0,
    snippet: '',
    kind: 'object',
    warnings: [] as string[],
    ...patch,
  }
}

// 失败样例逐件给齐：错误、1 基行列、出错片段、classifyFailure 的方向判断
// （hint 与后端 2026-10-31 改写后的文案逐字同步：出路是就地「AI 一键美化」，
// 不再把人赶去聊天模块——fixture 不跟着走，指路断言就是拿旧地图考新路线）
function errResult(patch: Record<string, unknown> = {}) {
  return {
    ok: false,
    output: '',
    error: 'JSON 语法错误：缺少结束花括号',
    hint: '看起来是尾部被截断（从日志窗口选区复制时最常见，往往少了末尾的 } 或 ]）。回源头把整段复制全；补不回来就按「AI 一键美化」让本机模型认结构。',
    line: 7,
    col: 12,
    snippet: '…{"a":1,"b":[1,2…',
    kind: '',
    warnings: [] as string[],
    ...patch,
  }
}

function mountView() {
  return mount(JsonFmtView, { attachTo: document.body })
}

// AI 族用例共同前置：贴原文 → 本地主干读不动 → 失败块带主动作钮上屏。
// 钮位与 title 的口径也在这里钉：外发事实（会离开这台机器）与顺序（先确认再发）
// 必须在点之前就写在钮上，不靠弹窗补课。
async function mountFailure() {
  svc.Beautify.mockResolvedValue(errResult())
  const w = mountView()
  await flushMicrotasks()
  await w.find('textarea').setValue(RAW_JSON)
  await vi.advanceTimersByTimeAsync(300)
  await flushMicrotasks()
  const btn = w.find('.jfmt-ai-btn')
  expect(btn.exists()).toBe(true)
  expect(btn.text()).toContain('✨ AI 一键美化')
  expect(btn.attributes('title')).toContain('内容将发给本机 AI')
  expect(btn.attributes('title')).toContain('会离开这台机器')
  expect(btn.attributes('title')).toContain('先确认再发')
  return w
}

beforeEach(() => {
  takeForChat() // 排空桥（模块级单例跨用例存活）
  takeForFmt()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  useToast().clearToast()
  takeForChat()
  takeForFmt()
})

describe('JsonFmtView 主干三件套', () => {
  it('输入变化 300ms 防抖自动 Beautify，入参逐字等于 Wave 3 三字段契约', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    expect(svc.Beautify).not.toHaveBeenCalled() // 挂载空输入：不发 RPC
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(299)
    expect(svc.Beautify).not.toHaveBeenCalled() // 防抖未到期不触发
    await vi.advanceTimersByTimeAsync(1)
    expect(svc.Beautify).toHaveBeenCalledTimes(1)
    // 精确对象断言（非 objectContaining）：unescape/query/replace/extractAll 已随
    // 规则族退役——谁复活谁红，这里不认第二字段
    expect(svc.Beautify).toHaveBeenCalledWith({ text: RAW_JSON, indent: 2, sortKeys: false })
    w.unmount()
  })

  it('空输入与纯空白都不发 RPC，输出区是 .empty-hint；退役功能的 UI 一件不残', async () => {
    vi.useFakeTimers()
    const w = mountView()
    await flushMicrotasks()
    expect(svc.Beautify).not.toHaveBeenCalled()
    expect(w.find('.empty-hint').exists()).toBe(true)
    await w.find('textarea').setValue('   \n  ')
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).not.toHaveBeenCalled()
    expect(w.find('.empty-hint').exists()).toBe(true)
    // 退役断言：AI 提取钮、展开转义、查找/替换/统计工具栏都不该再存在
    // （就地「AI 一键美化」是失败块主动作，不在退役之列——它只在读不动时出现）
    expect(w.text()).not.toContain('让 AI 提取')
    expect(w.text()).not.toContain('键路径统计')
    expect(w.text()).not.toContain('执行替换')
    expect(w.text()).not.toContain('转义还原')
    expect(w.find('.jfmt-find').exists()).toBe(false)
    expect(w.find('.jfmt-switch').exists()).toBe(false)
    expect(w.findAll('textarea')).toHaveLength(1) // 输出是只读 pre，不再第二个 textarea
    w.unmount()
  })

  it('手动「美化」立即发请求并吃掉待发防抖（不双发）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON) // 防抖已排队
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
    await w.find('textarea').setValue(RAW_JSON)
    w.unmount()
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).not.toHaveBeenCalled()
  })

  it('缩进换档（含压缩=0）与键排序立即重算带上新开关，不待防抖、不双发', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON) // 300ms 防抖在排队
    await w.find('select').setValue('0')
    await flushMicrotasks()
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 开关变更即刻发，没等定时器
    expect(svc.Beautify).toHaveBeenCalledWith({ text: RAW_JSON, indent: 0, sortKeys: false })
    await w.find('input[type="checkbox"]').setValue(true)
    await flushMicrotasks()
    expect(svc.Beautify).toHaveBeenCalledTimes(2)
    expect(svc.Beautify).toHaveBeenLastCalledWith({ text: RAW_JSON, indent: 0, sortKeys: true })
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).toHaveBeenCalledTimes(2) // 没有第三发
    w.unmount()
  })

  it('成功结果如实上屏：kind 中文标签、后端 output 原样进只读区、复制钮回填 output', async () => {
    vi.useFakeTimers()
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const res = okResult()
    svc.Beautify.mockResolvedValue(res)
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    expect(w.find('.jfmt-meta .chip').text()).toContain('对象')
    // 逐字节对后端 output：pre 原样呈现，不裁不重排（textContent 而非 text()，后者会折叠空白）
    expect(w.find('.jfmt-out').element.textContent).toBe(res.output)
    await w.find('.jfmt-copy-btn').trigger('click')
    await flushMicrotasks()
    expect(writeText).toHaveBeenCalledWith(res.output)
    w.unmount()
  })
})

describe('JsonFmtView 失败块与次要聊天链接', () => {
  it('ok=false：error/第 line 行 第 col 列/snippet/hint 四件齐上屏，原始输入不被清空', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(errResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const banner = w.find('.banner.banner-error')
    expect(banner.text()).toContain('JSON 语法错误：缺少结束花括号')
    expect(banner.text()).toContain('第 7 行')
    expect(banner.text()).toContain('第 12 列')
    expect(banner.text()).toContain('…{"a":1,"b":[1,2…')
    // classifyFailure 的指路文案原样显示，一个字都不改写；且指的方向是就地钮
    // （「AI 一键美化」字样），不再赶人去聊天模块
    expect(banner.text()).toContain('看起来是尾部被截断')
    expect(banner.text()).toContain('AI 一键美化')
    expect(w.find('textarea').element.value).toBe(RAW_JSON) // 输入原样保留
    w.unmount()
  })

  it('给不出位置的失败（line=0）：位置行不渲染，error 与 hint 照上', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(errResult({ line: 0, col: 0, snippet: '', hint: '本模块不静默截断；截出要看的那一段再贴，或改用文件工具。' }))
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const banner = w.find('.banner.banner-error')
    expect(banner.text()).toContain('JSON 语法错误')
    expect(banner.text()).toContain('本模块不静默截断')
    expect(banner.text()).not.toContain('第 0 行')
    w.unmount()
  })

  it('warnings 逐条可见（成功与失败路径都不吞）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(
      okResult({ warnings: ['检测到重复键 "a"：保留最后一个', '输入含 BOM，已剔除'] }),
    )
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const warns = w.findAll('.banner.banner-warn')
    expect(warns).toHaveLength(2)
    expect(warns[0].text()).toBe('检测到重复键 "a"：保留最后一个')
    expect(warns[1].text()).toBe('输入含 BOM，已剔除')
    w.unmount()
    // 失败路径后端也可能带 warnings：与错误横幅同框，一条不吞
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(errResult({ warnings: ['尾部有无法归类的残字'] }))
    const w2 = mountView()
    await flushMicrotasks()
    await w2.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    expect(w2.findAll('.banner.banner-warn')).toHaveLength(1)
    expect(w2.find('.banner.banner-error').exists()).toBe(true)
    w2.unmount()
  })

  it('次要链接「或带到 AI 聊天细聊」：写桥 (toChat 原文) + emit navigate，本地零确认零外发零 RPC 加发', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(errResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const link = w.find('.jfmt-chat-link')
    expect(link.exists()).toBe(true)
    // title 把分工说出来：发送与确认仍在聊天侧——这条链接这里再弹一次确认，
    // 就是同一件事问两遍的反面教材
    expect(link.attributes('title')).toContain('发送与确认仍在聊天侧')
    await link.trigger('click')
    await flushMicrotasks()
    expect(peekForChat()).toBe(RAW_JSON) // 交的是原文，不是残块
    expect(w.emitted('navigate')).toEqual([['/ext/aichat']])
    expect(confirmBox.confirm).not.toHaveBeenCalled() // 本地零确认（确认归聊天侧）
    expect(svc.ExtractByAI).not.toHaveBeenCalled() // 交接不是外发
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 交接不加发任何美化调用
    w.unmount()
  })

  it('成功结果在场时 AI 钮与聊天链接都不在场（出路只在读不动时指，不钓成功的人）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    expect(w.find('.jfmt-ai-btn').exists()).toBe(false) // 就地主出路只属于失败块
    expect(w.find('.jfmt-chat-link').exists()).toBe(false)
    expect(peekForChat()).toBeNull()
    expect(svc.ExtractByAI).not.toHaveBeenCalled()
    w.unmount()
  })
})

describe('JsonFmtView AI 一键美化（失败块主动作）', () => {
  it('确认取消：零 RPC 零外发、失败块原样（没拿到同意就不许有外发）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(false)
    const w = await mountFailure()
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    // 确认文案在按下之前就把两件事实讲清：内容离开本机 + 耗时几十秒量级（#128）
    const arg = confirmBox.confirm.mock.calls[0][0]
    expect(arg.tone).toBe('warning')
    expect(arg.description).toContain('离开这台机器')
    expect(arg.description).toContain('几十秒')
    expect(svc.ExtractByAI).not.toHaveBeenCalled()
    expect(peekForChat()).toBeNull()
    expect(w.emitted('navigate')).toBeUndefined()
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 取消也不加发本地重算
    expect(w.find('.jfmt-ai-btn').exists()).toBe(true) // 失败块原样，出路还指着
    w.unmount()
  })

  it('确认后 ExtractByAI 收到逐字 {text, indent, sortKeys}（与 Beautify 同一份三字段契约，开关跟工具条现值）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    svc.ExtractByAI.mockResolvedValue(okResult())
    const w = await mountFailure()
    // 先换档（缩进 4 触发第二发本地重算），再按 AI：契约字段必须带上新开关，
    // 不是把默认值 2 焊死在钮里
    await w.find('select').setValue('4')
    await flushMicrotasks()
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    // 精确对象断言（同 Beautify 的规矩）：不认第二字段
    expect(svc.ExtractByAI).toHaveBeenCalledWith({ text: RAW_JSON, indent: 4, sortKeys: false })
    w.unmount()
  })

  it('成功返回就地替换失败块：output 进只读区、warnings 逐条上屏、钮随失败块一起退场（出路已被消费）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    const aiRes = okResult({
      output: '{\n  "recovered": true\n}',
      warnings: ['AI 提取结果含重复键 "a"：保留最后一个'],
    })
    svc.ExtractByAI.mockResolvedValue(aiRes)
    const w = await mountFailure()
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    expect(w.find('.banner.banner-error').exists()).toBe(false) // 失败块被就地替换
    expect(w.find('.jfmt-out').element.textContent).toBe(aiRes.output) // 逐字节原样
    expect(w.findAll('.banner.banner-warn')).toHaveLength(1) // 后端 warnings 一条不吞
    expect(w.find('.jfmt-ai-btn').exists()).toBe(false)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 采纳 AI 结果不加发本地重算
    w.unmount()
  })

  it('待决「等待确认…」与在途「AI 认结构中…」双闸禁钮：双击只一发 confirm 只一发 RPC（弹窗期间第二下并发外发是教训在册）', async () => {
    vi.useFakeTimers()
    const ask = deferred<boolean>()
    confirmBox.confirm.mockReturnValue(ask.promise)
    const ai = deferred()
    svc.ExtractByAI.mockReturnValue(ai.promise)
    const w = await mountFailure()
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    expect(w.find('.jfmt-ai-btn').text()).toContain('等待确认…')
    expect(w.find('.jfmt-ai-btn').attributes('disabled')).toBeDefined()
    // 待决期再点一下（模拟没等到弹窗就双击的人）：aiAsking 必须先于 await 置闸，
    // 否则弹窗期间按钮一直可点，双击就是两发并发外发
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    ask.resolve(true)
    await flushMicrotasks()
    expect(svc.ExtractByAI).toHaveBeenCalledTimes(1)
    expect(w.find('.jfmt-ai-btn').text()).toContain('AI 认结构中…')
    expect(w.find('.jfmt-ai-btn').attributes('disabled')).toBeDefined()
    await w.find('.jfmt-ai-btn').trigger('click') // 在途第三下：aiBusy 闸停住
    await flushMicrotasks()
    expect(svc.ExtractByAI).toHaveBeenCalledTimes(1)
    expect(w.find('.jfmt-ai-btn').text()).not.toContain('等待确认…')
    ai.resolve(okResult())
    await flushMicrotasks()
    expect(w.find('.jfmt-ai-btn').exists()).toBe(false) // 采纳完成，钮随失败块退场
    w.unmount()
  })

  it('AI 一发失败走独立横幅：不砸在屏的失败块（"AI 不行"不该把用户已拿在手里的东西一起砸掉）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    svc.ExtractByAI.mockRejectedValue(new Error('未装命令行/未登录'))
    const w = await mountFailure()
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    // 三通道分列：模板顶部 AI 错误条在前、失败块在后，各说各的事
    const errs = w.findAll('.banner.banner-error')
    expect(errs).toHaveLength(2)
    expect(errs[0].text()).toContain('未装命令行/未登录')
    expect(errs[0].text()).toContain('AI 美化失败不影响上面的结果')
    expect(errs[1].text()).toContain('JSON 语法错误：缺少结束花括号') // lastResult 原样
    expect(errs[1].text()).toContain('第 7 行')
    expect(w.find('textarea').element.value).toBe(RAW_JSON)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // AI 失败不触发本地重算
    // 钮回到待命态可以再按（环境修好就能重试，不是一次性死路）
    expect(w.find('.jfmt-ai-btn').text()).toContain('✨ AI 一键美化')
    expect(w.find('.jfmt-ai-btn').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('返回前改了输入：aiNote 说明「未采用」且 lastResult 原样不动（返回≠采用，但也不是失败）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    const ai = deferred()
    svc.ExtractByAI.mockReturnValue(ai.promise)
    const w = await mountFailure()
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    // 在途可编辑（横幅明说的承诺）：几十秒里用户改了自己的输入
    await w.find('textarea').setValue('{"edited":1}')
    ai.resolve(okResult())
    await flushMicrotasks()
    const note = w.find('.banner.banner-info')
    expect(note.exists()).toBe(true)
    expect(note.text()).toContain('未采用')
    expect(note.text()).toContain('输入在这期间被改过') // 说清为什么屏上没动静
    // 未采用 = 在屏的失败块原样，AI 那份不算数
    expect(w.find('.banner.banner-error').text()).toContain('JSON 语法错误')
    expect(w.find('.jfmt-out').exists()).toBe(false)
    w.unmount()
  })

  it('AI 采用后在飞的本地旧发被 reqSeq 摘掉覆盖权：晚到的旧结果不倒灌（AI 几十秒 vs 本地几十毫秒，不设这道闸成果会被顶掉）', async () => {
    vi.useFakeTimers()
    confirmBox.confirm.mockResolvedValue(true)
    // 第一发快失败（失败块上屏带 AI 钮）；第二发（换档触发）故意挂起在途
    const slow = deferred()
    svc.Beautify.mockResolvedValueOnce(errResult()).mockReturnValueOnce(slow.promise)
    svc.ExtractByAI.mockResolvedValue(okResult({ output: 'AI 的结果' }))
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    await w.find('select').setValue('4') // 第二发本地在途（此刻钮仍在：running 不吞上轮结果）
    await flushMicrotasks()
    expect(svc.Beautify).toHaveBeenCalledTimes(2)
    await w.find('.jfmt-ai-btn').trigger('click')
    await flushMicrotasks()
    expect(w.find('.jfmt-out').text()).toContain('AI 的结果') // 采纳即上屏
    // 现在才放行那发早已过期的本地旧发：它揣着 seq=2，而采用把 reqSeq 推到 3
    slow.resolve(okResult({ output: '迟到的旧发倒灌' }))
    await flushMicrotasks()
    expect(w.find('.jfmt-out').element.textContent).toBe('AI 的结果') // 一字不动
    expect(w.text()).not.toContain('迟到的旧发倒灌')
    expect(w.find('.banner.banner-error').exists()).toBe(false) // 旧发即便是失败也不许顶掉成果
    w.unmount()
  })
})

describe('JsonFmtView 回程桥消费', () => {
  it('onMounted 时桥里有 toFmt 货：填入输入并立即美化一次（不等防抖、不双发）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const INBOUND = '{\n"from":"chat","n":[1,2,3]\n}'
    handToFmt(INBOUND)
    const w = mountView()
    await flushMicrotasks()
    expect(w.find('textarea').element.value).toBe(INBOUND)
    expect(svc.Beautify).toHaveBeenCalledTimes(1)
    expect(svc.Beautify).toHaveBeenCalledWith({ text: INBOUND, indent: 2, sortKeys: false })
    await vi.advanceTimersByTimeAsync(1000)
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 消费那发吃掉了 watch 排的防抖
    expect(w.text()).toContain('来自 AI 聊天') // 来源条说出文本出处（不是用户自己打的）
    w.unmount()
  })

  it('takeForFmt 一次性：消费后桥清空，再次挂载不二次弹入', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    handToFmt('{"once":true}')
    const w = mountView()
    await flushMicrotasks()
    expect(takeForFmt()).toBeNull() // 已被挂载消费
    w.unmount()
    const w2 = mountView()
    await flushMicrotasks()
    expect(w2.find('textarea').element.value).toBe('')
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 第二次挂载零加发
    w2.unmount()
  })

  it('方向不互串：桥里是 toChat 的货时美化器不接、也不清它（留给聊天侧消费）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    handOff('带日志前缀的脏文本 {"a":1…')
    const w = mountView()
    await flushMicrotasks()
    expect(svc.Beautify).not.toHaveBeenCalled()
    expect(w.find('textarea').element.value).toBe('')
    expect(peekForChat()).toBe('带日志前缀的脏文本 {"a":1…') // 原样还在桥里
    w.unmount()
  })

  it('传输层失败（RPC 抛）走独立横幅：结果区复位、提示可点「美化」重试', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockRejectedValue(new Error('模块未就绪'))
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    expect(w.find('.banner.banner-error').text()).toContain('调用格式化服务失败')
    expect(w.find('.banner.banner-error').text()).toContain('模块未就绪')
    expect(w.find('.jfmt-out').exists()).toBe(false)
    w.unmount()
  })
})
