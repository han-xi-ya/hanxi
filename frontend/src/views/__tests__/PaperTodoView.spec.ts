// 特征测试（Wave 5 · 批 0 迁移后基线）：PaperTodoView 已收敛至托管控制台共享契约
// （ManagedControlBar/ManagedVersionPanel/ManagedExtrasCard + adapters/papertodo）。
// 锁定：双变体经 variant 槽声明与切换（失败回滚）、单变体下载/换装/已装语义、
// 卸载确认文案随数据在册动态变化（保留数据承诺逐字）、重试按当前变体、
// 官方命令通道三按钮、listInstalled 退化 0/1 的 meta 成色、轮询与订阅生命周期。
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
  // 交互面：确认/输入经 useConfirm/usePrompt 全局单例（见 BCUView.spec 同注释）
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('PaperTodoView 控制台与命令通道', () => {
  it('挂载拉取状态/版本/变体/运行时/联动/仓库', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    for (const fn of [svc.GetStatus, svc.ListReleases, svc.GetInstalledVersion, svc.GetVariant, svc.GetRuntimeStatus, svc.GetFollowOnExit, svc.RepositoryURL]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('三按钮命令通道：唤回(primary)/收拢(#primary-action 槽)/退出(quit) 各自落 API 并 toast 回执', async () => {
    stubDefaults({ state: 'running', version: '1.2.0' })
    svc.OpenWindow.mockResolvedValue({ message: '已唤回全部纸片' })
    svc.HidePapers.mockResolvedValue({ message: '已收拢全部纸片' })
    svc.Quit.mockResolvedValue({ message: '已发送退出命令' })
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.control-btns button')
    expect(btns).toHaveLength(3)
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
    expect(useToast().toastMsg.value).toBe('已发送退出命令')
    wrapper.unmount()
  })

  it('stopped 态：收拢/退出禁用；引导行按 adapter.hint 现词', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.control-btns button')
    expect(btns[1].attributes('disabled')).toBeDefined()
    expect(btns[2].attributes('disabled')).toBeDefined()
    expect(wrapper.find('.hint-line').text()).toContain('尚未运行：点击「唤回纸片」启动')
    wrapper.unmount()
  })

  it('external 态：banner-warn 原词（经 adapter.banner 投影）', async () => {
    stubDefaults({ state: 'external' })
    const { wrapper } = await mountView()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('检测到外部 PaperTodo 实例（非 Hanxi 托管）')
    wrapper.unmount()
  })
})

describe('PaperTodoView 变体体系（adapter.variant 槽）', () => {
  it('no-runtime 变体 + 无 .NET 10 运行时 → 警示注记 warn（运行时经快照扩展字段回流）', async () => {
    stubDefaults({ state: 'stopped' }, { variant: 'no-runtime', runtime: { hasDesktop10: false, desktopRuntimes: [] } })
    const { wrapper } = await mountView()
    const note = wrapper.find('.variant-note')
    expect(note.classes()).toContain('warn')
    expect(note.text()).toContain('启动失败')
    wrapper.unmount()
  })

  it('SetVariant 成功后重拉版本投影，远程大小随 no-runtime 资产更新', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetVariant.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const releasesBefore = svc.ListReleases.mock.calls.length
    const installedBefore = svc.GetInstalledVersion.mock.calls.length
    const remoteSize = () => wrapper.findAll('.tbl tbody tr')[0].findAll('td')[2].text()
    expect(remoteSize()).toBe('71.0 MB')

    const radios = wrapper.findAll('.variant-opt input[type="radio"]')
    await radios[1].trigger('change')
    await flushMicrotasks()

    expect(svc.SetVariant).toHaveBeenCalledWith('no-runtime')
    expect(svc.ListReleases).toHaveBeenCalledTimes(releasesBefore + 1)
    expect(svc.GetInstalledVersion).toHaveBeenCalledTimes(installedBefore + 1)
    expect(remoteSize()).toBe('2.3 MB')
    wrapper.unmount()
  })

  it('切换变体成功 toast；失败回滚选中态', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    svc.SetVariant.mockResolvedValue(undefined)
    const radios = wrapper.findAll('.variant-opt input[type="radio"]')
    expect(radios).toHaveLength(2)
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

  it('异版本行=下载安装（按当前变体偏好落参）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountView()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.find('.ver-status').classes()).toContain('idle')
    const btn = row.findAll('button').find((b) => b.text() === '下载安装')!
    await btn.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.3.0', 'self-contained')
    wrapper.unmount()
  })

  it('同版本同变体=已安装（行内无下载钮）；选另一变体后变体卡出现「换装」并可落参下载', async () => {
    stubDefaults({ state: 'stopped' }, { variant: 'no-runtime' })
    svc.ListReleases.mockResolvedValue([{ ...release130, version: '1.2.0' }])
    svc.SetVariant.mockResolvedValue(undefined)
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountView()
    // installed 1.2.0 self-contained = 远程 1.2.0 → 已安装态；当前变体偏好 no-runtime → 换装可用
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.find('.ver-status').classes()).toContain('installed')
    expect(row.text()).not.toContain('下载安装')
    const switchBtn = wrapper.findAll('.variant-card button').find((b) => b.text() === '换装')!
    expect(switchBtn.attributes('title')).toContain('覆盖安装为精简版变体（便签数据不动）')
    await switchBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.2.0', 'no-runtime')
    wrapper.unmount()
  })

  it('同版本同变体 → 无换装入口（仅已安装态）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ListReleases.mockResolvedValue([{ ...release130, version: '1.2.0' }])
    const { wrapper } = await mountView()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.find('.ver-status').classes()).toContain('installed')
    expect(wrapper.text()).not.toContain('换装')
    wrapper.unmount()
  })

  it('error 事件后状态列显失败，错误详情与重试链接呈现（重试按当前变体）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountView()
    runtime.handlers['papertodo:version-download']({ data: { version: '1.3.0', stage: 'error', message: '网络中断' } })
    await nextTick()
    expect(wrapper.find('.ver-status').classes()).toContain('error')
    expect(wrapper.find('.ver-status').text()).toBe('失败')
    expect(wrapper.find('.dl-error').text()).toBe('网络中断')
    const retry = wrapper.findAll('.retry-link').find((a) => a.text() === '重试')!
    await retry.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.3.0', 'self-contained')
    wrapper.unmount()
  })

  it('already-installed 回执弹变体名 toast', async () => {
    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockResolvedValue('already-installed')
    const { wrapper } = await mountView()
    const btn = wrapper.findAll('.tbl tbody tr')[0].findAll('button').find((b) => b.text() === '下载安装')!
    await btn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('版本 1.3.0（完整版）已安装')
    wrapper.unmount()
  })
})

