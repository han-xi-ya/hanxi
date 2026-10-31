// 特征测试：JsonFmtView（JSON 基础美化器，2026-10-31 收纳裁决后的主干三件套）。
// 钉住的口径：
//  - 入参逐字等于 Wave 3 三字段契约 {text, indent, sortKeys}——曾经长出来的
//    unescape/query/replace/extractAll 已随规则族退役，不得回潮（精确对象断言）；
//  - 失败块四件齐（error/行列/snippet/hint）一件不丢、输入不清空；
//  - warnings 逐条上屏（成功与失败路径都不吞）；
//  - 「带去 AI 聊天」只写桥（direction=toChat）+ emit navigate，本地零外发——
//    外发确认链归聊天侧，这里弹第二次确认就是两处各问一遍的反面教材；
//  - 回程桥消费：toFmt 有货即填入并立即美化（不等防抖、不双发），读后即清，
//    方向不互串（toChat 的货美化器不接）。
// 后端绑定经 vi.mock 打桩（missingBindingSeam 兜底在册）；fake timers 下排干 await
// 一律走 flushMicrotasks（JsonFmtView.spec 头注纪律自旧版沿用）；桥是模块级单例，
// 每条用例前后 take* 双向排空，防跨用例弹入。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import JsonFmtView from '../JsonFmtView.vue'
import { useToast } from '../../composables/useToast'
import { handOff, handToFmt, peekForChat, takeForChat, takeForFmt } from '../../composables/useJsonChatBridge'

const svc = vi.hoisted(() => ({
  Beautify: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/jsonfmt', () => ({ JsonFmtService: svc }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
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
function errResult(patch: Record<string, unknown> = {}) {
  return {
    ok: false,
    output: '',
    error: 'JSON 语法错误：缺少结束花括号',
    hint: '看起来是尾部被截断（从日志窗口选区复制时最常见，往往少了末尾的 } 或 ]）。回源头把整段复制全；补不回来就按「带去 AI 聊天」让模型认结构。',
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

describe('JsonFmtView 失败块与带去 AI 聊天', () => {
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
    // classifyFailure 的指路文案原样显示，一个字都不改写
    expect(banner.text()).toContain('看起来是尾部被截断')
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

  it('失败时「带去 AI 聊天」：写桥 (toChat 原文) + emit navigate，本地零外发零 RPC 加发', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(errResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    const btn = w.find('.jfmt-chat-btn')
    expect(btn.exists()).toBe(true)
    // title 把外发事实说出来，不含糊
    expect(btn.attributes('title')).toContain('内容将发给本机 AI')
    expect(btn.attributes('title')).toContain('会离开这台机器')
    await btn.trigger('click')
    await flushMicrotasks()
    expect(peekForChat()).toBe(RAW_JSON) // 交的是原文，不是残块
    expect(w.emitted('navigate')).toEqual([['/ext/aichat']])
    expect(svc.Beautify).toHaveBeenCalledTimes(1) // 交接不加发任何美化调用
    w.unmount()
  })

  it('成功结果在场时「带去 AI 聊天」不在场（出路由失败文案指，不钓成功的人）', async () => {
    vi.useFakeTimers()
    svc.Beautify.mockResolvedValue(okResult())
    const w = mountView()
    await flushMicrotasks()
    await w.find('textarea').setValue(RAW_JSON)
    await vi.advanceTimersByTimeAsync(300)
    await flushMicrotasks()
    expect(w.find('.jfmt-chat-btn').exists()).toBe(false)
    expect(peekForChat()).toBeNull()
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
