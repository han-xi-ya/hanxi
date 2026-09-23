// 特征测试（组 B · 批 1 迁移件）：PicLiteView 收敛进托管控制台共享契约
// （src/adapters/piclite + components/managed），行为基线对照迁移前视图逐字保持——
// 仅共享面板表内标准词按批 0 契约形定档（安装中→下载中、verify/extract 阶段
// 合并为「校验解压安装…」、官方 MSI→官方下载、空态 CTA 安装最新版→下载最新版、
// 下载失败前缀 安装失败:→下载失败: 收编进 ACTION_ERROR_PREFIX 单一词源），
// 调用序列/toast/确认输入/轮询/事件断言逐字保留。绑定/事件经 vi.mock 打桩。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PicLiteView from '../PicLiteView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(), ListInstalledVersions: vi.fn(), GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(), OpenWindow: vi.fn(), Quit: vi.fn(),
  DownloadVersion: vi.fn(), SetActiveVersion: vi.fn(), RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(), OpenDir: vi.fn(), OpenConfigDir: vi.fn(),
  GetFollowOnExit: vi.fn(), SetFollowOnExit: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/piclite/picliteservice', () => svc)

const installedV = { version: '1.2.0', exePath: 'C:\\hx\\piclite\\piclite.exe', dir: 'C:\\hx\\piclite', size: 8388608, installedAt: '2026-08-01', isImport: false }
const release1 = { version: '1.2.0', size: 8388608, published: '2024-07-01T00:00:00Z', isPre: false }

// N43 后【stopped+零版本】夹具会落「未安装」呈现——本组视图测描述运行态/动作链，
// 默认补一条已装记录；确需未安装场景的用例请显式传 []。
const STUB_INSTALLED: Array<{ version: string }> = [{ version: 'v1.0.0' }]

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = STUB_INSTALLED, releases: unknown[] = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/amiaoapp/PicLite')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(PicLiteView)) : h('div')) })
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

describe('PicLiteView', () => {
  it('挂载拉全量 + 订阅 piclite 双事件', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.GetActiveVersion).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(svc.GetFollowOnExit).toHaveBeenCalled()
    expect(svc.RepositoryURL).toHaveBeenCalled()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['piclite:instance-state', 'piclite:version-download'])
    wrapper.unmount()
  })

  it('唤窗与退出动作及禁用矩阵', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: 'PicLite 已启动并打开工作台' })
    const { wrapper } = await mountView()
    const [openBtn, quitBtn] = wrapper.findAll('.control-btns .btn')
    expect(openBtn.text()).toContain('打开窗口')
    expect(openBtn.attributes('title')).toBe('启动 PicLite 并打开工作台窗口')
    expect(quitBtn.attributes('disabled')).toBeDefined()
    await openBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('PicLite 已启动并打开工作台')
    wrapper.unmount()
  })

  it('外部实例：退出钮可点且 title 指引托盘退出', async () => {
    stubDefaults({ state: 'external' })
    const { wrapper } = await mountView()
    const quitBtn = wrapper.findAll('.control-btns .btn')[1]
    expect(quitBtn.attributes('disabled')).toBeUndefined()
    expect(quitBtn.attributes('title')).toBe('外部实例请在 PicLite 托盘菜单退出')
    wrapper.unmount()
  })

  it('五态文案与提示条变体', async () => {
    stubDefaults({ state: 'running', version: '1.2.0', startedAt: new Date().toISOString() }, [installedV])
    let ctx = await mountView()
    expect(ctx.wrapper.find('.status-word').text()).toBe('运行中')
    expect(ctx.wrapper.find('.banner-ok').text()).toContain('PicLite 正在运行')
    ctx.wrapper.unmount()

    stubDefaults({ state: 'external' })
    ctx = await mountView()
    expect(ctx.wrapper.find('.banner-warn').text()).toContain('外部 PicLite 实例')
    ctx.wrapper.unmount()
  })

  it('卸载确认：取消不调后端；确认卸载 + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    let { wrapper } = await mountView()
    await wrapper.find('.installed-card .btn-danger-outline').trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('确定卸载 PicLite 1.2.0')
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
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.2.0')
    expect(useToast().toastMsg.value).toBe('已卸载 1.2.0')
    wrapper.unmount()
  })

  it('导入本地输入框取消不动作', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toContain('PicLite 安装目录完整路径')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('下载进度事件：百分比与管理提取阶段文案（契约定档「校验解压安装…」）', async () => {
    stubDefaults({ state: 'stopped' }, [], [release1])
    const { wrapper } = await mountView()
    runtime.handlers['piclite:version-download']({ data: { version: '1.2.0', stage: 'downloading', done: 75, total: 100 } })
    await nextTick()
    expect(wrapper.find('.ver-status.downloading').exists()).toBe(true)
    expect(wrapper.find('.dl-percent').text()).toBe('75%')
    runtime.handlers['piclite:version-download']({ data: { version: '1.2.0', stage: 'extract', done: 100, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toContain('校验解压安装')
    wrapper.unmount()
  })

  it('联动开关与数据目录：经 adapter extras 走后端并回执 toast', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    await wrapper.find('.extras-card input[type="checkbox"]').trigger('change')
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false)
    expect(useToast().toastMsg.value).toBe('已关闭：Hanxi 退出不影响该工具，PicLite 继续独立运行（下次启动生效）')

    await wrapper.findAll('.extras-card .btn').find((b) => b.text().includes('数据目录'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenConfigDir).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('轮询激活启动、停用停止；事件推送即时改写', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['piclite:instance-state']({ data: { state: 'failed', error: '进程闪退' } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('异常退出')
    wrapper.unmount()

    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      const ctx = await mountView()
      const base = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(7500)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
      ctx.show.value = false
      await nextTick()
      const after = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(10000)
      expect(svc.GetStatus.mock.calls.length).toBe(after)
      ctx.wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
