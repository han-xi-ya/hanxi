// 特征测试：DdnsGoView 托管契约 + 本视图特有面（进程日志流 / Web 端口设置 / 面板子窗口动作）。
// 基线先以迁移前形态跑绿（16/16），迁移后仅卸载/导入两例改经 useConfirm/usePrompt 单例驱动，
// 可观察行为（是否调后端/toast/文案要素）不变。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DdnsGoView from '../DdnsGoView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  Start: vi.fn(),
  OpenConsole: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  OpenDir: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
  GetListenPort: vi.fn(),
  SetListenPort: vi.fn(),
  Logs: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/ddnsgo/ddnsgoservice', () => svc)

const installedV66 = {
  version: '6.6.0',
  exePath: 'C:\\data\\ddnsgo\\6.6.0\\ddns-go.exe',
  dir: 'C:\\data\\ddnsgo\\6.6.0',
  size: 20 * 1024 * 1024,
  installedAt: '2026-08-01',
}

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/jeessy2/ddns-go')
  svc.GetListenPort.mockResolvedValue(9876)
  svc.Logs.mockResolvedValue(['启动历史行1', 'IP 未变化'])
}

const show = ref(true)

async function mountView() {
  show.value = true
  const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(DdnsGoView)) : h('div')) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return wrapper
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  useToast().clearToast()
  // 对话框单例兜底复位（正常用例内都已 settle）
  useConfirm().settleConfirm(false)
  usePrompt().settlePrompt(null)
})

describe('DdnsGoView 初始装载', () => {
  it('挂载拉取状态/版本/开关/端口，并回填最近日志历史（Logs(200)）', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.GetListenPort).toHaveBeenCalled()
    expect(svc.Logs).toHaveBeenCalledWith(200)
    expect(w.find('.dd-log-line').text()).toBe('启动历史行1')
    w.unmount()
  })

  it('订阅三条事件（download/state/log）', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    expect(Object.keys(runtime.handlers).sort()).toEqual(
      ['ddnsgo:instance-log', 'ddnsgo:instance-state', 'ddnsgo:version-download'],
    )
    w.unmount()
  })
})

describe('DdnsGoView 状态与横幅', () => {
  const cases: Array<[Record<string, unknown>, string]> = [
    [{ state: 'stopped' }, '未运行'],
    [{ state: 'running', version: '6.6.0', listenAddr: '127.0.0.1:9876' }, '运行中'],
    [{ state: 'starting' }, '启动中…'],
    [{ state: 'failed', error: '端口被占' }, '异常退出'],
    [{ state: 'external' }, '外部运行'],
  ]

  it('五态文案矩阵', async () => {
    for (const [snap, text] of cases) {
      stubDefaults(snap)
      const w = await mountView()
      expect(w.find('.status-word').text()).toBe(text)
      w.unmount()
    }
  })

  it('external 提示 Windows 服务/面板退出；running 提示含面板地址；addr-tag 显示监听地址', async () => {
    stubDefaults({ state: 'external' })
    let w = await mountView()
    expect(w.find('.banner').text()).toContain('Windows 服务')
    w.unmount()

    stubDefaults({ state: 'running', version: '6.6.0', listenAddr: '127.0.0.1:9876' })
    w = await mountView()
    expect(w.find('.banner').text()).toContain('http://127.0.0.1:9876')
    expect(w.find('.addr-tag').text()).toContain('127.0.0.1:9876')
    w.unmount()
  })
})

describe('DdnsGoView 进程日志流', () => {
  it('instance-log 事件逐行追加；warnish 特征词行挂警示着色；「未变化」不算异常', async () => {
    stubDefaults({ state: 'running', version: '6.6.0', listenAddr: '127.0.0.1:9876' })
    const w = await mountView()
    runtime.handlers['ddnsgo:instance-log']({ data: { line: '更新失败: AliDNS refused' } })
    runtime.handlers['ddnsgo:instance-log']({ data: { line: 'IP 未变化，跳过更新' } })
    await nextTick()
    const lines = w.findAll('.dd-log-line')
    expect(lines[lines.length - 2].classes()).toContain('dd-log-warn')
    expect(lines[lines.length - 1].classes()).not.toContain('dd-log-warn')
    w.unmount()
  })

  it('清屏按钮清空日志区', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    expect(w.findAll('.dd-log-line').length).toBe(2)
    await w.find('.dd-log-tools button').trigger('click')
    expect(w.findAll('.dd-log-line').length).toBe(0)
    w.unmount()
  })

  it('状态切入 running 时重取日志历史（引擎环形缓冲重启）', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    const before = svc.Logs.mock.calls.length
    runtime.handlers['ddnsgo:instance-state']({ data: { state: 'running', version: '6.6.0', listenAddr: '127.0.0.1:9876' } })
    await flushMicrotasks()
    expect(svc.Logs.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
  })
})

