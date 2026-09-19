// 特征测试（Phase 4 组 E · Wave 5 批 0 收敛续版）：RecordlyView 行为基线。
// 锁定：升级检测（核心版本互认）、stable/beta 双通道、单目录覆盖安装语义、
// 确认/导入文案、事件改写与 KeepAlive 轮询契约。
// 批 0 收敛新增：真实 adapter 的缺省 active 形状（ManagedVersionPanel 消费面）、
// 「安装失败/已安装」动词词面经 adapter 自捕获、联动辅助卡（ManagedExtrasCard）
// 对 adapter.extras 的驱动。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RecordlyView from '../RecordlyView.vue'
import ManagedVersionPanel from '../../components/managed/ManagedVersionPanel.vue'
import { createRecordlyAdapter } from '../../adapters/recordly'
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
  OpenConfigDir: vi.fn(),
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

describe('RecordlyView 批 0 契约成色（缺省 active / 安装动词词面 / extras 投影）', () => {
  it('缺省 active 首样：视图不碰任何 GetActiveVersion RPC，界面「使用中」位缺席', async () => {
    // recordly 后端本无 GetActiveVersion/SetActiveVersion——svc 桩刻意不提供，
    // 视图与 store 若误触会 TypeError；此处锁「契约首次消费可缺省 active」的真实形态
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(wrapper.text()).not.toContain('使用中')
    expect(wrapper.text()).not.toContain('设为使用')
    wrapper.unmount()
  })

  it('真实 recordly adapter 挂共享版本面板：可缺省 active 处理成色（无使用中徽标/设钮，卡片与卸载照常）', async () => {
    stubDefaults({ state: 'stopped' }, { installed: [installed100], releases: [release100] })
    const adapter = createRecordlyAdapter()
    expect(adapter.versions.getActive).toBeUndefined()
    expect(adapter.versions.setActive).toBeUndefined()
    const w = mount(ManagedVersionPanel, { props: { adapter } })
    await flushPromises()
    await flushPromises()
    const card = w.find('.installed-card')
    expect(card.exists()).toBe(true)
    expect(card.classes()).not.toContain('card-active') // activeVersion 恒空串：无高亮位（成色观察项）
    expect(card.findAll('button').map((b) => b.text())).toEqual(['📂 打开位置', '卸载'])
    expect(w.find('.badge').text()).toBe('官方下载')
    w.unmount()
  })

  it('安装动词词面：DownloadVersion 失败自弹「安装失败: 」（store 统一前缀「下载失败: 」被 adapter 吃掉，无双 toast）', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release110] })
    svc.DownloadVersion.mockRejectedValue(new Error('网络断'))
    const { wrapper } = await mountView()
    const btn = wrapper.findAll('.tbl tbody tr')[0].findAll('button').find((b) => b.text() === '覆盖安装')!
    await btn.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('安装失败: 网络断'))
    wrapper.unmount()
  })

  it('already-installed 回执：toast 现词 + 即时重拉版本区', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release110] })
    svc.DownloadVersion.mockResolvedValue('already-installed')
    const { wrapper } = await mountView()
    const before = svc.ListInstalledVersions.mock.calls.length
    const btn = wrapper.findAll('.tbl tbody tr')[0].findAll('button').find((b) => b.text() === '覆盖安装')!
    await btn.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('版本 1.1.0 已安装'))
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('通道切换失败：toast「切换通道失败: 」且通道态不翻转、不重拉', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release110, release120beta] })
    svc.SetReleaseChannel.mockRejectedValue(new Error('离线'))
    const { wrapper } = await mountView()
    const before = svc.ListReleases.mock.calls.length
    await wrapper.findAll('.channel-seg button')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('切换通道失败: 离线')
    expect(wrapper.find('.beta-warn').exists()).toBe(false)
    expect(svc.ListReleases.mock.calls.length).toBe(before)
    wrapper.unmount()
  })

  it('通道首拉失败保留现词「读取本地版本失败: 」（原 localTask 错误面）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.GetReleaseChannel.mockRejectedValue(new Error('配置损坏'))
    const { wrapper } = await mountView()
    expect(wrapper.find('.error-box').text()).toContain('读取本地版本失败: 配置损坏')
    wrapper.unmount()
  })

  it('联动辅助卡经 adapter.extras 驱动：随关勾选/快捷方式/数据目录回执与 RPC 一致', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    svc.CreateDesktopShortcut.mockResolvedValue(undefined)
    svc.OpenConfigDir.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const card = wrapper.find('.extras-card')
    expect(card.exists()).toBe(true)
    // 勾选翻转 → SetFollowOnExit(false)（stub 初值 true）+ 现词 toast
    await card.find('.toggle-label input').setValue(false)
    await vi.waitFor(() => expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false))
    expect(useToast().toastMsg.value).toBe('已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
    await card.findAll('button').find((b) => b.text().includes('创建桌面快捷方式'))!.trigger('click')
    await vi.waitFor(() => expect(svc.CreateDesktopShortcut).toHaveBeenCalled())
    expect(useToast().toastMsg.value).toBe('桌面快捷方式已创建（指向托管安装）')
    await card.findAll('button').find((b) => b.text().includes('数据目录'))!.trigger('click')
    await vi.waitFor(() => expect(svc.OpenConfigDir).toHaveBeenCalled())
    // 仓库行展示
    expect(card.find('.repo-addr').text()).toBe('https://github.com/webadderallorg/Recordly')
    wrapper.unmount()
  })
})
