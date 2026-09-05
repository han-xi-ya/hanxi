// 特征测试（Phase 4 组 E）：BCUView 迁移前行为基线。
// 锁定：六态文案/推荐变体/双变体下载键/确认与导入文案/事件改写/KeepAlive 轮询契约。
// 迁移到共享层后，除 window.confirm/prompt 的"交互机制"断言换为 useConfirm/usePrompt
// 单例外，其余断言必须原样保持全绿。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BCUView from '../BCUView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  GetDotnetEnvironment: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  OpenDir: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/bcu/bcuservice', () => svc)

const installed610 = {
  version: '6.1.0',
  exePath: 'C:\\data\\bcu\\6.1.0\\BCUninstaller.exe',
  dir: 'C:\\data\\bcu\\6.1.0',
  size: 76 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
}

const release620 = {
  version: '6.2.0',
  size: 76 * 1024 * 1024,
  published: '2026-08-02T00:00:00Z',
  isPre: false,
  fddName: 'bcu-6.2.0-fdd.zip',
  fddSize: 12 * 1024 * 1024,
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(snap: Record<string, unknown>, opts: { installed?: unknown[]; releases?: unknown[]; hasNet8?: boolean } = {}) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [installed610])
  svc.ListReleases.mockResolvedValue(opts.releases ?? [release620])
  svc.GetActiveVersion.mockResolvedValue('6.1.0')
  svc.GetDotnetEnvironment.mockResolvedValue({ hasNet8: opts.hasNet8 ?? true, desktopVersions: ['8.0.11'] })
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/BCUninstaller/Bulk-Crap-Uninstaller')
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(BCUView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  // 迁移后交互面：window.confirm/prompt 已被 useConfirm/usePrompt 单例收编，
  // 测试通过 settle* 模拟宿主对话框的确认/取消出口。
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('BCUView 初始装载与状态文案', () => {
  it('挂载拉取状态/版本/.NET 环境/联动开关', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    for (const fn of [svc.GetStatus, svc.ListReleases, svc.ListInstalledVersions, svc.GetActiveVersion, svc.GetDotnetEnvironment, svc.GetFollowOnExit, svc.RepositoryURL]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('五态文案：运行中/启动中…/异常退出/外部运行/未运行', async () => {
    const cases: Array<[Record<string, unknown>, string]> = [
      [{ state: 'stopped' }, '未运行'],
      [{ state: 'running', version: '6.1.0', startedAt: new Date().toISOString() }, '运行中'],
      [{ state: 'starting' }, '启动中…'],
      [{ state: 'failed', error: 'BCU 进程启动失败' }, '异常退出'],
      [{ state: 'external' }, '外部运行'],
    ]
    for (const [snap, word] of cases) {
      stubDefaults(snap)
      const { wrapper } = await mountView()
      expect(wrapper.find('.status-word').text()).toBe(word)
      wrapper.unmount()
    }
  })

  it('提示条三分支互斥：external 警告 / failed 带后端错误 / running 成功说明', async () => {
    // 迁移后提示条为 UiBanner（全局 .banner/.banner-{tone} 原子）
    stubDefaults({ state: 'external' })
    let r = await mountView()
    expect(r.wrapper.find('.banner').classes()).toContain('banner-warn')
    r.wrapper.unmount()

    stubDefaults({ state: 'failed', error: 'WebView2 缺失' })
    r = await mountView()
    expect(r.wrapper.find('.banner').text()).toContain('WebView2 缺失')
    r.wrapper.unmount()

    stubDefaults({ state: 'running', version: '6.1.0' })
    r = await mountView()
    expect(r.wrapper.find('.banner').classes()).toContain('banner-ok')
    r.wrapper.unmount()
  })
})

describe('BCUView .NET 环境与推荐变体', () => {
  it('本机有 .NET 8 → 推荐精简版（ok 徽标，精简版按钮 primary）', async () => {
    stubDefaults({ state: 'stopped' }, { hasNet8: true })
    const { wrapper } = await mountView()
    expect(wrapper.find('.dotnet-banner').classes()).toContain('ok')
    expect(wrapper.find('.dotnet-banner').text()).toContain('推荐下载')
    const btns = wrapper.findAll('.variant-btns button')
    // 便携版 / 精简版：推荐 fdd → 精简版 primary
    expect(btns[0].classes()).toContain('btn-secondary')
    expect(btns[1].classes()).toContain('btn-primary')
    wrapper.unmount()
  })

  it('无 .NET 8 → 推荐便携版（warn 徽标警示精简版无法启动）', async () => {
    stubDefaults({ state: 'stopped' }, { hasNet8: false })
    const { wrapper } = await mountView()
    expect(wrapper.find('.dotnet-banner').classes()).toContain('warn')
    expect(wrapper.find('.dotnet-banner').text()).toContain('无法启动')
    const btns = wrapper.findAll('.variant-btns button')
    expect(btns[0].classes()).toContain('btn-primary')
    expect(btns[1].classes()).toContain('btn-secondary')
    wrapper.unmount()
  })
})

describe('BCUView 操作流', () => {
  it('打开窗口：调 OpenWindow → toast 回执 → 刷新状态', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: '已启动 BCU' })
    const { wrapper } = await mountView()
    const before = svc.GetStatus.mock.calls.length
    const openBtn = wrapper.findAll('.control-btns button')[0]
    await openBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('已启动 BCU')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('退出按钮禁用态：stopped 禁用、external 可用（不越权代退）', async () => {
    stubDefaults({ state: 'stopped' })
    let r = await mountView()
    expect(r.wrapper.findAll('.control-btns button')[1].attributes('disabled')).toBeDefined()
    r.wrapper.unmount()

    stubDefaults({ state: 'external' })
    r = await mountView()
    svc.Quit.mockResolvedValue({ message: '已发送关闭窗口消息' })
    const quitBtn = r.wrapper.findAll('.control-btns button')[1]
    expect(quitBtn.attributes('disabled')).toBeUndefined()
    await quitBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    r.wrapper.unmount()
  })

  it('双变体下载：键为 version|variant，进度事件按变体独立渲染', async () => {
    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.variant-btns button')
    await btns[1].trigger('click') // 精简版
    expect(svc.DownloadVersion).toHaveBeenCalledWith('6.2.0', 'fdd')

    runtime.handlers['bcu:version-download']({
      data: { version: '6.2.0', variant: 'fdd', stage: 'downloading', done: 50, total: 100 },
    })
    await nextTick()
    expect(wrapper.find('.variant-progress .dl-percent').text()).toBe('50%')
    // portable 未受影响：状态列整体为下载中
    expect(wrapper.find('.bcu-ver-status').classes()).toContain('downloading')
    wrapper.unmount()
  })

  it('卸载确认：取消不动后端，确认走 RemoveVersion 并刷新', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    // 迁移后由 useConfirm 全局单例承接，文案逐字保留（含"不可恢复"警示）
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 BCU 6.1.0？')
    expect(confirmState.options.description).toContain('不可恢复')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('6.1.0')
    expect(useToast().toastMsg.value).toBe('已卸载 6.1.0')
    wrapper.unmount()
  })

  it('导入本地：prompt 取消不导入，填路径 trim 后导入', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ version: '6.0.0' })
    const { wrapper } = await mountView()
    const importBtn = wrapper.findAll('.btn-group button')[0]

    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.label).toContain('BCUninstaller.exe') // 原 prompt 首行指引逐字保留
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()

    await importBtn.trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\bcu\\copy  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\bcu\\copy')
    expect(useToast().toastMsg.value).toContain('已导入 BCU 6.0.0')
    wrapper.unmount()
  })

  it('运行中的版本禁止卸载（按钮禁用）', async () => {
    stubDefaults({ state: 'running', version: '6.1.0' })
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    expect(uninstallBtn.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})

describe('BCUView 事件与轮询生命周期', () => {
  it('instance-state 事件即时改写界面；卸载双 unlisten', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['bcu:instance-state']({ data: { state: 'running', version: '6.2.0', pid: 777 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.pid-tag').text()).toContain('777')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('激活即轮询 2.5s 刷状态；KeepAlive 停用后停止（不泄漏）', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      const { wrapper, show } = await mountView()
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

  it('下载完成事件：800ms 后清进度并刷新版本列表', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      const { wrapper } = await mountView()
      runtime.handlers['bcu:version-download']({ data: { version: '6.2.0', variant: 'portable', stage: 'done' } })
      await vi.advanceTimersByTimeAsync(0)
      // done 立刻触发 loadVersions 重拉，并挂出 800ms 的进度清除定时器
      expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(1)
      expect(wrapper.find('.variant-progress').exists()).toBe(true)
      await vi.advanceTimersByTimeAsync(900)
      expect(wrapper.find('.variant-progress').exists()).toBe(false)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
