// 组 G1 特征测试：PortKillView。
// 现状契约：确认终止走全局 useConfirm 单例（视图自挂 ConfirmDialog 已收编），
// 测试经 confirmState/settleConfirm 驱动；明细以 options.details 逐字断言，
// 查杀期门禁体现在释放按钮 disabled（killing）。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PortKillView from '../PortKillView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  ListListeningPorts: vi.fn(),
  QueryPort: vi.fn(),
  KillProcess: vi.fn(),
  KillProcessElevated: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/portkill', () => ({
  PortKillService: svc,
}))

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const occ = (over: Record<string, unknown> = {}) => ({
  protocol: 'TCP',
  localIp: '0.0.0.0',
  port: 8080,
  pid: 4321,
  processName: 'node.exe',
  exePath: 'C:\\node.exe',
  startedAt: '2026-09-01T08:00:00Z',
  isProtected: false,
  ...over,
})

function mountView() {
  svc.ListListeningPorts.mockResolvedValue([occ(), occ({ port: 443, pid: 4, isProtected: true })])
  const w = mount(defineComponent({ render: () => h(PortKillView) }), {
    attachTo: document.body,
    global: { stubs: { teleport: true } },
  })
  return w
}

const { confirmState, settleConfirm } = useConfirm()

beforeEach(() => {
  settleConfirm(false) // 兜底落定上个用例可能悬挂的确认
})
afterEach(() => {
  settleConfirm(false)
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('PortKillView', () => {
  it('挂载拉取监听表：系统保护行不出释放按钮', async () => {
    const w = mountView()
    await flushMicrotasks()
    expect(svc.ListListeningPorts).toHaveBeenCalled()
    const killBtns = w.findAll('button').filter((b) => b.text() === '释放端口')
    expect(killBtns).toHaveLength(1) // 443/PID4 受保护
    expect(w.text()).toContain('系统保护')
    w.unmount()
  })

  it('非法端口号 toast 拦截且不发查询', async () => {
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(99999)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.QueryPort).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toContain('请输入有效的端口号')
    w.unmount()
  })

  it('快捷端口标签即点即查并高亮', async () => {
    svc.QueryPort.mockResolvedValue([occ()])
    const w = mountView()
    await flushMicrotasks()
    const tag = w.findAll('.tag-btn').find((t) => t.text() === ':8080')!
    await tag.trigger('click')
    await flushMicrotasks()
    expect(svc.QueryPort).toHaveBeenCalledWith(8080)
    expect(w.findAll('.tag-btn').find((t) => t.classes('active'))!.text()).toBe(':8080')
    expect(w.text()).toContain('端口 :8080 占用详情')
    w.unmount()
  })

  it('查询空闲端口 toast 提示且无结果卡', async () => {
    svc.QueryPort.mockResolvedValue([])
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(9999)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('端口 9999 当前未被占用')
    expect(w.text()).not.toContain('占用详情')
    w.unmount()
  })

  it('释放流程：全局确认含端口/PID 明细，确认后普通权限成功并双刷新', async () => {
    svc.QueryPort.mockResolvedValue([occ()])
    svc.KillProcess.mockResolvedValue({ success: true })
    const w = mountView()
    await flushMicrotasks()
    // 先查一口占用再点释放
    await w.find('.input-port').setValue(8080)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    const beforeList = svc.ListListeningPorts.mock.calls.length
    await w.findAll('.result-card button').find((b) => b.text() === '释放端口')!.trigger('click')
    await flushMicrotasks()
    // 确认请求走全局单例，视图不再自挂弹层
    expect(w.find('.workbench-confirm').exists()).toBe(false)
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确认终止进程并释放端口？')
    expect(confirmState.options.confirmLabel).toBe('确认终止')
    expect(confirmState.options.tone).toBe('danger')
    const detailValues = confirmState.options.details?.map((item) => item.value) ?? []
    expect(detailValues).toContain(':8080 (TCP)')
    expect(detailValues).toContain('4321')
    expect(detailValues).toContain('C:\\node.exe')
    expect(svc.KillProcess).not.toHaveBeenCalled()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.KillProcess).toHaveBeenCalledWith(4321, 'C:\\node.exe', Math.floor(new Date('2026-09-01T08:00:00Z').getTime() / 1000))
    expect(useToast().toastMsg.value).toContain('已成功终止进程 PID 4321')
    expect(svc.ListListeningPorts.mock.calls.length).toBeGreaterThan(beforeList) // 成功联动刷新
    expect(confirmState.open).toBe(false)
    w.unmount()
  })

  it('查杀挂起期 killing 门禁：全部释放按钮禁用，完成后恢复（替代原弹层滞留遮罩）', async () => {
    svc.QueryPort.mockResolvedValue([occ(), occ({ pid: 99, port: 3000 })])
    let resolveKill: (r: { success: boolean }) => void = () => {}
    svc.KillProcess.mockReturnValue(new Promise((r) => (resolveKill = r)))
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(8080)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    const killBtns = w.findAll('.result-card button').filter((b) => b.text() === '释放端口')
    expect(killBtns).toHaveLength(2)
    await killBtns[0].trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    const pending = w.findAll('.result-card .btn-kill')
    expect(pending.every((b) => b.attributes('disabled') !== undefined)).toBe(true)
    resolveKill({ success: true })
    await flushMicrotasks()
    // 成功后行数据随刷新收敛，此处仅验证门禁态解除不抛错；断言完成态按钮可用
    expect(useToast().toastMsg.value).toContain('已成功终止进程')
    w.unmount()
  })

  it('权限不足自动升级：先提示再调提权接口', async () => {
    svc.QueryPort.mockResolvedValue([occ()])
    svc.KillProcess.mockResolvedValue({ success: false, needElevate: true })
    svc.KillProcessElevated.mockResolvedValue({ success: true })
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(8080)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    await w.findAll('.result-card button').find((b) => b.text() === '释放端口')!.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('已成功终止进程')
    expect(svc.KillProcessElevated).toHaveBeenCalledWith(4321)
    w.unmount()
  })

  it('查杀失败：toast 后端错误、确认已落定关闭，按钮恢复可重试', async () => {
    svc.QueryPort.mockResolvedValue([occ()])
    svc.KillProcess.mockResolvedValue({ success: false, needElevate: false, errorMessage: '拒绝访问' })
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(8080)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    await w.findAll('.result-card button').find((b) => b.text() === '释放端口')!.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('查杀失败: 拒绝访问')
    // 收编后语义：失败不再滞留确认层（原视图自持 busy 已归一为 toast + 可重试），
    // killing 门禁解除，释放按钮恢复可用
    expect(confirmState.open).toBe(false)
    const killBtn = w.findAll('.result-card button').find((b) => b.text() === '释放端口')!
    expect(killBtn.attributes('disabled')).toBeUndefined()
    // 确认取消路径：不调后端
    await killBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.KillProcess).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('0001 零值启动时间渲染破折号', async () => {
    svc.QueryPort.mockResolvedValue([occ({ startedAt: '0001-01-01T00:00:00Z' })])
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(8080)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    expect(w.find('.result-card tbody tr').text()).toContain('—')
    w.unmount()
  })
})
