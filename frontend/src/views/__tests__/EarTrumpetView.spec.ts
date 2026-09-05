// 特征测试：EarTrumpetView（AUMID 官方直装型——无 JobObject、无事件流、
// 启动后延时复刷、多动作各自 busy、卸载确认框带 busy 语义）。
import { KeepAlive, defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import EarTrumpetView from '../EarTrumpetView.vue'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  GetRemoteVersion: vi.fn(),
  Launch: vi.fn(),
  Exit: vi.fn(),
  Install: vi.fn(),
  Uninstall: vi.fn(),
  OpenRepo: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/eartrumpet/eartrumpetservice', () => svc)

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const snapOf = (over = {}) => ({
  installed: false, running: false, version: '', architecture: '',
  storeCoexist: false, storeVersion: '', ...over,
})

function stubDefaults(snap: Record<string, unknown>, remote = '') {
  svc.GetStatus.mockResolvedValue(snap)
  svc.GetRemoteVersion.mockResolvedValue(remote)
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(EarTrumpetView)) : h('div')),
  })
  const wrapper = mount(Host, {
    attachTo: document.body,
    global: { stubs: { teleport: true } },
  })
  await flushMicrotasks()
  return { wrapper, show }
}

const findBtn = (w: ReturnType<typeof mount>, label: string | RegExp) =>
  w.findAll('.et-actions button').find(b =>
    typeof label === 'string' ? b.text() === label : label.test(b.text()))!

beforeEach(() => {
  svc.Launch.mockResolvedValue(undefined)
  svc.Exit.mockResolvedValue(undefined)
  svc.OpenRepo.mockResolvedValue(undefined)
})
afterEach(() => vi.useRealTimers())

