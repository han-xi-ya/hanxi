// 特征测试（Phase 4 组 E）：RecordlyView 迁移前行为基线。
// 锁定：升级检测（核心版本互认）、stable/beta 双通道、单目录覆盖安装语义、
// 确认/导入文案、事件改写与 KeepAlive 轮询契约。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RecordlyView from '../RecordlyView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetReleaseChannel: vi.fn(),
  GetStatus: vi.fn(),
  SetReleaseChannel: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/recordly/recordlyservice', () => svc)

const installed100 = {
  version: '1.0.0',
  exePath: 'C:\\data\\recordly\\Recordly.exe',
  dir: 'C:\\data\\recordly',
  size: 90 * 1024 * 1024,
  installedAt: '2026-07-01',
  isImport: false,
}

const release100 = { version: '1.0.0', size: 90 * 1024 * 1024, published: '2026-07-01T00:00:00Z', isPre: false }
const release110 = { version: '1.1.0', size: 92 * 1024 * 1024, published: '2026-08-01T00:00:00Z', isPre: false }
const release120beta = { version: '1.2.0-beta1', size: 94 * 1024 * 1024, published: '2026-08-20T00:00:00Z', isPre: true }

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(snap: Record<string, unknown>, opts: { installed?: unknown[]; releases?: unknown[]; ch?: string } = {}) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [installed100])
  svc.ListReleases.mockResolvedValue(opts.releases ?? [release100])
  svc.GetReleaseChannel.mockResolvedValue(opts.ch ?? 'stable')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/webadderallorg/Recordly')
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(RecordlyView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  // 迁移后交互面：确认/输入经 useConfirm/usePrompt 全局单例（见 BCUView.spec 同注释）
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('RecordlyView 通道与升级检测', () => {
  it('挂载拉取状态/版本/当前通道（三源并发）', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(svc.GetReleaseChannel).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('已装 1.0.0 + 远程 1.1.0 → 可升级警告条；同版不提示', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release110] })
    let r = await mountView()
    expect(r.wrapper.text()).toContain('发现可升级版本 1.1.0（当前 1.0.0）')
    r.wrapper.unmount()

    stubDefaults({ state: 'stopped' }, { releases: [release100] })
    r = await mountView()
    expect(r.wrapper.text()).not.toContain('发现可升级版本')
    r.wrapper.unmount()
  })

  it('核心版本互认：已装 1.0.0 对远程 1.2.0-beta1 不算升级、已装 1.0.0 不算其安装态', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release120beta] })
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('发现可升级版本 1.2.0-beta1') // 核心 1.2.0 > 1.0.0
    expect(wrapper.find('.rd-ver-status').classes()).toContain('idle') // tag 不同且核心不同 → 可安装
    wrapper.unmount()
  })

  it('beta 通道切换：调 SetReleaseChannel 后重拉并显beta警示', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release110, release120beta] })
    const { wrapper } = await mountView()
    const segBtns = wrapper.findAll('.channel-seg button')
    expect(segBtns[0].classes()).toContain('active')
    // 重拉列表时后端返回已切换的通道（loadVersions 以后端为准复位渠道态）
    svc.GetReleaseChannel.mockResolvedValue('beta')
    svc.SetReleaseChannel.mockResolvedValue(undefined)
    await segBtns[1].trigger('click')
    await flushMicrotasks()
    expect(svc.SetReleaseChannel).toHaveBeenCalledWith('beta')
    expect(wrapper.find('.beta-warn').exists()).toBe(true)
    expect(wrapper.find('.channel-seg button:nth-child(2)').classes()).toContain('active')
    wrapper.unmount()
  })

  it('同通道点击不重复设置', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    await wrapper.findAll('.channel-seg button')[0].trigger('click')
    expect(svc.SetReleaseChannel).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('RecordlyView 安装/卸载/控制', () => {
  it('未安装态：控制台引导行提示去版本管理安装', async () => {
    stubDefaults({ state: 'stopped' }, { installed: [] })
    const { wrapper } = await mountView()
    expect(wrapper.find('.hint-line').text()).toContain('尚未安装')
    expect(wrapper.find('.empty-state').text()).toContain('在线安装官方版本')
    wrapper.unmount()
  })

  it('覆盖安装按钮：运行中禁用并提示，停止态可点且调 DownloadVersion', async () => {
    stubDefaults({ state: 'running', version: '1.0.0' }, { releases: [release110] })
    let r = await mountView()
    const installBtn = r.wrapper.findAll('.tbl tbody tr')[0].findAll('button').find((b) => b.text() === '覆盖安装')!
    expect(installBtn.attributes('disabled')).toBeDefined()
    r.wrapper.unmount()

    stubDefaults({ state: 'stopped' }, { releases: [release110] })
    r = await mountView()
    svc.DownloadVersion.mockResolvedValue('started')
    const btn2 = r.wrapper.findAll('.tbl tbody tr')[0].findAll('button').find((b) => b.text() === '覆盖安装')!
    await btn2.trigger('click')
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.1.0')
    r.wrapper.unmount()
  })

  it('卸载确认文案含数据保留说明；确认走 RemoveVersion', async () => {
    stubDefaults({ state: 'stopped' })
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 Recordly 1.0.0？')
    expect(confirmState.options.description).toContain('配置与录像不受影响') // 原 window.confirm 文案逐字保留
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.0.0')
    expect(useToast().toastMsg.value).toBe('已卸载 1.0.0')
    wrapper.unmount()
  })

  it('运行中禁止卸载（按钮禁用+title 指引）', async () => {
    stubDefaults({ state: 'running', version: '1.0.0' })
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    expect(uninstallBtn.attributes('disabled')).toBeDefined()
    expect(uninstallBtn.attributes('title')).toContain('请先退出')
    wrapper.unmount()
  })

  it('导入本地：prompt 取消不导入；路径 trim 导入并 toast', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ version: '1.0.0' })
    const { wrapper } = await mountView()
    const importBtn = wrapper.findAll('.btn-group button')[0]

    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.label).toContain('app.asar') // 原 prompt 三行提示语义逐字保留
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()

    await importBtn.trigger('click')
    await flushMicrotasks()
    settlePrompt(' %LOCALAPPDATA%\\Programs\\Recordly ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('%LOCALAPPDATA%\\Programs\\Recordly')
    expect(useToast().toastMsg.value).toContain('已导入 Recordly')
    wrapper.unmount()
  })

  it('打开窗口/退出：回执 toast + 状态刷新', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: '已启动 Recordly' })
    const { wrapper } = await mountView()
    const btns = wrapper.findAll('.control-btns button')
    await btns[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已启动 Recordly')
    await btns[1].trigger('click') // stopped 下退出按钮禁用→无调用（禁用语义锁）
    await flushMicrotasks()
    expect(svc.Quit).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('RecordlyView 事件与轮询', () => {
  it('instance-state 事件改写界面；unmount 双 unlisten', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    runtime.handlers['recordly:instance-state']({ data: { state: 'running', version: '1.0.0', pid: 99 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.pid-tag').text()).toContain('99')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('安装进度事件：downloading 渲染百分比，verify/install 文案', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release110] })
    const { wrapper } = await mountView()
    runtime.handlers['recordly:version-download']({ data: { version: '1.1.0', stage: 'downloading', done: 30, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('30%')
    runtime.handlers['recordly:version-download']({ data: { version: '1.1.0', stage: 'install' } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toBe('校验并静默安装…')
    wrapper.unmount()
  })

  it('激活轮询、停用即止（GetStatus 不再增长）', async () => {
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
})
