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
