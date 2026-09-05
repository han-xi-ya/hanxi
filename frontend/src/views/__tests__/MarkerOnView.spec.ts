// 特征测试（characterization test）：MarkerOnView 是 15+ 托管家族视图的代表样本。
// 本文件锁定的不是"应该怎样"，而是"迁移前事实怎样"——Phase 3/4 换骨架后必须全绿，
// 任何文案/交互差异都会在 CI 直接暴露。
//
// 测试 seam 约定（docs/FRONTEND.md §8）：
// 1. Wails 生成绑定（frontend/bindings/**）在 happy-dom 下无原生运行时，一律 vi.mock 为纯函数表；
// 2. @wailsio/runtime 的 Events 以回调捕获桩替代，便于模拟后端事件推送；
// 3. 生命周期：视图轮询由 onActivated 启动，故测试必须包在 <KeepAlive> 内，与真实外壳一致。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MarkerOnView from '../MarkerOnView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

// ---------- 绑定层打桩 ----------
const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  ToggleAnnotate: vi.fn(),
  StopAnnotate: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
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

// 相对路径与 MarkerOnView.vue 中 '../../bindings/...' 指向同一模块（__tests__ 深一层）
vi.mock('../../../bindings/hanxi/internal/modules/markeron/markeronservice', () => svc)
vi.mock('../../../bindings/hanxi/internal/app', () => ({
  AppService: { OpenPath: vi.fn() },
}))

const installedV102 = {
  version: 'v1.0.2',
  exePath: 'C:\\data\\markeron\\v1.0.2\\MarkerOn.exe',
  dir: 'C:\\data\\markeron\\v1.0.2',
  size: 4 * 1024 * 1024,
  installedAt: '2026-08-01',
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.RepositoryURL.mockResolvedValue('https://github.com/ifer47/markeron')
}

/** 纯微任务排空：不依赖 setTimeout，fake timers 下同样可用（flushPromises 会死锁）。 */
async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

/** 包在 KeepAlive 中挂载（复刻 App.vue 外壳），返回 wrapper 与宿主句柄。 */
async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(MarkerOnView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('MarkerOnView 初始装载', () => {
  it('挂载即拉取状态、版本、活动版本与联动开关', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(svc.GetActiveVersion).toHaveBeenCalled()
    expect(svc.GetFollowOnExit).toHaveBeenCalled()
    expect(svc.RepositoryURL).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('订阅 markeron 双事件并共享同一 unlisten 句柄', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['markeron:instance-state', 'markeron:version-download'])
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(2) // 卸载时两条订阅全部清理
  })
})

describe('MarkerOnView 六态矩阵（状态 → 文案/按钮/提示条）', () => {
  const cases: Array<{ name: string; snap: Record<string, unknown>; word: string; label: string; banner?: string }> = [
    { name: '未运行', snap: { state: 'stopped' }, word: '未运行', label: '启动 MarkerOn' },
    { name: '后台待命', snap: { state: 'running', version: '1.0.2', startedAt: new Date().toISOString() }, word: '已启动（未标注）', label: '开启标注' },
    { name: '标注中', snap: { state: 'running', drawing: true, version: '1.0.2' }, word: '标注已开启', label: '退出标注', banner: '标注已开启' },
    { name: '启动中', snap: { state: 'starting' }, word: '启动中…', label: '启动中…' },
    { name: '异常退出', snap: { state: 'failed', error: '缺少 WebView2 Runtime' }, word: '异常退出', label: '重试启动', banner: '缺少 WebView2 Runtime' },
  ]

  for (const c of cases) {
    it(c.name, async () => {
      stubDefaults(c.snap, [installedV102])
      const { wrapper } = await mountInKeepAlive()
      expect(wrapper.find('.status-word').text()).toBe(c.word)
      expect(wrapper.find('.annotate-toggle').text()).toContain(c.label)
      // 提示条迁移至 UiBanner：语义类前缀 hint-banner → 全局原子 .banner
      if (c.banner) {
        expect(wrapper.find('.banner').exists()).toBe(true)
        expect(wrapper.find('.banner').text()).toContain(c.banner)
      } else {
        expect(wrapper.find('.banner').exists()).toBe(false)
      }
      wrapper.unmount()
    })
  }

  it('外部实例且无已装版本：按钮禁用并给出快捷键指引用', async () => {
    stubDefaults({ state: 'external' }, []) // installed 为空
    const { wrapper } = await mountInKeepAlive()
    const toggle = wrapper.find('.annotate-toggle')
    expect(toggle.attributes('disabled')).toBeDefined()
    expect(toggle.attributes('title')).toContain('无可用信使程序')
    expect(wrapper.find('.banner').classes()).toContain('banner-warn')
    wrapper.unmount()
  })

  it('外部实例但有已装版本：可切换（不禁用）', async () => {
    stubDefaults({ state: 'external' }, [installedV102])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.annotate-toggle').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('.annotate-toggle').text()).toContain('切换标注')
    wrapper.unmount()
  })
})

