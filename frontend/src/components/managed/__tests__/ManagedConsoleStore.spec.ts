import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ManagedModuleAdapter, ManagedReleaseRecord, ManagedSnapshot, NormalizedProgress } from '../adapter'
import { useManagedConsole } from '../store'

const stoppedSnap: ManagedSnapshot = {
  state: 'stopped',
  version: '',
  pid: 0,
  error: '',
  startedAt: ''
}

const release: ManagedReleaseRecord = {
  version: 'v1.2.3',
  published: '2026-09-19T00:00:00Z',
  size: 1024
}

function fakeAdapter(
  overrides: {
    orchestration?: 'shared' | 'custom'
    onProgress?: (progress: NormalizedProgress) => void
  } = {}
) {
  const events: { progress?: (progress: NormalizedProgress) => void } = {}
  const adapter: ManagedModuleAdapter = {
    getStatus: vi.fn(async () => ({ ...stoppedSnap })) as never,
    subscribeInstanceState: vi.fn(),
    subscribeProgress: (cb) => {
      events.progress = cb
    },
    onProgress: overrides.onProgress,
    versions: {
      orchestration: overrides.orchestration,
      listInstalled: vi.fn(async () => []),
      listReleases: vi.fn(async () => []),
      download: vi.fn(async () => ({})),
      remove: vi.fn(async () => ({})),
      openDir: vi.fn(async () => ({}))
    }
  }
  return { adapter, events }
}

function mountStore(adapter: ManagedModuleAdapter) {
  let store!: ReturnType<typeof useManagedConsole>
  const Probe = defineComponent({
    setup() {
      store = useManagedConsole(adapter)
      return () => h('span')
    }
  })
  const wrapper = mount(Probe)
  return {
    wrapper,
    get store() {
      return store
    }
  }
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('ManagedConsoleStore 版本编排归属', () => {
  it("orchestration='custom' 时不首拉版本，进度仅旁路 onProgress 且不写/清共享票据", async () => {
    vi.useFakeTimers()
    const onProgress = vi.fn()
    const { adapter, events } = fakeAdapter({ orchestration: 'custom', onProgress })
    const mounted = mountStore(adapter)
    await flushPromises()

    expect(adapter.versions.listInstalled).not.toHaveBeenCalled()
    expect(adapter.versions.listReleases).not.toHaveBeenCalled()

    const downloading: NormalizedProgress = {
      key: 'portable:v1.2.3',
      stage: 'downloading',
      done: 1,
      total: 4,
      message: ''
    }
    events.progress?.(downloading)
    await mounted.wrapper.vm.$nextTick()
    expect(onProgress).toHaveBeenCalledWith(downloading)
    expect(mounted.store.downloading).toEqual({})

    const done = { ...downloading, stage: 'done', done: 4 }
    events.progress?.(done)
    await vi.advanceTimersByTimeAsync(900)
    expect(onProgress).toHaveBeenLastCalledWith(done)
    expect(mounted.store.downloading).toEqual({})
    expect(adapter.versions.listInstalled).not.toHaveBeenCalled()
    expect(adapter.versions.listReleases).not.toHaveBeenCalled()
    mounted.wrapper.unmount()
  })

  it('缺省 shared 路径首拉版本，并在进度 done 后清票据及重拉列表', async () => {
    vi.useFakeTimers()
    const onProgress = vi.fn()
    const { adapter, events } = fakeAdapter({ onProgress })
    const mounted = mountStore(adapter)
    await flushPromises()

    expect(adapter.versions.listInstalled).toHaveBeenCalledTimes(1)
    expect(adapter.versions.listReleases).toHaveBeenCalledTimes(1)

    const downloading: NormalizedProgress = {
      key: release.version,
      stage: 'downloading',
      done: 1,
      total: 4,
      message: ''
    }
    events.progress?.(downloading)
    await mounted.wrapper.vm.$nextTick()
    expect(onProgress).toHaveBeenCalledWith(downloading)
    expect(mounted.store.downloading[release.version]).toEqual(downloading)

    const installedCallsBefore = (adapter.versions.listInstalled as ReturnType<typeof vi.fn>).mock.calls.length
    events.progress?.({ ...downloading, stage: 'done', done: 4 })
    await flushPromises()
    expect((adapter.versions.listInstalled as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(
      installedCallsBefore
    )
    expect(mounted.store.downloading[release.version]?.stage).toBe('done')

    await vi.advanceTimersByTimeAsync(801)
    expect(mounted.store.downloading[release.version]).toBeUndefined()
    mounted.wrapper.unmount()
  })
})

describe('ManagedConsoleStore runExclusive', () => {
  it('同一时刻只执行首个动作，结束后释放 busy 并允许下一次执行', async () => {
    const { adapter } = fakeAdapter()
    const mounted = mountStore(adapter)
    await flushPromises()

    const gate = deferred<string>()
    const firstAction = vi.fn(() => gate.promise)
    const blockedAction = vi.fn(async () => 'blocked')

    const first = mounted.store.runExclusive(firstAction)
    expect(mounted.store.busy).toBe(true)
    await expect(mounted.store.runExclusive(blockedAction)).resolves.toBeUndefined()
    expect(blockedAction).not.toHaveBeenCalled()

    gate.resolve('first-result')
    await expect(first).resolves.toBe('first-result')
    expect(mounted.store.busy).toBe(false)

    await expect(mounted.store.runExclusive(blockedAction)).resolves.toBe('blocked')
    expect(blockedAction).toHaveBeenCalledTimes(1)
    mounted.wrapper.unmount()
  })

  it('动作拒绝时仍释放单飞闩', async () => {
    const { adapter } = fakeAdapter()
    const mounted = mountStore(adapter)
    await flushPromises()

    await expect(
      mounted.store.runExclusive(async () => {
        throw new Error('动作失败')
      })
    ).rejects.toThrow('动作失败')
    expect(mounted.store.busy).toBe(false)

    const retry = vi.fn(async () => 42)
    await expect(mounted.store.runExclusive(retry)).resolves.toBe(42)
    expect(retry).toHaveBeenCalledTimes(1)
    mounted.wrapper.unmount()
  })
})
