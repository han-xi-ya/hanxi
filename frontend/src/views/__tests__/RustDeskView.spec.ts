// 特征测试（迁移基线）：RustDeskView 的托管家族契约锁定。
// 断言的是"渲染结果 + 后端调用序列"；卸载/导入的确认机制已按迁移铁律从
// window.confirm/prompt 收编为 useConfirm/usePrompt 单例（App.vue 宿主），
// 下方两例经单例裁决驱动，可观察行为（是否调后端/toast/文案要素）与迁移前一致。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RustDeskView from '../RustDeskView.vue'
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
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/rustdesk/rustdeskservice', () => svc)

const installedV123 = {
  version: '1.2.3',
  exePath: 'C:\\data\\rustdesk\\1.2.3\\rustdesk-1.2.3-x86_64.exe',
  dir: 'C:\\data\\rustdesk\\1.2.3',
  size: 8 * 1024 * 1024,
  installedAt: '2026-08-01',
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/rustdesk/rustdesk')
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(RustDeskView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return wrapper
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  useToast().clearToast()
  // 对话框单例兜底复位（正常用例内都已 settle）
  const { settleConfirm } = useConfirm()
  const { settlePrompt } = usePrompt()
  settleConfirm(false)
  settlePrompt(null)
})

describe('RustDeskView 初始装载', () => {
  it('挂载即拉取状态、版本、活动版本与联动开关/仓库地址', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(svc.GetActiveVersion).toHaveBeenCalled()
    expect(svc.GetFollowOnExit).toHaveBeenCalled()
    expect(svc.RepositoryURL).toHaveBeenCalled()
    w.unmount()
  })
})

describe('RustDeskView 状态矩阵', () => {
  const cases: Array<[string, Record<string, unknown>, string]> = [
    ['stopped', { state: 'stopped' }, '未运行'],
    ['running', { state: 'running', version: '1.2.3', startedAt: new Date().toISOString() }, '运行中'],
    ['starting', { state: 'starting' }, '启动中…'],
    ['failed', { state: 'failed', error: '解压超时' }, '异常退出'],
    ['external', { state: 'external' }, '外部运行'],
  ]

  for (const [name, snap, text] of cases) {
    it(name, async () => {
      stubDefaults(snap)
      const w = await mountView()
      expect(w.find('.status-word').text()).toBe(text)
      w.unmount()
    })
  }

  it('提示条三变体互斥：external=warn / failed=error 且含后端错误 / running=ok 含断会话警示', async () => {
    // 迁移后由 UiBanner 承载（.banner.banner-{tone}，role=note），语义变体与文案不变
    stubDefaults({ state: 'external' })
    let w = await mountView()
    expect(w.find('.banner').classes()).toContain('banner-warn')
    expect(w.find('.banner').text()).toContain('非 Hanxi 托管')
    w.unmount()

    stubDefaults({ state: 'failed', error: 'WebView2 缺失' })
    w = await mountView()
    expect(w.find('.banner').classes()).toContain('banner-error')
    expect(w.find('.banner').text()).toContain('WebView2 缺失')
    w.unmount()

    stubDefaults({ state: 'running', version: '1.2.3' })
    w = await mountView()
    expect(w.find('.banner').classes()).toContain('banner-ok')
    expect(w.find('.banner').text()).toContain('断开进行中的会话')
    w.unmount()
  })

  it('stopped 无提示条但有引导行；running 展示版本与 uptime、退出按钮可用', async () => {
    stubDefaults({ state: 'stopped' })
    let w = await mountView()
    expect(w.find('.banner').exists()).toBe(false)
    expect(w.find('.hint-line').text()).toContain('尚未运行')
    const quitBtn = w.findAll('.control-btns .btn')[1]
    expect(quitBtn.attributes('disabled')).toBeDefined()
    w.unmount()

    stubDefaults({ state: 'running', version: '1.2.3', pid: 4242 })
    w = await mountView()
    expect(w.find('.ver-pill').text()).toBe('1.2.3')
    expect(w.find('.pid-tag').text()).toContain('4242')
    expect(w.findAll('.control-btns .btn')[1].attributes('disabled')).toBeUndefined()
    w.unmount()
  })
})

describe('RustDeskView 控制操作', () => {
  it('打开窗口：成功 toast 回执并立即刷新状态', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: 'RustDesk 已启动并唤起窗口' })
    const w = await mountView()
    const calls = svc.GetStatus.mock.calls.length
    await w.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('RustDesk 已启动并唤起窗口')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(calls)
    w.unmount()
  })

  it('打开窗口失败：toast 原始错误并仍刷新状态', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockRejectedValue(new Error('进程树未出现'))
    const w = await mountView()
    const calls = svc.GetStatus.mock.calls.length
    await w.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('进程树未出现')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(calls)
    w.unmount()
  })

  it('退出失败：toast 带「退出失败: 」前缀', async () => {
    stubDefaults({ state: 'running', version: '1.2.3' })
    svc.Quit.mockRejectedValue(new Error('终止超时'))
    const w = await mountView()
    await w.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('退出失败: 终止超时')
    w.unmount()
  })
})

