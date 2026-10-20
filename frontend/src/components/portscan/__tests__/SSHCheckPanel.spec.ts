// SSH 检查面板的特征测试。锁三件事：① 连接测试与凭据测试是两个动作，凭据区默认收起；
// ② 展开后密码只进这一次请求，不回流到任何展示面；③ 结论翻译成人话的映射不错位。
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SSHCheckPanel from '../SSHCheckPanel.vue'
import { useToast } from '../../../composables/useToast'

const svc = vi.hoisted(() => ({
  CheckSSH: vi.fn(),
  CheckSSHAuth: vi.fn(),
}))

vi.mock('../../../../bindings/hanxi/internal/modules/portscan', () => ({
  PortScanService: svc,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) {
    await Promise.resolve()
  }
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

describe('SSHCheckPanel', () => {
  afterEach(() => {
    vi.clearAllMocks()
    useToast().clearToast()
  })

  it('开箱即测：默认已填 127.0.0.1:22，点「连接测试」按结构化字段发出', async () => {
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    expect(svc.CheckSSH).toHaveBeenCalledWith({ host: '127.0.0.1', port: 22, timeoutMs: 5000 })
    // 只点连接测试就顺手发凭据的话，等于偷偷往对端日志里写记录
    expect(svc.CheckSSHAuth).not.toHaveBeenCalled()
    w.unmount()
  })

  it('常用端口胶囊只改端口', async () => {
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.findAll('.btn-quick')[1].trigger('click') // 2222
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    expect(svc.CheckSSH.mock.calls[0][0].port).toBe(2222)
    w.unmount()
  })

  it('凭据区默认收起：展开前页面上没有密码输入', async () => {
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    // 按类名计数，不按 type="password"：本工具刻意把密码框做成明文（测试场景要能
    // 核对粘贴来的字符），所以"遮挡类型"不再是可断言的特征。
    expect(w.findAll('.check-cred-input')).toHaveLength(0)
    const credBtn = w.findAll('.check-advanced button')[0]
    expect(credBtn.text()).toContain('测试账号密码')
    await credBtn.trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.check-cred-input')).toHaveLength(2)
    // 副作用必须写在明面上，不能让人在不知情下往别人日志里写失败记录
    expect(w.find('.check-cred').text()).toContain('fail2ban')
    w.unmount()
  })

  it('凭据不全时「试登录」禁用；齐了才发出，且密码只在这一次请求里', async () => {
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.findAll('.check-advanced button')[0].trigger('click')
    await flushMicrotasks()
    const run = w.find('.check-cred .btn-secondary')
    expect((run.element as HTMLButtonElement).disabled).toBe(true)

    const inputs = w.findAll('.check-cred-input')
    await inputs[0].setValue('zifeiyu')
    await inputs[1].setValue('p@ss:word')
    expect((run.element as HTMLButtonElement).disabled).toBe(false)

    await run.trigger('click')
    await flushMicrotasks()
    expect(svc.CheckSSHAuth).toHaveBeenCalledWith({
      host: '127.0.0.1', port: 22, user: 'zifeiyu', pass: 'p@ss:word', timeoutMs: 5000,
    })
    // 结果区渲染的是后端给的剥密文本，页面任何角落都不该再出现那次输入的密码
    expect(w.text()).not.toContain('p@ss:word')
    w.unmount()
  })

  it('可登录：chip positive + 主机密钥指纹上屏', async () => {
    svc.CheckSSHAuth.mockResolvedValue(authOK)
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.findAll('.check-advanced button')[0].trigger('click')
    const inputs = w.findAll('.check-cred-input')
    await inputs[0].setValue('u')
    await inputs[1].setValue('p')
    await w.find('.check-cred .btn-secondary').trigger('click')
    await flushMicrotasks()

    const cards = w.findAll('.check-verdict')
    const authCard = cards[cards.length - 1]
    expect(authCard.find('.chip-positive').text()).toBe('可登录')
    expect(authCard.text()).toContain('SHA256:AAAA')
    expect(authCard.text()).toContain('ssh-ed25519')
    w.unmount()
  })

  it('被拒绝：chip warning，并把「也可能不接受密码登录」这句一并显示', async () => {
    svc.CheckSSHAuth.mockResolvedValue({
      addr: '127.0.0.1:22',
      verdict: 'wrong-credentials',
      canLogin: false,
      reason: '这组凭据被拒绝。也可能是该服务器不接受密码登录（例如只允许密钥）——详见: ssh: unable to authenticate',
      hostKeyType: 'ssh-ed25519',
      hostKeyFingerprint: 'SHA256:BBBB',
      latencyMs: 70,
    })
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.findAll('.check-advanced button')[0].trigger('click')
    const inputs = w.findAll('.check-cred-input')
    await inputs[0].setValue('u')
    await inputs[1].setValue('p')
    await w.find('.check-cred .btn-secondary').trigger('click')
    await flushMicrotasks()

    const cards = w.findAll('.check-verdict')
    const authCard = cards[cards.length - 1]
    expect(authCard.find('.chip-warning').text()).toBe('被拒绝')
    expect(authCard.text()).toContain('不接受密码登录')
    // 认证失败也必须保留指纹：它是"这台机器是不是它"的证据
    expect(authCard.text()).toContain('SHA256:BBBB')
    w.unmount()
  })

  it('后端拒绝入参：横幅如实呈现，不静默', async () => {
    svc.CheckSSH.mockRejectedValue(new Error('请填写主机地址（IP 或域名）'))
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.find('.check-host').setValue('   ')
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    const banner = w.find('.banner.banner-error')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('请填写主机地址')
    w.unmount()
  })

  it('在途：按钮 disabled，重复点击被单飞守卫拦住', async () => {
    let resolveFn: (value: unknown) => void = () => {}
    svc.CheckSSH.mockImplementation(() => new Promise(resolve => { resolveFn = resolve }))
    const w = mount(SSHCheckPanel)
    await flushMicrotasks()
    await w.find('.check-row .btn-secondary').trigger('click')
    await new Promise(r => setTimeout(r, 0))
    expect((w.find('.check-row .btn-secondary').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.find('.state-box').text()).toContain('正在与对端交换')
    await w.find('.check-row .btn-secondary').trigger('click')
    expect(svc.CheckSSH).toHaveBeenCalledTimes(1)
    resolveFn(checkResult)
    await flushMicrotasks()
    expect(w.find('.check-server').text()).toContain('OpenSSH_9.6')
    expect(w.text()).toContain('能不能登录点上面')
    w.unmount()
  })
})
