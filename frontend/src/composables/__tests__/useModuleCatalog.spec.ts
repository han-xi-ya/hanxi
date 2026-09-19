// useModuleCatalog 特征测试：两源（ListCatalog+ListModuleStates）并发合并、
// ext:changed 防抖重拉（突发只拉一次）、单飞合流、错误不抛只挂 error 且保留旧投影。
// 描述文案随 ModuleCatalogItem.description 契约收编，不再借第三源 ListModules。
// bindings 经 vi.mock 打桩（FRONTEND.md §8 测试 seam），单例状态经 resetModules 逐例隔离。
import { beforeEach, describe, expect, it, vi } from 'vitest'

const appSvc = vi.hoisted(() => ({
  ListCatalog: vi.fn(),
  ListModuleStates: vi.fn(),
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

async function freshModule(): Promise<typeof import('../useModuleCatalog')> {
  vi.resetModules()
  return import('../useModuleCatalog')
}

function catalogItem(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    name: `模块 ${id}`,
    description: `这是 ${id} 的功能描述`,
    category: 'efficiency',
    delivery: 'builtin-logical',
    capabilities: ['tray-commands'],
    entrypoints: ['rpc', 'navigation'],
    compatibility: { hostRange: '*', platform: ['windows'] },
    permissions: [],
    owner: 'hanxi',
    ...over,
  }
}

function stateOf(id: string, over: Record<string, unknown> = {}) {
  return {
    schema: 1,
    moduleId: id,
    delivery: 'installed',
    policy: 'enabled',
    runtime: 'active',
    health: 'current',
    primaryAction: 'open',
    summary: 'running',
    ...over,
  }
}

function stubTwoSources(catalog: unknown[], states: unknown[]) {
  appSvc.ListCatalog.mockResolvedValue(catalog)
  appSvc.ListModuleStates.mockResolvedValue(states)
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
  runtime.handlers = {}
})

describe('useModuleCatalog', () => {
  it('并发拉取两源并按 moduleId 合并为 ModuleEntry[]（描述随目录项到达）', async () => {
    const mod = await freshModule()
    stubTwoSources(
      [catalogItem('memo', { description: '随手记描述' }), catalogItem('wifi', { description: 'WiFi 描述' })],
      [stateOf('memo')],
    )
    const { entries, loaded, loading, error, refresh } = mod.useModuleCatalog()
    await refresh()
    expect(appSvc.ListCatalog).toHaveBeenCalledTimes(1)
    expect(appSvc.ListModuleStates).toHaveBeenCalledTimes(1)
    expect(entries.value).toHaveLength(2)
    expect(entries.value[0].catalog.id).toBe('memo')
    expect(entries.value[0].state?.moduleId).toBe('memo')
    expect(entries.value[0].catalog.description).toBe('随手记描述')
    // 目录驱动：无状态投影的模块仍在列（state 为 null，由卡片显示"状态未知"）
    expect(entries.value[1].state).toBeNull()
    expect(loaded.value).toBe(true)
    expect(loading.value).toBe(false)
    expect(error.value).toBeNull()
  })

  it('拉取失败：error 挂出、不抛错、保留旧投影（stale 语义），下次成功即恢复', async () => {
    const mod = await freshModule()
    stubTwoSources([catalogItem('memo')], [stateOf('memo')])
    const { entries, error, loaded, refresh } = mod.useModuleCatalog()
    await refresh()
    expect(entries.value).toHaveLength(1)
    appSvc.ListModuleStates.mockRejectedValueOnce(new Error('投影面炸了'))
    await refresh()
    expect(error.value).toContain('投影面炸了')
    expect(entries.value).toHaveLength(1)
    expect(loaded.value).toBe(true)
    await refresh()
    expect(error.value).toBeNull()
  })

  it('首调用 useModuleCatalog() 即触发冷加载并订阅 ext:changed', async () => {
    const mod = await freshModule()
    stubTwoSources([catalogItem('memo')], [stateOf('memo')])
    const { entries, refresh } = mod.useModuleCatalog()
    await refresh()
    expect(entries.value).toHaveLength(1)
    expect(runtime.handlers['ext:changed']).toBeTypeOf('function')
  })

  it('ext:changed 突发 3 连击只防抖重拉一次，新投影经重拉到达（无本地 patch）', async () => {
    vi.useFakeTimers()
    try {
      const mod = await freshModule()
      stubTwoSources(
        [catalogItem('memo')],
        [stateOf('memo', { summary: 'running', primaryAction: 'open' })],
      )
      const { entries } = mod.useModuleCatalog()
      await vi.advanceTimersByTimeAsync(0) // 放行冷加载微任务
      expect(entries.value[0].state?.summary).toBe('running')
      const before = appSvc.ListCatalog.mock.calls.length

      // 窗口内多次广播：不应立刻重拉
      runtime.handlers['ext:changed']()
      runtime.handlers['ext:changed']()
      runtime.handlers['ext:changed']()
      expect(appSvc.ListCatalog.mock.calls.length).toBe(before)

      // 后端真实变化（停用）：只有重拉才拿得到——证明前端不手工改缓存
      appSvc.ListModuleStates.mockResolvedValue([
        stateOf('memo', {
          summary: 'installed-disabled',
          policy: 'disabled',
          runtime: 'inactive',
          primaryAction: 'enable',
        }),
      ])
      await vi.advanceTimersByTimeAsync(300)
      expect(appSvc.ListCatalog.mock.calls.length).toBe(before + 1)
      expect(entries.value[0].state?.summary).toBe('installed-disabled')
    } finally {
      vi.useRealTimers()
    }
  })

  it('并发 refresh 合流为单飞（in-flight 复用）', async () => {
    const mod = await freshModule()
    let resolveCatalog: (v: unknown) => void = () => {}
    appSvc.ListCatalog.mockReturnValue(new Promise((r) => { resolveCatalog = r }))
    appSvc.ListModuleStates.mockResolvedValue([])
    const { refresh } = mod.useModuleCatalog() // 冷加载即占住 inflight
    const second = refresh()
    expect(appSvc.ListCatalog.mock.calls.length).toBe(1)
    resolveCatalog([])
    await Promise.all([refresh(), second])
    expect(appSvc.ListCatalog.mock.calls.length).toBe(1)
  })
})
