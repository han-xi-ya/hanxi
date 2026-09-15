// 特征测试：WSLView 基线锁定。
// 断言对象：流式体检（pending 骨架 → 分相点亮 → done 收口）、提权操作确认链路、
// 正规卸载入口、版本页懒加载在线清单、MSI 直链参数拼装、发行版白名单安装传参。
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
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
}))

vi.mock('../../../bindings/hanxi/internal/modules/wsl/wslservice', () => api)

// Wails 运行时事件总线桩：捕获 wsl:readiness 订阅回调，测试内手动分相投喂。
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

async function setup() {
  api.StartReadiness.mockResolvedValue(undefined)
  api.GetReleases.mockResolvedValue(OVERVIEW)
  api.ListOnlineDistros.mockResolvedValue(ONLINE)
  api.SetDefaultVersion2.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.InstallDistro.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.UninstallWsl.mockResolvedValue({ success: true, message: '双路卸载完成' })
  api.DownloadMsi.mockResolvedValue({ success: true, message: '开始下载，保存到系统下载文件夹' })
  api.RevealDownload.mockResolvedValue(undefined)
  api.ListInstances.mockResolvedValue(INSTANCES)
  api.ListDistroExports.mockResolvedValue([])
  api.ListPortRules.mockResolvedValue({ rules: [], foreign: [], pending: false })
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
  confirmFn.mockResolvedValue(true)
})

describe('WSLView 流式体检', () => {
  it('挂载即启动流式体检：骨架 9 项先以「检测中」渲染', async () => {
    const w = await setup()
    expect(api.StartReadiness).toHaveBeenCalledTimes(1)
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
    // 完成落章：每行到齐都有记号——✓ 通过 8、⚠ 注意 1、✓（灰）已检测不判定 1（vbs）
    expect(w.findAll('.check-trail.ok').length).toBe(8)
    expect(w.findAll('.check-trail.warn').length).toBe(1)
    expect(w.findAll('.check-trail.info').length).toBe(1)
    expect(w.findAll('.check-trail').length).toBe(10)
    expect(w.text()).toContain('★ 默认')
    // 状态归一呈现：本地化原文只进 title 备查，正文一律中文枚举；占用走注册表列
    expect(w.text()).toContain('运行中')
    expect(w.text()).toContain('已停止')
    expect(w.text()).toContain('12.3 MB')
  })

  it('error 阶段落错误框并解除 streaming', async () => {
    const w = await setup()
    emitReadiness({ stage: 'error', error: 'WSL 就绪探针执行失败：PowerShell 被拦截' })
    await flushPromises()
    expect(w.find('.error-box').text()).toContain('PowerShell 被拦截')
  })
})

