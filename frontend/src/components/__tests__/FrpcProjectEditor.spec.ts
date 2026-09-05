// 特征测试（组 G4）：FrpcProjectEditor——高危件（表单⇄TOML 双向同步、校验、批量端口）。
// 锁定迁移前行为基线：防抖预览、模式切换的生成/解析调用、校验消息逐字、批量规则生成。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FrpcProjectEditor from '../../components/FrpcProjectEditor.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  GenerateToml: vi.fn(),
  ParseToml: vi.fn(),
  SaveProject: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/frpc/frpcservice', () => svc)

const writeText = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))

function factory(project: unknown = null) {
  return mount(FrpcProjectEditor, {
    props: { project: project as never, installedVersions: ['v0.61.1'] },
    attachTo: document.body,
  })
}

/** 按可见文本片段找按钮（VTU 无 :has-text，统一手写过滤）。 */
function btnText(scope: { findAll: (s: string) => Array<{ text: () => string; trigger: (e: string) => Promise<void> }> }, t: string) {
  const hit = scope.findAll('button').find((b) => b.text().includes(t))
  if (!hit) throw new Error(`未找到按钮：${t}`)
  return hit
}

async function settle() {
  for (let i = 0; i < 30; i++) await Promise.resolve()
}

const fullRule = (over: Record<string, unknown> = {}) => ({
  name: 'ssh', type: 'tcp', role: 'server', localIp: '127.0.0.1', localPort: 22, remotePort: 6000,
  customDomains: [], subdomain: '', secretKey: '', serverName: '', bindAddr: '', bindPort: 0,
  hostHeaderRewrite: '', proxyProtocolVersion: '', bandwidthLimit: '', encryptTransport: false, ...over,
})

beforeEach(() => {
  vi.useFakeTimers()
  svc.GenerateToml.mockResolvedValue('# generated toml')
  Object.defineProperty(globalThis.navigator, 'clipboard', { value: { writeText }, configurable: true })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('FrpcProjectEditor 表单⇄TOML 同步', () => {
  it('挂载即防抖生成预览（250ms 内不触发，之后带默认 draft 调用）', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(100)
    expect(svc.GenerateToml).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(200)
    expect(svc.GenerateToml).toHaveBeenCalledTimes(1)
    const payload = svc.GenerateToml.mock.calls[0][0]
    expect(payload.server.serverPort).toBe(7000)
    expect(payload.proxies[0].localPort).toBe(8080) // 新项目的默认规则
    expect(w.find('.toml-pre').text()).toContain('# generated toml')
    w.unmount()
  })

  it('表单改动 250ms 后反映到 GenerateToml 入参与预览', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    await w.find('input[placeholder^="如：联调环境"]').setValue('web')
    await w.find('input[placeholder="frp.example.com 或 1.2.3.4"]').setValue('a.b.c.d')
    await vi.advanceTimersByTimeAsync(300)
    const last = svc.GenerateToml.mock.calls.at(-1)![0]
    expect(last.name).toBe('web')
    expect(last.server.serverAddr).toBe('a.b.c.d')
    w.unmount()
  })

  it('切到源码模式：GenerateToml 结果进 textarea；GenerateToml 抛错则拦截切换并报错', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    await btnText(w, 'TOML 源码模式').trigger('click')
    await settle()
    expect((w.find('.raw-toml-textarea').element as HTMLTextAreaElement).value).toBe('# generated toml')

    const w2 = factory()
    await vi.advanceTimersByTimeAsync(300) // 先让挂载预览消费正常 mock，再投拒（Once 防挂载吞桩）
    svc.GenerateToml.mockRejectedValueOnce(new Error('字段炸了'))
    await btnText(w2, 'TOML 源码模式').trigger('click')
    await settle()
    expect(w2.find('.error-box').text()).toContain('切换至源码模式失败（当前表单有误）')
    expect(w2.find('.raw-toml-textarea').exists()).toBe(false)
    w.unmount()
    w2.unmount()
  })

  it('源码→表单：ParseToml 成功回写 draft；失败保留源码态并提示无法切回', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    await btnText(w, 'TOML 源码模式').trigger('click')
    await settle()
    svc.ParseToml.mockResolvedValue({
      server: { serverAddr: 'z.z.z.z', serverPort: 7500, token: 't2', tlsEnable: false, useEncryption: true, useCompression: false, logLevel: 'warn' },
      proxies: [fullRule({ name: 'web-rule', localPort: 3000, remotePort: 3001 })],
    })
    await btnText(w, '可视化表单').trigger('click')
    await settle()
    expect((w.find('input[placeholder^="如：联调环境"]').element as HTMLInputElement).value).toBe('') // name 不由解析回写（现状）
    expect(w.find('.proxy-row .input-name').element).toBeTruthy()
    expect((w.find('input[placeholder="frp.example.com 或 1.2.3.4"]').element as HTMLInputElement).value).toBe('z.z.z.z')

    await btnText(w, 'TOML 源码模式').trigger('click')
    await settle()
    svc.ParseToml.mockRejectedValueOnce(new Error('line 3: unexpected token'))
    await btnText(w, '可视化表单').trigger('click')
    await settle()
    expect(w.find('.error-box').text()).toContain('TOML 语法错误，无法切回表单')
    expect(w.find('.error-box').text()).toContain('line 3: unexpected token')
    w.unmount()
  })
})

