// ManagedVersionPanel 契约测试（Wave 5 · 批 0）：
// 锁死共享版本面板对 adapter 的消费面——有/无 active 双形态、进度键归一
// （vscode 型复合键）、双空态、下载→事件流（进度驻留/完成重拉/失败重试）
// 与导入钮能力自适应。面板独立挂载（自建 store），事件经 fake adapter 的
// subscribeProgress 捕获回调直推，不触真实 Wails runtime。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ManagedVersionPanel from '../ManagedVersionPanel.vue'
import type { ManagedModuleAdapter, ManagedReleaseRecord, NormalizedProgress } from '../adapter'
import { useToast } from '../../../composables/useToast'

const rel = (version: string, extra: Partial<ManagedReleaseRecord> = {}): ManagedReleaseRecord => ({
  version,
  published: '2026-08-01T00:00:00Z',
  size: 8 * 1024 * 1024,
  isPre: false,
  ...extra,
})

const rec = (version: string) => ({
  version,
  exePath: `C:\\data\\${version}\\tool.exe`,
  dir: `C:\\data\\${version}`,
  size: 8 * 1024 * 1024,
  installedAt: '2026-08-01',
  isImport: false,
  source: '',
})

const stopped = { state: 'stopped', version: '', pid: 0, error: '', startedAt: '' }

function fakeAdapter(opts: {
  installed?: ReturnType<typeof rec>[]
  releases?: ManagedReleaseRecord[]
  active?: string | null
  progressKey?: (r: ManagedReleaseRecord) => string
  copy?: ManagedModuleAdapter['copy']
} = {}) {
  const events: { instance: Array<(s: never) => void>; progress: Array<(p: NormalizedProgress) => void> } = {
    instance: [],
    progress: [],
  }
  const versions: ManagedModuleAdapter['versions'] = {
    listInstalled: vi.fn(async () => opts.installed ?? []) as never,
    listReleases: vi.fn(async () => opts.releases ?? []) as never,
    download: vi.fn(async () => ({})) as never,
    remove: vi.fn(async () => ({ message: '已卸载', reloadVersions: true })) as never,
    openDir: vi.fn(async () => ({})) as never,
  }
  if (opts.active !== null) {
    versions.getActive = vi.fn(async () => opts.active ?? '') as never
    versions.setActive = vi.fn(async (v: string) => ({ message: `已将 ${v} 设为使用版本`, activeVersion: v })) as never
  }
  if (opts.progressKey) versions.progressKey = opts.progressKey
  const adapter: ManagedModuleAdapter = {
    getStatus: vi.fn(async () => ({ ...stopped })) as never,
    subscribeInstanceState: (cb) => events.instance.push(cb as never),
    subscribeProgress: (cb) => events.progress.push(cb),
    versions,
    copy: opts.copy,
  }
  return { adapter, events }
}

