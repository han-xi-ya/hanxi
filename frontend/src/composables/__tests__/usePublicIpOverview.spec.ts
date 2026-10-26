// usePublicIpOverview 领域层直测：activeAdapters 过滤口径的边界成员与
// loadNetworkInfo 的错误契约。视图层锚过「回环/无地址网卡被过滤」的渲染结果，
// 本处钉死规则本体的两类边角（纯 v6 网卡必须保留——它是 IPv6 查看功能的存在前提）。
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePublicIpOverview } from '../usePublicIpOverview'

const svc = vi.hoisted(() => ({
  GetNetworkOverview: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/publicip', () => ({ PublicIPService: svc }))

function adapter(over: Record<string, unknown> = {}) {
  return {
    name: 'A', description: '', isPhysical: false, isLoopback: false,
    ipv4: [], ipv6: [], ipv6Details: [], gateway: '', ipv6Gateway: '', dnsServers: [], mac: '',
    ...over,
  }
}

afterEach(() => {
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

describe('usePublicIpOverview', () => {
  it('activeAdapters 过滤口径：回环出局、v4/v6 任一有址即留、双空出局；概览未至为空数组', async () => {
    svc.GetNetworkOverview.mockResolvedValue({
      adapters: [
        adapter({ name: 'Loopback', isLoopback: true, ipv4: ['127.0.0.1'] }), // 有址但回环：仍出局
        adapter({ name: 'v6Only', ipv6: ['fd00::1'] }), // 纯 v6 网卡：IPv6 功能的主角，必须保留
        adapter({ name: 'Down' }), // 双空：出局
        adapter({ name: 'WLAN', ipv4: ['192.168.1.66'] }),
      ],
    })
    const api = usePublicIpOverview()
    expect(api.activeAdapters.value).toEqual([]) // 概览未落地不炸渲染
    await api.loadNetworkInfo()
    expect(api.activeAdapters.value.map((a) => a.name)).toEqual(['v6Only', 'WLAN']) // 序=后端原序
  })

  it('拉取失败逐字契约「获取网络配置信息失败: <原因>」，重拉成功后错误自动让位', async () => {
    svc.GetNetworkOverview.mockRejectedValueOnce(new Error('netsh 拒绝访问'))
    const api = usePublicIpOverview()
    await api.loadNetworkInfo(true)
    expect(svc.GetNetworkOverview).toHaveBeenCalledWith(true) // force 逐字透传
    expect(api.errorMsg.value).toBe('获取网络配置信息失败: netsh 拒绝访问')
    svc.GetNetworkOverview.mockResolvedValueOnce({ adapters: [] })
    await api.loadNetworkInfo()
    expect(svc.GetNetworkOverview).toHaveBeenLastCalledWith(false) // 默认非强制
    expect(api.errorMsg.value).toBe('')
    expect(api.loading.value).toBe(false)
  })
})
