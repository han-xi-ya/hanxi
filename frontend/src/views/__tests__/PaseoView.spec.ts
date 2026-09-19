// 特征测试（Wave 5 · 批 0 收敛件新增）：PaseoView 行为基线 + 契约消费成色。
// 锁定：stable/beta 双通道、使用版本三态（未设=自动最新回退高亮 / 显式设定 /
// 设定失败重拉）、「未验证哈希」方言列、多版本卡片逐版本卸载禁用、升级警告条、
// 常驻自动更新提示条、安装动词词面、双数据目录钮（extras.dataDir + #extras-action）、
// 事件改写与 KeepAlive 轮询契约；并挂载共享 ManagedVersionPanel 锁真实 adapter
// 供 getActive/setActive 时面板的成色（含「使用中」与页面「使用版本」的词面差）。
// 绑定/事件经 vi.mock 打桩；KeepAlive 宿主复刻 App.vue 外壳。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PaseoView from '../PaseoView.vue'
import ManagedVersionPanel from '../../components/managed/ManagedVersionPanel.vue'
import { createPaseoAdapter } from '../../adapters/paseo'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetReleaseChannel: vi.fn(),
  GetStatus: vi.fn(),
  SetActiveVersion: vi.fn(),
  SetReleaseChannel: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  OpenDir: vi.fn(),
  OpenElectronDataDir: vi.fn(),
  OpenDaemonHome: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/paseo/paseoservice', () => svc)

const installed080 = {
  version: '0.8.0',
  exePath: 'C:\\data\\paseo\\versions\\paseo_0.8.0\\Paseo.exe',
  dir: 'C:\\data\\paseo\\versions\\paseo_0.8.0',
  size: 100 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
  source: 'Paseo-win-x64-0.8.0.zip',
  verifiedHash: true,
}

const installed070 = {
  version: '0.7.0',
  exePath: 'C:\\data\\paseo\\versions\\paseo_0.7.0\\Paseo.exe',
  dir: 'C:\\data\\paseo\\versions\\paseo_0.7.0',
  size: 98 * 1024 * 1024,
  installedAt: '2026-07-01',
  isImport: false,
  source: 'Paseo-win-x64-0.7.0.zip',
  verifiedHash: false, // 早期版无官方 digest：「未验证哈希」方言列样本
}

