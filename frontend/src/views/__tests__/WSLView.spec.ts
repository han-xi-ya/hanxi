// 特征测试：WSLView 基线锁定（交互整改 W1–W9 后结构）。
// 断言对象：流式体检（Stale 保活复采）、busy 分级锁与跨页签进度、失败行内保活、
// 落位持久化后端 RPC 语义（set/dir + 防抖回写）、五页签结构（官方清单并入添加实例·商店源）、
// 提权确认链与危险级降噪、MSI 直链参数、端口转发规则清单、行级「⋯ 更多」下拉开合。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WSLView from '../WSLView.vue'

const api = vi.hoisted(() => ({
  StartReadiness: vi.fn(),
  GetReleases: vi.fn(),
  ListOnlineDistros: vi.fn(),
  InstallWsl: vi.fn(),
  UpdateWsl: vi.fn(),
  UpdateWslWebDownload: vi.fn(),
  SetDefaultVersion2: vi.fn(),
  InstallDistro: vi.fn(),
  InstallDistroTo: vi.fn(),
  UninstallWsl: vi.fn(),
  DisableWslFeatures: vi.fn(),
  EnableWslFeatures: vi.fn(),
  DownloadMsi: vi.fn(),
  RevealDownload: vi.fn(),
  OpenReleaseTag: vi.fn(),
  OpenReleasesPage: vi.fn(),
  OpenOfficialDocs: vi.fn(),
  OpenPowerSettings: vi.fn(),
  ListInstances: vi.fn(),
  OpenTerminal: vi.fn(),
  OpenDistroFolder: vi.fn(),
  RestartDistro: vi.fn(),
  TerminateDistro: vi.fn(),
  SetDefaultDistro: vi.fn(),
  UnregisterDistro: vi.fn(),
  ExportDistro: vi.fn(),
  ListDistroExports: vi.fn(),
  MoveDistro: vi.fn(),
  RevealDistroExport: vi.fn(),
  GetDistroForensics: vi.fn(),
  CloneDistro: vi.fn(),
  ImportDistro: vi.fn(),
  ImportDistroVhd: vi.fn(),
  PickDistroImageDialog: vi.fn(),
  PickFolderDialog: vi.fn(),
  GetWslConf: vi.fn(),
  SaveWslConf: vi.fn(),
  CompactDistro: vi.fn(),
  ListPortRules: vi.fn(),
  AddPortRule: vi.fn(),
  UpdatePortRule: vi.fn(),
  RemovePortRule: vi.fn(),
  ApplyPortRules: vi.fn(),
  CleanupPortRules: vi.fn(),
  ClearPortLedgerFile: vi.fn(),
  CancelClone: vi.fn(),
  CancelCompact: vi.fn(),
  CancelMsiDownload: vi.fn(),
  GetWslHostConf: vi.fn(),
  SaveWslHostConf: vi.fn(),
  ShutdownWsl: vi.fn(),
  // F9 USB 直通（usbipd-win 集成）
  GetUsbOverview: vi.fn(),
  BindUsbDevice: vi.fn(),
  UnbindUsbDevice: vi.fn(),
  UnbindAbsentUsbDevice: vi.fn(),
  AttachUsbDevice: vi.fn(),
  DetachUsbDevice: vi.fn(),
  SetUsbAutoAttach: vi.fn(),
  SetUsbShare: vi.fn(),
  SetUsbShareEnabled: vi.fn(),
  RemoveUsbShare: vi.fn(),
  ReplayUsbNow: vi.fn(),
  OpenUsbipdReleases: vi.fn(),
  // W1：落位持久化后端 RPC（真实绑定尚未生成，本 mock 先兜住模块路径）
  GetDistroInstallDir: vi.fn(),
  SetDistroInstallDir: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/wsl/wslservice', () => api)

// Wails 运行时事件总线桩：捕获订阅回调，测试内手动分相投喂。
const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data?: unknown }) => void>,
}))
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data?: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))

interface ConfirmOpts {
  title?: string
  description?: string
  tone?: string
  confirmLabel?: string
  details?: Array<{ label: string; value: string }>
}
const confirmFn = vi.hoisted(() => vi.fn(async (_opts: ConfirmOpts) => true))
vi.mock('../../composables/useConfirm', () => ({
  useConfirm: () => ({ confirm: confirmFn }),
}))

const SYSTEM_ITEMS = [
  { key: 'build', label: '系统版本', state: 'ok', value: 'Build 26200.9168', detail: '满足 WSL2 门槛' },
  { key: 'arch', label: 'CPU 架构', state: 'ok', value: 'AMD64', detail: '支持' },
  { key: 'virt', label: 'CPU 虚拟化 (VT-x / AMD-V)', state: 'ok', value: '监控程序运行中', detail: '正常' },
  { key: 'hypervisor', label: '虚拟机监控程序 (Hyper-V/VBS)', state: 'ok', value: '正在运行', detail: '正常' },
  { key: 'feature', label: '可选功能 (虚拟机平台)', state: 'ok', value: '虚拟机平台 已启用 · 旧版WSL 关', detail: '硬前提就位' },
  { key: 'vbs', label: '基于虚拟化的安全性 (VBS)', state: 'info', value: '运行中', detail: '兼容' },
  { key: 'store', label: 'Microsoft Store', state: 'ok', value: '已安装', detail: '正常' },
  { key: 'form', label: '安装形态 (MSIX/MSI/启动器)', state: 'ok', value: 'MSIX 2.7.13.0 · MSI 2.7.13.0', detail: '双形态共存' },
]

const REPORT = {
  verdict: 'attention',
  verdictTitle: '硬件与系统满足，可以安装 WSL2——但有注意项',
  verdictDetail: 'GitHub API 被拦截——正是 wsl --install 报「已禁止(403)」的根因',
  checks: [
    ...SYSTEM_ITEMS,
    { key: 'net', label: 'GitHub 安装通道', state: 'warn', value: 'API 403', detail: '换网络或离线 MSI' },
    { key: 'wsl', label: 'WSL 本体', state: 'ok', value: '2.7.13', detail: '已安装' },
  ],
  wslVersion: '2.7.13',
  vmPlatformEnabled: true,
  machineArch: 'x64',
  distros: [{ name: 'Ubuntu', default: true, state: '正在运行', version: '2' }],
  collectedAt: '2026-09-06 16:00:00',
}

const OVERVIEW = {
  releases: [
    {
      tag: '2.9.10', published: '2026-08-01', prerelease: false,
      pageUrl: 'https://github.com/microsoft/WSL/releases/tag/2.9.10',
      assets: [
        { platform: 'x64', name: 'wsl.2.9.10.0.x64.msi', size: 18_000_000, url: 'https://gh/x64.msi' },
        { platform: 'ARM64', name: 'wsl.2.9.10.0.arm64.msi', size: 15_000_000, url: 'https://gh/arm64.msi' },
      ],
    },
  ],
  latest: '2.9.10', localVersion: '2.7.13', relation: 'update',
  relationDetail: '本机 2.7.13 落后于最新正式版 2.9.10，建议更新',
  isStale: false, fetchedAt: '2026-09-06 16:00:01',
}

const ONLINE = [
  { id: 'Ubuntu-24.04', label: 'Ubuntu 24.04 LTS' },
  { id: 'Debian', label: 'Debian GNU/Linux' },
]

function emitReadiness(payload: unknown) {
  runtime.handlers['wsl:readiness']?.({ data: payload })
}

// 实例管理用发行版数据（状态原文故意本地化：前端必须归一呈现）
const INSTANCES = [
  { name: 'Ubuntu', running: true, default: true, version: '2', stateText: '正在运行', basePath: 'C:\\lxss\\u', vhdxPath: 'C:\\lxss\\u\\ext4.vhdx', sizeBytes: 12.3 * 1024 * 1024 },
  { name: 'Debian', running: false, default: false, version: '1', stateText: '已停止', basePath: '', vhdxPath: '', sizeBytes: 0 },
]

function ok(message = '操作已完成') {
  return { success: true, message }
}

type InstallPref = { set: boolean; dir: string }

