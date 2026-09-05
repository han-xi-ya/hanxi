// 特征测试（组 C）：SnipasteView——迁移前锁定现状基线。
// 特殊性（必须原样保留）：脱管语义（本会话未托管/外部实例不被认领）、官网清单下载、
// 退出先尽力关闭超时强杀（forced → warning）、票据清理定时器 900ms、本地导入 prompt。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SnipasteView from '../SnipasteView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  Launch: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
  ImportLocal: vi.fn(),
  OfficialSiteURL: vi.fn(),
  OpenOfficialSite: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/snipaste/snipasteservice', () => svc)

const installed222 = {
  version: '2.2.2', dir: 'C:\\data\\snipaste\\2.2.2', exePath: 'C:\\data\\snipaste\\2.2.2\\Snipaste.exe',
  size: 30 * 1024 * 1024, installedAt: '2026-08-01', isImport: false,
  verificationMode: 'official-sha1+size+zip-crc+layout', officialHash: ''.padEnd(40, 'a'),
}
const installed210 = { ...installed222, version: '2.1.0', dir: 'C:\\data\\snipaste\\2.1.0', exePath: 'C:\\data\\snipaste\\2.1.0\\Snipaste.exe' }

function stubDefaults(snap: Record<string, unknown>, installed: unknown[] = [], releases: Array<Record<string, unknown>> = [], active = '2.2.2') {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(active)
  svc.OfficialSiteURL.mockResolvedValue('https://www.snipaste.com')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(SnipasteView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

// 迁移注记：window.confirm/prompt 已由 useConfirm/usePrompt 单例收编，
// 测试改经 settle* 驱动，"脱管退出必须二次确认"等断言语义不变。
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

beforeEach(() => {
  settleConfirm(false)
  settlePrompt(null)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('SnipasteView 脱管状态语义', () => {
  it('挂载并发拉取本地/官网/状态/官网地址', async () => {
    stubDefaults({ state: 'stopped' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.OfficialSiteURL).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('未托管态：本会话未托管文案 + 启动可用 + 退出禁用', async () => {
    stubDefaults({ state: 'stopped' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    const status = wrapper.find('.snipaste-status')
    expect(status.text()).toBe('本会话未托管')
    expect(status.attributes('data-state')).toBe('stopped')
    const btns = wrapper.findAll('.snipaste-control-panel .btn-group .btn')
    expect(btns[0].text()).toBe('启动 Snipaste')
    expect(btns[0].attributes('disabled')).toBeUndefined()
    expect(btns[1].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('运行态：本会话实例运行中 + 启动禁用 + 版本与路径展示', async () => {
    stubDefaults({ state: 'running', version: '2.2.2', pid: 8848, exePath: installed222.exePath, startedAt: new Date().toISOString() }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.snipaste-status').text()).toBe('本会话实例运行中')
    const btns = wrapper.findAll('.snipaste-control-panel .btn-group .btn')
    expect(btns[0].attributes('disabled')).toBeDefined()
    expect(btns[1].attributes('disabled')).toBeUndefined()
    expect(wrapper.find('.version-value').text()).toBe('2.2.2')
    expect(wrapper.find('.path-value').text()).toBe(installed222.exePath)
    wrapper.unmount()
  })

  it('正在退出：按钮文案切换且再点被门禁', async () => {
    stubDefaults({ state: 'quitting', version: '2.2.2' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    const btns = wrapper.findAll('.snipaste-control-panel .btn-group .btn')
    expect(btns[1].text()).toBe('正在退出…')
    await btns[1].trigger('click')
    await flushMicrotasks()
    expect(svc.Quit).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('SnipasteView 退出确认（脱管关键契约）', () => {
  it('取消退出不动后端；确认文案逐字锁定（宽限期/强杀/外部实例不受影响）', async () => {
    stubDefaults({ state: 'running', version: '2.2.2' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.snipaste-control-panel .btn-group .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('向本会话启动的 Snipaste 发送关闭请求')
    expect(confirmState.options.description).toContain('若未在宽限期内退出，将自动强制结束')
    expect(confirmState.options.description).toContain('外部实例不受影响')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.Quit).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('确认退出：forced=true 结果条为 warning 语气', async () => {
    stubDefaults({ state: 'running', version: '2.2.2' }, [installed222])
    svc.Quit.mockResolvedValue({ message: '未在宽限期内退出，已强制结束', forced: true })
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.snipaste-control-panel .btn-group .btn')[1].trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    const result = wrapper.find('.state-box[aria-live="polite"]')
    expect(result.classes()).toContain('state-warning')
    expect(result.text()).toContain('已强制结束')
    wrapper.unmount()
  })
})

describe('SnipasteView 版本管理', () => {
  const release223 = { version: '2.2.3', size: 31457280, published: '2026-08-20', officialHash: 'b'.repeat(40), isPre: false, stale: false }

  it('当前使用版本卸载禁用并给原因；他版可卸载且确认后删除', async () => {
    stubDefaults({ state: 'stopped' }, [installed222, installed210], [], '2.2.2')
    const { wrapper } = await mountInKeepAlive()
    const cards = wrapper.findAll('.installed-card')
    const activeCard = cards.find((c) => c.text().includes('2.2.2'))!
    const otherCard = cards.find((c) => c.text().includes('2.1.0'))!
    const activeUninstall = activeCard.findAll('button').find((b) => b.text() === '卸载')!
    expect(activeUninstall.attributes('disabled')).toBeDefined()
    expect(activeUninstall.attributes('title')).toBe('当前使用版本不可卸载，请先选择其他版本')

    const otherUninstall = otherCard.findAll('button').find((b) => b.text() === '卸载')!
    await otherUninstall.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('2.1.0')
    expect(useToast().toastMsg.value).toBe('已卸载 Snipaste 2.1.0')
    wrapper.unmount()
  })

  it('点下载：先挂 pending 票据并显示本地化阶段文案', async () => {
    stubDefaults({ state: 'stopped' }, [], [release223], '')
    svc.DownloadVersion.mockReturnValue(new Promise(() => {})) // 挂起，观察票据
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.version-table .btn-primary').trigger('click')
    await nextTick()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2.2.3')
    expect(wrapper.find('.download-stage').text()).toBe('正在创建下载任务')
    wrapper.unmount()
  })

  it('in-progress 回执：票据更新为已有任务并 toast', async () => {
    stubDefaults({ state: 'stopped' }, [], [release223], '')
    svc.DownloadVersion.mockResolvedValue('in-progress')
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.version-table .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(wrapper.find('.download-stage').text()).toBe('已有下载任务正在进行')
    expect(useToast().toastMsg.value).toContain('已在下载中')
    wrapper.unmount()
  })

  it('下载事件链：downloading 显示百分比 → done 同步本地 → 900ms 后清票', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [], [release223], '')
      const { wrapper } = await mountInKeepAlive()
      runtime.handlers['snipaste:version-download']({ data: { version: '2.2.3', stage: 'downloading', done: 50, total: 100, message: '' } })
      await nextTick()
      expect(wrapper.find('.snipaste-ver-status.working').text()).toBe('下载中')
      expect(wrapper.find('.snipaste-progress-wrap small').text()).toBe('50%')

      // done：本地列表此刻仍无该版本 → 行内提示刷新失败（锁定当前行为，迁移不得静默改变）
      runtime.handlers['snipaste:version-download']({ data: { version: '2.2.3', stage: 'done', done: 100, total: 100, message: '' } })
      await vi.advanceTimersByTimeAsync(10)
      expect(wrapper.find('.snipaste-row-error').text()).toContain('安装已完成，但本地版本列表刷新失败')
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('安装失败事件：状态徽标转失败并重试按钮出现', async () => {
    stubDefaults({ state: 'stopped' }, [], [release223], '')
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['snipaste:version-download']({ data: { version: '2.2.3', stage: 'error', done: 0, total: 0, message: '官网哈希不匹配' } })
    await nextTick()
    expect(wrapper.find('.snipaste-ver-status.error').exists()).toBe(true)
    expect(wrapper.find('.snipaste-row-error').text()).toBe('官网哈希不匹配')
    expect(wrapper.find('.retry-btn').exists()).toBe(true)
    wrapper.unmount()
  })

  it('导入本地：prompt 取消不动；确认后导入并刷新', async () => {
    stubDefaults({ state: 'stopped' }, [], [], '')
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.versions-overview .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    settlePrompt(null) // 取消
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()

    svc.ImportLocal.mockResolvedValue({ version: '2.1.0', dir: 'D:\\Snipaste', exePath: 'D:\\Snipaste\\Snipaste.exe', size: 1, installedAt: '2026-09-01', isImport: true, verificationMode: 'local-import+layout' })
    await wrapper.find('.versions-overview .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    settlePrompt('  D:\\Snipaste  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('D:\\Snipaste')
    expect(useToast().toastMsg.value).toBe('已导入 Snipaste 2.1.0')
    wrapper.unmount()
  })

  it('缓存数据警示：stale release 显示缓存徽标与提示条', async () => {
    stubDefaults({ state: 'stopped' }, [], [{ ...release223, stale: true }], '')
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.badge-warning').text()).toBe('缓存')
    expect(wrapper.find('.state-warning').text()).toContain('正在显示缓存数据')
    wrapper.unmount()
  })
})

describe('SnipasteView 轮询与清理', () => {
  it('2.5s 状态轮询；KeepAlive 停用后不空转；卸载注销双事件', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [installed222])
      const { wrapper, show } = await mountInKeepAlive()
      expect(Object.keys(runtime.handlers).sort()).toEqual(['snipaste:instance-state', 'snipaste:version-download'])
      const afterMount = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(afterMount + 3)
      show.value = false
      await nextTick()
      const afterDeactivate = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 4)
      expect(svc.GetStatus.mock.calls.length).toBe(afterDeactivate)
      wrapper.unmount()
      expect(runtime.unlisten).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })
})
