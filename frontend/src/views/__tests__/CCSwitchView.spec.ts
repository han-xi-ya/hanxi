// 特征测试（组 A 模板之二）：CCSwitchView 与 MarkerOnView 同构但各有专属分支——
// 打开窗口/退出双按钮、导入本地（usePrompt 收编 window.prompt）、卸载二次确认。
// 绑定/事件经 vi.mock 打桩；KeepAlive 宿主复刻 App.vue 外壳。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CCSwitchView from '../CCSwitchView.vue'
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
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/ccswitch/ccswitchservice', () => svc)

const installedV342 = {
  version: 'v3.4.2',
  exePath: 'C:\\data\\ccswitch\\v3.4.2\\cc-switch.exe',
  dir: 'C:\\data\\ccswitch\\v3.4.2',
  size: 8 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
  source: '',
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/farion1231/cc-switch')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(CCSwitchView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('CCSwitchView 装载与状态', () => {
  it('初始并发拉取状态/版本/活动版本/联动开关/仓库', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    for (const fn of [svc.GetStatus, svc.ListReleases, svc.ListInstalledVersions, svc.GetActiveVersion, svc.GetFollowOnExit, svc.RepositoryURL]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('未运行时提示引导行存在、banner 缺席', async () => {
    stubDefaults({ state: 'stopped' }, [installedV342])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    expect(wrapper.find('.banner').exists()).toBe(false)
    expect(wrapper.find('.hint-line').text()).toContain('尚未运行')
    wrapper.unmount()
  })

  it('运行中：banner-ok + 版本号 + 退出按钮可用', async () => {
    stubDefaults({ state: 'running', version: 'v3.4.2', pid: 777, startedAt: new Date().toISOString() }, [installedV342])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(wrapper.find('.banner').text()).toContain('闲置 3 分钟自动退出')
    const quit = wrapper.findAll('.control-btns .btn').find((b) => b.text().includes('退出'))!
    expect(quit.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('外部实例：banner-warn；停动态退出按钮禁用，外部态按钮保持可用（title 指引托盘退出）', async () => {
    stubDefaults({ state: 'external' }, [installedV342])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.banner').classes()).toContain('banner-warn')
    const quit = wrapper.findAll('.control-btns .btn').find((b) => b.text().includes('退出'))!
    // 现状契约：external 可点（title 提示"外部实例请在 CC Switch 托盘退出"），仅完全停止态禁用
    expect(quit.attributes('disabled')).toBeUndefined()
    expect(quit.attributes('title')).toContain('外部实例')
    wrapper.unmount()

    // 对照：stopped 态退出禁用
    vi.clearAllMocks()
    stubDefaults({ state: 'stopped' }, [installedV342])
    const { wrapper: w2 } = await mountInKeepAlive()
    const quit2 = w2.findAll('.control-btns .btn').find((b) => b.text().includes('退出'))!
    expect(quit2.attributes('disabled')).toBeDefined()
    w2.unmount()
  })

  it('异常退出：banner-error 显示后端错误文案', async () => {
    stubDefaults({ state: 'failed', error: '单实例协议超时' }, [installedV342])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.banner').text()).toContain('单实例协议超时')
    wrapper.unmount()
  })
})

describe('CCSwitchView 控制操作', () => {
  it('打开窗口：成功走回执 toast 并刷新状态', async () => {
    stubDefaults({ state: 'stopped' }, [installedV342])
    svc.OpenWindow.mockResolvedValue({ message: '已启动并唤起窗口' })
    const { wrapper } = await mountInKeepAlive()
    const open = wrapper.findAll('.control-btns .btn').find((b) => b.text().includes('打开窗口'))!
    await open.trigger('click')
    await flushPromises()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已启动并唤起窗口')
    wrapper.unmount()
  })

  it('打开窗口抛错也 toast 且刷新状态、恢复非 busy', async () => {
    stubDefaults({ state: 'stopped' }, [installedV342])
    svc.OpenWindow.mockRejectedValue(new Error('Tauri 单实例通道失败'))
    const { wrapper } = await mountInKeepAlive()
    const open = wrapper.findAll('.control-btns .btn').find((b) => b.text().includes('打开窗口'))!
    await open.trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('Tauri 单实例通道失败')
    expect(open.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('退出：Quit 回执 toast', async () => {
    stubDefaults({ state: 'running', version: 'v3.4.2' }, [installedV342])
    svc.Quit.mockResolvedValue({ message: '已发送退出' })
    const { wrapper } = await mountInKeepAlive()
    const quit = wrapper.findAll('.control-btns .btn').find((b) => b.text().includes('退出'))!
    await quit.trigger('click')
    await flushPromises()
    expect(svc.Quit).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已发送退出')
    wrapper.unmount()
  })
})

describe('CCSwitchView 危险与输入流程（useConfirm / usePrompt 收编）', () => {
  it('卸载必经确认：取消不动后端，文案逐字保留', async () => {
    stubDefaults({ state: 'stopped' }, [installedV342])
    const { confirmState, settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 CC Switch v3.4.2？')
    expect(confirmState.options.description).toContain('供应商配置不受影响')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('确认卸载：RemoveVersion + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installedV342])
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushPromises()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v3.4.2')
    expect(useToast().toastMsg.value).toBe('已卸载 v3.4.2')
    wrapper.unmount()
  })

  it('导入本地：usePrompt 收编原生输入，取消不调后端', async () => {
    stubDefaults({ state: 'stopped' })
    const { promptState, settlePrompt } = usePrompt()
    const { wrapper } = await mountInKeepAlive()
    const importBtn = wrapper.findAll('.btn-group button').find((b) => b.text().includes('导入本地'))!
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toBe('导入本地 CC Switch')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('导入本地：提交路径 trim 后送后端并 toast 版本', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ version: 'v3.5.0' })
    const { settlePrompt } = usePrompt()
    const { wrapper } = await mountInKeepAlive()
    const importBtn = wrapper.findAll('.btn-group button').find((b) => b.text().includes('导入本地'))!
    await importBtn.trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\Tools\\cc-switch  ')
    await flushPromises()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\Tools\\cc-switch')
    expect(useToast().toastMsg.value).toContain('已导入 CC Switch v3.5.0')
    wrapper.unmount()
  })
})

describe('CCSwitchView 事件与轮询契约', () => {
  it('instance-state 事件即时改写界面；卸载注销双订阅', async () => {
    stubDefaults({ state: 'stopped' }, [installedV342])
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['ccswitch:instance-state']({ data: { state: 'running', version: 'v3.4.2', pid: 1 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('激活轮询 2.5s 刷新，停用后不泄漏', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [installedV342])
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