async function mountPanel(adapter: ManagedModuleAdapter) {
  const w = mount(ManagedVersionPanel, { props: { adapter } })
  // 真实/fake 双定时器兼容：onMounted 首拉全在微任务链上
  await flushPromises()
  await flushPromises()
  return w
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('ManagedVersionPanel 有/无 active 双形态', () => {
  it('多版本模块：active 卡高亮 + 使用中徽标 + 隐藏设为使用；其余卡可设', async () => {
    const { adapter } = fakeAdapter({
      installed: [rec('v1.0.0'), rec('v1.1.0')],
      releases: [rel('v1.1.0')],
      active: 'v1.1.0',
    })
    const w = await mountPanel(adapter)
    const cards = w.findAll('.installed-card')
    expect(cards).toHaveLength(2)
    const activeCard = cards[1]
    expect(activeCard.classes()).toContain('card-active')
    expect(activeCard.find('.badge').text()).toBe('使用中')
    expect(activeCard.findAll('button').map((b) => b.text())).toEqual(['📂 打开位置', '卸载'])
    const otherCard = cards[0]
    expect(otherCard.classes()).not.toContain('card-active')
    const setBtn = otherCard.findAll('button').find((b) => b.text() === '设为使用')!
    await setBtn.trigger('click')
    await vi.waitFor(() => expect(adapter.versions.setActive).toHaveBeenCalledWith('v1.0.0'))
    expect(useToast().toastMsg.value).toBe('已将 v1.0.0 设为使用版本')
    // 设后高亮即时迁移（store.activeVersion 由回执驱动，不重拉）
    expect(w.findAll('.installed-card')[0].find('.badge').text()).toBe('使用中')
    w.unmount()
  })

  it('单目录模块（无 getActive/setActive）：无使用中位与设钮，仍可渲染卡片与卸载', async () => {
    const { adapter } = fakeAdapter({ installed: [rec('3.1.0')], active: null })
    const w = await mountPanel(adapter)
    const card = w.find('.installed-card')
    expect(card.classes()).not.toContain('card-active')
    expect(card.findAll('button').map((b) => b.text())).toEqual(['📂 打开位置', '卸载'])
    expect(card.find('.badge').text()).toBe('官方下载')
    await card.findAll('button').find((b) => b.text() === '卸载')!.trigger('click')
    await vi.waitFor(() => expect(adapter.versions.remove).toHaveBeenCalled())
    w.unmount()
  })

  it('无 active 但运行中：快照版本卡亮「运行中」徽标', async () => {
    const { adapter, events } = fakeAdapter({ installed: [rec('3.1.0')], active: null })
    const w = await mountPanel(adapter)
    events.instance[0]({ state: 'running', version: '3.1.0', pid: 1, error: '', startedAt: '' } as never)
    await w.vm.$nextTick()
    expect(w.find('.installed-card .badge').text()).toBe('运行中')
    w.unmount()
  })
})

describe('ManagedVersionPanel 进度键归一', () => {
  it('复合键（vscode 型 form:version）：行按 progressKey 反查匹配', async () => {
    const { adapter, events } = fakeAdapter({
      releases: [rel('1.90.0')],
      progressKey: (r) => `installer:${r.version}`,
    })
    const w = await mountPanel(adapter)
    events.progress[0]({ key: 'portable:1.90.0', stage: 'downloading', done: 50, total: 100, message: '' })
    await w.vm.$nextTick()
    // 键不匹配：安装版行不受便携版进度污染
    expect(w.find('.ver-status.downloading').exists()).toBe(false)
    events.progress[0]({ key: 'installer:1.90.0', stage: 'downloading', done: 50, total: 100, message: '' })
    await w.vm.$nextTick()
    expect(w.find('.ver-status').text()).toBe('下载中')
    expect(w.find('.dl-percent').text()).toBe('50%')
    w.unmount()
  })

  it('verify/extract 阶段合并为「校验解压安装…」、resolve 等透出 message', async () => {
    const { adapter, events } = fakeAdapter({ releases: [rel('v1')] })
    const w = await mountPanel(adapter)
    events.progress[0]({ key: 'v1', stage: 'verify', done: 0, total: 0, message: '' })
    await w.vm.$nextTick()
    expect(w.find('.dl-meta-text').text()).toBe('校验解压安装…')
    events.progress[0]({ key: 'v1', stage: 'resolve', done: 0, total: 0, message: '镜像回退中' })
    await w.vm.$nextTick()
    expect(w.find('.dl-error').text()).toBe('镜像回退中')
    w.unmount()
  })
})

describe('ManagedVersionPanel 空态与导入能力自适应', () => {
  it('首用空态：引导文案 + 一键下载最新；无远程列表时降级为刷新钮', async () => {
    const { adapter } = fakeAdapter({
      releases: [rel('v9.9.9')],
      copy: { firstUseEmpty: '尚未安装示例工具——下载官方便携版，或「导入本地安装」' },
    })
    const w = await mountPanel(adapter)
    const empty = w.find('.empty-state.first-use')
    expect(empty.text()).toContain('尚未安装示例工具')
    const dlBtn = empty.findAll('button').find((b) => b.text().includes('下载最新版'))!
    expect(dlBtn.text()).toContain('v9.9.9')
    w.unmount()

    const { adapter: w2a } = fakeAdapter({ releases: [] })
    const w2 = await mountPanel(w2a)
    expect(w2.find('.empty-state button').text()).toBe('↻ 刷新远程列表')
    w2.unmount()
  })

  it('远程表空行文案走 adapter.copy.remoteUnavailable', async () => {
    const { adapter } = fakeAdapter({ releases: [], copy: { remoteUnavailable: 'GitHub API 不可达' } })
    const w = await mountPanel(adapter)
    expect(w.find('.table-container .empty-hint').text()).toBe('GitHub API 不可达')
    w.unmount()
  })

  it('无 importLocal 的模块不渲染导入钮；有时点击走 store.runImport 回执', async () => {
    const { adapter } = fakeAdapter({ releases: [rel('v1')] })
    const w = await mountPanel(adapter)
    expect(w.findAll('.btn-group button').map((b) => b.text())).toEqual(['↻ 刷新远程列表'])
    adapter.versions.importLocal = vi.fn(async () => ({ message: '已导入 v2', reloadVersions: true }))
    const w2 = await mountPanel(adapter)
    const importBtn = w2.findAll('.btn-group button').find((b) => b.text().includes('导入本地'))!
    await importBtn.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('已导入 v2'))
    w2.unmount()
    w.unmount()
  })
})

