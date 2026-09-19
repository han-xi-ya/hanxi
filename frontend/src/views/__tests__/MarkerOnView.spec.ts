// 特征测试：MarkerOnView 是托管家族中「六态主钮」的独苗样本——迁移到批 0 共享
// 契约（adapters/markeron + ManagedConsoleShell）后，本文件继续锁定迁移前事实：
// 六态 label/variant/disabled/hint、banner 三变体、停止钮语义、卸载二次确认、
// 事件与轮询契约全部逐字等价；仅两处受共享契约强制的收敛单独建档（钮常驻禁用
// 替代 v-if 隐藏；停止失败 toast 前缀归一 ACTION_ERROR_PREFIX.quit）。
//
// 测试 seam 约定（docs/FRONTEND.md §8）：
// 1. Wails 生成绑定（frontend/bindings/**）在 happy-dom 下无原生运行时，一律 vi.mock 为纯函数表；
// 2. @wailsio/runtime 的 Events 以回调捕获桩替代，便于模拟后端事件推送；
// 3. 生命周期：状态轮询由 onActivated 启动，测试必须包在 <KeepAlive> 内，与真实外壳一致；
// 4. adapter.toggle 槽的切换闩 annotateBusy 是模块级单例，afterEach 复位防跨例污染。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MarkerOnView from '../MarkerOnView.vue'
import { annotateBusy } from '../../adapters/markeron'
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

// 相对路径与 adapters/markeron.ts 中 '../../bindings/...' 指向同一模块（__tests__ 深一层）
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

/** 钮区按 Shell 约定取钮：toggle（#primary-action 槽注入）在前，停止（quit 声明位）恒居末位。 */
function controlBtns(wrapper: Awaited<ReturnType<typeof mountInKeepAlive>>['wrapper']) {
  return wrapper.findAll('.control-btns .btn')
}

