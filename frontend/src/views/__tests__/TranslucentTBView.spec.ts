// 特征测试（Wave 5 · 批 1 收敛件 + 双形态 Wave 打包线）：TranslucentTBView——
// 四件中最完整的共享契约消费样本（版本 Tab 全量 ManagedVersionPanel）。
// 锁定：控制条钮序（启动/重设/安装目录/退出）与各态禁用与 title、reset 槽动词
// （成功回执不刷快照）、启动钮的「无已装版本」禁用旁路、banner/hint 互斥、
// 共享面板词表（校验解压安装/已安装 btn-ghost/预发布徽标/远程空态）、
// 确认与导入文案逐字、事件改写与 KeepAlive 轮询契约；
// 双形态 Wave 追加：打包版区块三态（已装/未装/预读失败降级）、busy 单飞、
// 缓存拦截文案裸串透传、远程行 #release-actions 标注与确认文案、
// AV 鉴别钮双语义（打包对照置顶 / 打包线不可用时降级钮零变化）。
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
  // 打包线冻结契约五动词（bindings 待再生，spec 全 mock 顶替）
  GetMsixState: vi.fn(),
  InstallMsix: vi.fn(),
  UninstallMsix: vi.fn(),
  RemoveMsixCache: vi.fn(),
  LaunchMsix: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))

