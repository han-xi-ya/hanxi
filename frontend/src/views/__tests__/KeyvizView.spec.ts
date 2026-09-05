// 特征测试（组 B）：KeyvizView 迁移前行为基线；迁移后除确认/输入交互机制外逐字保持。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import KeyvizView from '../KeyvizView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(), ListInstalledVersions: vi.fn(), GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(), StartKeyviz: vi.fn(), Quit: vi.fn(),
  DownloadVersion: vi.fn(), SetActiveVersion: vi.fn(), RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(), OpenDir: vi.fn(), GetFollowOnExit: vi.fn(), SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(), OpenRepository: vi.fn(),
}))

const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, (event: { data: unknown }) => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return () => {}
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/keyviz/keyvizservice', () => svc)

const installedV = { version: '1.1.3', exePath: 'C:\\hx\\keyviz\\keyviz.exe', dir: 'C:\\hx\\keyviz', size: 2097152, installedAt: '2026-08-01', isImport: false }
const release1 = { version: '1.1.3', size: 2097152, published: '2024-06-01T00:00:00Z', isPre: false }

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: unknown[] = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/mulaRahul/keyviz')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(KeyvizView)) : h('div')) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

// 迁移注记（有意变更）：window.confirm/prompt 已由 useConfirm/usePrompt 全局单例收编，
// 交互面驱动相应改为 settleConfirm/settlePrompt；文案与调用序列断言逐字保持。
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

afterEach(() => {
  settleConfirm(false) // 防悬挂
  settlePrompt(null)
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('KeyvizView', () => {
  it('挂载拉全量 + 订阅 keyviz 双事件', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['keyviz:instance-state', 'keyviz:version-download'])
    wrapper.unmount()
  })

  it('五态文案：未运行/运行中/外部/异常', async () => {
    stubDefaults({ state: 'stopped' })
    let ctx = await mountView()
    expect(ctx.wrapper.find('.status-word').text()).toBe('未运行')
    ctx.wrapper.unmount()

    stubDefaults({ state: 'running', version: '1.1.3', startedAt: new Date().toISOString() }, [installedV])
    ctx = await mountView()
    expect(ctx.wrapper.find('.status-word').text()).toBe('运行中')
    expect(ctx.wrapper.find('.banner-ok').text()).toContain('Keyviz 正在运行')
    ctx.wrapper.unmount()

    stubDefaults({ state: 'external' })
    ctx = await mountView()
    expect(ctx.wrapper.find('.banner-warn').text()).toContain('外部 Keyviz 实例')
    ctx.wrapper.unmount()

    stubDefaults({ state: 'failed', error: 'msiexec 提取失败' })
    ctx = await mountView()
    expect(ctx.wrapper.find('.banner-error').text()).toContain('msiexec 提取失败')
    ctx.wrapper.unmount()
  })

  it('启动：成功 toast 回传 message；重入与退出禁用矩阵', async () => {
    stubDefaults({ state: 'stopped' })
    svc.StartKeyviz.mockResolvedValue({ message: 'Keyviz 已启动' })
    const { wrapper } = await mountView()
    const [startBtn, quitBtn] = wrapper.findAll('.control-btns .btn')
    expect(quitBtn.attributes('disabled')).toBeDefined()
    await startBtn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('Keyviz 已启动')
    wrapper.unmount()
  })

  it('退出：running 时可用，调用 Quit 并 toast 回执', async () => {
    stubDefaults({ state: 'running', version: '1.1.3', startedAt: new Date().toISOString() }, [installedV])
    svc.Quit.mockResolvedValue({ message: 'Keyviz 已退出' })
    const { wrapper } = await mountView()
    const quitBtn = wrapper.findAll('.control-btns .btn')[1]
    expect(quitBtn.attributes('disabled')).toBeUndefined()
    await quitBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.Quit).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('Keyviz 已退出')
    wrapper.unmount()
  })

  it('卸载确认：取消不调后端；确认卸载 + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installedV])
    let { wrapper } = await mountView()
    await wrapper.find('.installed-card .btn-danger-outline').trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toContain('确定卸载 Keyviz 1.1.3')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()

    svc.RemoveVersion.mockResolvedValue(undefined)
    ;({ wrapper } = await mountView())
    await wrapper.find('.installed-card .btn-danger-outline').trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.1.3')
    expect(useToast().toastMsg.value).toBe('已卸载 1.1.3')
    wrapper.unmount()
  })

  it('导入本地输入框取消不动作', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountView()
    await wrapper.find('.control-panel .btn-group .btn').trigger('click')
    await flushMicrotasks()
    expect(promptState.open).toBe(true)
    expect(promptState.options.title).toContain('Keyviz 安装目录完整路径')
    settlePrompt(null)
    await flushMicrotasks()
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('事件推送改写状态与下载进度（管理提取文案）', async () => {
    stubDefaults({ state: 'stopped' }, [], [release1])
    const { wrapper } = await mountView()
    runtime.handlers['keyviz:instance-state']({ data: { state: 'running', version: '1.1.3', startedAt: new Date().toISOString() } })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('运行中')

    runtime.handlers['keyviz:version-download']({ data: { version: '1.1.3', stage: 'downloading', done: 25, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-percent').text()).toBe('25%')
    runtime.handlers['keyviz:version-download']({ data: { version: '1.1.3', stage: 'extract', done: 100, total: 100 } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toContain('管理提取安装')
    wrapper.unmount()
  })

  it('轮询激活启动、停用停止', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      const { wrapper, show } = await mountView()
      const base = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
      show.value = false
      await nextTick()
      const after = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(15000)
      expect(svc.GetStatus.mock.calls.length).toBe(after)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
