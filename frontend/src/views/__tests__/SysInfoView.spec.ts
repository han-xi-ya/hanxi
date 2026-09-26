// 特征测试（N23 升级）：内存卡"清理账目透明 + 清空工作集次级钮"。
// 断言对象：双钮均为 btn-secondary 形态、确认框（工作集为 danger 且预告变卡）、
// 逐项链账渲染（实测/降级两口径）、工作集回执计数、两动作 UI 单飞、失败告警。
// 确认交互经全局 useConfirm 单例（confirmState/settleConfirm）驱动，同 EnvCheck 家族。
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SysInfoView from '../SysInfoView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const api = vi.hoisted(() => ({
  GetReport: vi.fn(),
  PurgeStandby: vi.fn(),
  EmptyWorkingSets: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/sysinfo/sysinfoservice', () => api)

const KB = 1024
const MB = 1024 * KB
const GB = 1024 * MB

function baseReport() {
  return {
    machine: { hostname: 'TESTBOX', manufacturer: '', model: '', biosVendor: '', biosVersion: '', productId: '', systemSku: '' },
    os: { productName: 'Windows 11 Pro', edition: 'Pro', version: '23H2', build: '22631', installDate: '', is64Bit: true, uptime: '1天', uptimeSeconds: 86400 },
    cpu: { name: 'Test CPU', vendor: 'TestVendor', speedMHz: 3200, cores: 8, logical: 16, sockets: 1 },
    memory: { totalBytes: 32 * GB, availableBytes: 4 * GB, loadPercent: 87, commitTotal: 6 * GB, commitLimit: 37 * GB },
    gpus: [] as unknown[],
    displays: [] as unknown[],
    volumes: [] as unknown[],
    network: [] as unknown[],
    errors: [] as string[],
  }
}

const { confirmState, settleConfirm } = useConfirm()

let wrapper: VueWrapper | null = null

async function mountView() {
  wrapper = mount(SysInfoView, { attachTo: document.body, global: { stubs: { teleport: true } } })
  await flushPromises()
  return wrapper
}

function actionButtons(w: VueWrapper) {
  const nodes = w.findAll('.sys-card-actions button')
  expect(nodes.length).toBe(2)
  return nodes
}

/** 点钮 → 等确认框打开 → 落定 → 等动作完成 */
async function clickAndConfirm(w: VueWrapper, index: number, accept: boolean) {
  await actionButtons(w)[index].trigger('click')
  await nextTick()
  expect(confirmState.open).toBe(true)
  settleConfirm(accept)
  await flushPromises()
}

beforeEach(() => {
  api.GetReport.mockResolvedValue(baseReport())
})