// sysinfo 档案服务（B1 点名通路）：缺省按"模块被停用（调用门拒）"打桩——
// 既有用例一律走静默降级路径，AV 点名用例在体内覆写。
const sysinfo = vi.hoisted(() => ({ GetReport: vi.fn() }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/translucenttb/translucenttbservice', () => svc)
vi.mock('../../../bindings/hanxi/internal/modules/sysinfo/sysinfoservice', () => sysinfo)

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

// 打包线预读缺省形：未装、无缓存——区块渲染"未安装"行、probeReady=true（但
// 无 msixbundle 资产的远程行不触发对照钮）。既有 36 例即在此基线上零变化。
const msixNone = { installed: false, version: '', packageFamily: '', cache: [] }

function stubDefaults(
  snap: Record<string, unknown>,
  opts: { installed?: unknown[]; releases?: unknown[]; active?: string; msix?: unknown } = {},
) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(opts.installed ?? [installed20261])
  svc.ListReleases.mockResolvedValue(opts.releases ?? [release20262])
  svc.GetActiveVersion.mockResolvedValue(opts.active ?? '2026.1')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/TranslucentTB/TranslucentTB')
  svc.GetMsixState.mockResolvedValue(opts.msix ?? msixNone)
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
  sysinfo.GetReport.mockReset().mockRejectedValue(new Error('模块已停用'))
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

// ---------- 崩溃处置升级（2026-09-26）：B1 主动点名 + C-迷你降级直钮 ----------

const release20261 = { version: '2026.1', published: '2026-02-01T00:00:00Z', isPre: false, size: 2048000 }
const release20251 = { version: '2025.1', published: '2025-03-01T00:00:00Z', isPre: false, size: 2048000 }

const avFailed = {
  state: 'failed',
  version: '2026.2',
  pid: 0,
  exitCode: 3221225477, // 0xC0000005（本机账目正的 32 位值）
  error: 'TranslucentTB 异常退出（退出码 3221225477 / 0xC0000005），访问违例，非许可/依赖问题。排查按序——① 重启电脑后再启动一次',
  external: false,
  startedAt: '',
  stoppedAt: '2026-09-26T10:00:03Z',
}

describe('TranslucentTBView AV 崩溃处置（B1 点名 + C-迷你降级钮）', () => {
  it('B1：GetReport 命中 → banner 追加点名句，后端话术逐字保真', async () => {
    sysinfo.GetReport.mockResolvedValue({
      gpus: [{ desc: 'ToDesk Virtual Display Adapter' }],
      displays: [{ name: '\\\\.\\DISPLAY7', width: 0, height: 0, primary: false }],
    })
    stubDefaults(avFailed, { installed: [installed20262], releases: [release20262, release20261] })
    const { wrapper } = await mountInKeepAlive()
    await flushMicrotasks()
    const text = wrapper.find('.banner').text()
    expect(text).toContain('排查按序') // 后端动态话术逐字在前
    expect(text).toContain('检见虚拟显卡「ToDesk Virtual Display Adapter」')
    expect(text).toContain('空壳监视器「\\\\.\\DISPLAY7」')
    expect(text).toContain('建议设备管理器禁用该显示器验证')
    wrapper.unmount()
  })

  it('B1：sysinfo 停用（调用门拒）→ 静默降级通用 AV 话术，不打扰不报错', async () => {
    // beforeEach 缺省即 mockRejectedValue（停用形态）
    stubDefaults(avFailed, { installed: [installed20262], releases: [release20262] })
    const { wrapper } = await mountInKeepAlive()
    await flushMicrotasks()
    const text = wrapper.find('.banner').text()
    expect(text).toContain('排查按序')
    expect(text).not.toContain('检见')
    wrapper.unmount()
  })

  it('C-迷你：AV + 本机无旧版 + 远程有更早稳定版 → 「⬇ 装 2026.1 试」钮，点击走既有下载链', async () => {
    stubDefaults(
      avFailed,
      { installed: [installed20262], releases: [release20262, release20261, release20251] },
    )
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountInKeepAlive()
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns.map((b) => b.text())).toEqual([
      '🌫️ 启动', '🪄 重设任务栏状态', '🗂 安装目录', '⬇ 装 2026.1 试', '⏻ 退出',
    ])
    await btns[3].trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2026.1')
    wrapper.unmount()
  })

  it('C-迷你不出钮：本机已有旧版在场（后端话术直给）；非 AV 崩溃不点名不调档案', async () => {
    stubDefaults(
      avFailed,
      { installed: [installed20261, installed20262], releases: [release20262, release20261] },
    )
    let r = await mountInKeepAlive()
    expect(r.wrapper.findAll('.control-btns .btn').map((b) => b.text())).toEqual([
      '🌫️ 启动', '🪄 重设任务栏状态', '🗂 安装目录', '⏻ 退出',
    ])
    r.wrapper.unmount()

    const callsBefore = sysinfo.GetReport.mock.calls.length
    stubDefaults(
      { state: 'failed', version: '2026.2', exitCode: 1, error: 'TranslucentTB 异常退出（退出码 1）' },
      { installed: [installed20262], releases: [release20262, release20251] },
    )
    r = await mountInKeepAlive()
    expect(r.wrapper.findAll('.control-btns .btn')).toHaveLength(4)
    await flushMicrotasks()
    expect(sysinfo.GetReport.mock.calls.length).toBe(callsBefore)
    r.wrapper.unmount()
  })
})

// ---------- 双形态 Wave：打包版（系统管理）区块 / 远程行标注 / AV 打包对照联动 ----------

const release20262Msix = {
  ...release20262,
  assets: [
    { platform: 'windows', form: 'portable', label: 'TranslucentTB-portable-x64.zip', managed: true },
    { platform: 'windows', form: 'package', label: 'TranslucentTB_2026.2.0.0_x64.msixbundle', managed: false },
  ],
}

const msixInstalledState = {
  installed: true,
  version: '2026.2',
  packageFamily: '45896TranslucentTB.TransparentTB_8wekyb3d8bbwe',
  cache: [
    { version: '2026.2', path: 'C:\\data\\hanxi\\ttb-msix-cache\\bundle-2026.2.msixbundle', size: 4096000 },
    { version: '2026.1', path: 'C:\\data\\hanxi\\ttb-msix-cache\\bundle-2026.1.msixbundle', size: 3072000 },
  ],
}

describe('TranslucentTBView 打包版区块（状态/缓存/降级/动词）', () => {
  it('已装态：状态行（已装 vX + 包族 mono 截断带 title）+ 启动/卸载钮 + 缓存行 fmtSize 与移除钮', async () => {
    stubDefaults({ state: 'stopped' }, { msix: msixInstalledState })
    const { wrapper } = await mountInKeepAlive()
    const block = wrapper.find('.tb-msix')
    expect(block.find('.tb-msix-state.installed').text()).toContain('已装 v2026.2')
    const family = block.find('.tb-msix-family')
    expect(family.text()).toBe('45896TranslucentTB.TransparentTB_8wekyb3d8bbwe')
    expect(family.attributes('title')).toBe('45896TranslucentTB.TransparentTB_8wekyb3d8bbwe')
    expect(block.findAll('.tb-msix-actions .btn').map((b) => b.text())).toEqual(['▶ 启动', '卸载'])
    const rows = block.findAll('.tb-msix-cache-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.tb-msix-cache-size').text()).toBe('3.9 MB')
    expect(rows[1].find('.tb-msix-cache-size').text()).toBe('2.9 MB')
    expect(rows[1].findAll('button').map((b) => b.text())).toEqual(['移除'])
    wrapper.unmount()
  })

  it('未装态：出"未安装"行与「版本管理」指路，无启动/卸载钮', async () => {
    stubDefaults({ state: 'stopped' }) // 缺省 msixNone
    const { wrapper } = await mountInKeepAlive()
    const block = wrapper.find('.tb-msix')
    expect(block.text()).toContain('未安装')
    expect(block.text()).toContain('可装打包版')
    expect(block.findAll('.tb-msix-actions .btn')).toHaveLength(0)
    expect(block.findAll('.tb-msix-cache-row')).toHaveLength(0)
    wrapper.unmount()
  })

  it('GetMsixState 失败：全块降级单行提示（状态/缓存不渲染，便携区照常）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.GetMsixState.mockRejectedValue(new Error('Appx 包状态查询失败'))
    const { wrapper } = await mountInKeepAlive()
    const block = wrapper.find('.tb-msix')
    expect(block.find('.tb-msix-degraded').text()).toContain('打包版状态暂不可读取')
    expect(block.find('.tb-msix-status').exists()).toBe(false)
    expect(block.find('.tb-msix-cache').exists()).toBe(false)
    // 便携线零感知：控制条与远程区照常
    expect(wrapper.findAll('.control-btns .btn')).toHaveLength(4)
    wrapper.unmount()
  })

  it('启动钮走 LaunchMsix；卸载经 danger 确认（仅动包、便携缓存不碰）→ UninstallMsix → 重读', async () => {
    stubDefaults({ state: 'stopped' }, { msix: msixInstalledState })
    svc.LaunchMsix.mockResolvedValue(undefined)
    svc.UninstallMsix.mockResolvedValue(undefined)
    const { wrapper } = await mountInKeepAlive()
    const btns = () => wrapper.findAll('.tb-msix-actions .btn')
    await btns()[0].trigger('click')
    await flushMicrotasks()
    expect(svc.LaunchMsix).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('已提交打包版启动请求')

    await btns()[1].trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('卸载打包版 TranslucentTB？')
    expect(confirmState.options.tone).toBe('danger')
    expect(confirmState.options.description).toContain('2026.2')
    expect(confirmState.options.description).toContain('便携版文件与安装包缓存均不受影响')
    const readsBefore = svc.GetMsixState.mock.calls.length
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.UninstallMsix).toHaveBeenCalledTimes(1)
    expect(svc.GetMsixState.mock.calls.length).toBe(readsBefore + 1)
    expect(useToast().toastMsg.value).toBe('打包版已卸载（便携版未受影响）')
    wrapper.unmount()
  })

  it('缓存移除：运行中拦截等后端错误文案原样透传 toast（不加前缀）；成功走回执+重读', async () => {
    stubDefaults({ state: 'stopped' }, { msix: msixInstalledState })
    svc.RemoveMsixCache.mockRejectedValue(new Error('打包版 2026.2 正在运行，其安装包暂不可移除'))
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.tb-msix-cache-row')[0].findAll('button')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.RemoveMsixCache).toHaveBeenCalledWith('2026.2')
    expect(useToast().toastMsg.value).toBe('打包版 2026.2 正在运行，其安装包暂不可移除')
    wrapper.unmount()

    stubDefaults({ state: 'stopped' }, { msix: msixInstalledState })
    svc.RemoveMsixCache.mockResolvedValue(undefined)
    const r = await mountInKeepAlive()
    const readsBefore = svc.GetMsixState.mock.calls.length
    await r.wrapper.findAll('.tb-msix-cache-row')[0].findAll('button')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已移除打包版安装包缓存 2026.2')
    expect(svc.GetMsixState.mock.calls.length).toBe(readsBefore + 1)
    r.wrapper.unmount()
  })

  it('InstallMsix 在途：busy 闩住全部打包钮（含刷新），落定后复位', async () => {
    stubDefaults({ state: 'stopped' }, { msix: msixInstalledState, releases: [release20262Msix] })
    let settleInstall: ((v: undefined) => void) | null = null
    svc.InstallMsix.mockImplementation(() => new Promise<void>((res) => { settleInstall = res }))
    const { wrapper } = await mountInKeepAlive()
    const rowBtn = wrapper.findAll('.tbl button').find((b) => b.text() === '装打包版')!
    await rowBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.InstallMsix).toHaveBeenCalledWith('2026.2')
    // InstallMsix 未落定：区块钮/刷新/行钮全灭，且不误弹成功 toast
    expect(wrapper.find('.tb-msix-state.installed').classes()).toContain('installed')
    expect(wrapper.findAll('.tb-msix-actions .btn')[0].attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('.tb-msix-actions .btn')[1].attributes('disabled')).toBeDefined()
    expect(wrapper.find('.tb-msix-head .btn').attributes('disabled')).toBeDefined()
    expect(rowBtn.attributes('disabled')).toBeDefined()
    settleInstall!(undefined)
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('打包版 2026.2 已安装（便携版未受影响）')
    wrapper.unmount()
  })
})

