// WslDistroTable 行级「⋯ 更多」下拉结构锁（机主实跑反馈整改 N40）。
// jsdom 测不了真实遮挡，锁的是可断言形态：面板 Teleport 直挂 body（脱离
// .table-container/.content-area 两层裁剪链）、fixed 坐标内联写入、下方空间不够
// 向上翻转（.up + bottom）、滚动/缩放/复采等锚点失效即收、面板内点击不当外点误收。
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

const confirmFn = vi.hoisted(() => vi.fn(async () => true))
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
function mountTable(instances = [instance('Ubuntu'), instance('Debian', { running: false, default: false })]) {
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

  it('面板内 pointerdown 不算外点；点菜单项先收面板再执行动作', async () => {
    const wrapper = mountTable()
    const panel = await openMenu(wrapper)
    panel!.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(bodyPanel()).not.toBeNull() // 面板内点击不得在 pointerdown 阶段被收掉

    const item = Array.from(bodyPanel()!.querySelectorAll<HTMLButtonElement>('button'))
      .find(b => b.textContent?.includes('文件'))!
    item.click()
    await flushPromises()
    expect(bodyPanel()).toBeNull() // rowMenuAction 收合
    expect(api.OpenDistroFolder).toHaveBeenCalledWith('Ubuntu')
  })
})
