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

// ---------- P0 批 3：状态真相三态与版本互认（4.1/4.2/4.3） ----------

function batch3Adapter(opts: {
  getStatus?: () => Promise<ManagedSnapshot>
  installed?: Array<{ version: string }>
  sameVersion?: (a: string, b: string) => boolean
  download?: (rel: ManagedReleaseRecord) => Promise<Record<string, unknown>>
}) {
  let stateCb!: (s: ManagedSnapshot) => void
  const adapter = {
    getStatus: opts.getStatus ?? (async () => ({ ...stoppedSnap })),
    subscribeInstanceState: (cb) => {
      stateCb = cb
    },
    subscribeProgress: () => {},
    versions: {
      listInstalled: async () => opts.installed ?? [],
      listReleases: async () => [],
      getActive: async () => '',
      sameVersion: opts.sameVersion,
      download: opts.download ?? (async () => ({})),
      remove: async () => ({}),
      openDir: async () => ({}),
    },
  } as unknown as ManagedModuleAdapter
  return { adapter, fireState: (s: ManagedSnapshot) => stateCb(s) }
}

describe('ManagedConsoleStore 批 3 状态真相', () => {
  it('4.1 取态失败置 statusError 且保留旧快照，实例事件到达即清除', async () => {
    let fail = false
    const { adapter, fireState } = batch3Adapter({
      getStatus: async () => {
        if (fail) throw new Error('rpc down')
        return { ...stoppedSnap, state: 'running', version: '9.9.9', pid: 42 }
      },
    })
    const mounted = mountStore(adapter)
    await flushPromises()
    expect(mounted.store.statusError).toBe(false)
    expect(mounted.store.state).toBe('running')

    fail = true
    await mounted.store.refresh()
    expect(mounted.store.statusError).toBe(true)
    // 旧快照仍在（最后已知事实），但已标记为不可确认
    expect(mounted.store.runningVersion).toBe('9.9.9')
    expect(mounted.store.lastStatusAt).toBeGreaterThan(0)

    fireState({ ...stoppedSnap })
    expect(mounted.store.statusError).toBe(false)
    expect(mounted.store.state).toBe('stopped')
    mounted.wrapper.unmount()
  })

  it('4.2 本地区解析前 localResolved=false，load 完成后为 true', async () => {
    const gate = deferred<unknown[]>()
    const { adapter } = batch3Adapter({})
    ;(adapter.versions as { listInstalled: unknown }).listInstalled = () => gate.promise
    const mounted = mountStore(adapter)
    await flushPromises()
    expect(mounted.store.localResolved).toBe(false)

    gate.resolve([])
    await flushPromises()
    expect(mounted.store.localResolved).toBe(true)
    mounted.wrapper.unmount()
  })

  it('4.3 already-installed 清票经 sameVersion 互认（beta tag 与核心版本判等）', async () => {
    const coreMutual = (a: string, b: string) =>
      a.replace(/^v/, '').replace(/-beta\.\d+$/, '') === b.replace(/^v/, '').replace(/-beta\.\d+$/, '')
    const betaRelease: ManagedReleaseRecord = { ...release, version: 'v1.0.0-beta.1' }
    const { adapter } = batch3Adapter({
      installed: [{ version: '1.0.0' }],
      sameVersion: coreMutual,
      download: async () => ({ message: '版本已安装', reloadVersions: true }),
    })
    const mounted = mountStore(adapter)
    await flushPromises()

    await mounted.store.runDownload(betaRelease)
    // 互认命中（1.0.0 ≡ v1.0.0-beta.1 核心）：pending 票据以重拉后的已装事实收掉
    expect(mounted.store.downloading['v1.0.0-beta.1']).toBeUndefined()
    mounted.wrapper.unmount()
  })

  it('4.3 负例：未互认版本不清票，progressKey 缺省下票据保留待事件收口', async () => {
    const unrelated: ManagedReleaseRecord = { ...release, version: 'v9.9.9' }
    const { adapter } = batch3Adapter({
      installed: [{ version: '1.0.0' }],
      sameVersion: (a, b) => a === b,
      download: async () => ({ message: '开始后台安装', reloadVersions: true }),
    })
    const mounted = mountStore(adapter)
    await flushPromises()

    await mounted.store.runDownload(unrelated)
    expect(mounted.store.downloading['v9.9.9']).toBeDefined()
    expect(mounted.store.downloading['v9.9.9'].stage).toBe('resolve')
    mounted.wrapper.unmount()
  })
})
// ---------- N43 状态真相：未安装 ≠ 未运行 ----------
describe('N43 未安装呈现', () => {
  it('本地已解析且零版本：stateText 落"未安装"，hint 指路版本管理', async () => {
    const { adapter } = fakeAdapter()
    const probe = mountStore(adapter)
    await flushPromises()
    expect(probe.store.notInstalled).toBe(true)
    expect(probe.store.stateText).toBe('未安装')
    expect(probe.store.hint ?? '').toContain('版本管理')
    probe.wrapper.unmount()
  })
  it('primary 在未安装态不发起动词 RPC，直走 goVersions 接线', async () => {
    const { adapter } = fakeAdapter()
    const runSpy = vi.fn()
    ;(adapter as unknown as { control: unknown }).control = {
      primary: { run: runSpy, label: '启动' },
    }
    const probe = mountStore(adapter)
    await flushPromises()
    let jumped = 0
    probe.store.goVersions = () => { jumped++ }
    await probe.store.runControl('primary')
    expect(runSpy).not.toHaveBeenCalled()
    expect(jumped).toBe(1)
    probe.wrapper.unmount()
  })
  it('external 在跑实例在场时不判未安装（运行事实优先，N43④口径）', async () => {
    const { adapter } = fakeAdapter()
    ;(adapter.getStatus as ReturnType<typeof vi.fn>).mockResolvedValue({ ...stoppedSnap, state: 'external' })
    const probe = mountStore(adapter)
    await flushPromises()
    expect(probe.store.notInstalled).toBe(false)
    probe.wrapper.unmount()
  })
})
