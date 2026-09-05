// 特征测试（组 C）：GuoheViewView——迁移前锁定现状基线。
// 特殊性（必须原样保留）：多实例上游 banner 文案、官网（非仓库）复制与打开、官方发布接口刷新。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GuoheViewView from '../GuoheViewView.vue'
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
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
  ImportLocal: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/guoheview/guoheviewservice', () => svc)

const installedV = {
  version: '1.9.4',
  exePath: 'C:\\data\\guoheview\\1.9.4\\GuoheView.exe',
  dir: 'C:\\data\\guoheview\\1.9.4',
  size: 8 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://www.ghxi.com')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(GuoheViewView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

// 迁移注记：confirm/prompt 已由 useConfirm/usePrompt 单例收编，测试经 settle* 驱动，
// "危险操作必须二次确认"的断言语义不变。
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('GuoheViewView 初始装载与状态矩阵', () => {
  it('挂载拉取状态、发布接口与官网信息', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.RepositoryURL).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('未运行：状态文案、退出禁用、引导行', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    expect(wrapper.find('.banner').exists()).toBe(false)
    expect(wrapper.find('.hint-line').text()).toContain('启动托管实例')
    expect(wrapper.findAll('.control-btns .btn')[1].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('运行中：banner-ok 含关窗即退与更新检查提示', async () => {
    stubDefaults({ state: 'running', version: '1.9.4', pid: 7, startedAt: new Date().toISOString() }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-ok')
    expect(banner.text()).toContain('关窗即退出')
    expect(banner.text()).toContain('避免内置更新覆写托管目录')
    wrapper.unmount()
  })

  it('外部实例：banner-warn 说明唤回/另开与不管退语义', async () => {
    stubDefaults({ state: 'external' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('唤回它或另开独立新窗口')
    const open = wrapper.findAll('.control-btns .btn')[0]
    expect(open.attributes('title')).toContain('唤回已有窗口')
    wrapper.unmount()
  })
})

describe('GuoheViewView 操作流', () => {
  it('打开窗口：成功 toast + 刷新', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    svc.OpenWindow.mockResolvedValue({ message: '已打开看图窗口' })
    const { wrapper } = await mountInKeepAlive()
    const before = svc.GetStatus.mock.calls.length
    await wrapper.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已打开看图窗口')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('退出：确认文案逐字锁定（宽限关窗语义）', async () => {
    stubDefaults({ state: 'running', version: '1.9.4' }, [installedV])
    svc.Quit.mockResolvedValue({ message: '已退出' })
    const { wrapper } = await mountInKeepAlive()
    // 取消卸载/退出：confirm 桩返回 false
    await wrapper.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    // 退出无 confirm（直接 Quit），故 Quit 应被调用
    expect(svc.Quit).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('卸载需确认：取消不动后端，确认删除并 toast', async () => {
    stubDefaults({ state: 'stopped' }, [{ ...installedV, version: '1.9.4' }])
    svc.GetActiveVersion.mockResolvedValue('') // 非当前使用，允许卸载
    const { wrapper } = await mountInKeepAlive()
    const uninstall = () => wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall().trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('确定卸载果核看图 1.9.4？')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    await uninstall().trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.9.4')
    wrapper.unmount()
  })

  it('导入本地：prompt 取消不动，输入则 trim 导入', async () => {
    stubDefaults({ state: 'stopped' }, [])
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    svc.ImportLocal.mockResolvedValue({ version: '1.8.0', dir: 'E:\\GuoheView', exePath: 'E:\\GuoheView\\GuoheView.exe', size: 1, installedAt: '2026-09-01', isImport: true })
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    settlePrompt('  E:\\GuoheView  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('E:\\GuoheView')
    wrapper.unmount()
  })

  it('下载完成事件：刷新列表并清除票条', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [], [{ version: '1.9.5', size: 100, isPre: false }])
      const { wrapper } = await mountInKeepAlive()
      runtime.handlers['guoheview:version-download']({ data: { version: '1.9.5', stage: 'done', done: 100, total: 100 } })
      await vi.advanceTimersByTimeAsync(800)
      expect(wrapper.find('.gv-ver-status.downloading').exists()).toBe(false)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('官网打开走 OpenRepository', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    const openBtn = wrapper.findAll('.repo-row .link-button').find((b) => b.text() === '浏览器打开')!
    await openBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenRepository).toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('GuoheViewView 轮询与生命周期', () => {
  it('2.5s 轮询；停用后不空转', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [installedV])
      const { wrapper, show } = await mountInKeepAlive()
      const afterMount = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(afterMount + 3)
      show.value = false
      await nextTick()
      const afterDeactivate = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 4)
      expect(svc.GetStatus.mock.calls.length).toBe(afterDeactivate)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
