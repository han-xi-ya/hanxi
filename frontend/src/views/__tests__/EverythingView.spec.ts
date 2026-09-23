// 特征测试：EverythingView = 托管家族契约 + 内嵌搜索控制台特有面
// （即时搜索/组合输入守卫/300 截断/结果操作/ES 组件态）。列宽拖拽与 localStorage
// 记忆为既有独立功能，本次迁移不触碰，仅锁"不破坏"。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import EverythingView from '../EverythingView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  StartBackground: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  DownloadVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenTarget: vi.fn(),
  RevealTarget: vi.fn(),
  EnsureSearchTool: vi.fn(),
  Search: vi.fn(),
  // 二波收敛基线补桩：视图现状消费 GetFollowOnExit/SetFollowOnExit（此前由 loadExtras 的
  // try/catch 静默吞掉 TypeError）；补齐后联动开关面才可测，既有用例不消费这两个键。
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/everything/everythingservice', () => svc)

const installedV15 = {
  version: '1.5.0.1371',
  exePath: 'C:\\data\\everything\\1.5.0.1371\\Everything.exe',
  dir: 'C:\\data\\everything\\1.5.0.1371',
  size: 2 * 1024 * 1024,
  installedAt: '2026-08-01',
}

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function stubDefaults(snap: Record<string, unknown>, installed: Array<{ version: string }> = [], releases: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue(snap)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue(installed[0]?.version ?? '')
  svc.EnsureSearchTool.mockResolvedValue('C:\\es.exe')
  svc.Search.mockResolvedValue([])
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(EverythingView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return wrapper
}

/** 经 Enter 触发一次搜索并冲刷。 */
async function search(w: Awaited<ReturnType<typeof mountView>>, q: string) {
  await w.find('.search-input').setValue(q)
  await w.find('.search-input').trigger('keyup', { key: 'Enter' })
  await flushMicrotasks()
}

beforeEach(() => {
  vi.stubGlobal('confirm', vi.fn(() => false))
  vi.stubGlobal('prompt', vi.fn(() => null))
  localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  useToast().clearToast()
  // 对话框单例兜底复位（正常用例内都已 settle）
  useConfirm().settleConfirm(false)
  usePrompt().settlePrompt(null)
})

describe('EverythingView 初始装载', () => {
  it('挂载拉取状态/版本并预就绪搜索组件（EnsureSearchTool）', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.EnsureSearchTool).toHaveBeenCalledTimes(1)
    expect(w.find('.ev-status-pill').text()).toBe('ES 就绪')
    w.unmount()
  })
})

