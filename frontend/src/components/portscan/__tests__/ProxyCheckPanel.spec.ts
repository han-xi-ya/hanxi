// ProxyCheckPanel（代理连通性测试标签页）的特征测试。原先六条挂在 PortScanView.spec 下，
// 面板抽成子组件后随 DOM 一起搬来——视图那边只留标签编排的断言。
// 后端是同步单条接口：断言集中在"发出什么入参"与"结果怎么呈现"，不涉及事件与取消。
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProxyCheckPanel from '../ProxyCheckPanel.vue'
import { useToast } from '../../../composables/useToast'

const svc = vi.hoisted(() => ({
  CheckProxy: vi.fn(),
}))

vi.mock('../../../../bindings/hanxi/internal/modules/portscan', () => ({
  PortScanService: svc,
}))

// bindings 模块加载期会引 runtime，面板本身不订阅事件，桩掉即可
vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) {
    await Promise.resolve()
  }
}

// 与后端 pickEgress 同构的一份实况：4 源里 ipify 失败、两源一致报 1.2.3.4、
// 一源少数派报 5.6.7.8 → 结论取众数，agree/sources = 2/3。
const okResult = {
  addr: 'socks5://127.0.0.1:7890',
  ok: true,
  egressIp: '1.2.3.4',
  egressSource: 'https://ifconfig.me/ip',
  egressAgree: 2,
  egressSources: 3,
  latencyMs: 120,
  probes: [
    { source: 'https://api.ipify.org', error: 'socks connect: host unreachable', latencyMs: 90 },
    { source: 'https://ifconfig.me/ip', ip: '1.2.3.4', latencyMs: 110 },
    { source: 'https://ip.3322.net', ip: '1.2.3.4', latencyMs: 120 },
    { source: 'https://ddns.oray.com/checkip', ip: '5.6.7.8', latencyMs: 130 },
  ],
}

describe('ProxyCheckPanel', () => {
  afterEach(() => {
    vi.clearAllMocks()
    useToast().clearToast()
  })

  it('开箱即测：默认已填本机常见口，点「测试」按结构化字段发出', async () => {
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    // 这条锁的是这一轮改造的全部理由：不手写 URL、前端也不先行校验一遍
    expect(svc.CheckProxy).toHaveBeenCalledWith({
      kind: 'socks5', host: '127.0.0.1', port: 7890, user: '', pass: '',
      timeoutMs: 5000, skipTlsVerify: false,
    })
    w.unmount()
  })

  it('常用端口胶囊只改端口，不改协议', async () => {
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    await w.findAll('.btn-quick')[3].trigger('click') // 1080
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    const req = svc.CheckProxy.mock.calls[0][0]
    expect(req.port).toBe(1080)
    expect(req.kind).toBe('socks5')
    w.unmount()
  })

  it('低频项默认收起：账号密码与超时/证书不占主路径', async () => {
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    expect(w.findAll('.check-cred-input')).toHaveLength(0)
    expect(w.find('input[type="checkbox"]').exists()).toBe(false)
    await w.findAll('.check-advanced button')[0].trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.check-cred-input')).toHaveLength(2)
    // 填了账号就把状态带出按钮文案，收起时也不让人忘了它存在
    await w.findAll('.check-cred-input')[0].setValue('zifeiyu')
    await w.findAll('.check-advanced button')[1].trigger('click')
    await w.findAll('.check-advanced button')[0].trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.check-advanced button')[0].text()).toContain('zifeiyu')
    w.unmount()
  })

  it('校验只在后端一处：地址留空照样发出，由后端拒绝并进横幅', async () => {
    svc.CheckProxy.mockRejectedValue(new Error('请填写代理地址（IP 或主机名）'))
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    await w.find('.check-host').setValue('   ')
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    // 前端不复制一份校验（两处迟早漂移），空值也交给后端判
    expect(svc.CheckProxy).toHaveBeenCalledTimes(1)
    expect(svc.CheckProxy.mock.calls[0][0].host).toBe('')
    const banner = w.find('.banner.banner-error')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('请填写代理地址')
    w.unmount()
  })

  it('判通：结论 positive，逐源 IP 分别成行，少数派标「与结论不同」', async () => {
    svc.CheckProxy.mockResolvedValue(okResult)
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()

    const verdict = w.find('.check-verdict')
    expect(verdict.find('.chip-positive').text()).toBe('通')
    expect(verdict.text()).toContain('1.2.3.4')
    // 一致度必须可见：2/3 与 3/3 的可信度不是一回事
    expect(verdict.text()).toContain('2/3 源一致')
    expect(verdict.text()).toContain('ifconfig.me')

    const rows = w.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(4)
    expect(rows[0].text()).toContain('api.ipify.org')
    expect(rows[0].text()).toContain('host unreachable')
    // 与众数一致 / 与结论不同，是这张表最需要一眼分清的两种情况
    expect(rows[1].text()).toContain('一致')
    expect(rows[1].find('.chip-warning').exists()).toBe(false)
    expect(rows[3].text()).toContain('5.6.7.8')
    expect(rows[3].find('.chip-warning').text()).toBe('与结论不同')
    w.unmount()
  })

  it('判不通：chip danger + 后端原因原文上屏，无源结果时不渲染空表', async () => {
    svc.CheckProxy.mockResolvedValue({
      addr: 'http://127.0.0.1:8080',
      ok: false,
      latencyMs: 340,
      egressAgree: 0,
      egressSources: 0,
      error: '5 个源都没拿到出口 IP；首个原因：socks connect: EOF',
      probes: [],
    })
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    await w.find('.check-row .btn-secondary').trigger('click')
    await flushMicrotasks()
    const verdict = w.find('.check-verdict')
    expect(verdict.find('.chip-danger').text()).toBe('不通')
    expect(verdict.text()).toContain('socks connect: EOF')
    expect(w.findAll('.tbl tbody tr')).toHaveLength(0)
    w.unmount()
  })

  it('在途：按钮 disabled + 进行中提示，重复点击被单飞守卫拦住', async () => {
    let resolveFn: (value: unknown) => void = () => {}
    svc.CheckProxy.mockImplementation(() => new Promise((resolve) => { resolveFn = resolve }))
    const w = mount(ProxyCheckPanel)
    await flushMicrotasks()
    await w.find('.check-row .btn-secondary').trigger('click')
    await new Promise(r => setTimeout(r, 0))
    expect((w.find('.check-row .btn-secondary').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.find('.state-box').text()).toContain('正在经该代理')
    await w.find('.check-row .btn-secondary').trigger('click')
    expect(svc.CheckProxy).toHaveBeenCalledTimes(1)
    resolveFn(okResult)
    await flushMicrotasks()
    expect(w.findAll('.tbl tbody tr')).toHaveLength(4)
    expect((w.find('.check-row .btn-secondary').element as HTMLButtonElement).disabled).toBe(false)
    w.unmount()
  })
})