describe('WSLView 操作流', () => {
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

  it('重启引导条纯检测驱动：CBS 台账报 pending 才出现，与操作语境无关', async () => {
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

  it('正规卸载：危险确认 + 双路卸载调用', async () => {
    const w = await setup()
    await completeCheck(w)
    const btn = w.findAll('button').find(b => b.text().includes('卸载 WSL'))!
    await btn.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts | undefined
    expect(opts?.tone).toBe('danger')
    expect(api.UninstallWsl).toHaveBeenCalledTimes(1)
    expect(api.StartReadiness).toHaveBeenCalledTimes(2) // 卸载后自动流式复查
  })
})

describe('WSLView 版本与发行版', () => {
  it('在线清单懒加载：切到版本页才查询', async () => {
    const w = await setup()
    expect(api.ListOnlineDistros).not.toHaveBeenCalled()
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    expect(api.ListOnlineDistros).toHaveBeenCalledTimes(1)
    expect(w.text()).toContain('Ubuntu-24.04')
    expect(w.text()).toContain('Debian')
  })

  it('API 被拦降级：fallback 载荷必须如实标注订阅源来源', async () => {
    api.GetReleases.mockResolvedValueOnce({ ...OVERVIEW, fallback: true, localVersion: '', relation: 'unknown' })
    const w = await setup()
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    expect(w.text()).toContain('订阅源')
    expect(w.text()).toContain('403')
  })

  it('应用内下载：受理参数精确、事件驱动进度到"已下载+打开位置"闭环', async () => {
    const w = await setup()
    await completeCheck(w) // machineArch 需体检报告到位才可知（未知时兜底列全是设计行为）
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    expect(w.text()).toContain('2.9.10')
    expect(w.text()).toContain('本机') // machineArch=x64 → 本机徽章
    // 异架构（ARM64）整行隐藏：仅剩本机架构一个下载按钮
    const dl = w.findAll('button').filter(b => b.text().includes('下载'))
    expect(dl.length).toBe(1)
    expect(w.text()).not.toContain('ARM64')
    await dl[0].trigger('click')
    await flushPromises()
    expect(api.DownloadMsi).toHaveBeenCalledWith('2.9.10', 'wsl.2.9.10.0.x64.msi')
    // 模拟进度事件：downloading 出进度条，done 收口为已下载 + 打开位置
    runtime.handlers['wsl:msi-download']?.({ data: { tag: '2.9.10', platform: 'x64', stage: 'downloading', done: 5e6, total: 18e6 } })
    await flushPromises()
    expect(w.find('.ui-progress').exists()).toBe(true)
    runtime.handlers['wsl:msi-download']?.({ data: { tag: '2.9.10', platform: 'x64', stage: 'done', done: 18e6, total: 18e6, path: 'C:\\Users\\x\\Downloads\\wsl.2.9.10.0.x64.msi' } })
    await flushPromises()
    expect(w.text()).toContain('已下载')
    const reveal = w.findAll('button').find(b => b.text().includes('打开位置'))!
    await reveal.trigger('click')
    await flushPromises()
    expect(api.RevealDownload).toHaveBeenCalledWith('2.9.10', 'wsl.2.9.10.0.x64.msi')
  })

  it('发行版安装：把清单 ID 原样交给后端白名单校验', async () => {
    const w = await setup()
    await completeCheck(w) // distroBlockedReason 需报告：vmPlatformEnabled=true、无 rebootPending → 放行
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    expect(w.find('.distro-block-banner').exists()).toBe(false)
    const install = w.findAll('button').find(b => b.text().includes('安装'))!
    await install.trigger('click')
    await flushPromises()
    expect(api.InstallDistro).toHaveBeenCalledWith('Ubuntu-24.04')
  })

  it('虚拟机平台未生效时预告拦截：版本页黄条警告 + 安装按钮禁用', async () => {
    const w = await setup()
    // 投喂"已启用但 CBS 欠重启"的报告——WSL2 起不了虚拟机，装发行版注定失败
    emitReadiness({ stage: 'done', report: { ...REPORT, rebootPending: true } })
    await flushPromises()
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    const banner = w.find('.distro-block-banner')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('重启')
    // 按钮禁用，点不动（免得弹了 UAC 才失败）
    const install = w.findAll('button').find(b => b.text().includes('安装'))!
    expect(install.attributes('disabled')).toBeDefined()
    await install.trigger('click')
    expect(api.InstallDistro).not.toHaveBeenCalled()
  })
})

describe('WSLView 发行版实例管理', () => {
  // 行内按钮顺序（与模板一致）：0 终端 1 设默认 2 终止 3 导出 4 迁移 5 删除 6 克隆 7 取证 8 wsl.conf 9 瘦身
  async function consoleMounted() {
    const w = await setup()
    await completeCheck(w)
    return w
  }
  const rowBtns = (w: Awaited<ReturnType<typeof consoleMounted>>, row: number) =>
    w.findAll('tbody tr')[row].findAll('button')
  // 覆盖列表数据源并复采（setup 默认 mock 会重置，故覆盖必须发生在挂载后）
  async function reloadWith(w: Awaited<ReturnType<typeof consoleMounted>>, list: unknown[]) {
    api.ListInstances.mockResolvedValue(list)
    await w.findAll('button').find(b => b.text().includes('刷新列表'))!.trigger('click')
    await flushPromises()
  }

  it('唤终端免确认直达，发行版名原样透传后端白名单', async () => {
    api.OpenTerminal.mockResolvedValue(ok('已启动终端会话'))
    const w = await consoleMounted()
    await rowBtns(w, 0)[0].trigger('click')
    await flushPromises()
    expect(confirmFn).not.toHaveBeenCalled()
    expect(api.OpenTerminal).toHaveBeenCalledWith('Ubuntu')
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

  it('删除：数据销毁级危险确认 + details 点名占用与位置；确认被拒不触达', async () => {
    api.UnregisterDistro.mockResolvedValue(ok('已删除'))
    const w = await consoleMounted()
    await rowBtns(w, 0)[5].trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('不可恢复')
    expect(opts.details?.find(d => d.label === '磁盘占用')?.value).toBe('12.3 MB')
    expect(opts.details?.find(d => d.label === '数据位置')?.value).toBe('C:\\lxss\\u')
    expect(api.UnregisterDistro).toHaveBeenCalledWith('Ubuntu')

    const w2 = await consoleMounted()
    confirmFn.mockResolvedValueOnce(false)
    await rowBtns(w2, 0)[5].trigger('click')
    await flushPromises()
    expect(api.UnregisterDistro).toHaveBeenCalledTimes(1) // 第二次被拒不得新增调用
  })

  it('导出两段式：先展开格式选择不进命令，开始导出透传 gzip 参数；记录全量列出逐份直达', async () => {
    api.ExportDistro.mockResolvedValue({ ...ok('Ubuntu 已导出'), id: 'Ubuntu-20260914-120000.tar.gz' })
    api.RevealDistroExport.mockResolvedValue(undefined)
    const w = await consoleMounted()
    await rowBtns(w, 0)[3].trigger('click')
    await flushPromises()
    // 第一段：只展开内联格式选择，未触达命令、未弹确认
    const editor = w.find('.export-row-editor')
    expect(editor.exists()).toBe(true)
    expect(api.ExportDistro).not.toHaveBeenCalled()
    expect(confirmFn).not.toHaveBeenCalled()
    // 切到未压缩 tar 再开始：gzip=false 透传
    await editor.findAll('input[type="radio"]')[1].setValue()
    // 成功后会拉取登记列表：先备好记录 mock，逐份挂出「打开位置」
    api.ListDistroExports.mockResolvedValue([
      { id: 'Ubuntu-20260914-120000.tar.gz', name: 'Ubuntu', path: 'C:\\dl\\Ubuntu-20260914-120000.tar.gz', size: 1024, at: '2026-09-14 12:00:00' },
    ])
    await editor.findAll('button')[0].trigger('click') // ✔ 开始导出
    await flushPromises()
    expect(api.ExportDistro).toHaveBeenCalledWith('Ubuntu', false)
    await flushPromises()
    const log = w.find('.export-log')
    expect(log.exists()).toBe(true)
    expect(log.text()).toContain('Ubuntu-20260914-120000.tar.gz')
    await log.findAll('button').find(b => b.text().includes('打开位置'))!.trigger('click')
    await flushPromises()
    expect(api.RevealDistroExport).toHaveBeenCalledWith('Ubuntu-20260914-120000.tar.gz')
  })

  it('迁移：内联表单→空路径禁确认→确认链点名 --shutdown 全局停机与运行中实例→路径透传并收起', async () => {
    api.MoveDistro.mockResolvedValue(ok('迁移完成'))
    const w = await consoleMounted()
    // Debian 改为运行中：迁移预警必须点名会被 --shutdown 连带打停的实例
    await reloadWith(w, [
      INSTANCES[0],
      { ...INSTANCES[1], running: true, stateText: '正在运行' },
    ])
    await rowBtns(w, 0)[4].trigger('click')
    await flushPromises()
    const editor = w.find('.move-editor')
    expect(editor.exists()).toBe(true)
    expect(editor.text()).toContain('wsl --shutdown')
    expect(editor.text()).toContain('Debian') // 运行中的其它实例被点名预警
    const submit = editor.findAll('button')[0] // 确认迁移
    expect(submit.attributes('disabled')).toBeDefined()
    await w.find('#wsl-move-target').setValue('  D:\\WSL\\Ubuntu  ')
    await submit.trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('wsl --shutdown')
    expect(opts.details?.find(d => d.label === '目标目录')?.value).toBe('D:\\WSL\\Ubuntu')
    expect(api.MoveDistro).toHaveBeenCalledWith('Ubuntu', 'D:\\WSL\\Ubuntu')
    expect(w.find('.move-editor').exists()).toBe(false) // 完成后收起
  })

  it('全局互斥：导出在飞时其余发行版操作全部禁用', async () => {
    let release!: (v: ReturnType<typeof ok>) => void
    api.ExportDistro.mockImplementation(() => new Promise(res => { release = res }))
    const w = await consoleMounted()
    await rowBtns(w, 0)[3].trigger('click')
    await w.find('.export-row-editor').findAll('button')[0].trigger('click') // ✔ 开始导出
    await flushPromises()
    await flushPromises()
    // 导出编辑行占 tr[1]，Debian 行下移 tr[2]：导出/删除在忙时禁用（终止钮本来就因已停止禁用，不作判据）
    expect(rowBtns(w, 2)[3].attributes('disabled')).toBeDefined()
    expect(rowBtns(w, 2)[5].attributes('disabled')).toBeDefined()
    release(ok('导出完成'))
    await flushPromises()
    await flushPromises()
    // 收口后编辑行收起，Debian 回到 tr[1]
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

  it('取证抽屉：磁盘双口径/稀疏/df/IP 渲染，复采重发，再点收起', async () => {
    api.GetDistroForensics.mockResolvedValue({
      name: 'Ubuntu', pfn: 'CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc',
      basePath: 'C:\\lxss\\u', vhdxPath: 'C:\\lxss\\u\\ext4.vhdx',
      logicalBytes: 62_914_560, allocBytes: 20_971_520, sparse: true,
      running: true, runtimeKnown: true,
      dfTotalMB: 50579, dfUsedMB: 13634, dfAvailMB: 37135, dfUsePct: 27, dfOk: true,
      ipv4: '172.25.4.136', notes: [],
    })
    const w = await consoleMounted()
    await rowBtns(w, 0)[7].trigger('click')
    await flushPromises()
    expect(api.GetDistroForensics).toHaveBeenCalledWith('Ubuntu')
    const panel = w.find('.forensics-panel')
    expect(panel.text()).toContain('稀疏盘')
    expect(panel.text()).toContain('60.0 MB') // 逻辑 62,914,560
    expect(panel.text()).toContain('20.0 MB') // 实占 20,971,520
    expect(panel.text()).toContain('172.25.4.136')
    expect(panel.find('.ui-progress').exists()).toBe(true)
    // 复采：换一份停止态数据投喂
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
    expect(panel2.text()).toContain('未运行') // Notes 如实呈现缺项，不编数字
    expect(panel2.find('.ui-progress').exists()).toBe(false)
    expect(panel2.text()).not.toContain('172.25')
    // 再点钮收起抽屉
    await rowBtns(w, 0)[7].trigger('click')
    await flushPromises()
    expect(w.find('.forensics-panel').exists()).toBe(false)
  })

  it('克隆：表单先行→受理等 wsl:clone 终态；done 放闸关表单复采', async () => {
    api.CloneDistro.mockResolvedValue({ success: true, message: '开始克隆' })
    const w = await consoleMounted()
    await rowBtns(w, 0)[6].trigger('click')
    await flushPromises()
    const editor = w.find('.clone-row-editor')
    expect(editor.exists()).toBe(true)
    expect(api.CloneDistro).not.toHaveBeenCalled() // 先填表不进命令
    // 空目标目录时确认钮禁用
    expect(editor.findAll('button')[0].attributes('disabled')).toBeDefined()
    await w.find('#wsl-clone-name').setValue('Ubuntu-Copy')
    await w.find('#wsl-clone-target').setValue('D:\\WSL\\Ubuntu-Copy')
    await editor.findAll('button')[0].trigger('click')
    await flushPromises()
    const opts = confirmFn.mock.calls.at(-1)?.[0] as ConfirmOpts
    expect(opts.description).toContain('先终止源发行版')
    expect(opts.details?.find(d => d.label === '目标目录')?.value).toBe('D:\\WSL\\Ubuntu-Copy')
    expect(api.CloneDistro).toHaveBeenCalledWith('Ubuntu', 'Ubuntu-Copy', 'D:\\WSL\\Ubuntu-Copy')
    // 在飞：表单切进度态、全局闸门关门
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'copying', done: 500, total: 1000 } })
    await flushPromises()
    expect(w.find('.clone-row-editor').text()).toContain('拷贝数据盘中')
    expect(rowBtns(w, 2)[5].attributes('disabled')).toBeDefined() // 编辑行占位后 Debian 在 tr[2]
    // 终态 done：清闸、收表单、复采
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'done', done: 1000, total: 1000, message: '已克隆为 Ubuntu-Copy' } })
    await flushPromises()
    expect(w.find('.clone-row-editor').exists()).toBe(false)
    expect(rowBtns(w, 1)[5].attributes('disabled')).toBeUndefined()
  })

  it('克隆失败：error 留表单可见并放闸，不吞错', async () => {
    api.CloneDistro.mockResolvedValue({ success: true, message: '开始克隆' })
    const w = await consoleMounted()
    await rowBtns(w, 0)[6].trigger('click')
    await w.find('#wsl-clone-name').setValue('Ubuntu-Copy')
    await w.find('#wsl-clone-target').setValue('D:\\x')
    await w.find('.clone-row-editor').findAll('button')[0].trigger('click')
    await flushPromises()
    runtime.handlers['wsl:clone']?.({ data: { source: 'Ubuntu', target: 'Ubuntu-Copy', stage: 'error', done: 0, total: 0, error: '挂载克隆盘失败（已拷贝的数据盘保留在 D:\\x，可自行处理）' } })
    await flushPromises()
    const box = w.find('.clone-row-editor .error-box')
    expect(box.exists()).toBe(true)
    expect(box.text()).toContain('保留在 D:\\x')
    expect(rowBtns(w, 2)[5].attributes('disabled')).toBeUndefined() // 闸门已放
    // 「知道了，收起」关表单
    await box.find('button').trigger('click')
    await flushPromises()
    expect(w.find('.clone-row-editor').exists()).toBe(false)
  })

  it('wsl.conf 编辑器：装载回填文本与版本警示，保存按当前文本透传', async () => {
    api.GetWslConf.mockResolvedValue({
      name: 'Ubuntu', text: '[boot]\nsystemd=true', missing: false, wslVersion: '2.7.13',
      warnings: ['[boot]：systemd / command 等 [boot] 项要求 WSL 2.4.4+（老版本会静默忽略）'],
    })
    api.SaveWslConf.mockResolvedValue(ok('已写入并复验一致'))
    const w = await consoleMounted()
    await rowBtns(w, 0)[8].trigger('click')
    await flushPromises()
    expect(api.GetWslConf).toHaveBeenCalledWith('Ubuntu')
    const editor = w.find('.conf-row-editor')
    expect(editor.exists()).toBe(true)
    expect(editor.find('textarea').element.value).toBe('[boot]\nsystemd=true')
    expect(editor.text()).toContain('2.4.4') // 版本门控警示如实呈现
    await editor.find('textarea').setValue('[boot]\nsystemd=false')
    await editor.findAll('button')[0].trigger('click') // ✔ 保存写回
    await flushPromises()
    expect(api.SaveWslConf).toHaveBeenCalledWith('Ubuntu', '[boot]\nsystemd=false')
    // 保存成功后引导"终止使其生效"：第二道确认 + 免 UAC 直调
    expect(confirmFn).toHaveBeenCalledTimes(2)
    expect(confirmFn.mock.calls[1][0].title).toContain('生效')
    expect(api.TerminateDistro).toHaveBeenCalledWith('Ubuntu')
  })

  it('瘦身：受理→阶段事件驱动呈现，done 省量对比并放闸，错误留在表单', async () => {
    api.CompactDistro.mockResolvedValue({ success: true, message: '开始瘦身' })
    const w = await consoleMounted()
    await rowBtns(w, 0)[9].trigger('click')
    await flushPromises()
    expect(w.find('.compact-row-editor').text()).toContain('强制先全量备份')
    await w.find('.compact-row-editor').findAll('button')[0].trigger('click') // 🗜 确认开始瘦身
    await flushPromises()
    expect(confirmFn).toHaveBeenCalledTimes(1)
    expect(api.CompactDistro).toHaveBeenCalledWith('Ubuntu', '')
    runtime.handlers['wsl:compact']?.({ data: { name: 'Ubuntu', stage: 'backup', beforeMB: 0, afterMB: 0, message: '备份中' } })
    await flushPromises()
    expect(w.find('.compact-row-editor').text()).toContain('全量备份中')
    runtime.handlers['wsl:compact']?.({ data: { name: 'Ubuntu', stage: 'done', tier: 'tier2', beforeMB: 20480, afterMB: 8192, message: '瘦身完成（Tier2）' } })
    await flushPromises()
    const editor = w.find('.compact-row-editor')
    expect(editor.text()).toContain('20.0 GB → 8.0 GB')
    expect(editor.text()).toContain('tier2')
    // done 即放闸；编辑行占 tr[1]，Debian 删除钮恢复可用
    expect(rowBtns(w, 2)[5].attributes('disabled')).toBeUndefined()
    await editor.findAll('button').find(b => b.text().includes('收起'))!.trigger('click')
    await flushPromises()
    expect(w.find('.compact-row-editor').exists()).toBe(false)
  })

  it('导入：三要素齐才可提交，参数原样透传，成功收面板复采', async () => {
    api.ImportDistro.mockResolvedValue(ok('Fresh 已导入'))
    const w = await consoleMounted()
    await w.findAll('button').find(b => b.text().includes('导入发行版'))!.trigger('click')
    await flushPromises()
    const panel = w.find('.import-panel')
    expect(panel.exists()).toBe(true)
    const submit = panel.findAll('button')[0]
    expect(submit.attributes('disabled')).toBeDefined() // 空表单禁提交
    await w.find('#wsl-import-name').setValue('Fresh')
    await w.find('#wsl-import-target').setValue('D:\\WSL\\Fresh')
    await w.find('#wsl-import-tar').setValue('C:\\dl\\Ubuntu.tar.gz')
    await panel.findAll('button')[0].trigger('click')
    await flushPromises()
    expect(api.ImportDistro).toHaveBeenCalledWith('Fresh', 'D:\\WSL\\Fresh', 'C:\\dl\\Ubuntu.tar.gz')
    expect(w.find('.import-panel').exists()).toBe(false) // 成功即收
  })
})

