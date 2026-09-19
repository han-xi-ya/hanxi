// 回归测试（二波收敛完成版）：VSCodeView 双形态托管控制台正式行为基线。
// 全部断言按「现行为」逐字锁定（不代表收敛后目标形态）：双形态状态快照与 banner 分叉、
// 进度键 form:version 隔离、OpenWindow/Quit 按 form 分派与错误文案不对称（打开失败无前缀 /
// 退出失败带「退出失败: 」）、安装版 confirm-required 二段闸、DownloadVersion 动作回执分支、
// 安装版探测单对象语义（视图不消费 GetInstalledApp——桩刻意省略，误触即 TypeError 现形，
// 对照 Recordly 批 0 同法）、版本表双列方言（Commit 列 / 可下载·可安装词面 / 官方哈希徽标）、
// 便携版卡片（使用中/本地导入/三层校验/数据目录）、extras 联动、instance-state 按 form 改写、
// 双事件注册与 2.5s KeepAlive 轮询、1s 运行时长 ticker。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import VSCodeView from '../VSCodeView.vue'
import ManagedConsoleShell from '../../components/managed/ManagedConsoleShell.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  ListRemoteVersions: vi.fn(),
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

// 桩刻意不含 GetInstalledApp / OpenPreferredWindow / Shutdown：安装版探测现状只经
// GetStatus().installed 单对象进入界面；收敛后若前端误触缺席方法会以异常/toast 现形。

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

vi.mock('../../../bindings/hanxi/internal/modules/vscode/vscodeservice', () => svc)

type Form = 'portable' | 'installer'

const snap = (state: string, extra: Record<string, unknown> = {}) => ({
  form: '', version: '', pid: 0, exitCode: 0, error: '', external: false, startedAt: '', stoppedAt: '', state,
  ...extra,
})
const NOT_INSTALLED = { installed: false, version: '', dir: '', exePath: '' }
const statusOfForms = (
  p: Record<string, unknown>,
  i: Record<string, unknown>,
  installed: Record<string, unknown> = NOT_INSTALLED,
) => ({ portable: p, installer: i, installed, followOnExit: false })

const v1361 = {
  version: '1.136.1',
  exePath: 'D:\\vscode\\versions\\vscode_1.136.1\\Code.exe',
  dir: 'D:\\vscode\\versions\\vscode_1.136.1',
  size: 2097152, installedAt: '2026-08-01 10:00:00', isImport: false, source: '', verified: true,
}
const v1350 = {
  version: '1.135.0',
  exePath: 'D:\\vscode\\versions\\vscode_1.135.0\\Code.exe',
  dir: 'D:\\vscode\\versions\\vscode_1.135.0',
  size: 2097152, installedAt: '2026-07-01 10:00:00', isImport: true, source: 'E:\\old\\VSCode', verified: false,
}
const relP = (over: Record<string, unknown> = {}) => ({
  version: '1.136.1', commit: 'c'.repeat(40), downloadUrl: '', assetName: 'VSCode-win32-x64-1.136.1.zip',
  size: 2097152, sha256: 'a'.repeat(64), ...over,
})
const relI = (over: Record<string, unknown> = {}) => ({
  version: '1.136.1', commit: 'd'.repeat(40), downloadUrl: '', assetName: 'VSCodeUserSetup-x64-1.136.1.exe',
  size: 1048576, sha256: '', ...over,
})

