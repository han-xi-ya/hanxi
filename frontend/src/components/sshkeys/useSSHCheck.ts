// SSH 检查的领域状态与动作。
//
// 形制取自 portscan 的 useProxyCheck，但 2026-10-22 起随能力一起归到 sshkeys 模块，
// 刻意**不共用**那份状态：两者的凭据边界不同——代理测试的凭据只在本机出网路径上，
// 而这里的凭据会送进别人的 sshd（见下方 testAuth 的显式动作说明）。
//
// 两个动作对应后端两个 RPC，**不合并**，UI 侧也按两区块分区（SSHCheckPanel.vue）：
//   - test()      → CheckSSH：协议第一回合版本交换，不碰凭据、不在对端 sshd 日志
//                   留记录；结论止于"能不能连、是不是 SSH"，**不等于能登录**。
//   - testAuth()  → CheckSSHAuth：真的发起一次登录尝试，会在对端 sshd 留下
//                   Failed password 记录、可能触发 fail2ban。所以它只能是显式按钮。
//
// 口令纪律（三条，spec 逐条锁死，不是口头承诺）：
//   ① 输入框默认掩码（type=password），明文只随显式「显示」勾选出现；
//   ② 任一出口即清空：成功 / 失败（testAuth 的 finally，两分支同走）/ 主动清除
//      （清除口令按钮）/ 切页（KeepAlive 失活 onDeactivated、卸载 onScopeDispose）/
//      关窗（pagehide、卸载）——**不是在成功回调里顺手补救**；
//   ③ 不进复制全文、不进结果对象（后端本就没有该字段）、不进 localStorage、
//      不进 console。
// ②的一处已知盲区如实记下：SshKeysView 的页签切换用 v-show（有意保留主机/端口
// 与上次结论），不触发任何组件生命周期，面板内部感知不到"这一页被藏起来了"；
// 页内切走时若要清口令，须视图侧配合（已随本轮改动上报，非本文件权限）。
import { ref, shallowRef, getCurrentInstance, getCurrentScope, onScopeDispose, onDeactivated } from 'vue'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { HostEntry, SSHAuthResult, SSHCheckResult } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
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