afterEach(() => {
  settleConfirm(false) // 兜底落定未决确认，防跨用例悬挂
  useToast().clearToast()
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

describe('内存卡双清理钮形态', () => {
  it('两钮均为 btn-secondary（无 primary），工作集钮独立存在', async () => {
    const w = await mountView()
    const [standby, ws] = actionButtons(w)
    expect(standby.classes()).toContain('btn-secondary')
    expect(ws.classes()).toContain('btn-secondary')
    expect(w.find('.btn-primary').exists()).toBe(false)
    expect(standby.text()).toContain('清理可回收内存')
    expect(ws.text()).toContain('清空工作集')
  })

  it('取消工作集确认不发 RPC', async () => {
    const w = await mountView()
    await clickAndConfirm(w, 1, false)
    expect(api.EmptyWorkingSets).not.toHaveBeenCalled()
  })
})

describe('待机清理：先量后清再对账', () => {
  const measuredResult = {
    beforeAvailableBytes: 4 * GB,
    afterAvailableBytes: 11 * GB,
    availableDeltaBytes: 7 * GB,
    success: true,
    usedHelper: false,
    elevated: true,
    message: 'ok',
    beforeTotalBytes: 32 * GB,
    pagesMeasured: true,
    beforeStandbyBytes: 8 * GB,
    afterStandbyBytes: GB,
    beforeModifiedBytes: 512 * MB,
    afterModifiedBytes: 640 * MB,
    processesEmptied: 0,
    processesSkipped: 0,
  }

  it('实测账目逐项送达：待机前后、修改页、可用增量、范围说明', async () => {
    api.PurgeStandby.mockResolvedValue(measuredResult)
    const w = await mountView()
    await clickAndConfirm(w, 0, true)
    const text = w.text()
    expect(text).toContain('清理前待机 8.0 GB → 后 1.0 GB')
    expect(text).toContain('修改页 512.0 MB → 640.0 MB')
    expect(text).toContain('可用 +7.0 GB')
    expect(text).toContain('压缩存储（内存压缩）本按钮不触碰')
    expect(useToast().toastMsg.value).toContain('清理完成')
  })

  it('拿不到页列表实测时如实降级，不装懂', async () => {
    api.PurgeStandby.mockResolvedValue({ ...measuredResult, pagesMeasured: false, beforeStandbyBytes: 0, afterStandbyBytes: 0, beforeModifiedBytes: 0, afterModifiedBytes: 0 })
    const w = await mountView()
    await clickAndConfirm(w, 0, true)
    const text = w.text()
    expect(text).toContain('拿不到实测')
    expect(text).not.toContain('清理前待机')
    expect(text).toContain('可用 +7.0 GB')
  })

  it('失败回执走红色告警行而非账目行', async () => {
    api.PurgeStandby.mockResolvedValue({ ...measuredResult, pagesMeasured: false, success: false, message: '当前令牌未持有 SeProfileSingleProcessPrivilege 权限' })
    const w = await mountView()
    await clickAndConfirm(w, 0, true)
    expect(w.find('.sys-action-error').text()).toContain('SeProfileSingleProcessPrivilege')
    expect(w.text()).not.toContain('清理前待机')
  })
})

describe('清空工作集：谨慎语义次级钮', () => {
  const wsResult = {
    beforeAvailableBytes: 2 * GB,
    afterAvailableBytes: 6 * GB,
    availableDeltaBytes: 4 * GB,
    success: true,
    usedHelper: true,
    elevated: true,
    message: 'ok',
    beforeTotalBytes: 32 * GB,
    pagesMeasured: true,
    beforeStandbyBytes: 6 * GB,
    afterStandbyBytes: 9 * GB,
    beforeModifiedBytes: MB,
    afterModifiedBytes: 2 * MB,
    processesEmptied: 97,
    processesSkipped: 12,
  }

  it('确认框为 danger 且预告变卡；回执含成功/跳过计数', async () => {
    api.EmptyWorkingSets.mockResolvedValue(wsResult)
    const w = await mountView()
    await actionButtons(w)[1].trigger('click')
    await nextTick()
    expect(confirmState.options.title).toContain('清空全部进程工作集')
    expect(confirmState.options.tone).toBe('danger')
    expect(confirmState.options.description).toContain('普遍变卡')
    expect(confirmState.options.description).toContain('只想立刻看数字大跌时按')
    settleConfirm(true)
    await flushPromises()
    expect(api.EmptyWorkingSets).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('已收 97 个进程工作集（跳过 12 个')
    expect(useToast().toastMsg.value).toContain('可用内存 +4.0 GB')
    expect(w.text()).toContain('已收 97 个进程工作集，跳过 12 个')
    expect(w.text()).toContain('压回待机列表（不是释放）')
  })
})

describe('两清理动作共享单飞', () => {
  it('一个动作在飞时另一钮禁用，落地后恢复', async () => {
    let release: (v: unknown) => void = () => {}
    api.PurgeStandby.mockImplementation(() => new Promise((r) => { release = r }))
    const w = await mountView()
    await actionButtons(w)[0].trigger('click')
    await nextTick()
    settleConfirm(true)
    await flushPromises()
    const [standby, ws] = actionButtons(w)
    expect(standby.attributes('disabled')).toBeDefined()
    expect(ws.attributes('disabled')).toBeDefined()
    expect(standby.text()).toContain('清理中…')
    release({ beforeAvailableBytes: 0, afterAvailableBytes: 0, availableDeltaBytes: 0, success: true, usedHelper: false, elevated: true, message: 'ok', pagesMeasured: false })
    await flushPromises()
    expect(ws.attributes('disabled')).toBeUndefined()
  })
})