// setup：默认"从未设置"（set=false）→ 输入框回落 D:\wsl（W1 语义）。
// installPref 传 {set,dir} 定制后端返回值；传 'throw' 模拟后端拉取失败（静默降级）。
async function setup(opts: { installPref?: InstallPref | 'throw' } = {}) {
  api.StartReadiness.mockResolvedValue(undefined)
  api.GetReleases.mockResolvedValue(OVERVIEW)
  api.ListOnlineDistros.mockResolvedValue(ONLINE)
  api.SetDefaultVersion2.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.InstallDistro.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.InstallDistroTo.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.ImportDistro.mockResolvedValue(ok('导入完成'))
  api.ImportDistroVhd.mockResolvedValue(ok('挂载完成'))
  api.PickDistroImageDialog.mockResolvedValue('')
  api.PickFolderDialog.mockResolvedValue('')
  api.UninstallWsl.mockResolvedValue({ success: true, message: '双路卸载完成' })
  api.DownloadMsi.mockResolvedValue({ success: true, message: '开始下载，保存到系统下载文件夹' })
  api.RevealDownload.mockResolvedValue(undefined)
  api.ListInstances.mockResolvedValue(INSTANCES)
  api.ListDistroExports.mockResolvedValue([])
  api.ListPortRules.mockResolvedValue({ rules: [], foreign: [], pending: false })
  api.GetUsbOverview.mockResolvedValue({ installed: false, devices: [], ledger: [], autoEnabled: false, releasesPage: 'https://github.com/dorssel/usbipd-win/releases' })
  if (opts.installPref === 'throw') {
    api.GetDistroInstallDir.mockRejectedValue(new Error('RPC 通道掉线'))
  } else {
    api.GetDistroInstallDir.mockResolvedValue(opts.installPref ?? { set: false, dir: '' })
  }
  api.SetDistroInstallDir.mockResolvedValue(undefined)
  const wrapper = mount(WSLView)
  await flushPromises()
  return wrapper
}

async function completeCheck(wrapper: ReturnType<typeof mount>) {
  emitReadiness({ stage: 'wsl', items: [REPORT.checks[9]] })
  emitReadiness({ stage: 'net', items: [REPORT.checks[8]] })
  emitReadiness({ stage: 'system', items: SYSTEM_ITEMS })
  emitReadiness({ stage: 'done', report: REPORT })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
  confirmFn.mockResolvedValue(true)
})

afterEach(() => {
  vi.useRealTimers()
  // Teleport 面板挂 body（wrapper 外）：用例间清场，防残留菜单被后案 querySelector 误命中。
  document.body.innerHTML = ''
})

// 行内常驻钮顺序（W8 收纳后）：0 终端 1 重启 2 关机 3 删除 4 ⋯更多；
// 「⋯ 更多」面板钮：文件 / 设默认 / 导出 / 迁移 / 克隆 / 瘦身 / 详情 / wsl.conf。
// N40 遮挡整改后面板 Teleport 直挂 body（行内 absolute 被表格滚动容器裁剪），
// 面板断言一律 document.body 直查、点击走原生 click（同 WslUsbPanel.spec 语系）。
const rowBtns = (w: Awaited<ReturnType<typeof setup>>, row: number) =>
  w.findAll('tbody tr')[row].findAll('button')
async function openMore(w: Awaited<ReturnType<typeof setup>>, row: number) {
  await rowBtns(w, row)[4].trigger('click')
  await flushPromises()
  return document.body.querySelector<HTMLElement>('.row-menu-panel')
}
async function menuClick(w: Awaited<ReturnType<typeof setup>>, row: number, text: string) {
  const panel = await openMore(w, row)
  expect(panel).not.toBeNull()
  const btn = Array.from(panel!.querySelectorAll<HTMLButtonElement>('button'))
    .find(b => b.textContent?.includes(text))!
  btn.click()
  await flushPromises()
}

describe('WSLView 流式体检', () => {
  it('挂载即启动流式体检 + 后端拉取落位偏好：骨架 10 项先以「检测中」渲染', async () => {
    const w = await setup()
    expect(api.StartReadiness).toHaveBeenCalledTimes(1)
    expect(api.GetDistroInstallDir).toHaveBeenCalledTimes(1)
    const rows = w.findAll('.check-row')
    expect(rows.length).toBe(10)
    expect(w.text().match(/检测中/g)?.length).toBe(10)
    // done 之前不出结论条
    expect(w.text()).not.toContain(REPORT.verdictTitle)
  })

  it('分相到达逐项点亮，done 收口结论与发行版表', async () => {
    const w = await setup()
    emitReadiness({ stage: 'wsl', items: [REPORT.checks[9]] })
    await flushPromises()
    expect(w.text()).toContain('检测中') // 其余 9 项仍 pending
    expect(w.findAll('.check-row.pending').length).toBe(9)
    expect(w.text()).not.toContain('Build 26200.9168') // system 阶段未到，探针项不得抢跑

    await completeCheck(w)
    expect(w.findAll('.check-row.pending').length).toBe(0)
    expect(w.text()).toContain(REPORT.verdictTitle)
    // 完成落章：✓ 通过 8、⚠ 注意 1、ℹ 已检测不判定 1（vbs）——info 不再与 pass 共用灰勾
    expect(w.findAll('.check-trail.ok').length).toBe(8)
    expect(w.findAll('.check-trail.warn').length).toBe(1)
    expect(w.findAll('.check-trail.info').length).toBe(1)
    expect(w.findAll('.check-trail.info')[0].text()).toBe('ℹ')
    expect(w.findAll('.check-trail').length).toBe(10)
    expect(w.text()).toContain('★ 默认')
    // 状态归一呈现：本地化原文只进 title 备查，正文一律中文枚举；占用走注册表列
    expect(w.text()).toContain('运行中')
    expect(w.text()).toContain('已停止')
    expect(w.text()).toContain('12.3 MB')
  })

  it('复采保旧值（Stale 基线，W9）：重新体检不清屏，标题区标注复采中', async () => {
    const w = await setup()
    await completeCheck(w)
    api.StartReadiness.mockImplementationOnce(() => new Promise(() => {})) // 永挂起：停在 streaming 态
    await w.findAll('button').find(b => b.text().includes('重新体检'))!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('复采中')
    expect(w.text()).toContain(REPORT.verdictTitle) // 旧结论在位可读，不整页闪回骨架
    expect(w.findAll('.check-row.pending').length).toBe(0)
  })

  it('error 阶段落错误框并解除 streaming', async () => {
    const w = await setup()
    emitReadiness({ stage: 'error', error: 'WSL 就绪探针执行失败：PowerShell 被拦截' })
    await flushPromises()
    expect(w.find('.error-box').text()).toContain('PowerShell 被拦截')
  })
})

describe('WSLView 标签页布局（页签 6→5→6：F9 追加「🔌 USB 直通」）', () => {
  // 顺序锁定：懒加载断言全部按索引导航，插页或换序会连锁打破它们。
  it('六页顺序锁定：就绪检测 → 本机发行版 → 添加实例 → 本体版本 → 端口转发 → USB 直通', async () => {
    const w = await setup()
    expect(w.findAll('.main-tab-btn').map(b => b.text())).toEqual([
      '🐧 就绪检测', '💻 本机发行版', '➕ 添加实例', '🧩 本体版本', '🔀 端口转发', '🔌 USB 直通',
    ])
    // 「📦 官方发行版」独立页签已删（与添加实例·商店源同清单双入口归一）
    expect(w.text()).not.toContain('📦 官方发行版')
  })

  it('页签 aria 接线：id-prefix 生成 tab/panel 配对（W9）', async () => {
    const w = await setup()
    expect(w.find('.main-tab-btn').attributes('id')).toBe('wsl-main-console-tab')
    const panel = w.find('#wsl-main-console-panel')
    expect(panel.exists()).toBe(true)
    expect(panel.attributes('role')).toBe('tabpanel')
    expect(panel.attributes('aria-labelledby')).toBe('wsl-main-console-tab')
  })

  it('未装 WSL 且无实例时，本机发行版页给引导空态而非管理表', async () => {
    const w = await setup()
    // setup 的默认 mock 挂载后才生效，置空须发生在挂载后并复采一次
    api.ListInstances.mockResolvedValue([])
    await w.findAll('button').find(b => b.text().includes('刷新列表'))!.trigger('click')
    await flushPromises()
    emitReadiness({ stage: 'done', report: { ...REPORT, wslVersion: '' } })
    await flushPromises()
    // 页签是 v-show 常驻 DOM：.distro-head 也是添加实例页官方清单的表头，
    // 断言必须限定在本机发行版面板内，否则扫到邻页表头误报。
    expect(w.find('#wsl-main-distros-panel .distro-head').exists()).toBe(false)
    expect(w.text()).toContain('装好发行版后实例会列在这里')
  })
})

