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
    await wrapper.find('.tbl .btn-primary').trigger('click')
    await nextTick()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('2.2.3')
    expect(wrapper.find('.download-stage').text()).toBe('正在创建下载任务')
    wrapper.unmount()
  })

  it('in-progress 回执：票据更新为已有任务并 toast', async () => {
    stubDefaults({ state: 'stopped' }, [], [release223], '')
    svc.DownloadVersion.mockResolvedValue('in-progress')
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.tbl .btn-primary').trigger('click')
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
    expect(wrapper.find('#snipaste-versions-panel .link-button').exists()).toBe(true)
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
    expect(wrapper.find('.chip-warning').text()).toBe('缓存')
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

// ── 二波收敛前置 · 现状行为基线补强 ─────────────────────────────────────────
// 既有用例锁了脱管语义/确认闸/forced 回执等主面；以下补齐收敛最易丢的分支：
// launch/quit 动词回执全谱（info/warning/error 三态与 toast 不对称）、票据
// cancelCleanup 重锚、already-installed 即时清票、校验方式三态方言、卸载禁用
// 原因 title 四态、quitting/external 全貌、无版本引导、setActive、extras 缺席
// 形态（Snipaste 本家无 FollowOnExit/Shutdown/双通道概念）与 onActivated 重拉。

const installed223 = { ...installed222, version: '2.2.3', dir: 'C:\\data\\snipaste\\2.2.3', exePath: 'C:\\data\\snipaste\\2.2.3\\Snipaste.exe' }
const release223b = { version: '2.2.3', size: 31457280, published: '2026-08-20', officialHash: 'b'.repeat(40), isPre: false, stale: false }

describe('SnipasteView 控制动词回执全谱', () => {
  it('launch 成功：message 双通道（state-info 结果条 + toast）并本地/状态双刷新；失败：「启动失败：」error 态同样双通道', async () => {
    stubDefaults({ state: 'stopped' }, [installed222])
    svc.Launch.mockResolvedValue({ message: 'Snipaste 2.2.2 已在本会话启动', pid: 9 })
    const { wrapper } = await mountInKeepAlive()
    const beforeLocal = svc.ListInstalledVersions.mock.calls.length
    const beforeStatus = svc.GetStatus.mock.calls.length
    await wrapper.findAll('.snipaste-control-panel .btn-group .btn')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.Launch).toHaveBeenCalledTimes(1)
    const box = wrapper.find('.state-box[aria-live="polite"]')
    expect(box.classes()).toContain('state-info')
    expect(box.text()).toBe('Snipaste 2.2.2 已在本会话启动')
    expect(useToast().toastMsg.value).toBe('Snipaste 2.2.2 已在本会话启动')
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(beforeLocal)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(beforeStatus)
    wrapper.unmount()

    stubDefaults({ state: 'stopped' }, [installed222])
    svc.Launch.mockRejectedValue(new Error('Snipaste.exe 缺失'))
    const m2 = await mountInKeepAlive()
    await m2.wrapper.findAll('.snipaste-control-panel .btn-group .btn')[0].trigger('click')
    await flushMicrotasks()
    const errBox = m2.wrapper.find('.state-box[aria-live="polite"]')
    expect(errBox.classes()).toContain('state-error')
    expect(errBox.text()).toBe('启动失败：Snipaste.exe 缺失') // 全角冒号方言（家族多数用半角）
    expect(useToast().toastMsg.value).toBe('启动失败：Snipaste.exe 缺失')
    m2.wrapper.unmount()
  })

  it('quit 回执：forced=false 走 state-info；Quit 异常仅 state-error「退出失败：」（catch 不弹 toast，与 launch 不对称现状）', async () => {
    stubDefaults({ state: 'running', version: '2.2.2' }, [installed222])
    svc.Quit.mockResolvedValue({ message: '已优雅退出', forced: false })
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.snipaste-control-panel .btn-group .btn')[1].trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    const box = wrapper.find('.state-box[aria-live="polite"]')
    expect(box.classes()).toContain('state-info')
    expect(box.text()).toBe('已优雅退出')
    expect(useToast().toastMsg.value).toBe('已优雅退出')
    wrapper.unmount()

    stubDefaults({ state: 'running', version: '2.2.2' }, [installed222])
    svc.Quit.mockRejectedValue(new Error('管道断了'))
    const m2 = await mountInKeepAlive()
    await m2.wrapper.findAll('.snipaste-control-panel .btn-group .btn')[1].trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    const errBox = m2.wrapper.find('.state-box[aria-live="polite"]')
    expect(errBox.classes()).toContain('state-error')
    expect(errBox.text()).toBe('退出失败：管道断了')
    expect(useToast().toastMsg.value).not.toContain('退出失败') // quit catch 不弹 toast（现状与 launch 不对称）
    m2.wrapper.unmount()
  })

  it('quitting 全貌：状态词「正在退出」+ data-state、启动钮禁用（ownedRunning 含 quitting）', async () => {
    stubDefaults({ state: 'quitting', version: '2.2.2' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    const status = wrapper.find('.snipaste-status')
    expect(status.text()).toBe('正在退出')
    expect(status.attributes('data-state')).toBe('quitting')
    const btns = wrapper.findAll('.snipaste-control-panel .btn-group .btn')
    expect(btns[0].attributes('disabled')).toBeDefined()
    expect(btns[1].attributes('disabled')).toBeDefined() // 「正在退出…」+ 门禁（点击面已另锁）
    wrapper.unmount()
  })

  it('external 快照=不认领：状态词兜底「本会话未托管」但 data-state 透出、启动可用退出禁用（无强杀面）', async () => {
    stubDefaults({ state: 'stopped' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['snipaste:instance-state']({ data: { state: 'external', version: '2.2.2', pid: 5 } })
    await nextTick()
    const status = wrapper.find('.snipaste-status')
    expect(status.text()).toBe('本会话未托管') // stateText 词表外兜底
    expect(status.attributes('data-state')).toBe('external')
    const btns = wrapper.findAll('.snipaste-control-panel .btn-group .btn')
    expect(btns[0].attributes('disabled')).toBeUndefined() // ownedRunning 不含 external
    expect(btns[1].attributes('disabled')).toBeDefined()
    expect(wrapper.find('.version-value').text()).toBe('2.2.2')
    wrapper.unmount()
  })

  it('尚无版本引导：「尚未安装」+ 前往版本管理钮在场、启动钮门禁不放行', async () => {
    stubDefaults({ state: 'stopped' }, [], [], '')
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.version-value').text()).toBe('尚未安装')
    expect(wrapper.find('.muted-copy').text()).toContain('先下载官网免安装版')
    const btns = wrapper.findAll('.snipaste-control-panel .btn-group .btn')
    expect(btns[0].text()).toBe('前往版本管理')
    expect(btns[1].attributes('disabled')).toBeDefined()
    await btns[1].trigger('click')
    await flushMicrotasks()
    expect(svc.Launch).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('SnipasteView 票据路径补锁', () => {
  it('RPC 失败路径：pending 票据转失败 + rowError 原文 + 重试链重发并覆新票据', async () => {
    stubDefaults({ state: 'stopped' }, [], [release223b], '')
    svc.DownloadVersion.mockRejectedValue(new Error('官网 404'))
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.tbl .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(wrapper.find('.snipaste-ver-status.error').text()).toBe('失败')
    expect(wrapper.find('.snipaste-row-error').text()).toBe('官网 404')
    svc.DownloadVersion.mockResolvedValue('in-progress')
    await wrapper.find('#snipaste-versions-panel .link-button').trigger('click') // 重试
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.download-stage').text()).toBe('已有下载任务正在进行')
    wrapper.unmount()
  })

  it('already-installed 回执：本地刷新成功即清票回「已安装」（无 900ms 延时票据残留）', async () => {
    stubDefaults({ state: 'stopped' }, [], [release223b], '')
    // 动态本地清单：点击下载钮时后端报已安装，刷新即见 2.2.3（清票走 refreshed 成功支）
    let localList: unknown[] = []
    svc.ListInstalledVersions.mockImplementation(() => Promise.resolve(localList))
    svc.DownloadVersion.mockResolvedValue('already-installed')
    const { wrapper } = await mountInKeepAlive()
    localList = [installed223]
    await wrapper.find('.tbl .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(wrapper.find('.download-stage').exists()).toBe(false)
    expect(wrapper.find('.snipaste-row-error').exists()).toBe(false) // 刷新成功不挂行错（区别于失败支现词）
    expect(wrapper.find('.snipaste-ver-status.installed').text()).toBe('已安装')
    expect(wrapper.find('.installed-label').text()).toBe('已安装')
    wrapper.unmount()
  })

  it('cancelCleanup 路径：900ms 窗口内第二次 done 事件撤旧计时器、清票时刻重锚', async () => {
    vi.useFakeTimers()
    try {
      stubDefaults({ state: 'stopped' }, [installed223], [release223b], '2.2.2')
      const { wrapper } = await mountInKeepAlive()
      runtime.handlers['snipaste:version-download']({ data: { version: '2.2.3', stage: 'done', done: 100, total: 100, message: '' } })
      await vi.advanceTimersByTimeAsync(10) // done#1：本地命中 → 排 900ms 清票计时器
      expect(wrapper.find('.download-stage').text()).toBe('安装完成，正在同步本地版本')
      await vi.advanceTimersByTimeAsync(400) // t≈410
      runtime.handlers['snipaste:version-download']({ data: { version: '2.2.3', stage: 'done', done: 100, total: 100, message: '' } })
      await vi.advanceTimersByTimeAsync(10) // done#2：cancelCleanup 撤旧计时器再重排
      await vi.advanceTimersByTimeAsync(850) // t≈1270 > done#1 的 910：若无取消票据早该消失
      expect(wrapper.find('.download-stage').exists()).toBe(true)
      await vi.advanceTimersByTimeAsync(100) // t≈1370 > done#2 的 1320：按新锚点清票
      expect(wrapper.find('.download-stage').exists()).toBe(false)
      expect(wrapper.find('.installed-label').exists()).toBe(true)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('SnipasteView 校验方言与卸载门禁', () => {
  it('校验方式三态：装机层 official-sha1/本地导入/弱校验 + 官网列有/无哈希两态', async () => {
    stubDefaults({ state: 'stopped' }, [
      installed222,
      { ...installed222, version: '2.0.0', verificationMode: 'local-import+layout', isImport: true },
      { ...installed222, version: '1.9.0', verificationMode: 'size+zip-crc+layout', officialHash: '' },
    ], [release223b, { ...release223b, version: '2.2.4', officialHash: '' }], '2.2.2')
    const { wrapper } = await mountInKeepAlive()
    const metas = wrapper.findAll('.inst-meta').map((m) => m.text())
    expect(metas.some((t) => t.includes('官方 SHA-1 + 大小 + ZIP CRC + 布局'))).toBe(true)
    expect(metas.some((t) => t.includes('本地导入 + 布局检查'))).toBe(true)
    expect(metas.some((t) => t.includes('大小 + ZIP CRC + 布局') && !t.includes('官方'))).toBe(true)
    const rows = wrapper.findAll('.tbl tbody tr')
    expect(rows[0].findAll('td')[4].text()).toBe('官方 SHA-1') // 官网列方言：有 officialHash
    expect(rows[1].findAll('td')[4].text()).toBe('大小 + ZIP CRC + 布局') // 无哈希降级
    wrapper.unmount()
  })

  it('卸载禁用原因 title 四态：running/starting/quitting 逐态现词 + 使用中优先 + 常态「卸载此版本」', async () => {
    const cases: Array<[Record<string, unknown>, string]> = [
      [{ state: 'running', version: '2.2.2' }, '该版本正在运行，请先退出进程'],
      [{ state: 'starting', version: '2.2.2' }, '该版本正在启动'],
      [{ state: 'quitting', version: '2.2.2' }, '该版本正在退出'],
    ]
    for (const [snap, title] of cases) {
      stubDefaults(snap, [installed222, installed210], [], '2.1.0')
      const { wrapper } = await mountInKeepAlive()
      const runningCard = wrapper.findAll('.installed-card').find((c) => c.text().includes('2.2.2'))!
      const u = runningCard.findAll('button').find((b) => b.text() === '卸载')!
      expect(u.attributes('disabled')).toBeDefined()
      expect(u.attributes('title')).toBe(title)
      const activeCard = wrapper.findAll('.installed-card').find((c) => c.text().includes('2.1.0'))!
      expect(activeCard.findAll('button').find((b) => b.text() === '卸载')!.attributes('title'))
        .toBe('当前使用版本不可卸载，请先选择其他版本') // active 优先于其它理由
      wrapper.unmount()
    }
    stubDefaults({ state: 'stopped' }, [installed222, installed210], [], '2.2.2')
    const { wrapper } = await mountInKeepAlive()
    const plain = wrapper.findAll('.installed-card').find((c) => c.text().includes('2.1.0'))!
      .findAll('button').find((b) => b.text() === '卸载')!
    expect(plain.attributes('disabled')).toBeUndefined()
    expect(plain.attributes('title')).toBe('卸载此版本')
    wrapper.unmount()
  })

  it('设为使用：toast「已将 X 设为启动版本」+「当前使用」徽标迁移；失败仅行内 rowError', async () => {
    stubDefaults({ state: 'stopped' }, [installed222, installed210], [], '2.2.2')
    svc.SetActiveVersion.mockResolvedValue('2.1.0')
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.installed-card').find((c) => c.text().includes('2.1.0'))!
      .findAll('button').find((b) => b.text() === '设为使用')!.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已将 2.1.0 设为启动版本')
    const moved = wrapper.findAll('.installed-card').find((c) => c.text().includes('2.1.0'))!
    expect(moved.text()).toContain('当前使用')
    expect(moved.findAll('button').map((b) => b.text())).not.toContain('设为使用')
    wrapper.unmount()

    stubDefaults({ state: 'stopped' }, [installed222, installed210], [], '2.2.2')
    svc.SetActiveVersion.mockRejectedValue(new Error('目录不存在'))
    const m2 = await mountInKeepAlive()
    await m2.wrapper.findAll('.installed-card').find((c) => c.text().includes('2.1.0'))!
      .findAll('button').find((b) => b.text() === '设为使用')!.trigger('click')
    await flushMicrotasks()
    expect(m2.wrapper.find('.snipaste-row-error').text()).toBe('目录不存在')
    expect(useToast().toastMsg.value).not.toContain('目录不存在') // setActive 失败不弹 toast（现状，仅行内报错）
    m2.wrapper.unmount()
  })
})

describe('SnipasteView 表方言与 extras 缺席形态', () => {
  it('官网表六列方言：有校验列、无 Commit/通道列；本地 fmtSize 空值「未知」与发布时间空值「未知」', async () => {
    stubDefaults({ state: 'stopped' }, [], [
      { ...release223b, isPre: true },
      { version: '2.2.4', size: 0, published: '', officialHash: '', isPre: false, stale: false },
    ], '')
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.findAll('.tbl thead th').map((th) => th.text())).toEqual(['版本', '状态', '大小', '发布时间', '校验', '操作'])
    const rows = wrapper.findAll('.tbl tbody tr')
    expect(rows[0].find('.chip-warning').text()).toBe('预发布')
    expect(rows[0].findAll('td')[2].text()).toBe('30.0 MB')
    expect(rows[1].findAll('td')[2].text()).toBe('未知') // 本视图 fmtSize 方言（家族标准形是「—」）
    expect(rows[1].findAll('td')[3].text()).toBe('未知')
    wrapper.unmount()
  })

  it('extras 缺席形态：Snipaste 无 FollowOnExit/Shutdown/桌面快捷/仓库行——勾选框与「随 Hanxi」字样绝不出现', async () => {
    expect((svc as Record<string, unknown>).GetFollowOnExit).toBeUndefined() // 桩刻意省略：误触即 TypeError
    expect((svc as Record<string, unknown>).SetFollowOnExit).toBeUndefined()
    expect((svc as Record<string, unknown>).CreateDesktopShortcut).toBeUndefined()
    stubDefaults({ state: 'stopped' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.find('.toggle-label').exists()).toBe(false)
    expect(wrapper.find('.extras-card').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('随 Hanxi')
    wrapper.unmount()
  })

  it('官网链接：现词带 URL 走 OpenOfficialSite；失败 toast「打开官网失败：」全角方言', async () => {
    stubDefaults({ state: 'stopped' }, [installed222])
    const { wrapper } = await mountInKeepAlive()
    const link = wrapper.find('.info-panel .link-button')
    expect(link.text()).toContain('打开 Snipaste 官网 · https://www.snipaste.com')
    await link.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenOfficialSite).toHaveBeenCalledTimes(1)
    svc.OpenOfficialSite.mockRejectedValue(new Error('无浏览器'))
    await link.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('打开官网失败：无浏览器')
    wrapper.unmount()
  })

  it('KeepAlive 回访：onActivated 显式重拉本地版本与状态（轮询 immediateFirstRun=false 不补首拍）', async () => {
    stubDefaults({ state: 'stopped' }, [installed222])
    const { wrapper, show } = await mountInKeepAlive()
    const local = svc.ListInstalledVersions.mock.calls.length
    const st = svc.GetStatus.mock.calls.length
    show.value = false
    await nextTick()
    show.value = true
    await nextTick()
    await flushMicrotasks()
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThan(local)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(st)
    wrapper.unmount()
  })
})
