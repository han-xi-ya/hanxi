// 快捷菜单轮盘特征测试：512 双环（扇区级联外扩）视图的条目渲染、子环时序状态机、
// 外甩取消、分级 Esc 与键盘收起契约。
// 2026-09 列表版 → 轮盘版 → 双环版：主盘常驻不换层；分组悬停 dwell 到点在外圈
// 帽带展开子环（useWheelRingState），点击分组钉住；子环/叶子派发 Launch(path)。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuickMenuPopup from '../QuickMenuPopup.vue'
import type { MenuItem } from '../../../bindings/hanxi/internal/modules/quickmenu/models'

const svc = vi.hoisted(() => ({
  ListItems: vi.fn().mockResolvedValue([]),
  Launch: vi.fn().mockResolvedValue(undefined),
  Dismiss: vi.fn().mockResolvedValue(undefined),
  OpenSettings: vi.fn().mockResolvedValue(undefined),
}))
const useWailsEventMock = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/hanxi/internal/modules/quickmenu', () => ({
  QuickMenuService: svc,
}))
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: useWailsEventMock,
}))

// 绑定层 MenuItem 为全字段类型（icon/children 恒在），夹具经工厂补齐与后端形态一致
const leaf = (index: number, label: string, type: string, hint: string, children: MenuItem[] | null = null): MenuItem =>
  ({ index, label, type, hint, icon: '', children })

const items: MenuItem[] = [
  leaf(0, 'Snipaste', 'exe', 'D:\\tools\\Snipaste.exe'),
  leaf(1, '启动 Everything', 'command', 'everything/launch'),
  leaf(2, '局域网扫描', 'route', '/ext/lan'),
]

// 双环夹具：index 1 为分组（托盘/轮盘共用配置中的 group 节点），子条目带自身索引
const itemsWithGroup: MenuItem[] = [
  items[0],
  leaf(1, '搜索工具', 'group', '', [
    leaf(0, 'Everything', 'command', 'everything/start'),
    leaf(1, 'Everything 托盘', 'command', 'everything/tray'),
  ]),
  items[2],
]

// 极坐标（视图内 pointermove 换算的镜像）：0°=12 点、顺时针；圆心 (256,256)。
function polarPoint(r: number, deg: number) {
  const rad = ((deg - 90) * Math.PI) / 180
  return { clientX: 256 + r * Math.cos(rad), clientY: 256 + r * Math.sin(rad) }
}
/** 主环第 i 枚（共 n）扇区带中线悬停点 */
function mainPoint(i: number, n: number) {
  return polarPoint(118, (360 / n) * (i + 0.5))
}

function flushMicrotasks(times = 20) {
  return (async () => { for (let i = 0; i < times; i++) await Promise.resolve() })()
}

const Host = defineComponent({ render: () => h(QuickMenuPopup) })

async function mountReady(list = items) {
  svc.ListItems.mockResolvedValue(list)
  const w = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return w
}

afterEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
  svc.ListItems.mockResolvedValue([])
  svc.Launch.mockResolvedValue(undefined)
  svc.Dismiss.mockResolvedValue(undefined)
  svc.OpenSettings.mockResolvedValue(undefined)
})