describe('FrpcProjectEditor 校验与保存', () => {
  it('空名称保存：拦截并提示，不调 SaveProject', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    await btnText(w, '保存项目').trigger('click')
    await settle()
    expect(w.find('.error-box').text()).toContain('请填写项目名称')
    expect(svc.SaveProject).not.toHaveBeenCalled()
    w.unmount()
  })

  it('tcp 规则缺远程端口：校验消息逐字锁定', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    await w.find('input[placeholder^="如：联调环境"]').setValue('ok')
    await w.find('input[placeholder="frp.example.com 或 1.2.3.4"]').setValue('1.2.3.4')
    await w.find('.proxy-row .input-name').setValue('r1')
    await btnText(w, '保存项目').trigger('click')
    await settle()
    expect(w.find('.error-box').text()).toContain('规则「r1」的公网远程端口 (remotePort) 无效')
    w.unmount()
  })

  it('合法保存：payload 含修剪字段，成功 emit saved + toast', async () => {
    const savedOk = { id: 'new1', name: 'web', proxies: [], server: {} }
    svc.SaveProject.mockResolvedValue(savedOk)
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    await w.find('input[placeholder^="如：联调环境"]').setValue('  web  ')
    await w.find('input[placeholder="frp.example.com 或 1.2.3.4"]').setValue('1.2.3.4')
    await w.find('.proxy-row .input-name').setValue('r1')
    const remoteInput = w.findAll('.proxy-row input[type="number"]').at(-1)!
    await remoteInput.setValue('6001')
    await btnText(w, '保存项目').trigger('click')
    await settle()
    expect(svc.SaveProject).toHaveBeenCalledTimes(1)
    expect(svc.SaveProject.mock.calls[0][0].name).toBe('web')
    expect(w.emitted('saved')?.[0]?.[0]).toMatchObject({ id: 'new1' })
    expect(useToast().toastMsg.value).toContain('项目已保存')
    w.unmount()
  })
})

describe('FrpcProjectEditor 批量端口', () => {
  async function openBatch(w: ReturnType<typeof factory>) {
    await btnText(w, '批量端口导入').trigger('click')
    await settle()
    const modal = w.find('.modal-backdrop')
    const inputs = modal.findAll('input')
    // 顺序：prefix / localIp / localPorts / remotePorts
    return { modal, prefix: inputs[0], localIp: inputs[1], localPorts: inputs[2], remotePorts: inputs[3] }
  }

  it('区间+列表混合展开为多条规则并替换空白默认规则', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    const { modal, localPorts, remotePorts } = await openBatch(w)
    await localPorts.setValue('8080-8082,9000')
    await remotePorts.setValue('9080,9081,9082,9083')
    await btnText(modal, '确认生成并添加').trigger('click')
    await settle()
    expect(w.findAll('.proxy-row').length).toBe(4)
    expect((w.findAll('.input-name')[0].element as HTMLInputElement).value).toBe('proxy_8080')
    expect(useToast().toastMsg.value).toContain('已批量添加 4 条端口规则')
    w.unmount()
  })

  it('本地/远程数量不一致：报逐字错误且不入规则', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    const { modal, localPorts, remotePorts } = await openBatch(w)
    await localPorts.setValue('8080-8082')
    await remotePorts.setValue('9080')
    await btnText(modal, '确认生成并添加').trigger('click')
    await settle()
    expect(modal.find('.error-box').text()).toContain('本地端口数量 (3) 与 远程端口数量 (1) 不一致')
    expect(w.findAll('.proxy-row').length).toBe(1)
    w.unmount()
  })

  it('区间跨度 >100 拒绝', async () => {
    const w = factory()
    await vi.advanceTimersByTimeAsync(300)
    const { modal, localPorts, remotePorts } = await openBatch(w)
    await localPorts.setValue('1000-1200')
    await remotePorts.setValue('2000-2200')
    await btnText(modal, '确认生成并添加').trigger('click')
    await settle()
    expect(modal.find('.error-box').text()).toContain('跨度超过 100')
    w.unmount()
  })
})
