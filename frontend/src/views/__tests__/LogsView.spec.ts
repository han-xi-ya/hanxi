// 特征测试：运行日志查看器（文件列表/内容防竞态/搜索过滤/自动刷新开关/清理流/轮询生命周期）。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import LogsView from '../LogsView.vue'
import { useToast } from '../../composables/useToast'

const appSvc = vi.hoisted(() => ({
  ListLogFiles: vi.fn(),
  ReadLogContent: vi.fn(),
  GetAppInfo: vi.fn(),
  OpenPath: vi.fn(),
  ClearLogs: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

const FILES = [
  { name: 'app-2026-09-05.log', size: 20480, modTime: '2026-09-05 10:00:00' },
  { name: 'app-2026-09-04.log', size: 10240, modTime: '2026-09-04 23:00:00' },
]
const CONTENT = '2026-09-05 INFO boot ok\n2026-09-05 ERROR frpc auth failed\n2026-09-05 INFO wechat message'

function stubs() {
  appSvc.ListLogFiles.mockResolvedValue(FILES)
  appSvc.ReadLogContent.mockResolvedValue(CONTENT)
  appSvc.GetAppInfo.mockResolvedValue({ logsDir: 'C:\\data\\logs' })
  appSvc.ClearLogs.mockResolvedValue(undefined)
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(LogsView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushPromises()
  return { wrapper, show }
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('LogsView', () => {
  it('挂载自动选中首个日志并读取内容，行数与列表一致', async () => {
    stubs()
    const { wrapper } = await mountView()
    expect(appSvc.ReadLogContent).toHaveBeenCalledWith('app-2026-09-05.log', 300)
    expect(wrapper.find('.file-name').text()).toBe('app-2026-09-05.log')
    expect(wrapper.find('.line-count').text()).toContain('3 行')
    expect(wrapper.find('.log-content').text()).toContain('boot ok')
    wrapper.unmount()
  })

  it('搜索过滤大小写不敏感，计数联动', async () => {
    stubs()
    const { wrapper } = await mountView()
    await wrapper.find('.input-search').setValue('error')
    expect(wrapper.find('.line-count').text()).toContain('1 行')
    expect(wrapper.find('.log-content').text()).toContain('auth failed')
    expect(wrapper.find('.log-content').text()).not.toContain('boot ok')
    wrapper.unmount()
  })

  it('无匹配时显示空态占位文案', async () => {
    stubs()
    const { wrapper } = await mountView()
    await wrapper.find('.input-search').setValue('不存在的词')
    expect(wrapper.find('.empty-logs').text()).toBe('暂无匹配的日志内容')
    wrapper.unmount()
  })

  it('自动刷新勾选框关闭后周期不再拉取（开启时按 2.5s 节奏）', async () => {
    vi.useFakeTimers()
    try {
      stubs()
      const { wrapper } = await mountView()
      const base = appSvc.ReadLogContent.mock.calls.length
      await vi.advanceTimersByTimeAsync(5000)
      expect(appSvc.ReadLogContent.mock.calls.length).toBeGreaterThanOrEqual(base + 2)
      const checkbox = wrapper.find('.auto-refresh-label input') as any
      checkbox.element.checked = false
      checkbox.trigger('change')
      await nextTick()
      const paused = appSvc.ReadLogContent.mock.calls.length
      await vi.advanceTimersByTimeAsync(7500)
      expect(appSvc.ReadLogContent.mock.calls.length).toBe(paused)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('清理历史：ClearLogs → toast → 重拉列表与内容', async () => {
    stubs()
    const { wrapper } = await mountView()
    const listCalls = appSvc.ListLogFiles.mock.calls.length
    await wrapper.find('.btn-danger-outline').trigger('click')
    await flushPromises()
    expect(appSvc.ClearLogs).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已清理历史日志')
    expect(appSvc.ListLogFiles.mock.calls.length).toBeGreaterThan(listCalls)
    wrapper.unmount()
  })

  it('KeepAlive 停用后轮询不泄漏', async () => {
    vi.useFakeTimers()
    try {
      stubs()
      const { wrapper, show } = await mountView()
      show.value = false
      await nextTick()
      const frozen = appSvc.ReadLogContent.mock.calls.length
      await vi.advanceTimersByTimeAsync(10000)
      expect(appSvc.ReadLogContent.mock.calls.length).toBe(frozen)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
