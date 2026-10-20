// SSH 检查的领域状态与动作（第三个标签页，形制同 useProxyCheck）。
//
// 两个动作对应后端两个 RPC，**不合并**：
//   - test()      → CheckSSH：只读版本横幅，对端日志干净；
//   - testAuth()  → CheckSSHAuth：真的发起一次密码认证，会在对端 sshd 留下
//                   Failed password 记录、可能触发 fail2ban。所以它只能是显式按钮。
// 凭据只存在于这里的内存态：不落盘、不进结果字段、不回显。
import { ref, shallowRef } from 'vue'
import * as PortScanAPI from '../../../bindings/hanxi/internal/modules/portscan'
import type { SSHAuthResult, SSHCheckResult } from '../../../bindings/hanxi/internal/modules/portscan/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'
import { useClipboard } from '../../composables/useClipboard'

// 常见 SSH 端口，点一下填进端口栏。NAS/路由器映射口各家默认值不同，这里只标
// "哪类环境爱用这个口"，别拿它当判据。
export const SSH_PORT_PRESETS = [
  { port: 22, title: '标准 SSH 口' },
  { port: 2222, title: '常见备选口（无 root 权限时常用）' },
  { port: 60022, title: 'NAS / 内网映射口' },
]

// 结论 → 色彩与短标签。后端把 verdict 分得很细，UI 这边只负责把它翻译成人眼能
// 分档的颜色；措辞与枚举一一对应，不做二次判断。
export const SSH_AUTH_TONE: Record<string, 'positive' | 'warning' | 'danger'> = {
  'login-ok': 'positive',
  'wrong-credentials': 'warning',
  unreachable: 'danger',
  timeout: 'warning',
}

export const SSH_AUTH_LABEL: Record<string, string> = {
  'login-ok': '可登录',
  'wrong-credentials': '被拒绝',
  unreachable: '没连上',
  timeout: '超时',
}

export function useSSHCheck() {
  const form = ref({ host: '127.0.0.1', port: 22 })
  const cred = ref({ user: '', pass: '' })
  const credOpen = ref(false)
  const optsOpen = ref(false)
  const timeoutMs = ref(5000)

  const result = shallowRef<SSHCheckResult | null>(null)
  const auth = shallowRef<SSHAuthResult | null>(null)
  const error = ref('')
  // 两个动作共用一把单飞锁：同一时刻只该有一次对服务端的动作在跑，
  // 免得版本探测与登录尝试互相搅在一起看不清结果
  const { busy: checking, run } = useAsyncAction()
  const { copyWithToast } = useClipboard()

  async function test() {
    error.value = ''
    result.value = null
    const res = await run(() => PortScanAPI.PortScanService.CheckSSH({
      host: form.value.host.trim(),
      port: Number(form.value.port) || 0,
      timeoutMs: Number(timeoutMs.value) || 0,
    }))
    if (res.ok) {
      result.value = res.data ?? null
    } else {
      // 主机/端口不合法由后端一处判并如实给中文原因
      error.value = getErrorMessage(res.error)
    }
  }

  async function testAuth() {
    error.value = ''
    auth.value = null
    const res = await run(() => PortScanAPI.PortScanService.CheckSSHAuth({
      host: form.value.host.trim(),
      port: Number(form.value.port) || 0,
      user: cred.value.user.trim(),
      pass: cred.value.pass,
      timeoutMs: Number(timeoutMs.value) || 0,
    }))
    if (res.ok) {
      auth.value = res.data ?? null
    } else {
      error.value = getErrorMessage(res.error)
    }
  }

  async function copyResult() {
    const lines: string[] = []
    if (result.value) {
      const r = result.value
      lines.push(r.ok
        ? `${r.addr}\t可达\t${r.latencyMs}ms\t${r.serverVersion}`
        : `${r.addr}\t不可达\t${r.latencyMs}ms\t${r.error || '未知原因'}`)
    }
    if (auth.value) {
      const a = auth.value
      lines.push(`${a.addr}\t${SSH_AUTH_LABEL[a.verdict] ?? a.verdict}\t${a.latencyMs}ms\t` +
        `${a.canLogin ? '用户名密码可用' : a.reason || '未知原因'}\t指纹 ${a.hostKeyFingerprint || '未获取'}`)
    }
    if (!lines.length) return
    await copyWithToast(lines.join('\n'), '已复制 SSH 测试结果')
  }

  return {
    form, cred, credOpen, optsOpen, timeoutMs,
    result, auth, error, checking,
    test, testAuth, copyResult,
  }
}