describe('WSLView 端口转发', () => {
  const PP_VIEW = {
    rules: [
      { id: 'pp-a', distro: 'Ubuntu', port: 8080, guest: 8080, listen: '0.0.0.0', firewall: true, note: 'web', enabled: true, applied: true, activeIP: '172.25.4.136', targetIP: '172.25.9.99', distroRunning: true },
      { id: 'pp-b', distro: 'Debian', port: 9000, guest: 9000, listen: '0.0.0.0', firewall: false, note: '', enabled: false, applied: false, activeIP: '', targetIP: '', distroRunning: false },
    ],
    foreign: [{ listenAddr: '0.0.0.0', listenPort: 3389, connectAddr: '172.25.4.200', connectPort: 3389 }],
    pending: true,
  }

  it('Tab 懒加载；漂移/停用状态分列，外部转发只展示', async () => {
    const w = await setup()
    api.ListPortRules.mockResolvedValue(PP_VIEW) // setup 的默认 mock 之后覆盖
    expect(api.ListPortRules).not.toHaveBeenCalled() // 冷页不探测
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    expect(api.ListPortRules).toHaveBeenCalledTimes(1)
    expect(w.text()).toContain('账本有改动尚未应用')
    expect(w.text()).toContain('IP 漂移·需重应用')
    expect(w.text()).toContain('已停用')
    expect(w.text()).toContain('外部转发')
    expect(w.text()).toContain('3389')
  })

  it('镜像网络时端口转发页给出 localhost 直通提示', async () => {
    const w = await setup()
    api.ListPortRules.mockResolvedValue({ ...PP_VIEW, networkMode: 'mirrored' })
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    expect(w.text()).toContain('镜像网络')
    expect(w.text()).toContain('localhost 直通')
  })

  it('添加走账本参数直传并复采；应用经 UAC 确认链', async () => {
    api.AddPortRule.mockResolvedValue({ ...PP_VIEW.rules[0], applied: false })
    api.ApplyPortRules.mockResolvedValue({ success: true, message: '已同步' })
    const w = await setup()
    await w.findAll('.main-tab-btn')[2].trigger('click')
    await flushPromises()
    await w.find('#wsl-pp-distro').setValue('Ubuntu')
    await w.find('#wsl-pp-port').setValue('8081')
    await w.find('#wsl-pp-guest').setValue('80')
    await w.findAll('button').find(b => b.text().includes('＋ 添加'))!.trigger('click')
    await flushPromises()
    expect(api.AddPortRule).toHaveBeenCalledWith('Ubuntu', 8081, 80, '127.0.0.1', false, '')
    expect(api.ListPortRules).toHaveBeenCalledTimes(2) // 添加即复采账本
    // 应用规则：确认链 → 提权批量脚本（后端拼装，前端只触发）。
    // 复采后账本里得有规则，「应用」钮才非禁用态：先刷新拉回有规则的视图。
    api.ListPortRules.mockResolvedValue(PP_VIEW)
    await w.findAll('button').find(b => b.text().trim() === '↻ 刷新')!.trigger('click')
    await flushPromises()
    await w.findAll('button').find(b => b.text().includes('应用规则'))!.trigger('click')
    await flushPromises()
    expect(confirmFn).toHaveBeenCalledTimes(1)
    expect(api.ApplyPortRules).toHaveBeenCalledTimes(1)
  })
})

