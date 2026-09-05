// usePolling 的 KeepAlive 契约测试：激活启动/停用静默/幂等防重/卸载清理。
// fake timers 下禁用 flushPromises（§25 死锁坑），统一用纯微任务排空。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePolling } from '../usePolling'

async function flushMicrotasks(times = 10) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

/** 通用 KeepAlive 宿主（复刻 App.vue 的缓存结构），show=false 模拟切走。 */
function mountInKeepAlive(setupFn: () => unknown) {
  const show = ref(true)
  const Child = defineComponent({ setup: setupFn, render: () => null })
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(Child)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  return { wrapper, show }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('usePolling', () => {
  it('挂载即首跑一次，此后按间隔周期触发', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { wrapper } = mountInKeepAlive(() => usePolling(fn, 2500))
    expect(fn).toHaveBeenCalledTimes(1) // immediateFirstRun 默认开启
    await vi.advanceTimersByTimeAsync(2500 * 3)
    expect(fn).toHaveBeenCalledTimes(4) // 1 + 3
    wrapper.unmount()
  })

  it('KeepAlive 停用后周期归零，重新激活恢复并再刷一帧', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { wrapper, show } = mountInKeepAlive(() => usePolling(fn, 1000))
    await vi.advanceTimersByTimeAsync(3000)
    const running = fn.mock.calls.length
    expect(running).toBeGreaterThanOrEqual(4)

    show.value = false // 切走 → onDeactivated
    await nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(fn.mock.calls.length).toBe(running) // 停用期间完全静默

    show.value = true // 切回 → onActivated：立即补一帧
    await nextTick()
    expect(fn.mock.calls.length).toBe(running + 1)
    wrapper.unmount()
  })

  it('mounted+activated 双触发下 start 幂等，只有一个定时器', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    let api: ReturnType<typeof usePolling> | null = null
    const { wrapper } = mountInKeepAlive(() => (api = usePolling(fn, 1000)))
    api!.start() // 再手动开两次也不得叠定时器
    api!.start()
    await vi.advanceTimersByTimeAsync(2000)
    expect(fn.mock.calls.length).toBe(3) // 1 首跑 + 2 周期，无双倍
    wrapper.unmount()
  })

  it('immediateFirstRun:false 时挂载不跑，纯按周期', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { wrapper } = mountInKeepAlive(() => usePolling(fn, 1000, { immediateFirstRun: false }))
    expect(fn).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    expect(fn).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('isPolling 随激活/停用翻转；unmount 后定时器彻底清理', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    let api: ReturnType<typeof usePolling> | null = null
    const { wrapper, show } = mountInKeepAlive(() => (api = usePolling(fn, 1000)))
    expect(api!.isPolling.value).toBe(true)

    show.value = false
    await nextTick()
    expect(api!.isPolling.value).toBe(false)

    show.value = true
    await nextTick()
    expect(api!.isPolling.value).toBe(true)

    wrapper.unmount()
    const after = fn.mock.calls.length
    await vi.advanceTimersByTimeAsync(5000)
    expect(fn.mock.calls.length).toBe(after)
  })

  it('异步回调抛错不杀后续周期', async () => {
    vi.useFakeTimers()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    let n = 0
    const { wrapper } = mountInKeepAlive(() =>
      usePolling(async () => {
        n++
        if (n === 1) throw new Error('首次炸掉')
      }, 1000),
    )
    await vi.advanceTimersByTimeAsync(3000)
    expect(n).toBeGreaterThanOrEqual(3) // 第一跳失败后仍持续
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
    wrapper.unmount()
  })
})
