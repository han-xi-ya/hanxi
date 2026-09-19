// 特征测试（Wave 5 · 批 1 收敛件）：RufusView——共享契约（ManagedControlBar /
// ManagedExtrasCard + store）与方言版本 Tab（单文件版「校验落位…」、清单驱动
// #extras-action「打开位置」钮、740 提权重启）的迁移后行为基线。
// 锁定：banner 三分支互斥、启停钮声明（含提权 title）、下载事件流与重试链、
// 确认/输入文案逐字、事件改写与 KeepAlive 轮询契约。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RufusView from '../RufusView.vue'
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
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

// ElevateRestart 依赖宿主提权探测：默认按「已提权」隐身，提权用例单独覆写
const appSvc = vi.hoisted(() => ({
  IsElevated: vi.fn(),
  RestartElevated: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/rufus/rufusservice', () => svc)
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

const installedV415 = {
  version: 'v4.15',
  exePath: 'C:\\data\\rufus\\v4.15\\rufus.exe',
  dir: 'C:\\data\\rufus\\v4.15',
  size: 1835008,
  installedAt: '2026-08-01',
  isImport: false,
  source: '',
}

const releaseV416 = {
  version: 'v4.16',
  published: '2026-08-02T00:00:00Z',
  isPre: false,
  assetName: 'rufus-4.16p.exe',
  assetUrl: 'https://github.com/pbatard/rufus/releases/download/v4.16/rufus-4.16p.exe',
  size: 1888256,
  sha256: 'a'.repeat(64),
}

function stubDefaults(
  snap: Record<string, unknown>,
  opts: { installed?: unknown[]; releases?: unknown[] } = {},
) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [installedV415])
  svc.ListReleases.mockResolvedValue(opts.releases ?? [releaseV416])
  const inst = (opts.installed ?? [installedV415]) as Array<{ version?: string }>
  svc.GetActiveVersion.mockResolvedValue(inst[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/pbatard/rufus')
  appSvc.IsElevated.mockResolvedValue(true)
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(RufusView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  // 交互面经 useConfirm/usePrompt 单例收编，测试以 settle* 模拟对话框出口
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('RufusView 装载与状态投影', () => {
  it('挂载拉取状态/版本/活动版本/联动开关/仓库', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    for (const fn of [svc.GetStatus, svc.ListReleases, svc.ListInstalledVersions, svc.GetActiveVersion, svc.GetFollowOnExit, svc.RepositoryURL]) {
      expect(fn).toHaveBeenCalled()
    }
    wrapper.unmount()
  })

  it('running：banner-ok 含写盘不可中断警示；退出钮可用且 title 含半成品盘', async () => {
    stubDefaults({ state: 'running', version: 'v4.15', pid: 9, startedAt: new Date().toISOString() })
    const { wrapper } = await mountInKeepAlive()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-ok')
    expect(banner.text()).toContain('半成品启动盘')
    const quit = wrapper.findAll('.control-btns .btn')[1]
    expect(quit.attributes('disabled')).toBeUndefined()
    expect(quit.attributes('title')).toContain('半成品盘')
    wrapper.unmount()
  })

  it('external：banner-warn；退出 title 指引直接关窗', async () => {
    stubDefaults({ state: 'external' })
    const { wrapper } = await mountInKeepAlive()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('原名版')
    const quit = wrapper.findAll('.control-btns .btn')[1]
    expect(quit.attributes('disabled')).toBeUndefined()
    expect(quit.attributes('title')).toContain('在其窗口关闭')
    wrapper.unmount()
  })

  it('failed：banner-error 透后端错误；含「管理员」时追提权重启入口（未提权可见）', async () => {
    stubDefaults({ state: 'failed', error: '磁盘扫描被拒' })
    let r = await mountInKeepAlive()
    expect(r.wrapper.find('.banner').text()).toContain('磁盘扫描被拒')
    expect(r.wrapper.find('.elevate-restart').exists()).toBe(false)
    r.wrapper.unmount()

    stubDefaults({ state: 'failed', error: '启动失败：要求提升（管理员）' })
    appSvc.IsElevated.mockResolvedValue(false)
    r = await mountInKeepAlive()
    expect(r.wrapper.find('.elevate-restart').exists()).toBe(true)
    expect(r.wrapper.find('.elevate-restart').text()).toContain('以管理员身份重启')
    r.wrapper.unmount()
  })

  it('stopped：无 banner、引导行含提权告诫；starting：主钮禁用「启动中…」', async () => {
    stubDefaults({ state: 'stopped' })
    let r = await mountInKeepAlive()
    expect(r.wrapper.find('.banner').exists()).toBe(false)
    expect(r.wrapper.find('.hint-line').text()).toContain('要求提升')
    expect(r.wrapper.findAll('.control-btns .btn')[1].attributes('disabled')).toBeDefined()
    r.wrapper.unmount()

    stubDefaults({ state: 'starting' })
    r = await mountInKeepAlive()
    const open = r.wrapper.findAll('.control-btns .btn')[0]
    expect(open.attributes('disabled')).toBeDefined()
    expect(open.attributes('title')).toBe('启动中…')
    r.wrapper.unmount()
  })
})

describe('RufusView 控制操作', () => {
  it('打开窗口：OpenWindow 回执 toast 并刷新状态', async () => {
    stubDefaults({ state: 'stopped' })
    svc.OpenWindow.mockResolvedValue({ message: '已启动并唤起窗口' })
    const { wrapper } = await mountInKeepAlive()
    const before = svc.GetStatus.mock.calls.length
    await wrapper.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已启动并唤起窗口')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('退出：Quit 回执 toast；失败带「退出失败: 」前缀', async () => {
    stubDefaults({ state: 'running', version: 'v4.15' })
    svc.Quit.mockResolvedValue({ message: '已发送关窗消息' })
    let r = await mountInKeepAlive()
    await r.wrapper.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已发送关窗消息')
    r.wrapper.unmount()

    stubDefaults({ state: 'running', version: 'v4.15' })
    svc.Quit.mockRejectedValue(new Error('通道断开'))
    r = await mountInKeepAlive()
    await r.wrapper.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('退出失败: 通道断开')
    r.wrapper.unmount()
  })
})

describe('RufusView 联动卡与「打开位置」具名位', () => {
  it('未装任何版本：打开位置禁用；已装：title 指向运行/活动版本目录并可点达 OpenDir', async () => {
    stubDefaults({ state: 'stopped' }, { installed: [] })
    let r = await mountInKeepAlive()
    const dirBtn = () => r.wrapper.find('.extras-btns button')
    expect(dirBtn().attributes('disabled')).toBeDefined()
    expect(dirBtn().attributes('title')).toBe('尚未安装任何版本')
    r.wrapper.unmount()

    stubDefaults({ state: 'running', version: 'v4.15', pid: 9 })
    r = await mountInKeepAlive()
    const btn = r.wrapper.find('.extras-btns button')
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(btn.attributes('title')).toContain('v4.15 的便携目录')
    await btn.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenDir).toHaveBeenCalledWith('C:\\data\\rufus\\v4.15')
    r.wrapper.unmount()
  })

  it('随关勾选：翻转调 SetFollowOnExit 并弹对应回执；仓库钮调 OpenRepository', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const { wrapper } = await mountInKeepAlive()
    const checkbox = wrapper.find('.toggle-label input[type="checkbox"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    await checkbox.setValue(false)
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(false)
    expect(useToast().toastMsg.value).toContain('继续独立运行')
    await wrapper.findAll('.repo-row .link-button').find((b) => b.text() === '浏览器打开')!.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenRepository).toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('RufusView 方言版本 Tab（store 单源 + 「校验落位」词表）', () => {
  it('下载安装：DownloadVersion 按版本；downloading 事件驱动百分比；verify 阶段「校验落位…」', async () => {
    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountInKeepAlive()
    const dlBtn = wrapper.findAll('.tbl button').find((b) => b.text() === '下载安装')!
    await dlBtn.trigger('click')
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v4.16')
    runtime.handlers['rufus:version-download']({ data: { version: 'v4.16', stage: 'downloading', done: 50, total: 100 } })
    await nextTick()
    expect(wrapper.find('.rf-ver-status').classes()).toContain('downloading')
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    runtime.handlers['rufus:version-download']({ data: { version: 'v4.16', stage: 'verify', done: 100, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toBe('校验落位…')
    wrapper.unmount()
  })

  it('error 票据：失败态 + 重试链回到 DownloadVersion；下载抛错统一「下载失败: 」', async () => {
    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['rufus:version-download']({ data: { version: 'v4.16', stage: 'error', done: 0, total: 0, message: 'MZ 魔数不符' } })
    await nextTick()
    expect(wrapper.find('.rf-ver-status').classes()).toContain('error')
    expect(wrapper.find('.dl-error').attributes('title')).toBe('MZ 魔数不符')
    await wrapper.find('.retry-link').trigger('click')
    expect(svc.DownloadVersion).toHaveBeenCalledTimes(1)
    wrapper.unmount()

    stubDefaults({ state: 'stopped' })
    svc.DownloadVersion.mockRejectedValue(new Error('网络断'))
    const r = await mountInKeepAlive()
    await r.wrapper.findAll('.tbl button').find((b) => b.text() === '下载安装')!.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('下载失败: 网络断'))
    r.wrapper.unmount()
  })

  it('already-installed：回执 toast 并重拉版本；done 800ms 清票据', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      svc.DownloadVersion.mockResolvedValue('already-installed')
      const { wrapper } = await mountInKeepAlive()
      const callsBefore = svc.ListInstalledVersions.mock.calls.length
      await wrapper.findAll('.tbl button').find((b) => b.text() === '下载安装')!.trigger('click')
      await vi.advanceTimersByTimeAsync(0)
      expect(useToast().toastMsg.value).toBe('版本 v4.16 已安装')
      expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(callsBefore)
      // done 票据驻留 800ms 后清除
      svc.DownloadVersion.mockResolvedValue('started')
      runtime.handlers['rufus:version-download']({ data: { version: 'v4.16', stage: 'downloading', done: 1, total: 2 } })
      await vi.advanceTimersByTimeAsync(0)
      expect(wrapper.find('.rf-ver-status.downloading').exists()).toBe(true)
      runtime.handlers['rufus:version-download']({ data: { version: 'v4.16', stage: 'done', done: 2, total: 2 } })
      await vi.advanceTimersByTimeAsync(900)
      expect(wrapper.find('.rf-ver-status.downloading').exists()).toBe(false)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('卸载确认：取消不动后端；确认走 RemoveVersion 并 toast（rufus.ini 预告逐字）', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 Rufus v4.15？')
    expect(confirmState.options.description).toContain('rufus.ini 便携设置一并移除')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    await uninstall.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v4.15')
    expect(useToast().toastMsg.value).toBe('已卸载 v4.15')
    wrapper.unmount()
  })

  it('导入本地 exe：prompt 取消不导入；提交 trim 后导入并 toast', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ ...installedV415, version: 'v4.14', isImport: true })
    const { wrapper } = await mountInKeepAlive()
    const importBtn = wrapper.findAll('.btn-group button')[0]
    expect(importBtn.text()).toBe('⇥ 导入本地 exe')
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toContain('rufus-4.15p.exe')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    await importBtn.trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\dl\\rufus.exe  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\dl\\rufus.exe')
    expect(useToast().toastMsg.value).toBe('已导入 Rufus v4.14')
    wrapper.unmount()
  })

  it('运行中版本禁止卸载（按钮禁用 + title 指引先退出）', async () => {
    stubDefaults({ state: 'running', version: 'v4.15' })
    const { wrapper } = await mountInKeepAlive()
    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    expect(uninstall.attributes('disabled')).toBeDefined()
    expect(uninstall.attributes('title')).toBe('请先退出 Rufus')
    wrapper.unmount()
  })
})

describe('RufusView 事件与轮询契约', () => {
  it('instance-state 事件即时改写界面；卸载注销双订阅', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['rufus:instance-state']({ data: { state: 'running', version: 'v4.15', pid: 42 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.pid-tag').text()).toContain('42')
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2)
  })

  it('激活轮询 2.5s 刷状态；KeepAlive 停用后不泄漏', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
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