describe('WSLView 提权操作与确认链', () => {
  it('提权操作：确认 → 白名单命令 → 完成后重新流式体检', async () => {
    const w = await setup()
    await completeCheck(w)
    const btn = w.findAll('button').find(b => b.text().includes('默认WSL2'))!
    await btn.trigger('click')
    await flushPromises()
    expect(confirmFn).toHaveBeenCalledTimes(1)
    expect(api.SetDefaultVersion2).toHaveBeenCalledTimes(1)
    expect(api.StartReadiness).toHaveBeenCalledTimes(2) // 初始 + 操作后复查
  })

  it('提权操作失败也复采（W2-3）：catch 补 loadInstances，半成功不隐身', async () => {
    api.InstallWsl.mockRejectedValueOnce(new Error('UAC 被取消'))
    const w = await setup()
    await completeCheck(w)
    const before = api.ListInstances.mock.calls.length
    await w.findAll('button').find(b => b.text().includes('一键开启'))!.trigger('click')
    await flushPromises()
    expect(api.ListInstances.mock.calls.length).toBeGreaterThan(before)
    // 失败已放闸：按钮恢复可用，且文案不换字（宽度稳定）
    const install = w.findAll('button').find(b => b.text().includes('一键开启'))!
    expect(install.text()).toContain('🚀 一键开启')
    expect(install.attributes('disabled')).toBeUndefined()
  })

  it('UAC 前置确认被拒时不触达提权通道', async () => {
    confirmFn.mockResolvedValueOnce(false)
    const w = await setup()
    await completeCheck(w)
    const btn = w.findAll('button').find(b => b.text().includes('一键开启'))!
    await btn.trigger('click')
    await flushPromises()
    expect(api.InstallWsl).not.toHaveBeenCalled()
    expect(w.text()).not.toContain('待重启生效')
  })

  it('重启引导纯检测驱动：CBS 台账报 pending 才出现，与操作语境无关', async () => {
    const w = await setup()
    await completeCheck(w) // REPORT 无 rebootPending：即使刚做过体检也不打扰
    expect(w.text()).not.toContain('待重启生效')
    // 系统真挂了账：done 报告带 rebootPending → 引导条出现
    emitReadiness({ stage: 'done', report: { ...REPORT, rebootPending: true } })
    await flushPromises()
    expect(w.text()).toContain('待重启生效')
  })

  it('重启引导无常驻按钮（已收编进按需引导条）', async () => {
    const w = await setup()
    await completeCheck(w)
    expect(w.findAll('button').some(b => b.text().includes('稍后重启'))).toBe(false)
  })

  it('虚拟机平台开关按钮状态驱动：已启用见「关闭」，未启用见「开启」且各走各的命令', async () => {
    api.EnableWslFeatures.mockResolvedValue({ success: true, message: '启用指令已执行' })
    const w = await setup()
    await completeCheck(w) // REPORT.vmPlatformEnabled = true
    expect(w.findAll('button').some(b => b.text().includes('关闭虚拟机平台'))).toBe(true)
    expect(w.findAll('button').some(b => b.text().includes('开启虚拟机平台'))).toBe(false)

    // 换一份"平台已禁用"的报告投喂：按钮翻转为开启，点击走 EnableWslFeatures
    api.StartReadiness.mockImplementationOnce(async () => {
      emitReadiness({ stage: 'done', report: { ...REPORT, vmPlatformEnabled: false } })
    })
    await w.findAll('button').find(b => b.text().includes('重新体检'))!.trigger('click')
    await flushPromises()
    const open = w.findAll('button').find(b => b.text().includes('开启虚拟机平台'))!
    expect(open).toBeTruthy()
    await open.trigger('click')
    await flushPromises()
    expect(api.EnableWslFeatures).toHaveBeenCalledTimes(1)
    expect(api.DisableWslFeatures).not.toHaveBeenCalled()
  })

  it('正规卸载：danger 确认 + 专用钮文案「继续卸载」+ 双路卸载调用', async () => {
    const w = await setup()
    await completeCheck(w)
    const btn = w.findAll('button').find(b => b.text().includes('卸载 WSL'))!
    await btn.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts | undefined
    expect(opts?.tone).toBe('danger')
    expect(opts?.confirmLabel).toBe('继续卸载')
    expect(api.UninstallWsl).toHaveBeenCalledTimes(1)
    expect(api.StartReadiness).toHaveBeenCalledTimes(2) // 卸载后自动流式复查
  })

  it('危险级降噪（W7）：关虚拟机平台保持 danger 且钮文案「仍要关闭」', async () => {
    api.DisableWslFeatures.mockResolvedValue({ success: true, message: '已关闭' })
    const w = await setup()
    await completeCheck(w)
    await w.findAll('button').find(b => b.text().includes('关闭虚拟机平台'))!.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('danger')
    expect(opts.confirmLabel).toBe('仍要关闭')
  })

  it('忙时按钮不换字防宽度跳动（W9）：在飞按钮保留原文案并挂 spinner', async () => {
    let release!: (v: unknown) => void
    api.InstallWsl.mockImplementation(() => new Promise(res => { release = res }))
    const w = await setup()
    await completeCheck(w)
    await w.findAll('button').find(b => b.text().includes('一键开启'))!.trigger('click')
    await flushPromises()
    const install = w.findAll('button').find(b => b.text().includes('一键开启'))!
    expect(install.text()).toContain('🚀 一键开启') // 文案未被"执行中…"替换
    expect(install.find('.btn-spin').exists()).toBe(true) // 以 spinner 表忙
    release({ success: true, message: '已安装' })
    await flushPromises()
  })
})

