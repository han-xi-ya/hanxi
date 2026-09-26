// 桌面留言板模块页（MsgBoardView，N6 重设计 + v2 主角化 + v3 流体三柱 + v4 所见
// 即所得牌桌）特征测试：加载回显、类型速挂（单击填词/双击保存+挂出）、保存走
// SetConfig 全量快照、保存失败回读不私留假状态、挂/撤与状态事件、异常 chip 只在
// 异常时出现、预览与表单同源联动；N30 追加全屏预览（纯前端浮层，不触后端）与
// 舞台按牌宽防裁切缩放。
// 重设计 v4（牌桌：左舞台/右操作栏）：语义断言零删减、逐条映射——
//   .mb-hero 由 v3 左簇控制卡变为**牌面舞台整卡**（.mb-toolbar 40px 工具条收纳
//     状态字/挂撤大钮/全屏预览，.mb-echo 回显栏收纳已存牌面回显与脏态——
//     "状态+主操作+回显同区聚合"语义原样，类名 status-word/hero-cta/hero-echo-*
//     零迁移）；
//   v3 的 .mb-rail（左簇）语义翻转为**右侧窄操作栏**：#mb-text（正文行）、
//     类型速挂（.mb-types/.type-btn/.type-grid）、字号滑杆、everyScreen/目标屏/
//     热键行、保存行（.work-actions）、契约折叠、adv-flag 异常旗标全部聚合其中；
//   .mb-work/.mb-stage/.mb-draft 聚合层随骨架消亡：同源联动断言改挂
//     .mb-hero .mbp-scaled（舞台大牌）与 .mb-rail #mb-text，"全页牌面画法唯一"
//     （.bc-card 计数 1）语义不变；
//   字号控件 range 档与 24–200 钳位契约原样（挂在操作栏）；舞台缩放上限升为 1.0
//     （能 1:1 就 1:1，实测卡宽替代猜测），happy-dom 下 ResizeObserver 为空实现、
//     回落 300 基准盒 + 字号×13 兜底线——64px 时 scale(0.34615…)、120px 时
//     scale(0.18461538461538463)（与 v3 断言逐位一致）、文案「预览缩到 18%，真牌
//     大 5 倍」原样；
//   v3"参数配置"details 折叠取消（一行一控件全展开），异常汇总旗标 .adv-flag
//     上移到操作栏头部 chip，banner-warn 热键横幅语义原样。
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
    await wrapper.find('.work-actions .btn-secondary').trigger('click')
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
    await wrapper.find('.work-actions .btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('注册失败')
    expect((wrapper.find('.mb-hotkey').element as HTMLInputElement).value).toBe('Ctrl+Alt+B')
    wrapper.unmount()
  })

  it('挂/撤按钮调 Toggle；msgboard:changed 事件刷新状态徽标', async () => {
    const wrapper = await mountView(status({ shown: false }))
    await wrapper.find('.mb-hero .btn-primary').trigger('click')
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
    await wrapper.find('.work-actions .btn-secondary').trigger('click')
    await flushPromises()
    expect(api.SetConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('超出上限')
    wrapper.unmount()
  })
})

describe('重设计 v4 骨架（所见即所得的牌桌）', () => {
  it('两区聚合：右操作栏收纳正文/速挂/字号/配置全部控件，左舞台收纳大牌', async () => {
    const wrapper = await mountView(
      status({ shown: true, keepAwake: true }),
      config({ text: '🤝 开会中\n请勿打扰' }),
    )
    const rail = wrapper.find('.mb-rail')
    expect(rail.exists()).toBe(true)
    // 编辑三件套（正文/速挂/字号）+ 低频配置（多屏/热键）全在操作栏——
    // v3 左簇的"高频控件同一聚合"语义翻转成"右栏一控件一行"
    expect(rail.find('#mb-text').exists()).toBe(true)
    expect(rail.find('.mb-types .type-grid').exists()).toBe(true)
    expect(rail.find('input[aria-label="多屏同时挂牌"]').exists()).toBe(true)
    expect(rail.find('.mb-hotkey').exists()).toBe(true)
    // 舞台（.mb-hero）含工具条与大牌盒，控件绝不在舞台里再摆一份
    const hero = wrapper.find('.mb-hero')
    expect(hero.find('.mb-toolbar .status-word').exists()).toBe(true)
    expect(hero.find('.mbp-box .mbp-scaled').exists()).toBe(true)
    // 字号滑杆在操作栏（改一下即时落在大牌上）；range 档仍吃 24–200 钳位契约
    const range = rail.find('input[aria-label="字号"]')
    expect(range.exists()).toBe(true)
    expect(range.attributes('type')).toBe('range')
    expect(range.attributes('min')).toBe('24')
    expect(range.attributes('max')).toBe('200')
    wrapper.unmount()
  })

  it('主操作区存在：状态字、挂撤大钮与已存牌面回显聚在同一张牌桌主位（hero）卡', async () => {
    const wrapper = await mountView(
      status({ shown: true, keepAwake: true }),
      config({ text: '🤝 开会中\n请勿打扰' }),
    )
    const hero = wrapper.find('.mb-hero')
    expect(hero.exists()).toBe(true)
    expect(hero.find('.status-word').text()).toBe('已挂牌')
    expect(hero.find('.hero-cta').classes()).toContain('btn-danger-outline')
    expect(hero.find('.hero-cta').text()).toContain('撤下留言牌')
    // 回显只报已存牌面的真话（首行主题 + 副行）
    expect(hero.find('.hero-echo-main').text()).toBe('🤝 开会中')
    expect(hero.find('.hero-echo-sub').text()).toBe('请勿打扰')
    wrapper.unmount()
  })

  it('草稿与大牌同源：编辑框在操作栏、牌面在舞台，页内只此一块牌面画法', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('.mb-rail #mb-text').exists()).toBe(true)
    expect(wrapper.find('.mb-hero .mbp-scaled .bc-title').text()).toBe('马上回来')
    // 回显栏是纯文本非第二块 BoardCard——全页牌面画法唯一（防"两处预览各画各的"回潮）
    expect(wrapper.findAll('.bc-card')).toHaveLength(1)
    wrapper.unmount()
  })
})

