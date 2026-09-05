// 特征测试（Phase 4 组 E）：PaperTodoView 迁移前行为基线。
// 锁定：双变体三态操作（安装/换装/已装）、卸载确认文案随数据在册动态变化、
// 重试沿用最近变体、变体失败回滚、官方命令通道三按钮、轮询生命周期。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PaperTodoView from '../PaperTodoView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  GetInstalledVersion: vi.fn(),
  GetVariant: vi.fn(),
  GetRuntimeStatus: vi.fn(),
  GetStatus: vi.fn(),
  OpenWindow: vi.fn(),
  HidePapers: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveInstalled: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  SetVariant: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
  OpenReleasesPage: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/papertodo/papertodoservice', () => svc)

const installed120 = {
  version: '1.2.0',
  variant: 'self-contained',
  exePath: 'C:\\data\\papertodo\\PaperTodo.exe',
  dir: 'C:\\data\\papertodo',
  size: 71 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
  hasData: true,
  assetSha256: 'a'.repeat(64),
}

const release130 = {
  version: '1.3.0',
  published: '2026-08-20T00:00:00Z',
  selfContained: { size: 71 * 1024 * 1024 },
  noRuntime: { size: 2400 * 1024 },
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(snap: Record<string, unknown>, opts: { installed?: unknown; variant?: string; runtime?: Record<string, unknown> | null } = {}) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListReleases.mockResolvedValue([release130])
  svc.GetInstalledVersion.mockResolvedValue(opts.installed === undefined ? installed120 : opts.installed)
  svc.GetVariant.mockResolvedValue(opts.variant ?? 'self-contained')
  svc.GetRuntimeStatus.mockResolvedValue(opts.runtime === undefined ? { hasDesktop10: true, desktopRuntimes: ['10.0.2'] } : opts.runtime)
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/snownico0722/PaperTodo')
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(PaperTodoView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  // 迁移后交互面：确认/输入经 useConfirm/usePrompt 全局单例（见 BCUView.spec 同注释）
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('PaperTodoView 控制台与命令通道', () => {
  it('挂载拉取状态/版本/变体/运行时/联动', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    for (const fn of [svc.GetStatus, svc.ListReleases, svc.GetInstalledVersion, svc.GetVariant, svc.GetRuntimeStatus, svc.GetFollowOnExit]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('三按钮命令通道：唤回/收拢/退出各自落 API 并 toast 回执', async () => {
    stubDefaults({ state: 'running', version: '1.2.0' })
    svc.OpenWindow.mockResolvedValue({ message: '已唤回全部纸片' })
    svc.HidePapers.mockResolvedValue({ message: '已收拢全部纸片' })
    svc.Quit.mockResolvedValue({ message: '已发送退出命令' })
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.control-btns button')
    await btns[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已唤回全部纸片')
    await btns[1].trigger('click')
    await flushMicrotasks()
    expect(svc.HidePapers).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已收拢全部纸片')
    await btns[2].trigger('click')
    await flushMicrotasks()
    expect(svc.Quit).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('stopped 态：收拢/退出禁用', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.control-btns button')
    expect(btns[1].attributes('disabled')).toBeDefined()
    expect(btns[2].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})

describe('PaperTodoView 变体体系', () => {
  it('no-runtime 变体 + 无 .NET 10 运行时 → 警示注记 warn', async () => {
    stubDefaults({ state: 'stopped' }, { variant: 'no-runtime', runtime: { hasDesktop10: false, desktopRuntimes: [] } })
    const { wrapper } = await mountView()
    const note = wrapper.find('.variant-note')
    expect(note.classes()).toContain('warn')
    expect(note.text()).toContain('启动失败')
    wrapper.unmount()
  })

  it('切换变体成功 toast；失败回滚选中态', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    svc.SetVariant.mockResolvedValue(undefined)
    const radios = wrapper.findAll('.variant-opt input[type="radio"]')
    await radios[1].trigger('change')
    await flushMicrotasks()
    expect(svc.SetVariant).toHaveBeenCalledWith('no-runtime')
    expect(useToast().toastMsg.value).toContain('已切换为精简版')

    svc.SetVariant.mockRejectedValue(new Error('存储不可写'))
    await radios[0].trigger('change') // 切回完整版 → 失败回滚到 no-runtime
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('设置失败')
    expect((radios[1].element as HTMLInputElement).checked).toBe(true)
    wrapper.unmount()
  })

  it('双变体三态：本版同变体=已安装、本版异变体=换装、异版本=安装', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    const cells = row.findAll('.asset-cell')
    // release 1.3.0 vs installed 1.2.0 → 两个变体都是"安装"
    expect(cells[0].find('button').text()).toBe('安装')
    expect(cells[1].find('button').text()).toBe('安装')
    // 装此版 覆盖链接存在（installed 版本不同）
    expect(row.text()).toContain('装此版')
    wrapper.unmount()
  })

  it('同版本异变体 → 换装；同版本同变体 → 已安装', async () => {
    stubDefaults({ state: 'stopped' }, { installed: { ...installed120 }, variant: 'no-runtime' })
    // 当前托管 1.2.0（installed），远程表只有 1.3.0 —— 改用 1.2.0 远程行验证三态
    svc.ListReleases.mockResolvedValue([{ ...release130, version: '1.2.0' }])
    await flushMicrotasks()
    const { wrapper } = await mountView()
    await flushMicrotasks()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.find('.pt-ver-status').classes()).toContain('installed')
    const cells = row.findAll('.asset-cell')
    // installed variant=self-contained：完整版列=已安装，精简版列=换装
    expect(cells[0].text()).toContain('已安装')
    expect(cells[1].find('button').text()).toBe('换装')
    svc.DownloadVersion.mockResolvedValue('started')
    await cells[1].find('button').trigger('click')
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.2.0', 'no-runtime')
    wrapper.unmount()
  })

  it('error 事件后状态列显失败；重试链接现状不渲染（疑似死代码，见迁移报告）', async () => {
    // 现状锁定：statusOf 对 stage==='error' 返回 'error'，而"重试"链接挂在
    // v-if="statusOf==='downloading'" 分支内 → 二者互斥，retry-link 永不可见，
    // retryDownload/lastVariant 逻辑事实上不可达。迁移不改行为，仅交回主线决策。
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['papertodo:version-download']({ data: { version: '1.3.0', stage: 'error', message: '网络中断' } })
    await nextTick()
    expect(wrapper.find('.pt-ver-status').classes()).toContain('error')
    expect(wrapper.find('.pt-ver-status').text()).toBe('失败')
    expect(wrapper.text()).not.toContain('重试')
    wrapper.unmount()
  })
})

describe('PaperTodoView 卸载/导入（数据保留语义）', () => {
  it('便签数据在册：确认文案承诺原地保留；确认只调 RemoveInstalled', async () => {
    stubDefaults({ state: 'stopped' })
    svc.RemoveInstalled.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载（保留数据）')!

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('便签数据（data.json、图片库、plugins）将原地保留')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveInstalled).not.toHaveBeenCalled()

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveInstalled).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('已卸载 PaperTodo（便签数据已保留）')
    wrapper.unmount()
  })

  it('无数据态：确认文案如实说明当前没有便签数据', async () => {
    stubDefaults({ state: 'stopped' }, { installed: { ...installed120, hasData: false } })
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载（保留数据）')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.options.description).toContain('当前没有便签数据')
    settleConfirm(false)
    wrapper.unmount()
  })

  it('导入本地：hasData 回执带数据随行标注', async () => {
    stubDefaults({ state: 'stopped' }, { installed: null })
    svc.ImportLocal.mockResolvedValue({ version: '1.1.0', hasData: true })
    const { wrapper } = await mountView()
    await wrapper.findAll('.btn-group button')[0].trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.label).toContain('PaperTodo.exe') // 原 prompt 首行指引逐字保留
    settlePrompt(' C:\\mine\\PaperTodo ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\mine\\PaperTodo')
    expect(useToast().toastMsg.value).toBe('已导入 PaperTodo 1.1.0（便签数据随行）')
    wrapper.unmount()
  })
})

describe('PaperTodoView 事件与轮询', () => {
  it('instance-state 改写界面；unmount 双 unlisten', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['papertodo:instance-state']({ data: { state: 'running', version: '1.2.0', pid: 55 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('Releases 页直达按钮调 OpenReleasesPage', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenReleasesPage.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const releasesBtn = wrapper.findAll('.repo-row button').find((b) => b.text() === 'Releases 页')!
    await releasesBtn.trigger('click')
    expect(svc.OpenReleasesPage).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('激活轮询、停用即止', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      const { wrapper, show } = await mountView()
      const base = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
      show.value = false
      await nextTick()
      const after = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 5)
      expect(svc.GetStatus.mock.calls.length).toBe(after)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
