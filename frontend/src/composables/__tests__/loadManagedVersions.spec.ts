import { describe, expect, it, vi } from 'vitest'
import { loadManagedVersions } from '../loadManagedVersions'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

describe('loadManagedVersions', () => {
  it('远程挂起时本地版本先完成并可写回', async () => {
    const remote = deferred<Array<{ version: string }>>()
    const state = { remote: [] as Array<{ version: string }>, local: [] as Array<{ version: string }>, active: '', loading: false, error: '' }
    const done = loadManagedVersions({
      remote: () => remote.promise,
      local: async () => [{ version: 'v1' }],
      active: async () => 'v1',
      setRemote: value => { state.remote = value },
      setLocal: value => { state.local = value },
      setActive: value => { state.active = value },
      setLoading: value => { state.loading = value },
      setError: value => { state.error = value },
    })
    await done
    expect(state.local).toEqual([{ version: 'v1' }])
    expect(state.active).toBe('v1')
    expect(state.loading).toBe(true)
    remote.resolve([{ version: 'v2' }])
    await flush()
    expect(state.remote).toEqual([{ version: 'v2' }])
    expect(state.loading).toBe(false)
  })

  it('数据源同步抛出（非 Promise）也走分支错误：不穿透调用方、loading 收口', async () => {
    const errors: string[] = []
    let loading = true
    await loadManagedVersions({
      // 形如绑定缺席时直接 throw 的实现：旧写法在求值位抛出，穿透整函数
      remote: () => {
        throw new Error('no runtime')
      },
      local: () => {
        throw new Error('no fs')
      },
      active: async () => '',
      setRemote: vi.fn(),
      setLocal: vi.fn(),
      setActive: vi.fn(),
      setLoading: (value) => { loading = value },
      setError: (value) => { if (value) errors.push(value) },
    })
    await flush()
    expect(errors).toContain('读取本地版本失败: no fs')
    expect(errors).toContain('获取远程版本列表失败: no runtime')
    expect(loading).toBe(false) // 远程分支同步抛出不得把 loading 永挂 true
  })

  it('远程失败保留旧清单，本地失败保留旧资产', async () => {
    const setRemote = vi.fn()
    const setLocal = vi.fn()
    const errors: string[] = []
    await loadManagedVersions({
      remote: async () => { throw new Error('offline') },
      local: async () => { throw new Error('disk') },
      active: async () => 'v1',
      setRemote,
      setLocal,
      setActive: vi.fn(),
      setLoading: vi.fn(),
      setError: value => { errors.push(value) },
    })
    await flush()
    expect(setRemote).not.toHaveBeenCalled()
    expect(setLocal).not.toHaveBeenCalled()
    expect(errors).toContain('读取本地版本失败: disk')
    expect(errors).toContain('获取远程版本列表失败: offline')
  })
})

describe('loadManagedVersions 单侧失败与快慢时序', () => {
  it('一源成功一源拒绝（远程拒绝）：本地/active 照常写回，错误账只落远程一条，loading 由远程分支收口', async () => {
    const setRemote = vi.fn()
    const setLocal = vi.fn()
    const setActive = vi.fn()
    const errors: string[] = []
    let loading = true
    await loadManagedVersions({
      remote: async () => { throw new Error('代理断') },
      local: async () => [{ version: 'v1' }],
      active: async () => 'v1',
      setRemote,
      setLocal,
      setActive,
      setLoading: (value) => { loading = value },
      setError: (value) => { if (value) errors.push(value) },
    })
    await flush()
    expect(setLocal).toHaveBeenCalledWith([{ version: 'v1' }])
    expect(setActive).toHaveBeenCalledWith('v1')
    expect(setRemote).not.toHaveBeenCalled() // 失败保留旧清单：不写空数组冒充"远程无版本"
    expect(errors).toEqual(['获取远程版本列表失败: 代理断']) // 本地成功即不叠本地账
    expect(loading).toBe(false)
  })

  it('一源成功一源拒绝（本地拒绝）：远程照常写回且 await 方不被拖挂，错误账只落本地一条', async () => {
    const setRemote = vi.fn()
    const setLocal = vi.fn()
    const setActive = vi.fn()
    const errors: string[] = []
    let loading = true
    await loadManagedVersions({
      remote: async () => [{ version: 'v9' }],
      local: async () => { throw new Error('目录损坏') },
      active: async () => '',
      setRemote,
      setLocal,
      setActive,
      setLoading: (value) => { loading = value },
      setError: (value) => { if (value) errors.push(value) },
    })
    expect(setRemote).toHaveBeenCalledWith([{ version: 'v9' }])
    expect(setLocal).not.toHaveBeenCalled()
    expect(setActive).not.toHaveBeenCalled() // local/active 同括 Promise.all：本地炸则 active 回包也不写
    expect(errors).toEqual(['读取本地版本失败: 目录损坏'])
    expect(loading).toBe(false)
  })

  it('本地慢于远程：远程先回即放行 loading 且本地迟到回包照常写回', async () => {
    let settleLocal!: (value: { version: string }[]) => void
    const local = new Promise<{ version: string }[]>((res) => { settleLocal = res })
    const events: string[] = []
    let loading = true
    const done = loadManagedVersions({
      remote: async () => { events.push('remote:write'); return [{ version: 'v9' }] },
      local: () => local,
      active: async () => { events.push('active:write'); return 'v1' },
      setRemote: () => {},
      setLocal: () => { events.push('local:write') },
      setActive: () => {},
      setLoading: (value) => { events.push(`loading:${value}`); loading = value },
      setError: (value) => { events.push(`error:${value}`) },
    })
    await flush()
    // 远程已了结：loading 先行收口，本地扫描在途不双挂
    expect(loading).toBe(false)
    expect(events).toContain('remote:write')
    expect(events).not.toContain('local:write')

    settleLocal([{ version: 'v1' }])
    await done
    expect(events).toContain('local:write') // 迟到的本地账照样入表（本函数不丢，代次丢弃属 store 面）
  })

  it('数据源回 null/undefined 归一为空数组/空串，不记为失败（Go nil 切片直译形态）', async () => {
    const setRemote = vi.fn()
    const setLocal = vi.fn()
    const setActive = vi.fn()
    const errors: string[] = []
    await loadManagedVersions({
      remote: async () => null,
      local: async () => undefined,
      active: async () => null,
      setRemote,
      setLocal,
      setActive,
      setLoading: vi.fn(),
      setError: (value) => { if (value) errors.push(value) },
    })
    await flush()
    expect(setRemote).toHaveBeenCalledWith([])
    expect(setLocal).toHaveBeenCalledWith([])
    expect(setActive).toHaveBeenCalledWith('')
    expect(errors).toEqual([])
  })
})