describe('EverythingView 托管状态面', () => {
  it('running 区分后台/窗口两态文案', async () => {
    stubDefaults({ state: 'running', mode: 'background', version: '1.5.0.1371' })
    let w = await mountView()
    expect(w.find('.status-word').text()).toBe('后台运行中')
    expect(w.find('.banner').text()).toContain('秒开')
    w.unmount()

    stubDefaults({ state: 'running', mode: 'window', version: '1.5.0.1371' })
    w = await mountView()
    expect(w.find('.status-word').text()).toBe('窗口运行中')
    w.unmount()
  })

  it('三控制按钮态：启动后台在 running/external 下禁用；退出仅 running/starting/external 可用', async () => {
    stubDefaults({ state: 'running', mode: 'background' })
    let w = await mountView()
    let btns = w.findAll('.control-btns .btn')
    expect(btns[0].attributes('disabled')).toBeDefined()
    expect(btns[2].attributes('disabled')).toBeUndefined()
    w.unmount()

    stubDefaults({ state: 'stopped' })
    w = await mountView()
    btns = w.findAll('.control-btns .btn')
    expect(btns[0].attributes('disabled')).toBeUndefined()
    expect(btns[2].attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('启动后台成功 toast 回执并刷新', async () => {
    stubDefaults({ state: 'stopped' })
    svc.StartBackground.mockResolvedValue({ message: '后台索引已驻留' })
    const w = await mountView()
    const before = svc.GetStatus.mock.calls.length
    await w.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.StartBackground).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('后台索引已驻留')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
  })

  it('退出失败 toast 带前缀', async () => {
    stubDefaults({ state: 'running', mode: 'background' })
    svc.Quit.mockRejectedValue(new Error('落盘超时'))
    const w = await mountView()
    await w.findAll('.control-btns .btn')[2].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('退出失败: 落盘超时')
    w.unmount()
  })

  it('es 组件下载事件独立成态（不影响版本 map），done 后回到就绪', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'stopped' })
    svc.EnsureSearchTool.mockRejectedValue(new Error('未就绪'))
    const w = await mountView()
    expect(w.find('.ev-status-pill').text()).toBe('ES 未装')
    runtime.handlers['everything:download']({ data: { component: 'es', stage: 'downloading', version: '' } })
    await nextTick()
    expect(w.find('.ev-status-pill').text()).toBe('组件安装中…')
    runtime.handlers['everything:download']({ data: { component: 'es', stage: 'done', version: '' } })
    await vi.advanceTimersByTimeAsync(900)
    expect(w.find('.ev-status-pill').text()).toBe('ES 就绪')
    w.unmount()
    vi.useRealTimers()
  })

  it('版本下载事件驱动进度与 done 消失', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'stopped' }, [], [{ version: '1.5.0.1372', channel: 'beta', size: 100, published: '2026-08-02T00:00:00Z' }])
    const w = await mountView()
    runtime.handlers['everything:download']({ data: { component: 'app', version: '1.5.0.1372', stage: 'downloading', done: 30, total: 100 } })
    await nextTick()
    expect(w.find('.dl-percent').text()).toBe('30%')
    runtime.handlers['everything:download']({ data: { component: 'app', version: '1.5.0.1372', stage: 'done', done: 100, total: 100 } })
    await vi.advanceTimersByTimeAsync(900)
    expect(w.find('.dl-percent').exists()).toBe(false)
    w.unmount()
    vi.useRealTimers()
  })
})

