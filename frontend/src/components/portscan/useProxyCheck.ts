// 代理连通性测试的领域状态与动作（自 PortScanView 拆出）。
// 形制同 components/publicip/useDiagFormModel：状态与动作进 composable，DOM 进
// ProxyCheckPanel.vue，视图只负责标签编排——这样再加一个 tab 不必往 700 行的
// PortScanView 里继续堆。
import { ref, shallowRef } from 'vue'
import * as PortScanAPI from '../../../bindings/hanxi/internal/modules/portscan'
import type { ProxyCheckResult } from '../../../bindings/hanxi/internal/modules/portscan/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'
import { useClipboard } from '../../composables/useClipboard'

// 本机常见代理入站口，点一下填进端口栏。含义只标到"哪家的默认口"这一层：
// 具体是不是这个用途取决于本机配置——测出来不通不代表端口错，别拿它当判据。
export const PROXY_PORT_PRESETS = [
  { port: 7890, title: 'Clash 系默认 mixed 口' },
  { port: 7891, title: 'Clash 系备用口' },
  { port: 7892, title: 'Clash 系第三口' },
  { port: 1080, title: '通用 SOCKS 口' },
  { port: 8080, title: '通用 HTTP 代理口' },
  { port: 8118, title: 'Privoxy 默认口' },
]

export type ProxyKind = 'socks5' | 'http' | 'https'

/** 出口源只留主机名：并排显示时整条 URL 太长且没信息量。 */
export function sourceHost(raw?: string): string {
  if (!raw) return '未知源'
  try {
    return new URL(raw).host
  } catch {
    return raw
  }
}

export function useProxyCheck() {
  // 预填本机最常见的一格：打开面板即可直接点测试，这才是这个功能的主要用法。
  const form = ref({ kind: 'socks5' as ProxyKind, host: '127.0.0.1', port: 7890, user: '', pass: '' })
  // 低频项默认收起：绝大多数本地代理不需要认证，也不该改超时。让它们占住主路径
  // 那一行，才是真正的"不友好"。收起后若已填过账号，靠按钮文案把状态带出来。
  const credOpen = ref(false)
  const optsOpen = ref(false)
  const timeoutMs = ref(5000)
  const skipTls = ref(false)

  const result = shallowRef<ProxyCheckResult | null>(null)
  const error = ref('')
  // 单飞守卫：后端同步返回、不可取消，叠两次只会让后一次盖掉前一次还看不清
  // （调用门是计数式的、并不互斥，所以这层防重入只能在前端做）
  const { busy: checking, run } = useAsyncAction()
  const { copyWithToast } = useClipboard()

  async function test() {
    error.value = ''
    result.value = null
    const res = await run(() => PortScanAPI.PortScanService.CheckProxy({
      kind: form.value.kind,
      host: form.value.host.trim(),
      port: Number(form.value.port) || 0,
      user: form.value.user.trim(),
      pass: form.value.pass,
      timeoutMs: Number(timeoutMs.value) || 0,
      skipTlsVerify: Boolean(skipTls.value),
    }))
    if (res.ok) {
      result.value = res.data ?? null
    } else {
      // 参数不合法（空地址、端口越界、把整条 URL 塞进地址栏…）由后端一处判并
      // 如实给中文原因——前端不复制一份校验，两处迟早漂移。
      error.value = getErrorMessage(res.error)
    }
  }

  async function copyResult() {
    const r = result.value
    if (!r) return
    // 地址与原因都由后端剥好凭据，复制面与展示面同源
    const head = r.ok
      ? `${r.addr}\t通\t${r.latencyMs}ms\t${r.egressIp || '-'}（${r.egressAgree}/${r.egressSources} 源一致，代表源 ${sourceHost(r.egressSource)}）`
      : `${r.addr}\t不通\t${r.latencyMs}ms\t${r.error || '未知原因'}`
    const lines = [head, ...(r.probes ?? []).map(p => `\t${sourceHost(p.source)}\t${p.ip || p.error || '-'}`)]
    await copyWithToast(lines.join('\n'), '已复制代理测试结果')
  }

  return { form, credOpen, optsOpen, timeoutMs, skipTls, result, error, checking, test, copyResult }
}