const release090 = { version: '0.9.0', size: 102 * 1024 * 1024, published: '2026-09-01T00:00:00Z', isPre: false }
const release080 = { version: '0.8.0', size: 100 * 1024 * 1024, published: '2026-08-01T00:00:00Z', isPre: false }

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(
  snap: Record<string, unknown>,
  opts: { installed?: unknown[]; releases?: unknown[]; ch?: string; active?: string } = {},
) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [installed080, installed070])
  svc.ListReleases.mockResolvedValue(opts.releases ?? [release090])
  svc.GetActiveVersion.mockResolvedValue(opts.active ?? '')
  svc.GetReleaseChannel.mockResolvedValue(opts.ch ?? 'stable')
  svc.GetFollowOnExit.mockResolvedValue(false)
  svc.RepositoryURL.mockResolvedValue('https://github.com/getpaseo/paseo')
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(PaseoView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  // 交互面：确认/输入经 useConfirm/usePrompt 全局单例（见 CCSwitchView.spec 同注释）
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('PaseoView 装载与通道', () => {
  it('挂载并发拉取状态/已装/远程/使用版本/通道/联动/仓库', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    for (const fn of [
      svc.GetStatus, svc.ListInstalledVersions, svc.ListReleases, svc.GetActiveVersion,
      svc.GetReleaseChannel, svc.GetFollowOnExit, svc.RepositoryURL,
    ]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('beta 通道切换：SetReleaseChannel 后重拉列表并显尝鲜警示；同通道不重复设置', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release090] })
    const { wrapper } = await mountView()
    const before = svc.ListReleases.mock.calls.length
    await wrapper.findAll('.channel-seg button')[0].trigger('click') // stable=当前通道
    expect(svc.SetReleaseChannel).not.toHaveBeenCalled()
    svc.SetReleaseChannel.mockResolvedValue('beta')
    svc.GetReleaseChannel.mockResolvedValue('beta')
    await wrapper.findAll('.channel-seg button')[1].trigger('click')
    await vi.waitFor(() => expect(svc.SetReleaseChannel).toHaveBeenCalledWith('beta'))
    expect(svc.ListReleases.mock.calls.length).toBeGreaterThan(before)
    expect(wrapper.find('.beta-warn').text()).toBe('beta 为上游预发布版，仅供尝鲜')
    wrapper.unmount()
  })

  it('通道切换失败：toast「切换通道失败: 」且通道态不翻转', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetReleaseChannel.mockRejectedValue(new Error('离线'))
    const { wrapper } = await mountView()
    await wrapper.findAll('.channel-seg button')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('切换通道失败: 离线')
    expect(wrapper.find('.beta-warn').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('PaseoView 使用版本三态与方言列', () => {
  it('active 为空=自动最新：meta 现词「自动最新（0.8.0）」，最新卡高亮 + 使用版本徽标，次卡带设为使用/未验证哈希', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(wrapper.find('.meta-info').text()).toContain('使用版本')
    expect(wrapper.find('.meta-info').text()).toContain('自动最新（0.8.0）')
    const cards = wrapper.findAll('.installed-card')
    expect(cards[0].classes()).toContain('card-active')
    expect(cards[0].text()).toContain('使用版本')
    expect(cards[1].classes()).not.toContain('card-active')
    expect(cards[1].text()).toContain('未验证哈希')
    expect(cards[1].findAll('button').map((b) => b.text())).toContain('设为使用')
    expect(cards[0].findAll('button').map((b) => b.text())).not.toContain('设为使用')
    wrapper.unmount()
  })

  it('显式 active=0.7.0：高亮与徽标迁移到 0.7.0 卡，0.8.0 卡可设', async () => {
    stubDefaults({ state: 'stopped' }, { active: '0.7.0' })
    const { wrapper } = await mountView()
    expect(wrapper.find('.meta-info').text()).toContain('使用版本')
    expect(wrapper.find('.meta-info').text()).not.toContain('自动最新')
    expect(wrapper.find('.meta-info').text()).toContain('0.7.0')
    const cards = wrapper.findAll('.installed-card')
    expect(cards[1].classes()).toContain('card-active')
    expect(cards[1].text()).toContain('使用版本')
    expect(cards[0].findAll('button').map((b) => b.text())).toContain('设为使用')
    wrapper.unmount()
  })

  it('设为使用成功：回执「下次启动将使用 Paseo X」+ activeVersion 迁移高亮（不重拉）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetActiveVersion.mockResolvedValue('0.7.0')
    const { wrapper } = await mountView()
    const before = svc.ListInstalledVersions.mock.calls.length
    const setBtn = wrapper.findAll('.installed-card')[1].findAll('button').find((b) => b.text() === '设为使用')!
    await setBtn.trigger('click')
    await vi.waitFor(() => expect(svc.SetActiveVersion).toHaveBeenCalledWith('0.7.0'))
    expect(useToast().toastMsg.value).toBe('下次启动将使用 Paseo 0.7.0')
    expect(svc.ListInstalledVersions.mock.calls.length).toBe(before)
    const cards = wrapper.findAll('.installed-card')
    expect(cards[0].findAll('button').map((b) => b.text())).toContain('设为使用')
    expect(cards[1].text()).toContain('使用版本')
    wrapper.unmount()
  })

  it('设为使用失败：toast「设置失败: 」且重拉列表核对后端真实值（现视图逐字行为）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetActiveVersion.mockRejectedValue(new Error('版本不存在'))
    const { wrapper } = await mountView()
    const before = svc.ListInstalledVersions.mock.calls.length
    const setBtn = wrapper.findAll('.installed-card')[1].findAll('button').find((b) => b.text() === '设为使用')!
    await setBtn.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('设置失败: 版本不存在'))
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('逐版本卸载禁用：运行中版本卡禁用并提示，他版本可卸且 title 保共享数据现词', async () => {
    stubDefaults({ state: 'running', version: '0.8.0', pid: 4242, startedAt: '' })
    const { wrapper } = await mountView()
    const uninstallOf = (i: number) => wrapper.findAll('.installed-card')[i].findAll('button').find((b) => b.text() === '卸载')!
    expect(uninstallOf(0).attributes('disabled')).toBeDefined()
    expect(uninstallOf(0).attributes('title')).toBe('请先退出该版本')
    expect(uninstallOf(1).attributes('disabled')).toBeUndefined()
    expect(uninstallOf(1).attributes('title')).toBe('仅删本版本目录，共享数据不受影响')
    wrapper.unmount()
  })

  it('卸载确认：title 含版本号、description 保双数据目录现词；确认后 Remove + toast', async () => {
    stubDefaults({ state: 'stopped' })
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card')[0].findAll('button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 Paseo 0.8.0？')
    expect(confirmState.options.description).toContain('%APPDATA%\\Paseo 与 ~/.paseo 中的设置、会话与手机配对数据为共享用户数据')
    settleConfirm(true)
    await vi.waitFor(() => expect(svc.RemoveVersion).toHaveBeenCalledWith('0.8.0'))
    expect(useToast().toastMsg.value).toBe('已卸载 0.8.0')
    wrapper.unmount()
  })
})

describe('PaseoView 升级判定与提示条', () => {
  it('升级警告条：最新已装核心 < 通道最新核心', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release090] })
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('发现可升级版本 0.9.0（当前最新已装 0.8.0）')
    wrapper.unmount()
  })

  it('常驻「上游无自动更新禁用开关」信息条恒显（共享件无第二位常驻 banner，留视图）', async () => {
    stubDefaults({ state: 'running', version: '0.8.0' })
    const { wrapper } = await mountView()
    const info = wrapper.findAll('.banner').find((b) => b.classes().includes('banner-info'))!
    expect(info.text()).toContain('上游无自动更新禁用开关')
    expect(info.text()).toContain('版本升级请统一走这里')
    wrapper.unmount()
  })

  it('外部实例：banner-warn 现词（共享数据锁组指引）；退出钮可点且 title 指引窗口内退出', async () => {
    stubDefaults({ state: 'external' })
    const { wrapper } = await mountView()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('共享数据模式下同一实例锁组')
    const quit = wrapper.findAll('.control-btns button').find((b) => b.text().includes('退出'))!
    expect(quit.attributes('disabled')).toBeUndefined()
    expect(quit.attributes('title')).toBe('外部实例请在 Paseo 窗口内退出')
    wrapper.unmount()
  })

  it('未运行引导行引用将启版本（active 空=自动最新已装 0.8.0）', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(wrapper.find('.hint-line').text()).toContain('启动 Paseo 0.8.0')
    wrapper.unmount()
  })
})

