// 特征测试：MangoDiskView（原版 GUI 纯托管 + 完整性漂移检测 + 双定时器 +
// window.confirm/prompt 原生弹窗基线——迁移到 useConfirm/usePrompt 后机制变、语义不变）。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import MangoDiskView from '../MangoDiskView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetFollowOnExit: vi.fn(),
  GetStatus: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenDir: vi.fn(),
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
vi.mock('../../../bindings/hanxi/internal/modules/mangodisk/mangodiskservice', () => svc)

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const snapOf = (over = {}) => ({ state: 'stopped', version: '', pid: 0, error: '', ...over })
const installedOf = (version: string, over = {}) => ({
  version, dir: `C:\\d\\${version}`, exePath: `C:\\d\\${version}\\MangoDisk.exe`,
  size: 8 * 1024 * 1024, installedAt: '2026-08-01', integrity: 'verified',
  integrityNote: '哈希与 PE 身份一致', isImport: false, currentSha256: 'b'.repeat(64),
  fileVersion: version, ...over,
})
const releaseOf = (version: string, over = {}) => ({
  version, size: 8 * 1024 * 1024, published: '2026-08-02', isPre: false, ...over,
})

function stubDefaults(snap: Record<string, unknown>, local: unknown[] = [], remote: unknown[] = [], active = '') {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(local)
  svc.ListReleases.mockResolvedValue(remote)
  svc.GetActiveVersion.mockResolvedValue(active)
  svc.GetFollowOnExit.mockResolvedValue(true)
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(MangoDiskView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

async function gotoVersions(wrapper: ReturnType<typeof mount>) {
  await wrapper.findAll('.main-tab-btn')[1].trigger('click')
}

beforeEach(() => {
  svc.OpenWindow.mockResolvedValue({ message: '窗口已唤起' })
  svc.Quit.mockResolvedValue({ message: '已退出' })
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useToast().clearToast()
  useConfirm().settleConfirm(false) // 单例卫生：未裁决的确认不得泄漏到下条用例
  usePrompt().settlePrompt(null)
})

describe('MangoDiskView 控制台状态', () => {
  it('未运行：徽标「未运行」、主按钮「启动 MangoDisk」、退出禁用、无横幅', async () => {
    stubDefaults(snapOf(), [installedOf('1.1.0')], [], '1.1.0')
    const { wrapper } = await mountView()
    expect(wrapper.find('.md-state-badge').text()).toBe('未运行')
    expect(wrapper.find('.banner').exists()).toBe(false)
    expect(wrapper.find('.btn-primary').text()).toBe('启动 MangoDisk')
    expect(wrapper.findAll('.md-actions button')[1].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('运行中：横幅 ok、退出可用、复制区显示 PID 与时长', async () => {
    stubDefaults(snapOf({ state: 'running', version: '1.1.0', pid: 1234 }), [installedOf('1.1.0')], [], '1.1.0')
    const { wrapper } = await mountView()
    expect(wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(wrapper.find('.banner').text()).toContain('正在运行')
    expect(wrapper.find('.md-control-copy').text()).toContain('PID 1234')
    wrapper.unmount()
  })

  it('外部实例：warn 横幅 + 打开窗口文案 + 退出禁用', async () => {
    stubDefaults(snapOf({ state: 'external' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.banner').classes()).toContain('banner-warn')
    expect(wrapper.find('.banner').text()).toContain('外部实例')
    expect(wrapper.find('.btn-primary').text()).toBe('打开窗口')
    wrapper.unmount()
  })

  it('完整性优先级：drifted 横幅压过 running-ok（锁现状优先级）', async () => {
    stubDefaults(
      snapOf({ state: 'running', version: '1.1.0', pid: 1 }),
      [installedOf('1.1.0', { integrity: 'drifted', integrityNote: '上游更新器替换了 EXE' })],
      [], '1.1.0',
    )
    const { wrapper } = await mountView()
    expect(wrapper.find('.banner').classes()).toContain('banner-warn')
    expect(wrapper.find('.banner').text()).toContain('上游更新器替换了 EXE')
    wrapper.unmount()
  })

  it('启动动作：OpenWindow → toast → 双复刷；失败仅 loadVersions', async () => {
    stubDefaults(snapOf())
    const { wrapper } = await mountView()
    await wrapper.find('.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow.mock.calls.length).toBe(1)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(1)
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(1)
    wrapper.unmount()
  })
})

describe('MangoDiskView 版本仓库', () => {
  it('已装行：integrity 徽标文案、使用中 pill、卸载禁用（运行中版本）', async () => {
    stubDefaults(
      snapOf({ state: 'running', version: '1.1.0', pid: 1 }),
      [installedOf('1.1.0'), installedOf('1.0.0', { integrity: 'invalid', integrityNote: '文件缺失' })],
      [releaseOf('1.2.0')], '1.1.0',
    )
    const { wrapper } = await mountView()
    await gotoVersions(wrapper)
    const rows = wrapper.findAll('.md-version-row')
    expect(rows.length).toBe(2)
    expect(rows[0].text()).toContain('使用中')
    expect(rows[0].text()).toContain('官方校验')
    expect(rows[1].text()).toContain('安装无效')
    const uninstallBtns = rows.map(r => r.findAll('button').find(b => b.text() === '卸载')!)
    expect(uninstallBtns[0].attributes('disabled')).toBeDefined() // 运行版本不可卸
    expect(uninstallBtns[1].attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('卸载走确认：取消不调后端，确认则 RemoveVersion + toast（机制已由 window.confirm 收编至 useConfirm，语义不变）', async () => {
    const { confirmState, settleConfirm } = useConfirm()
    stubDefaults(snapOf(), [installedOf('1.0.0')], [], '')
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    await gotoVersions(wrapper)
    const uninstallBtn = () => wrapper.findAll('.md-version-row button').find(b => b.text() === '卸载')!
    await uninstallBtn().trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('MangoDisk 1.0.0')
    expect(confirmState.options.description).toContain('不会删除 %LOCALAPPDATA%')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion.mock.calls.length).toBe(0)
    await uninstallBtn().trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion.mock.calls[0]).toEqual(['1.0.0'])
    expect(useToast().toastMsg.value).toBe('已卸载 1.0.0')
    wrapper.unmount()
  })

  it('导入本地：prompt 取消/空串不动后端；有值 trim 后 ImportLocal（机制已由 window.prompt 收编至 usePrompt，语义不变）', async () => {
    const { promptState, settlePrompt } = usePrompt()
    stubDefaults(snapOf(), [], [], '')
    svc.ImportLocal.mockResolvedValue(installedOf('9.9.9'))
    const { wrapper } = await mountView()
    await gotoVersions(wrapper)
    const importBtn = () => wrapper.findAll('.md-toolbar button').find(b => b.text() === '导入本地 EXE')!
    await importBtn().trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.description).toContain('Hanxi 只导入该 EXE')
    settlePrompt(null) // 取消
    await flushMicrotasks()
    expect(svc.ImportLocal.mock.calls.length).toBe(0)
    await importBtn().trigger('click')
    await flushMicrotasks()
    settlePrompt('') // 空串同样拒绝
    await flushMicrotasks()
    expect(svc.ImportLocal.mock.calls.length).toBe(0)
    await importBtn().trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\x\\MangoDisk.exe  ')
    await flushMicrotasks()
    expect(svc.ImportLocal.mock.calls[0]).toEqual(['C:\\x\\MangoDisk.exe'])
    wrapper.unmount()
  })

  it('退出联动开关：成功翻转+toast；失败回滚不翻转', async () => {
    stubDefaults(snapOf(), [installedOf('1.1.0')], [], '1.1.0')
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    const sw = wrapper.find('.md-switch')
    expect(sw.text()).toBe('已开启')
    await sw.trigger('click')
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false)
    expect(wrapper.find('.md-switch').text()).toBe('已关闭')
    svc.SetFollowOnExit.mockRejectedValue(new Error('存储锁'))
    await wrapper.find('.md-switch').trigger('click')
    await flushMicrotasks()
    expect(wrapper.find('.md-switch').text()).toBe('已关闭') // 失败不改值
    wrapper.unmount()
  })

  it('远程下载：进度事件渲染百分比条；done 800ms 后消失并复刷列表', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults(snapOf(), [], [releaseOf('1.2.0')], '')
      const { wrapper } = await mountView()
      await vi.advanceTimersByTimeAsync(0)
      await gotoVersions(wrapper)
      runtime.handlers['mangodisk:version-download']({
        data: { version: '1.2.0', stage: 'downloading', done: 40, total: 100 },
      })
      await nextTick()
      expect(wrapper.find('.md-progress').exists()).toBe(true)
      runtime.handlers['mangodisk:version-download']({
        data: { version: '1.2.0', stage: 'done', done: 100, total: 100 },
      })
      await nextTick()
      const lists = svc.ListInstalledVersions.mock.calls.length
      await vi.advanceTimersByTimeAsync(820)
      expect(svc.ListInstalledVersions.mock.calls.length).toBe(lists + 1)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('2.5s 状态轮询与 1s uptime tick；KeepAlive 停用双停', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults(snapOf({ state: 'running', version: '1.1.0', pid: 1 }), [installedOf('1.1.0')], [], '1.1.0')
      const { wrapper, show } = await mountView()
      const before = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 2)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(before + 2)
      show.value = false
      await nextTick()
      const idle = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 5)
      expect(svc.GetStatus.mock.calls.length).toBe(idle) // 停用不泄漏
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('instance-state 事件即时改写界面', async () => {
    stubDefaults(snapOf({ state: 'stopped' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.md-state-badge').text()).toBe('未运行')
    runtime.handlers['mangodisk:instance-state']({ data: snapOf({ state: 'running', pid: 9, version: '1.1.0' }) })
    await nextTick()
    expect(wrapper.find('.md-state-badge').text()).toBe('运行中')
    wrapper.unmount()
  })
})

