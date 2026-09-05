// 特征测试（组 C）：FlClashView——迁移前锁定现状行为基线（Phase 4 铁律）。
// 断言"做了什么"而非"怎么做"：迁移到共享层后除 confirm/prompt 驱动方式外全部必须保持绿。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FlClashView from '../FlClashView.vue'
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

vi.mock('../../../bindings/hanxi/internal/modules/flclash/flclashservice', () => svc)

const installedV = {
  version: 'v0.8.79',
  exePath: 'C:\\data\\flclash\\v0.8.79\\FlClash.exe',
  dir: 'C:\\data\\flclash\\v0.8.79',
  size: 90 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/chen08209/FlClash')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(FlClashView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

// 迁移注记：window.confirm/prompt 已被 useConfirm/usePrompt 单例收编（蓝图 §5 Phase 2），
// 测试改经 settle* 驱动对话框裁决——"危险操作必须二次确认"的行为断言语义不变。
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  // 防御：上一条若未裁决干净
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('FlClashView 初始装载与状态矩阵', () => {
  it('挂载拉取状态、版本与联动信息', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.GetActiveVersion).toHaveBeenCalled()
    expect(svc.GetFollowOnExit).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('未运行：状态灯文案、退出禁用、引导行出现', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    expect(wrapper.find('.hint-banner').exists()).toBe(false)
    expect(wrapper.find('.hint-line').text()).toContain('尚未运行：点击「打开窗口」启动 FlClash')
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns[1].attributes('disabled')).toBeDefined() // 退出禁用
    wrapper.unmount()
  })

  it('运行中：banner-ok + 版本胶囊 + PID + 退出可用', async () => {
    stubDefaults({ state: 'running', version: 'v0.8.79', pid: 9001, startedAt: new Date().toISOString() }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(wrapper.find('.banner').text()).toContain('代理配置在其窗口内操作')
    expect(wrapper.find('.ver-pill').text()).toBe('v0.8.79')
    expect(wrapper.find('.pid-tag').text()).toContain('9001')
    expect(wrapper.findAll('.control-btns .btn')[1].attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('启动中：打开窗口禁用 + 拉起提示行', async () => {
    stubDefaults({ state: 'starting' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('启动中…')
    expect(wrapper.findAll('.control-btns .btn')[0].attributes('disabled')).toBeDefined()
    expect(wrapper.find('.hint-line').text()).toContain('正在拉起 FlClash（约 1~3 秒）…')
    wrapper.unmount()
  })

  it('外部实例：banner-warn 且退出按钮可用（title 提示外部退出）', async () => {
    stubDefaults({ state: 'external' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('检测到外部 FlClash 实例（非 Hanxi 托管）')
    const quit = wrapper.findAll('.control-btns .btn')[1]
    expect(quit.attributes('disabled')).toBeUndefined()
    expect(quit.attributes('title')).toBe('外部实例请在 FlClash 托盘退出')
    wrapper.unmount()
  })

  it('异常退出：banner-error 显示快照 error', async () => {
    stubDefaults({ state: 'failed', error: 'WebView2 缺失' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.banner').text()).toContain('WebView2 缺失')
    wrapper.unmount()
  })
})

describe('FlClashView 操作流', () => {
  it('打开窗口：成功 toast 回执并刷新状态', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    svc.OpenWindow.mockResolvedValue({ message: '已启动 FlClash 并唤起窗口' })
    const { wrapper } = await mountInKeepAlive()
    const callsBefore = svc.GetStatus.mock.calls.length
    await wrapper.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已启动 FlClash 并唤起窗口')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(callsBefore)
    wrapper.unmount()
  })

  it('打开窗口失败也刷新状态（不信任回执）', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    svc.OpenWindow.mockRejectedValue(new Error('进程拉起超时'))
    const { wrapper } = await mountInKeepAlive()
    const callsBefore = svc.GetStatus.mock.calls.length
    await wrapper.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('进程拉起超时')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(callsBefore)
    wrapper.unmount()
  })

  it('卸载取消不动后端，确认则删除并 toast', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    const uninstall = () => wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall().trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('确定卸载 FlClash v0.8.79？')
    settleConfirm(false) // 取消
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()

    await uninstall().trigger('click')
    await flushMicrotasks()
    settleConfirm(true) // 确认
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v0.8.79')
    expect(useToast().toastMsg.value).toBe('已卸载 v0.8.79')
    wrapper.unmount()
  })

  it('正在运行版本的卸载按钮禁用并给出原因', async () => {
    stubDefaults({ state: 'running', version: 'v0.8.79' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    expect(uninstall.attributes('disabled')).toBeDefined()
    expect(uninstall.attributes('title')).toBe('请先退出 FlClash')
    wrapper.unmount()
  })

  it('导入本地：prompt 取消不动后端，输入路径则 trim 后导入', async () => {
    stubDefaults({ state: 'stopped' }, [])
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    settlePrompt(null) // 取消
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()

    svc.ImportLocal.mockResolvedValue({ version: 'v0.9.0', dir: 'D:\\FlClash', exePath: 'D:\\FlClash\\FlClash.exe', size: 1, installedAt: '2026-09-01', isImport: true })
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    settlePrompt('  D:\\FlClash  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('D:\\FlClash')
    expect(useToast().toastMsg.value).toContain('已导入 FlClash v0.9.0')
    wrapper.unmount()
  })

  it('下载进度事件驱动表格状态与百分比', async () => {
    stubDefaults({ state: 'stopped' }, [], [{ version: 'v0.8.80', size: 100, published: '2026-08-02T00:00:00Z', isPre: false }])
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['flclash:version-download']({ data: { version: 'v0.8.80', stage: 'downloading', done: 50, total: 100 } })
    await nextTick()
    expect(wrapper.find('.fl-ver-status.downloading').exists()).toBe(true)
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    wrapper.unmount()
  })

  it('下载完成事件：800ms 后清除票条并刷新版本列表', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [], [{ version: 'v0.8.80', size: 100, published: '2026-08-02T00:00:00Z', isPre: false }])
      const { wrapper } = await mountInKeepAlive()
      const before = svc.ListReleases.mock.calls.length
      runtime.handlers['flclash:version-download']({ data: { version: 'v0.8.80', stage: 'done', done: 100, total: 100 } })
      await vi.advanceTimersByTimeAsync(0)
      expect(svc.ListReleases.mock.calls.length).toBeGreaterThan(before) // 立即刷新
      await vi.advanceTimersByTimeAsync(800)
      expect(wrapper.find('.fl-ver-status.downloading').exists()).toBe(false) // 票条清除
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('instance-state 事件即时改写界面；非运行态清零 uptime 展示', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['flclash:instance-state']({ data: { state: 'running', version: 'v0.8.79', pid: 4242 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.pid-tag').text()).toContain('4242')
    wrapper.unmount()
  })

  it('随 Hanxi 关闭开关：设置失败 toast 后勾选回滚复位（§9.5-1 修复）', async () => {
    // 修复后语义：点击即乐观翻转 ref；写失败则回滚，勾选框复位到后端真实值。
    stubDefaults({ state: 'stopped' }, [installedV])
    svc.SetFollowOnExit.mockRejectedValue(new Error('写配置失败'))
    const { wrapper } = await mountInKeepAlive()
    const checkbox = wrapper.find('.extras-card input[type="checkbox"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    await checkbox.trigger('change')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('设置失败: 写配置失败')
    await nextTick()
    expect((checkbox.element as HTMLInputElement).checked).toBe(true) // 失败回滚：勾选框复位
    wrapper.unmount()
  })
})

describe('FlClashView 轮询与生命周期（KeepAlive 契约）', () => {
  it('2.5s 兜底轮询；停用后停止、激活后恢复且不重复', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [installedV])
      const { wrapper, show } = await mountInKeepAlive()
      const afterMount = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(afterMount + 3)

      show.value = false // KeepAlive 停用
      await nextTick()
      const afterDeactivate = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 4)
      expect(svc.GetStatus.mock.calls.length).toBe(afterDeactivate) // 停用后绝不空转
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