describe('WSLView busy 分级锁与跨页签进度（P0，W2）', () => {
  it('克隆在飞：只锁本行写操作与全局钮，放行只读与其他发行版；全页签横幅 + 页签·运行中', async () => {
    let release!: (v: unknown) => void
    api.CloneDistro.mockImplementation(() => new Promise(res => { release = res }))
    const w = await setup()
    await completeCheck(w)
    await menuClick(w, 0, '克隆')
    // 展开即预填新名与落位：直接提交
    await w.find('.clone-row-editor').findAll('button').find(b => b.text().includes('开始克隆'))!.trigger('click')
    await flushPromises()
    // 行结构：tr[0]=Ubuntu tr[1]=克隆行内编辑器 tr[2]=Debian
    expect(rowBtns(w, 0)[1].attributes('disabled')).toBeDefined() // Ubuntu 重启关门
    expect(rowBtns(w, 0)[3].attributes('disabled')).toBeDefined() // Ubuntu 删除关门
    expect(rowBtns(w, 2)[0].attributes('disabled')).toBeUndefined() // Debian 终端照常（只读）
    expect(rowBtns(w, 2)[3].attributes('disabled')).toBeUndefined() // Debian 删除照常（他行不连坐）
    const install = w.findAll('button').find(b => b.text().includes('一键开启'))!
    expect(install.attributes('disabled')).toBeDefined() // 全局互斥类在别操作在飞时关门
    // 页签标题追加运行中
    expect(w.findAll('.main-tab-btn')[1].text()).toContain('·运行中')
    // 常驻横幅：克隆免 UAC，文案不得谎称提权窗口
    const banner = w.find('#wsl-main-console-panel .busy-banner')
    expect(banner.text()).toContain('克隆 Ubuntu → Ubuntu-Copy：拷贝数据盘中')
    expect(banner.text()).not.toContain('提权窗口')
    // 切到添加实例页：横幅仍在（跨页签可见）
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    expect(w.find('#wsl-main-add-panel .busy-banner').text()).toContain('克隆 Ubuntu')
    // Debian 终端此刻真的可点并触达后端
    api.OpenTerminal.mockResolvedValue(ok('已启动终端会话'))
    await rowBtns(w, 2)[0].trigger('click')
    await flushPromises()
    expect(api.OpenTerminal).toHaveBeenCalledWith('Debian')
    // 受理返回（事件尚未终态）：闸门仍由事件收口
    release({ success: true, message: '已开始克隆' })
    await flushPromises()
    expect(w.findAll('.main-tab-btn')[1].text()).toContain('·运行中')
    // done 终态：放闸、收表单、标记消失、横幅退场
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'done', done: 1000, total: 1000, message: '已克隆为 Ubuntu-Copy' } })
    await flushPromises()
    expect(w.findAll('.main-tab-btn')[1].text()).not.toContain('·运行中')
    expect(w.findAll('.busy-banner').every(b => !b.isVisible())).toBe(true)
    // 回发行版页：克隆编辑器已收，Ubuntu 行恢复可用
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    expect(w.find('.clone-row-editor').exists()).toBe(false)
    expect(rowBtns(w, 0)[1].attributes('disabled')).toBeUndefined()
  })

  it('提权类在飞：横幅如实点名提权窗口，全站写操作关门但只读放行', async () => {
    let release!: (v: unknown) => void
    api.UpdateWsl.mockImplementation(() => new Promise(res => { release = res }))
    const w = await setup()
    await completeCheck(w)
    await w.findAll('button').find(b => b.text().includes('🔄 更新'))!.trigger('click')
    await flushPromises()
    // 全局在飞：连 Debian 的写钮也关门（tr[1]），终端只读不禁
    expect(rowBtns(w, 1)[3].attributes('disabled')).toBeDefined()
    expect(rowBtns(w, 1)[0].attributes('disabled')).toBeUndefined()
    expect(w.find('.busy-banner').text()).toContain('提权窗口')
    release({ success: true, message: '更新完成' })
    await flushPromises()
    expect(rowBtns(w, 1)[3].attributes('disabled')).toBeUndefined()
  })

  it('只读刷新永远可用：克隆在飞时列表刷新钮不灰', async () => {
    let release!: (v: unknown) => void
    api.CloneDistro.mockImplementation(() => new Promise(res => { release = res }))
    const w = await setup()
    await completeCheck(w)
    await menuClick(w, 0, '克隆')
    await w.find('.clone-row-editor').findAll('button').find(b => b.text().includes('开始克隆'))!.trigger('click')
    await flushPromises()
    const refresh = w.findAll('button').find(b => b.text().includes('刷新列表'))!
    expect(refresh.attributes('disabled')).toBeUndefined()
    release({ success: true, message: '已开始克隆' })
    await flushPromises()
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'done', done: 1000, total: 1000 } })
    await flushPromises()
  })
})

describe('WSLView 失败回报行内保活（P0，W3）', () => {
  it('克隆提交失败：错误驻留本行红条（不再秒清表单），闸门即放', async () => {
    api.CloneDistro.mockRejectedValue(new Error('目标盘所在卷空间不足'))
    const w = await setup()
    await completeCheck(w)
    await menuClick(w, 0, '克隆')
    await w.find('.clone-row-editor').findAll('button').find(b => b.text().includes('开始克隆'))!.trigger('click')
    await flushPromises()
    const editor = w.find('.clone-row-editor')
    expect(editor.exists()).toBe(true) // 旧实现置 null 直接吞掉表单——现在驻留
    expect(editor.text()).toContain('克隆失败')
    expect(editor.find('.error-box').text()).toContain('目标盘所在卷空间不足')
    expect(w.findAll('.main-tab-btn')[1].text()).not.toContain('·运行中') // 闸门已放
    // 「知道了，收起」关表单
    await editor.find('.error-box').findAll('button')[0].trigger('click')
    await flushPromises()
    expect(w.find('.clone-row-editor').exists()).toBe(false)
  })

  it('瘦身未受理：错误驻留本行红条，终态可收起', async () => {
    api.CompactDistro.mockRejectedValue(new Error('备份目录不可写'))
    const w = await setup()
    await completeCheck(w)
    await menuClick(w, 0, '瘦身')
    await w.find('.compact-row-editor').findAll('button').find(b => b.text().includes('确认开始瘦身'))!.trigger('click')
    await flushPromises()
    const editor = w.find('.compact-row-editor')
    expect(editor.exists()).toBe(true)
    expect(editor.text()).toContain('瘦身中止')
    expect(editor.find('.error-box').text()).toContain('备份目录不可写')
    await editor.findAll('button').find(b => b.text().includes('收起'))!.trigger('click')
    await flushPromises()
    expect(w.find('.compact-row-editor').exists()).toBe(false)
  })

  it('克隆事件流 error 也在行内驻留（回归锁）', async () => {
    api.CloneDistro.mockResolvedValue({ success: true, message: '开始克隆' })
    const w = await setup()
    await completeCheck(w)
    await menuClick(w, 0, '克隆')
    await w.find('.clone-row-editor').findAll('button').find(b => b.text().includes('开始克隆'))!.trigger('click')
    await flushPromises()
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'error', done: 0, total: 0, error: '挂载克隆盘失败（已拷贝的数据盘保留在 D:\\x，可自行处理）' } })
    await flushPromises()
    const box = w.find('.clone-row-editor .error-box')
    expect(box.exists()).toBe(true)
    expect(box.text()).toContain('保留在 D:\\x')
    expect(rowBtns(w, 2)[3].attributes('disabled')).toBeUndefined() // error 即放闸（编辑行占 tr[1]）
    await box.find('button').trigger('click')
    await flushPromises()
    expect(w.find('.clone-row-editor').exists()).toBe(false)
  })
})

const dirInput = (w: Awaited<ReturnType<typeof setup>>) =>
  (w.find('#wsl-install-dir').element as HTMLInputElement).value

describe('WSLView 安装落位持久化：后端 RPC（W1）', () => {
  it('set=false（从未设置）→ 回落默认 D:\\wsl；用户改动防抖 500ms 后写回', async () => {
    vi.useFakeTimers()
    const w = await setup({ installPref: { set: false, dir: '' } })
    expect(dirInput(w)).toBe('D:\\wsl')
    expect(api.SetDistroInstallDir).not.toHaveBeenCalled() // 拉取回显不触发回写
    await w.find('#wsl-install-dir').setValue('E:\\WSL')
    await vi.advanceTimersByTimeAsync(499)
    expect(api.SetDistroInstallDir).not.toHaveBeenCalled() // 防抖未到期
    await vi.advanceTimersByTimeAsync(2)
    expect(api.SetDistroInstallDir).toHaveBeenCalledTimes(1)
    expect(api.SetDistroInstallDir).toHaveBeenCalledWith('E:\\WSL')
    vi.useRealTimers()
  })

  it('set=true 且 dir=""（显式选系统默认）→ 输入框留空 + 常驻警示条（W5）', async () => {
    const w = await setup({ installPref: { set: true, dir: '' } })
    expect(dirInput(w)).toBe('')
    expect(api.SetDistroInstallDir).not.toHaveBeenCalled()
    const warn = w.findAll('.banner-warn').find(b => b.text().includes('留空 = 用系统默认位置'))
    expect(warn).toBeTruthy()
    expect(warn!.text()).toContain('C 盘')
    // 输入框不再顶着过时长按说明：placeholder 已瘦身
    expect(w.find('#wsl-install-dir').attributes('placeholder')).toBe('例：D:\\wsl')
    // 标签更名"安装基目录"
    expect(w.find('label[for="wsl-install-dir"]').text()).toBe('安装基目录')
  })

  it('set=true 且有值 → 原样回填；防抖窗口内改回原值不写回', async () => {
    vi.useFakeTimers()
    const w = await setup({ installPref: { set: true, dir: 'E:\\WSL' } })
    expect(dirInput(w)).toBe('E:\\WSL')
    await w.find('#wsl-install-dir').setValue('X:\\tmp')
    await vi.advanceTimersByTimeAsync(100)
    await w.find('#wsl-install-dir').setValue('E:\\WSL')
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.SetDistroInstallDir).not.toHaveBeenCalled() // 净变化为零：不打扰后端
    vi.useRealTimers()
  })

  it('后端拉取失败 → 静默降级默认值（console.warn 留痕，不拦渲染）', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const w = await setup({ installPref: 'throw' })
    expect(dirInput(w)).toBe('D:\\wsl')
    expect(warnSpy).toHaveBeenCalled()
    warnSpy.mockRestore()
  })

  it('写回失败静默降级：仅 console.warn，不拦后续操作', async () => {
    vi.useFakeTimers()
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const w = await setup()
    api.SetDistroInstallDir.mockRejectedValue(new Error('RPC 掉线'))
    await w.find('#wsl-install-dir').setValue('F:\\wsl')
    await vi.advanceTimersByTimeAsync(600)
    expect(warnSpy).toHaveBeenCalled()
    expect(dirInput(w)).toBe('F:\\wsl') // 内存值保留，本会话继续可用
    warnSpy.mockRestore()
    vi.useRealTimers()
  })
})