describe('PaperTodoView 卸载/导入（数据保留语义）', () => {
  it('已装卡 0/1 成色：meta 行携当前托管变体、数据在册与单目录覆盖升级话术', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    const meta = wrapper.find('.meta-info')
    expect(meta.text()).toContain('已安装')
    expect(meta.text()).toContain('当前托管 1.2.0 · 完整版')
    expect(meta.text()).toContain('便签数据在册')
    expect(meta.text()).toContain('单目录覆盖升级：便签数据原地保留；「卸载」只删程序不删数据；回滚旧版 = 在下方重新安装该版本')
    expect(meta.text()).toContain('此为审计基线')
    wrapper.unmount()
  })

  it('便签数据在册：确认文案承诺原地保留；确认只调 RemoveInstalled', async () => {
    stubDefaults({ state: 'stopped' })
    svc.RemoveInstalled.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 PaperTodo？')
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
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.options.description).toContain('当前没有便签数据')
    settleConfirm(false)
    wrapper.unmount()
  })

  it('导入本地：hasData 回执带数据随行标注；取消不动后端', async () => {
    stubDefaults({ state: 'stopped' }, { installed: null })
    svc.ImportLocal.mockResolvedValue({ version: '1.1.0', hasData: true })
    const { wrapper } = await mountView()
    await wrapper.findAll('.btn-group button')[0].trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.label).toContain('PaperTodo.exe') // 原 prompt 首行指引逐字保留
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()

    await wrapper.findAll('.btn-group button')[0].trigger('click')
    await flushMicrotasks()
    settlePrompt(' C:\\mine\\PaperTodo ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\mine\\PaperTodo')
    expect(useToast().toastMsg.value).toBe('已导入 PaperTodo 1.1.0（便签数据随行）')
    wrapper.unmount()
  })

  it('首用空态：引导话术逐字 + 下载最新版按当前变体落参', async () => {
    stubDefaults({ state: 'stopped' }, { installed: null })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountView()
    const empty = wrapper.find('.empty-state')
    expect(empty.text()).toContain('尚未安装 PaperTodo —— 下载官方绿色版')
    await empty.find('button').trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.3.0', 'self-contained')
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
    const releasesBtn = wrapper.findAll('.extras-card button').find((b) => b.text() === 'Releases 页')!
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