describe('EverythingView 内嵌搜索', () => {
  it('Enter 即时搜索：Search(q,300)、结果行渲染、路径拼接（无尾杠补 \\）', async () => {
    stubDefaults({ state: 'running', mode: 'background' })
    svc.Search.mockResolvedValue([
      { name: 'go.mod', path: 'D:\\proj', size: 1024, modified: '2026-09-01 10:00', isDir: false },
      { name: 'src', path: 'D:\\proj\\', size: 0, modified: '2026-09-01 09:00', isDir: true },
    ])
    const w = await mountView()
    await search(w, 'go.mod')
    expect(svc.Search).toHaveBeenCalledWith('go.mod', 300)
    const rows = w.findAll('.result-tbl tbody tr')
    expect(rows).toHaveLength(2)
    expect(w.find('.results-meta').text()).toContain('共 2 条结果')
    // 打开按钮 → OpenTarget 带拼接全路径；目录路径尾杠不重复
    await rows[0].findAll('.link-button')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenTarget).toHaveBeenCalledWith('D:\\proj\\go.mod')
    await rows[1].findAll('.link-button')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenTarget).toHaveBeenCalledWith('D:\\proj\\src')
    w.unmount()
  })

  it('定位按钮走 RevealTarget；点击名称单元格复制完整路径', async () => {
    stubDefaults({ state: 'running', mode: 'background' })
    svc.Search.mockResolvedValue([{ name: 'a.txt', path: 'C:\\t', size: 5, modified: '2026-09-01', isDir: false }])
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    const w = await mountView()
    await search(w, 'a')
    await w.find('.result-tbl tbody tr .link-button:nth-child(2)').trigger('click')
    await flushMicrotasks()
    expect(svc.RevealTarget).toHaveBeenCalledWith('C:\\t\\a.txt')
    await w.find('.result-name .copy-cell').trigger('click')
    await flushMicrotasks()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('C:\\t\\a.txt')
    expect(useToast().toastMsg.value).toBe('已复制完整路径')
    w.unmount()
  })

  it('空关键词：清空结果且不发起搜索；输入即搜防抖 350ms 生效', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'running', mode: 'background' })
    const w = await mountView()
    await w.find('.search-input').setValue('abc')
    await vi.advanceTimersByTimeAsync(350)
    expect(svc.Search).toHaveBeenCalledWith('abc', 300)
    await w.find('.search-input').setValue('   ')
    await w.find('.search-input').trigger('input')
    await vi.advanceTimersByTimeAsync(700)
    // 空词清除并停止请求
    expect(w.find('.results-wrap').text()).toContain('输入即搜')
    vi.useRealTimers()
    w.unmount()
  })

  it('无匹配 toast 提示；满 300 条打截断警示', async () => {
    stubDefaults({ state: 'running', mode: 'background' })
    svc.Search.mockResolvedValue([])
    let w = await mountView()
    await search(w, 'zzz')
    expect(useToast().toastMsg.value).toBe('「zzz」无匹配结果')
    expect(w.find('.empty-hint').text()).toContain('无匹配结果')
    w.unmount()

    svc.Search.mockResolvedValue(Array.from({ length: 300 }, (_, i) => ({ name: `f${i}`, path: 'C:\\x', size: 1, modified: '', isDir: false })))
    w = await mountView()
    await search(w, 'f')
    expect(w.find('.warn-text').text()).toContain('300 条上限')
    w.unmount()
  })

  it('搜索抛错：错误行显示且组件在位', async () => {
    stubDefaults({ state: 'running', mode: 'background' })
    svc.Search.mockRejectedValue(new Error('ES 连接失败'))
    const w = await mountView()
    await search(w, 'boom')
    expect(w.find('.error-box').text()).toContain('ES 连接失败')
    w.unmount()
  })

  it('中文组合输入期间不触发防抖搜索（composition 守卫）', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'running', mode: 'background' })
    const w = await mountView()
    const input = w.find('.search-input')
    await input.trigger('compositionstart')
    await input.setValue('中文')
    await input.trigger('input')
    await vi.advanceTimersByTimeAsync(350)
    expect(svc.Search).not.toHaveBeenCalled()
    await input.trigger('compositionend')
    await vi.advanceTimersByTimeAsync(350)
    expect(svc.Search).toHaveBeenCalledWith('中文', 300)
    vi.useRealTimers()
    w.unmount()
  })
})

describe('EverythingView 版本管理', () => {
  it('卸载经确认：取消不动后端（迁移后走 useConfirm 单例，索引库警示锁定）', async () => {
    const { confirmState, settleConfirm } = useConfirm()
    stubDefaults({ state: 'stopped' }, [installedV15])
    const w = await mountView()
    const uninstallBtn = w.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('索引库')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    w.unmount()
  })

  it('确认卸载：RemoveVersion+toast+刷新', async () => {
    const { settleConfirm } = useConfirm()
    stubDefaults({ state: 'stopped' }, [installedV15])
    svc.RemoveVersion.mockResolvedValue(undefined)
    const w = await mountView()
    const uninstallBtn = w.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.5.0.1371')
    expect(useToast().toastMsg.value).toBe('已卸载 1.5.0.1371')
    w.unmount()
  })

  it('导入本地：prompt 值 trim 调 ImportLocal（迁移后走 usePrompt 单例）', async () => {
    const { settlePrompt } = usePrompt()
    stubDefaults({ state: 'stopped' })
    svc.ImportLocal.mockResolvedValue({ version: '1.4.1' })
    const w = await mountView()
    await w.find('.btn-group button').trigger('click')
    await flushMicrotasks()
    settlePrompt('  C:\\Program Files\\Everything  ')
    await flushMicrotasks()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\Program Files\\Everything')
    expect(useToast().toastMsg.value).toContain('含配置与索引库')
    w.unmount()
  })

  it('通道徽标文案：stable=稳定 / beta=1.5 测试', async () => {
    stubDefaults({ state: 'stopped' }, [], [
      { version: '1.4.1', channel: 'stable', size: 1, published: '' },
      { version: '1.5.0.1', channel: 'beta', size: 1, published: '' },
    ])
    const w = await mountView()
    const badges = w.findAll('.channel-badge').map((b) => b.text())
    expect(badges).toEqual(['稳定', '1.5 测试'])
    w.unmount()
  })
})