describe('WSLView 官方发行版清单并入添加实例·商店源（W4）', () => {
  it('懒加载：冷页不查；首次进添加实例页即查且共用清单不重查', async () => {
    const w = await setup()
    expect(api.ListOnlineDistros).not.toHaveBeenCalled()
    await w.findAll('.main-tab-btn')[2].trigger('click') // ➕ 添加实例（商店源默认选中）
    await flushPromises()
    expect(api.ListOnlineDistros).toHaveBeenCalledTimes(1)
    await w.findAll('.main-tab-btn')[0].trigger('click')
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    expect(api.ListOnlineDistros).toHaveBeenCalledTimes(1) // 已载过不重查
  })

  it('商店源面板即清单表格：下拉+单钮双入口已删，逐行「⬇ 安装」携基目录走 InstallDistroTo', async () => {
    const w = await setup()
    await completeCheck(w) // vmPlatformEnabled=true、无 rebootPending → 放行
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    expect(w.find('#wsl-add-store').exists()).toBe(false) // 原下拉入口删除
    expect(w.text()).not.toContain('安装所选发行版')
    expect(w.text()).toContain('可安装的官方发行版 (2)')
    expect(w.text()).toContain('Ubuntu-24.04')
    expect(w.text()).toContain('Debian')
    // 落位回显（三源共用基目录行）
    expect(w.text()).toContain('安装落位：D:\\wsl')
    const install = w.findAll('button').find(b => b.text().includes('⬇ 安装'))!
    expect(install.attributes('disabled')).toBeUndefined()
    await install.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.description).toContain('自动迁移落位')
    expect(api.InstallDistroTo).toHaveBeenCalledWith('Ubuntu-24.04', 'D:\\wsl')
  })

  it('清单空态自带「↻ 重新查询」（P1-1：修复空态指向不存在的刷新钮）', async () => {
    const w = await setup()
    api.ListOnlineDistros.mockResolvedValue([])
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    expect(api.ListOnlineDistros).toHaveBeenCalledTimes(1)
    const empty = w.find('.empty-state')
    expect(empty.exists()).toBe(true)
    const retry = empty.findAll('button').find(b => b.text().includes('重新查询'))
    expect(retry).toBeTruthy()
    expect(empty.text()).not.toContain('点右侧按钮')
    await retry!.trigger('click')
    await flushPromises()
    expect(api.ListOnlineDistros).toHaveBeenCalledTimes(2) // 空态内即可自救
  })

  it('虚拟机平台未生效时预告拦截：黄条警告 + 行内安装钮禁用', async () => {
    const w = await setup()
    emitReadiness({ stage: 'done', report: { ...REPORT, rebootPending: true } })
    await flushPromises()
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    const banner = w.find('.distro-block-banner')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('重启')
    const install = w.findAll('button').find(b => b.text().includes('⬇ 安装'))!
    expect(install.attributes('disabled')).toBeDefined()
    await install.trigger('click')
    expect(api.InstallDistroTo).not.toHaveBeenCalled()
  })
})