afterEach(() => {
  vi.restoreAllMocks()
  annotateBusy.value = false // 模块级切换闩复位（防半途未 resolve 的 ToggleAnnotate 污染下例）
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
  const cases: Array<{ name: string; snap: Record<string, unknown>; word: string; label: string; variant: string; banner?: string }> = [
    { name: '未运行', snap: { state: 'stopped' }, word: '未运行', label: '启动 MarkerOn', variant: 'btn-toggle-primary' },
    { name: '后台待命', snap: { state: 'running', version: 'v1.0.2', startedAt: new Date().toISOString() }, word: '已启动（未标注）', label: '开启标注', variant: 'btn-toggle-outline' },
    { name: '标注中', snap: { state: 'running', drawing: true, version: 'v1.0.2' }, word: '标注已开启', label: '退出标注', variant: 'btn-toggle-active', banner: '标注已开启' },
    { name: '启动中', snap: { state: 'starting' }, word: '启动中…', label: '启动中…', variant: 'btn-toggle-primary' },
    { name: '异常退出', snap: { state: 'failed', error: '缺少 WebView2 Runtime' }, word: '异常退出', label: '重试启动', variant: 'btn-toggle-danger', banner: '缺少 WebView2 Runtime' },
  ]

  for (const c of cases) {
    it(c.name, async () => {
      stubDefaults(c.snap, [installedV102])
      const { wrapper } = await mountInKeepAlive()
      expect(wrapper.find('.status-word').text()).toBe(c.word)
      const toggle = wrapper.find('.annotate-toggle')
      expect(toggle.text()).toContain(c.label)
      expect(toggle.classes()).toContain(c.variant)
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

  it('title 回落副文案：后台待命悬停指引「MarkerOn 已在后台运行」', async () => {
    stubDefaults({ state: 'running', version: 'v1.0.2' }, [installedV102])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.annotate-toggle').attributes('title')).toBe('MarkerOn 已在后台运行')
    wrapper.unmount()
  })

  it('状态说明行逐字保留（原 control-detail，未随共享 hint 位收编）', async () => {
    stubDefaults({ state: 'stopped' })
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.control-detail').text()).toBe('点击「启动 MarkerOn」后台运行，随后可开启桌面标注。')
    wrapper.unmount()
  })
})

describe('MarkerOnView 操作流', () => {
  it('点击切换标注（adapter.toggle 槽）：置闩变文案、禁用双钮 → 回执 toast → 事件推送驱动六态迁移', async () => {
    stubDefaults({ state: 'running', version: 'v1.0.2' }, [installedV102])
    let resolveToggle: (v: { message: string }) => void = () => {}
    svc.ToggleAnnotate.mockReturnValue(new Promise((r) => (resolveToggle = r)))
    const { wrapper } = await mountInKeepAlive()

    await wrapper.find('.annotate-toggle').trigger('click')
    // 切换进行中：主按钮变文案并禁用（原单一 busy 口径——停止钮同步交叉禁用）
    const toggle = wrapper.find('.annotate-toggle')
    expect(toggle.text()).toContain('处理中…')
    expect(toggle.attributes('disabled')).toBeDefined()
    const stop = controlBtns(wrapper).find((b) => b.text().includes('停止'))!
    expect(stop.attributes('disabled')).toBeDefined()

    resolveToggle({ message: '已切换桌面标注状态' })
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('已切换桌面标注状态')
    expect(wrapper.find('.annotate-toggle').attributes('disabled')).toBeUndefined()

    // 原动作后显式 refreshStatus 的等价通路：后端 Toggle 恒 emit instance-state
    runtime.handlers['markeron:instance-state']({
      data: { state: 'running', drawing: true, version: 'v1.0.2' },
    })
    await nextTick()
    expect(wrapper.find('.annotate-toggle').text()).toContain('退出标注')
    wrapper.unmount()
  })

  it('ToggleAnnotate 返回 started 时按 reloadVersions 重拉已装与远程版本投影', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102], [
      { version: 'v1.2.0', size: 100, published: '2026-08-02T00:00:00Z', isPre: false },
    ])
    svc.ToggleAnnotate.mockResolvedValue({ outcome: 'started', message: 'MarkerOn 已启动' })
    const { wrapper } = await mountInKeepAlive()
    const installedCalls = svc.ListInstalledVersions.mock.calls.length
    const releaseCalls = svc.ListReleases.mock.calls.length

    await wrapper.find('.annotate-toggle').trigger('click')
    await flushPromises()

    expect(svc.ToggleAnnotate).toHaveBeenCalledTimes(1)
    expect(svc.ListInstalledVersions).toHaveBeenCalledTimes(installedCalls + 1)
    expect(svc.ListReleases).toHaveBeenCalledTimes(releaseCalls + 1)
    wrapper.unmount()
  })

  it('ToggleAnnotate 抛错走裸串 toast（无前缀）且恢复非 busy', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102])
    svc.ToggleAnnotate.mockRejectedValue(new Error('信使通道超时'))
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.annotate-toggle').trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('信使通道超时') // 逐字原口径：失败不加前缀
    expect(wrapper.find('.annotate-toggle').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('钮区钮序按 Shell 约定：toggle（槽注入）在前、⏻ 停止恒居末位；running 可停并回执', async () => {
    stubDefaults({ state: 'running', version: 'v1.0.2' }, [installedV102])
    svc.StopAnnotate.mockResolvedValue({ stopped: true, external: false, message: 'MarkerOn 已停止' })
    const { wrapper } = await mountInKeepAlive()
    const btns = controlBtns(wrapper)
    expect(btns).toHaveLength(2)
    expect(btns[0].classes()).toContain('annotate-toggle')
    expect(btns[1].text()).toBe('⏻ 停止')
    await btns[1].trigger('click')
    await flushPromises()
    expect(svc.StopAnnotate).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('MarkerOn 已停止')
    wrapper.unmount()
  })

  it('停止钮常驻禁用替代原 v-if 隐藏：stopped 禁用、停止失败前缀归一「退出失败: 」（共享契约收敛）', async () => {
    stubDefaults({ state: 'stopped' }, [installedV102])
    const { wrapper } = await mountInKeepAlive()
    const stop = controlBtns(wrapper).find((b) => b.text().includes('停止'))!
    expect(stop.exists()).toBe(true)
    expect(stop.attributes('disabled')).toBeDefined()
    wrapper.unmount()

    stubDefaults({ state: 'running', version: 'v1.0.2' }, [installedV102])
    svc.StopAnnotate.mockRejectedValue(new Error('管道忙'))
    const { wrapper: w2 } = await mountInKeepAlive()
    const stop2 = controlBtns(w2).find((b) => b.text().includes('停止'))!
    await stop2.trigger('click')
    await flushPromises()
    // store.ts ACTION_ERROR_PREFIX.quit 单一词源（原视图「停止失败: 」（逐字）在共享件下归一）
    expect(useToast().toastMsg.value).toBe('退出失败: 管道忙')
    w2.unmount()
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
      data: { state: 'running', drawing: true, version: 'v1.0.2', pid: 4242 },
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

  it('下载失败事件：状态显失败且操作列呈现错误详情与重试（§9.5-2 家族样板）', async () => {
    // 13 个样板视图同构修复，此处锁定家族代表形态；其余视图结构一致不逐一重复断言。
    stubDefaults({ state: 'stopped' }, [], [{ version: 'v1.2.0', size: 100, published: '2026-08-02T00:00:00Z', isPre: false }])
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['markeron:version-download']({
      data: { version: 'v1.2.0', stage: 'error', message: '压缩包校验失败' },
    })
    await nextTick()
    expect(wrapper.find('.ver-status.error').text()).toBe('失败')
    expect(wrapper.find('.dl-error').text()).toBe('压缩包校验失败')
    expect(wrapper.find('.retry-link').text()).toBe('重试')
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