describe('EverythingView 轮询与 KeepAlive', () => {
  it('激活后 2.5s 轮询；停用后静默（搜索防抖/拖拽计时器不因此泄漏）', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'running', mode: 'background', startedAt: new Date().toISOString() })
    const show = ref(true)
    const Host = defineComponent({ render: () => (show.value ? h(KeepAlive, null, h(EverythingView)) : h('div')) })
    const w = mount(Host, { attachTo: document.body })
    await flushMicrotasks()
    const base = svc.GetStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 3)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThanOrEqual(base + 3)
    show.value = false
    await nextTick()
    const afterOff = svc.GetStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(2500 * 5)
    expect(svc.GetStatus.mock.calls.length).toBe(afterOff)
    w.unmount()
    vi.useRealTimers()
  })
})

// ── 二波收敛前置 · 现状行为基线补强 ─────────────────────────────────────────
// 上述既有用例锁主链路；以下补齐收敛重构时最易丢失的分支面：联动开关、双分发守卫、
// 票据阶段词面/失败重试、ES 未装补装流、busy 单飞门、版本卡三钮接线、首下引导、
// 列表错误方言、通道列六列方言与快照徽标。全部断言「现行为」，不代表收敛后形态。

describe('EverythingView 联动开关（此前整面漏锁）', () => {
  it('挂载拉取 GetFollowOnExit；勾选 SetFollowOnExit(true) 开词回执；失败回滚勾选并现「设置失败: 」', async () => {
    stubDefaults({ state: 'stopped' })
    svc.GetFollowOnExit.mockResolvedValue(false)
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const w = await mountView()
    expect(svc.GetFollowOnExit).toHaveBeenCalled()
    const box = w.find('.control-panel .toggle-label input')
    expect((box.element as HTMLInputElement).checked).toBe(false)
    await box.setValue(true)
    await flushMicrotasks()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(true)
    expect(useToast().toastMsg.value).toBe('已开启：Hanxi 退出时一并关闭 Everything')
    w.unmount()

    stubDefaults({ state: 'stopped' })
    svc.GetFollowOnExit.mockResolvedValue(false)
    svc.SetFollowOnExit.mockRejectedValueOnce(new Error('注册表写失败'))
    const w2 = await mountView()
    await w2.find('.control-panel .toggle-label input').setValue(true)
    await flushMicrotasks()
    expect((w2.find('.control-panel .toggle-label input').element as HTMLInputElement).checked).toBe(false) // 失败回滚
    expect(useToast().toastMsg.value).toBe('设置失败: 注册表写失败')
    w2.unmount()
  })
})