describe('全屏预览与小预览缩放（N30）', () => {
  // 浮层经 Teleport 落在 body 上，wrapper.find 够不着——按仓库 Teleport 测试
  // 惯例直接查 document。
  const fullLayer = () => document.body.querySelector<HTMLElement>('.mbp-full')
  const openBtn = (w: Awaited<ReturnType<typeof mountView>>) =>
    w.findAll('button').find((b) => b.text().includes('全屏预览'))!

  it('默认字号 64 时舞台按兜底卡宽缩放；字号翻倍后按 288px 容纳线收缩', async () => {
    const wrapper = await mountView()
    const scaled = wrapper.find('.mbp-scaled')
    // happy-dom 无 RO 实测：回落 300 基准盒 + 字号×13 兜底宽——(300−12)/(64×13)≈0.346
    expect(scaled.attributes('style')).toContain('scale(0.34615384615384615)')
    await wrapper.find('input[aria-label="字号"]').setValue(120)
    // 兜底卡宽＝字号×13，越过 288px 容纳线后缩放＝288/(120×13)≈0.185（与 v3 逐位一致）
    expect(scaled.attributes('style')).toContain('scale(0.18461538461538463)')
    // 机主反馈 2026-09-26：文案改一句大白话「预览缩到 18%，真牌大 5 倍」——
    // 百分比按 scale(0.1846…) 取整，倍数仍走 previewZoom 的 round(1/scale)
    expect(wrapper.find('.mbp-caption').text()).toContain('预览缩到 18%，真牌大 5 倍')
    wrapper.unmount()
  })

  it('点「全屏预览」：body 上浮层渲染同源 BoardCard 与当前草稿，后端零调用', async () => {
    const wrapper = await mountView()
    await wrapper.find('#mb-text').setValue('☕ 去茶水间了\n5 分钟内回来')
    expect(fullLayer()).toBeNull()
    await openBtn(wrapper).trigger('click')
    const layer = fullLayer()
    expect(layer).not.toBeNull()
    expect(layer!.querySelector('.bc-title')?.textContent).toBe('☕ 去茶水间了')
    expect(layer!.querySelector('.bc-sub')?.textContent).toBe('5 分钟内回来')
    // 牌面吃真牌同档字号（内联 style 由 BoardCard 定标链给出）
    expect(layer!.querySelector('.bc-card')?.getAttribute('style')).toContain('font-size: 64px')
    // 纯前端动作：不保存、不挂牌
    expect(api.SetConfig).not.toHaveBeenCalled()
    expect(api.Toggle).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('Esc 与点击浮层任意处都即退；未打开时按 Esc 不误触任何链路', async () => {
    const wrapper = await mountView()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(api.Toggle).not.toHaveBeenCalled()

    await openBtn(wrapper).trigger('click')
    expect(fullLayer()).not.toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(fullLayer()).toBeNull()

    await openBtn(wrapper).trigger('click')
    fullLayer()!.click()
    await flushPromises()
    expect(fullLayer()).toBeNull()
    wrapper.unmount()
  })

  it('浮层打开时编辑草稿即刻反映到牌面（预览联动的是草稿不是已存配置）', async () => {
    const wrapper = await mountView()
    await openBtn(wrapper).trigger('click')
    await wrapper.find('#mb-text').setValue('🤝 开会中')
    await flushPromises()
    expect(fullLayer()!.querySelector('.bc-title')?.textContent).toBe('🤝 开会中')
    wrapper.unmount()
  })

  it('卸载后 Esc 监听随浮层一起收干净（不留僵尸监听）', async () => {
    const wrapper = await mountView()
    await openBtn(wrapper).trigger('click')
    wrapper.unmount()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(fullLayer()).toBeNull()
  })
})
