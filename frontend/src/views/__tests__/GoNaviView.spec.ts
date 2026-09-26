// GoNaviView 特征测试（三线并行 · 冻结契约口径）：标准壳（ManagedConsoleShell）
// 装配下的账外漂移警示（banner + 状态灯 warn 档 + 页头徽标三处同源）、
// metaHints 四条如实披露、末版卸载预告复用（soleVersionUninstallNote）、
// 运行态下载 confirm 闸。绑定面经 vi.mock 打桩（真实生成物落盘前由
// vitest.config 的解析缝兜底）；事件经 @wailsio/runtime 打桩。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GoNaviView from '../GoNaviView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  QuitAdvisory: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/gonavi/gonaviservice', () => svc)
vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

function statusOf(partial: Record<string, unknown>) {
  return { state: 'stopped', version: '', pid: 0, error: '', startedAt: '', drifted: false, driftNote: '', ...partial }
}

const installed210 = {
  version: '2.1.0',
  exePath: 'D:\\gonavi\\2.1.0\\gonavi.exe',
  dir: 'D:\\gonavi\\2.1.0',
  size: 9 * 1024 * 1024,
  installedAt: '2026-09-01',
  isImport: false,
  source: '',
}

const release220 = { version: '2.2.0', published: '2026-09-20T00:00:00Z', size: 10 * 1024 * 1024 }

function stubDefaults(snap: Record<string, unknown>, installed: unknown[] = [installed210], releases: unknown[] = [release220]) {
  svc.GetStatus.mockResolvedValue(statusOf(snap))
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue((installed[0] as { version?: string })?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(false)
  svc.RepositoryURL.mockResolvedValue('https://example.test/gonavi')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountAndOpenVersions() {
  const wrapper = mount(GoNaviView, { attachTo: document.body })
  await flushMicrotasks()
  const tabs = wrapper.findAll('.main-tab-btn')
  await tabs[1].trigger('click')
  await flushMicrotasks()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
  document.body.innerHTML = ''
})

describe('GoNaviView 账外漂移警示（三处同源）', () => {
  it('drifted：状态区 warn 横幅（含逐字口径与后端附言）+ 状态灯 warn 档 + 页头徽标', async () => {
    stubDefaults({
      state: 'running',
      version: '2.1.0',
      pid: 4242,
      drifted: true,
      driftNote: 'SHA256 与账本落位摘要不符',
      startedAt: new Date().toISOString(),
    })
    const wrapper = await mountAndOpenVersions()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('二进制已被应用自更新替换，与下载账目不一致')
    expect(banner.text()).toContain('SHA256 与账本落位摘要不符')
    expect(wrapper.find('.status-light').classes()).toContain('warn')
    expect(wrapper.find('.drift-badge').exists()).toBe(true)
    expect(wrapper.find('.drift-badge').text()).toBe('账外漂移')
    wrapper.unmount()
  })

  it('无漂移对照：running 走 ok 横幅、徽标缺席', async () => {
    stubDefaults({ state: 'running', version: '2.1.0', pid: 4242, startedAt: new Date().toISOString() })
    const wrapper = mount(GoNaviView)
    await flushMicrotasks()
    expect(wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(wrapper.find('.drift-badge').exists()).toBe(false)
    wrapper.unmount()
  })

  it('metaHints 四条如实披露进版本页 meta-info（漂移徽章语义在列）', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = await mountAndOpenVersions()
    const hints = wrapper.findAll('.meta-info .hint-dim')
    expect(hints).toHaveLength(4)
    const joined = hints.map((h) => h.text()).join('\n')
    expect(joined).toContain('账外漂移')
    expect(joined).toContain('%USERPROFILE%\\.gonavi')
    expect(joined).toContain('强杀兜底')
    wrapper.unmount()
  })
})

describe('GoNaviView 卸载与下载闸', () => {
  it('末版卸载预告复用：确认文案含"最后一个版本"与数据目录不随删，确认后走 RemoveVersion + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installed210])
    const wrapper = await mountAndOpenVersions()
    const { confirmState, settleConfirm } = useConfirm()
    svc.RemoveVersion.mockResolvedValue(undefined)

    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 GoNavi 2.1.0？')
    expect(confirmState.options.description).toContain('这是最后一个版本，卸载后将回到未安装状态')
    settleConfirm(true)
    await flushPromises()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('2.1.0')
    expect(useToast().toastMsg.value).toBe('已卸载 2.1.0')
    wrapper.unmount()
  })

  it('下载 confirm 闸：运行实例在→弹框如实预告，同意才 DownloadVersion；拒绝静默不触后端', async () => {
    stubDefaults({ state: 'running', version: '2.1.0', pid: 7, startedAt: new Date().toISOString() })
    const wrapper = await mountAndOpenVersions()
    const { confirmState, settleConfirm } = useConfirm()
    svc.DownloadVersion.mockResolvedValue('')

    const download = wrapper.findAll('.tbl tbody tr button').find((b) => b.text() === '下载安装')!
    await download.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('实例正在运行')
    settleConfirm(true)
    await flushPromises()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2.2.0')
    wrapper.unmount()

    // 拒绝分支：全新挂载（闸的现态经 getStatus 重新喂入）
    vi.clearAllMocks()
    stubDefaults({ state: 'running', version: '2.1.0', pid: 7, startedAt: new Date().toISOString() })
    const w2 = await mountAndOpenVersions()
    const download2 = w2.findAll('.tbl tbody tr button').find((b) => b.text() === '下载安装')!
    await download2.trigger('click')
    await flushMicrotasks()
    settleConfirm(false)
    await flushPromises()
    expect(svc.DownloadVersion).not.toHaveBeenCalled()
    w2.unmount()
  })

  it('停动态无闸直通：确认框不弹、直接开始下载', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = await mountAndOpenVersions()
    const { confirmState } = useConfirm()
    svc.DownloadVersion.mockResolvedValue('')

    const download = wrapper.findAll('.tbl tbody tr button').find((b) => b.text() === '下载安装')!
    await download.trigger('click')
    await flushPromises()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2.2.0')
    expect(confirmState.open).toBe(false)
    wrapper.unmount()
  })
})
