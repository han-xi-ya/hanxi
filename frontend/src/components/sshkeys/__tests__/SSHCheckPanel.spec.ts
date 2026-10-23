// SSH 检查面板特征测试。2026-10-23 两动作分区改版后锁四件事：
// ① 分区与分级：「连接测试」「凭据核验」是两张卡（不是同卡并列两组按钮），
//    凭据卡整卡降档，且真实登录尝试的后果说明**常驻 DOM**——不依赖 hover、
//    不依赖展开、不进 tooltip/toast；
// ② 结论分层：CheckSSH 成功必带"不代表你能登录"边界标注；失败原文按后端结论
//    呈现并标注"未做归类"（outcome 枚举到位前**禁止按文本猜四态**）；
//    CheckSSHAuth 沿用 SSHAuthVerdict 标签（可登录/被拒绝/没连上/超时），
//    「被拒绝」必须两种可能（密码不对 / 不接受密码登录）并列在界面上；
// ③ 口令纪律可测化：字段默认 masked（历史注记"刻意明文——测试场景要能看见"
//    在跨机器凭据场景不成立已废，理由见组件内注释）；成功/失败/清除/切页/
//    关窗任一出口清空——清空走 finally、显式按钮与生命周期钩子，测试直接
//    持 cred 引用断言出口后的状态，证明不是成功回调里的补救；
// ④ 口令不进复制全文、localStorage、console；
// ⑤ 「从主机清单选择」只是省事入口：挂载拉一次 ListHosts，选中预填 主机/端口/用户
//    （label 备注优先、空备注退别名、密钥路径不进 label）；下拉一次性动作不留
//    选中态，手改地址后测试发的就是输入框现值；空清单/读失败都静默降级成禁用
//    占位项，两个测试动作永不受它影响（面板本职是自由探测仪）。
import { KeepAlive, defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SSHCheckPanel from '../SSHCheckPanel.vue'
import { useToast } from '../../../composables/useToast'

const svc = vi.hoisted(() => ({
  CheckSSH: vi.fn(),
  CheckSSHAuth: vi.fn(),
  // 面板挂载时为「从主机清单选择」下拉拉一次 ListHosts（读失败/空都静默降级，
  // 不占用例里其他断言）；用它的用例自己 mockResolvedValue / mockRejectedValue。
  ListHosts: vi.fn(),
}))

// 复制出口换成记录器：口令"不进复制全文"这条只能这样断言（clipboard 在
// happy-dom 里不可靠，走 execCommand 兜底会静默吞掉断言目标）。
const copies = vi.hoisted(() => [] as string[])

vi.mock('../../../../bindings/hanxi/internal/modules/sshkeys', () => ({
  // 键名必须与 bindings 的导出命名空间逐字一致（现为 SshKeysService，随 Go 侧
  // 服务结构体名走）。搬家时这里曾残留旧名 PortScanService——mock 不报错、只是
  // 永远命中不到真实现，测试照样"绿"，是假绿的典型形态。
  SshKeysService: svc,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

vi.mock('../../../composables/useClipboard', () => ({
  useClipboard: () => ({
    copy: vi.fn(async () => true),
    paste: vi.fn(async () => null),
    copyWithToast: async (text: string) => { copies.push(text); return true },
  }),
}))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) {
    await Promise.resolve()
  }
  await new Promise(r => setTimeout(r, 0))
}

// 面板在 script setup 里直接调 useSSHCheck()，dev 模式下 wrapper.vm 经
// setupState 代理把 ref 解包给测试——拿着这个引用就能在出口动作之后断言
// 口令字段本身（而不是只看 DOM 残影）。前提：composable 的清空是**原地改
// 属性**，不替换对象，useSSHCheck.ts 里有注记锁这一口径。
function credOf(w: ReturnType<typeof mount>) {
  return (w.vm as unknown as { cred: { user: string; pass: string } }).cred
}
function passInput(w: ReturnType<typeof mount>) {
  return w.find('.ssh-auth-block .ssh-pass')
}
function passValue(w: ReturnType<typeof mount>) {
  return (passInput(w).element as HTMLInputElement).value
}

const checkResult = {
  addr: '127.0.0.1:22',
  ok: true,
  serverVersion: 'SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu1',
  protocol: '2.0',
  software: 'OpenSSH_9.6p1',
  latencyMs: 41,
}

const authOK = {
  addr: '127.0.0.1:22',
  verdict: 'login-ok',
  canLogin: true,
  reason: '用户名密码可用',
  hostKeyType: 'ssh-ed25519',
  hostKeyFingerprint: 'SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
  latencyMs: 88,
}

// 后端 wrong-credentials 的 reason 原文形态（classifyAuthError 逐字如此拼）：
// "密码不对"与"不接受密码登录"两句并列——界面不许只留前半句。
const rejectedReason = '这组凭据被拒绝。也可能是该服务器不接受密码登录（例如只允许密钥）——' +
  'x/crypto/ssh 未导出可区分两者的信息：ssh: unable to authenticate'

async function mountWithCreds(user = 'zifeiyu', pass = 'p@ss:word') {
  const w = mount(SSHCheckPanel)
  await flushMicrotasks()
  const inputs = w.findAll('.ssh-auth-block .check-cred-input')
  await inputs[0].setValue(user)
  await inputs[1].setValue(pass)
  return { w, runBtn: w.find('.ssh-auth-block .btn-secondary') }
}

describe('SSHCheckPanel', () => {
  afterEach(() => {
    vi.clearAllMocks()
    copies.length = 0
    useToast().clearToast()
  })

  describe('分区与分级', () => {
    it('两区块是两张卡：「连接测试」与「凭据核验」并存，输入各归各卡', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const cards = w.findAll('section.section-card')
      expect(cards).toHaveLength(2)
      expect(w.find('.ssh-connect-block h2').text()).toBe('连接测试')
      expect(w.find('.ssh-auth-block h2').text()).toBe('凭据核验')
      // 主机/端口在连接测试卡，凭据输入在核验卡——同一张卡里并列两组按钮正是被改掉的问题
      expect(w.find('.ssh-connect-block .check-host').exists()).toBe(true)
      expect(w.find('.ssh-connect-block .ssh-pass').exists()).toBe(false)
      expect(passInput(w).exists()).toBe(true)
      w.unmount()
    })

    it('后果说明常驻凭据卡，不靠 hover/展开/tooltip 才能发现', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const notice = w.find('.ssh-auth-block .auth-notice')
      expect(notice.exists()).toBe(true)
      expect(notice.text()).toContain('真实登录尝试')
      expect(notice.text()).toContain('sshd 日志')
      expect(notice.text()).toContain('fail2ban')
      // 常驻=开箱即在 DOM 且有文字；同时禁止"后果只挂在 title 里"的形态
      //（端口胶囊的 title 是环境注释，不在此列，故只查"承载后果的构件不许是 title"）
      const titleCarriers = w.findAll('[title]').filter(el => /fail2ban|sshd 日志/.test(el.attributes('title') ?? ''))
      expect(titleCarriers).toHaveLength(0)
      w.unmount()
    })

    it('钮档纪律：两区块主操作钮同档同高、次级钮同档同高', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const connectPrimary = w.find('.ssh-connect-block .btn-secondary')
      const authPrimary = w.find('.ssh-auth-block .btn-secondary')
      expect(connectPrimary.exists()).toBe(true)
      expect(authPrimary.exists()).toBe(true)
      // 主钮都吃 .btn 基座标准高（--control-h-lg），谁都不许偷挂 btn-small
      expect(connectPrimary.classes()).not.toContain('btn-small')
      expect(authPrimary.classes()).not.toContain('btn-small')
      // 次级钮统一 ghost+small 档
      expect(w.find('.ssh-connect-block .btn-ghost.btn-small').exists()).toBe(true)
      expect(w.find('.ssh-auth-block .btn-ghost.btn-small').exists()).toBe(true)
      w.unmount()
    })
  })

  describe('连接测试（CheckSSH：不碰凭据）', () => {
    it('开箱即测：默认 127.0.0.1:22，点「连接测试」按结构化字段发出，绝不顺发凭据', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      expect(w.find('.ssh-connect-block .empty-hint').exists()).toBe(true)
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSH).toHaveBeenCalledWith({ host: '127.0.0.1', port: 22, timeoutMs: 5000 })
      // 只点连接测试就顺手发凭据的话，等于偷偷往对端日志里写记录
      expect(svc.CheckSSHAuth).not.toHaveBeenCalled()
      w.unmount()
    })

    it('常用端口胶囊只改端口；超时展开改预算', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.findAll('.ssh-connect-block .btn-quick')[1].trigger('click') // 2222
      await w.find('.ssh-connect-block .btn-ghost.btn-small').trigger('click') // 超时展开
      await flushMicrotasks()
      await w.find('.ssh-connect-block .check-num').setValue(8000)
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSH.mock.calls[0][0]).toMatchObject({ port: 2222, timeoutMs: 8000 })
      w.unmount()
    })

    it('成功：结论带"不代表你能登录"边界标注，且本卡不出现「可登录」字样', async () => {
      svc.CheckSSH.mockResolvedValue(checkResult)
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      const verdict = w.find('.ssh-connect-block .check-verdict')
      expect(verdict.find('.chip-positive').text()).toBe('可达')
      expect(verdict.find('.check-server').text()).toContain('OpenSSH_9.6')
      expect(verdict.text()).toContain('这只说明对方是 SSH 服务，不代表你能登录')
      expect(verdict.text()).not.toContain('可登录')
      w.unmount()
    })

    it('失败：原文照搬 + 标注"未做归类"，不拿文本猜四态', async () => {
      const raw = { addr: '127.0.0.1:22', ok: false, latencyMs: 60,
        error: '对端没按 SSH 协议回话（10 秒内）：read: operation timed out' }
      svc.CheckSSH.mockResolvedValue(raw)
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      const verdict = w.find('.ssh-connect-block .check-verdict')
      expect(verdict.find('.chip-danger').text()).toBe('不可达')
      // 原文逐字呈现（后端补 outcome 枚举前，UI 一个字的归类都不做）
      expect(verdict.find('.check-reason').text()).toBe(raw.error)
      expect(verdict.find('.verdict-scope').text()).toContain('按后端原文呈现')
      expect(verdict.find('.verdict-scope').text()).toContain('未做归类')
      w.unmount()
    })

    it('后端拒绝入参：错误横幅挂在连接测试卡，不串到凭据卡', async () => {
      svc.CheckSSH.mockRejectedValue(new Error('请填写主机地址（IP 或域名）'))
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('.ssh-connect-block .check-host').setValue('   ')
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      const banner = w.find('.ssh-connect-block .banner.banner-error')
      expect(banner.exists()).toBe(true)
      expect(banner.text()).toContain('请填写主机地址')
      expect(w.find('.ssh-auth-block .banner-error').exists()).toBe(false)
      w.unmount()
    })

    it('在途：本卡按钮 disabled、转圈挂本卡，重复点击被单飞守卫拦住', async () => {
      let resolveFn: (value: unknown) => void = () => {}
      svc.CheckSSH.mockImplementation(() => new Promise(resolve => { resolveFn = resolve }))
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks(1)
      expect((w.find('.ssh-connect-block .btn-secondary').element as HTMLButtonElement).disabled).toBe(true)
      expect(w.find('.ssh-connect-block .state-box').text()).toContain('正在与对端交换')
      // 凭据卡此刻不该冒出"进行中"的错位提示
      expect(w.find('.ssh-auth-block .state-box').exists()).toBe(false)
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      expect(svc.CheckSSH).toHaveBeenCalledTimes(1)
      resolveFn(checkResult)
      await flushMicrotasks()
      expect(w.find('.ssh-connect-block .check-server').text()).toContain('OpenSSH_9.6')
      w.unmount()
    })
  })

  describe('凭据核验（CheckSSHAuth：真实登录尝试）与口令纪律', () => {
    it('口令字段默认 masked，「显示」勾选是唯一明文开关', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      expect(passInput(w).attributes('type')).toBe('password')
      const toggle = w.find('.ssh-auth-block input[type="checkbox"]')
      expect((toggle.element as HTMLInputElement).checked).toBe(false)
      await toggle.setValue(true)
      expect(passInput(w).attributes('type')).toBe('text')
      await toggle.setValue(false)
      expect(passInput(w).attributes('type')).toBe('password')
      w.unmount()
    })

    it('凭据不全时「试登录」禁用；配齐清除钮才可用', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const run = w.find('.ssh-auth-block .btn-secondary')
      expect((run.element as HTMLButtonElement).disabled).toBe(true)
      const inputs = w.findAll('.ssh-auth-block .check-cred-input')
      await inputs[0].setValue('u')
      expect((run.element as HTMLButtonElement).disabled).toBe(true) // 缺口令
      await inputs[1].setValue('p')
      expect((run.element as HTMLButtonElement).disabled).toBe(false)
      expect((w.find('.ssh-auth-block .btn-ghost.btn-small').element as HTMLButtonElement).disabled).toBe(false)
      w.unmount()
    })

    it('发出：口令只在这一次请求参数里；成功出口即清空，页面不再有其踪影', async () => {
      svc.CheckSSHAuth.mockResolvedValue(authOK)
      const { w, runBtn } = await mountWithCreds()
      const cred = credOf(w)
      await runBtn.trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSHAuth).toHaveBeenCalledWith({
        host: '127.0.0.1', port: 22, user: 'zifeiyu', pass: 'p@ss:word', timeoutMs: 5000,
      })
      // 成功出口的清空由 testAuth 的 finally 完成（下一条锁失败出口，证明不是回调补救）
      expect(cred.pass).toBe('')
      expect(passValue(w)).toBe('')
      // 明文勾选也被收回：框回到 masked，勾回到未选
      expect(passInput(w).attributes('type')).toBe('password')
      expect((w.find('.ssh-auth-block input[type="checkbox"]').element as HTMLInputElement).checked).toBe(false)
      const verdict = w.find('.ssh-auth-block .check-verdict')
      expect(verdict.find('.chip-positive').text()).toBe('可登录')
      expect(verdict.text()).toContain('SHA256:AAAA')
      expect(verdict.text()).toContain('ssh-ed25519')
      expect(w.text()).not.toContain('p@ss:word')
      w.unmount()
    })

    it('失败出口同样清空：RPC 异常时横幅在凭据卡，口令不留存', async () => {
      svc.CheckSSHAuth.mockRejectedValue(new Error('请填写密码（本能力只测密码认证）'))
      const { w, runBtn } = await mountWithCreds()
      const cred = credOf(w)
      await runBtn.trigger('click')
      await flushMicrotasks()
      const banner = w.find('.ssh-auth-block .banner.banner-error')
      expect(banner.text()).toContain('请填写密码')
      // 横幅是口令最可能"顺嘴"漏出去的出口——错误文本原样上屏，锁它不含口令
      expect(banner.text()).not.toContain('p@ss:word')
      expect(cred.pass).toBe('')
      expect(passValue(w)).toBe('')
      w.unmount()
    })

    it('取消出口：「清除口令」即时清空并收回明文态', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('.ssh-auth-block input[type="checkbox"]').setValue(true)
      const inputs = w.findAll('.ssh-auth-block .check-cred-input')
      await inputs[1].setValue('p@ss:word')
      await w.find('.ssh-auth-block .btn-ghost.btn-small').trigger('click') // 清除口令
      await flushMicrotasks()
      expect(credOf(w).pass).toBe('')
      expect(passInput(w).attributes('type')).toBe('password')
      w.unmount()
    })

    it('切页出口：顶层 KeepAlive 换页（组件失活不卸载）口令即清空', async () => {
      const Host = defineComponent({
        components: { SSHCheckPanel, KeepAlive },
        template: '<KeepAlive><SSHCheckPanel v-if="on" /></KeepAlive>',
        data: () => ({ on: true }),
      })
      const h = mount(Host)
      await flushMicrotasks()
      const child = h.findComponent(SSHCheckPanel)
      const cred = (child.vm as unknown as { cred: { pass: string } }).cred
      await child.find('.ssh-auth-block .check-cred-input').setValue('u')
      await child.findAll('.ssh-auth-block .check-cred-input')[1].setValue('p@ss:word')
      ;(h.vm as unknown as { on: boolean }).on = false // 换页：KeepAlive 下是失活，不是卸载
      await flushMicrotasks()
      expect(cred.pass).toBe('')
      // 反证"这里发生的是失活而非卸载"：切回来若实例是重建的，用户名字段会被
      // 清空；缓存复用则口令之外的字段原样。没有这一步，本测试会因错误的
      // 生命周期路径（onScopeDispose）同样变绿，锁不住 onDeactivated 这条腿。
      ;(h.vm as unknown as { on: boolean }).on = true
      await flushMicrotasks()
      const back = h.findComponent(SSHCheckPanel)
      expect((back.find('.ssh-auth-block .check-cred-input').element as HTMLInputElement).value).toBe('u')
      expect((back.findAll('.ssh-auth-block .check-cred-input')[1].element as HTMLInputElement).value).toBe('')
      h.unmount()
    })

    it('关窗/卸载出口：pagehide 与组件卸载都清空', async () => {
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const cred = credOf(w)
      await w.findAll('.ssh-auth-block .check-cred-input')[1].setValue('p@ss:word')
      window.dispatchEvent(new Event('pagehide')) // 关窗（Wails 窗口关闭前事件）
      expect(cred.pass).toBe('')
      await w.findAll('.ssh-auth-block .check-cred-input')[1].setValue('p@ss:word')
      w.unmount() // 卸载/驱逐（onScopeDispose 兜底）
      expect(cred.pass).toBe('')
    })

    it('被拒绝：chip「被拒绝」，reason 全文与两种可能并列在界面上，指纹不丢', async () => {
      svc.CheckSSHAuth.mockResolvedValue({
        addr: '127.0.0.1:22',
        verdict: 'wrong-credentials',
        canLogin: false,
        reason: rejectedReason,
        hostKeyType: 'ssh-ed25519',
        hostKeyFingerprint: 'SHA256:BBBB',
        latencyMs: 70,
      })
      const { w, runBtn } = await mountWithCreds('u', 'p')
      await runBtn.trigger('click')
      await flushMicrotasks()
      const verdict = w.find('.ssh-auth-block .check-verdict')
      expect(verdict.find('.chip-warning').text()).toBe('被拒绝')
      // 后端原文两句并列——界面只截前半句就是骗人
      expect(verdict.find('.check-reason').text()).toContain('这组凭据被拒绝')
      expect(verdict.find('.check-reason').text()).toContain('不接受密码登录')
      // 兜底标注同样把两种可能摆全（reason 缺失/被截也不断档）
      const scope = verdict.find('.verdict-scope').text()
      expect(scope).toContain('密码不对')
      expect(scope).toContain('不接受密码登录')
      // 认证失败也必须保留指纹：它是"这台机器是不是它"的证据
      expect(verdict.text()).toContain('SHA256:BBBB')
      w.unmount()
    })

    it('没连上/超时：沿用 SSHAuthVerdict 标签与色档，不另造词', async () => {
      for (const [verdict, tone, label] of [
        ['unreachable', 'chip-danger', '没连上'],
        ['timeout', 'chip-warning', '超时'],
      ] as const) {
        svc.CheckSSHAuth.mockResolvedValue({ addr: '127.0.0.1:22', verdict, canLogin: false, reason: 'r', latencyMs: 9 })
        const { w, runBtn } = await mountWithCreds('u', 'p')
        await runBtn.trigger('click')
        await flushMicrotasks()
        expect(w.find(`.ssh-auth-block .check-verdict .${tone}`).text()).toBe(label)
        w.unmount()
      }
    })

    it('出口全景：复制全文/结果对象/localStorage/console 都容不下那枚口令', async () => {
      const consoleSpies = [
        vi.spyOn(console, 'log').mockImplementation(() => {}),
        vi.spyOn(console, 'warn').mockImplementation(() => {}),
        vi.spyOn(console, 'error').mockImplementation(() => {}),
      ]
      const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {})
      svc.CheckSSH.mockResolvedValue(checkResult)
      svc.CheckSSHAuth.mockResolvedValue(authOK)
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      const runBtn = w.find('.ssh-auth-block .btn-secondary')
      const inputs = w.findAll('.ssh-auth-block .check-cred-input')
      await inputs[0].setValue('zifeiyu')
      await inputs[1].setValue('p@ss:word')
      await runBtn.trigger('click')
      await flushMicrotasks()
      // 结果对象层面：auth 来自后端，本无口令字段；前端侧也不许把 cred 挂上去
      const auth = (w.vm as unknown as { auth: Record<string, unknown> | null }).auth
      expect(JSON.stringify(auth)).not.toContain('p@ss:word')
      await w.find('.ssh-connect-block .card-header .btn-ghost.btn-small').trigger('click') // 复制结果
      await flushMicrotasks()
      expect(copies).toHaveLength(1)
      expect(copies[0]).toContain('SSH-2.0-OpenSSH')
      expect(copies[0]).toContain('可登录')
      expect(copies[0]).not.toContain('p@ss:word')
      expect(setItem).not.toHaveBeenCalled()
      for (const spy of consoleSpies) {
        for (const call of spy.mock.calls) {
          expect(call.map(String).join(' ')).not.toContain('p@ss:word')
        }
      }
      w.unmount()
      vi.restoreAllMocks()
    })
  })

  // —— 「从主机清单选择」= 省事入口，不是唯一门 ——
  // 面板定位是自由探测仪：测一个还没登记的地址是它的本职。清单只负责挂一次
  // ListHosts、给下拉当选项、选中时预填三字段；读失败/空清单都静默降级成
  // 一行禁用占位，两个测试动作永不受它影响。放在文件最后：afterEach 用
  // clearAllMocks（不清实现），这里的 ListHosts.mockRejectedValue 不许泄漏给别的用例。
  describe('从主机清单选择（预填三字段；空/失败静默降级；不劫持手输）', () => {
    const LEDGER_ROW = {
      alias: '8-155-34-133', host: '8.155.34.133', port: 0, user: 'ubuntu',
      keyPath: 'C:\\hidden\\id_ed25519', note: '开发机', enabled: true,
    }
    const LEDGER_ROW2 = {
      alias: 'nas01', host: '192.168.1.10', port: 2222, user: '',
      keyPath: '', note: '', enabled: true,
    }

    it('挂载拉一次清单；label 备注优先（开发机（8-155-34-133））、空备注退回别名、密钥路径不进 label', async () => {
      svc.ListHosts.mockResolvedValue([LEDGER_ROW, LEDGER_ROW2])
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      expect(svc.ListHosts).toHaveBeenCalledTimes(1)
      const sel = w.find('#ssh-host-pick')
      expect(sel.text()).toContain('开发机（8-155-34-133）')
      const nasOpt = sel.findAll('option').find(o => (o.element as HTMLOptionElement).value === 'nas01')
      expect(nasOpt).toBeDefined()
      expect(nasOpt!.text()).toBe('nas01')
      expect(sel.text()).not.toContain('C:\\hidden') // 这是选择器，不是详情表
      w.unmount()
    })

    it('选中即预填 主机/端口/用户（端口 0 显式写成 22）；选本身不发任何测试动作；下拉即刻回落占位不留选中态', async () => {
      svc.ListHosts.mockResolvedValue([LEDGER_ROW, LEDGER_ROW2])
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const sel = w.find('#ssh-host-pick')
      await sel.setValue('8-155-34-133')
      await flushMicrotasks()
      expect((w.find('.ssh-connect-block .check-host').element as HTMLInputElement).value).toBe('8.155.34.133')
      expect((w.find('.ssh-connect-block .check-port').element as HTMLInputElement).value).toBe('22')
      expect((w.findAll('.ssh-auth-block .check-cred-input')[0].element as HTMLInputElement).value).toBe('ubuntu')
      // 选中只是预填，不是发射：两个动作都不许被顺带点着
      expect(svc.CheckSSH).not.toHaveBeenCalled()
      expect(svc.CheckSSHAuth).not.toHaveBeenCalled()
      // 不留"选中项"残留状态——下拉与输入框从此各管各的
      expect((sel.element as HTMLSelectElement).value).toBe('')
      w.unmount()
    })

    it('选过清单行之后再手改地址：输入框现值照常生效，不被残留状态劫持；清单仍只在挂载拉过一次', async () => {
      svc.ListHosts.mockResolvedValue([LEDGER_ROW])
      svc.CheckSSH.mockResolvedValue(checkResult)
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      await w.find('#ssh-host-pick').setValue('8-155-34-133')
      await w.find('.ssh-connect-block .check-host').setValue('manual.example')
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSH).toHaveBeenCalledWith({ host: 'manual.example', port: 22, timeoutMs: 5000 })
      expect(svc.ListHosts).toHaveBeenCalledTimes(1)
      w.unmount()
    })

    it('清单为空：禁用占位项「主机清单还没有记录，可直接输入地址」，不是空下拉；手输测试照常', async () => {
      svc.ListHosts.mockResolvedValue([])
      svc.CheckSSH.mockResolvedValue(checkResult)
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const sel = w.find('#ssh-host-pick')
      const ph = sel.findAll('option').find(o => o.text().includes('主机清单还没有记录'))
      expect(ph).toBeDefined()
      expect(ph!.attributes('disabled')).toBeDefined()
      await w.find('.ssh-connect-block .check-host').setValue('9.9.9.9')
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSH).toHaveBeenCalledWith({ host: '9.9.9.9', port: 22, timeoutMs: 5000 })
      w.unmount()
    })

    it('清单读取失败：静默降级成一行禁用占位（错误原文不上屏），两个测试动作照常可用', async () => {
      svc.ListHosts.mockRejectedValue(new Error('清单文件已损坏，拒绝以空账本覆盖'))
      svc.CheckSSH.mockResolvedValue(checkResult)
      svc.CheckSSHAuth.mockResolvedValue(authOK)
      const w = mount(SSHCheckPanel)
      await flushMicrotasks()
      const sel = w.find('#ssh-host-pick')
      const ph = sel.findAll('option').find(o => o.text().includes('清单读取失败'))
      expect(ph).toBeDefined()
      expect(ph!.attributes('disabled')).toBeDefined()
      // 静默 = 不把错误原文抬上屏、不冒横幅——读清单失败不许惊动探测仪本体
      expect(w.text()).not.toContain('清单文件已损坏')
      expect(w.findAll('.banner-error')).toHaveLength(0)
      await w.find('.ssh-connect-block .check-host').setValue('8.8.8.8')
      await w.find('.ssh-connect-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSH).toHaveBeenCalledWith({ host: '8.8.8.8', port: 22, timeoutMs: 5000 })
      const inputs = w.findAll('.ssh-auth-block .check-cred-input')
      await inputs[0].setValue('u')
      await inputs[1].setValue('p')
      await w.find('.ssh-auth-block .btn-secondary').trigger('click')
      await flushMicrotasks()
      expect(svc.CheckSSHAuth).toHaveBeenCalledWith({
        host: '8.8.8.8', port: 22, user: 'u', pass: 'p', timeoutMs: 5000,
      })
      w.unmount()
    })
  })
})
