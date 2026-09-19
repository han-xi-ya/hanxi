// 特征测试（组 B · 批 0 共享契约迁入件）：QuickLookView 迁移（ManagedControlBar/
// ManagedExtrasCard + store + adapter）后的行为基线。
// 断言"做了什么"而非"怎么做"：启停/重载三钮矩阵、卸载/导入确认文案、方言表进度
// 呈现（安装中/哈希校验/解压安装）、双事件订阅、轮询与 KeepAlive 契约逐字保留；
// 状态灯类名由视图私有 .ql-status-light 落回共享标准形 .status-light（选择器随
// 结构更新），并新增重载动词回执用例锁定 reset 扩展槽接线。
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

// confirm/prompt 由 useConfirm/usePrompt 全局单例收编（adapter 内消费），
// 交互面经 settle* 驱动；文案与调用序列断言逐字保持。
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

  it('状态灯五态文案与三钮禁用矩阵（stopped：启动可点、重载/退出禁用）', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    expect(wrapper.find('.status-light').classes()).toContain('stopped')
    const startBtn = wrapper.findAll('.control-btns .btn')[0]
    const reloadBtn = wrapper.findAll('.control-btns .btn')[1]
    const quitBtn = wrapper.findAll('.control-btns .btn')[2]
    expect(startBtn.attributes('disabled')).toBeUndefined()
    expect(reloadBtn.attributes('disabled')).toBeDefined() // 仅运行中可重载
    expect(quitBtn.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('running：banner-ok(slim) + 重载可用 + 启动禁用 + 运行时长行', async () => {
    stubDefaults({ state: 'running', version: '0.4.0', pid: 777, startedAt: new Date().toISOString() }, [installedV])
    const { wrapper } = await mountView()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.banner-ok').exists()).toBe(true)
    expect(wrapper.find('.banner-ok').classes()).toContain('slim')
    expect(wrapper.find('.banner-ok').text()).toContain('QuickLook 正在运行')
    expect(wrapper.find('.pid-tag').text()).toContain('777')
    expect(wrapper.find('.uptime-tag').exists()).toBe(true)
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns[0].attributes('disabled')).toBeDefined() // 启动禁用（已在运行）
    expect(btns[1].attributes('disabled')).toBeUndefined() // 重载可用
    wrapper.unmount()
  })

  it('external→banner-warn；failed→banner-error 显示快照错误', async () => {
    stubDefaults({ state: 'external' })
    let ctx = await mountView()
    expect(ctx.wrapper.find('.banner-warn').text()).toContain('外部 QuickLook 实例')
    const quit = ctx.wrapper.findAll('.control-btns .btn')[2]
    expect(quit.attributes('disabled')).toBeUndefined() // external 可点（title 指引托盘退出）
    expect(quit.attributes('title')).toBe('外部实例请在 QuickLook 托盘菜单退出')
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

  it('重载配置（#primary-action 槽 + adapter.reset）：成功 toast 回执；失败前缀「重载失败: 」（不刷状态）', async () => {
    stubDefaults({ state: 'running', version: '0.4.0', pid: 1, startedAt: new Date().toISOString() }, [installedV])
    svc.Reload.mockResolvedValue('已请求重载配置')
    const { wrapper } = await mountView()
    const before = svc.GetStatus.mock.calls.length
    await wrapper.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(svc.Reload).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已请求重载配置')
    expect(svc.GetStatus.mock.calls.length).toBe(before) // 现状：重载动作不触发状态复刷

    svc.Reload.mockRejectedValue(new Error('管道断开'))
    await wrapper.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('重载失败: 管道断开')
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
