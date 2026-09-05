// 组 G1 特征测试：PortKillView——锁迁移前基线。
// 现状契约：确认终止为手搓 modal（迁移后换标准 ConfirmDialog，busy 文案 '终止中…'→
// 对话框标准 '处理中…'）；断言以文本/流程结果为准，跨两种载体兼容。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PortKillView from '../PortKillView.vue'
import { useToast } from '../../composables/useToast'

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
    // teleport 原地渲染：迁前手搓 modal 无影响，迁后 ConfirmDialog 可被 wrapper 查询（双态兼容）
    global: { stubs: { teleport: true } },
  })
  return w
}

afterEach(() => {
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

  it('释放流程：确认弹层含端口/PID 明细，确认后普通权限成功并双刷新', async () => {
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
    // 确认界面出现（modal 或标准对话框，载体兼容断言）
    expect(w.text()).toContain('确认终止进程并释放端口')
    expect(w.text()).toContain('8080')
    expect(w.text()).toContain('4321')
    await w.findAll('button').find((b) => b.text().includes('确认终止'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.KillProcess).toHaveBeenCalledWith(4321, 'C:\\node.exe', Math.floor(new Date('2026-09-01T08:00:00Z').getTime() / 1000))
    expect(useToast().toastMsg.value).toContain('已成功终止进程 PID 4321')
    expect(svc.ListListeningPorts.mock.calls.length).toBeGreaterThan(beforeList) // 成功联动刷新
    expect(w.text()).not.toContain('确认终止进程并释放端口') // 弹层已关
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
    await w.findAll('button').find((b) => b.text().includes('确认终止'))!.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('已成功终止进程')
    expect(svc.KillProcessElevated).toHaveBeenCalledWith(4321)
    w.unmount()
  })

  it('查杀失败展示后端错误且不关确认层？——现状：失败不清 targetToKill（钉死）', async () => {
    svc.QueryPort.mockResolvedValue([occ()])
    svc.KillProcess.mockResolvedValue({ success: false, needElevate: false, errorMessage: '拒绝访问' })
    const w = mountView()
    await flushMicrotasks()
    await w.find('.input-port').setValue(8080)
    await w.findAll('button').find((b) => b.text().includes('查询占用'))!.trigger('click')
    await flushMicrotasks()
    await w.findAll('.result-card button').find((b) => b.text() === '释放端口')!.trigger('click')
    await flushMicrotasks()
    await w.findAll('button').find((b) => b.text().includes('确认终止'))!.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('查杀失败: 拒绝访问')
    // 现状：失败路径不清 targetToKill，确认层仍在（行为如实锁定，是否算 bug 交主线）
    expect(w.text()).toContain('确认终止进程并释放端口')
    // 取消可关
    await w.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    expect(w.text()).not.toContain('确认终止进程并释放端口')
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