describe('QuickMenuPopup 主环', () => {
  it('挂载即拉取条目，扇区按钮渲染名称与类型标记（aria-label）', async () => {
    const w = await mountReady()
    expect(svc.ListItems).toHaveBeenCalled()
    const sectors = w.findAll('.sector-btn')
    expect(sectors).toHaveLength(3)
    // ≤8 项时图标下方带短名
    expect(sectors[0].find('.sector-name').text()).toBe('Snipaste')
    // 类型标记收进可访问性标签（exe/command/route → 程序/命令/页面）
    expect(sectors[0].attributes('aria-label')).toBe('Snipaste（程序）')
    expect(sectors[1].attributes('aria-label')).toBe('启动 Everything（命令）')
    expect(sectors[2].attributes('aria-label')).toBe('局域网扫描（页面）')
    w.unmount()
  })

  it('点击叶子扇区按钮按一元路径回调 Launch', async () => {
    const w = await mountReady()
    await w.findAll('.sector-btn')[1].trigger('click')
    expect(svc.Launch).toHaveBeenCalledWith([1])
    w.unmount()
  })

  it('分组扇区带 haspopup/expanded 语义，悬停读数标"分组"', async () => {
    const w = await mountReady(itemsWithGroup)
    const groupBtn = w.findAll('.sector-btn')[1]
    expect(groupBtn.attributes('aria-haspopup')).toBe('true')
    expect(groupBtn.attributes('aria-expanded')).toBe('false')
    expect(groupBtn.attributes('aria-label')).toBe('搜索工具（分组，展开子环）')
    w.unmount()
  })

  it('空条目 hub 走引导态，"配置条目"打开设置页', async () => {
    const w = await mountReady([])
    expect(w.find('.hub').text()).toContain('暂无条目')
    await w.find('.hub-action').trigger('click')
    expect(svc.OpenSettings).toHaveBeenCalled()
    w.unmount()
  })

  it('加载失败 hub 显示错误与重试，重试成功后恢复轮盘', async () => {
    svc.ListItems.mockRejectedValueOnce(new Error('服务未就绪'))
    const w = mount(Host)
    await flushMicrotasks()
    expect(w.find('.hub-state-error').text()).toBe('加载失败')
    svc.ListItems.mockResolvedValue(items)
    await w.find('.hub-action').trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.sector-btn')).toHaveLength(3)
    w.unmount()
  })

  it('Esc 收起弹窗', async () => {
    const w = await mountReady()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(svc.Dismiss).toHaveBeenCalled()
    w.unmount()
  })

  it('点击中心 hub 收起弹窗（中心核动作）', async () => {
    const w = await mountReady()
    await w.find('.hub-hit').trigger('click')
    expect(svc.Dismiss).toHaveBeenCalled()
    w.unmount()
  })

  it('订阅 quickmenu:opening：每次唤出重拉条目（配置热更新）', async () => {
    const w = await mountReady()
    const call = useWailsEventMock.mock.calls.find(([name]) => name === 'quickmenu:opening')
    expect(call).toBeTruthy()
    svc.ListItems.mockClear()
    ;(call![1] as () => void)()
    await flushMicrotasks()
    expect(svc.ListItems).toHaveBeenCalled()
    w.unmount()
  })
})

describe('N6 牌面重做（F1-F4）', () => {
  it('密度变量随条目数下发（3 项走 ≤6 宽裕档 64px 按钮宽）', async () => {
    const w = await mountReady()
    expect(w.find('.popup').attributes('style')).toContain('--d-btn-w: 64px')
    w.unmount()
  })
  it('分组扇区常挂 ▸ 角标（可展开从意外变预告），叶子无角标', async () => {
    const w = await mountReady(itemsWithGroup)
    const btns = w.findAll('.sector-btn')
    expect(btns[0].find('.sector-caret').exists()).toBe(false)
    expect(btns[1].find('.sector-caret').text()).toContain('▸')
    w.unmount()
  })
  it('悬停扇区面：is-active 与径向外顶内联样式同步下发', async () => {
    const w = await mountReady()
    await w.findAll('.sector')[0].trigger('mouseenter')
    const path = w.findAll('.sector')[0]
    expect(path.classes()).toContain('is-active')
    // 首扇区中线 60°（3 项制）：外顶向量应为有限数值 translate
    expect(path.attributes('style') ?? '').toContain('translate(')
    w.unmount()
  })
})

