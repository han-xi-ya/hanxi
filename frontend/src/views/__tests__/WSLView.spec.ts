// 特征测试：WSLView 基线锁定。
// 断言对象：流式体检（pending 骨架 → 分相点亮 → done 收口）、提权操作确认链路、
// 正规卸载入口、版本页懒加载在线清单、MSI 直链参数拼装、发行版白名单安装传参。
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WSLView from '../WSLView.vue'

const api = vi.hoisted(() => ({
  StartReadiness: vi.fn(),
  GetReadiness: vi.fn(),
  GetReleases: vi.fn(),
  ListOnlineDistros: vi.fn(),
  InstallWsl: vi.fn(),
  InstallWslNoDistro: vi.fn(),
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

interface ConfirmOpts { title?: string; description?: string; tone?: string }
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

async function setup() {
  api.StartReadiness.mockResolvedValue(undefined)
  api.GetReleases.mockResolvedValue(OVERVIEW)
  api.ListOnlineDistros.mockResolvedValue(ONLINE)
  api.SetDefaultVersion2.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.InstallDistro.mockResolvedValue({ success: true, message: '操作已执行完毕' })
  api.UninstallWsl.mockResolvedValue({ success: true, message: '双路卸载完成' })
  api.DownloadMsi.mockResolvedValue({ success: true, message: '开始下载，保存到系统下载文件夹' })
  api.RevealDownload.mockResolvedValue(undefined)
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
    expect(w.text()).toContain('正在运行')
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
    await w.findAll('.main-tab-btn')[1].trigger('click')
    await flushPromises()
    const install = w.findAll('button').find(b => b.text().includes('安装'))!
    await install.trigger('click')
    await flushPromises()
    expect(api.InstallDistro).toHaveBeenCalledWith('Ubuntu-24.04')
  })
})