describe('PaseoView 安装动词与导入', () => {
  it('安装钮词面「安装」：调 DownloadVersion；失败自弹「安装失败: 」（store 前缀「下载失败: 」被吃掉）', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release090] })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountView()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.find('.ps-ver-status').classes()).toContain('idle')
    await row.findAll('button').find((b) => b.text() === '安装')!.trigger('click')
    await vi.waitFor(() => expect(svc.DownloadVersion).toHaveBeenCalledWith('0.9.0'))
    wrapper.unmount()

    stubDefaults({ state: 'stopped' }, { releases: [release090] })
    svc.DownloadVersion.mockRejectedValue(new Error('网络断'))
    const r2 = await mountView()
    await r2.wrapper.findAll('.tbl tbody tr')[0].findAll('button').find((b) => b.text() === '安装')!.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('安装失败: 网络断'))
    r2.wrapper.unmount()
  })

  it('安装状态精确匹配（目录名与去 v tag 互等，无 coreCompare 方言）：0.8.0 行显已安装', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release090, release080] })
    const { wrapper } = await mountView()
    const rows = wrapper.findAll('.tbl tbody tr')
    expect(rows[0].find('.ps-ver-status').classes()).toContain('idle')
    expect(rows[1].find('.ps-ver-status').classes()).toContain('installed')
    wrapper.unmount()
  })

  it('导入本地：prompt 三行提示逐字；取消不导入，提交 trim 后导入并 toast', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ ...installed080, version: '0.8.5' })
    const { wrapper } = await mountView()
    const importBtn = wrapper.findAll('.btn-group button')[0]
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.label).toContain('app.asar')
    expect(promptState.options.description).toContain('导入不搬数据')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    await importBtn.trigger('click')
    await flushMicrotasks()
    settlePrompt(' %LOCALAPPDATA%\\Programs\\Paseo ')
    await vi.waitFor(() => expect(svc.ImportLocal).toHaveBeenCalledWith('%LOCALAPPDATA%\\Programs\\Paseo'))
    expect(useToast().toastMsg.value).toContain('已导入 Paseo 0.8.5')
    wrapper.unmount()
  })
})

