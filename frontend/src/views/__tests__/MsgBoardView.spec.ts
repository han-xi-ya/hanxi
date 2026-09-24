// 桌面留言板模块页（MsgBoardView，N6 重设计）特征测试：加载回显、类型速挂
// （单击填词/双击保存+挂出）、保存走 SetConfig 全量快照、保存失败回读不私留
// 假状态、挂/撤与状态事件、异常 chip 只在异常时出现、预览与表单同源联动。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import MsgBoardView from '../MsgBoardView.vue'

const api = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  GetConfig: vi.fn(),
  SetConfig: vi.fn(),
  ListScreens: vi.fn(),
  Toggle: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/msgboard', () => ({
  MsgBoardService: api,
}))

const status = (over = {}) => ({ shown: false, hotkey: 'Ctrl+Alt+B', hotkeyActive: true, keepAwake: false, ...over })
const config = (over = {}) => ({ text: '马上回来', fontSize: 64, screen: '', hotkey: 'Ctrl+Alt+B', everyScreen: true, ...over })
const screens = [
  { device: '\\\\.\\DISPLAY1', width: 2560, height: 1440, isPrimary: true },
  { device: '\\\\.\\DISPLAY2', width: 1920, height: 1080, isPrimary: false },
]

async function mountView(st = status(), cfg = config()) {
  api.GetStatus.mockResolvedValue(st)
  api.GetConfig.mockResolvedValue(cfg)
  api.ListScreens.mockResolvedValue(screens)
  const wrapper = mount(MsgBoardView, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  api.GetStatus.mockResolvedValue(status())
  api.GetConfig.mockResolvedValue(config())
  api.ListScreens.mockResolvedValue(screens)
  api.SetConfig.mockResolvedValue(undefined)
  api.Toggle.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

describe('多屏同时挂牌（N29）', () => {
  it('everyScreen 默认真：目标显示器选择器缺席；关勾后出现且只列副屏', async () => {
    const w = await mountView()
    expect(w.find('input[aria-label="多屏同时挂牌"]').exists()).toBe(true)
    expect((w.find('input[aria-label="多屏同时挂牌"]').element as HTMLInputElement).checked).toBe(true)
    expect(w.find('select[aria-label="目标显示器"]').exists()).toBe(false)
    await w.find('input[aria-label="多屏同时挂牌"]').setValue(false)
    const sel = w.find('select[aria-label="目标显示器"]')
    expect(sel.exists()).toBe(true)
    // 下拉只列副屏（主屏走"默认"空值项）——旧口径在单屏模式原样保留
    const options = sel.findAll('option')
    expect(options).toHaveLength(2)
    expect(options[0].attributes('value')).toBe('')
    expect(options[1].text()).toContain('DISPLAY2')
    w.unmount()
  })

  it('勾动态变化计入脏判定，保存全量快照携 everyScreen', async () => {
    const w = await mountView()
    await w.find('input[aria-label="多屏同时挂牌"]').setValue(false)
    await flushPromises()
    const save = w.findAll('button').find((b) => b.text().includes('保存'))!
    await save.trigger('click')
    await flushPromises()
    expect(api.SetConfig).toHaveBeenCalledTimes(1)
    expect(api.SetConfig.mock.calls[0][0]).toMatchObject({ everyScreen: false })
    w.unmount()
  })
})

describe('MsgBoardView', () => {
  it('挂载并行拉取状态/配置/显示器并回显表单；预览与正文同源', async () => {
    const wrapper = await mountView()
    expect((wrapper.find('#mb-text').element as HTMLTextAreaElement).value).toBe('马上回来')
    expect((wrapper.find('.mb-hotkey').element as HTMLInputElement).value).toBe('Ctrl+Alt+B')
    expect(wrapper.text()).toContain('未挂牌')
    // 健康态头章只有"未挂牌"一枚 chip：热键/防休眠不再仪表盘化
    expect(wrapper.text()).not.toContain('热键在位')
    expect(wrapper.text()).not.toContain('热键未在位')
    // 实时预览渲染当前草稿正文（BoardCard 同源画法）
    expect(wrapper.find('.bc-title').text()).toBe('马上回来')
    wrapper.unmount()
  })

  it('类型钮单击=填词进脏态（不保存不挂出），高亮当前匹配类型', async () => {
    const wrapper = await mountView(status(), config({ text: '' }))
    const tea = wrapper.findAll('.type-btn')[2] // ☕ 茶水
    await tea.trigger('click')
    expect((wrapper.find('#mb-text').element as HTMLTextAreaElement).value).toContain('去茶水间了')
    expect(wrapper.text()).toContain('未保存')
    expect(api.SetConfig).not.toHaveBeenCalled()
    expect(api.Toggle).not.toHaveBeenCalled()
    // 预览即时联动（编辑所见即所得）
    expect(wrapper.find('.bc-title').text()).toContain('去茶水间了')
    wrapper.unmount()
  })

  it('类型钮双击=填词+保存一步挂出（未挂态 Save→Toggle 各一次）', async () => {
    const wrapper = await mountView(status({ shown: false }), config({ text: '' }))
    const walk = wrapper.findAll('.type-btn')[3] // 🚶 小憩
    await walk.trigger('dblclick')
    await flushPromises()
    expect(api.SetConfig).toHaveBeenCalledTimes(1)
    expect(api.SetConfig.mock.calls[0][0].text).toContain('遛弯回血')
    expect(api.Toggle).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('类型钮双击在已挂态只热更不 Toggle（Toggle 是翻转语义，盲调会撤牌）', async () => {
    const wrapper = await mountView(status({ shown: true, keepAwake: true }))
    const lunch = wrapper.findAll('.type-btn')[1]
    await lunch.trigger('dblclick')
    await flushPromises()
    expect(api.SetConfig).toHaveBeenCalledTimes(1)
    expect(api.Toggle).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('保存把全量快照交给 SetConfig；成功落"已保存"', async () => {
    const wrapper = await mountView()
    await wrapper.find('#mb-text').setValue('🍜 干饭去了\n一小时后回')
    await wrapper.find('.action-row .btn-secondary').trigger('click')
    await flushPromises()
    expect(api.SetConfig).toHaveBeenCalledTimes(1)
    expect(api.SetConfig.mock.calls[0][0]).toEqual({
      text: '🍜 干饭去了\n一小时后回', fontSize: 64, screen: '', hotkey: 'Ctrl+Alt+B', everyScreen: true,
    })
    expect(wrapper.text()).toContain('已保存')
    wrapper.unmount()
  })

  it('SetConfig 失败（热键占用回滚）：错误可见，且配置以服务端回读为准', async () => {
    api.SetConfig.mockRejectedValue(new Error('热键 "Ctrl+Alt+J" 注册失败（可能已被其它程序占用）'))
    api.GetConfig.mockResolvedValue(config({ hotkey: 'Ctrl+Alt+B' })) // 服务端已回滚旧键
    const wrapper = await mountView()

    await wrapper.find('.mb-hotkey').setValue('Ctrl+Alt+J')
    await wrapper.find('.action-row .btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('注册失败')
    expect((wrapper.find('.mb-hotkey').element as HTMLInputElement).value).toBe('Ctrl+Alt+B')
    wrapper.unmount()
  })

  it('挂/撤按钮调 Toggle；msgboard:changed 事件刷新状态徽标', async () => {
    const wrapper = await mountView(status({ shown: false }))
    await wrapper.find('.action-row .btn-primary').trigger('click')
    expect(api.Toggle).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('立即挂牌')

    api.GetStatus.mockResolvedValue(status({ shown: true, keepAwake: true }))
    runtime.handlers['msgboard:changed']({ data: undefined })
    await flushPromises()
    expect(wrapper.text()).toContain('已挂牌')
    expect(wrapper.text()).toContain('撤下留言牌')
    // 正常防休眠不再上 chip（只有失败才警示）
    expect(wrapper.text()).not.toContain('防休眠')
    wrapper.unmount()
  })

  it('热键未在位：警示 chip + 折叠区横幅 + 高级设置异常旗标', async () => {
    const wrapper = await mountView(status({ hotkeyActive: false }))
    expect(wrapper.text()).toContain('热键未在位')
    expect(wrapper.find('.adv-flag').exists()).toBe(true)
    expect(wrapper.find('.banner-warn').text()).toContain('已被其它程序抢占')
    wrapper.unmount()
  })

  it('正文超上限：保存按钮本地拦截并报错，不往返后端', async () => {
    const wrapper = await mountView()
    await wrapper.find('#mb-text').setValue('长'.repeat(401))
    await wrapper.find('.action-row .btn-secondary').trigger('click')
    await flushPromises()
    expect(api.SetConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('超出上限')
    wrapper.unmount()
  })
})
