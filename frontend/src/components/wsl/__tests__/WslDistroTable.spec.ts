// WslDistroTable 行级「⋯ 更多」下拉结构锁（机主实跑反馈整改 N40）+ 复选批量启停锁
// （机主 2026-09-26 实时反馈："点击后它不关闭，因为我有可能需要点多次操作" + "启动和停止可以复选"）。
// jsdom 测不了真实遮挡，锁的是可断言形态：面板 Teleport 直挂 body（脱离
// .table-container/.content-area 两层裁剪链）、fixed 坐标内联写入、下方空间不够
// 向上翻转（.up + bottom）、滚动/缩放/复采等锚点失效即收、面板内点击不当外点误收、
// 菜单项动作执行但面板常驻；复选列三态（全选/半态/清除）、批量启停串行直调单行后端、
// 聚合含失败点名不谎报全成、在飞禁重入。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WslDistroTable from '../WslDistroTable.vue'

const api = vi.hoisted(() => ({
  ListDistroExports: vi.fn(),
  OpenDistroFolder: vi.fn(),
  OpenTerminal: vi.fn(),
  TerminateDistro: vi.fn(),
  RestartDistro: vi.fn(),
  SetDefaultDistro: vi.fn(),
  UnregisterDistro: vi.fn(),
  ExportDistro: vi.fn(),
  MoveDistro: vi.fn(),
  CloneDistro: vi.fn(),
  CompactDistro: vi.fn(),
  GetDistroForensics: vi.fn(),
  GetWslConf: vi.fn(),
  SaveWslConf: vi.fn(),
  RevealDistroExport: vi.fn(),
  CancelClone: vi.fn(),
  CancelCompact: vi.fn(),
}))

// 确认框 mock 带形参声明（沿 WSLView.spec 惯例）：calls 元组才有 opts 可取，
// 免掉对零参元组的非法下标断言（vue-tsc TS2352/TS2493）。
interface ConfirmOpts {
  title?: string
  description?: string
  tone?: string
  confirmLabel?: string
  details?: Array<{ label: string; value: string }>
}
const confirmFn = vi.hoisted(() => vi.fn(async (_opts: ConfirmOpts) => true))
const showToast = vi.hoisted(() => vi.fn())
const loadInstances = vi.hoisted(() => vi.fn(async () => {}))

