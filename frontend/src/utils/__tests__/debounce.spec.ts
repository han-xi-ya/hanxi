// 特征测试：锁定 HistoryPanel（watch 重排）与 useEverythingSearch（停顿触发 +
// 回车/卸载清档）两处手写 debounce 的共同行为——trailing 单次、窗口内重置、
// cancel 清待执行且幂等、触发后可重新上档。
import { afterEach, describe, expect, it, vi } from 'vitest'
import { debounce } from '../debounce'

afterEach(() => {
  vi.useRealTimers()
})

describe('debounce', () => {
  it('窗口内不触发，静默到期后恰好触发一次', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 350)
    d()
    expect(fn).not.toHaveBeenCalled()
    vi.advanceTimersByTime(350)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('窗口内重复调用重置倒计时：只算最后一次', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 350)
    d()
    vi.advanceTimersByTime(200)
    d()
    vi.advanceTimersByTime(200)
    expect(fn).not.toHaveBeenCalled() // 首档 350ms 已被重置，不会到点
    vi.advanceTimersByTime(150)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('cancel 清除待执行（回车直发/卸载清档语义）', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 350)
    d()
    d.cancel()
    vi.advanceTimersByTime(1000)
    expect(fn).not.toHaveBeenCalled()
  })

  it('触发后可重新上档；无待执行时 cancel 幂等不误伤', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 350)
    d()
    vi.advanceTimersByTime(350)
    expect(fn).toHaveBeenCalledTimes(1)
    d.cancel()
    vi.advanceTimersByTime(350)
    expect(fn).toHaveBeenCalledTimes(1)
    d()
    vi.advanceTimersByTime(350)
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('cancel 后重新调用照常触发（清档不熄火）', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 350)
    d()
    d.cancel()
    d()
    vi.advanceTimersByTime(350)
    expect(fn).toHaveBeenCalledTimes(1)
  })
})
