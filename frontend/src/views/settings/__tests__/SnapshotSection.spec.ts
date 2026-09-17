// 历史版本分区特征测试：状态回填、git/备份双模式呈现、立即快照、
// 预览弹窗文件清单与恢复确认链（mock 打桩范式照 SettingsSections.spec）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnapshotSection from '../SnapshotSection.vue'
import { useToast } from '../../../composables/useToast'
import { useConfirm } from '../../../composables/useConfirm'

const snapSvc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListRevisions: vi.fn(),
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
    const switches = w.findAll('.switch')
    expect((switches[0].element as HTMLInputElement).checked).toBe(true)
    expect(w.text()).toContain('Git 历史仓库')
    expect(w.text()).toContain('D:\\hx\\.snapshots')
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('config.json, memo/a.md')
  })

  it('无版本时空态给出引导文案', async () => {
    stubGitStatus({ revisionCount: 0 })
    snapSvc.ListRevisions.mockResolvedValue([])
    const w = await mountView()
    expect(w.text()).toContain('暂无历史版本')
    expect(w.findAll('tbody tr')).toHaveLength(0)
  })

  it('降级备份模式退化为打开目录说明（Q3 裁定），不拉版本列表', async () => {
    stubGitStatus({ mode: 'backup', gitAvailable: false })
    const w = await mountView()
    expect(w.text()).toContain('本机备份模式')
    expect(w.text()).toContain('最近 30 份')
    expect(snapSvc.ListRevisions).not.toHaveBeenCalled()
    await w.findAll('.btn-small').find((b) => b.text().includes('打开目录'))!.trigger('click')
    expect(snapSvc.OpenHistoryDir).toHaveBeenCalled()
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
