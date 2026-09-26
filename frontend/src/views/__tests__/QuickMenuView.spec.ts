// 快捷菜单模块页特征测试：状态 chip、条目编辑面可见性、空态与"前往设置"导航事件契约。
// N5-C1 后页面内嵌共享编辑面 TrayItemsEditor（走 AppService 托盘 RPC），按测试 seam
// 约定补 app 绑定打桩——仅基础设施，断言零改动。
// 重设计批次：语义断言（RPC 链/两段式/导航事件）零改动；两处纯结构选择器按新 DOM
// 更新——①阈值参数由 .subtitle 长句改为页头 .qm-params chip 行；②"前往设置页配置"
// 大钮降为编辑面板页脚链接 .qm-settings-link（navigate 契约与断言语义不变）。
// 另补双栏主区与状态区渲染两条新结构断言。
// v3 布局批：「当前条目」镜像面板整块删除，条目可见性/空态两条用例的选择器与文案
// 迁到编辑面与轮盘舱宿主（语义"条目名+机器值+类型可见"与"空态引导+navigate 契约"
// 不动）；页头 chip 改绑确认值 refs，触发参数用例顺带钉住"应用后页头回显"回归。
// GetTriggerConfig 陈旧桩删除（页面从不调用，回显走 GetStatus）。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QuickMenuView from '../QuickMenuView.vue'
import { WHEEL_SKIN_STORAGE_KEY } from '../../components/quickmenu/wheelSkin'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListItems: vi.fn().mockResolvedValue([]),
  SetTriggerConfig: vi.fn().mockResolvedValue([450, 16]),
  // 皮肤双源打桩：默认"后端不可达"（reject）→ 存量用例全走 localStorage 镜像降级；
  // 真相链路（fetch 覆写 / commit 上账）由皮肤专测 Once 开闸。
  GetSkin: vi.fn().mockRejectedValue(new Error('测试打桩：后端皮肤通道未开')),
  SetSkin: vi.fn().mockRejectedValue(new Error('测试打桩：后端皮肤通道未开')),
}))

const traySvc = vi.hoisted(() => ({
  ListTrayMenuOptions: vi.fn().mockResolvedValue([]),
  GetTrayMenu: vi.fn().mockResolvedValue([]),
  SetTrayMenu: vi.fn().mockResolvedValue(undefined),
  PickExeFile: vi.fn().mockResolvedValue(''),
}))

vi.mock('../../../bindings/hanxi/internal/modules/quickmenu', () => ({
  QuickMenuService: svc,
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: traySvc }))

const status = { trapActive: true, holdMs: 450, moveTol: 16, itemCount: 2 }
const items = [
  { index: 0, label: 'Snipaste', type: 'exe', hint: 'D:\\tools\\Snipaste.exe' },
  { index: 1, label: '启动 Everything', type: 'command', hint: 'everything/launch' },
]

function flushMicrotasks(times = 20) {
  return (async () => { for (let i = 0; i < times; i++) await Promise.resolve() })()
}

const Host = defineComponent({ render: () => h(QuickMenuView) })

async function mountReady(st = status, list = items) {
  svc.GetStatus.mockResolvedValue(st)
  svc.ListItems.mockResolvedValue(list)
  const w = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return w
}

beforeEach(() => localStorage.clear())

afterEach(() => {
  vi.clearAllMocks()
  svc.GetStatus.mockResolvedValue(status)
  svc.ListItems.mockResolvedValue(items)
  svc.GetSkin.mockRejectedValue(new Error('测试打桩：后端皮肤通道未开'))
  svc.SetSkin.mockRejectedValue(new Error('测试打桩：后端皮肤通道未开'))
  traySvc.ListTrayMenuOptions.mockResolvedValue([])
  traySvc.GetTrayMenu.mockResolvedValue([])
  localStorage.clear()
})

