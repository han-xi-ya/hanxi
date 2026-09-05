import { ref } from 'vue'
import type { Ref } from 'vue'
import * as PublicIpAPI from '../../bindings/hanxi/internal/modules/publicip'
import type { PingSummary, TracerouteSummary } from '../../bindings/hanxi/internal/modules/publicip/models'
import { getErrorMessage } from '../utils/errors'

export interface UsePublicIpPingReturn {
  targetInput: Ref<string>
  count: Ref<number>
  loading: Ref<boolean>
  result: Ref<PingSummary | null>
  error: Ref<string>
  run: () => Promise<void>
}

export interface UsePublicIpTracerouteReturn {
  targetInput: Ref<string>
  maxHops: Ref<number>
  loading: Ref<boolean>
  result: Ref<TracerouteSummary | null>
  error: Ref<string>
  run: () => Promise<void>
}

/**
 * ICMP Ping 探测状态（PublicIpView「Ping 连通性测试」区块的领域层）。
 *
 * 目标/次数由视图持有、面板组件经 v-model 双向绑定——网卡芯片的「⚡ 快捷 Ping」
 * 需要跨 Tab 回填目标并立即发起，故状态不能下沉进面板组件。
 * 结果为一次性快照（无轮询），错误文案「Ping 执行失败: 」属业务契约，逐字保留。
 */
export function usePublicIpPing(): UsePublicIpPingReturn {
  const targetInput = ref('1.1.1.1')
  const count = ref(4)
  const loading = ref(false)
  const result = ref<PingSummary | null>(null)
  const error = ref('')

  async function run() {
    if (!targetInput.value.trim()) return
    loading.value = true
    error.value = ''
    try {
      result.value = await PublicIpAPI.PublicIPService.PingTarget(targetInput.value.trim(), count.value)
    } catch (e: unknown) {
      error.value = `Ping 执行失败: ${getErrorMessage(e)}`
    } finally {
      loading.value = false
    }
  }

  return { targetInput, count, loading, result, error, run }
}

/**
 * 路由追踪（Traceroute）状态（PublicIpView「路由追踪」区块的领域层）。
 *
 * 与 Ping 同构但参数语义不同（最大跳数），故各立一个 composable 而非泛型合流，
 * 以免为省 20 行把两侧契约揉成一团。
 * 错误文案「路由追踪执行失败: 」属业务契约，逐字保留。
 */
export function usePublicIpTraceroute(): UsePublicIpTracerouteReturn {
  const targetInput = ref('1.1.1.1')
  const maxHops = ref(20)
  const loading = ref(false)
  const result = ref<TracerouteSummary | null>(null)
  const error = ref('')

  async function run() {
    if (!targetInput.value.trim()) return
    loading.value = true
    error.value = ''
    try {
      result.value = await PublicIpAPI.PublicIPService.TraceRoute(targetInput.value.trim(), maxHops.value)
    } catch (e: unknown) {
      error.value = `路由追踪执行失败: ${getErrorMessage(e)}`
    } finally {
      loading.value = false
    }
  }

  return { targetInput, maxHops, loading, result, error, run }
}