describe('WSLView 发行版实例管理', () => {
  async function consoleMounted() {
    const w = await setup()
    await completeCheck(w)
    return w
  }
  // 覆盖列表数据源并复采（setup 默认 mock 会重置，故覆盖必须发生在挂载后）
  async function reloadWith(w: Awaited<ReturnType<typeof setup>>, list: unknown[]) {
    api.ListInstances.mockResolvedValue(list)
    await w.findAll('button').find(b => b.text().includes('刷新列表'))!.trigger('click')
    await flushPromises()
  }

  it('唤终端免确认直达（只读操作不受分级锁），发行版名原样透传', async () => {
    api.OpenTerminal.mockResolvedValue(ok('已启动终端会话'))
    const w = await consoleMounted()
    await rowBtns(w, 0)[0].trigger('click')
    await flushPromises()
    expect(confirmFn).not.toHaveBeenCalled()
    expect(api.OpenTerminal).toHaveBeenCalledWith('Ubuntu')
  })

  it('文件管理器收进「更多」但仍是只读免确认直达', async () => {
    api.OpenDistroFolder.mockResolvedValue(ok('已在资源管理器打开 \\\\wsl$\\Ubuntu'))
    const w = await consoleMounted()
    await menuClick(w, 0, '文件')
    expect(confirmFn).not.toHaveBeenCalled()
    expect(api.OpenDistroFolder).toHaveBeenCalledWith('Ubuntu')
  })

  it('重启：运行中实例给 warning 确认（会话中断如实声明）并透传名', async () => {
    api.RestartDistro.mockResolvedValue(ok('Ubuntu 已重启'))
    const w = await consoleMounted()
    await rowBtns(w, 0)[1].trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('warning')
    expect(opts.description).toContain('会话会被中断')
    expect(opts.description).toContain('不做后台保活')
    expect(api.RestartDistro).toHaveBeenCalledWith('Ubuntu')
  })

  it('重启：停止态实例文案降级为直接拉起验证（default 基调）', async () => {
    api.RestartDistro.mockResolvedValue(ok('Debian 已重启'))
    const w = await consoleMounted()
    await rowBtns(w, 1)[1].trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('default')
    expect(opts.description).toContain('当前已停止')
    expect(api.RestartDistro).toHaveBeenCalledWith('Debian')
  })

  it('终止仅对运行中实例可用；用户态操作如实声明不弹 UAC', async () => {
    api.TerminateDistro.mockResolvedValue(ok('Ubuntu 已终止'))
    const w = await consoleMounted()
    const debianStop = rowBtns(w, 1)[2]
    expect(debianStop.attributes('disabled')).toBeDefined()
    await debianStop.trigger('click')
    expect(api.TerminateDistro).not.toHaveBeenCalled()

    await rowBtns(w, 0)[2].trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.description).toContain('不会弹出 UAC')
    expect(api.TerminateDistro).toHaveBeenCalledWith('Ubuntu')
  })

  it('删除：常驻危险钮 + danger 确认带专用钮文案；details 点名占用与位置；确认被拒不触达', async () => {
    api.UnregisterDistro.mockResolvedValue(ok('已删除'))
    const w = await consoleMounted()
    await rowBtns(w, 0)[3].trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('danger')
    expect(opts.confirmLabel).toBe('🗑 删除数据并移除')
    expect(opts.description).toContain('不可恢复')
    expect(opts.details?.find(d => d.label === '磁盘占用')?.value).toBe('12.3 MB')
    expect(opts.details?.find(d => d.label === '数据位置')?.value).toBe('C:\\lxss\\u')
    expect(api.UnregisterDistro).toHaveBeenCalledWith('Ubuntu')

    const w2 = await consoleMounted()
    confirmFn.mockResolvedValueOnce(false)
    await rowBtns(w2, 0)[3].trigger('click')
    await flushPromises()
    expect(api.UnregisterDistro).toHaveBeenCalledTimes(1) // 第二次被拒不得新增调用
  })

  it('导出两段式：更多里展开格式选择不进命令，开始导出透传 gzip 参数；成功自动展开记录抽屉', async () => {
    api.ExportDistro.mockResolvedValue({ ...ok('Ubuntu 已导出'), id: 'Ubuntu-20260914-120000.tar.gz' })
    api.RevealDistroExport.mockResolvedValue(undefined)
    const w = await consoleMounted()
    await menuClick(w, 0, '导出')
    // 第一段：只展开内联格式选择，未触达命令、未弹确认
    const editor = w.find('.export-row-editor')
    expect(editor.exists()).toBe(true)
    expect(api.ExportDistro).not.toHaveBeenCalled()
    expect(confirmFn).not.toHaveBeenCalled()
    await editor.findAll('input[type="radio"]')[1].setValue() // 切未压缩 tar
    api.ListDistroExports.mockResolvedValue([
      { id: 'Ubuntu-20260914-120000.tar.gz', name: 'Ubuntu', path: 'C:\\dl\\Ubuntu-20260914-120000.tar.gz', size: 1024, at: '2026-09-14 12:00:00' },
    ])
    await editor.findAll('button')[0].trigger('click') // ✔ 开始导出
    await flushPromises()
    expect(api.ExportDistro).toHaveBeenCalledWith('Ubuntu', false)
    await flushPromises()
    const log = w.find('.export-log')
    expect(log.exists()).toBe(true)
    expect(log.attributes('open')).toBeDefined() // W9：成功即自动展开
    expect(log.text()).toContain('Ubuntu-20260914-120000.tar.gz')
    await log.findAll('button').find(b => b.text().includes('打开位置'))!.trigger('click')
    await flushPromises()
    expect(api.RevealDistroExport).toHaveBeenCalledWith('Ubuntu-20260914-120000.tar.gz')
  })

  it('迁移：术语清洗（W6）+ warning 降级（W7）+ 留空置灰有解释（W5）+ 路径透传', async () => {
    api.MoveDistro.mockResolvedValue(ok('迁移完成'))
    const w = await consoleMounted()
    // Debian 改为运行中：迁移预警必须点名会被 --shutdown 连带打停的实例
    await reloadWith(w, [INSTANCES[0], { ...INSTANCES[1], running: true, stateText: '正在运行' }])
    await menuClick(w, 0, '迁移')
    const editor = w.find('.move-editor')
    expect(editor.exists()).toBe(true)
    expect(editor.text()).toContain('wsl --shutdown')
    expect(editor.text()).toContain('Debian')
    expect(editor.text()).not.toContain('瞬时冲突') // W6：内部重试策略不再进用户文案
    // 展开即预填「安装基目录\发行版名」（不再是空框靠占位符糊弄）
    expect((w.find('#wsl-move-target').element as HTMLInputElement).value).toBe('D:\\wsl\\Ubuntu')
    const submit = editor.findAll('button').find(b => b.text().includes('确认迁移'))!
    await w.find('#wsl-move-target').setValue('')
    expect(submit.attributes('disabled')).toBeDefined()
    expect(editor.text()).toContain('置灰') // W5：解释为什么灰
    await w.find('#wsl-move-target').setValue('  D:\\WSL\\Ubuntu  ')
    await submit.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('warning') // W7：danger 只留数据销毁级
    expect(opts.description).toContain('wsl --shutdown')
    expect(opts.description).not.toContain('瞬时冲突')
    expect(opts.details?.find(d => d.label === '目标目录')?.value).toBe('D:\\WSL\\Ubuntu')
    expect(api.MoveDistro).toHaveBeenCalledWith('Ubuntu', 'D:\\WSL\\Ubuntu')
    expect(w.find('.move-editor').exists()).toBe(false) // 完成后收起
  })

  it('分级锁（W2 取代旧全局互斥）：Ubuntu 导出在飞时 Debian 照常可用、本行关门', async () => {
    let release!: (v: ReturnType<typeof ok>) => void
    api.ExportDistro.mockImplementation(() => new Promise(res => { release = res }))
    const w = await consoleMounted()
    await menuClick(w, 0, '导出')
    await w.find('.export-row-editor').findAll('button')[0].trigger('click')
    await flushPromises()
    await flushPromises()
    // 导出编辑行占 tr[1]，Debian 在 tr[2]：终端/删除照常（只读永远放行、他行不连坐）
    expect(rowBtns(w, 2)[0].attributes('disabled')).toBeUndefined()
    expect(rowBtns(w, 2)[3].attributes('disabled')).toBeUndefined()
    // 本行（Ubuntu）写操作关门
    expect(rowBtns(w, 0)[3].attributes('disabled')).toBeDefined()
    release(ok('导出完成'))
    await flushPromises()
    await flushPromises()
    // 收口后编辑行收起，Debian 回到 tr[1]
    expect(w.find('.export-row-editor').exists()).toBe(false)
    expect(rowBtns(w, 1)[3].attributes('disabled')).toBeUndefined()
  })

  it('列表复采失败：错误框 + 重试钮，不影响体检区', async () => {
    const w = await consoleMounted()
    api.ListInstances.mockRejectedValue(new Error('wsl.exe 拒绝访问'))
    await w.findAll('button').find(b => b.text().includes('刷新列表'))!.trigger('click')
    await flushPromises()
    const errBox = w.find('.error-box')
    expect(errBox.exists()).toBe(true)
    expect(errBox.text()).toContain('wsl.exe 拒绝访问')
    expect(w.text()).toContain('GitHub API 被拦截') // 体检结论区不受牵连
    api.ListInstances.mockResolvedValue(INSTANCES)
    await w.findAll('button').find(b => b.text().includes('重试'))!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('运行中')
  })

  it('取证抽屉：收进更多、双口径渲染、复采重发、再点收起', async () => {
    api.GetDistroForensics.mockResolvedValue({
      name: 'Ubuntu', pfn: 'CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc',
      basePath: 'C:\\lxss\\u', vhdxPath: 'C:\\lxss\\u\\ext4.vhdx',
      logicalBytes: 62_914_560, allocBytes: 20_971_520, sparse: true,
      running: true, runtimeKnown: true,
      dfTotalMB: 50579, dfUsedMB: 13634, dfAvailMB: 37135, dfUsePct: 27, dfOk: true,
      ipv4: '172.25.4.136', notes: [],
    })
    const w = await consoleMounted()
    await menuClick(w, 0, '详情')
    expect(api.GetDistroForensics).toHaveBeenCalledWith('Ubuntu')
    const panel = w.find('.forensics-panel')
    expect(panel.text()).toContain('稀疏盘')
    expect(panel.text()).toContain('60.0 MB')
    expect(panel.text()).toContain('20.0 MB')
    expect(panel.text()).toContain('172.25.4.136')
    expect(panel.find('.ui-progress').exists()).toBe(true)
    api.GetDistroForensics.mockResolvedValue({
      name: 'Ubuntu', pfn: '', basePath: 'C:\\lxss\\u', vhdxPath: 'C:\\lxss\\u\\ext4.vhdx',
      logicalBytes: 62_914_560, allocBytes: 62_914_560, sparse: false,
      running: false, runtimeKnown: true,
      dfTotalMB: 0, dfUsedMB: 0, dfAvailMB: 0, dfUsePct: 0, dfOk: false,
      ipv4: '', notes: ['发行版未运行，guest 内用量与 IP 未探测（避免只读取证顺手启动它）'],
    })
    await w.find('.fx-foot button').trigger('click')
    await flushPromises()
    expect(api.GetDistroForensics).toHaveBeenCalledTimes(2)
    const panel2 = w.find('.forensics-panel')
    expect(panel2.text()).toContain('未运行')
    expect(panel2.find('.ui-progress').exists()).toBe(false)
    expect(panel2.text()).not.toContain('172.25')
    await menuClick(w, 0, '详情') // 再点收起
    expect(w.find('.forensics-panel').exists()).toBe(false)
  })

  it('克隆：白话确认文案（W6）→ 受理等 wsl:clone 终态；done 放闸关表单复采', async () => {
    api.CloneDistro.mockResolvedValue({ success: true, message: '开始克隆' })
    const w = await consoleMounted()
    await menuClick(w, 0, '克隆')
    const editor = w.find('.clone-row-editor')
    expect(editor.exists()).toBe(true)
    expect(api.CloneDistro).not.toHaveBeenCalled()
    expect((w.find('#wsl-clone-name').element as HTMLInputElement).value).toBe('Ubuntu-Copy')
    expect((w.find('#wsl-clone-target').element as HTMLInputElement).value).toBe('D:\\wsl\\Ubuntu-Copy')
    const submit = editor.findAll('button').find(b => b.text().includes('开始克隆'))!
    await w.find('#wsl-clone-target').setValue('')
    expect(submit.attributes('disabled')).toBeDefined()
    expect(editor.text()).toContain('置灰') // W5：留空致灰有解释
    await w.find('#wsl-clone-target').setValue('D:\\WSL\\Ubuntu-Copy')
    await submit.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.description).toContain('克隆会先关机「Ubuntu」')
    expect(opts.description).toContain('原发行版不受影响')
    expect(opts.description).toContain('需要 WSL 2.7.3 以上')
    expect(opts.description).toContain('导出 → 导入')
    expect(opts.description).not.toContain('import-in-place') // 命令细节撤出确认框
    expect(opts.details?.find(d => d.label === '目标目录')?.value).toBe('D:\\WSL\\Ubuntu-Copy')
    expect(api.CloneDistro).toHaveBeenCalledWith('Ubuntu', 'Ubuntu-Copy', 'D:\\WSL\\Ubuntu-Copy')
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'copying', done: 500, total: 1000 } })
    await flushPromises()
    expect(w.find('.clone-row-editor').text()).toContain('拷贝数据盘中')
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'done', done: 1000, total: 1000, message: '已克隆为 Ubuntu-Copy' } })
    await flushPromises()
    expect(w.find('.clone-row-editor').exists()).toBe(false)
    expect(rowBtns(w, 1)[3].attributes('disabled')).toBeUndefined() // Debian 回 tr[1] 且可用
  })

  it('目录输入体验：克隆/迁移展开即预填「安装基目录\\实例名」，📁 回填后仍可改', async () => {
    const w = await consoleMounted()
    await menuClick(w, 0, '克隆')
    expect((w.find('#wsl-clone-name').element as HTMLInputElement).value).toBe('Ubuntu-Copy')
    expect((w.find('#wsl-clone-target').element as HTMLInputElement).value).toBe('D:\\wsl\\Ubuntu-Copy')
    api.PickFolderDialog.mockResolvedValueOnce('E:\\Pool')
    await w.find('.clone-row-editor').findAll('button').find(b => b.text().includes('📁 选目录'))!.trigger('click')
    await flushPromises()
    expect(api.PickFolderDialog).toHaveBeenCalledTimes(1)
    expect((w.find('#wsl-clone-target').element as HTMLInputElement).value).toBe('E:\\Pool')
    await w.find('#wsl-clone-target').setValue('E:\\Pool\\deep')
    expect((w.find('#wsl-clone-target').element as HTMLInputElement).value).toBe('E:\\Pool\\deep')
    await w.find('.clone-row-editor').findAll('button').find(b => b.text().includes('取消'))!.trigger('click')
    await flushPromises()
    await menuClick(w, 0, '迁移')
    expect((w.find('#wsl-move-target').element as HTMLInputElement).value).toBe('D:\\wsl\\Ubuntu')
  })

  it('wsl.conf 编辑器：更多里打开、装载回填文本与版本警示，保存按当前文本透传', async () => {
    api.GetWslConf.mockResolvedValue({
      name: 'Ubuntu', text: '[boot]\nsystemd=true', missing: false, wslVersion: '2.7.13',
      warnings: ['[boot]：systemd / command 等 [boot] 项要求 WSL 2.4.4+（老版本会静默忽略）'],
    })
    api.SaveWslConf.mockResolvedValue(ok('已写入并复验一致'))
    const w = await consoleMounted()
    await menuClick(w, 0, 'wsl.conf')
    await flushPromises()
    expect(api.GetWslConf).toHaveBeenCalledWith('Ubuntu')
    const editor = w.find('.conf-row-editor')
    expect(editor.exists()).toBe(true)
    expect(editor.find('textarea').element.value).toBe('[boot]\nsystemd=true')
    expect(editor.text()).toContain('2.4.4')
    await editor.find('textarea').setValue('[boot]\nsystemd=false')
    // conf 编辑器换装 UiClipboardField 后按钮序前多了"粘贴"钮，改按文案定位保存钮
    await editor.findAll('button').find(b => b.text().includes('保存写回'))!.trigger('click')
    await flushPromises()
    expect(api.SaveWslConf).toHaveBeenCalledWith('Ubuntu', '[boot]\nsystemd=false')
    expect(confirmFn).toHaveBeenCalledTimes(2) // 保存 + 生效引导
    expect(confirmFn.mock.calls[1][0].title).toContain('生效')
    expect(api.TerminateDistro).toHaveBeenCalledWith('Ubuntu')
  })

  it('瘦身：四步白话确认（W6）→ 阶段事件呈现去黑话，done 省量对比并放闸、备份工件自动可见', async () => {
    api.CompactDistro.mockResolvedValue({ success: true, message: '开始瘦身' })
    const w = await consoleMounted()
    await menuClick(w, 0, '瘦身')
    expect(w.find('.compact-row-editor').text()).toContain('强制先全量备份')
    expect(w.find('.compact-row-editor').text()).not.toContain('Tier') // 表单说明条也不见 Tier 黑话
    await w.find('.compact-row-editor').findAll('button').find(b => b.text().includes('确认开始瘦身'))!.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.description).toContain('① 先强制做一份全量 tar 备份')
    expect(opts.description).toContain('④ 仍省得不够时，注销旧实例、从刚才的备份重建一个新实例')
    expect(opts.description).not.toMatch(/Tier[12]/)
    expect(api.CompactDistro).toHaveBeenCalledWith('Ubuntu', '')
    runtime.handlers['wsl:compact']?.({ data: { name: 'Ubuntu', stage: 'backup', beforeMB: 0, afterMB: 0, message: '备份中' } })
    await flushPromises()
    expect(w.find('.compact-row-editor').text()).toContain('全量备份中')
    runtime.handlers['wsl:compact']?.({ data: { name: 'Ubuntu', stage: 'optimize', beforeMB: 0, afterMB: 0 } })
    await flushPromises()
    expect(w.find('.compact-row-editor').text()).toContain('压缩数据盘中') // 原 'Tier1 压缩中'
    runtime.handlers['wsl:compact']?.({ data: { name: 'Ubuntu', stage: 'reimport', beforeMB: 0, afterMB: 0 } })
    await flushPromises()
    expect(w.find('.compact-row-editor').text()).toContain('从备份重建中') // 原 'Tier2 注销重导入中'
    api.ListDistroExports.mockResolvedValue([
      { id: 'Ubuntu-backup.tar', name: 'Ubuntu', path: 'C:\\dl\\Ubuntu-backup.tar', size: 2048, at: '2026-09-15 10:00:00' },
    ])
    runtime.handlers['wsl:compact']?.({ data: { name: 'Ubuntu', stage: 'done', tier: 'tier2', beforeMB: 20480, afterMB: 8192, message: '瘦身完成（Tier2）' } })
    await flushPromises()
    const editor = w.find('.compact-row-editor')
    expect(editor.text()).toContain('20.0 GB → 8.0 GB')
    expect(editor.text()).toContain('已走备份重建') // tier 徽标白话化
    expect(w.find('.export-log').attributes('open')).toBeDefined() // 备份工件即刻可见
    expect(rowBtns(w, 2)[3].attributes('disabled')).toBeUndefined() // done 即放闸（编辑行占 tr[1]）
    await editor.findAll('button').find(b => b.text().includes('收起'))!.trigger('click')
    await flushPromises()
    expect(w.find('.compact-row-editor').exists()).toBe(false)
  })

  it('导入面板已迁往「➕ 添加实例」页：本机发行版页不再内嵌导入表单', async () => {
    const w = await consoleMounted()
    expect(w.find('#wsl-import-name').exists()).toBe(false)
    expect(w.findAll('button').some(b => b.text().includes('导入发行版'))).toBe(false)
  })
})