describe('TranslucentTBView 远程行打包标注（ManagedVersionPanel #release-actions 槽）', () => {
  it('assets 含 .msixbundle：行挂「可装打包版」chip 与「装打包版」钮；确认文案点明互不干扰且不关闭便携实例', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release20262Msix] })
    svc.InstallMsix.mockResolvedValue(undefined)
    const { wrapper } = await mountInKeepAlive()
    const row = wrapper.findAll('.tbl tbody tr')[0]
    expect(row.find('.tb-msix-chip').text()).toBe('可装打包版')
    // chip title 词表走共享件：package → 「包」
    expect(row.find('.tb-msix-chip').attributes('title')).toContain('包形态')
    const btn = row.findAll('button').find((b) => b.text() === '装打包版')!
    await btn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('安装 TranslucentTB 2026.2 打包版？')
    expect(confirmState.options.description).toContain('互不干扰')
    expect(confirmState.options.description).toContain('不关闭当前运行的便携实例')
    const readsBefore = svc.GetMsixState.mock.calls.length
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.InstallMsix).toHaveBeenCalledWith('2026.2')
    expect(svc.GetMsixState.mock.calls.length).toBe(readsBefore + 1) // 完成后刷新 GetMsixState
    wrapper.unmount()
  })

  it('确认取消不落 InstallMsix', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release20262Msix] })
    const { wrapper } = await mountInKeepAlive()
    const btn = wrapper.findAll('.tbl button').find((b) => b.text() === '装打包版')!
    await btn.trigger('click')
    await flushMicrotasks()
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.InstallMsix).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('无 form/assets 数据：chip 与钮静默缺席（既有远程行零变化）', async () => {
    stubDefaults({ state: 'stopped' }, { releases: [release20262] })
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.tb-msix-chip').exists()).toBe(false)
    expect(wrapper.findAll('.tbl button').some((b) => b.text() === '装打包版')).toBe(false)
    wrapper.unmount()
  })
})

