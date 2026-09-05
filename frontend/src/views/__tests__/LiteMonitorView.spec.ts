// 特征测试（组 B）：LiteMonitorView 迁移前行为基线；迁移后除确认/输入交互机制外逐字保持。
// LiteMonitor 特有：.NET 8 运行时缺失常驻警示（独立于状态横幅）、GetRuntimeStatus 拉取。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import LiteMonitorView from '../LiteMonitorView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(), ListInstalledVersions: vi.fn(), GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(), GetRuntimeStatus: vi.fn(), OpenWindow: vi.fn(), Quit: vi.fn(),
  DownloadVersion: vi.fn(), SetActiveVersion: vi.fn(), RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(), OpenDir: vi.fn(), GetFollowOnExit: vi.fn(), SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(), RepositoryURL: vi.fn(), OpenRepository: vi.fn(),
}))

const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, (event: { data: unknown }) => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return () => {}
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/litemonitor/litemonitorservice', () => svc)

const installedV = { version: '1.0.5', exePath: 'C:\\hx\\lm\\LiteMonitor.exe', dir: 'C:\\hx\\lm', size: 1572864, installedAt: '2026-08-01', isImport: false }
const release1 = { version: '1.0.5', size: 1572864, published: '2024-08-01T00:00:00Z', isPre: false }

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: unknown[] = [], hasDesktop8 = true) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.GetRuntimeStatus.mockResolvedValue({ hasDesktop8 })
  svc.RepositoryURL.mockResolvedValue('https://github.com/Diorser/LiteMonitor')
}

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(LiteMonitorView)) : h('div')) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

// 迁移注记（有意变更）：window.confirm/prompt 已由 useConfirm/usePrompt 全局单例收编，
// 交互面驱动相应改为 settleConfirm/settlePrompt；文案与调用序列断言逐字保持。
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

afterEach(() => {
  settleConfirm(false) // 防悬挂
  settlePrompt(null)
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('LiteMonitorView', () => {
  it('挂载拉全量（含运行时检测）+ 订阅双事件', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.GetRuntimeStatus).toHaveBeenCalled()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['litemonitor:instance-state', 'litemonitor:version-download'])
    wrapper.unmount()
  })

  it('缺 .NET 8 桌面运行时时常驻警示；具备时无', async () => {
    stubDefaults({ state: 'stopped' }, [], [], false)
    let ctx = await mountView()
    expect(ctx.wrapper.text()).toContain('未检测到 .NET 8 桌面运行时')
    ctx.wrapper.unmount()

    stubDefaults({ state: 'stopped' }, [], [], true)
    ctx = await mountView()
    expect(ctx.wrapper.text()).not.toContain('未检测到 .NET 8 桌面运行时')
    ctx.wrapper.unmount()
  })

  it('唤窗动作与状态文案', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: 'LiteMonitor 已启动' })
    const { wrapper } = await mountView()
    const [openBtn, quitBtn] = wrapper.findAll('.control-btns .btn')
    expect(openBtn.text()).toContain('打开窗口')
    expect(quitBtn.attributes('disabled')).toBeDefined()
    await openBtn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('LiteMonitor 已启动')
    wrapper.unmount()

    stubDefaults({ state: 'running', version: '1.0.5', startedAt: new Date().toISOString() }, [installedV])
    const ctx = await mountView()
    expect(ctx.wrapper.find('.status-word').text()).toBe('运行中')
    expect(ctx.wrapper.find('.banner-ok').text()).toContain('LiteMonitor 正在运行')
    ctx.wrapper.unmount()
  })

  it('卸载确认：取消不调后端；确认卸载 + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    let { wrapper } = await mountView()
    await wrapper.find('.installed-card .btn-danger-outline').trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('确定卸载 LiteMonitor 1.0.5')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()

    svc.RemoveVersion.mockResolvedValue(undefined)
    ;({ wrapper } = await mountView())
    await wrapper.find('.installed-card .btn-danger-outline').trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.0.5')
    expect(useToast().toastMsg.value).toBe('已卸载 1.0.5')
    wrapper.unmount()
  })

  it('下载失败 toast 用「下载失败」前缀（本视图特有措辞）', async () => {
    stubDefaults({ state: 'stopped' }, [], [release1])
    svc.DownloadVersion.mockRejectedValue(new Error('GitHub 限流'))
    const { wrapper } = await mountView()
    await wrapper.find('.tbl tbody .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('下载失败: GitHub 限流')
    wrapper.unmount()
  })

  it('导入本地输入框取消不动作', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toContain('LiteMonitor 便携目录完整路径')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('下载进度事件：verify 阶段文案为「校验解压安装…」（三态合并措辞）', async () => {
    stubDefaults({ state: 'stopped' }, [], [release1])
    const { wrapper } = await mountView()
    runtime.handlers['litemonitor:version-download']({ data: { version: '1.0.5', stage: 'verify', done: 100, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toContain('校验解压安装')
    wrapper.unmount()
  })

  it('轮询激活启动、停用停止', async () => {
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
      await vi.advanceTimersByTimeAsync(10000)
      expect(svc.GetStatus.mock.calls.length).toBe(after)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
