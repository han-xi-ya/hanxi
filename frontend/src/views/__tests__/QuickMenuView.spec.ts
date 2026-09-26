// 快捷菜单模块页特征测试：状态 chip、条目预览、空态与"前往设置"导航事件契约。
// N5-C1 后页面内嵌共享编辑面 TrayItemsEditor（走 AppService 托盘 RPC），按测试 seam
// 约定补 app 绑定打桩——仅基础设施，断言零改动。
// 重设计批次：语义断言（RPC 链/两段式/导航事件）零改动；两处纯结构选择器按新 DOM
// 更新——①阈值参数由 .subtitle 长句改为页头 .qm-params chip 行；②"前往设置页配置"
// 大钮降为编辑面板页脚链接 .qm-settings-link（navigate 契约与断言语义不变）。
// 另补双栏主区与状态区渲染两条新结构断言。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuickMenuView from '../QuickMenuView.vue'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListItems: vi.fn().mockResolvedValue([]),
  GetTriggerConfig: vi.fn().mockResolvedValue([450, 16]),
  SetTriggerConfig: vi.fn().mockResolvedValue([450, 16]),
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

afterEach(() => {
  vi.clearAllMocks()
  svc.GetStatus.mockResolvedValue(status)
  svc.ListItems.mockResolvedValue(items)
  traySvc.ListTrayMenuOptions.mockResolvedValue([])
  traySvc.GetTrayMenu.mockResolvedValue([])
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

  it('钩子未启用显示警示 chip（文字状态不靠颜色单传）', async () => {
    const w = await mountReady({ ...status, trapActive: false })
    expect(w.find('.chip').classes()).toContain('chip-warning')
    expect(w.find('.chip').text()).toBe('钩子未启用')
    w.unmount()
  })

  it('条目预览含名称、mono 提示与类型标记', async () => {
    const w = await mountReady()
    const rows = w.findAll('.item-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.item-label').text()).toBe('Snipaste')
    expect(rows[0].find('.item-hint').text()).toBe('D:\\tools\\Snipaste.exe')
    expect(rows[1].find('.item-kind').text()).toBe('命令')
    w.unmount()
  })

  it('空条目走占位引导，"前往设置页配置"直达设置·托盘菜单分区', async () => {
    const w = await mountReady(status, [])
    expect(w.find('.empty-state').text()).toContain('尚未配置任何条目')
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