function stubDefaults(
  status: Record<string, unknown>,
  opts: { installed?: unknown[]; active?: string; releasesP?: unknown[]; releasesI?: unknown[] } = {},
) {
  svc.GetStatus.mockResolvedValue(status)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [])
  svc.GetActiveVersion.mockResolvedValue(opts.active ?? '')
  svc.ListRemoteVersions.mockImplementation((form: string) =>
    Promise.resolve(form === 'installer' ? opts.releasesI ?? [] : opts.releasesP ?? []))
  svc.GetFollowOnExit.mockResolvedValue(false)
  svc.RepositoryURL.mockResolvedValue('https://code.visualstudio.com')
  svc.OpenWindow.mockResolvedValue({ action: 'opened', external: false, message: '已唤起窗口' })
  svc.Quit.mockResolvedValue({ stopped: true, external: false, message: '已退出' })
  svc.DownloadVersion.mockResolvedValue({ action: 'started', external: false, message: '' })
  svc.SetActiveVersion.mockResolvedValue(opts.active ?? '')
  svc.RemoveVersion.mockResolvedValue(undefined)
  svc.OpenDir.mockResolvedValue(undefined)
  svc.ImportLocal.mockResolvedValue({ ...v1361, version: '1.137.0' })
  svc.SetFollowOnExit.mockResolvedValue(undefined)
  svc.CreateDesktopShortcut.mockResolvedValue(undefined)
  svc.OpenRepository.mockResolvedValue(undefined)
}

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(VSCodeView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

/** 控制台两形态条：index 0=便携版，1=安装版（v-for 固定序）。 */
function bar(w: Awaited<ReturnType<typeof mountView>>['wrapper'], form: Form) {
  return w.findAll('.control-bar')[form === 'portable' ? 0 : 1]
}

const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.unstubAllGlobals()
  useToast().clearToast()
  settleConfirm(false)
  settlePrompt(null)
})

describe('VSCodeView 共享壳接线', () => {
  it('采用 custom 版本编排，并通过 control-bar / versions-body 两个专属槽收口双形态 UI', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')))
    const { wrapper } = await mountView()
    const shell = wrapper.findComponent(ManagedConsoleShell)
    expect(shell.exists()).toBe(true)
    expect(shell.props('adapter').versions.orchestration).toBe('custom')
    expect(shell.vm.$slots['control-bar']).toBeTypeOf('function')
    expect(shell.vm.$slots['versions-body']).toBeTypeOf('function')
    wrapper.unmount()
  })
})

describe('VSCodeView 装载与双形态快照', () => {
  it('挂载并发拉取：两形态远程表各一次 + 本地三源 + extras 双源；GetStatus 首帧一次', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')))
    const { wrapper } = await mountView()
    expect(svc.GetStatus).toHaveBeenCalledTimes(1)
    expect(svc.ListRemoteVersions.mock.calls.map((c) => c[0]).sort()).toEqual(['installer', 'portable'])
    expect(svc.ListRemoteVersions).toHaveBeenCalledTimes(2)
    for (const fn of [svc.ListInstalledVersions, svc.GetActiveVersion, svc.GetFollowOnExit, svc.RepositoryURL]) {
      expect(fn).toHaveBeenCalledTimes(1)
    }
    wrapper.unmount()
  })

  it('双形态独立渲染：便携 running（灯/版本/PID/⏱/ok banner）+ 安装 stopped（未运行/退出禁用/探测提示行）', async () => {
    stubDefaults(statusOfForms(
      snap('running', { form: 'portable', version: '1.136.1', pid: 4242 }),
      snap('stopped', { form: 'installer' }),
      { installed: true, version: '1.135.0', dir: 'C:\\vscode-inst', exePath: 'C:\\vscode-inst\\Code.exe' },
    ))
    const { wrapper } = await mountView()
    const bp = bar(wrapper, 'portable')
    expect(bp.text()).toContain('便携版')
    expect(bp.find('.status-word').text()).toBe('运行中')
    expect(bp.find('.vc-status-light').classes()).toContain('running')
    expect(bp.find('.ver-pill').text()).toBe('1.136.1')
    expect(bp.find('.pid-tag').text()).toContain('4242')
    expect(bp.find('.uptime-tag').text()).toContain('⏱')
    expect(bp.find('.banner').classes()).toContain('banner-ok')
    expect(bp.find('.banner').text()).toContain('完全隔离')
    const bi = bar(wrapper, 'installer')
    expect(bi.find('.status-word').text()).toBe('未运行')
    expect(bi.find('.banner').exists()).toBe(false)
    expect(bi.findAll('.control-btns .btn')[1].attributes('disabled')).toBeDefined() // stopped 退出禁用
    expect(bi.find('.hint-line').text()).toContain('本机安装版 VS Code 1.135.0')
    wrapper.unmount()
  })

  it('external/failed 分叉：两形态 external 文案不同、external 可退出（不越权 title）、failed 透出快照 error', async () => {
    stubDefaults(statusOfForms(
      snap('external', { form: 'portable', external: true }),
      snap('failed', { form: 'installer', error: '安装版闪退了' }),
    ))
    const { wrapper } = await mountView()
    const bp = bar(wrapper, 'portable')
    expect(bp.find('.banner').classes()).toContain('banner-warn')
    expect(bp.find('.banner').text()).toContain('外部启动的便携版实例')
    expect(bp.findAll('.control-btns .btn')[1].attributes('disabled')).toBeUndefined() // external 可退出
    expect(bp.findAll('.control-btns .btn')[1].attributes('title')).toBe('外部实例不越权终止')
    const bi = bar(wrapper, 'installer')
    expect(bi.find('.banner').classes()).toContain('banner-error')
    expect(bi.find('.banner').text()).toBe('安装版闪退了')
    wrapper.unmount()
  })

  it('installer external/running 分叉 + starting 打开禁用；GetStatus 静默失败仅显未运行', async () => {
    stubDefaults(statusOfForms(
      snap('stopped', { form: 'portable' }),
      snap('external', { form: 'installer', external: true }),
    ))
    let m = await mountView()
    expect(bar(m.wrapper, 'installer').find('.banner').text()).toContain('互斥体探测')
    m.wrapper.unmount()

    stubDefaults(statusOfForms(
      snap('running', { form: 'installer', version: '1.135.0' }),
      snap('running', { form: 'portable', version: '1.136.1' }),
    ))
    m = await mountView()
    expect(bar(m.wrapper, 'installer').find('.banner').text()).toContain('安装版正在运行')
    m.wrapper.unmount()

    stubDefaults(statusOfForms(
      snap('starting', { form: 'portable' }),
      snap('stopped', { form: 'installer' }),
    ))
    m = await mountView()
    expect(bar(m.wrapper, 'portable').findAll('.control-btns .btn')[0].attributes('disabled')).toBeDefined()
    m.wrapper.unmount()

    svc.GetStatus.mockRejectedValue(new Error('RPC 掉线'))
    m = await mountView()
    expect(bar(m.wrapper, 'portable').find('.status-word').text()).toBe('未运行')
    expect(bar(m.wrapper, 'portable').find('.banner').exists()).toBe(false)
    m.wrapper.unmount()
  })
})