describe('EverythingView 控制面补锁', () => {
  it('唤窗回执 message 直出 toast；external 快照：warn 条现词、启动禁用、退出可用且 title 指引导轨退出', async () => {
    stubDefaults({ state: 'running', mode: 'window' })
    svc.OpenWindow.mockResolvedValue({ message: '已唤起搜索窗口' })
    let w = await mountView()
    await w.findAll('.control-btns .btn')[1].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenWindow).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('已唤起搜索窗口')
    w.unmount()

    stubDefaults({ state: 'external' })
    w = await mountView()
    const btns = w.findAll('.control-btns .btn')
    expect(btns[0].attributes('disabled')).toBeDefined() // 外部实例不重复拉起
    expect(btns[2].attributes('disabled')).toBeUndefined()
    expect(btns[2].attributes('title')).toBe('外部实例请在 Everything 托盘退出')
    expect(w.find('.banner').classes()).toContain('banner-warn')
    expect(w.find('.banner').text()).toContain('外部 Everything 实例')
    expect(w.find('.status-word').text()).toBe('外部运行')
    w.unmount()
  })

  it('failed 快照：error banner 透出快照 error 字段；状态词「异常退出」', async () => {
    stubDefaults({ state: 'failed', error: '索引库损坏' })
    const w = await mountView()
    expect(w.find('.banner').classes()).toContain('banner-error')
    expect(w.find('.banner').text()).toBe('索引库损坏') // 有 error 用 error，无则兜底话术
    expect(w.find('.status-word').text()).toBe('异常退出')
    w.unmount()
  })

  it('instance-state 事件改写：后台运行中 + PID 呈现；转 stopped 后 uptime 标记即摘', async () => {
    stubDefaults({ state: 'stopped' })
    const w = await mountView()
    runtime.handlers['everything:instance-state']({ data: { state: 'running', mode: 'background', version: '1.5.0.1371', pid: 7, startedAt: new Date(Date.now() - 65000).toISOString() } })
    await nextTick()
    expect(w.find('.status-word').text()).toBe('后台运行中')
    expect(w.find('.pid-tag').text()).toContain('PID 7')
    expect(w.find('.uptime-tag').exists()).toBe(true)
    runtime.handlers['everything:instance-state']({ data: { state: 'stopped' } })
    await nextTick()
    expect(w.find('.status-word').text()).toBe('未运行')
    expect(w.find('.uptime-tag').exists()).toBe(false)
    w.unmount()
  })

  it('busy 单飞门：控制请求在途期间 Enter 搜索不发 Search（useAsyncAction 共享 busy）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.StartBackground.mockReturnValue(new Promise(() => {})) // 永挂起 → busy 恒真
    const w = await mountView()
    await w.findAll('.control-btns .btn')[0].trigger('click')
    await flushMicrotasks()
    await w.find('.search-input').setValue('x')
    await w.find('.search-input').trigger('keyup', { key: 'Enter' })
    await flushMicrotasks()
    expect(svc.Search).not.toHaveBeenCalled()
    svc.StartBackground.mockResolvedValue({ message: 'ok' }) // 复位防泄漏到后续用例
    w.unmount()
  })
})

describe('EverythingView DownloadTicket 双分发补锁', () => {
  it('es/app 交叉隔离：es 组件票据不进版本 map；缺 component / null 票据整体忽略', async () => {
    stubDefaults({ state: 'stopped' }, [], [{ version: '1.5.0.1372', channel: 'beta', size: 100, published: '' }])
    const w = await mountView()
    runtime.handlers['everything:download']({ data: { component: 'es', version: '', stage: 'downloading', done: 50, total: 100 } })
    await nextTick()
    expect(w.find('.ev-status-pill').text()).toBe('组件安装中…') // es 态独立
    expect(w.find('.download-cell').exists()).toBe(false) // 版本表不受污染
    expect(w.find('.tbl tbody tr').text()).toContain('可安装')
    runtime.handlers['everything:download']({ data: { component: '', version: '1.5.0.1372', stage: 'downloading', done: 30, total: 100 } })
    runtime.handlers['everything:download']({ data: null })
    await nextTick()
    expect(w.find('.dl-percent').exists()).toBe(false) // 无 component/空票据被守卫吞掉
    w.unmount()
  })

  it('app done 票据：即时重拉版本三源、800ms 后清键回到可安装+下载钮', async () => {
    vi.useFakeTimers()
    stubDefaults({ state: 'stopped' }, [], [{ version: '1.5.0.1372', channel: 'beta', size: 100, published: '' }])
    const w = await mountView()
    const relBefore = svc.ListReleases.mock.calls.length
    runtime.handlers['everything:download']({ data: { component: 'app', version: '1.5.0.1372', stage: 'done', done: 100, total: 100 } })
    await vi.advanceTimersByTimeAsync(900)
    expect(svc.ListReleases.mock.calls.length).toBeGreaterThan(relBefore) // done 触发 loadVersions
    expect(w.find('.dl-percent').exists()).toBe(false)
    expect(w.find('.tbl tbody tr .btn-primary').exists()).toBe(true)
    vi.useRealTimers()
    w.unmount()
  })

  it('app verify 阶段词「校验解压安装…」；error 票据转失败 + 行内原文 + 重试链接重发原参', async () => {
    stubDefaults({ state: 'stopped' }, [], [{ version: '1.5.0.1372', channel: 'beta', size: 100, published: '' }])
    svc.DownloadVersion.mockResolvedValue('started')
    const w = await mountView()
    runtime.handlers['everything:download']({ data: { component: 'app', version: '1.5.0.1372', stage: 'verify', done: 0, total: 0 } })
    await nextTick()
    expect(w.find('.dl-meta-text').text()).toBe('校验解压安装…')
    expect(w.find('.ver-status').classes()).toContain('downloading')
    runtime.handlers['everything:download']({ data: { component: 'app', version: '1.5.0.1372', stage: 'error', message: '哈希不符' } })
    await nextTick()
    expect(w.find('.ver-status').classes()).toContain('error')
    expect(w.find('.ver-status').text()).toBe('失败')
    expect(w.find('.dl-error').text()).toBe('哈希不符')
    await w.find('.retry-link').trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenLastCalledWith('1.5.0.1372')
    w.unmount()
  })

  it('ES 未装形态：安装按钮现形并可补发 EnsureSearchTool（就绪后按钮摘除）', async () => {
    stubDefaults({ state: 'stopped' })
    svc.EnsureSearchTool.mockRejectedValueOnce(new Error('离线')) // 挂载期 ensureTool 失败
    const w = await mountView()
    expect(w.find('.ev-status-pill').text()).toBe('ES 未装')
    expect(useToast().toastMsg.value).toBe('搜索组件就绪失败: 离线') // useEverythingSearch 现词
    const installBtn = w.find('.search-line .link-button')
    expect(installBtn.exists()).toBe(true)
    svc.EnsureSearchTool.mockResolvedValue('C:\\es.exe')
    await installBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.EnsureSearchTool).toHaveBeenCalledTimes(2) // RPC 调用面：仅验重发次数与结果态
    expect(w.find('.ev-status-pill').text()).toBe('ES 就绪')
    expect(w.find('.search-line .link-button').exists()).toBe(false)
    w.unmount()
  })
})