describe('WSLView 全局配置与全停', () => {
  it('.wslconfig 面板：打开即读、模式徽标呈现、保存两段确认（拒绝全停不打断保存）', async () => {
    api.GetWslHostConf.mockResolvedValue({
      path: 'C:\\Users\\me\\.wslconfig', text: '[networking]\nnetworkingMode=mirrored',
      missing: false, networkMode: 'mirrored', warnings: [],
    })
    api.SaveWslHostConf.mockResolvedValue({ success: true, message: '已写入并复验一致' })
    const w = await setup()
    await w.findAll('button').find(b => b.text().includes('.wslconfig'))!.trigger('click')
    await flushPromises()
    expect(api.GetWslHostConf).toHaveBeenCalledTimes(1)
    const panel = w.findAll('.import-panel').find(p => p.text().includes('宿主全局配置'))!
    expect(panel.text()).toContain('当前网络模式：镜像网络')
    // 保存确认放行、全停引导拒绝：只保存不 shutdown
    confirmFn.mockResolvedValueOnce(true).mockResolvedValueOnce(false)
    await panel.find('textarea').setValue('[networking]\nnetworkingMode=nat')
    await panel.findAll('button').find(b => b.text().includes('保存写回'))!.trigger('click')
    await flushPromises()
    expect(api.SaveWslHostConf).toHaveBeenCalledWith('[networking]\nnetworkingMode=nat')
    expect(api.ShutdownWsl).not.toHaveBeenCalled()
    // 保存为 danger 级确认 + 生效引导各一次
    expect(confirmFn).toHaveBeenCalledTimes(2)
    expect((confirmFn.mock.calls[0][0] as ConfirmOpts).tone).toBe('danger')
  })

  it('全停：确认链 → wsl --shutdown 直调 → 复采列表', async () => {
    api.ShutdownWsl.mockResolvedValue({ success: true, message: '已全部停止' })
    const w = await setup()
    await w.findAll('button').find(b => b.text().includes('全停'))!.trigger('click')
    await flushPromises()
    expect(confirmFn).toHaveBeenCalledTimes(1)
    expect(api.ShutdownWsl).toHaveBeenCalledTimes(1)
    await flushPromises() // 收口：复采列表/转发不被在飞锁卡住
    expect(w.findAll('button').find(b => b.text().includes('全停'))!.attributes('disabled')).toBeUndefined()
  })
})
