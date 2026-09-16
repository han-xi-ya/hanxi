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

  // 失败类 toast 契约（#49 收口升级版）：8000ms 默认时长固化在 showErrorToast 一处。
  it('showErrorToast 默认保活 8000ms，普通 showToast 仍是 2500ms', () => {
    const { toastMsg, showToast, showErrorToast } = useToast()
    showErrorToast('克隆失败: exit status 42')
    expect(toastMsg.value).toBe('克隆失败: exit status 42')
    vi.advanceTimersByTime(7999)
    expect(toastMsg.value).toBe('克隆失败: exit status 42')
    vi.advanceTimersByTime(1)
    expect(toastMsg.value).toBe('')
    showToast('普通播报')
    vi.advanceTimersByTime(2500)
    expect(toastMsg.value).toBe('')
  })

  it('showErrorToast 保留显式 duration 覆盖能力（长短都可覆）', () => {
    const { toastMsg, showErrorToast } = useToast()
    showErrorToast('短暂错误提示', { duration: 1000 })
    vi.advanceTimersByTime(1000)
    expect(toastMsg.value).toBe('')
    showErrorToast('超长驻留', { duration: 15000 })
    vi.advanceTimersByTime(8000)
    expect(toastMsg.value).toBe('超长驻留')
    vi.advanceTimersByTime(7000)
    expect(toastMsg.value).toBe('')
  })

  it('showErrorToast 与 showToast 共用单例计时器：后发者清除前者定时器', () => {
    const { toastMsg, showToast, showErrorToast } = useToast()
    showErrorToast('先来错误')
    vi.advanceTimersByTime(1000)
    showToast('后来普通', { duration: 500 })
    vi.advanceTimersByTime(500)
    expect(toastMsg.value).toBe('') // 错误条的 8000ms 定时器已被重置，不得复活后把新消息抹掉
    vi.advanceTimersByTime(7000)
    expect(toastMsg.value).toBe('')
  })
})