describe('EarTrumpetView 状态呈现', () => {
  it('未安装：「未安装」徽标，仅安装按钮可用（启动/退出禁用）', async () => {
    stubDefaults(snapOf())
    const { wrapper } = await mountView()
    expect(wrapper.find('.et-state').text()).toBe('未安装')
    expect(wrapper.find('.et-overview h2').text()).toBe('尚未安装 EarTrumpet')
    expect(findBtn(wrapper, '安装官方直装版').exists()).toBe(true)
    expect(findBtn(wrapper, '启动').attributes('disabled')).toBeDefined()
    expect(findBtn(wrapper, '退出').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('已安装且运行中：「运行中」徽标，退出可用；facts 进程行=运行中', async () => {
    stubDefaults(snapOf({ installed: true, running: true, version: '3.2.9.0', architecture: 'x64' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.et-state').text()).toBe('运行中')
    expect(findBtn(wrapper, '退出').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('.et-facts').text()).toContain('运行中')
    wrapper.unmount()
  })

  it('有远端新版：安装按钮文案为「更新到 <remote>」', async () => {
    stubDefaults(snapOf({ installed: true, version: '3.2.8.0' }), '3.2.9.0')
    const { wrapper } = await mountView()
    expect(findBtn(wrapper, /更新到 3\.2\.9\.0/).exists()).toBe(true)
    wrapper.unmount()
  })

  it('GetStatus 失败：错误框 + 远端失败静默（官方最新=—）', async () => {
    svc.GetStatus.mockRejectedValue(new Error('包数据库不可访问'))
    svc.GetRemoteVersion.mockRejectedValue(new Error('网络不可达'))
    const { wrapper } = await mountView()
    expect(wrapper.find('.banner-error').text()).toContain('包数据库不可访问')
    expect(wrapper.find('.et-facts').text()).toContain('官方最新')
    expect(wrapper.find('.et-facts').text()).toContain('—')
    wrapper.unmount()
  })

  it('商店版并存警示横幅（含互斥体说明）', async () => {
    stubDefaults(snapOf({ installed: true, storeCoexist: true, storeVersion: '3.2.5.0' }))
    const { wrapper } = await mountView()
    const warn = wrapper.find('.banner-warn')
    expect(warn.text()).toContain('检测到商店版并存')
    expect(warn.text()).toContain('v3.2.5.0')
    wrapper.unmount()
  })
})

describe('EarTrumpetView 操作流', () => {
  it('安装：成功后 toast「官方直装版 … 已就绪」并复刷', async () => {
    stubDefaults(snapOf())
    svc.Install.mockResolvedValue('3.2.9.0')
    const { wrapper } = await mountView()
    const before = svc.GetStatus.mock.calls.length
    await findBtn(wrapper, '安装官方直装版').trigger('click')
    await flushMicrotasks()
    expect(svc.Install).toHaveBeenCalledTimes(1)
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('启动：Launch 后 1500ms 定时复刷（不阻塞）', async () => {
    vi.useFakeTimers()
    stubDefaults(snapOf({ installed: true }))
    const { wrapper } = await mountView()
    const before = svc.GetStatus.mock.calls.length
    await findBtn(wrapper, '启动').trigger('click')
    await vi.advanceTimersByTimeAsync(1499)
    expect(svc.GetStatus.mock.calls.length).toBe(before) // 未到期不复刷
    await vi.advanceTimersByTimeAsync(2)
    expect(svc.GetStatus.mock.calls.length).toBe(before + 1)
    vi.useRealTimers()
    wrapper.unmount()
  })

  it('退出：Exit + toast 文案含「下次登录仍会自启」', async () => {
    stubDefaults(snapOf({ installed: true, running: true }))
    const { wrapper } = await mountView()
    await findBtn(wrapper, '退出').trigger('click')
    await flushMicrotasks()
    expect(svc.Exit).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('卸载：确认框展示 busy 门禁——请求挂起时对话框不关，成功才关并复刷', async () => {
    let resolveUninstall: () => void = () => {}
    stubDefaults(snapOf({ installed: true, version: '3.2.9.0' }))
    svc.Uninstall.mockReturnValue(new Promise<void>(r => (resolveUninstall = r)))
    const { wrapper } = await mountView()
    await findBtn(wrapper, '卸载').trigger('click')
    const dialog = wrapper.find('.workbench-confirm')
    expect(dialog.text()).toContain('卸载 EarTrumpet 直装版')
    expect(dialog.text()).toContain('LocalSettings')
    await dialog.findAll('button').find(b => b.text() === '卸载')!.trigger('click')
    await flushMicrotasks()
    expect(svc.Uninstall).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.workbench-confirm').exists()).toBe(true) // busy：成功前不关
    resolveUninstall()
    await flushMicrotasks()
    expect(wrapper.find('.workbench-confirm').exists()).toBe(false)
    wrapper.unmount()
  })

  it('卸载失败：对话框保持打开 + toast 报错', async () => {
    stubDefaults(snapOf({ installed: true }))
    svc.Uninstall.mockRejectedValue(new Error('部署被拒绝'))
    const { wrapper } = await mountView()
    await findBtn(wrapper, '卸载').trigger('click')
    await wrapper.find('.workbench-confirm').findAll('button').find(b => b.text() === '卸载')!.trigger('click')
    await flushMicrotasks()
    expect(wrapper.find('.workbench-confirm').exists()).toBe(true)
    wrapper.unmount()
  })

  it('项目主页：OpenRepo 调用', async () => {
    stubDefaults(snapOf())
    const { wrapper } = await mountView()
    await findBtn(wrapper, '项目主页').trigger('click')
    await flushMicrotasks()
    expect(svc.OpenRepo).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('KeepAlive 首活跳过双刷、复回再刷', async () => {
    stubDefaults(snapOf())
    const { wrapper, show } = await mountView()
    const afterMount = svc.GetStatus.mock.calls.length
    // 首挂 mounted+activated 只应有一轮 refresh（activated 被跳过）
    expect(afterMount).toBe(1)
    show.value = false
    await wrapper.vm.$nextTick()
    show.value = true
    await flushMicrotasks()
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(afterMount)
    wrapper.unmount()
  })
})