describe('EverythingView 版本区补锁', () => {
  it('版本卡三钮接线：使用中位无设钮；运行版卸载禁用+现词 title；打开位置走 OpenTarget(dir)', async () => {
    stubDefaults({ state: 'running', mode: 'window', version: '1.5.0.1371' }, [installedV15])
    const w = await mountView()
    const card = w.find('.installed-card')
    expect(card.classes()).toContain('card-active') // active=installed[0]（stubDefaults 派生）
    expect(card.text()).toContain('使用中')
    const btns = card.findAll('button')
    expect(btns.map((b) => b.text())).not.toContain('设为使用')
    const uninstall = btns.find((b) => b.text() === '卸载')!
    expect(uninstall.attributes('disabled')).toBeDefined() // isRunning 位由视图算好传入
    expect(uninstall.attributes('title')).toBe('请先退出 Everything')
    await btns.find((b) => b.text().includes('打开位置'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenTarget).toHaveBeenCalledWith(installedV15.dir) // 目录语义收口 OpenTarget
    w.unmount()
  })

  it('无 active 时卡出「设为使用」：成功迁移高亮不重拉列表', async () => {
    stubDefaults({ state: 'running', mode: 'window', version: '1.5.0.1371' }, [installedV15])
    svc.GetActiveVersion.mockResolvedValue('')
    svc.SetActiveVersion.mockResolvedValue('1.5.0.1371')
    const w = await mountView()
    const card = w.find('.installed-card')
    expect(card.text()).toContain('运行中') // 非 active 但运行版本命中
    const before = svc.ListInstalledVersions.mock.calls.length
    await card.findAll('button').find((b) => b.text() === '设为使用')!.trigger('click')
    await flushMicrotasks()
    expect(svc.SetActiveVersion).toHaveBeenCalledWith('1.5.0.1371')
    expect(useToast().toastMsg.value).toBe('已将 1.5.0.1371 设为使用版本')
    expect(svc.ListInstalledVersions.mock.calls.length).toBe(before) // 就地迁移高亮，不重拉
    expect(w.find('.installed-card').classes()).toContain('card-active')
    w.unmount()
  })

  it('首用引导：stable 在前列面「下载 稳定版 X」；started 回执静默、失败带「下载失败: 」、already-installed 现词', async () => {
    stubDefaults({ state: 'stopped' }, [], [
      { version: '1.4.1', channel: 'stable', size: 1, published: '' },
      { version: '1.5.0.1', channel: 'beta', size: 1, published: '' },
    ])
    svc.DownloadVersion.mockResolvedValue('started')
    const w = await mountView()
    const btn = w.find('.empty-state .btn-primary')
    expect(btn.text()).toContain('下载 稳定版 1.4.1') // releases[0].channel 决定词面
    await btn.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.4.1')
    expect(useToast().toastMsg.value).toBe('') // started 静默（进度全走事件，现状）
    svc.DownloadVersion.mockRejectedValue(new Error('官网断了'))
    await btn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('下载失败: 官网断了')
    svc.DownloadVersion.mockResolvedValue('already-installed')
    await btn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('版本 1.4.1 已安装')
    w.unmount()
  })

  it('列表加载失败方言经 loadManagedVersions：本地「读取本地版本失败: 」、远程「获取远程版本列表失败: 」', async () => {
    stubDefaults({ state: 'stopped' })
    svc.ListInstalledVersions.mockRejectedValue(new Error('目录坏了'))
    let w = await mountView()
    expect(w.find('.error-box').text()).toBe('读取本地版本失败: 目录坏了')
    w.unmount()

    stubDefaults({ state: 'stopped' })
    svc.ListReleases.mockRejectedValue(new Error('网络断'))
    w = await mountView()
    expect(w.find('.error-box').text()).toBe('获取远程版本列表失败: 网络断')
    w.unmount()
  })

  it('通道列六列方言：未知通道原样透出、非 stable 一律测试档、stale 快照徽标、fmtSize/fmtDate 标准形「—」', async () => {
    stubDefaults({ state: 'stopped' }, [], [
      { version: '1.4.1', channel: 'stable', size: 0, published: '', stale: true },
      { version: '1.4.0', channel: 'legacy', size: 2097152, published: '2026-08-01T00:00:00Z' },
    ])
    const w = await mountView()
    expect(w.findAll('.tbl thead th').map((th) => th.text())).toEqual(['通道', '版本', '状态', '大小', '发布时间', '操作'])
    const rows = w.findAll('.tbl tbody tr')
    expect(rows[0].find('.channel-badge').classes()).toContain('ch-stable')
    expect(rows[0].find('.badge-pre').text()).toBe('快照') // stale 降级标记
    expect(rows[0].findAll('td')[3].text()).toBe('—') // utils/format 标准形（vs Snipaste 本地「未知」方言）
    expect(rows[0].findAll('td')[4].text()).toBe('—')
    expect(rows[1].find('.channel-badge').text()).toBe('legacy') // 未知通道原样
    expect(rows[1].find('.channel-badge').classes()).toContain('ch-beta') // 非 stable 全归测试档配色
    expect(rows[1].findAll('td')[4].text()).toBe('2026-08-01') // fmtDate ISO 前 10 位
    w.unmount()
  })
})

// ---------- P0 批 3·4.4：状态刷新代次 ----------
describe('EverythingView 请求代次', () => {
  it('旧 GetStatus 响应晚到不覆盖新状态', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' })
      let call = 0
      let releaseSlow!: (v: unknown) => void
      svc.GetStatus.mockImplementation(() => {
        call++
        if (call === 1) return new Promise((r) => { releaseSlow = r }) as unknown as Promise<never>
        return Promise.resolve({
          state: 'running', mode: 'background', version: '1.5.0.1371', pid: 42,
          error: '', external: false, startedAt: new Date().toISOString(),
        })
      })
      const w = await mountView()
      await vi.advanceTimersByTimeAsync(2600) // 轮询 call2 → 后台运行中
      expect(w.find('.status-word').text()).toBe('后台运行中')
      releaseSlow({ state: 'stopped' }) // 首拉旧响应晚到
      await vi.advanceTimersByTimeAsync(50)
      await flushMicrotasks()
      expect(w.find('.status-word').text()).toBe('后台运行中')
      w.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