describe('ManagedVersionPanel 下载→事件流', () => {
  it('下载安装→进度驻留→done 800ms 清票据并重拉版本；already-installed 即时回执', async () => {
    vi.useFakeTimers()
    const { adapter, events } = fakeAdapter({ releases: [rel('v1')] })
    const w = await mountPanel(adapter)
    const dlBtn = w.findAll('.tbl button').find((b) => b.text() === '下载安装')!
    await dlBtn.trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(adapter.versions.download).toHaveBeenCalled()
    // 进度事件驱动表内单元格
    events.progress[0]({ key: 'v1', stage: 'downloading', done: 1, total: 4, message: '' })
    await w.vm.$nextTick()
    expect(w.find('.dl-percent').text()).toBe('25%')
    // done：先保留 100% 观感，800ms 后清票据；期间版本区已重拉
    const releasesCallsBefore = (adapter.versions.listReleases as ReturnType<typeof vi.fn>).mock.calls.length
    events.progress[0]({ key: 'v1', stage: 'done', done: 4, total: 4, message: '' })
    await w.vm.$nextTick()
    await vi.advanceTimersByTimeAsync(900)
    expect(w.find('.download-cell').exists()).toBe(false)
    expect((adapter.versions.listReleases as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(releasesCallsBefore)
    // already-installed：回执 toast + 即时 reloadVersions
    ;(adapter.versions.download as ReturnType<typeof vi.fn>).mockResolvedValue({
      message: '版本 v1 已安装',
      reloadVersions: true,
    })
    await w.findAll('.tbl button').find((b) => b.text() === '下载安装')!.trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(useToast().toastMsg.value).toBe('版本 v1 已安装')
    w.unmount()
  })

  it('失败票据：状态列报「失败」+ 错误 tooltip，重试链回到 download', async () => {
    const { adapter, events } = fakeAdapter({ releases: [rel('v1')] })
    const w = await mountPanel(adapter)
    events.progress[0]({ key: 'v1', stage: 'error', done: 0, total: 0, message: 'sha256 不一致' })
    await w.vm.$nextTick()
    expect(w.find('.ver-status').text()).toBe('失败')
    const retry = w.find('.retry-link')
    expect(w.find('.dl-error').attributes('title')).toBe('sha256 不一致')
    await retry.trigger('click')
    expect(adapter.versions.download).toHaveBeenCalled()
    w.unmount()
  })

  it('download 抛错统一 toast「下载失败: …」', async () => {
    const { adapter } = fakeAdapter({ releases: [rel('v1')] })
    ;(adapter.versions.download as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('网络断'))
    const w = await mountPanel(adapter)
    await w.findAll('.tbl button').find((b) => b.text() === '下载安装')!.trigger('click')
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('下载失败: 网络断'))
    w.unmount()
  })
})
