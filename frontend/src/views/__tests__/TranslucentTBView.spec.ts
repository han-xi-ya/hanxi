// 特征测试（Wave 5 · 批 1 收敛件）：TranslucentTBView——四件中最完整的共享契约
// 消费样本（版本 Tab 全量 ManagedVersionPanel）。
// 锁定：控制条钮序（启动/重设/安装目录/退出）与各态禁用与 title、reset 槽动词
// （成功回执不刷快照）、启动钮的「无已装版本」禁用旁路、banner/hint 互斥、
// 共享面板词表（校验解压安装/已安装 btn-ghost/预发布徽标/远程空态）、
// 确认与导入文案逐字、事件改写与 KeepAlive 轮询契约。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TranslucentTBView from '../TranslucentTBView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  Start: vi.fn(),
  ResetState: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/translucenttb/translucenttbservice', () => svc)

const installed20261 = {
  version: '2026.1',
  exePath: 'C:\\data\\ttb\\2026.1\\TranslucentTB.exe',
  dir: 'C:\\data\\ttb\\2026.1',
  size: 512000,
  installedAt: '2026-08-01',
  isImport: false,
  source: '',
}

const installed20262 = {
  version: '2026.2',
  exePath: 'C:\\data\\ttb\\2026.2\\TranslucentTB.exe',
  dir: 'C:\\data\\ttb\\2026.2',
  size: 514048,
  installedAt: '2026-08-15',
  isImport: false,
  source: '',
}

const release20262 = {
  version: '2026.2',
  published: '2026-08-14T00:00:00Z',
  isPre: false,
  assetName: 'TranslucentTB-portable-x64.zip',
  assetUrl: 'https://github.com/TranslucentTB/TranslucentTB/releases/download/v2026.2/TranslucentTB-portable-x64.zip',
  size: 2048000,
  sha256: 'b'.repeat(64),
}

function stubDefaults(
  snap: Record<string, unknown>,
  opts: { installed?: unknown[]; releases?: unknown[]; active?: string } = {},
) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [installed20261])
  svc.ListReleases.mockResolvedValue(opts.releases ?? [release20262])
  svc.GetActiveVersion.mockResolvedValue(opts.active ?? '2026.1')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/TranslucentTB/TranslucentTB')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(TranslucentTBView)) : h('div')),
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