describe('VSCodeView 控制动词按 form 分派', () => {
  it('openWindow/quit 带 form 参数分派；回执 message 直出 toast；错误文案不对称', async () => {
    stubDefaults(statusOfForms(
      snap('running', { form: 'portable', version: '1.136.1' }),
      snap('running', { form: 'installer', version: '1.135.0' }),
    ))
    svc.OpenWindow.mockImplementation((form: string) => Promise.resolve({ action: 'opened', external: false, message: `已唤起 ${form}` }))
    svc.Quit.mockImplementation((form: string) =>
      form === 'installer' ? Promise.reject(new Error('没门')) : Promise.resolve({ stopped: true, external: false, message: '便携版已退出' }))
    const { wrapper } = await mountView()
    const before = svc.GetStatus.mock.calls.length
    await bar(wrapper, 'portable').findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalledWith('portable')
    expect(useToast().toastMsg.value).toBe('已唤起 portable')
    await bar(wrapper, 'installer').findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(svc.Quit).toHaveBeenCalledWith('installer')
    expect(useToast().toastMsg.value).toBe('退出失败: 没门') // quit 失败带前缀
    await bar(wrapper, 'portable').findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('便携版已退出')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before) // 每动词后刷新
    wrapper.unmount()

    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')))
    svc.OpenWindow.mockRejectedValue(new Error('exe 缺失'))
    const m2 = await mountView()
    await bar(m2.wrapper, 'portable').findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('exe 缺失') // openWindow 失败无前缀（不对称现状）
    m2.wrapper.unmount()
  })
})

