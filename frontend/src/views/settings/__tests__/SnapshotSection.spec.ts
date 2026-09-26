// 历史版本分区特征测试：状态回填、git/备份双模式呈现（P4 解禁后同面）、立即快照、
// 按文件浏览（N33 批 A 立链、批 B 拆分后行为零回退：清单分组/时间线切换/行级对比/
// 恢复解算链——断言穿过 SnapshotFileList/SnapshotTimeline 两子件的组合渲染）、
// 预览弹窗文件清单与恢复确认链（mock 打桩范式照 SettingsSections.spec）。
// 批 D 追加：旧摘要呈现回改（版本号串→能推则推+tooltip 留原文）、备份摘要
// 整拷贝限定语、两模式版本容量整句、P8-A chip 正文去 Git 化（技术名进悬停）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnapshotSection from '../SnapshotSection.vue'
import { decorateLegacySummary } from '../../../constants/snapshotLabels'
import type { TrackedFile } from '../../../../bindings/hanxi/internal/snapshot/models'
import { useToast } from '../../../composables/useToast'
import { useConfirm } from '../../../composables/useConfirm'

const snapSvc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListRevisions: vi.fn(),
  ListFiles: vi.fn(),
  FileHistory: vi.fn(),
  DiffFile: vi.fn(),
  PreviewRevision: vi.fn(),
  RevisionDetail: vi.fn(),
  PreviewFile: vi.fn(),
  RestoreFile: vi.fn(),
  SetPreferences: vi.fn(),
  CheckpointNow: vi.fn(),
  OpenHistoryDir: vi.fn(),
}))
vi.mock('../../../../bindings/hanxi/internal/snapshot', () => ({ CheckpointService: snapSvc }))

function stubGitStatus(overrides: Record<string, unknown> = {}) {
  snapSvc.GetStatus.mockResolvedValue({
    enabled: true, mode: 'git', gitAvailable: true, lastCommitAt: '2026-09-17T14:30:00+08:00',
    revisionCount: 2, idleSeconds: 300, intervalMinutes: 5,
    snapshotDir: 'D:\\hx\\.snapshots', ...overrides,
  })
}

const revisions = [
  { id: 'aaaabbbb11112222', time: '2026-09-17T14:30:00+08:00', summary: 'config.json, memo/a.md' },
  { id: 'ccccdddd33334444', time: '2026-09-16T09:00:00+08:00', summary: 'state/projects.json' },
]

// ListFiles 桩：标题映射 Display、已删除徽标、零版本新文件各占一位（组序由后端保证）
const tracked = [
  { path: 'memo/memo_9.md', display: '被删的便签', group: 'memo', revisions: 2, lastChange: '2026-09-17T14:00:00+08:00', alive: false },
  { path: 'config.json', display: 'config.json', group: 'config', revisions: 2, lastChange: '2026-09-17T14:00:00+08:00', alive: true },
  { path: 'state/live.json', display: 'live.json', group: 'state', revisions: 0, lastChange: '2026-09-15T10:00:00+08:00', alive: true },
]

// 被删便签时间线：新→旧，D 在前、最后存在版本（M）在后
const goneHistory = [
  { revisionId: 'bbbb222233334444', time: '2026-09-17T14:00:00+08:00', status: 'D', summary: 'memo/memo_9.md' },
  { revisionId: 'aaaa111122223333', time: '2026-09-16T09:00:00+08:00', status: 'M', summary: 'memo/memo_9.md' },
]

async function mountView() {
  const w = mount(SnapshotSection)
  await flushPromises()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
})