// ---------- F9 USB 直通（usbipd-win） ----------

const USB_SHARE = {
  installed: true,
  version: '5.3.0',
  devices: [
    { busId: '2-3', description: 'USB-SERIAL CH340', instanceId: 'USB\\VID_1A86&PID_7523\\X', vid: '1a86', pid: '7523', state: 'shared', forced: false },
    { busId: '1-1', description: 'ESP32 开发板', instanceId: 'USB\\VID_303A&PID_1001\\Y', vid: '303a', pid: '1001', state: 'notshared', forced: false },
    { busId: '1-4', description: '加密狗', instanceId: 'USB\\VID_0988&PID_03DC\\Z', vid: '0988', pid: '03dc', state: 'attached', clientIp: '127.0.1.1', forced: false },
    { busId: '', description: 'FTDI（已拔出）', instanceId: 'USB\\VID_0403&PID_6001\\FT1', vid: '0403', pid: '6001', guid: 'aaaa1111-2222-3333-4444-555555555555', state: 'shared', forced: false },
  ],
  ledger: [
    { id: 'usb-1', busId: '1-4', vid: '0988', pid: '03dc', description: '加密狗', distro: 'Ubuntu', enabled: true, addedAt: '2026-09-18 10:00:00', lastStatus: '已附加到 Ubuntu', lastAt: '2026-09-18 10:00:05' },
  ],
  autoEnabled: true,
  replayBusy: false,
  lastReplay: '[10:00:05] 重放完成：附加 1 台，跳过 0 台',
  releasesPage: 'https://github.com/dorssel/usbipd-win/releases',
}