describe('VSCodeView 下载与动作回执', () => {
  it('安装版 confirm-required 二段闸：取消不重入；放行带 confirm=true 重入', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), { releasesI: [relI()] })
    svc.DownloadVersion.mockResolvedValue({ action: 'confirm-required', external: false, message: '运行中的实例会被 Inno 强关' })
    const { wrapper } = await mountView()
    await wrapper.findAll('.vc-form-switch button')[1].trigger('click') // 切「安装器 EXE」
    await nextTick()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.text()).toContain('可安装')
    const btn = row.findAll('button').find((b) => b.text() === '安装/升级')!
    await btn.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenLastCalledWith('1.136.1', 'installer', false)
    expect(svc.DownloadVersion).toHaveBeenCalledTimes(1)
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确认升级安装 VS Code 1.136.1？')
    expect(confirmState.options.description).toBe('运行中的实例会被 Inno 强关')
    settleConfirm(false) // 取消：绝不重入（防「不带参数永远被拦」死锁的反向锁）
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledTimes(1)
    await btn.trigger('click')
    await flushMicrotasks()
    svc.DownloadVersion.mockResolvedValue({ action: 'installing', external: false, message: '安装版静默安装进行中' })
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenLastCalledWith('1.136.1', 'installer', true)
    expect(useToast().toastMsg.value).toBe('安装版静默安装进行中')
    wrapper.unmount()
  })

  it('DownloadVersion 回执分支：already-installed 即时重拉；普通 message 直出不重拉；异常带「操作失败: 」前缀', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), { releasesP: [relP()] })
    svc.DownloadVersion.mockResolvedValue({ action: 'already-installed', external: false, message: '该版本已安装' })
    let m = await mountView()
    let before = svc.ListInstalledVersions.mock.calls.length
    await m.wrapper.find('.tbl tbody tr .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('该版本已安装')
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(before)
    m.wrapper.unmount()

    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), { releasesP: [relP()] })
    svc.DownloadVersion.mockResolvedValue({ action: 'started', external: false, message: '已开始下载便携版' })
    m = await mountView()
    before = svc.ListInstalledVersions.mock.calls.length
    await m.wrapper.find('.tbl tbody tr .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已开始下载便携版')
    expect(svc.ListInstalledVersions.mock.calls.length).toBe(before) // 非 already-installed 不重拉

    // 异常路径同表复点（无票据时按钮恒在 idle 位）
    svc.DownloadVersion.mockRejectedValue(new Error('网络断'))
    await m.wrapper.find('.tbl tbody tr .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('操作失败: 网络断')
    m.wrapper.unmount()
  })
})

describe('VSCodeView 进度事件与键隔离', () => {
  it('进度键 form:version 隔离：同版本号两形态各表各进度；非法事件（缺 form/version）忽略', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), {
      releasesP: [relP()], releasesI: [relI()],
    })
    const { wrapper } = await mountView()
    runtime.handlers['vscode:version-download']({ data: { form: 'portable', version: '1.136.1', stage: 'downloading', done: 30, total: 100, message: '' } })
    runtime.handlers['vscode:version-download']({ data: { version: '1.136.1', stage: 'downloading', done: 50, total: 100 } }) // 缺 form → 忽略
    runtime.handlers['vscode:version-download']({ data: { form: 'installer', stage: 'downloading', done: 50, total: 100 } }) // 缺 version → 忽略
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('30%')
    await wrapper.findAll('.vc-form-switch button')[1].trigger('click')
    await nextTick()
    expect(wrapper.find('.dl-percent').exists()).toBe(false) // 安装表不受便携进度污染
    expect(wrapper.find('.tbl tbody tr').text()).toContain('可安装')
    runtime.handlers['vscode:version-download']({ data: { form: 'installer', version: '1.136.1', stage: 'downloading', done: 70, total: 100, message: '' } })
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('70%')
    expect(wrapper.find('.tbl tbody tr').text()).toContain('安装中') // installer 在途词面为「安装中」
    await wrapper.findAll('.vc-form-switch button')[0].trigger('click')
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('30%')
    expect(wrapper.find('.tbl tbody tr').text()).toContain('下载中')
    wrapper.unmount()
  })

  it('done 事件：800ms 清键 + 版本列表与状态双刷新', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), { releasesP: [relP()] })
      const { wrapper } = await mountView()
      const dlBefore = svc.ListInstalledVersions.mock.calls.length
      const stBefore = svc.GetStatus.mock.calls.length
      runtime.handlers['vscode:version-download']({ data: { form: 'portable', version: '1.136.1', stage: 'done', done: 100, total: 100, message: '' } })
      await nextTick()
      expect(wrapper.find('.dl-percent').exists()).toBe(false) // done 非 downloading 阶段走 dl-meta-text
      await vi.advanceTimersByTimeAsync(900)
      expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(dlBefore) // loadVersions
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(stBefore) // refreshStatus
      expect(wrapper.findAll('.tbl tbody tr')[0].find('.btn-primary').exists()).toBe(true) // 键清后回 idle 出下载钮
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('verify/extract 与 install 阶段词面分流（安装版「静默安装中…」）', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), { releasesP: [relP()], releasesI: [relI()] })
    const { wrapper } = await mountView()
    runtime.handlers['vscode:version-download']({ data: { form: 'portable', version: '1.136.1', stage: 'verify', done: 0, total: 0, message: '' } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toBe('校验解压中…')
    await wrapper.findAll('.vc-form-switch button')[1].trigger('click')
    runtime.handlers['vscode:version-download']({ data: { form: 'installer', version: '1.136.1', stage: 'install', done: 0, total: 0, message: '' } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toBe('静默安装中…')
    wrapper.unmount()
  })
})