vi.mock('../../../../bindings/hanxi/internal/modules/wsl/wslservice', () => api)
vi.mock('../../../composables/useToast', () => ({ useToast: () => ({ showToast }) }))
vi.mock('../../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: confirmFn }) }))
vi.mock('../../../composables/useClipboard', () => ({ useClipboard: () => ({ copy: vi.fn() }) }))
vi.mock('../../../composables/useWailsEvent', () => ({ useWailsEvent: () => {} }))

function instance(name: string, over: Partial<{ running: boolean; default: boolean; version: string }> = {}) {
  return {
    name, running: true, default: false, version: '2', stateText: '正在运行',
    basePath: `C:\\wsl\\${name}`, vhdxPath: '', sizeBytes: 0, ...over,
  }
}

// 挂载过的 wrapper 逐一登记：afterEach 先 unmount（Teleport 面板与 window/document
// 监听随组件生命周期摘除），再清 body——只清 DOM 不卸载，残活组件的下一次 patch
// 会打在已被移除的锚点上炸掉整个调度队列，殃及同期用例。
const wrappers: ReturnType<typeof mount>[] = []
function mountTable(
  instances = [instance('Ubuntu'), instance('Debian', { running: false, default: false })],
  propOver: Record<string, unknown> = {},
) {
  const wrapper = mount(WslDistroTable, {
    attachTo: document.body,
    props: {
      report: null, ready: true, instances,
      instLoading: false, instError: '', loadInstances,
      busyAny: false, globalBusy: false, busyWith: () => false, busyDistro: () => false, rowBusy: () => false,
      startOp: vi.fn(), finishOp: vi.fn(),
      cloneProg: null, compProg: null, cloneBusy: false, compTerm: true,
      cloneStageText: {}, compactStageText: {},
      installDir: 'D:\\wsl', previewSubdir: (b: string, id: string) => `${b}\\${id}`,
      pickFolderInto: vi.fn(async () => {}), modeWord: (m?: string | null) => m ?? '',
      ...propOver,
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

// 行内常驻钮顺序：0 终端 1 重启 2 关机 3 删除 4 ⋯更多（触发钮留在行内，面板在 body）。
const moreBtn = (wrapper: ReturnType<typeof mountTable>, row = 0) =>
  wrapper.findAll('.row-menu')[row].findAll('button')[0]
const bodyPanel = () => document.body.querySelector<HTMLElement>('.row-menu-panel')

async function openMenu(wrapper: ReturnType<typeof mountTable>, row = 0) {
  await moreBtn(wrapper, row).trigger('click')
  await flushPromises()
  return bodyPanel()
}

beforeEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
  api.ListDistroExports.mockResolvedValue([])
  api.OpenDistroFolder.mockResolvedValue({ success: true, message: '已打开' })
  api.TerminateDistro.mockResolvedValue({ success: true, message: '已终止' })
  api.RestartDistro.mockResolvedValue({ success: true, message: '已重启' })
})

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('「⋯ 更多」面板的遮挡整改形态（Teleport + fixed）', () => {
  it('面板直挂 body、不在表格滚动容器与行内；触发钮仍常驻行内', async () => {
    const wrapper = mountTable()
    expect(moreBtn(wrapper).text()).toContain('更多')
    expect(await openMenu(wrapper)).not.toBeNull()
    const panel = bodyPanel()!
    expect(panel.parentElement).toBe(document.body)
    expect(panel.closest('.table-container')).toBeNull()
    expect(wrapper.find('.row-menu-panel').exists()).toBe(false) // 行内已无面板
    expect(wrapper.findAll('tbody .row-menu-panel').length).toBe(0)
  })

  it('打开即以触发钮 rect 写入 fixed 内联坐标（不再是 CSS 常量 top:100%）', async () => {
    const wrapper = mountTable()
    const panel = await openMenu(wrapper)
    expect(panel!.style.left).not.toBe('')
    expect(panel!.style.visibility).toBe('visible') // 量尺后解除离屏隐藏
    expect(panel!.style.top).not.toBe('') // jsdom rect 全 0：默认向下
    expect(panel!.style.bottom).toBe('')
  })

  it('视口下方空间不足且上方放得下：向上翻转（.up + bottom 锚定）', async () => {
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(240)
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(160)
    const wrapper = mountTable()
    const trigger = moreBtn(wrapper).element as HTMLButtonElement
    trigger.getBoundingClientRect = () =>
      ({ bottom: 700, top: 660, left: 800, right: 940, width: 140, height: 40 }) as DOMRect
    await moreBtn(wrapper).trigger('click')
    await flushPromises()
    const panel = bodyPanel()!
    expect(panel.classList.contains('up')).toBe(true)
    expect(panel.style.bottom).toBe(`${window.innerHeight - 660 + 4}px`)
    expect(panel.style.top).toBe('')
  })

  it('第二行开菜单：面板仍是单实例（同时只开一行，直挂 body）', async () => {
    const wrapper = mountTable()
    expect(await openMenu(wrapper, 0)).not.toBeNull()
    await openMenu(wrapper, 1)
    expect(document.body.querySelectorAll('.row-menu-panel').length).toBe(1)
    const items = bodyPanel()!.querySelectorAll('button')
    expect(items.length).toBe(8)
  })
})

describe('「⋯ 更多」锚点失效与外点收合', () => {
  it('打开期间任意滚动（含祖先容器 capture）即收合', async () => {
    const wrapper = mountTable()
    expect(await openMenu(wrapper)).not.toBeNull()
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(bodyPanel()).toBeNull()
  })

  it('打开期间窗口缩放即收合', async () => {
    const wrapper = mountTable()
    expect(await openMenu(wrapper)).not.toBeNull()
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(bodyPanel()).toBeNull()
  })

  it('列表复采（行位移、旧坐标失信）即收合', async () => {
    const wrapper = mountTable()
    expect(await openMenu(wrapper)).not.toBeNull()
    await wrapper.setProps({ instances: [instance('Ubuntu'), instance('openEuler')] })
    await flushPromises()
    expect(bodyPanel()).toBeNull()
  })

  it('Esc 收合；面板体外（body 空白处）pointerdown 收合', async () => {
    const wrapper = mountTable()
    expect(await openMenu(wrapper)).not.toBeNull()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(bodyPanel()).toBeNull()

    expect(await openMenu(wrapper)).not.toBeNull()
    document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(bodyPanel()).toBeNull()
  })

  it('面板内 pointerdown 不算外点；点菜单项执行动作但面板常驻（机主：可能要连点多次）', async () => {
    const wrapper = mountTable()
    const panel = await openMenu(wrapper)
    panel!.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(bodyPanel()).not.toBeNull() // 面板内点击不得在 pointerdown 阶段被收掉

    // 「导出」是展开内联表单类动作：执行后复采不触发，面板必须继续挂着
    const item = Array.from(bodyPanel()!.querySelectorAll<HTMLButtonElement>('button'))
      .find(b => b.textContent?.includes('导出'))!
    item.click()
    await flushPromises()
    expect(wrapper.find('.export-row-editor').exists()).toBe(true) // 动作已执行
    expect(bodyPanel()).not.toBeNull() // 面板常驻不关

    expect(wrapper.find('.export-row-editor td').attributes('colspan')).toBe('6') // 复选列加入后编辑器整行同步扩列

    // 常驻期间可连点第二项（「详情」展开取证行）
    const forensics = Array.from(bodyPanel()!.querySelectorAll<HTMLButtonElement>('button'))
      .find(b => b.textContent?.includes('详情'))!
    forensics.click()
    await flushPromises()
    expect(api.GetDistroForensics).toHaveBeenCalledWith('Ubuntu')
    expect(bodyPanel()).not.toBeNull()

    // 收合仍走既有五通道：Esc
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(bodyPanel()).toBeNull()
  })

  it('常驻后再点同一行触发钮即收（toggle 语义不破坏）', async () => {
    const wrapper = mountTable()
    expect(await openMenu(wrapper)).not.toBeNull()
    await moreBtn(wrapper).trigger('click')
    await flushPromises()
    expect(bodyPanel()).toBeNull()
  })
})

// ---------- 复选列与批量启停（机主 2026-09-26："启动和停止可以复选，批量操作体验好很多"） ----------
const headCheck = (wrapper: ReturnType<typeof mountTable>) => wrapper.find('thead input[type="checkbox"]')
const rowChecks = (wrapper: ReturnType<typeof mountTable>) => wrapper.findAll('tbody input[type="checkbox"]')
const batchBtn = (wrapper: ReturnType<typeof mountTable>, word: string) =>
  wrapper.findAll('.batch-bar button').find(b => b.text().includes(word))!

describe('复选列与批量启停', () => {
  it('表头多出复选列且每行勾选钮带点名 aria-label；勾选即高亮行并浮现批量条', async () => {
    const wrapper = mountTable()
    expect(wrapper.findAll('thead th').length).toBe(6)
    expect(headCheck(wrapper).attributes('aria-label')).toBe('全选发行版')
    expect(rowChecks(wrapper).map(b => b.attributes('aria-label')))
      .toEqual(['选择发行版 Ubuntu', '选择发行版 Debian'])
    expect(wrapper.find('.batch-bar').exists()).toBe(false) // 常态零占位

    await rowChecks(wrapper)[0].setValue(true)
    expect(wrapper.findAll('tbody tr')[0].classes()).toContain('row-selected')
    const bar = wrapper.find('.batch-bar')
    expect(bar.exists()).toBe(true)
    expect(bar.text()).toContain('已选 1 项')
    // 启停各只统计勾选集中状态相符子集：Ubuntu 运行中 → 停止钮吃 1、启动钮 0 个即禁按
    expect(batchBtn(wrapper, '批量停止').text()).toContain('（1）')
    expect(batchBtn(wrapper, '批量启动').attributes('disabled')).toBeDefined()
  })

  it('表头三态：半选 indeterminate → 点全选 → 再点清除（批量条随选择集隐现）', async () => {
    const wrapper = mountTable()
    await rowChecks(wrapper)[0].setValue(true)
    await flushPromises()
    const head = headCheck(wrapper).element as HTMLInputElement
    expect(head.checked).toBe(false)
    expect(head.indeterminate).toBe(true) // 半态

    await headCheck(wrapper).setValue(true) // change → toggleSelectAll（非全选即全选）
    await flushPromises()
    expect(head.checked).toBe(true)
    expect(head.indeterminate).toBe(false)
    expect(rowChecks(wrapper).every(c => (c.element as HTMLInputElement).checked)).toBe(true)

    await headCheck(wrapper).setValue(false) // 已全选 → 清空
    await flushPromises()
    expect(rowChecks(wrapper).some(c => (c.element as HTMLInputElement).checked)).toBe(false)
    expect(wrapper.find('.batch-bar').exists()).toBe(false)
  })

  it('批量停止：一次确认点名名单与跳过项 → 串行直调单行 TerminateDistro → 失败项点名聚合不谎报全成 → 清选择只复采一次', async () => {
    const wrapper = mountTable([
      instance('Ubuntu'), instance('Debian', { running: true }),
      instance('Fedora', { running: true }), instance('Base', { running: false }),
    ])
    api.TerminateDistro.mockImplementation((name: string) =>
      name === 'Fedora' ? Promise.reject(new Error('拒绝访问')) : Promise.resolve({ success: true, message: '已终止' }))
    for (const box of rowChecks(wrapper)) await box.setValue(true)
    await flushPromises()
    expect(batchBtn(wrapper, '批量停止').text()).toContain('（3）') // 停止态 Base 不入停止队列
    await batchBtn(wrapper, '批量停止').trigger('click')
    await flushPromises()
    expect(confirmFn).toHaveBeenCalledTimes(1) // 批量只问一次，不再逐行弹确认
    const opts = confirmFn.mock.calls.at(-1)![0]
    expect(opts.details?.find(d => d.label === '批量停止')?.value).toBe('Ubuntu、Debian、Fedora')
    expect(opts.details?.find(d => d.label === '自动跳过')?.value).toBe('Base')
    expect(api.TerminateDistro.mock.calls.map(c => c[0])).toEqual(['Ubuntu', 'Debian', 'Fedora']) // 串行按行序
    expect(api.TerminateDistro).not.toHaveBeenCalledWith('Base')
    const toast = showToast.mock.calls.at(-1)![0] as string
    expect(toast).toContain('成功 2')
    expect(toast).toContain('失败 1')
    expect(toast).toContain('Fedora：拒绝访问') // 失败项点名，不吞不谎报
    expect(toast).toContain('跳过 1')
    expect(loadInstances).toHaveBeenCalledTimes(1) // 收口统一复采一次
    expect(wrapper.find('.batch-bar').exists()).toBe(false) // 选择清空
  })

  it('批量启动：只对勾选中的停止项走「重启即拉起」单行通道；运行中勾选项如实归入跳过', async () => {
    const wrapper = mountTable()
    for (const box of rowChecks(wrapper)) await box.setValue(true)
    await flushPromises()
    expect(batchBtn(wrapper, '批量启动').text()).toContain('（1）') // 仅 Debian 停止
    await batchBtn(wrapper, '批量启动').trigger('click')
    await flushPromises()
    expect(api.RestartDistro).toHaveBeenCalledTimes(1)
    expect(api.RestartDistro).toHaveBeenCalledWith('Debian')
    expect(api.TerminateDistro).not.toHaveBeenCalled()
    const toast = showToast.mock.calls.at(-1)![0] as string
    expect(toast).toContain('批量启动完成：1 个全部成功')
    expect(toast).toContain('Ubuntu') // 跳过点名
  })

  it('批量在飞不得重入：钮与勾选框全部禁用态，二次点击零触达；放行后串行续跑至收口', async () => {
    const pending: Array<(v: unknown) => void> = []
    api.TerminateDistro.mockImplementation(() => new Promise(res => { pending.push(res) }))
    const wrapper = mountTable([instance('Ubuntu'), instance('Debian', { running: true })])
    for (const box of rowChecks(wrapper)) await box.setValue(true)
    await flushPromises()
    await batchBtn(wrapper, '批量停止').trigger('click')
    await flushPromises()
    expect(api.TerminateDistro).toHaveBeenCalledTimes(1)
    expect(batchBtn(wrapper, '批量停止').attributes('disabled')).toBeDefined()
    expect(batchBtn(wrapper, '批量启动').attributes('disabled')).toBeDefined()
    expect(rowChecks(wrapper)[0].attributes('disabled')).toBeDefined() // 批量期禁勾选
    await batchBtn(wrapper, '批量停止').trigger('click')
    await flushPromises()
    expect(api.TerminateDistro).toHaveBeenCalledTimes(1) // 重入被拒

    pending[0]({ success: true, message: '已终止' })
    await flushPromises()
    expect(api.TerminateDistro).toHaveBeenCalledTimes(2) // 串行续跑第二台
    expect(batchBtn(wrapper, '批量停止').attributes('disabled')).toBeDefined() // 仍在本批在飞
    pending[1]({ success: true, message: '已终止' })
    await flushPromises()
    expect(loadInstances).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.batch-bar').exists()).toBe(false)
    expect(rowChecks(wrapper)[0].attributes('disabled')).toBeUndefined() // 闸放
  })

  it('本行已有其他写操作在飞的避开不进批量队列（确认框与回执都点名）', async () => {
    const wrapper = mountTable(
      [instance('Ubuntu'), instance('Debian', { running: true }), instance('Fedora', { running: true })],
      { busyDistro: (n: string) => n === 'Debian' },
    )
    for (const box of rowChecks(wrapper)) await box.setValue(true)
    await flushPromises()
    await batchBtn(wrapper, '批量停止').trigger('click')
    await flushPromises()
    expect(api.TerminateDistro.mock.calls.map(c => c[0])).toEqual(['Ubuntu', 'Fedora']) // Debian 在飞避让
    const opts = confirmFn.mock.calls.at(-1)![0]
    expect(opts.details?.find(d => d.label === '自动跳过')?.value).toBe('Debian')
    const toast = showToast.mock.calls.at(-1)![0] as string
    expect(toast).toContain('跳过 1（状态不符或在飞）：Debian')
  })

  it('复采后消失的发行版从选择集出账（不悬幽灵名）', async () => {
    const wrapper = mountTable()
    await rowChecks(wrapper)[1].setValue(true) // 勾 Debian
    await wrapper.setProps({ instances: [instance('Ubuntu')] })
    await flushPromises()
    expect(wrapper.find('.batch-bar').exists()).toBe(false)
  })
})