describe('TranslucentTBView 钮区（声明式主钮 + 走槽钮）', () => {
  it('钮序逐字：启动/重设/安装目录/退出', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns.map((b) => b.text())).toEqual(['🌫️ 启动', '🪄 重设任务栏状态', '🗂 安装目录', '⏻ 退出'])
    wrapper.unmount()
  })

  it('启动钮：无已装版本禁用并指引版本管理；已装 stopped 可点（Start 回执 + 刷状态）', async () => {
    stubDefaults({ state: 'stopped' }, { installed: [] })
    let r = await mountInKeepAlive()
    const start = () => r.wrapper.findAll('.control-btns .btn')[0]
    expect(start().attributes('disabled')).toBeDefined()
    expect(start().attributes('title')).toBe('尚未安装，请先在「版本管理」下载')
    r.wrapper.unmount()

    stubDefaults({ state: 'stopped' })
    svc.Start.mockResolvedValue({ message: 'TranslucentTB 已启动（驻系统托盘）' })
    r = await mountInKeepAlive()
    expect(start().attributes('disabled')).toBeUndefined()
    expect(start().attributes('title')).toBe('启动 TranslucentTB（驻系统托盘）')
    const before = svc.GetStatus.mock.calls.length
    await start().trigger('click')
    await flushMicrotasks()
    expect(svc.Start).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('TranslucentTB 已启动（驻系统托盘）')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    r.wrapper.unmount()
  })

  it('启动钮他态：running 禁用「已在运行」；external 禁用「外部实例已在运行」', async () => {
    stubDefaults({ state: 'running', version: '2026.1' })
    let r = await mountInKeepAlive()
    const start = r.wrapper.findAll('.control-btns .btn')[0]
    expect(start.attributes('disabled')).toBeDefined()
    expect(start.attributes('title')).toBe('已在运行')
    r.wrapper.unmount()

    stubDefaults({ state: 'external' })
    r = await mountInKeepAlive()
    const start2 = r.wrapper.findAll('.control-btns .btn')[0]
    expect(start2.attributes('disabled')).toBeDefined()
    expect(start2.attributes('title')).toBe('外部实例已在运行')
    r.wrapper.unmount()
  })

  it('重设钮走 reset 槽：running/external 可用，ResetState 回执（失败裸错误串）', async () => {
    stubDefaults({ state: 'stopped' })
    let r = await mountInKeepAlive()
    const reset = () => r.wrapper.findAll('.control-btns .btn')[1]
    expect(reset().attributes('disabled')).toBeDefined()
    expect(reset().attributes('title')).toBe('实例未在运行')
    r.wrapper.unmount()

    stubDefaults({ state: 'running', version: '2026.1' })
    svc.ResetState.mockResolvedValue({ message: '已向实例投递重设指令' })
    r = await mountInKeepAlive()
    expect(reset().attributes('disabled')).toBeUndefined()
    expect(reset().attributes('title')).toContain('Reset dynamic state')
    await reset().trigger('click')
    await flushMicrotasks()
    expect(svc.ResetState).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('已向实例投递重设指令')
    r.wrapper.unmount()

    stubDefaults({ state: 'running', version: '2026.1' })
    svc.ResetState.mockRejectedValue(new Error('信使通道断开'))
    r = await mountInKeepAlive()
    await reset().trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('信使通道断开')
    r.wrapper.unmount()
  })

  it('安装目录钮：按 running > active > 任一已装解析并 OpenDir 直达', async () => {
    stubDefaults({ state: 'running', version: '2026.2', pid: 7 }, { installed: [installed20261, installed20262], active: '2026.1' })
    const { wrapper } = await mountInKeepAlive()
    const dirBtn = wrapper.findAll('.control-btns .btn')[2]
    expect(dirBtn.attributes('disabled')).toBeUndefined()
    await dirBtn.trigger('click')
    await flushMicrotasks()
    // 运行中优先当前版本，其次才是 active
    expect(svc.OpenDir).toHaveBeenCalledWith('C:\\data\\ttb\\2026.2')
    wrapper.unmount()
  })

  it('退出钮：stopped 禁用；external 可点且 title 指引托盘退出', async () => {
    stubDefaults({ state: 'stopped' })
    let r = await mountInKeepAlive()
    const quit = () => r.wrapper.findAll('.control-btns .btn')[3]
    expect(quit().attributes('disabled')).toBeDefined()
    r.wrapper.unmount()

    stubDefaults({ state: 'external' })
    svc.Quit.mockResolvedValue({ message: '已发送退出' })
    r = await mountInKeepAlive()
    expect(quit().attributes('disabled')).toBeUndefined()
    expect(quit().attributes('title')).toContain('托盘菜单退出')
    await quit().trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已发送退出')
    r.wrapper.unmount()
  })
})