describe('TranslucentTBView AV 打包对照联动（优先语义，降级钮共存）', () => {
  it('AV + 打包线可用 + 崩溃版本有 msixbundle → 对照钮顶掉降级钮；点击免确认直 InstallMsix(崩溃版本)，成功文案宣布对照完成', async () => {
    stubDefaults(
      avFailed,
      { installed: [installed20262], releases: [release20262Msix, release20261] },
    ) // 缺省 msixNone：预读成功且未装 → probeReady
    svc.InstallMsix.mockResolvedValue(undefined)
    const { wrapper } = await mountInKeepAlive()
    await flushMicrotasks()
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns.map((b) => b.text())).toEqual([
      '🌫️ 启动', '🪄 重设任务栏状态', '🗂 安装目录', '⬇ 装打包版对照（#85 鉴别）', '⏻ 退出',
    ])
    await btns[3].trigger('click')
    await flushMicrotasks()
    expect(svc.InstallMsix).toHaveBeenCalledWith('2026.2') // 装的是崩溃版本本体：同版本异形态才是真鉴别
    expect(confirmState.open).toBe(false) // 对照直钮免确认
    expect(useToast().toastMsg.value).toContain('#85 对照步骤完成')
    wrapper.unmount()
  })

  it('打包版已装（probeReady 关）→ 原「⬇ 装 X 试」降级钮逐字回位，点击仍走便携下载链', async () => {
    stubDefaults(
      avFailed,
      { installed: [installed20262], releases: [release20262Msix, release20261], msix: msixInstalledState },
    )
    svc.DownloadVersion.mockResolvedValue('started')
    const { wrapper } = await mountInKeepAlive()
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns.map((b) => b.text())).toEqual([
      '🌫️ 启动', '🪄 重设任务栏状态', '🗂 安装目录', '⬇ 装 2026.1 试', '⏻ 退出',
    ])
    await btns[3].trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2026.1')
    expect(svc.InstallMsix).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('打包线预读失败（不可用）→ 对照钮缺席、降级钮零变化（既有 36 例口径）', async () => {
    stubDefaults(
      avFailed,
      { installed: [installed20262], releases: [release20262Msix, release20261] },
    )
    svc.GetMsixState.mockRejectedValue(new Error('Appx 包状态查询失败'))
    const { wrapper } = await mountInKeepAlive()
    await flushMicrotasks()
    const btns = wrapper.findAll('.control-btns .btn')
    expect(btns.map((b) => b.text())).toEqual([
      '🌫️ 启动', '🪄 重设任务栏状态', '🗂 安装目录', '⬇ 装 2026.1 试', '⏻ 退出',
    ])
    wrapper.unmount()
  })

  it('崩溃版本远程行无 msixbundle 资产 → 即便打包线可用也回降级钮（不硬造对照）', async () => {
    stubDefaults(
      avFailed,
      { installed: [installed20262], releases: [release20262, release20261] },
    )
    const { wrapper } = await mountInKeepAlive()
    await flushMicrotasks()
    expect(wrapper.findAll('.control-btns .btn')[3].text()).toBe('⬇ 装 2026.1 试')
    wrapper.unmount()
  })
})
