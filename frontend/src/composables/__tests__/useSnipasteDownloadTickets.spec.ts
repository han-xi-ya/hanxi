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

  it('error 后重取票（begin）：行内错误清账、票据回 pending、闸门重新合上', () => {
    // 视图侧 download() 的 catch 走 fail()，用户再点同一版本钮即 begin()——
    // 该 error→重取 迁移此前无人锁：错误残影若不清，行内红字会盖住新一轮下载。
    const { api, wrapper } = mountTickets()
    api.fail('2.2.3', '网络中断')
    expect(api.isBusy('2.2.3')).toBe(false)

    api.begin('2.2.3')
    expect(api.ticketOf('2.2.3')?.stage).toBe('pending')
    expect(api.ticketOf('2.2.3')?.message).toBe('正在创建下载任务')
    expect(api.rowErrors.value['2.2.3']).toBe('')
    expect(api.isBusy('2.2.3')).toBe(true)
    wrapper.unmount()
  })

  it('done 静默清票窗口内重新 begin：旧 retire 计时器作废，新票不被误吞', async () => {
    vi.useFakeTimers()
    const { api, wrapper } = mountTickets()
    const done = { key: '2.2.3', stage: 'done', done: 100, total: 100, message: '' }
    await api.handleProgress(done, async () => true, () => true)
    await vi.advanceTimersByTimeAsync(500) // retire 在途（900ms 未至）

    api.begin('2.2.3') // 立刻再点下载
    await vi.advanceTimersByTimeAsync(1500) // 旧计时器若未作废，会在新票上清票
    expect(api.ticketOf('2.2.3')).toBeDefined()
    expect(api.ticketOf('2.2.3')?.stage).toBe('pending')
    wrapper.unmount()
  })

  it('markInProgress：后端回执已在下载时清行错并立“正在进行”票', () => {
    const { api, wrapper } = mountTickets()
    api.fail('2.2.3', '上一次失败残留')
    api.markInProgress('2.2.3')
    expect(api.rowErrors.value['2.2.3']).toBe('')
    expect(api.ticketOf('2.2.3')?.message).toBe('已有下载任务正在进行')
    expect(api.isBusy('2.2.3')).toBe(true) // 合闸防连点二次投递
    wrapper.unmount()
  })
})
