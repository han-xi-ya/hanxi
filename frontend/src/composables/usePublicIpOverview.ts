import { computed, ref } from 'vue'
import type { ComputedRef, Ref } from 'vue'
import * as PublicIpAPI from '../../bindings/hanxi/internal/modules/publicip'
import type { NetworkOverview } from '../../bindings/hanxi/internal/modules/publicip/models'
import type { Adapter } from '../../bindings/hanxi/internal/platform/models'
import { getErrorMessage } from '../utils/errors'

export interface UsePublicIpOverviewReturn {
  overview: Ref<NetworkOverview | null>
  loading: Ref<boolean>
  errorMsg: Ref<string>
  activeAdapters: ComputedRef<Adapter[]>
  loadNetworkInfo: (force?: boolean) => Promise<void>
}

/**
 * 公网出口 IP 与本机网卡概览的拉取状态（PublicIpView「IP 与网卡查看」区块的领域层）。
 *
 * 契约为「进页拉一次 + 手动强制刷新」，无周期轮询，故不涉及 usePolling 的
 * KeepAlive onActivated/onDeactivated 生命周期纪律（docs/FRONTEND.md §6 铁律 3）。
 * 错误文案「获取网络配置信息失败: 」属业务契约，逐字保留。
 */
export function usePublicIpOverview(): UsePublicIpOverviewReturn {
  const overview = ref<NetworkOverview | null>(null)
  const loading = ref(false)
  const errorMsg = ref('')

  // 过滤出有意义的活跃网卡
  const activeAdapters = computed<Adapter[]>(() => {
    if (!overview.value?.adapters) return []
    return overview.value.adapters.filter(a => {
      if (a.isLoopback) return false
      return (a.ipv4 && a.ipv4.length > 0) || (a.ipv6 && a.ipv6.length > 0)
    })
  })

  async function loadNetworkInfo(force = false) {
    loading.value = true
    errorMsg.value = ''
    try {
      overview.value = await PublicIpAPI.PublicIPService.GetNetworkOverview(force)
    } catch (e: unknown) {
      errorMsg.value = `获取网络配置信息失败: ${getErrorMessage(e)}`
    } finally {
      loading.value = false
    }
  }

  return { overview, loading, errorMsg, activeAdapters, loadNetworkInfo }
}
