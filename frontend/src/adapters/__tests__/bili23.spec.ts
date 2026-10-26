// ============================================================================
// Bili23 adapter · 退出三态回执与强杀在途闩（本模块核心语义的取数场）
//
// 断言的均为可观察外部事实：
//  - control.quit 把后端 QuitOutcome.message 逐字透传（stopped/hidden/asked 三态
//    差异话术禁止被前端合并或加工——toast 词面即后端逐态分化本身）；
//  - 实例态事件只当迁移信号用：收到推送后必须回读 GetStatus、以聚合 Status 为
//    单一状态源交付（引擎 Snapshot 不带 windowVisible，直接转发=托盘态永远投不出）；
//  - running+窗口隐藏时状态词/灯色双覆写（「运行中 · 已收入托盘」+ warn 档），
//    其余状态回落 TOOL_STATE_META 单源；
//  - 强杀钮：确认闸未裁决（false）不发 RPC；在途期重入被闩拒入且不重复弹确认；
//    成功回执经 showToast 原样上墙并回 true；失败回 false 且闩释放；
//  - external 态钮位分叉：quit 禁用、danger 仍可点（点了只领"不越权"回执）。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  ForceStop: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))
const toasts = vi.hoisted(() => ({ showToast: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/bili23/service', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))
vi.mock('../../composables/useToast', () => ({ useToast: () => toasts }))

import { createBili23Adapter } from '../bili23'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('bili23 · 实例态迁移信号与订阅接线', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('事件接线：instance-state 收到迁移信号一律回读聚合 Status 交付（引擎载荷本身不出手）；version-download 单键形且缺 version 丢弃', async () => {
    const adapter = createBili23Adapter()
    const cb = vi.fn()
    adapter.subscribeInstanceState!(cb)
    expect(lastHandler().name).toBe('bili23:instance-state')

    const fresh = { state: 'running', version: '1.9.0', pid: 7, error: '', startedAt: '', windowVisible: false }
    svc.GetStatus.mockResolvedValueOnce(fresh)
    lastHandler().handler({ state: 'running', pid: 7 })
    await flush()
    // 交付的是回读的聚合 Status（含 windowVisible），而非引擎快照本体
    expect(cb).toHaveBeenCalledWith(fresh)

    // 空载荷连回读都不该触发（无事发生）；回读为空不投影（视图拿 null=无新事实）
    cb.mockClear()
    svc.GetStatus.mockClear()
    lastHandler().handler(null)
    expect(svc.GetStatus).not.toHaveBeenCalled()
    svc.GetStatus.mockResolvedValueOnce(null)
    lastHandler().handler({ state: 'stopped' })
    await flush()
    expect(cb).not.toHaveBeenCalled()

    // 进度面走单键归一（key=version），缺 version 的畸形帧丢弃
    const pcb = vi.fn()
    adapter.subscribeProgress(pcb)
    expect(lastHandler().name).toBe('bili23:version-download')
    lastHandler().handler({ version: '1.9.0', stage: 'downloading', done: 1, total: 2, message: '下载中' })
    expect(pcb).toHaveBeenCalledWith({ key: '1.9.0', stage: 'downloading', done: 1, total: 2, message: '下载中' })
    pcb.mockClear()
    lastHandler().handler({ version: '', stage: 'done', done: 1, total: 1, message: '' })
    expect(pcb).not.toHaveBeenCalled()
  })
})

describe('bili23 · 状态词/灯色/退出三态回执', () => {
  it('running+窗口隐藏：状态词如实"已收入托盘"、灯位转 warn；可见时不吞五态词表单源', () => {
    const adapter = createBili23Adapter()
    expect(adapter.stateText!({ state: 'running', windowVisible: false } as never)).toBe('运行中 · 已收入托盘')
    expect(adapter.statusTone!({ state: 'running', windowVisible: false } as never)).toBe('warn')
    // 窗口可见：词面必须回到 TOOL_STATE_META 的「运行中」、灯档回到状态本体
    expect(adapter.stateText!({ state: 'running', windowVisible: true } as never)).toBe('运行中')
    expect(adapter.statusTone!({ state: 'running', windowVisible: true } as never)).toBe('running')
    expect(adapter.stateText!({ state: 'failed', windowVisible: true } as never)).toBe('异常退出')
    // banner：hidden 态给琥珀指引，stopped 无横幅
    expect(adapter.banner!({ state: 'running', windowVisible: false } as never, undefined as never)?.tone).toBe('warn')
    expect(adapter.banner!({ state: 'stopped' } as never, undefined as never)).toBeNull()
  })

  it('quit 三态回执逐字透传（不合并成单一"已退出"）；external 态 quit 禁用而 danger 仍可点', async () => {
    const adapter = createBili23Adapter()
    svc.Quit.mockResolvedValueOnce({ message: '已收入其托盘，进程仍在托管' })
    expect(await adapter.control!.quit!.run()).toEqual({ message: '已收入其托盘，进程仍在托管' })
    svc.Quit.mockResolvedValueOnce({ message: '弹窗询问中' })
    expect(await adapter.control!.quit!.run()).toEqual({ message: '弹窗询问中' })

    svc.OpenWindow.mockResolvedValueOnce({ message: '已启动' })
    expect(await adapter.control!.primary!.run()).toEqual({ message: '已启动' })

    expect(adapter.control!.primary!.disabledFor!('starting')).toBe(true)
    expect(adapter.control!.quit!.disabledFor!('external')).toBe(true)
    expect(adapter.danger.disabledFor('external')).toBe(false)
    expect(adapter.danger.titleFor('external')).toBe('外部实例不在 Hanxi 管辖范围')
    expect(adapter.danger.disabledFor('stopped')).toBe(true)
  })
})

describe('bili23 · 强杀在途闩（danger.run 回执分叉）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('确认拒绝与 RPC 失败都回 false：拒绝不发 ForceStop，失败话术进 toast 不裸抛', async () => {
    const adapter = createBili23Adapter()
    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.danger.run()).toBe(false)
    expect(svc.ForceStop).not.toHaveBeenCalled()

    ui.confirm.mockResolvedValueOnce(true)
    svc.ForceStop.mockRejectedValueOnce(new Error('拒绝访问'))
    expect(await adapter.danger.run()).toBe(false)
    expect(toasts.showToast).toHaveBeenCalledWith('强制结束失败: 拒绝访问')
  })

  it('在途期重入被闩拒入（不重复弹确认框）；裁决后单次 RPC、回执 toast 原样、返回 true', async () => {
    const adapter = createBili23Adapter()
    let settle!: (v: boolean) => void
    ui.confirm.mockImplementation(() => new Promise<boolean>((r) => { settle = r }))

    const first = adapter.danger.run()
    const reentry = await adapter.danger.run() // 确认未裁决期间的并发点击
    expect(reentry).toBe(false)
    expect(ui.confirm).toHaveBeenCalledTimes(1)

    svc.ForceStop.mockResolvedValueOnce({ message: '已强制结束 Bili23' })
    settle(true)
    expect(await first).toBe(true)
    expect(svc.ForceStop).toHaveBeenCalledTimes(1)
    expect(toasts.showToast).toHaveBeenCalledWith('已强制结束 Bili23')

    // 闩已释放：裁决后再次进入可再弹确认
    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.danger.run()).toBe(false)
  })

})
