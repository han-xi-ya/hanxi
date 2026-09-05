// 组 G1 特征测试：WifiView——锁迁移前行为基线（§9 手册步骤 1）。
// 现状契约：无轮询/事件订阅；剪贴板为 navigator.clipboard 直用（迁移后走 useClipboard，
// 失败 toast 从 `复制失败: {msg}` 收敛为含 '复制失败' 前缀——断言用 toContain 双态兼容）。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import WifiView from '../WifiView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  ListProfiles: vi.fn(),
}))
const qr = vi.hoisted(() => ({ toString: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/wifi', () => ({
  WifiService: svc,
}))
vi.mock('qrcode', () => ({ default: qr }))

function flushMicrotasks(times = 20) {
  return (async () => { for (let i = 0; i < times; i++) await Promise.resolve() })()
}

const Host = defineComponent({ render: () => h(WifiView) })

async function mountWith(profiles: Array<{ ssid: string; password: string }>) {
  svc.ListProfiles.mockResolvedValue(profiles)
  const w = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useToast().clearToast()
})

describe('WifiView', () => {
  it('挂载即拉取列表并渲染 SSID 与密码明文', async () => {
    const w = await mountWith([{ ssid: 'Home_5G', password: 's3cret' }])
    expect(svc.ListProfiles).toHaveBeenCalled()
    expect(w.find('.pw').text()).toBe('s3cret')
    expect(w.find('tbody strong').text()).toBe('Home_5G')
    w.unmount()
  })

  it('无密码网络显示破折号且不给复制按钮', async () => {
    const w = await mountWith([{ ssid: 'Cafe', password: '' }])
    expect(w.find('tbody tr').text()).toContain('—')
    expect(w.findAll('.btn-icon')).toHaveLength(1) // 仅扫码钮
    w.unmount()
  })

  it('空列表给出占位提示', async () => {
    const w = await mountWith([])
    expect(w.find('.empty-hint').text()).toContain('未发现已保存的 Wi-Fi 网络')
    expect(w.find('.hint-sub').text()).toContain('WLAN')
    w.unmount()
  })

  it('加载失败进 error-box（文案前缀锁定）', async () => {
    svc.ListProfiles.mockRejectedValue(new Error('WLAN 服务不可用'))
    const w = mount(Host)
    await flushMicrotasks()
    expect(w.find('.error-box').text()).toContain('加载 Wi-Fi 列表失败: WLAN 服务不可用')
    w.unmount()
  })

  it('刷新按钮再次拉取', async () => {
    const w = await mountWith([{ ssid: 'A', password: 'b' }])
    const before = svc.ListProfiles.mock.calls.length
    await w.findAll('button').find((b) => b.text().includes('刷新列表'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.ListProfiles.mock.calls.length).toBe(before + 1)
    w.unmount()
  })

  it('扫码按钮生成 WIFI: 协议二维码弹窗（含 nopass 变体与转义）', async () => {
    qr.toString.mockResolvedValue('<svg data-qa="qr"></svg>')
    const w = await mountWith([{ ssid: 'Home;5G', password: 'p,w' }])
    await w.find('.btn-icon').trigger('click')
    await flushMicrotasks()
    expect(qr.toString).toHaveBeenCalled()
    const text: string = qr.toString.mock.calls[0][0]
    expect(text).toBe('WIFI:T:WPA;S:Home\\;5G;P:p\\,w;;')
    expect(w.find('.qr-container').html()).toContain('qr')
    // 关闭
    await w.find('.btn-close').trigger('click')
    expect(w.find('.modal-overlay').exists()).toBe(false)
    w.unmount()
  })

  it('"未设置密码或无法读取"走 nopass 且不显示复制按钮', async () => {
    qr.toString.mockResolvedValue('<svg/>')
    const w = await mountWith([{ ssid: 'Guest', password: '未设置密码或无法读取' }])
    const icons = w.findAll('.btn-icon')
    expect(icons).toHaveLength(1) // 仅 📱
    await icons[0].trigger('click')
    await flushMicrotasks()
    expect(qr.toString.mock.calls[0][0]).toBe('WIFI:T:nopass;S:Guest;;')
    // 现状：哨兵串非空，弹窗原样展示该提示语（而非「无密码」——那是空串的分支）
    expect(w.find('.qr-info').text()).toContain('未设置密码或无法读取')
    w.unmount()
  })

  it('复制密码成功 toast', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = await mountWith([{ ssid: 'Home', password: 'pw123' }])
    const copyBtn = w.findAll('.btn-icon')[1]
    await copyBtn.trigger('click')
    await flushMicrotasks()
    expect(writeText).toHaveBeenCalledWith('pw123')
    expect(useToast().toastMsg.value).toBe('已复制密码: Home')
    w.unmount()
  })

  it('复制失败 toast 以「复制失败」起头（具体细节随实现收敛）', async () => {
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('用户拒绝')) } })
    vi.stubGlobal('document', document)
    const w = await mountWith([{ ssid: 'Home', password: 'pw' }])
    const exec = vi.fn(() => false)
    Object.defineProperty(document, 'execCommand', { value: exec, configurable: true })
    await w.findAll('.btn-icon')[1].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('复制失败')
    w.unmount()
  })
})
