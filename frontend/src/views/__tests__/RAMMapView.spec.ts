// 特征测试（机主钮位反馈批）：RAMMapView 的 A/B 双路提权选择器（N22 两颗钮
// 「仅提权启动 RAMMap」+「以管理员身份重启」）必须紧贴状态卡下方（#console-extra
// 首位）呈现，DOM 序锁定其不得沉到说明卡之后/页面底部孤悬；
// A/B 双路语义与三重契约之③提权预告文案逐字保留，一并锁死。
import { KeepAlive, defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RAMMapView from '../RAMMapView.vue'
import { useConfirm } from '../../composables/useConfirm'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  OpenWindow: vi.fn(),
  OpenWindowElevated: vi.fn(),
  Quit: vi.fn(),
  ElevationStatus: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  OfficialSiteURL: vi.fn(),
  OpenOfficialSite: vi.fn(),
}))

const appSvc = vi.hoisted(() => ({
  IsElevated: vi.fn(),
  RestartElevated: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn(() => vi.fn()), Emit: vi.fn() },
}))
vi.mock('../../../bindings/hanxi/internal/modules/rammap/rammapservice', () => svc)
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

const installed = [
  {
    version: '2025-01-01', dir: 'D:\\hx\\versions\\rammap_2025-01-01',
    exePath: 'D:\\hx\\versions\\rammap_2025-01-01\\RAMMap64.exe',
    size: 425984, installedAt: '2025-01-02', isImport: false, source: '',
  },
]

function stubBase(snap: Record<string, unknown>) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue([{ version: '2025-01-01', published: '2025-01-01T00:00:00Z', size: 425984, assetName: 'RAMMap.zip', assetUrl: 'https://download.sysinternals.com/files/RAMMap.zip' }])
  svc.GetActiveVersion.mockResolvedValue('2025-01-01')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.OfficialSiteURL.mockResolvedValue('https://learn.microsoft.com/sysinternals/downloads/rammap')
  svc.ElevationStatus.mockResolvedValue({ requiresElevation: true, hostElevated: false, executionLevel: 'requireAdministrator' })
  appSvc.IsElevated.mockResolvedValue(false) // 默认未提权：ElevateRestart 可见
}

async function flushAll(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(RAMMapView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushAll()
  await nextTick()
  return wrapper
}

const { confirmState, settleConfirm } = useConfirm()

beforeEach(() => {
  settleConfirm(false)
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
  document.body.innerHTML = ''
})

describe('RAMMapView A/B 提权选择器钮位（机主反馈：不孤悬页底）', () => {
  it('未提权 + stopped：选择器紧跟状态卡块、先于说明卡（DOM 序锁死）', async () => {
    stubBase({ state: 'stopped' })
    const w = await mountView()
    expect(w.find('.elev-choice').exists()).toBe(true)
    const kids = Array.from(w.find('.tab-body').element.children) as HTMLElement[]
    const idx = (cls: string) => kids.findIndex((el) => el.classList.contains(cls))
    expect(idx('control-bar')).toBe(0) // 状态头卡恒居控制台首位
    expect(idx('elev-choice')).toBeGreaterThan(idx('control-bar')) // 选择器在状态卡块之下
    expect(idx('elev-choice')).toBeLessThan(idx('info-details')) // 绝不沉到说明卡/页面尾部
    // 三重契约之③：未提权引导行与选择器同属状态区，预告文案不丢
    expect(w.find('.hint-line').text()).toContain('管理员')
    w.unmount()
  })

  it('A/B 双路语义锁定：说明句 + 「仅提权启动」主钮 + 「以管理员身份重启」并排', async () => {
    stubBase({ state: 'stopped' })
    const w = await mountView()
    const ec = w.find('.elev-choice')
    expect(ec.text()).toContain('RAMMap 本体要求管理员权限')
    expect(ec.text()).toContain('一次性看数据选「仅提权启动」')
    const btns = ec.findAll('button')
    expect(btns.map((b) => b.text()).join('｜')).toContain('仅提权启动 RAMMap')
    expect(ec.find('.elevate-restart').exists()).toBe(true)
    expect(ec.find('.elevate-restart').text()).toContain('以管理员身份重启')
    w.unmount()
  })

  it('A 路点击：确认框「弹 UAC 启动」逐字，落定后走 OpenWindowElevated', async () => {
    stubBase({ state: 'stopped' })
    svc.OpenWindowElevated.mockResolvedValue({ message: '已单独提权启动 RAMMap', launchMode: 'external-elevated', external: true, elevated: true, canQuit: false })
    const w = await mountView()
    await w.find('.elev-choice .btn-primary').trigger('click')
    await flushAll()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('仅以管理员启动 RAMMap')
    expect(confirmState.options.confirmLabel).toBe('弹 UAC 启动')
    settleConfirm(true)
    await flushAll()
    expect(svc.OpenWindowElevated).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('已提权宿主：选择器隐身（不谎报提权需求）', async () => {
    stubBase({ state: 'stopped' })
    svc.ElevationStatus.mockResolvedValue({ requiresElevation: true, hostElevated: true, executionLevel: 'requireAdministrator' })
    appSvc.IsElevated.mockResolvedValue(true)
    const w = await mountView()
    expect(w.find('.elev-choice').exists()).toBe(false)
    w.unmount()
  })

  it('running：选择器缺席（A/B 引导只服务 stopped/failed 决策点）', async () => {
    stubBase({ state: 'running', version: '2025-01-01', pid: 42, startedAt: new Date().toISOString() })
    const w = await mountView()
    expect(w.find('.elev-choice').exists()).toBe(false)
    w.unmount()
  })
})
