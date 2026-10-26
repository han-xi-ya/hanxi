// useNotification 契约：Toast 浮窗与历史列表的定时器对称清理（5s 自动消失、手动
// 移除清计时器、超限挤掉清计时器、同 id 重复推送不叠卡）、字段补齐、历史上限与
// markAsRead/loadHistory 的后端交互。断言外部可验证事实：列表长度/内容、RPC 调用
// 与在途计时器数（vi.getTimerCount），不镜像实现内部结构。
// bindings 经 vi.mock 打桩（全仓测试 seam 约定）；模块级单例状态经 resetModules 逐例隔离。
import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetHistory: vi.fn().mockResolvedValue([]),
  MarkAsRead: vi.fn().mockResolvedValue(undefined),
  MarkAllAsRead: vi.fn().mockResolvedValue(undefined),
  ClearHistory: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../bindings/hanxi/internal/notify', () => ({
  NotificationService: svc,
}))

type NotifyMod = typeof import('../useNotification')

/** 每例重置模块级单例（activeToasts/notifications 是模块态，须隔离）。 */
async function fresh(): Promise<NotifyMod> {
  vi.resetModules()
  return await import('../useNotification')
}

const notif = (over: Partial<Record<string, unknown>> = {}) =>
  ({
    id: 'n1',
    moduleId: 'fileshare',
    title: '同步完成',
    message: '3 个文件已同步',
    level: 'success',
    route: '/ext/fileshare',
    timestamp: 1700000000000,
    read: false,
    ...over,
  }) as never

beforeEach(() => {
  vi.useFakeTimers()
  svc.GetHistory.mockResolvedValue([])
  svc.MarkAsRead.mockResolvedValue(undefined)
  svc.MarkAllAsRead.mockResolvedValue(undefined)
  svc.ClearHistory.mockResolvedValue(undefined)
})

describe('useNotification.pushToast 浮窗生命周期', () => {
  it('推送后 5s 内可见、到点恰好自动消失且不留残计时器', async () => {
    const { useNotification } = await fresh()
    const { activeToasts } = useNotification()
    useNotification().pushToast(notif())
    expect(activeToasts.value).toHaveLength(1)
    vi.advanceTimersByTime(4999)
    expect(activeToasts.value).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(activeToasts.value).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('手动 removeToast 同时清除其计时器（splice 不算清理）', async () => {
    const { useNotification } = await fresh()
    const { activeToasts, removeToast } = useNotification()
    useNotification().pushToast(notif({ id: 'rm1' }))
    removeToast('rm1')
    expect(activeToasts.value).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
    vi.advanceTimersByTime(10000) // 计时器若未清，到点会对空列表再跑一轮
    expect(activeToasts.value).toHaveLength(0)
  })

  it('最多 4 张浮卡：第 5 张挤掉最旧且被挤卡的计时器一并作废', async () => {
    const { useNotification } = await fresh()
    const { activeToasts } = useNotification()
    const api = useNotification()
    for (let i = 0; i < 5; i++) api.pushToast(notif({ id: `t${i}` }))
    expect(activeToasts.value).toHaveLength(4)
    expect(activeToasts.value[0].id).toBe('t4') // 最新在前
    expect(vi.getTimerCount()).toBe(4) // 被挤掉的 t0 计时器已清，不在途
    vi.advanceTimersByTime(5000)
    expect(activeToasts.value).toHaveLength(0)
  })

  it('同 id 重复推送只留一张最新卡：杜绝双键叠卡与旧计时器误清新卡致旧卡永久滞留', async () => {
    const { useNotification } = await fresh()
    const { activeToasts } = useNotification()
    const api = useNotification()
    api.pushToast(notif({ id: 'dup', message: '第一次' }))
    api.pushToast(notif({ id: 'dup', message: '第二次' }))
    expect(activeToasts.value).toHaveLength(1)
    expect(activeToasts.value[0].message).toBe('第二次')
    expect(vi.getTimerCount()).toBe(1) // 旧卡计时器已清，只剩新卡一路
    vi.advanceTimersByTime(5000)
    expect(activeToasts.value).toHaveLength(0) // 旧实现此处会滞留一张无人收的卡
  })

  it('缺字段通知补齐：无 id 生成非空唯一 id、无 title 落「系统通知」、无 level 落 info', async () => {
    const { useNotification } = await fresh()
    const { notifications, activeToasts } = useNotification()
    const api = useNotification()
    api.pushToast(notif({ id: '', title: '', level: '' }))
    const n = notifications.value[0]
    expect(typeof n.id).toBe('string')
    expect(n.id.length).toBeGreaterThan(0)
    expect(n.title).toBe('系统通知')
    expect(n.level).toBe('info')
    expect(activeToasts.value[0].id).toBe(n.id) // 浮卡与历史同 id（removeToast/markAsRead 锚点）
    api.pushToast(notif({ id: '' }))
    expect(notifications.value[0].id).not.toBe(n.id) // 再次生成不撞已有 id
  })
})

describe('useNotification 历史与后端交互', () => {
  it('loadHistory 对 null 后端回包按空列表处理（视图侧不炸）', async () => {
    const { useNotification } = await fresh()
    const { notifications, loadHistory } = useNotification()
    svc.GetHistory.mockResolvedValueOnce(null)
    notifications.value = [notif({ id: 'stale' })]
    await loadHistory()
    expect(notifications.value).toEqual([])
  })

  it('markAsRead 以后端成功为准回写本地 read；clearHistory 双清', async () => {
    const { useNotification } = await fresh()
    const { notifications, markAsRead, clearHistory } = useNotification()
    const api = useNotification()
    api.pushToast(notif({ id: 'm1' }))
    await markAsRead('m1')
    expect(svc.MarkAsRead).toHaveBeenCalledWith('m1')
    expect(notifications.value.find((n) => n.id === 'm1')?.read).toBe(true)
    await clearHistory()
    expect(svc.ClearHistory).toHaveBeenCalledTimes(1)
    expect(notifications.value).toHaveLength(0)
  })

  it('历史上限 100：第 101 条挤掉最旧一条', async () => {
    const { useNotification } = await fresh()
    const { notifications } = useNotification()
    const api = useNotification()
    for (let i = 0; i < 101; i++) api.pushToast(notif({ id: `h${i}`, timestamp: i }))
    expect(notifications.value).toHaveLength(100)
    expect(notifications.value.some((n) => n.id === 'h0')).toBe(false)
    expect(notifications.value[0].id).toBe('h100')
  })

  it('同 id 重复推送历史不叠条目：原位替换保留最新载荷', async () => {
    const { useNotification } = await fresh()
    const { notifications } = useNotification()
    const api = useNotification()
    api.pushToast(notif({ id: 'd1', message: '旧' }))
    api.pushToast(notif({ id: 'd1', message: '新' }))
    expect(notifications.value).toHaveLength(1)
    expect(notifications.value[0].message).toBe('新')
  })

  it('unreadCount 只数未读；markAllAsRead 成功即清零', async () => {
    const { useNotification } = await fresh()
    const { unreadCount, markAllAsRead } = useNotification()
    const api = useNotification()
    api.pushToast(notif({ id: 'u1', read: false }))
    api.pushToast(notif({ id: 'u2', read: true }))
    expect(unreadCount.value).toBe(1)
    await markAllAsRead()
    expect(svc.MarkAllAsRead).toHaveBeenCalledTimes(1)
    expect(unreadCount.value).toBe(0)
  })
})
