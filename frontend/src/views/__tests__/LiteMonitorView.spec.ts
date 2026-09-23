// 特征测试（组 B · 批 1 迁移件）：LiteMonitorView 收敛进托管控制台共享契约
// （src/adapters/litemonitor + components/managed），行为基线对照迁移前视图逐字保持。
// 特化点断言：① GetRuntimeStatus 第三路数据经 adapter 快照扩展字段并入——
// 视图生命周期内仅单发（轮询期不重探）、缺运行时警示独立于状态横幅可并存；
// ② 740 提权直拒（error 含"管理员"）→ #console-extra 槽渲染 ElevateRestart；
// ③ 辅助卡「📂 打开位置」经 adapter 三源镜像解析目标目录（无版本时失败回执）。
// 迁移注记（有意变更）：导入钮词「⇥ 导入本地套件」按共享面板定档为「⇥ 导入本地安装」；
// 警示横幅相对状态横幅/引导行的位置由"上方"移至状态头之后（元素与文案不变）。
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

// ElevateRestart 组件挂载时探测 IsElevated（未提权 → 显示提权重启钮）
const app = vi.hoisted(() => ({ AppService: { IsElevated: vi.fn(), RestartElevated: vi.fn() } }))

const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, (event: { data: unknown }) => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return () => {}
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/app', () => app)
vi.mock('../../../bindings/hanxi/internal/modules/litemonitor/litemonitorservice', () => svc)

const installedV = { version: '1.0.5', exePath: 'C:\\hx\\lm\\LiteMonitor.exe', dir: 'C:\\hx\\lm', size: 1572864, installedAt: '2026-08-01', isImport: false }
const release1 = { version: '1.0.5', size: 1572864, published: '2024-08-01T00:00:00Z', isPre: false }

// N43 后【stopped+零版本】夹具会落「未安装」呈现——本组视图测描述运行态/动作链，
// 默认补一条已装记录；确需未安装场景的用例请显式传 []。
const STUB_INSTALLED: Array<{ version: string }> = [{ version: 'v1.0.0' }]

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = STUB_INSTALLED, releases: unknown[] = [], hasDesktop8 = true) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.GetRuntimeStatus.mockResolvedValue({ hasDesktop8 })
  svc.RepositoryURL.mockResolvedValue('https://github.com/Diorser/LiteMonitor')
  app.AppService.IsElevated.mockResolvedValue(false)
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

// 确认/输入交互经 useConfirm/usePrompt 全局单例驱动；文案断言逐字保持。
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

afterEach(() => {
  settleConfirm(false) // 防悬挂
  settlePrompt(null)
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('LiteMonitorView', () => {
  it('挂载拉全量（含运行时检测单发）+ 订阅双事件', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.GetRuntimeStatus).toHaveBeenCalledTimes(1)
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

  it('警示与状态横幅独立并存（快照扩展字段随事件回流不丢失）', async () => {
    stubDefaults({ state: 'stopped' }, [installedV], [], false)
    const { wrapper } = await mountView()
    runtime.handlers['litemonitor:instance-state']({ data: { state: 'running', version: '1.0.5', startedAt: new Date().toISOString() } })
    await nextTick()
    expect(wrapper.find('.banner-ok').text()).toContain('LiteMonitor 正在运行')
    expect(wrapper.find('.banner-warn').text()).toContain('未检测到 .NET 8 桌面运行时')
    wrapper.unmount()
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

  it('740 提权直拒：failed 且错误含"管理员"时出现提权重启钮；普通异常不出现', async () => {
    stubDefaults({ state: 'failed', error: '需要管理员权限运行（Win32 740 elevateHint）' })
    let ctx = await mountView()
    expect(ctx.wrapper.find('.banner-error').exists()).toBe(true)
    expect(ctx.wrapper.find('.elevate-restart').exists()).toBe(true)
    ctx.wrapper.unmount()

    stubDefaults({ state: 'failed', error: '进程闪退' })
    ctx = await mountView()
    expect(ctx.wrapper.find('.banner-error').exists()).toBe(true)
    expect(ctx.wrapper.find('.elevate-restart').exists()).toBe(false)
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

  it('下载失败 toast 用「下载失败」前缀（本视图特有措辞，与共享契约词源一致）', async () => {
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

  it('联动开关注释、随关回执与「打开位置」三源解析', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    svc.OpenDir.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    expect(wrapper.find('.toggle-label .hint-dim').text()).toContain('开启监控条常驻可关除此项')

    await wrapper.find('.extras-card input[type="checkbox"]').trigger('change')
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false)
    expect(useToast().toastMsg.value).toBe('已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')

    await wrapper.findAll('.extras-card .btn').find((b) => b.text().includes('打开位置'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenDir).toHaveBeenCalledWith('C:\\hx\\lm')

    await wrapper.findAll('.extras-card .btn').find((b) => b.text().includes('快捷方式'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.CreateDesktopShortcut).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('轮询激活启动、停用停止；运行时探测轮询期不重发', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      const { wrapper, show } = await mountView()
      const base = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
      expect(svc.GetRuntimeStatus).toHaveBeenCalledTimes(1)
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
