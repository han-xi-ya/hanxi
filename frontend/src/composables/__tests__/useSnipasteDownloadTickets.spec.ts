import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useSnipasteDownloadTickets } from '../useSnipasteDownloadTickets'

function mountTickets() {
  let api!: ReturnType<typeof useSnipasteDownloadTickets>
  const Host = defineComponent({
    setup() {
      api = useSnipasteDownloadTickets()
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  return { api, wrapper }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useSnipasteDownloadTickets', () => {
  it('done 先核验本地安装事实，再于 900ms 清票', async () => {
    vi.useFakeTimers()
    const { api, wrapper } = mountTickets()
    const installed = new Set(['2.2.3'])
    const refreshLocal = vi.fn(async () => true)

    await api.handleProgress(
      { key: '2.2.3', stage: 'done', done: 100, total: 100, message: '' },
      refreshLocal,
      (version) => installed.has(version),
    )

    expect(refreshLocal).toHaveBeenCalledTimes(1)
    expect(api.ticketOf('2.2.3')?.message).toBe('安装完成，正在同步本地版本')
    await vi.advanceTimersByTimeAsync(899)
    expect(api.ticketOf('2.2.3')).toBeDefined()
    await vi.advanceTimersByTimeAsync(1)
    expect(api.ticketOf('2.2.3')).toBeUndefined()
    wrapper.unmount()
  })

  it('重复 done 会取消旧计时器并把 900ms 清票窗口重锚', async () => {
    vi.useFakeTimers()
    const { api, wrapper } = mountTickets()
    const refreshLocal = vi.fn(async () => true)
    const isInstalled = () => true
    const done = { key: '2.2.3', stage: 'done', done: 100, total: 100, message: '' }

    await api.handleProgress(done, refreshLocal, isInstalled)
    await vi.advanceTimersByTimeAsync(400)
    await api.handleProgress(done, refreshLocal, isInstalled)
    await vi.advanceTimersByTimeAsync(500)
    expect(api.ticketOf('2.2.3')).toBeDefined()
    await vi.advanceTimersByTimeAsync(400)
    expect(api.ticketOf('2.2.3')).toBeUndefined()
    wrapper.unmount()
  })

  it('本地刷新失败保留 done 票据与行内错误，不安排静默清票', async () => {
    vi.useFakeTimers()
    const { api, wrapper } = mountTickets()

    await api.handleProgress(
      { key: '2.2.3', stage: 'done', done: 100, total: 100, message: '' },
      async () => false,
      () => false,
    )

    expect(api.rowErrors.value['2.2.3']).toContain('安装已完成，但本地版本列表刷新失败')
    await vi.advanceTimersByTimeAsync(2000)
    expect(api.ticketOf('2.2.3')?.stage).toBe('done')
    wrapper.unmount()
  })

  it('error 票据保留原始校验错误并允许重试', async () => {
    const { api, wrapper } = mountTickets()
    await api.handleProgress(
      { key: '2.2.3', stage: 'error', done: 0, total: 0, message: '官网哈希不匹配' },
      async () => true,
      () => false,
    )

    expect(api.ticketOf('2.2.3')?.stage).toBe('error')
    expect(api.rowErrors.value['2.2.3']).toBe('官网哈希不匹配')
    expect(api.isBusy('2.2.3')).toBe(false)
    wrapper.unmount()
  })
})
