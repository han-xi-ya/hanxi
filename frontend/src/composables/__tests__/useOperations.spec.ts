// useOperations 特征测试（Wave 4）：ListOperations 单源冷加载、operation:changed
// 防抖重拉（突发只拉一次、新投影只经重拉到达——前端不本地 patch）、
// active/resumable/recentFinished 三类只读筛选语义、失败保留旧投影 + stale 旗标、
// resumableTxnID 剥回灌前缀。bindings 经 vi.mock 打桩，单例状态逐例 resetModules。
import { beforeEach, describe, expect, it, vi } from 'vitest'

const appSvc = vi.hoisted(() => ({
  ListOperations: vi.fn(),
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
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

async function freshModule(): Promise<typeof import('../useOperations')> {
  vi.resetModules()
  return import('../useOperations')
}

function op(over: Record<string, unknown> = {}) {
  return {
    schema: 1,
    id: 'op-1',
    moduleId: 'markeron',
    kind: 'install',
    phase: 'download',
    status: 'running',
    progress: 42,
    cancellable: true,
    startedAt: '2026-09-18T10:00:00+08:00',
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
  runtime.handlers = {}
})

describe('useOperations', () => {
  it('冷加载 ListOperations 全量投影，并订阅 operation:changed', async () => {
    const mod = await freshModule()
    appSvc.ListOperations.mockResolvedValue([op(), op({ id: 'op-2', status: 'succeeded' })])
    const { operations, loaded, loading, error, refresh } = mod.useOperations()
    await refresh()
    expect(appSvc.ListOperations).toHaveBeenCalledTimes(1)
    expect(operations.value).toHaveLength(2)
    expect(loaded.value).toBe(true)
    expect(loading.value).toBe(false)
    expect(error.value).toBeNull()
    expect(runtime.handlers['operation:changed']).toBeTypeOf('function')
  })

  it('operation:changed 突发 3 连击只防抖重拉一次，新投影经重拉到达（无本地 patch）', async () => {
    vi.useFakeTimers()
    try {
      const mod = await freshModule()
      appSvc.ListOperations.mockResolvedValue([op({ status: 'running', progress: 10 })])
      const { active } = mod.useOperations()
      await vi.advanceTimersByTimeAsync(0) // 放行冷加载微任务
      expect(active.value[0].progress).toBe(10)
      const before = appSvc.ListOperations.mock.calls.length

      runtime.handlers['operation:changed']()
      runtime.handlers['operation:changed']()
      runtime.handlers['operation:changed']()
      expect(appSvc.ListOperations.mock.calls.length).toBe(before)

      // 后端真实推进：只有重拉才拿得到——证明前端不手工改缓存
      appSvc.ListOperations.mockResolvedValue([op({ status: 'running', progress: 88 })])
      await vi.advanceTimersByTimeAsync(300)
      expect(appSvc.ListOperations.mock.calls.length).toBe(before + 1)
      expect(active.value[0].progress).toBe(88)
    } finally {
      vi.useRealTimers()
    }
  })

  it('active = queued ∪ running（保持后端登记序），resumable = error.code==="resumable"', async () => {
    const mod = await freshModule()
    appSvc.ListOperations.mockResolvedValue([
      op({ id: 'a', status: 'queued', progress: null, phase: 'resolve' }),
      op({ id: 'b', status: 'running' }),
      op({ id: 'c', status: 'succeeded', phase: 'done' }),
      op({
        id: 'resumed-txn-9',
        status: 'failed',
        phase: 'unpack',
        error: { code: 'resumable', message: '上次进程未收口的托管事务', recoverable: true },
      }),
      op({
        id: 'd',
        status: 'failed',
        error: { code: 'disk-full', message: '磁盘空间不足', recoverable: false },
      }),
    ])
    const { active, resumable, operations, refresh } = mod.useOperations()
    await refresh()
    expect(active.value.map((o) => o.id)).toEqual(['a', 'b'])
    expect(resumable.value.map((o) => o.id)).toEqual(['resumed-txn-9'])
    // 只读筛选不改写投影本体
    expect(operations.value).toHaveLength(5)
  })

  it('recentFinished 只收终态、按收口时间倒序截断，activeOf 命中该模块在途项', async () => {
    const mod = await freshModule()
    appSvc.ListOperations.mockResolvedValue([
      op({ id: 'run', status: 'running', moduleId: 'rufus' }),
      op({ id: 'old', status: 'succeeded', phase: 'done', finishedAt: '2026-09-17T10:00:00+08:00' }),
      op({ id: 'new', status: 'failed', phase: 'place', finishedAt: '2026-09-18T09:00:00+08:00' }),
      op({ id: 'mid', status: 'cancelled', finishedAt: '2026-09-17T23:00:00+08:00' }),
    ])
    const { recentFinished, activeOf, refresh } = mod.useOperations()
    await refresh()
    expect(recentFinished(2).map((o) => o.id)).toEqual(['new', 'mid'])
    expect(recentFinished(10).map((o) => o.id)).toEqual(['new', 'mid', 'old'])
    expect(recentFinished(0)).toEqual([])
    expect(activeOf('rufus')?.id).toBe('run')
    expect(activeOf('memo')).toBeNull()
  })

  it('拉取失败：error 挂出、不抛错、保留旧投影，stale 旗标在位；下次成功即恢复', async () => {
    const mod = await freshModule()
    appSvc.ListOperations.mockResolvedValue([op()])
    const { operations, error, stale, refresh } = mod.useOperations()
    await refresh()
    expect(stale.value).toBe(false)
    appSvc.ListOperations.mockRejectedValueOnce(new Error('观察面炸了'))
    await refresh()
    expect(error.value).toContain('观察面炸了')
    expect(stale.value).toBe(true) // 有旧数据 + 刷新失败
    expect(operations.value).toHaveLength(1) // 保留旧投影
    await refresh()
    expect(error.value).toBeNull()
    expect(stale.value).toBe(false)
  })

  it('并发 refresh 合流为单飞（in-flight 复用）', async () => {
    const mod = await freshModule()
    let resolveList: (v: unknown) => void = () => {}
    appSvc.ListOperations.mockReturnValue(new Promise((r) => { resolveList = r }))
    const { refresh } = mod.useOperations() // 冷加载即占住 inflight
    const second = refresh()
    expect(appSvc.ListOperations.mock.calls.length).toBe(1)
    resolveList([])
    await Promise.all([refresh(), second])
    expect(appSvc.ListOperations.mock.calls.length).toBe(1)
  })

  it('resumableTxnID 优先取契约字段 txnId，无则剥回灌前缀降级', async () => {
    const mod = await freshModule()
    // txnId 存在：直接返回裸事务 ID，即便 id 是展示合成键。
    expect(
      mod.resumableTxnID(
        op({ id: 'resumed-7c9e6679', txnId: '7c9e6679-7425-40de-944b-e07fc1f90ae7' }) as never,
      ),
    ).toBe('7c9e6679-7425-40de-944b-e07fc1f90ae7')
    // 无 txnId（旧投影降级）：剥 "resumed-" 前缀。
    expect(mod.resumableTxnID(op({ id: 'resumed-abc' }) as never)).toBe('abc')
    // 无 txnId 且非回灌：原样返回。
    expect(mod.resumableTxnID(op({ id: 'live-txn-1' }) as never)).toBe('live-txn-1')
  })
})
