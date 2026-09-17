// 桌面留言板模块页（MsgBoardView）特征测试：加载回显、预设填词与脏态、
// 保存走 SetConfig 全量快照、保存失败回读不私留假状态、挂/撤按钮与状态事件。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import MsgBoardView from '../MsgBoardView.vue'

const api = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  GetConfig: vi.fn(),
  SetConfig: vi.fn(),
  ListPresets: vi.fn(),
  ListScreens: vi.fn(),
  Toggle: vi.fn(),
  Show: vi.fn(),
  Dismiss: vi.fn(),
  GetBoardContent: vi.fn(),
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
const config = (over = {}) => ({ text: '马上回来', fontSize: 64, screen: '', hotkey: 'Ctrl+Alt+B', ...over })
const screens = [
  { device: '\\\\.\\DISPLAY1', width: 2560, height: 1440, isPrimary: true },
  { device: '\\\\.\\DISPLAY2', width: 1920, height: 1080, isPrimary: false },
]

async function mountView(st = status(), cfg = config()) {
  api.GetStatus.mockResolvedValue(st)
  api.GetConfig.mockResolvedValue(cfg)
  api.ListPresets.mockResolvedValue(['马上回来', '会议中，请勿打扰', '下班了，有事请留言', '请勿动我电脑'])
  api.ListScreens.mockResolvedValue(screens)
  const wrapper = mount(MsgBoardView, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  api.GetStatus.mockResolvedValue(status())
  api.GetConfig.mockResolvedValue(config())
  api.ListPresets.mockResolvedValue(['马上回来', '会议中，请勿打扰'])
  api.ListScreens.mockResolvedValue(screens)
  api.SetConfig.mockResolvedValue(undefined)
  api.Toggle.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

describe('MsgBoardView', () => {
  it('挂载并行拉取状态/配置/预设/显示器并回显表单', async () => {
    const wrapper = await mountView()
    expect((wrapper.find('#mb-text').element as HTMLTextAreaElement).value).toBe('马上回来')
    expect((wrapper.find('.mb-hotkey').element as HTMLInputElement).value).toBe('Ctrl+Alt+B')
    expect(wrapper.text()).toContain('热键在位 Ctrl+Alt+B')
    expect(wrapper.text()).toContain('未挂牌')
    // 下拉只列副屏（主屏走"默认"空值项）
    const options = wrapper.findAll('.select-input option')
    expect(options).toHaveLength(2)
    expect(options[0].attributes('value')).toBe('')
    expect(options[1].text()).toContain('DISPLAY2')
    wrapper.unmount()
  })

  it('点预设填词 → 出现未保存脏态 → 保存把全量快照交给 SetConfig', async () => {
    const wrapper = await mountView()
    const preset = wrapper.findAll('.preset-chip')[1]
    await preset.trigger('click')
    expect((wrapper.find('#mb-text').element as HTMLTextAreaElement).value).toBe('会议中，请勿打扰')
    expect(wrapper.text()).toContain('未保存')

    await wrapper.find('.panel-actions .btn-primary').trigger('click')
    await flushPromises()
    expect(api.SetConfig).toHaveBeenCalledTimes(1)
    expect(api.SetConfig.mock.calls[0][0]).toEqual({
      text: '会议中，请勿打扰', fontSize: 64, screen: '', hotkey: 'Ctrl+Alt+B',
    })
    expect(wrapper.text()).toContain('已保存')
    wrapper.unmount()
  })

  it('SetConfig 失败（热键占用回滚）：错误可见，且配置以服务端回读为准', async () => {
    api.SetConfig.mockRejectedValue(new Error('热键 "Ctrl+Alt+J" 注册失败（可能已被其它程序占用）'))
    api.GetConfig.mockResolvedValue(config({ hotkey: 'Ctrl+Alt+B' })) // 服务端已回滚旧键
    const wrapper = await mountView()

    await wrapper.find('.mb-hotkey').setValue('Ctrl+Alt+J')
    await wrapper.find('.panel-actions .btn-primary').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('注册失败')
    expect((wrapper.find('.mb-hotkey').element as HTMLInputElement).value).toBe('Ctrl+Alt+B')
    wrapper.unmount()
  })

  it('挂/撤按钮调 Toggle；msgboard:changed 事件刷新状态徽标', async () => {
    const wrapper = await mountView(status({ shown: false }))
    await wrapper.find('.panel-actions .btn:last-child').trigger('click')
    expect(api.Toggle).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('立即挂牌')

    api.GetStatus.mockResolvedValue(status({ shown: true, keepAwake: true }))
    runtime.handlers['msgboard:changed']({ data: undefined })
    await flushPromises()
    expect(wrapper.text()).toContain('已挂牌')
    expect(wrapper.text()).toContain('防休眠生效')
    wrapper.unmount()
  })

  it('热键未在位时给警示横幅与黄色徽标', async () => {
    const wrapper = await mountView(status({ hotkeyActive: false }))
    expect(wrapper.text()).toContain('热键未在位')
    expect(wrapper.find('.banner-warn').text()).toContain('已被其它程序抢占')
    wrapper.unmount()
  })

  it('正文超上限：保存按钮本地拦截并报错，不往返后端', async () => {
    const wrapper = await mountView()
    await wrapper.find('#mb-text').setValue('长'.repeat(401))
    await wrapper.find('.panel-actions .btn-primary').trigger('click')
    await flushPromises()
    expect(api.SetConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('超出上限')
    wrapper.unmount()
  })
})
