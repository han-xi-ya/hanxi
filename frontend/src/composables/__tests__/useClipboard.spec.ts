// useClipboard：navigator.clipboard 主路 + execCommand 降级路 + 双败 false。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useClipboard } from '../useClipboard'

function stubSecureContext(value: boolean) {
  Object.defineProperty(window, 'isSecureContext', { value, configurable: true })
}

function stubClipboard(impl: { writeText?: unknown } | undefined) {
  Object.defineProperty(navigator, 'clipboard', { value: impl, configurable: true })
}

function stubExecCommand(returns: boolean | 'throws') {
  Object.defineProperty(document, 'execCommand', {
    configurable: true,
    value: () => {
      if (returns === 'throws') throw new Error('execCommand 不可用')
      return returns
    },
  })
}

// 记录环境原值， afterEach 精确复原，避免桩泄漏到同 worker 的后续用例
const originalClipboard = navigator.clipboard
const originalExecCommand = document.execCommand

beforeEach(() => {
  stubSecureContext(true)
})

afterEach(() => {
  vi.restoreAllMocks()
  stubClipboard(originalClipboard)
  Object.defineProperty(document, 'execCommand', { configurable: true, value: originalExecCommand })
})

describe('useClipboard', () => {
  it('安全上下文 + clipboard 可用：走 writeText，不触发降级', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    stubClipboard({ writeText })
    const execSpy = vi.fn()
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execSpy })

    const { copy } = useClipboard()
    expect(await copy('https://example.com')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('https://example.com')
    expect(execSpy).not.toHaveBeenCalled()
  })

  it('writeText 抛错：回退隐藏 textarea + execCommand，成功后不留残渣节点', async () => {
    stubClipboard({ writeText: vi.fn().mockRejectedValue(new Error('denied')) })
    stubExecCommand(true)
    const before = document.querySelectorAll('textarea').length

    const { copy } = useClipboard()
    expect(await copy('text-a')).toBe(true)
    expect(document.querySelectorAll('textarea').length).toBe(before) // 用完即删
  })

  it('无 clipboard API：直接走 execCommand 路径', async () => {
    stubClipboard(undefined)
    stubExecCommand(true)
    const { copy } = useClipboard()
    expect(await copy('text-b')).toBe(true)
  })

  it('execCommand 返回 false：如实报告失败', async () => {
    stubClipboard(undefined)
    stubExecCommand(false)
    const { copy } = useClipboard()
    expect(await copy('text-c')).toBe(false)
  })

  it('两路皆不可用：返回 false 且不抛', async () => {
    stubClipboard(undefined)
    stubExecCommand('throws')
    const { copy } = useClipboard()
    expect(await copy('text-d')).toBe(false)
  })

  it('非安全上下文（如 http 页）：跳过 clipboard 直接降级', async () => {
    stubSecureContext(false)
    const writeText = vi.fn()
    stubClipboard({ writeText })
    stubExecCommand(true)
    const { copy } = useClipboard()
    expect(await copy('text-e')).toBe(true)
    expect(writeText).not.toHaveBeenCalled()
  })
})
