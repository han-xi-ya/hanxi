// 特征测试（组 B）：QuickLookView 迁移前行为基线。
// 迁移（Phase 4：useConfirm/usePrompt/useWailsEvent/usePolling/共享件接管）后本文件
// 除"确认/输入框交互机制"按有意变更调整外，其余断言必须逐字保持全绿。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuickLookView from '../QuickLookView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(), ListInstalledVersions: vi.fn(), GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(), StartQuickLook: vi.fn(), Quit: vi.fn(), Reload: vi.fn(),
  DownloadVersion: vi.fn(), SetActiveVersion: vi.fn(), RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(), OpenDir: vi.fn(), GetFollowOnExit: vi.fn(), SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(), OpenRepository: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return () => {}
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/quicklook/quicklookservice', () => svc)

const installedV = { version: '0.4.0', exePath: 'C:\\hx\\0.4.0\\QuickLook.exe', dir: 'C:\\hx\\0.4.0', size: 4194304, installedAt: '2026-08-01', isImport: false }
const release1 = { version: '0.4.0', size: 4194304, published: '2024-05-01T00:00:00Z', isPre: false }

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: unknown[] = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/QL-Win/QuickLook')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(QuickLookView)) : h('div')) })
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

describe('QuickLookView', () => {
  it('挂载拉取状态/版本/联动开关，订阅双事件', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.GetFollowOnExit).toHaveBeenCalled()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['quicklook:instance-state', 'quicklook:version-download'])
    wrapper.unmount()
  })

  it('状态灯五态文案与按钮禁用矩阵', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    expect(wrapper.find('.ql-status-light').classes()).toContain('stopped')
    const startBtn = wrapper.findAll('.control-btns .btn')[0]
    const quitBtn = wrapper.findAll('.control-btns .btn')[2]
    expect(startBtn.attributes('disabled')).toBeUndefined()
    expect(quitBtn.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('running：banner-ok + 重载可用 + 启动禁用 + 运行时长行', async () => {
    stubDefaults({ state: 'running', version: '0.4.0', pid: 777, startedAt: new Date().toISOString() }, [installedV])
    const { wrapper } = await mountView()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.banner-ok').exists()).toBe(true)
    expect(wrapper.find('.banner-ok').text()).toContain('QuickLook 正在运行')
    expect(wrapper.find('.pid-tag').text()).toContain('777')
    expect(wrapper.find('.uptime-tag').exists()).toBe(true)
    wrapper.unmount()
  })

  it('external→banner-warn；failed→banner-error 显示快照错误', async () => {
    stubDefaults({ state: 'external' })
    let ctx = await mountView()
    expect(ctx.wrapper.find('.banner-warn').text()).toContain('外部 QuickLook 实例')
    ctx.wrapper.unmount()

    stubDefaults({ state: 'failed', error: '命名管道连接失败' })
    ctx = await mountView()
    expect(ctx.wrapper.find('.banner-error').text()).toContain('命名管道连接失败')
    ctx.wrapper.unmount()
  })

  it('启动成功：toast 回传 message 并刷新状态；失败 toast 原始错误', async () => {
    stubDefaults({ state: 'stopped' })
    svc.StartQuickLook.mockResolvedValue({ message: 'QuickLook 已启动' })
    let { wrapper } = await mountView()
    await wrapper.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('QuickLook 已启动')
    wrapper.unmount()

    stubDefaults({ state: 'stopped' })
    svc.StartQuickLook.mockRejectedValue(new Error('找不到版本'))
    ;({ wrapper } = await mountView())
    await wrapper.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('找不到版本')
    wrapper.unmount()
  })

  it('卸载经确认框：取消不动后端；确认则 RemoveVersion + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    let { wrapper } = await mountView()
    await wrapper.find('.installed-card .btn-danger-outline').trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('确定卸载 QuickLook 0.4.0')
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
    expect(svc.RemoveVersion).toHaveBeenCalledWith('0.4.0')
    expect(useToast().toastMsg.value).toBe('已卸载 0.4.0')
    wrapper.unmount()
  })

  it('导入本地：输入框取消不动作', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toContain('QuickLook 便携目录完整路径')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('instance-state 事件即时改写界面（不待轮询）', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['quicklook:instance-state']({ data: { state: 'running', version: '0.4.0', startedAt: new Date().toISOString() } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    wrapper.unmount()
  })

  it('version-download 事件驱动进度条百分比与校验/解压文案', async () => {
    stubDefaults({ state: 'stopped' }, [], [release1])
    const { wrapper } = await mountView()
    runtime.handlers['quicklook:version-download']({ data: { version: '0.4.0', stage: 'downloading', done: 50, total: 100 } })
    await nextTick()
    expect(wrapper.find('.ql-ver-status.downloading').exists()).toBe(true)
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    runtime.handlers['quicklook:version-download']({ data: { version: '0.4.0', stage: 'verify', done: 100, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toContain('哈希校验')
    wrapper.unmount()
  })

  it('轮询：激活期 2.5s 刷新；KeepAlive 停用后停止不泄漏', async () => {
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

  it('主选项卡切换', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    const tabs = wrapper.findAll('.main-tab-btn')
    expect(tabs[1].text()).toContain('版本管理')
    await tabs[1].trigger('click')
    expect(tabs[1].classes()).toContain('active')
    wrapper.unmount()
  })
})
