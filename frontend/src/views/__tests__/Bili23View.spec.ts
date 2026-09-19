// 特征测试（批 0 契约迁万件）：Bili23View 与 CCSwitchView 共享骨架，专属差异在
// 退出三态如实回执（stopped/hidden/asked 的 message 逐字上墙、禁止吞态）与
// #danger-extra 位的「强制结束」（ForceStop 非共享动词）。绑定/事件经 vi.mock
// 打桩；KeepAlive 宿主复刻 App.vue 外壳。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Bili23View from '../Bili23View.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  ForceStop: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  OpenConfigDir: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/bili23/service', () => svc)

const installedV2150 = {
  version: 'v2.15.0',
  exePath: 'C:\\data\\bili23\\v2.15.0\\Bili23.exe',
  dir: 'C:\\data\\bili23\\v2.15.0',
  size: 108 * 1024 * 1024,
  installedAt: '2026-08-01 10:00:00',
  isImport: false,
  source: '',
}

const releaseV2160 = {
  version: 'v2.16.0',
  published: '2026-08-15T00:00:00Z',
  isPre: false,
  assetName: 'Bili23-Downloader_2.16.0_windows_x64_portable.zip',
  assetUrl: 'https://github.com/ScottSloan/Bili23-Downloader/releases/download/v2.16.0/p.zip',
  size: 43 * 1024 * 1024,
  sha256: 'deadbeef',
}