describe('DdnsGoView 控制操作', () => {
  it('启动：成功 toast 回执并刷新；失败 toast 原始错误', async () => {
    stubDefaults({ state: 'stopped' })
    svc.Start.mockResolvedValue({ message: 'ddns-go 已后台启动' })
    let w = await mountView()
    await w.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.Start).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('ddns-go 已后台启动')
    w.unmount()

    svc.Start.mockRejectedValue(new Error('端口就绪超时'))
    w = await mountView()
    await w.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('端口就绪超时')
    w.unmount()
  })

  it('打开控制台走 OpenConsole（子 Webview 窗口）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenConsole.mockResolvedValue({ message: '面板窗口已打开' })
    const w = await mountView()
    await w.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenConsole).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('面板窗口已打开')
    w.unmount()
  })

  it('退出失败 toast 带「退出失败: 」前缀', async () => {
    stubDefaults({ state: 'running', version: '6.6.0' })
    svc.Quit.mockRejectedValue(new Error('配置写静默期超时'))
    const w = await mountView()
    await w.findAll('.control-btns .btn')[2].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('退出失败: 配置写静默期超时')
    w.unmount()
  })
})

describe('DdnsGoView Web 端口设置', () => {
  it('非法端口：toast 约束文案且不调后端', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    await w.find('.dd-port-input').setValue(80)
    await w.find('.port-ctrl button').trigger('click')
    await flushMicrotasks()
    expect(svc.SetListenPort).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toContain('1024~65535')
    w.unmount()
  })

  it('合法端口：运行中返回 pending 时提示下次启动生效', async () => {
    stubDefaults({ state: 'running', version: '6.6.0' })
    svc.SetListenPort.mockResolvedValue('pending')
    const w = await mountView()
    await w.find('.dd-port-input').setValue(9999)
    await w.find('.port-ctrl button').trigger('click')
    await flushMicrotasks()
    expect(svc.SetListenPort).toHaveBeenCalledWith(9999)
    expect(useToast().toastMsg.value).toContain('下次启动生效')
    w.unmount()
  })
})

describe('DdnsGoView 版本管理', () => {
  it('卸载经确认：取消不动后端（迁移后走 useConfirm 单例，文案要素锁定）', async () => {
    const { confirmState, settleConfirm } = useConfirm()
    stubDefaults({ state: 'stopped' }, [installedV66])
    const w = await mountView()
    const uninstallBtn = w.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(JSON.stringify(confirmState.options.details)).toContain('ddns_go_config')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    w.unmount()
  })

  it('导入本地：取消不动后端；有值 trim 调 ImportLocal（迁移后走 usePrompt 单例）', async () => {
    const { promptState, settlePrompt } = usePrompt()
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ version: '6.5.0' })
    const w = await mountView()
    await w.find('.btn-group button').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    settlePrompt('  D:\\ddns-go  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('D:\\ddns-go')
    expect(useToast().toastMsg.value).toBe('已导入 ddns-go 6.5.0')
    w.unmount()
  })

  it('download 事件驱动进度百分比与 done 后消失', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'stopped' }, [], [{ version: '6.7.0', size: 100, published: '2026-08-02T00:00:00Z', isPre: false }])
    const w = await mountView()
    runtime.handlers['ddnsgo:version-download']({ data: { version: '6.7.0', stage: 'downloading', done: 60, total: 100 } })
    await nextTick()
    expect(w.find('.dl-percent').text()).toBe('60%')
    runtime.handlers['ddnsgo:version-download']({ data: { version: '6.7.0', stage: 'done', done: 100, total: 100 } })
    await vi.advanceTimersByTimeAsync(900)
    expect(w.find('.dl-percent').exists()).toBe(false)
    w.unmount()
    vi.useRealTimers()
  })
})

describe('DdnsGoView 轮询与 KeepAlive', () => {
  it('激活后 2.5s 轮询刷新；停用后轮询静默', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'running', version: '6.6.0', startedAt: new Date().toISOString() })
    const w = await mountView()
    const base = svc.GetStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 3)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
    show.value = false
    await nextTick()
    const afterOff = svc.GetStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 5)
    expect(svc.GetStatus.mock.calls.length).toBe(afterOff)
    w.unmount()
    vi.useRealTimers()
  })
})
