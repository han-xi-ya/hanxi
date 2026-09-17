// useClipboard：navigator.clipboard 主路 + execCommand 降级路 + 双败 false；
// paste 只走 readText（不可用即 null）；copyWithToast 统一回执话术。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useClipboard } from '../useClipboard'
import { useToast } from '../useToast'

function stubSecureContext(value: boolean) {
  Object.defineProperty(window, 'isSecureContext', { value, configurable: true })
}

function stubClipboard(impl: { writeText?: unknown; readText?: unknown } | undefined) {
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

  it('paste：安全上下文走 readText，原样返回文本', async () => {
    const readText = vi.fn().mockResolvedValue('剪贴板里的字')
    stubClipboard({ readText })
    const { paste } = useClipboard()
    expect(await paste()).toBe('剪贴板里的字')
    expect(readText).toHaveBeenCalled()
  })

  it('paste：readText 抛错（权限拒绝）返回 null 不抛', async () => {
    stubClipboard({ readText: vi.fn().mockRejectedValue(new Error('NotAllowedError')) })
    const { paste } = useClipboard()
    expect(await paste()).toBeNull()
  })

  it('paste：无 clipboard API 或非安全上下文一律 null（不假装成功）', async () => {
    stubClipboard(undefined)
    const { paste } = useClipboard()
    expect(await paste()).toBeNull()

    stubSecureContext(false)
    stubClipboard({ readText: vi.fn().mockResolvedValue('x') })
    expect(await paste()).toBeNull()
  })
})

describe('copyWithToast（统一回执话术）', () => {
  it('成功：toast 自定义 okTip，返回 true', async () => {
    stubClipboard({ writeText: vi.fn().mockResolvedValue(undefined) })
    const { copyWithToast } = useClipboard()
    const { toastMsg } = useToast()
    toastMsg.value = ''
    expect(await copyWithToast('abc', '已复制 IP: 1.2.3.4')).toBe(true)
    expect(toastMsg.value).toBe('已复制 IP: 1.2.3.4')
  })

  it('成功缺省话术：「已复制」', async () => {
    stubClipboard({ writeText: vi.fn().mockResolvedValue(undefined) })
    const { copyWithToast } = useClipboard()
    const { toastMsg } = useToast()
    toastMsg.value = ''
    expect(await copyWithToast('abc')).toBe(true)
    expect(toastMsg.value).toBe('已复制')
  })

  it('失败：单一话术「复制失败」（不再泄漏 execCommand/剪贴板不可用等实现细节）', async () => {
    stubClipboard(undefined)
    stubExecCommand(false)
    const { copyWithToast } = useClipboard()
    const { toastMsg } = useToast()
    toastMsg.value = ''
    expect(await copyWithToast('abc', '仓库地址已复制')).toBe(false)
    expect(toastMsg.value).toBe('复制失败')
  })
})
