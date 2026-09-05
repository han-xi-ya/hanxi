// 特征测试：useToast 模块级单例语义（全应用共享一条 toastMsg）与定时器生命周期。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useToast } from '../useToast'

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  useToast().clearToast()
  vi.useRealTimers()
})

describe('useToast', () => {
  it('showToast 立即写入消息，默认 2500ms 后自动清除', () => {
    const { toastMsg, showToast } = useToast()
    showToast('已保存')
    expect(toastMsg.value).toBe('已保存')
    vi.advanceTimersByTime(2500)
    expect(toastMsg.value).toBe('')
  })

  it('支持自定义 duration', () => {
    const { toastMsg, showToast } = useToast()
    showToast('短暂', { duration: 500 })
    vi.advanceTimersByTime(400)
    expect(toastMsg.value).toBe('短暂')
    vi.advanceTimersByTime(100)
    expect(toastMsg.value).toBe('')
  })

  it('连续触发时重置旧定时器，只有最后一条会消失', () => {
    const { toastMsg, showToast } = useToast()
    showToast('第一条', { duration: 1000 })
    vi.advanceTimersByTime(500)
    showToast('第二条', { duration: 2500 })
    vi.advanceTimersByTime(1000)
    expect(toastMsg.value).toBe('第二条') // 第一条的计时器已被清除，不得把消息抹掉
    vi.advanceTimersByTime(1500)
    expect(toastMsg.value).toBe('')
  })

  it('clearToast 立即清空并取消待执行的隐藏', () => {
    const { toastMsg, showToast, clearToast } = useToast()
    showToast('待清除', { duration: 1000 })
    clearToast()
    expect(toastMsg.value).toBe('')
    vi.advanceTimersByTime(5000)
    expect(toastMsg.value).toBe('')
  })

  it('多次 useToast() 调用共享同一状态（模块级单例）', () => {
    const a = useToast()
    const b = useToast()
    a.showToast('来自 a')
    expect(b.toastMsg.value).toBe('来自 a')
    b.clearToast()
    expect(a.toastMsg.value).toBe('')
  })
})