describe('外扩子环（扇区级联）', () => {
  it('悬停分组扇区驻留满 dwell 后子环帽带展开，未到点不展开', async () => {
    vi.useFakeTimers()
    const w = await mountReady(itemsWithGroup)
    await w.find('.popup').trigger('pointermove', mainPoint(1, 3))
    await flushMicrotasks()
    vi.advanceTimersByTime(119)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(0)
    vi.advanceTimersByTime(1)
    await flushMicrotasks()
    const caps = w.findAll('.cap-btn')
    expect(caps.map((b) => b.find('.cap-name').text())).toEqual(['Everything', 'Everything 托盘'])
    expect(w.findAll('.sector-btn')[1].attributes('aria-expanded')).toBe('true')
    w.unmount()
  })

  it('子环扇区点击按 [父组索引, 子项索引] 二元路径派发', async () => {
    vi.useFakeTimers()
    const w = await mountReady(itemsWithGroup)
    await w.find('.popup').trigger('pointermove', mainPoint(1, 3))
    vi.advanceTimersByTime(120)
    await flushMicrotasks()
    await w.findAll('.cap-btn')[1].trigger('click')
    expect(svc.Launch).toHaveBeenCalledWith([1, 1])
    w.unmount()
  })

  it('离开保持区满 release 迟滞才收起；中途扫回撤销收起（防抖）', async () => {
    vi.useFakeTimers()
    const w = await mountReady(itemsWithGroup)
    await w.find('.popup').trigger('pointermove', mainPoint(1, 3))
    vi.advanceTimersByTime(120)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(2)
    // 扫出到 Snipaste 扇区（异组保持区 → 对新目标重起 dwell，旧环暂留）
    await w.find('.popup').trigger('pointermove', mainPoint(0, 3))
    vi.advanceTimersByTime(179)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(2) // 迟滞窗口未走满
    vi.advanceTimersByTime(1)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(0) // 到点收起（叶子组无开环意图）
    w.unmount()
  })

  it('指针进入帽带视为保持：跨出父楔形角范围不收起', async () => {
    vi.useFakeTimers()
    const w = await mountReady(itemsWithGroup)
    await w.find('.popup').trigger('pointermove', mainPoint(1, 3))
    vi.advanceTimersByTime(120)
    await flushMicrotasks()
    // 帽带第一枚锚点角 ≈ 180° − 60° + 12° = 132°（span=120 三等分槽取中心）
    await w.find('.popup').trigger('pointermove', polarPoint(205, 130))
    vi.advanceTimersByTime(600)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(2) // 仍在保持区，不收
    w.unmount()
  })

  it('点击分组钉住开环：离开保持区到点仍保留；再点解除并收起', async () => {
    vi.useFakeTimers()
    const w = await mountReady(itemsWithGroup)
    await w.findAll('.sector-btn')[1].trigger('click') // 未开 → 立即开并钉住
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(2)
    await w.find('.popup').trigger('pointermove', mainPoint(0, 3)) // 离开钉住组
    vi.advanceTimersByTime(400)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(2) // 钉住不收
    await w.findAll('.sector-btn')[1].trigger('click') // 再点：解除并收
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(0)
    w.unmount()
  })

  it('子条目超 8 截断呈现并在 hub 提示', async () => {
    vi.useFakeTimers()
    const many = Array.from({ length: 10 }, (_, i) => leaf(i, `子${i}`, 'command', ''))
    const w = await mountReady([
      items[0],
      leaf(1, '大组', 'group', '', many),
      items[2],
    ])
    await w.find('.popup').trigger('pointermove', mainPoint(1, 3))
    vi.advanceTimersByTime(120)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(8)
    expect(w.find('.hub').text()).toContain('仅显示前 8')
    w.unmount()
  })

  it('Esc 分级：子环展开先收子环（不关窗），再按收起', async () => {
    const w = await mountReady(itemsWithGroup)
    await w.findAll('.sector-btn')[1].trigger('click') // 钉住开环
    await flushMicrotasks()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(0)
    expect(svc.Dismiss).not.toHaveBeenCalled()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(svc.Dismiss).toHaveBeenCalled()
    w.unmount()
  })

  it('外甩取消态：越过 rCancel 加 is-cancel 类，滑回缓冲带内解除', async () => {
    const w = await mountReady(itemsWithGroup)
    await w.find('.popup').trigger('pointermove', polarPoint(250, 0)) // 12 点方向 r=250>244
    await flushMicrotasks()
    expect(w.find('.popup').classes()).toContain('is-cancel')
    await w.find('.popup').trigger('pointermove', polarPoint(200, 0)) // 收回帽带内
    await flushMicrotasks()
    expect(w.find('.popup').classes()).not.toContain('is-cancel')
    w.unmount()
  })

  it('拍平形态（后端已拍平，无 children）不产生任何子环节码', async () => {
    vi.useFakeTimers()
    const w = await mountReady(items) // 全部叶子
    await w.find('.popup').trigger('pointermove', mainPoint(1, 3))
    vi.advanceTimersByTime(500)
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(0)
    expect(w.findAll('.cap')).toHaveLength(0)
    w.unmount()
  })

  it('每次唤出 reset：openGroup/钉住态跨会话清零', async () => {
    const w = await mountReady(itemsWithGroup)
    await w.findAll('.sector-btn')[1].trigger('click') // 钉住开环
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(2)
    svc.ListItems.mockClear()
    const call = useWailsEventMock.mock.calls.find(([name]) => name === 'quickmenu:opening')
    ;(call![1] as () => void)()
    await flushMicrotasks()
    expect(w.findAll('.cap-btn')).toHaveLength(0) // 唤出回主环
    w.unmount()
  })
})