describe('历史版本分区', () => {
  it('git 模式回填状态、偏好与版本列表', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue(revisions)
    const w = await mountView()
    expect(snapSvc.GetStatus).toHaveBeenCalled()
    expect(w.text()).toContain('数据与存储') // 分区名机主拍板：备份/历史类归数据侧
    const switches = w.findAll('.switch')
    expect((switches[0].element as HTMLInputElement).checked).toBe(true)
    expect(w.text()).toContain('版本历史（可浏览最近 50 版）') // §6 口径 + 批 D P8-A：正文去 Git 化
    expect(w.find('.chip').attributes('title')).toContain('Git') // 技术名收进悬停 tooltip
    expect(w.text()).toContain('D:\\hx\\.snapshots')
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    // 批 D 旧摘要回改：config.json 命中静态名表推为"工作台设置"；memo/a.md
    // 无账可推（本例未喂 ListFiles）原样保留；账本原文永远留在 tooltip
    expect(rows[0].findAll('td')[1].text()).toBe('工作台设置、memo/a.md')
    expect(rows[0].findAll('td')[1].attributes('title')).toBe('config.json, memo/a.md')
    expect(w.text()).toContain('最多展示最近 50 版') // 版本列表卡头容量整句（git 档）
  })

  it('旧摘要回改纯函数：命中换名、推不出原样、非文件名形态不动（P7-A 不装新不伪造）', () => {
    const files = [
      { path: 'memo/memo_9.md', display: '购物清单', group: 'memo', revisions: 1, lastChange: '', alive: true },
      { path: 'state/ocr.json', display: 'ocr.json', group: 'state', revisions: 1, lastChange: '', alive: true },
    ] as TrackedFile[]
    // 三类来源各命中一位；"等 N 个文件"后缀原样保留
    expect(decorateLegacySummary('config.json, ocr.json, memo_9.md 等 8 个文件', files))
      .toBe('工作台设置、文字识别、购物清单 等 8 个文件')
    // 全推不出：连分隔符都不动（不制造"看起来换过脸"的假象）
    expect(decorateLegacySummary('live.json, memo_1.md', files)).toBe('live.json, memo_1.md')
    // 备份计数句/未来新式整句/空串：不命中文件名形态，一律原样
    expect(decorateLegacySummary('30 个文件', files)).toBe('30 个文件')
    expect(decorateLegacySummary('删除了便签《购物清单》；修改了 文字识别', files))
      .toBe('删除了便签《购物清单》；修改了 文字识别')
    expect(decorateLegacySummary('', files)).toBe('')
  })

  it('无版本时空态给出引导文案', async () => {
    stubGitStatus({ revisionCount: 0 })
    snapSvc.ListRevisions.mockResolvedValue([])
    const w = await mountView()
    expect(w.text()).toContain('暂无历史版本')
    expect(w.findAll('tbody tr')).toHaveLength(0)
  })

  it('备份模式与 git 同面解禁（N33 P4）：照样拉版本列表与文件清单', async () => {
    stubGitStatus({ mode: 'backup', gitAvailable: false })
    snapSvc.ListRevisions.mockResolvedValue(revisions)
    snapSvc.ListFiles.mockResolvedValue(tracked)
    const w = await mountView()
    expect(w.text()).toContain('本机备份模式')
    expect(w.text()).toContain('版本历史（整份拷贝 · 保留最近 30 份）') // §6 备份 chip 口径（批 D 直书整拷贝）
    expect(w.find('.chip').attributes('title')).toContain('Git') // P8-A：降级原因收进悬停
    expect(w.text()).toContain('最近 30 份')
    expect(w.text()).toContain('最多展示最近 30 份') // 版本列表卡头容量整句（备份档如实）
    expect(snapSvc.ListRevisions).toHaveBeenCalled()
    expect(snapSvc.ListFiles).toHaveBeenCalled()
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.findAll('.fa-file')).toHaveLength(3)
    await w.findAll('.btn-small').find((b) => b.text().includes('打开目录'))!.trigger('click')
    expect(snapSvc.OpenHistoryDir).toHaveBeenCalled()
  })

  it('备份模式摘要如实（批 D）："N 个文件"是总文件数，补整拷贝限定语、原文留 tooltip', async () => {
    stubGitStatus({ mode: 'backup', gitAvailable: false })
    snapSvc.ListRevisions.mockResolvedValue([
      { id: '20260917-143000', time: '2026-09-17T14:30:00+08:00', summary: '30 个文件' },
    ])
    const w = await mountView()
    const cell = w.findAll('tbody tr')[0].findAll('td')[1]
    expect(cell.text()).toBe('整份拷贝 · 30 个文件')
    expect(cell.attributes('title')).toBe('30 个文件')
  })

  // ---------- N33 批 A：按文件浏览 ----------

  it('清单分组渲染（标题映射与已删除徽标），点选拉时间线', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue([])
    snapSvc.ListFiles.mockResolvedValue(tracked)
    snapSvc.FileHistory.mockResolvedValue(goneHistory)
    const w = await mountView()
    expect(w.text()).toContain('便签')
    expect(w.text()).toContain('被删的便签') // Display 走后端标题映射
    expect(w.text()).toContain('工作台设置')
    expect(w.text()).toContain('模块状态')
    expect(w.text()).toContain('已删除')
    const rows = w.findAll('.fa-file')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('被删的便签')

    // 批 C：观察窗口径整句——清单卡头挂"最近 50 版观察窗"（数字单源常量渲染）
    expect(w.text()).toContain('3 个受保文件 · 最近 50 版观察窗')
    await rows[0].trigger('click')
    await flushPromises()
    expect(snapSvc.FileHistory).toHaveBeenCalledWith('memo/memo_9.md', 50)
    const evRows = w.findAll('.fa-ev-row')
    expect(evRows).toHaveLength(2)
    expect(evRows[0].text()).toContain('删除')
    expect(evRows[0].text()).toContain('恢复被删内容')
    expect(evRows[1].text()).toContain('恢复')
    // 批 C：双语义常驻——便签行"即时生效"徽标在组合视图中同样成立，
    // 时间线头沿链口径整句与清单窗内整句各说各话（不硬统一数字）
    expect(evRows[0].text()).toContain('即时生效')
    // 批 D 旧摘要回改上到时间线行：memo 尾段命中受保清单标题账，原文留 tooltip
    expect(evRows[1].find('.fa-sum').text()).toBe('被删的便签')
    expect(evRows[1].find('.fa-sum').attributes('title')).toBe('memo/memo_9.md')
    expect(w.find('.fa-detail-head').text()).toContain('共 2 条版本事件（跨改名沿用）')
    expect(rows[0].find('.fa-count').text()).toBe('最近 2 次变化')
    expect(rows[1].find('.fa-scope').text()).toBe('重启生效') // config.json 行
  })

  it('时间线 D 行恢复=解算最后存在版本（病灶 A 热修复回归）', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue([])
    snapSvc.ListFiles.mockResolvedValue(tracked)
    snapSvc.FileHistory.mockResolvedValue(goneHistory)
    snapSvc.RestoreFile.mockResolvedValue(undefined)
    const { confirmState, settleConfirm } = useConfirm()

    const w = await mountView()
    await w.findAll('.fa-file')[0].trigger('click')
    await flushPromises()
    await w.findAll('.fa-ev-row')[0].findAll('button')[1].trigger('click') // D 行「恢复被删内容」
    await flushPromises()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.tone).toBe('danger')
    expect(confirmState.options.description).toContain('删除前')
    settleConfirm(true)
    await flushPromises()
    // 恢复的不是 D 版本自身，而是时间线上第一条非 D（最后存在版本）
    expect(snapSvc.RestoreFile).toHaveBeenCalledWith('aaaa111122223333', 'memo/memo_9.md')
    expect(useToast().toastMsg.value).toContain('热恢复')
  })

  it('全 D 观察窗：不盲恢复，如实提示超窗', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue([])
    snapSvc.ListFiles.mockResolvedValue(tracked)
    snapSvc.FileHistory.mockResolvedValue([
      { revisionId: 'bbbb222233334444', time: '2026-09-17T14:00:00+08:00', status: 'D', summary: '' },
    ])
    const w = await mountView()
    await w.findAll('.fa-file')[0].trigger('click')
    await flushPromises()
    await w.findAll('.fa-ev-row')[0].findAll('button')[1].trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('观察窗')
    expect(snapSvc.RestoreFile).not.toHaveBeenCalled()
  })

  it('对比内联展开：DiffFile 带版本 id 与路径，行级 diff 删加着色呈现（批 B 统一单栏）', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue([])
    snapSvc.ListFiles.mockResolvedValue(tracked)
    snapSvc.FileHistory.mockResolvedValue(goneHistory)
    snapSvc.DiffFile.mockResolvedValue({
      path: 'memo/memo_9.md', status: 'M', old: '旧内容A', new: '新内容B',
      oldTruncated: false, newTruncated: false, summary: 'memo/memo_9.md',
    })
    const w = await mountView()
    await w.findAll('.fa-file')[0].trigger('click')
    await flushPromises()
    await w.findAll('.fa-ev-row')[1].findAll('button')[0].trigger('click') // M 行「对比」
    await flushPromises()
    expect(snapSvc.DiffFile).toHaveBeenCalledWith('aaaa111122223333', 'memo/memo_9.md')
    expect(w.text()).toContain('旧内容A')
    expect(w.text()).toContain('新内容B')
    // 行级呈现：单行互改 = 一删一加，删行在前（textdiff 在子件里算，父只管喂数据）
    expect(w.findAll('.dl-del')).toHaveLength(1)
    expect(w.findAll('.dl-add')).toHaveLength(1)
    // 再点收起
    await w.findAll('.fa-ev-row')[1].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(w.text()).not.toContain('旧内容A')
  })

  it('弹窗 D 行只出「恢复被删内容」不出「内容」，恢复经解算（病灶 A）', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue(revisions)
    snapSvc.RevisionDetail.mockResolvedValue([{ path: 'memo/gone.md', status: 'D' }])
    snapSvc.FileHistory.mockResolvedValue(goneHistory)
    snapSvc.RestoreFile.mockResolvedValue(undefined)
    const { confirmState, settleConfirm } = useConfirm()

    const w = await mountView()
    await w.findAll('tbody tr')[0].findAll('button')[0].trigger('click') // 预览
    await flushPromises()
    const fileRow = w.findAll('.file-row')[0]
    const btns = fileRow.findAll('button')
    expect(btns).toHaveLength(1)
    expect(btns[0].text()).toContain('恢复被删内容')

    await btns[0].trigger('click')
    await flushPromises()
    expect(snapSvc.FileHistory).toHaveBeenCalledWith('memo/gone.md', 50)
    expect(confirmState.open).toBe(true)
    settleConfirm(true)
    await flushPromises()
    expect(snapSvc.RestoreFile).toHaveBeenCalledWith('aaaa111122223333', 'memo/gone.md')
  })

  it('立即快照触发后重拉列表', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue(revisions)
    snapSvc.CheckpointNow.mockResolvedValue(undefined)
    const w = await mountView()
    const nowBtn = w.findAll('button').find((b) => b.text().includes('立即快照'))!
    await nowBtn.trigger('click')
    expect(snapSvc.CheckpointNow).toHaveBeenCalled()
    vi.useRealTimers()
    await new Promise((r) => setTimeout(r, 900))
    await flushPromises()
    expect(snapSvc.GetStatus.mock.calls.length).toBeGreaterThan(1)
  })

  it('预览弹窗列文件清单；恢复必经确认，确认后 RestoreFile+重启提示', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue(revisions)
    snapSvc.RevisionDetail.mockResolvedValue([
      { path: 'config.json', status: 'M' },
      { path: 'memo/memo_1.md', status: 'A' },
    ])
    snapSvc.PreviewFile.mockResolvedValue({ path: 'config.json', content: '{"theme":"light"}', size: 17, truncated: false })
    snapSvc.RestoreFile.mockResolvedValue(undefined)
    const { confirmState, settleConfirm } = useConfirm()

    const w = await mountView()
    await w.findAll('tbody tr')[0].findAll('button')[0].trigger('click') // 预览
    await flushPromises()
    expect(w.text()).toContain('config.json')
    expect(w.text()).toContain('memo/memo_1.md')

    await w.findAll('.file-row')[1].findAll('button')[0].trigger('click') // 内容
    await flushPromises()
    expect(snapSvc.PreviewFile).toHaveBeenCalledWith('aaaabbbb11112222', 'memo/memo_1.md')

    await w.findAll('.file-row')[0].findAll('button')[1].trigger('click') // 恢复 config → 弹确认
    await flushPromises()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(true)
    await flushPromises()
    expect(snapSvc.RestoreFile).toHaveBeenCalledWith('aaaabbbb11112222', 'config.json')
    expect(useToast().toastMsg.value).toContain('重启')
  })

  it('偏好改动即存：开关切换携带完整三字段', async () => {
    stubGitStatus()
    snapSvc.ListRevisions.mockResolvedValue([])
    snapSvc.SetPreferences.mockResolvedValue(undefined)
    const w = await mountView()
    await w.findAll('.switch')[0].setValue(false)
    await flushPromises()
    expect(snapSvc.SetPreferences).toHaveBeenCalledWith({ enabled: false, idleSeconds: 300, intervalMinutes: 5 })
  })
})