describe('PaseoView 联动辅助与双数据目录', () => {
  it('双数据目录钮：Electron 数据走 extras.dataDir，daemon 数据走 #extras-action 槽', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenElectronDataDir.mockResolvedValue(undefined)
    svc.OpenDaemonHome.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const card = wrapper.find('.extras-card')
    await card.findAll('button').find((b) => b.text().includes('Electron 数据'))!.trigger('click')
    await vi.waitFor(() => expect(svc.OpenElectronDataDir).toHaveBeenCalled())
    await card.findAll('button').find((b) => b.text().includes('daemon 数据'))!.trigger('click')
    await vi.waitFor(() => expect(svc.OpenDaemonHome).toHaveBeenCalled())
    expect(card.find('.toggle-label').text()).toContain('默认关闭：Hanxi 退出不影响 Paseo 及其 agent 会话')
    wrapper.unmount()
  })

  it('随关勾选：SetFollowOnExit + 连带终止 agent 会话现词回执', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    await wrapper.find('.extras-card .toggle-label input').setValue(true)
    await vi.waitFor(() => expect(svc.SetFollowOnExit).toHaveBeenCalledWith(true))
    expect(useToast().toastMsg.value).toBe('已开启：Hanxi 退出将连带终止 Paseo 及其上正在运行的 agent 会话（下次启动生效）')
    wrapper.unmount()
  })

  it('打开窗口/退出：经 ManagedControlBar 声明钮走回执 toast 与状态刷新', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: '已启动 Paseo' })
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.control-btns button')
    expect(btns[0].text()).toBe('🗔 打开窗口')
    await btns[0].trigger('click')
    await vi.waitFor(() => expect(svc.OpenWindow).toHaveBeenCalled())
    expect(useToast().toastMsg.value).toBe('已启动 Paseo')
    await btns[1].trigger('click') // stopped 下退出禁用→无调用（禁用语义锁）
    await flushMicrotasks()
    expect(svc.Quit).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('PaseoView 事件与轮询', () => {
  it('instance-state 事件改写界面；unmount 双 unlisten', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['paseo:instance-state']({ data: { state: 'running', version: '0.8.0', pid: 99 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.pid-tag').text()).toContain('99')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('安装进度事件：downloading 渲染百分比，verify 阶段现词「校验并解压…」', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release090] })
    const { wrapper } = await mountView()
    runtime.handlers['paseo:version-download']({ data: { version: '0.9.0', stage: 'downloading', done: 50, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    runtime.handlers['paseo:version-download']({ data: { version: '0.9.0', stage: 'verify' } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toBe('校验并解压…')
    wrapper.unmount()
  })

  it('激活轮询、停用即止（GetStatus 不再增长）', async () => {
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

describe('PaseoView 真实 adapter 挂共享面板（setActive 供位成色对照）', () => {
  it('面板对 paseo 的 getActive/setActive 正常消费：使用中位 + 设钮，词面为面板标准形「使用中」（页面方言为「使用版本」，故本页不采面板渲染）', async () => {
    stubDefaults({ state: 'stopped' }, { active: '0.8.0' })
    const adapter = createPaseoAdapter()
    expect(adapter.versions.getActive).toBeTypeOf('function')
    expect(adapter.versions.setActive).toBeTypeOf('function')
    const w = mount(ManagedVersionPanel, { props: { adapter } })
    await flushPromises()
    await flushPromises()
    const cards = w.findAll('.installed-card')
    expect(cards[0].classes()).toContain('card-active')
    expect(cards[0].find('.badge-active').text()).toBe('使用中') // 面板标准词面
    expect(cards[1].findAll('button').map((b) => b.text())).toContain('设为使用')
    w.unmount()
  })
})