describe('VSCodeView instance-state 按 form 改写', () => {
  it('事件仅改写归属形态快照；非 running 归零该形态 uptime', async () => {
    stubDefaults(statusOfForms(snap('stopped', { form: 'portable' }), snap('stopped', { form: 'installer' })))
    const { wrapper } = await mountView()
    runtime.handlers['vscode:instance-state']({ data: { form: 'installer', state: 'running', version: '1.135.0', pid: 77, error: '', external: false, startedAt: new Date(Date.now() - 30000).toISOString() } })
    await nextTick()
    expect(bar(wrapper, 'installer').find('.status-word').text()).toBe('运行中')
    expect(bar(wrapper, 'installer').find('.ver-pill').text()).toBe('1.135.0')
    expect(bar(wrapper, 'portable').find('.status-word').text()).toBe('未运行')
    runtime.handlers['vscode:instance-state']({ data: { form: 'installer', state: 'stopped', version: '', pid: 0, error: '', external: false } })
    await nextTick()
    expect(bar(wrapper, 'installer').find('.status-word').text()).toBe('未运行')
    expect(bar(wrapper, 'installer').find('.uptime-tag').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('VSCodeView 安装版探测单对象语义', () => {
  it('GetInstalledApp 不被前端消费：本机安装版卡与提示行全部出自 GetStatus().installed', async () => {
    expect((svc as Record<string, unknown>).GetInstalledApp).toBeUndefined() // 缺席桩＝误触即现形
    stubDefaults(statusOfForms(
      snap('stopped', { form: 'portable' }),
      snap('stopped', { form: 'installer' }),
      { installed: true, version: '1.135.0', dir: 'C:\\vscode-inst', exePath: 'C:\\vscode-inst\\Code.exe' },
    ))
    const { wrapper } = await mountView()
    const instCard = wrapper.findAll('.installed-card').find((c) => c.text().includes('User Installer'))!
    expect(instCard.text()).toContain('1.135.0')
    expect(instCard.text()).toContain('C:\\vscode-inst')
    expect(instCard.text()).toContain('共实例组')
    await instCard.findAll('button').find((b) => b.text().includes('打开位置'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenDir).toHaveBeenCalledWith('C:\\vscode-inst')
    wrapper.unmount()

    stubDefaults(statusOfForms(
      snap('stopped', { form: 'portable' }),
      snap('stopped', { form: 'installer' }),
      NOT_INSTALLED,
    ))
    const m2 = await mountView()
    expect(m2.wrapper.find('.empty-state').text()).toContain('本机未检测到安装版')
    expect(bar(m2.wrapper, 'installer').find('.hint-line').text()).toContain('本机暂无安装版')
    m2.wrapper.unmount()
  })
})

describe('VSCodeView 版本表双列方言', () => {
  it('列头含 Commit 无发布时间；Commit 截 10 位；官方哈希徽标仅便携最新带 sha256 行', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')), {
      releasesP: [relP(), relP({ version: '1.136.0', sha256: '' })],
      releasesI: [relI()],
    })
    const { wrapper } = await mountView()
    expect(wrapper.findAll('.tbl thead th').map((th) => th.text())).toEqual(['版本', '状态', '大小', 'Commit', '操作'])
    const rows = wrapper.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.badge-hash').exists()).toBe(true)
    expect(rows[0].find('.badge-hash').text()).toBe('官方哈希')
    expect(rows[1].find('.badge-hash').exists()).toBe(false)
    expect(rows[0].findAll('td')[3].text()).toBe('c'.repeat(10)) // (rel.commit||'').slice(0,10)
    expect(rows[0].findAll('td')[2].text()).toBe('2.0 MB') // fmtSize 标准形
    expect(rows[0].find('td button').text()).toBe('下载安装')
    expect(rows[0].text()).toContain('可下载')
    await wrapper.findAll('.vc-form-switch button')[1].trigger('click')
    await nextTick()
    const irow = wrapper.findAll('.tbl tbody tr')[0]
    expect(irow.text()).toContain('可安装')
    expect(irow.find('td button').text()).toBe('安装/升级')
    expect(irow.find('.badge-hash').exists()).toBe(false) // 安装版同版本无 sha256：徽标按便携首行判定
    wrapper.unmount()
  })
})

describe('VSCodeView 便携版卡片与目录动作', () => {
  it('使用中高亮/本地导入/三层校验徽标、设为使用、运行版卸载禁用、卸载确认闸、数据目录后缀', async () => {
    stubDefaults(statusOfForms(snap('stopped', { form: 'portable' }), snap('stopped', { form: 'installer' })), {
      installed: [v1361, v1350], active: '1.136.1',
    })
    const { wrapper } = await mountView()
    const cards = wrapper.findAll('.installed-card')
    const cardA = cards.find((c) => c.text().includes('1.136.1'))!
    const cardB = cards.find((c) => c.text().includes('1.135.0'))!
    expect(cardA.classes()).toContain('card-active')
    expect(cardA.text()).toContain('使用中')
    expect(cardA.text()).toContain('官方哈希') // verified=true 徽标
    expect(cardA.findAll('button').map((b) => b.text())).not.toContain('设为使用')
    expect(cardB.text()).toContain('本地导入') // isImport 优先：不渲染 官方哈希/三层校验 位
    expect(cardB.text()).not.toContain('三层校验')
    expect(cardB.text()).toContain('E:\\old\\VSCode')
    svc.SetActiveVersion.mockResolvedValue('1.135.0')
    await cardB.findAll('button').find((b) => b.text() === '设为使用')!.trigger('click')
    await flushMicrotasks()
    expect(svc.SetActiveVersion).toHaveBeenCalledWith('1.135.0')
    expect(useToast().toastMsg.value).toBe('已将便携版 1.135.0 设为使用版本')
    // 卸载闸：1.136.1 卡（非使用中）可卸
    const uninstall = cardA.findAll('button').find((b) => b.text() === '卸载')!
    await uninstall.trigger('click')
    await flushMicrotasks()
    expect(confirmState.options.title).toBe('确定卸载便携版 VS Code 1.136.1？')
    expect(confirmState.options.description).toContain('data\\ 内的配置与已装扩展')
    expect(confirmState.options.description).toContain('%APPDATA%\\Code 的日常数据不受影响')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    await uninstall.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.136.1')
    expect(useToast().toastMsg.value).toBe('已卸载 1.136.1')
    // 数据目录按钮：dir + '\\data' 后缀
    await cardB.findAll('button').find((b) => b.text().includes('数据'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenDir).toHaveBeenCalledWith('D:\\vscode\\versions\\vscode_1.135.0\\data')
    wrapper.unmount()

    // 运行版本卡禁用卸载 + 位置按钮按运行版本优先
    stubDefaults(statusOfForms(
      snap('running', { form: 'portable', version: '1.136.1' }),
      snap('stopped', { form: 'installer' }),
    ), { installed: [v1361, v1350], active: '1.135.0' })
    const m2 = await mountView()
    const runningCard = m2.wrapper.findAll('.installed-card').find((c) => c.text().includes('1.136.1'))!
    const ru = runningCard.findAll('button').find((b) => b.text() === '卸载')!
    expect(ru.attributes('disabled')).toBeDefined()
    expect(ru.attributes('title')).toBe('请先退出该便携版实例')
    expect(runningCard.text()).toContain('运行中') // 非使用中但运行 → badge-running
    await bar(m2.wrapper, 'portable').findAll('.control-btns .btn')[2].trigger('click') // 📂 位置
    await flushMicrotasks()
    expect(svc.OpenDir).toHaveBeenLastCalledWith('D:\\vscode\\versions\\vscode_1.136.1') // 运行版本优先于 active
    m2.wrapper.unmount()
  })
})

describe('VSCodeView 导入与 extras 联动', () => {
  it('importLocal：prompt 闸文案、取消不动、trim 提交与 toast', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')))
    const { wrapper } = await mountView()
    const importBtn = wrapper.findAll('.btn-group button')[0]
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toBe('导入本地便携版 VS Code')
    expect(promptState.options.label).toContain('Code.exe')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    svc.ImportLocal.mockResolvedValue({ ...v1361, version: '1.137.0' })
    await importBtn.trigger('click')
    await flushMicrotasks()
    settlePrompt('  D:\\my-vscode  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('D:\\my-vscode')
    expect(useToast().toastMsg.value).toBe('已导入便携版 1.137.0')
    wrapper.unmount()
  })

  it('extras：随关勾选成功/失败回滚、桌面快捷方式、官网复制与打开', async () => {
    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')))
    svc.GetFollowOnExit.mockResolvedValue(true)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    const { wrapper } = await mountView()
    const input = wrapper.find('.extras-card .toggle-label input')
    expect((input.element as HTMLInputElement).checked).toBe(true)
    await input.setValue(false)
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false)
    expect(useToast().toastMsg.value).toBe('已关闭：Hanxi 退出不影响 VS Code，继续独立运行（下次启动生效）')
    wrapper.unmount()

    stubDefaults(statusOfForms(snap('stopped'), snap('stopped')))
    svc.GetFollowOnExit.mockResolvedValue(true)
    svc.SetFollowOnExit.mockRejectedValue(new Error('配置写失败'))
    const m2 = await mountView()
    await m2.wrapper.find('.extras-card .toggle-label input').setValue(false)
    await flushMicrotasks()
    expect((m2.wrapper.find('.extras-card .toggle-label input').element as HTMLInputElement).checked).toBe(true) // 失败回滚
    expect(useToast().toastMsg.value).toBe('设置失败: 配置写失败')
    await m2.wrapper.find('.extras-card .btn-secondary').trigger('click') // 🖥 创建桌面快捷方式
    await flushMicrotasks()
    expect(svc.CreateDesktopShortcut).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('桌面快捷方式已创建（指向当前使用便携版）')
    expect(m2.wrapper.find('.repo-addr').text()).toBe('https://code.visualstudio.com')
    await m2.wrapper.findAll('.repo-row .link-button')[0].trigger('click') // 复制
    await flushMicrotasks()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('https://code.visualstudio.com')
    expect(useToast().toastMsg.value).toBe('官网地址已复制')
    await m2.wrapper.findAll('.repo-row .link-button')[1].trigger('click') // 浏览器打开
    await flushMicrotasks()
    expect(svc.OpenRepository).toHaveBeenCalledTimes(1)
    m2.wrapper.unmount()
  })
})

describe('VSCodeView 轮询与生命周期', () => {
  it('双事件注册、2.5s 状态轮询 KeepAlive 停用即止、卸载双注销、1s uptime ticker 按 startedAt 重算', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults(statusOfForms(
        snap('running', { form: 'portable', version: '1.136.1', startedAt: new Date(Date.now() - 65000).toISOString() }),
        snap('stopped', { form: 'installer' }),
      ))
      const { wrapper, show } = await mountView()
      expect(Object.keys(runtime.handlers).sort()).toEqual(['vscode:instance-state', 'vscode:version-download'])
      expect(bar(wrapper, 'portable').find('.uptime-tag').text()).toBe('⏱ 00:00') // 首帧 tick 早于快照就绪
      await vi.advanceTimersByTimeAsync(1000)
      expect(bar(wrapper, 'portable').find('.uptime-tag').text()).toBe('⏱ 01:06') // (65+1)s，按 startedAt 重算
      const base = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
      show.value = false
      await nextTick()
      const afterOff = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 5)
      expect(svc.GetStatus.mock.calls.length).toBe(afterOff)
      wrapper.unmount()
      await nextTick()
      expect(runtime.unlisten).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })
})