describe('QuickMenuView', () => {
  it('钩子在位显示正向 chip，并展示阈值参数', async () => {
    const w = await mountReady()
    // N6-C2：只读页也挂同源预览盘（条目数=扇区数）
    expect(w.findAll('.wp-sector').length).toBe(2)
    expect(w.find('.chip').classes()).toContain('chip-positive')
    expect(w.find('.chip').text()).toBe('监听在位')
    // 重设计：触发时长/位移容差 chip 化进页头状态区，数值仍如实回显
    const params = w.find('.qm-params')
    expect(params.text()).toContain('450')
    expect(params.text()).toContain('16')
    expect(params.findAll('.qm-param').length).toBe(4)
    w.unmount()
  })

  it('重设计状态区：页头渲染参数一览，双栏主区左编辑右预览', async () => {
    const w = await mountReady()
    // 状态区骨架：.qm-state 内先 h1+状态 chip 再参数 chip 行
    const state = w.find('.qm-state')
    expect(state.exists()).toBe(true)
    expect(state.find('h1').text()).toBe('快捷菜单')
    expect(state.find('.qm-params').exists()).toBe(true)
    // 双栏主区：左=条目编辑主场（挂 TrayItemsEditor），右=轮盘预览 sticky 栏
    const main = w.find('.qm-main')
    expect(main.exists()).toBe(true)
    expect(main.find('.qm-edit-col').exists()).toBe(true)
    expect(main.find('.qm-side-col').exists()).toBe(true)
    expect(main.find('.qm-edit-col .tray-editor').exists()).toBe(true)
    expect(main.find('.qm-side-col .wp').exists()).toBe(true)
    // 行为设置与使用说明降为次级折叠区
    expect(w.findAll('.qm-secondary details').length).toBe(2)
    w.unmount()
  })

  // v3 窄档换形回归（机主 628×940 截图批复）：happy-dom 回落口径=容器宽 0（RO 空
  // 实现、CSS.supports 恒真）→ 窄档。断言结构序与紧凑舱形态；CSS 侧 @container
  // 换形 happy-dom 不排版，按回落逻辑以 DOM 序+盘形态为准。
  it('窄档结构序：编辑主场在预览舱之前，盘面走 trim 满格小档', async () => {
    const w = await mountReady()
    const kids = w.find('.qm-main').element.children
    expect(kids[0].classList.contains('qm-edit-col')).toBe(true)
    expect(kids[1].classList.contains('qm-side-col')).toBe(true)
    const wp = w.find('.wp')
    expect(wp.classes()).toContain('wp-trim')
    expect(wp.attributes('style')).toContain('--wp-scale: 0.56') // 盒 179px ≤180 上限
    // viewBox 收进 [82,430]：盘面含描边+安全边满格，512 全窗死边归零，杜绝切边/打架
    expect(w.find('.wp-svg').attributes('viewBox')).toBe('82 82 348 348')
    w.unmount()
  })

  it('钩子未启用显示警示 chip（文字状态不靠颜色单传）', async () => {
    const w = await mountReady({ ...status, trapActive: false })
    expect(w.find('.chip').classes()).toContain('chip-warning')
    expect(w.find('.chip').text()).toBe('钩子未启用')
    w.unmount()
  })

  // v3 布局批：「当前条目」只读镜像面板删除，"条目名+机器值+类型可见"改由左列
  // 编辑面（同源共享组件 TrayItemsEditor，账目就是那份 TrayMenu）承担——选择器
  // 与文案随宿主迁移，断言语义不变。
  it('条目可见性在编辑面：名称、机器值与类型标记', async () => {
    traySvc.GetTrayMenu.mockResolvedValue([
      { type: 'exe', ref: '', path: 'D:\\tools\\Snipaste.exe', args: '', label: 'Snipaste', enabled: true },
      { type: 'command', ref: 'everything/launch', path: '', args: '', label: '', enabled: true },
    ])
    const w = await mountReady()
    const rows = w.findAll('.tray-row')
    expect(rows).toHaveLength(2)
    expect((rows[0].find('.tray-name').element as HTMLInputElement).value).toBe('Snipaste')
    expect(rows[0].find('.tray-ref').text()).toBe('D:\\tools\\Snipaste.exe')
    expect(rows[1].find('.tray-tag').text()).toBe('工具命令')
    expect(rows[1].find('.tray-ref').text()).toBe('everything/launch')
    w.unmount()
  })

  it('空条目走占位引导，"前往设置页配置"直达设置·托盘菜单分区', async () => {
    const w = await mountReady(status, [])
    // v3：空态引导两处自然在位——轮盘舱空盘示意 + 编辑面自带引导（镜像面板已删）
    expect(w.find('.wp-empty').text()).toContain('还没有条目')
    expect(w.find('.qm-edit-col .tray-editor > .state-box').text()).toContain('尚未配置托盘条目')
    // 重设计：跳设置由大钮降为编辑面板页脚链接，navigate 事件契约不变
    await w.find('.qm-settings-link').trigger('click')
    // Host 包裹渲染下 emit 挂在子组件 wrapper 上
    expect(w.findComponent(QuickMenuView).emitted('navigate')?.[0]).toEqual(['/settings/tray'])
    w.unmount()
  })

  it('加载失败显示错误与重试按钮', async () => {
    svc.GetStatus.mockRejectedValue(new Error('模块未初始化'))
    const w = mount(Host)
    await flushMicrotasks()
    expect(w.find('.state-error').text()).toContain('模块未初始化')
    expect(w.find('.state-box .btn')).toBeTruthy()
    w.unmount()
  })
})

