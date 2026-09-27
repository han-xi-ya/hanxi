import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ManagedModuleAdapter, ManagedReleaseRecord, ManagedSnapshot, NormalizedProgress } from '../adapter'
import { sameSnapshot } from '../adapter'
import { useManagedConsole } from '../store'
import { useToast } from '../../../composables/useToast'

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
  useToast().clearToast()
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

// ---------- perf：轮询空转归零（内容级比对跳过快照引用替换） ----------

describe('ManagedConsoleStore 轮询空转归零', () => {
  it('内容未变的轮询回包不替换 snap 引用：消费组件整轮零重渲染；变化照常穿透', async () => {
    vi.useFakeTimers()
    const { adapter } = fakeAdapter() // getStatus 每轮返回字段全同的新对象（现状即如此）
    const renders = vi.fn()
    let store!: ReturnType<typeof useManagedConsole>
    const Probe = defineComponent({
      setup() {
        store = useManagedConsole(adapter)
        return () => {
          renders()
          return h('span', store.snap?.state ?? '')
        }
      },
    })
    const wrapper = mount(Probe)
    await flushPromises()
    expect(store.snap?.state).toBe('stopped') // 首帧装载写入不受门控影响

    const baseline = renders.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 4 + 1000 * 4)
    expect(adapter.getStatus).toHaveBeenCalledTimes(6) // 首帧 1 + 14s 窗口内 5 轮周期：RPC 频次零变化
    expect(renders).toHaveBeenCalledTimes(baseline) // 但内容全同 → 快照引用不换代 → 消费面零渲染批

    ;(adapter.getStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...stoppedSnap,
      state: 'running',
      pid: 4321,
      startedAt: new Date().toISOString(),
    })
    await vi.advanceTimersByTimeAsync(2500)
    expect(renders).toHaveBeenCalledTimes(baseline + 1) // 真变化照常穿透
    expect(store.snap?.state).toBe('running')
    wrapper.unmount()
  })

  it('sameSnapshot 单测：嵌套方言字段逐层判等、缺字段/标量差异/null 语义如实', () => {
    expect(sameSnapshot({ state: 'running', drawing: { pen: 'a' } }, { state: 'running', drawing: { pen: 'a' } })).toBe(true)
    expect(sameSnapshot({ state: 'running', drawing: { pen: 'a' } }, { state: 'running', drawing: { pen: 'b' } })).toBe(false)
    expect(sameSnapshot({ state: 'running' }, { state: 'running', listenAddr: '' })).toBe(false)
    expect(sameSnapshot({ tags: ['a', 'b'] }, { tags: ['a'] })).toBe(false)
    expect(sameSnapshot(null, null)).toBe(true)
    expect(sameSnapshot({ state: 'running' }, null)).toBe(false)
    expect(sameSnapshot(null, { state: 'running' })).toBe(false)
  })
})

// ---------- 波 2A：runReset / runSlotVerb 编排（视图手抄接线器的公共层对位） ----------
describe('ManagedConsoleStore 波 2A runReset 编排', () => {
  it('成功弹后端 message；两分支均不刷快照不重拉版本（TTB/QL/PaperTodo 逐字现形制）', async () => {
    const { adapter } = fakeAdapter()
    const run = vi.fn(async () => ({ message: '任务栏状态已重设' }))
    adapter.reset = { run }
    const probe = mountStore(adapter)
    await flushPromises()
    const statusCalls = (adapter.getStatus as ReturnType<typeof vi.fn>).mock.calls.length
    const installCalls = (adapter.versions.listInstalled as ReturnType<typeof vi.fn>).mock.calls.length

    await probe.store.runReset()
    expect(run).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('任务栏状态已重设')
    expect(adapter.getStatus).toHaveBeenCalledTimes(statusCalls) // runControl「恒刷」不适用
    expect(adapter.versions.listInstalled).toHaveBeenCalledTimes(installCalls)

    // 失败分支：裸错误串（TTB/PaperTodo 现词），同样不刷
    run.mockRejectedValueOnce(new Error('RPC 断了'))
    await probe.store.runReset()
    expect(useToast().toastMsg.value).toBe('RPC 断了')
    expect(adapter.getStatus).toHaveBeenCalledTimes(statusCalls)
    probe.wrapper.unmount()
  })

  it('前缀参数位：opts.errorPrefix 显式（QuickLook「重载失败: 」对位）与 copy.errorPrefix.reset 覆写位', async () => {
    const { adapter } = fakeAdapter()
    adapter.reset = { run: vi.fn(async () => { throw new Error('管道无响应') }) }
    const probe = mountStore(adapter)
    await flushPromises()

    await probe.store.runReset({ errorPrefix: '重载失败: ' })
    expect(useToast().toastMsg.value).toBe('重载失败: 管道无响应')

    adapter.copy = { errorPrefix: { reset: '收拢失败: ' } }
    await probe.store.runReset()
    expect(useToast().toastMsg.value).toBe('收拢失败: 管道无响应')

    // 显式参数优先于覆写位
    await probe.store.runReset({ errorPrefix: '重载失败: ' })
    expect(useToast().toastMsg.value).toBe('重载失败: 管道无响应')
    probe.wrapper.unmount()
  })

  it('refreshSnapshot:true 参数位：成功/失败两分支均刷一次快照', async () => {
    const { adapter } = fakeAdapter()
    adapter.reset = { run: vi.fn(async () => ({ message: '已复位' })) }
    const probe = mountStore(adapter)
    await flushPromises()
    const baseline = (adapter.getStatus as ReturnType<typeof vi.fn>).mock.calls.length

    await probe.store.runReset({ refreshSnapshot: true })
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline + 1)

    ;(adapter.reset.run as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('炸'))
    await probe.store.runReset({ refreshSnapshot: true })
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline + 2)
    probe.wrapper.unmount()
  })

  it('单飞闩：在途中二次调用丢弃；无 reset 槽静默无操作', async () => {
    const { adapter } = fakeAdapter()
    const gate = deferred<{ message?: string }>()
    const run = vi.fn(() => gate.promise)
    adapter.reset = { run }
    const probe = mountStore(adapter)
    await flushPromises()

    const first = probe.store.runReset()
    expect(probe.store.busy).toBe(true)
    const second = probe.store.runReset()
    await second // 闩占用即弃，不排队
    expect(run).toHaveBeenCalledTimes(1)
    gate.resolve({ message: '已重设' })
    await first

    adapter.reset = undefined
    await probe.store.runReset() // 不抛错、无 toast
    expect(useToast().toastMsg.value).toBe('已重设')
    probe.wrapper.unmount()
  })
})

