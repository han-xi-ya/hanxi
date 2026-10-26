// usePublicIpDiagnostics 领域层直测：Ping/Traceroute 状态机的守卫分支。
// 视图层已锚过「空目标钮禁用」「Ping Tab Enter 在飞单飞」，本 spec 补的是
// 不经钮禁用的直调路径与 Traceroute 侧同构守卫（视图从未实测的那半边）。
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePublicIpPing, usePublicIpTraceroute } from '../usePublicIpDiagnostics'

const svc = vi.hoisted(() => ({
  PingTarget: vi.fn(),
  TraceRoute: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/publicip', () => ({ PublicIPService: svc }))

afterEach(() => {
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

describe('usePublicIpPing', () => {
  it('目标全空白：run 静默无操作——不触后端、不进 loading、不写错误', async () => {
    const api = usePublicIpPing()
    api.targetInput.value = '   '
    await api.run()
    expect(svc.PingTarget).not.toHaveBeenCalled()
    expect(api.loading.value).toBe(false)
    expect(api.error.value).toBe('')
    expect(api.result.value).toBeNull()
  })

  it('探测失败：错误文案契约「Ping 执行失败: <原因>」逐字，落定后可再发起', async () => {
    svc.PingTarget.mockRejectedValue(new Error('icmp timeout'))
    const api = usePublicIpPing()
    await api.run()
    expect(api.error.value).toBe('Ping 执行失败: icmp timeout')
    expect(api.loading.value).toBe(false)
    // 失败不留闸：下一次 run 必须能真正打到后端
    svc.PingTarget.mockResolvedValue({ results: [], packetsSent: 4, packetsReceived: 0 } as never)
    await api.run()
    expect(svc.PingTarget).toHaveBeenCalledTimes(2)
  })
})

describe('usePublicIpTraceroute', () => {
  it('在飞单飞：探测中重复 run 拒绝重入，收口后放行下一发', async () => {
    let settleFirst: (v: unknown) => void = () => {}
    svc.TraceRoute.mockReturnValueOnce(new Promise((r) => (settleFirst = r)))
    const api = usePublicIpTraceroute()
    const first = api.run()
    expect(api.loading.value).toBe(true)
    await api.run() // 在途直调第二发：闸门必须拒绝（跨 Tab 快捷入口同用此闸门）
    await api.run()
    expect(svc.TraceRoute).toHaveBeenCalledTimes(1)
    settleFirst({ hops: [], reached: false })
    await first
    expect(api.loading.value).toBe(false)
    svc.TraceRoute.mockResolvedValue({ hops: [], reached: true } as never)
    await api.run()
    expect(svc.TraceRoute).toHaveBeenCalledTimes(2)
  })

  it('目标裁剪后传参：粘贴带空白的目标照常发起', async () => {
    svc.TraceRoute.mockResolvedValue({ hops: [], reached: true })
    const api = usePublicIpTraceroute()
    api.targetInput.value = '  223.5.5.5  '
    api.maxHops.value = 12
    await api.run()
    expect(svc.TraceRoute).toHaveBeenCalledWith('223.5.5.5', 12)
    expect(api.result.value).toEqual({ hops: [], reached: true })
  })
})