// N5-C2 触发参数：草稿-应用两段式，回显以钳后返回值为准。
describe('触发参数（N5-C2）', () => {
  it('出厂回显 450/16，未改动时应用钮禁用', async () => {
    const w = await mountReady()
    const hold = w.find('input[aria-label="长按时长毫秒"]')
    const move = w.find('input[aria-label="位移容差像素"]')
    expect((hold.element as HTMLInputElement).value).toBe('450')
    expect((move.element as HTMLInputElement).value).toBe('16')
    const btn = w.findAll('button').find((b) => b.text().includes('应用'))!
    expect(btn.attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('改草稿→应用：RPC 带草稿值，回显取钳后返回；后端拒绝时回滚草稿', async () => {
    const w = await mountReady()
    await w.find('input[aria-label="长按时长毫秒"]').setValue('300')
    const btn = w.findAll('button').find((b) => b.text().includes('应用'))!
    expect(btn.attributes('disabled')).toBeUndefined()
    svc.SetTriggerConfig.mockResolvedValueOnce([300, 16])
    await btn.trigger('click')
    await flushMicrotasks()
    expect(svc.SetTriggerConfig).toHaveBeenCalledWith(300, 16)
    expect((w.find('input[aria-label=\"长按时长毫秒\"]').element as HTMLInputElement).value).toBe('300')
    // v3 T1 回归钉：应用后页头参数章同步回显钳后值（chip 曾直读状态快照不回写，说谎）
    expect(w.find('.qm-params').text()).toContain('300ms')

    svc.SetTriggerConfig.mockRejectedValueOnce(new Error('配置存储不可用'))
    await w.find('input[aria-label="长按时长毫秒"]').setValue('80')
    await w.findAll('button').find((b) => b.text().includes('应用'))!.trigger('click')
    await flushMicrotasks()
    // 失败不私留假状态：整页切错误态并如实报错，重试回读后草稿回到确认值 450
    expect(w.text()).toContain('配置存储不可用')
    await w.find('.state-box .btn').trigger('click')
    await flushMicrotasks()
    expect((w.find('input[aria-label="长按时长毫秒"]').element as HTMLInputElement).value).toBe('450')
    w.unmount()
  })
})

// 皮肤双源（收权批）：后端为真相、localStorage 为镜像降级。
describe('轮盘皮肤双源（后端真相 + 镜像兜底）', () => {
  it('后端可达：初值走 GetSkin（整数百分比 → 0–1），非本地镜像', async () => {
    // 镜像预埋一份 ink，但后端返回 veil → 真相应覆盖陈旧镜像（NAS 换机场景）
    localStorage.setItem(WHEEL_SKIN_STORAGE_KEY, JSON.stringify({ preset: 'ink' }))
    svc.GetSkin.mockResolvedValue({ preset: 'veil', faceAlpha: 60, stroke: 30, followModuleColor: true })
    const w = await mountReady()
    const skinSel = w.find('.qm-skin-panel')
    expect(skinSel.exists()).toBe(true)
    // veil 预设 chip（索引 1）应为选中态
    const chips = skinSel.findAll('.qm-skin-chip')
    expect(chips[1].classes()).toContain('is-on')
    // 百分比回化：veilPercent=1-0.6=40%、strokePercent=30%
    expect(skinSel.text()).toContain('40%')
    expect(skinSel.text()).toContain('30%')
    // 真相回填镜像（供弹窗降级/换窗读取）
    expect(JSON.parse(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY)!).preset).toBe('veil')
    w.unmount()
  })

  it('后端不可达：初值降级读镜像，不炸面板', async () => {
    // GetSkin 默认打桩为 reject（见 afterEach 重置）→ 走镜像
    localStorage.setItem(WHEEL_SKIN_STORAGE_KEY, JSON.stringify({ preset: 'ink', faceAlpha: 0.5, stroke: 0.8 }))
    const w = await mountReady()
    const chips = w.findAll('.qm-skin-chip')
    expect(chips[2].classes()).toContain('is-on') // ink
    w.unmount()
  })

  it('切换预设：乐观先行上镜像，随后以 SetSkin 整数百分比线格式上后端', async () => {
    svc.SetSkin.mockResolvedValue({ preset: 'ink', faceAlpha: 100, stroke: 55, followModuleColor: false })
    const w = await mountReady()
    await w.findAll('.qm-skin-chip')[2].trigger('click') // 点 ink
    await flushMicrotasks()
    // 线格式：DEFAULT stroke 0.55→55、faceAlpha 1→100，预设 ink
    expect(svc.SetSkin).toHaveBeenCalledWith({ preset: 'ink', faceAlpha: 100, stroke: 55, followModuleColor: false })
    expect(JSON.parse(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY)!).preset).toBe('ink')
    w.unmount()
  })

  it('SetSkin 后端拒绝：如实提示但页面不整片切错误态（皮肤是纯视觉账），本地镜像保留', async () => {
    // SetSkin 默认 reject（afterEach 重置）
    const w = await mountReady()
    await w.findAll('.qm-skin-chip')[2].trigger('click')
    await flushMicrotasks()
    // 无 .state-error 劫持（编辑器/预览仍在），乐观 ink 保留在镜像
    expect(w.find('.state-error').exists()).toBe(false)
    expect(w.find('.qm-main').exists()).toBe(true)
    expect(JSON.parse(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY)!).preset).toBe('ink')
    w.unmount()
  })

  it('两段式滑杆：input（连拖）只动镜像不发 RPC，change（落定）才上账；回显覆镜像', async () => {
    svc.SetSkin.mockResolvedValue({ preset: 'frost', faceAlpha: 40, stroke: 55, followModuleColor: false })
    const w = await mountReady()
    const veil = w.find('input[aria-label="盘面透明度百分比"]')
    // 拖拽中段：只发 input（setSkinLocal 写镜像，不发 RPC）。不用 setValue——
    // 它会连发 change，跳过分段语义直达提交，测不到"落定才上账"。
    ;(veil.element as HTMLInputElement).value = '60'
    await veil.trigger('input') // 透纱 60% → faceAlpha 0.4
    expect(svc.SetSkin).not.toHaveBeenCalled()
    expect(JSON.parse(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY)!).faceAlpha).toBeCloseTo(0.4)
    await veil.trigger('change') // 松手落定 → 一笔上后端（整数百分比线格式）
    await flushMicrotasks()
    expect(svc.SetSkin).toHaveBeenCalledWith(expect.objectContaining({ preset: 'frost', faceAlpha: 40 }))
    // 后端回显（钳域归正版）覆写本地与镜像：faceAlpha 落 0.4 → 面板仍显 60%
    expect(JSON.parse(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY)!).faceAlpha).toBeCloseTo(0.4)
    expect(w.find('.qm-skin-panel').text()).toContain('60%')
    w.unmount()
  })
})