describe('ManagedConsoleStore 波 2A runSlotVerb 编排', () => {
  it('settle 链缺省形：message 弹串 + reloadVersions 真值走 store.load 重拉版本区', async () => {
    const { adapter } = fakeAdapter()
    const probe = mountStore(adapter)
    await flushPromises()
    const installCalls = (adapter.versions.listInstalled as ReturnType<typeof vi.fn>).mock.calls.length

    await probe.store.runSlotVerb(async () => ({ message: '快捷方式已创建', reloadVersions: true }))
    expect(useToast().toastMsg.value).toBe('快捷方式已创建')
    expect(adapter.versions.listInstalled).toHaveBeenCalledTimes(installCalls + 1)
    probe.wrapper.unmount()
  })

  it('reloadVersionsVia 参数位：custom 编排注入自有重拉通道，store.load 不触发（vscode 对位）', async () => {
    const { adapter } = fakeAdapter()
    const probe = mountStore(adapter)
    await flushPromises()
    const installCalls = (adapter.versions.listInstalled as ReturnType<typeof vi.fn>).mock.calls.length
    const runtimeLoad = vi.fn(async () => {})

    await probe.store.runSlotVerb(async () => ({ message: '已入列', reloadVersions: true }), {
      reloadVersionsVia: runtimeLoad,
    })
    expect(runtimeLoad).toHaveBeenCalledTimes(1)
    expect(adapter.versions.listInstalled).toHaveBeenCalledTimes(installCalls)
    probe.wrapper.unmount()
  })

  it('refreshAfter 三档：缺省不刷 / ok 仅成功刷 / always 两分支均刷；errorPrefix 失败字面（vscode download「操作失败: 」对位）', async () => {
    const { adapter } = fakeAdapter()
    const probe = mountStore(adapter)
    await flushPromises()
    let baseline = (adapter.getStatus as ReturnType<typeof vi.fn>).mock.calls.length

    // 缺省 never：成败均不刷
    await probe.store.runSlotVerb(async () => ({ message: '静默世界' }))
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline)
    await probe.store.runSlotVerb(async () => { throw new Error('boom') })
    expect(useToast().toastMsg.value).toBe('boom') // 裸串（ddnsgo openConsole 现词）
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline)

    // ok：成功刷、失败不刷
    await probe.store.runSlotVerb(async () => ({}), { refreshAfter: 'ok' })
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline + 1)
    await probe.store.runSlotVerb(async () => { throw new Error('again') })
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline + 1)

    // always + errorPrefix：失败先弹带前缀 toast 再刷（vscode quitForm 时序对位）
    baseline = (adapter.getStatus as ReturnType<typeof vi.fn>).mock.calls.length
    await probe.store.runSlotVerb(async () => { throw new Error('操作炸了') }, {
      errorPrefix: '操作失败: ',
      refreshAfter: 'always',
    })
    expect(useToast().toastMsg.value).toBe('操作失败: 操作炸了')
    expect(adapter.getStatus).toHaveBeenCalledTimes(baseline + 1)
    probe.wrapper.unmount()
  })

  it('{ run } 槽形状与闭包形状等价；闩占用中丢弃返回 undefined', async () => {
    const { adapter } = fakeAdapter()
    const probe = mountStore(adapter)
    await flushPromises()

    const gate = deferred<void>()
    const specRun = vi.fn(() => gate.promise)
    const first = probe.store.runSlotVerb({ run: specRun })
    const skipped = await probe.store.runSlotVerb(async () => ({ message: '不该出现' }))
    expect(skipped).toBeUndefined()
    expect(useToast().toastMsg.value).not.toBe('不该出现')
    gate.resolve()
    await first

    const closure = vi.fn(async () => ({ message: '闭包回执' }))
    await probe.store.runSlotVerb(closure)
    expect(closure).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('闭包回执')
    probe.wrapper.unmount()
  })
})