describe('TranslucentTBView 状态投影', () => {
  it('banner 三分支互斥：external 可重设说明 / running 托盘设置 / failed 后端错误', async () => {
    stubDefaults({ state: 'external' })
    let r = await mountInKeepAlive()
    expect(r.wrapper.find('.banner').classes()).toContain('banner-warn')
    expect(r.wrapper.find('.banner').text()).toContain('可重设任务栏状态')
    r.wrapper.unmount()

    stubDefaults({ state: 'running', version: '2026.1' })
    r = await mountInKeepAlive()
    expect(r.wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(r.wrapper.find('.banner').text()).toContain('系统托盘图标菜单')
    r.wrapper.unmount()

    stubDefaults({ state: 'failed', error: '注入组件加载失败' })
    r = await mountInKeepAlive()
    expect(r.wrapper.find('.banner').text()).toContain('注入组件加载失败')
    r.wrapper.unmount()
  })

  it('stopped/starting 引导行', async () => {
    stubDefaults({ state: 'stopped' })
    let r = await mountInKeepAlive()
    expect(r.wrapper.find('.hint-line').text()).toContain('点击「🌫️ 启动」')
    expect(r.wrapper.find('.hint-line').text()).toContain('WinUI / VCLibs')
    r.wrapper.unmount()

    stubDefaults({ state: 'starting' })
    r = await mountInKeepAlive()
    expect(r.wrapper.find('.hint-line').text()).toContain('约 1~3 秒')
    r.wrapper.unmount()
  })
})

describe('TranslucentTBView 版本管理 Tab（共享 ManagedVersionPanel 全量接管）', () => {
  it('meta 提示行、首用空态、已装卡使用中徽标', async () => {
    stubDefaults({ state: 'stopped' }, { installed: [], releases: [release20262] })
    let r = await mountInKeepAlive()
    expect(r.wrapper.find('.empty-state.first-use').text()).toContain('尚未安装 TranslucentTB')
    expect(r.wrapper.findAll('.control-panel .btn-group button')[0].text()).toBe('⇥ 导入本地安装')
    r.wrapper.unmount()

    stubDefaults({ state: 'stopped' })
    r = await mountInKeepAlive()
    const meta = r.wrapper.find('.control-panel .meta-info')
    expect(meta.text()).toContain('portable-x64')
    expect(meta.text()).toContain('settings.json 随版本目录走')
    expect(r.wrapper.find('.installed-card .badge').text()).toBe('使用中')
    r.wrapper.unmount()
  })

  it('远程表：下载安装→DownloadVersion；verify 阶段「校验解压安装…」；已装行 btn-ghost', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release20262] })
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountInKeepAlive()
    const dlBtn = wrapper.findAll('.tbl button').find((b) => b.text() === '下载安装')!
    await dlBtn.trigger('click')
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2026.2')
    runtime.handlers['translucenttb:version-download']({ data: { version: '2026.2', stage: 'downloading', done: 25, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('25%')
    runtime.handlers['translucenttb:version-download']({ data: { version: '2026.2', stage: 'verify', done: 100, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toBe('校验解压安装…')
    wrapper.unmount()

    // 已装则共享面板以 btn-ghost 标记且无下载钮
    stubDefaults({ state: 'stopped' }, { installed: [installed20262] })
    const r = await mountInKeepAlive()
    expect(r.wrapper.find('.tbl .btn-ghost').text()).toBe('已安装')
    r.wrapper.unmount()
  })

  it('done 事件：800ms 清票据并重拉版本', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, { releases: [release20262] })
      const { wrapper } = await mountInKeepAlive()
      const before = svc.ListInstalledVersions.mock.calls.length
      runtime.handlers['translucenttb:version-download']({ data: { version: '2026.2', stage: 'done', done: 100, total: 100 } })
      await vi.advanceTimersByTimeAsync(0)
      expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(before)
      await vi.advanceTimersByTimeAsync(900)
      expect(wrapper.find('.ver-status.downloading').exists()).toBe(false)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('运行中版本禁止卸载：禁用 + title「请先退出 TranslucentTB」；卸载确认含 settings.json 备份预告', async () => {
    stubDefaults({ state: 'running', version: '2026.1' })
    const { wrapper } = await mountInKeepAlive()
    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    expect(uninstall.attributes('disabled')).toBeDefined()
    expect(uninstall.attributes('title')).toBe('请先退出 TranslucentTB')
    wrapper.unmount()

    stubDefaults({ state: 'stopped' })
    const r = await mountInKeepAlive()
    const btn = r.wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await btn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 TranslucentTB 2026.1？')
    expect(confirmState.options.description).toContain('如需保留请先备份')
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('2026.1')
    expect(useToast().toastMsg.value).toBe('已卸载 2026.1')
    r.wrapper.unmount()
  })

  it('导入本地：prompt 文案逐字（settings.json 整套迁入），提交 trim 后导入', async () => {
    stubDefaults({ state: 'stopped' }, { installed: [] })
    svc.ImportLocal.mockResolvedValue({ ...installed20261, isImport: true })
    const { wrapper } = await mountInKeepAlive()
    const importBtn = wrapper.findAll('.btn-group button')[0]
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.description).toContain('settings.json')
    expect(promptState.options.label).toContain('TranslucentTB.exe')
    settlePrompt('  E:\\TranslucentTB  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('E:\\TranslucentTB')
    expect(useToast().toastMsg.value).toBe('已导入 TranslucentTB 2026.1')
    wrapper.unmount()
  })

  it('随关勾选联动与仓库行', async () => {
    stubDefaults({ state: 'stopped' })
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.repo-row .k').text()).toBe('GitHub 仓库')
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

describe('TranslucentTBView 事件与轮询契约', () => {
  it('instance-state 事件即时改写界面；卸载注销双订阅', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['translucenttb:instance-state']({ data: { state: 'running', version: '2026.1', pid: 88 } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')
    expect(wrapper.find('.pid-tag').text()).toContain('88')
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