const usbRowBtn = (w: Awaited<ReturnType<typeof setup>>, row: number, text: string) =>
  w.find('#wsl-main-usb-panel').findAll('tbody tr')[row].findAll('button').find(b => b.text().includes(text))!

async function usbMounted() {
  const w = await setup()
  await completeCheck(w)
  api.GetUsbOverview.mockResolvedValue(USB_SHARE)
  api.BindUsbDevice.mockResolvedValue(ok('共享完成'))
  api.AttachUsbDevice.mockResolvedValue(ok('已附加'))
  api.DetachUsbDevice.mockResolvedValue(ok('已卸下'))
  api.SetUsbShare.mockResolvedValue(ok('已登记'))
  api.SetUsbShareEnabled.mockResolvedValue(ok('账本已更新'))
  api.RemoveUsbShare.mockResolvedValue(ok('已移除'))
  api.ReplayUsbNow.mockResolvedValue(ok('重放完成'))
  await w.findAll('.main-tab-btn')[5].trigger('click')
  await flushPromises()
  return w
}

describe('WSLView USB 直通页（F9）', () => {
  it('未装 usbipd：渲染引导卡（发布页 + winget 命令），不渲染设备表', async () => {
    const w = await setup()
    await completeCheck(w)
    await w.findAll('.main-tab-btn')[5].trigger('click')
    await flushPromises()
    const panel = w.find('#wsl-main-usb-panel')
    expect(panel.text()).toContain('需要 usbipd-win')
    expect(panel.text()).toContain('winget install --id dorssel.usbipd-win')
    expect(panel.find('table').exists()).toBe(false)
    await panel.findAll('button').find(b => b.text().includes('打开官方发布页'))!.trigger('click')
    await flushPromises()
    expect(api.OpenUsbipdReleases).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('设备表列集与状态归一（在场/未共享/已附加/不在场沉底），账本呈现上次结果', async () => {
    const w = await usbMounted()
    const panel = w.find('#wsl-main-usb-panel')
    expect(panel.text()).toContain('v5.3.0')
    expect(panel.text()).toContain('2-3')
    expect(panel.text()).toContain('1a86:7523')
    expect(panel.text()).toContain('已共享·不在场')
    expect(panel.text()).toContain('127.0.1.1')
    expect(panel.text()).toContain('重放完成：附加 1 台') // lastReplay 上屏
    const ledgerTable = panel.findAll('table')[1]
    expect(ledgerTable.text()).toContain('加密狗')
    expect(ledgerTable.text()).toContain('已附加到 Ubuntu')
    w.unmount()
  })

  it('一键直通：已共享设备免确认直达（目标发行版=默认 WSL2 实例），停止实例由后端顺手拉起', async () => {
    const w = await usbMounted()
    await usbRowBtn(w, 0, '直通').trigger('click')
    await flushPromises()
    expect(api.AttachUsbDevice).toHaveBeenCalledWith('2-3', 'Ubuntu')
    expect(confirmFn).not.toHaveBeenCalled()
    w.unmount()
  })

  it('一键直通：未共享设备先经 UAC 确认补 bind 再 attach；bind 未成即中止', async () => {
    const w = await usbMounted()
    await usbRowBtn(w, 1, '直通').trigger('click')
    await flushPromises()
    expect(confirmFn).toHaveBeenCalled()
    expect(api.BindUsbDevice).toHaveBeenCalledWith('1-1', false)
    expect(api.AttachUsbDevice).toHaveBeenCalledWith('1-1', 'Ubuntu')
    // bind 回执 success=false（UAC 取消族）：不得抢跑 attach
    api.BindUsbDevice.mockResolvedValueOnce({ success: false, message: '已取消 UAC 授权' })
    api.AttachUsbDevice.mockClear()
    await usbRowBtn(w, 1, '直通').trigger('click')
    await flushPromises()
    expect(api.AttachUsbDevice).not.toHaveBeenCalled()
    w.unmount()
  })

  it('卸下/取消共享/不在场退绑：各自路由到对应命令（取消共享收在高级操作区）', async () => {
    api.UnbindUsbDevice.mockResolvedValue(ok('已取消共享'))
    api.UnbindAbsentUsbDevice.mockResolvedValue(ok('已取消共享'))
    localStorage.setItem('hanxi.wsl.usb.advanced', '1') // 高级操作默认收起：本用例按展开态挂载
    const w = await usbMounted()
    await usbRowBtn(w, 2, '卸下').trigger('click')
    await flushPromises()
    expect(api.DetachUsbDevice).toHaveBeenCalledWith('1-4')
    await usbRowBtn(w, 0, '取消共享').trigger('click')
    await flushPromises()
    expect(api.UnbindUsbDevice).toHaveBeenCalledWith('2-3')
    await usbRowBtn(w, 3, '退绑').trigger('click')
    await flushPromises()
    expect(api.UnbindAbsentUsbDevice).toHaveBeenCalledWith('aaaa1111-2222-3333-4444-555555555555')
    w.unmount()
  })

  it('账本操作：登记走 SetUsbShare，停用/移除走条目命令；手动重放走 ReplayUsbNow', async () => {
    localStorage.setItem('hanxi.wsl.usb.advanced', '1') // 「⭐ 自动共享」在高级操作区内
    const w = await usbMounted()
    await usbRowBtn(w, 0, '自动共享').trigger('click')
    await flushPromises()
    expect(api.SetUsbShare).toHaveBeenCalledWith('2-3', 'Ubuntu')
    const ledgerBtns = w.find('#wsl-main-usb-panel').findAll('table')[1].findAll('tbody tr')[0].findAll('button')
    await ledgerBtns[0].trigger('click') // ⏸ 停用
    await flushPromises()
    expect(api.SetUsbShareEnabled).toHaveBeenCalledWith('usb-1', false)
    await ledgerBtns[1].trigger('click') // 🗑 移除（先确认）
    await flushPromises()
    expect(api.RemoveUsbShare).toHaveBeenCalledWith('usb-1')
    await w.find('#wsl-main-usb-panel').findAll('button').find(b => b.text().includes('重放共享'))!.trigger('click')
    await flushPromises()
    expect(api.ReplayUsbNow).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('总开关确认被拒不触达后端，且 checkbox 弹回账本真值（key 增强重挂载）', async () => {
    const w = await setup()
    await completeCheck(w)
    api.GetUsbOverview.mockResolvedValue({ ...USB_SHARE, autoEnabled: false })
    await w.findAll('.main-tab-btn')[5].trigger('click')
    await flushPromises()
    confirmFn.mockResolvedValueOnce(false) // 打开需确认：拒
    const sw = w.find('#wsl-main-usb-panel .auto-switch input')
    await sw.setValue(true) // 翻位 → @change → 确认被拒
    await flushPromises()
    expect(api.SetUsbAutoAttach).not.toHaveBeenCalled()
    expect((w.find('#wsl-main-usb-panel .auto-switch input').element as HTMLInputElement).checked).toBe(false)
    w.unmount()
  })

  it('轮询随页签生灭（v-if 生命周期即开关）：离开页面板即销毁，回页重拉', async () => {
    const w = await usbMounted()
    const callsOnOpen = api.GetUsbOverview.mock.calls.length
    await w.findAll('.main-tab-btn')[0].trigger('click') // 回就绪页
    await flushPromises()
    expect(w.find('#wsl-main-usb-panel').exists()).toBe(false)
    await w.findAll('.main-tab-btn')[5].trigger('click')
    await flushPromises()
    expect(w.find('#wsl-main-usb-panel').exists()).toBe(true)
    expect(api.GetUsbOverview.mock.calls.length).toBeGreaterThan(callsOnOpen)
    w.unmount()
  })
})
