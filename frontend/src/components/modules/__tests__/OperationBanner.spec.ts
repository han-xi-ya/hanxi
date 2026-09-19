// OperationBanner 特征测试（Wave 4）：
// - 空数据（无 resumable / 无 active / 无错误）整件不渲染，不放占位；
// - resumable：告警条逐条「模块名 + 上次操作失败于 phase + 原因文案」，
//   [前往重试] 上抛 /ext/<id>，[忽略残留] 经确认框调 DismissResumable（消费契约 txnId）
//   + toast，且"单写约束"如实文案在位；
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
      delivery: 'builtin-logical', capabilities: [], entrypoints: [],
      compatibility: { hostRange: '*', platform: ['windows'] }, permissions: [], owner: 'hanxi',
    },
  ])
  appSvc.ListModuleStates.mockResolvedValue([])
  appSvc.DismissResumable.mockResolvedValue(null)
}

function runningOp(over: Record<string, unknown> = {}) {
  return {
    schema: 1, id: 'run-1', moduleId: 'markeron', kind: 'install', phase: 'download',
    status: 'running', progress: 42, cancellable: true, startedAt: '2026-09-18T10:00:00+08:00',
    ...over,
  }
}
function resumableOp(over: Record<string, unknown> = {}) {
  return {
    schema: 1, id: 'resumed-abc123', txnId: 'abc123', moduleId: 'markeron', kind: 'install', phase: 'download',
    status: 'failed', cancellable: false, startedAt: '2026-09-18T09:00:00+08:00',
    error: { code: 'resumable', message: '上次进程未收口的托管事务', recoverable: true },
    ...over,
  }
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

  it('resumable：告警条呈现模块名/失败阶段/原因 + 单写约束如实文案', async () => {
    appSvc.ListOperations.mockResolvedValue([resumableOp()])
    const w = await mountBanner()
    expect(w.find('.op-resume').exists()).toBe(true)
    const row = w.find('.op-resume .op-item')
    expect(row.text()).toContain('MarkerOn 标注')
    expect(row.text()).toContain('安装')
    expect(row.text()).toContain('下载') // phase=download → 中文词表
    expect(row.text()).toContain('上次进程未收口的托管事务') // error.message 原文
    // 单写约束文案在位（忽略前无法开始新的安装事务）
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
})