describe('RustDeskView 版本管理', () => {
  it('卸载必须经确认：取消不动后端', async () => {
    const { confirmState, settleConfirm } = useConfirm()
    stubDefaults({ state: 'stopped' }, [installedV123])
    const w = await mountView()
    const uninstallBtn = w.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    // 迁移后走 useConfirm 全局单例（宿主在 App.vue），文案要素保持锁定
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('1.2.3')
    expect(confirmState.options.description).toContain('不可恢复')
    expect(JSON.stringify(confirmState.options.details)).toContain('%APPDATA%')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    w.unmount()
  })

  it('确认卸载：RemoveVersion 并 toast 结果、刷新列表', async () => {
    const { settleConfirm } = useConfirm()
    stubDefaults({ state: 'stopped' }, [installedV123])
    svc.RemoveVersion.mockResolvedValue(undefined)
    const w = await mountView()
    const before = svc.ListInstalledVersions.mock.calls.length
    const uninstallBtn = w.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.2.3')
    expect(useToast().toastMsg.value).toBe('已卸载 1.2.3')
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
  })

  it('运行中版本的卸载按钮禁用（先退出防线）', async () => {
    stubDefaults({ state: 'running', version: '1.2.3' }, [installedV123])
    svc.GetActiveVersion.mockResolvedValue('1.2.3')
    const w = await mountView()
    const uninstallBtn = w.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    expect(uninstallBtn.attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('导入本地：取消/空输入不动后端；有值去空格调 ImportLocal 并 toast', async () => {
    const { promptState, settlePrompt } = usePrompt()
    stubDefaults({ state: 'stopped' })
    let w = await mountView()
    await w.find('.btn-group button').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    settlePrompt(null) // 取消
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    w.unmount()

    svc.ImportLocal.mockResolvedValue({ version: '1.2.3' })
    w = await mountView()
    await w.find('.btn-group button').trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\Downloads\\rustdesk-1.2.3-x86_64.exe  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\Downloads\\rustdesk-1.2.3-x86_64.exe')
    expect(useToast().toastMsg.value).toBe('已导入 RustDesk 1.2.3')
    w.unmount()
  })
})

describe('RustDeskView 事件与轮询', () => {
  it('instance-state 事件即时改写界面', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    runtime.handlers['rustdesk:instance-state']({ data: { state: 'running', version: '1.9.9', pid: 9 } })
    await nextTick()
    expect(w.find('.status-word').text()).toBe('运行中')
    expect(w.find('.ver-pill').text()).toBe('1.9.9')
    w.unmount()
  })

  it('download 事件驱动进度条百分比', async () => {
    stubDefaults({ state: 'stopped' }, [], [{ version: '1.3.0', size: 100, published: '2026-08-02T00:00:00Z', isPre: false }])
    const w = await mountView()
    runtime.handlers['rustdesk:version-download']({ data: { version: '1.3.0', stage: 'downloading', done: 60, total: 100 } })
    await nextTick()
    expect(w.find('.dl-percent').text()).toBe('60%')
    w.unmount()
  })

  it('激活后 2.5s 轮询刷新；停用后轮询静默且 uptime 归零', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'running', version: '1.2.3', startedAt: new Date().toISOString() })
    const show = ref(true)
    const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(RustDeskView)) : h('div')) })
    const w = mount(Host, { attachTo: document.body })
    await flushMicrotasks()
    const base = svc.GetStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 3)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
    show.value = false
    await nextTick()
    const afterOff = svc.GetStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 5)
    expect(svc.GetStatus.mock.calls.length).toBe(afterOff)
    w.unmount()
  })
})

describe('RustDeskView 联动开关与仓库', () => {
  it('跟随退出开关：变更写入后端并回写本地态、toast 跟随新值（§9.5-1 修复）', async () => {
    // 修复后语义：成功路径回写 followOnExit，取消勾选后提示「已关闭」。
    stubDefaults({ state: 'stopped' })
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const w = await mountView()
    const checkbox = w.find('.toggle-label input')
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    await checkbox.setValue(false)
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false) // 目标值 = 新勾选态
    expect(useToast().toastMsg.value).toContain('已关闭') // 文案跟随新值
    await nextTick()
    expect((checkbox.element as HTMLInputElement).checked).toBe(false) // 视图与后端一致
    w.unmount()
  })

  it('复制仓库地址走 execCommand 回退时成功提示', async () => {
    stubDefaults({ state: 'stopped' })
    Object.defineProperty(document, 'execCommand', { value: vi.fn(() => true), configurable: true })
    const w = await mountView()
    const copyBtn = w.findAll('.repo-row .link-button').find((b) => b.text() === '复制')!
    await copyBtn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('仓库地址已复制')
    w.unmount()
  })

  it('浏览器打开按钮调用 OpenRepository', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenRepository.mockResolvedValue(undefined)
    const w = await mountView()
    const btn = w.findAll('.repo-row .link-button').find((b) => b.text() === '浏览器打开')!
    await btn.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenRepository).toHaveBeenCalled()
    w.unmount()
  })
})
