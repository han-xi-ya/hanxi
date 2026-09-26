// OperationBanner 特征测试（Wave 4）：
// - 空数据（无 resumable / 无 active / 无错误）整件不渲染，不放占位；
// - resumable：告警条逐条「模块名 + 上次操作失败于 phase + 原因文案」，
//   [前往重试] 上抛 /ext/<id>，[忽略残留] 经确认框调 DismissResumable（消费契约 txnId）
//   + toast；"单写约束"文案按 journal 态区分（F-a）：pending/running（含态锚点
//   读不出的保守档）保留占位警告，compensated 改口"不占名额、忽略仅为收口账目"；
// - active：information 条渲染 UiProgressBar（percent=progress），
//   cancellable=false 不伪装可取消（无取消钮 + 明说不可取消）；
// - 刷新失败保留旧投影：stale 错误条 + 重试。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'

const appSvc = vi.hoisted(() => ({
  ListOperations: vi.fn(),
  ListCatalog: vi.fn(),
  ListModuleStates: vi.fn(),
  DismissResumable: vi.fn(),
  CancelOperation: vi.fn(),
}))
const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, () => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: () => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

let banner: Component
async function setup() {
  vi.resetModules()
  vi.clearAllMocks()
  runtime.handlers = {}
  banner = (await import('../OperationBanner.vue')).default
  appSvc.ListCatalog.mockResolvedValue([
    {
      id: 'markeron', name: 'MarkerOn 标注', description: '', category: 'desktop',
      deliveryKind: 'builtin-logical', capabilities: [], entrypoints: [],
      compatibility: { hostRange: '*', platform: ['windows'] }, permissions: [], owner: 'hanxi',
    },
  ])
  appSvc.ListModuleStates.mockResolvedValue([])
  appSvc.DismissResumable.mockResolvedValue(null)
  appSvc.CancelOperation.mockResolvedValue(null)
}

function runningOp(over: Record<string, unknown> = {}) {
  return {
    schema: 1, id: 'run-1', moduleId: 'markeron', kind: 'install', phase: 'download',
    status: 'running', progress: 42, cancellable: true, startedAt: '2026-09-18T10:00:00+08:00',
    ...over,
  }
}
// 默认按占写位（journal state=running）形态：与后端 refilledOperation 的
// 「journal state=X」锚点拼法一致（operation/hub.go），单写警告分支消费。
function resumableOp(over: Record<string, unknown> = {}) {
  return {
    schema: 1, id: 'resumed-abc123', txnId: 'abc123', moduleId: 'markeron', kind: 'install', phase: 'download',
    status: 'failed', cancellable: false, startedAt: '2026-09-18T09:00:00+08:00',
    error: { code: 'resumable', message: '上次进程未收口的托管事务（journal state=running, phase=download），等待启动恢复处理', recoverable: true },
    ...over,
  }
}
/** compensated 形态：恢复流已接管补偿，不占写位（store.go 单写判定只看 pending/running）。 */
function compensatedOp(over: Record<string, unknown> = {}) {
  return resumableOp({
    error: { code: 'resumable', message: '上次进程未收口的托管事务（journal state=compensated, phase=remove），等待启动恢复处理', recoverable: true },
    ...over,
  })
}

async function mountBanner() {
  const w = mount(banner, { attachTo: document.body })
  await flushPromises()
  return w
}

beforeEach(setup)
afterEach(() => {
  document.body.innerHTML = ''
})