// 结论 → 色彩与短标签。后端把 verdict（SSHAuthVerdict 枚举）分得很细，UI 这边只
// 负责把它翻译成人眼能分档的颜色；措辞与枚举一一对应，不做二次判断。
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
  // —— 主机清单预填源（「从主机清单选择」下拉）——
  // 面板定位是**自由探测仪**：测一个还没登记的地址是它的本职，清单只是省事入口，
  // 不能变成唯一门。所以这份数据只做三件事：挂一次 ListHosts、给下拉当选项、
  // 选中时预填 主机/端口/用户；两个测试动作永远发的是输入框现值，与本数据无关。
  // 挂载拉一次即可（清单是账本不是活体，每次测试再拉并无收益，也不许把测试
  // 动作吊在它的延迟后面）。读失败**静默降级**：不报错、不阻断手输——与
  // useHosts.refreshKeyOptions 同一口径。也不走 run 单飞锁：挂载取数与用户
  // 立刻点测试可能并发，抢锁会把正当动作报成「操作进行中」。
  const ledgerHosts = shallowRef<HostEntry[]>([])
  const hostsLoaded = ref(false)
  const hostsLoadFailed = ref(false)

  async function loadLedgerHosts() {
    try {
      const list = await SshKeysAPI.SshKeysService.ListHosts()
      ledgerHosts.value = list ?? []
      hostsLoadFailed.value = false
    } catch {
      // 静默：失败只让下拉多说一行占位，不冒任何错误横幅。
      ledgerHosts.value = []
      hostsLoadFailed.value = true
    } finally {
      hostsLoaded.value = true
    }
  }

  const form = ref({ host: '127.0.0.1', port: 22 })
  // cred.pass 的清空一律**原地改属性**（不是替换整个对象）——spec 拿着引用
  // 断言"出口之后这个对象上的口令没了"，替换写法会让断言对着过期旧对象空转。
  const cred = ref({ user: '', pass: '' })
  const showPass = ref(false)
  const optsOpen = ref(false)
  const timeoutMs = ref(5000)

  const result = shallowRef<SSHCheckResult | null>(null)
  const auth = shallowRef<SSHAuthResult | null>(null)
  // 错误通道按区块分家：入参被后端拒绝时，哪一侧出的错就摆在哪一侧，
  // 不留"横幅挂在别家卡片里"的错位。
  const connectError = ref('')
  const authError = ref('')
  // 两个动作共用一把单飞锁：同一时刻只该有一次对服务端的动作在跑，
  // 免得版本探测与登录尝试互相搅在一起看不清结果
  const { busy: checking, run } = useAsyncAction()
  // 在途跑的是哪个动作（两区块并排后，转圈提示得挂在自己那一侧）
  const busyKind = ref<'connect' | 'auth' | ''>('')
  const { copyWithToast } = useClipboard()

  function clearPass() {
    // 原地改，理由见 cred 声明处
    cred.value.pass = ''
    // 明文勾选一并收回：口令都没了，没理由让输入框停在可见态
    showPass.value = false
  }

  async function test() {
    connectError.value = ''
    result.value = null
    busyKind.value = 'connect'
    const res = await run(() => SshKeysAPI.SshKeysService.CheckSSH({
      host: form.value.host.trim(),
      port: Number(form.value.port) || 0,
      timeoutMs: Number(timeoutMs.value) || 0,
    }))
    busyKind.value = ''
    if (res.ok) {
      result.value = res.data ?? null
    } else {
      // 主机/端口不合法由后端一处判并如实给中文原因
      connectError.value = getErrorMessage(res.error)
    }
  }

  async function testAuth() {
    authError.value = ''
    auth.value = null
    busyKind.value = 'auth'
    try {
      const res = await run(() => SshKeysAPI.SshKeysService.CheckSSHAuth({
        host: form.value.host.trim(),
        port: Number(form.value.port) || 0,
        user: cred.value.user.trim(),
        pass: cred.value.pass,
        timeoutMs: Number(timeoutMs.value) || 0,
      }))
      if (res.ok) {
        auth.value = res.data ?? null
      } else {
        authError.value = getErrorMessage(res.error)
      }
    } finally {
      // 成功与失败都从这个 finally 出口清空，不是在成功回调里补救：
      // 只要发起过这一次登录尝试，口令在本页就没有继续存在的理由。
      clearPass()
    }
  }

  // 勾掉「显示」不清口令——用户可能勾看一眼又取消、随后才点试登录；
  // 口令的清空出口就是纪律②列明的那几个（finally / 清除口令 / 失活 / 卸载 /
  // pagehide），不多做也不少做。

  // 切页（KeepAlive 换页失活 / 组件卸载）与关窗（pagehide / 进程退出前的卸载）：
  // 口令即时清空。onScopeDispose 只覆盖卸载；顶层 KeepAlive 换页时组件不卸载，
  // 失活是另一条路，必须单独接。pagehide 覆盖关窗——虽然进程随之退出，
  // 但把"清空"做成显式钩子而不是指望进程陪葬，这条纪律才可测。
  // 钩子只在 setup 上下文注册（本 composable 的设计用法）；脱离实例/作用域时
  // 静默跳过，清空本身幂等，双挂不冲突。
  const pageHideClear = () => clearPass()
  if (getCurrentInstance()) {
    onDeactivated(clearPass)
  }
  if (getCurrentScope()) {
    window.addEventListener('pagehide', pageHideClear)
    onScopeDispose(() => {
      window.removeEventListener('pagehide', pageHideClear)
      clearPass()
    })
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
    // 组行只用结果对象里的字段——后端 SSHAuthResult 本就没有口令字段，这里也刻意
    // 不去碰 cred：复制全文永远不含口令（spec 锁）。
    await copyWithToast(lines.join('\n'), '已复制 SSH 测试结果')
  }

  return {
    form, cred, showPass, optsOpen, timeoutMs,
    result, auth, connectError, authError, checking, busyKind,
    ledgerHosts, hostsLoaded, hostsLoadFailed, loadLedgerHosts,
    test, testAuth, copyResult, clearPass,
  }
}