/** 聚合 Status 形（service 层视图：引擎快照平铺 + windowVisible + uptime）。 */
function statusOf(overrides: Record<string, unknown> = {}) {
  return {
    version: '',
    state: 'stopped',
    pid: 0,
    exitCode: 0,
    error: '',
    external: false,
    startedAt: '',
    stoppedAt: '',
    windowVisible: false,
    uptimeSeconds: 0,
    ...overrides,
  }
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<Record<string, unknown>> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/ScottSloan/Bili23-Downloader')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(Bili23View)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

function findBtn(wrapper: Awaited<ReturnType<typeof mountInKeepAlive>>['wrapper'], text: string) {
  return wrapper.findAll('button').find((b) => b.text().includes(text))!
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('Bili23View 装载与状态投影', () => {
  it('初始并发拉取状态/版本/活动版本/联动开关/仓库', async () => {
    stubDefaults(statusOf())
    const { wrapper } = await mountInKeepAlive()
    for (const fn of [svc.GetStatus, svc.ListReleases, svc.ListInstalledVersions, svc.GetActiveVersion, svc.GetFollowOnExit, svc.RepositoryURL]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('未运行时提示引导行存在、banner 缺席', async () => {
    stubDefaults(statusOf(), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    expect(wrapper.find('.banner').exists()).toBe(false)
    expect(wrapper.find('.hint-line').text()).toContain('尚未运行')
    wrapper.unmount()
  })

  it('运行中且窗口可见：banner-ok + 状态词「运行中」', async () => {
    stubDefaults(statusOf({ state: 'running', version: 'v2.15.0', pid: 777, windowVisible: true, startedAt: new Date().toISOString() }), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(wrapper.find('.banner').text()).toContain('正在运行')
    wrapper.unmount()
  })

  it('运行中但窗口收入托盘：状态词覆写 + banner-warn（windowVisible 经 adapter 投影）', async () => {
    stubDefaults(statusOf({ state: 'running', version: 'v2.15.0', pid: 777, windowVisible: false, startedAt: new Date().toISOString() }), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('运行中 · 已收入托盘')
    expect(wrapper.find('.banner').classes()).toContain('banner-warn')
    expect(wrapper.find('.banner').text()).toContain('窗口已隐藏')
    wrapper.unmount()
  })

  it('外部实例：banner-warn；退出禁用、打开窗口可用', async () => {
    stubDefaults(statusOf({ state: 'external' }), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.banner').classes()).toContain('banner-warn')
    expect(wrapper.find('.banner').text()).toContain('外部 Bili23 实例')
    expect(findBtn(wrapper, '退出').attributes('disabled')).toBeDefined()
    expect(findBtn(wrapper, '打开窗口').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('异常退出：banner-error 显示后端错误文案', async () => {
    stubDefaults(statusOf({ state: 'failed', error: '进程启动失败: 文件不存在' }), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.banner').text()).toContain('进程启动失败: 文件不存在')
    wrapper.unmount()
  })
})

describe('Bili23View 退出三态如实回执（上墙不吞态）', () => {
  // 后端 QuitOutcome.message 逐态分化；共享 store 经 settle 原样弹 toast——
  // 断言逐字相等：既不得吞掉（无 toast），也不得改写（加前缀/合并文案）。
  const outcomes: Array<[string, Record<string, unknown>]> = [
    [
      'stopped（真退）',
      { stopped: true, external: false, hidden: false, asked: false, message: 'Bili23 已退出' },
    ],
    [
      'hidden（收入托盘仍托管）',
      {
        stopped: false, external: false, hidden: true, asked: false,
        message: 'Bili23 按「关闭窗口=最小化到托盘」设置收入自身托盘，进程仍在本机运行。可用「打开窗口」唤回、「强制结束」终结，或在其托盘菜单退出',
      },
    ],
    [
      'asked（弹窗询问中）',
      {
        stopped: false, external: false, hidden: false, asked: true,
        message: 'Bili23 已弹出退出询问对话框（「关闭窗口=总是询问」设置），请在其中选择退出或隐藏',
      },
    ],
  ]

  for (const [name, outcome] of outcomes) {
    it(`Quit ${name}：message 逐字上墙并刷新快照`, async () => {
      stubDefaults(statusOf({ state: 'running', version: 'v2.15.0', pid: 777, windowVisible: true }), [installedV2150])
      svc.Quit.mockResolvedValue(outcome)
      const { wrapper } = await mountInKeepAlive()
      const before = svc.GetStatus.mock.calls.length
      const quit = findBtn(wrapper, '退出')
      await quit.trigger('click')
      await flushPromises()
      expect(svc.Quit).toHaveBeenCalled()
      expect(useToast().toastMsg.value).toBe(outcome.message)
      // 动作后恒刷新：hidden/asked 态的界面现态（如收入托盘覆写）随新快照落屏
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
      wrapper.unmount()
    })
  }

  it('Quit 抛错：失败前缀「退出失败: 」+ 原文案，快照仍刷新', async () => {
    stubDefaults(statusOf({ state: 'running', version: 'v2.15.0', pid: 777, windowVisible: true }), [installedV2150])
    svc.Quit.mockRejectedValue(new Error('调用门拒绝（模块已停用）'))
    const { wrapper } = await mountInKeepAlive()
    await findBtn(wrapper, '退出').trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('退出失败: 调用门拒绝（模块已停用）')
    wrapper.unmount()
  })

  it('打开窗口：ControlOutcome.message 上墙', async () => {
    stubDefaults(statusOf(), [installedV2150])
    svc.OpenWindow.mockResolvedValue({ action: 'started', external: false, message: 'Bili23 v2.15.0 已启动' })
    const { wrapper } = await mountInKeepAlive()
    await findBtn(wrapper, '打开窗口').trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('Bili23 v2.15.0 已启动')
    wrapper.unmount()
  })
})

describe('Bili23View 强制结束（#danger-extra 位）', () => {
  it('危险位渲染于联动卡之后（契约位序），钮面文案为「⛔ 强制结束」', async () => {
    stubDefaults(statusOf({ state: 'running', version: 'v2.15.0', windowVisible: true }), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    const html = wrapper.html()
    expect(wrapper.find('.b23-danger-extra').exists()).toBe(true)
    expect(html).toContain('强制结束')
    expect(html.indexOf('extras-card')).toBeLessThan(html.indexOf('b23-danger-extra'))
    wrapper.unmount()
  })

  it('停机态强制结束禁用；卸载/导入等常规位不误伤', async () => {
    stubDefaults(statusOf(), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    expect(findBtn(wrapper, '强制结束').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('必经确认：取消不动后端；确认则 ForceStop + 回执逐字上墙 + 回读快照', async () => {
    stubDefaults(statusOf({ state: 'running', version: 'v2.15.0', pid: 777, windowVisible: true }), [installedV2150])
    svc.ForceStop.mockResolvedValue({
      stopped: true, external: false, hidden: false, asked: false,
      message: 'Bili23 已被强制结束（在途下载已中断，下次启动可续传）',
    })
    const { confirmState, settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    await findBtn(wrapper, '强制结束').trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('强制结束 Bili23？')
    expect(confirmState.options.description).toContain('断点续传')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.ForceStop).not.toHaveBeenCalled()
    // 再次触发并确认放行
    await findBtn(wrapper, '强制结束').trigger('click')
    await flushMicrotasks()
    const before = svc.GetStatus.mock.calls.length
    settleConfirm(true)
    await flushPromises()
    expect(svc.ForceStop).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('Bili23 已被强制结束（在途下载已中断，下次启动可续传）')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('外部实例：钮可点（title 指引管辖边界），回执如实透出"不强制执行"', async () => {
    stubDefaults(statusOf({ state: 'external' }), [installedV2150])
    svc.ForceStop.mockResolvedValue({
      stopped: false, external: true, hidden: false, asked: false,
      message: '当前是外部自行启动的实例，Hanxi 不对其强制执行',
    })
    const { settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    const force = findBtn(wrapper, '强制结束')
    expect(force.attributes('disabled')).toBeUndefined()
    expect(force.attributes('title')).toContain('外部实例不在 Hanxi 管辖范围')
    await force.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushPromises()
    expect(svc.ForceStop).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('当前是外部自行启动的实例，Hanxi 不对其强制执行')
    wrapper.unmount()
  })
})

describe('Bili23View 联动辅助与版本操作', () => {
  it('数据目录钮经 extras.dataDir 直达 OpenConfigDir（成功静默）', async () => {
    stubDefaults(statusOf(), [installedV2150])
    const { wrapper } = await mountInKeepAlive()
    await findBtn(wrapper, '数据目录').trigger('click')
    await flushPromises()
    expect(svc.OpenConfigDir).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('')
    wrapper.unmount()
  })

  it('卸载必经确认：取消不动后端，APPDATA 话术逐字保留', async () => {
    stubDefaults(statusOf(), [installedV2150])
    const { confirmState, settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 Bili23 v2.15.0？')
    expect(confirmState.options.description).toContain('不受影响')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('导入本地：usePrompt 收编输入，trim 后送后端并 toast 版本', async () => {
    stubDefaults(statusOf())
    svc.ImportLocal.mockResolvedValue({ ...installedV2150, version: 'v2.14.0', isImport: true })
    const { settlePrompt } = usePrompt()
    const { wrapper } = await mountInKeepAlive()
    await findBtn(wrapper, '导入本地').trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\Tools\\Bili23  ')
    await flushPromises()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\Tools\\Bili23')
    expect(useToast().toastMsg.value).toContain('已导入 Bili23 v2.14.0')
    wrapper.unmount()
  })
})

describe('Bili23View 事件归一与轮询契约', () => {
  it('instance-state 事件仅当迁移信号：回读聚合 Status 定现态；卸载注销双订阅', async () => {
    let current = statusOf({ state: 'stopped' })
    svc.GetStatus.mockImplementation(() => Promise.resolve(current))
    svc.ListInstalledVersions.mockResolvedValue([installedV2150])
    svc.ListReleases.mockResolvedValue([])
    svc.GetActiveVersion.mockResolvedValue('v2.15.0')
    svc.GetFollowOnExit.mockResolvedValue(false)
    svc.RepositoryURL.mockResolvedValue('https://github.com/ScottSloan/Bili23-Downloader')
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    // 引擎快照 payload（instance.Snapshot，无 windowVisible 字段）只当迁移信号：
    // 必须回读聚合 Status——若直装 payload，undefined 的 windowVisible 会误判成
    // 「已收入托盘」，此断言即区分两种实现。
    current = statusOf({ state: 'running', version: 'v2.15.0', pid: 999, windowVisible: true, startedAt: new Date().toISOString() })
    runtime.handlers['bili23:instance-state']({ data: { state: 'running', version: 'v2.15.0', pid: 999, exitCode: 0, error: '', external: false, startedAt: '', stoppedAt: '' } })
    await flushPromises()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('version-download 进度键归一：按版本对齐远程表行', async () => {
    stubDefaults(statusOf(), [], [releaseV2160])
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['bili23:version-download']({ data: { version: 'v2.16.0', stage: 'downloading', done: 50, total: 100, message: '' } })
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    wrapper.unmount()
  })

  it('激活轮询 2.5s 刷新，停用后不泄漏', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults(statusOf(), [installedV2150])
      const { wrapper, show } = await mountInKeepAlive()
      const afterMount = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(afterMount + 3)
      show.value = false
      await nextTick()
      const afterDeactivate = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 5)
      expect(svc.GetStatus.mock.calls.length).toBe(afterDeactivate)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