describe('MarkerOnView 操作流', () => {
  it('点击切换标注：置 busy → 调 ToggleAnnotate → toast 回执 → 状态刷新', async () => {
    stubDefaults({ state: 'running', version: '1.0.2' }, [installedV102])
    let resolveToggle: (v: { message: string }) => void = () => {}
    svc.ToggleAnnotate.mockReturnValue(new Promise((r) => (resolveToggle = r)))
    const { wrapper } = await mountInKeepAlive()
    const callsBefore = svc.GetStatus.mock.calls.length

    await wrapper.find('.annotate-toggle').trigger('click')
    // busy 期间：主按钮变文案并禁用
    expect(wrapper.find('.annotate-toggle').text()).toContain('处理中…')
    expect(wrapper.find('.annotate-toggle').attributes('disabled')).toBeDefined()

    resolveToggle({ message: '标注已开启' })
    await flushPromises()
    const { toastMsg } = useToast()
    expect(toastMsg.value).toBe('标注已开启')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(callsBefore) // 回执后立即刷新
    expect(wrapper.find('.annotate-toggle').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('ToggleAnnotate 抛错也走 toast 且恢复非 busy', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102])
    svc.ToggleAnnotate.mockRejectedValue(new Error('信使通道超时'))
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.annotate-toggle').trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('信使通道超时')
    expect(wrapper.find('.annotate-toggle').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  // 迁移注记：卸载确认已由原生 window.confirm 收编至 useConfirm 全局单例
  // （可访问性对话框，文案 title/description 逐字保留）——契约不变：必经二次确认。
  it('卸载版本必须经确认对话框，取消则不动后端', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102])
    const { confirmState, settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 MarkerOn 1.0.2？')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('确认卸载：调 RemoveVersion 并 toast 结果', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102])
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { settleConfirm } = useConfirm()
    const { wrapper } = await mountInKeepAlive()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushPromises()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v1.0.2')
    expect(useToast().toastMsg.value).toBe('已卸载 1.0.2')
    wrapper.unmount()
  })
})

describe('MarkerOnView 事件推送与轮询（KeepAlive 契约）', () => {
  it('后端 instance-state 事件即时改写界面（不待轮询）', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.status-word').text()).toBe('未运行')
    runtime.handlers['markeron:instance-state']({
      data: { state: 'running', drawing: true, version: '1.0.2', pid: 4242 },
    })
    await nextTick()
    expect(wrapper.find('.status-word').text()).toBe('标注已开启')
    expect(wrapper.find('.pid-tag').text()).toContain('4242')
    wrapper.unmount()
  })

  it('下载进度事件驱动版本表出现进度条与百分比', async () => {
    stubDefaults({ state: 'stopped' }, [], [{ version: 'v1.2.0', size: 100, published: '2026-08-02T00:00:00Z', isPre: false }])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.ver-status.idle').exists()).toBe(true)
    runtime.handlers['markeron:version-download']({
      data: { version: 'v1.2.0', stage: 'downloading', done: 50, total: 100 },
    })
    await nextTick()
    expect(wrapper.find('.ver-status.downloading').exists()).toBe(true)
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    wrapper.unmount()
  })

  it('激活时 2.5s 轮询刷新；KeepAlive 停用后轮询停止（不泄漏）', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [installedV102])
      const { wrapper, show } = await mountInKeepAlive()
      const afterMount = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 3)
      expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(afterMount + 3)

      // 切走（KeepAlive 停用）→ 轮询必须归零
      show.value = false
      await nextTick()
      const afterDeactivate = svc.GetStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2500 * 5)
      expect(svc.GetStatus.mock.calls.length).toBe(afterDeactivate)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
