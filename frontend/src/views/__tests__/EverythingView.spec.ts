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
    await w.find('.result-name').trigger('click')
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