describe('OperationBanner', () => {
  it('空数据（无在途/无残留/无错误）不渲染任何条', async () => {
    appSvc.ListOperations.mockResolvedValue([])
    const w = await mountBanner()
    expect(w.find('.operation-banner').exists()).toBe(false)
    expect(w.text()).toBe('')
    w.unmount()
  })

  it('resumable（pending/running 占位态）：告警条呈现模块名/失败阶段/原因 + 单写约束如实文案', async () => {
    appSvc.ListOperations.mockResolvedValue([resumableOp()])
    const w = await mountBanner()
    expect(w.find('.op-resume').exists()).toBe(true)
    const row = w.find('.op-resume .op-item')
    expect(row.text()).toContain('MarkerOn 标注')
    expect(row.text()).toContain('安装')
    expect(row.text()).toContain('下载') // phase=download → 中文词表
    expect(row.text()).toContain('上次进程未收口的托管事务') // error.message 原文
    // 单写约束文案在位（journal state=running 占写位 → 忽略前无法开始新的安装事务）
    expect(w.find('.op-resume .op-note').text()).toContain('无法开始新的安装事务')
    w.unmount()
  })

  it('resumable（compensated 态）：不占写位——收口注记改口，不再声称无法开新事务', async () => {
    appSvc.ListOperations.mockResolvedValue([compensatedOp()])
    const w = await mountBanner()
    const note = w.find('.op-resume .op-note').text()
    expect(note).toContain('不占用新事务名额')
    expect(note).toContain('忽略仅为收口账目')
    expect(note).not.toContain('无法开始新的安装事务') // overstated 警告不得出现在补偿态
    w.unmount()
  })

  it('resumable（compensated 态）：确认框"对新事务影响"如实给不占位口径', async () => {
    const { useConfirm } = await import('../../../composables/useConfirm')
    appSvc.ListOperations.mockResolvedValue([compensatedOp()])
    const w = await mountBanner()
    await w.findAll('.op-actions button').find((b) => b.text() === '忽略残留')!.trigger('click')
    await flushPromises()
    const { confirmState, settleConfirm } = useConfirm()
    expect(confirmState.open).toBe(true)
    const impact = (confirmState.options.details ?? []).find((d) => d.label === '对新事务影响')
    expect(impact?.value).toContain('不占用写位')
    expect(impact?.value).toContain('忽略仅为收口这笔账目')
    // 收口前限制的占位断言不得出现在补偿态确认框里
    expect((confirmState.options.details ?? []).some((d) => d.value.includes('无法开始新的安装事务'))).toBe(false)
    settleConfirm(false)
    await flushPromises()
    w.unmount()
  })

  it('resumable（混态）：存在占写位残留时整条注记保守保留单写警告', async () => {
    appSvc.ListOperations.mockResolvedValue([
      compensatedOp({ id: 'resumed-c', txnId: 'c' }),
      resumableOp(), // running 占位
    ])
    const w = await mountBanner()
    expect(w.find('.op-resume .op-note').text()).toContain('无法开始新的安装事务')
    w.unmount()
  })

  it('resumable：[前往重试] 上抛 navigate /ext/markeron', async () => {
    appSvc.ListOperations.mockResolvedValue([resumableOp()])
    const w = await mountBanner()
    const goto = w.findAll('.op-actions button').find((b) => b.text() === '前往重试')!
    await goto.trigger('click')
    expect(w.emitted('navigate')?.[0]).toEqual(['/ext/markeron'])
    w.unmount()
  })

  it('resumable：[忽略残留] 弹确认（不可翻案如实口径）→ DismissResumable(契约 txnId) + toast', async () => {
    const { useConfirm } = await import('../../../composables/useConfirm')
    const { useToast } = await import('../../../composables/useToast')
    appSvc.ListOperations.mockResolvedValue([resumableOp()])
    const w = await mountBanner()
    const ignore = w.findAll('.op-actions button').find((b) => b.text() === '忽略残留')!
    await ignore.trigger('click')
    await flushPromises()
    const { confirmState, settleConfirm } = useConfirm()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('不可翻案')
    settleConfirm(true)
    await flushPromises()
    expect(appSvc.DismissResumable).toHaveBeenCalledWith('abc123')
    expect(useToast().toastMsg.value).toContain('已忽略「MarkerOn 标注」的残留事务')
    w.unmount()
  })

  it('resumable：确认取消 → 不发忽略 RPC', async () => {
    const { useConfirm } = await import('../../../composables/useConfirm')
    appSvc.ListOperations.mockResolvedValue([resumableOp()])
    const w = await mountBanner()
    await w.findAll('.op-actions button').find((b) => b.text() === '忽略残留')!.trigger('click')
    await flushPromises()
    const { confirmState, settleConfirm } = useConfirm()
    expect(confirmState.open).toBe(true)
    settleConfirm(false)
    await flushPromises()
    expect(appSvc.DismissResumable).not.toHaveBeenCalled()
    w.unmount()
  })

  it('active：information 条 + UiProgressBar 同步 progress，cancellable=true 不出现"不可取消"提示', async () => {
    appSvc.ListOperations.mockResolvedValue([runningOp()])
    const w = await mountBanner()
    const bar = w.find('.op-running [role="progressbar"]')
    expect(bar.exists()).toBe(true)
    expect(bar.attributes('aria-valuenow')).toBe('42')
    expect(w.find('.op-running').text()).toContain('42%')
    expect(w.find('.op-running').text()).toContain('下载')
    expect(w.find('.op-running').text()).not.toContain('不支持取消')
    w.unmount()
  })

  it('active：cancellable=false 不伪装可取消（无取消钮，明说不可取消）', async () => {
    appSvc.ListOperations.mockResolvedValue([runningOp({ cancellable: false, progress: null })])
    const w = await mountBanner()
    const row = w.find('.op-running')
    expect(row.text()).toContain('不支持取消')
    expect(row.text()).toContain('进度不可量化') // progress=nil 不编造百分比
    expect(row.findAll('button').some((b) => b.text().includes('取消'))).toBe(false)
    expect(row.find('[role="progressbar"]').exists()).toBe(false)
    w.unmount()
  })

  it('观察面刷新失败：旧投影保留 + 如实标注上一次投影 + 重试', async () => {
    vi.useFakeTimers()
    try {
      appSvc.ListOperations.mockResolvedValue([runningOp()])
      const w = mount(banner, { attachTo: document.body })
      await vi.advanceTimersByTimeAsync(0) // 放行冷加载
      expect(w.find('.op-running').exists()).toBe(true)
      appSvc.ListOperations.mockRejectedValueOnce(new Error('观察面断了'))
      runtime.handlers['operation:changed']()
      await vi.advanceTimersByTimeAsync(300) // 防抖窗口到期 → 重拉失败
      expect(w.find('.op-stale').text()).toContain('上一次投影')
      expect(w.find('.op-stale').text()).toContain('观察面断了')
      expect(w.find('.op-running').exists()).toBe(true) // 旧投影仍在
      w.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('N26 在途可取消事务：出「取消」钮 → CancelOperation(moduleId, txnId) + 刷新', async () => {
    appSvc.ListOperations.mockResolvedValue([runningOp({ txnId: 'txn-9' })])
    const w = await mountBanner()
    const btn = w.findAll('.op-actions button').find((b) => b.text().includes('取消'))
    expect(btn, '取消钮缺席').toBeTruthy()
    await btn!.trigger('click')
    await flushPromises()
    expect(appSvc.CancelOperation).toHaveBeenCalledWith('markeron', 'txn-9')
    expect(appSvc.ListOperations.mock.calls.length).toBeGreaterThanOrEqual(2)
    w.unmount()
  })

  it('N26 无 txnId 的在途记录不出取消钮（无法精确匹配就不给按钮）', async () => {
    appSvc.ListOperations.mockResolvedValue([runningOp()])
    const w = await mountBanner()
    expect(w.text()).not.toContain('取消')
    w.unmount()
  })

})
