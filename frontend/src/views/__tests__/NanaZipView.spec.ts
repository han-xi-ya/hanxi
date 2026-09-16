// 特征测试：NanaZipView（MSIX 安装管理型，OperationAccepted+进度事件形态，无轮询）。
// 卸载/降级/移除缓存三类确认已收编为全局 useConfirm 单例：测试经
// confirmState/settleConfirm 驱动，文案与流程断言语义不变。
import { KeepAlive, defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import NanaZipView from '../NanaZipView.vue'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  GetPackageSnapshot: vi.fn(),
  ListCachedPackages: vi.fn(),
  ListReleases: vi.fn(),
  InstallVersion: vi.fn(),
  Launch: vi.fn(),
  Uninstall: vi.fn(),
  RemoveCachedPackage: vi.fn(),
}))
const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))
vi.mock('../../../bindings/hanxi/internal/modules/nanazip/nanazipservice', () => svc)

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const snapshot = (over = {}) => ({
  installed: false, revision: 1, version: '', architecture: '', packageStatus: '', packageFamily: '', ...over,
})
const release = (version: string, size = 5 * 1024 * 1024, stale = false) => ({
  version, size, published: '2026-08-02T00:00:00Z', sha256: 'a'.repeat(64), stale,
})
const progress = (over = {}) => ({
  operationId: 'op-1', kind: 'install', targetVersion: '', stage: 'preflight', done: 0, total: 0,
  message: '', terminal: false, success: false, errorCode: '', errorDetail: '', ...over,
})

function stubDefaults(snap: Record<string, unknown>, releases: unknown[] = [], cached: unknown[] = []) {
  svc.GetPackageSnapshot.mockResolvedValue(snap)
  svc.ListCachedPackages.mockResolvedValue(cached)
  svc.ListReleases.mockResolvedValue(releases)
}

async function mountView() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(NanaZipView)) : h('div')),
  })
  const wrapper = mount(Host, {
    attachTo: document.body,
    global: { stubs: { teleport: true } }, // 局部 ConfirmDialog Teleport 原地化
  })
  await flushMicrotasks()
  return { wrapper, show }
}

const { confirmState, settleConfirm } = useConfirm()

beforeEach(() => {
  settleConfirm(false) // 兜底落定上个用例可能悬挂的确认
})
afterEach(() => {
  settleConfirm(false)
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('NanaZipView 包状态呈现', () => {
  it('未安装：状态胶囊「未安装」，主操作为「选择版本安装」，无卸载按钮', async () => {
    stubDefaults(snapshot())
    const { wrapper } = await mountView()
    expect(wrapper.find('.msix-state').text()).toBe('未安装')
    expect(wrapper.find('.msix-overview h2').text()).toBe('尚未安装 NanaZip')
    const labels = wrapper.findAll('.msix-actions button').map(b => b.text())
    expect(labels).toContain('选择版本安装')
    expect(labels).not.toContain('卸载')
    wrapper.unmount()
  })

  it('已安装：版本标题、打开与卸载按钮就位；facts 展示包信息', async () => {
    stubDefaults(snapshot({ installed: true, version: '1.19.0', architecture: 'x64', packageStatus: 'Ok' }))
    const { wrapper } = await mountView()
    expect(wrapper.find('.msix-state').text()).toBe('已安装')
    expect(wrapper.find('.msix-overview h2').text()).toBe('NanaZip 1.19.0')
    expect(wrapper.find('.nanazip-state-box.error').exists()).toBe(false)
    const labels = wrapper.findAll('.msix-actions button').map(b => b.text())
    expect(labels).toEqual(expect.arrayContaining(['打开 NanaZip', '刷新状态', '卸载']))
    expect(wrapper.text()).toContain('x64')
    wrapper.unmount()
  })

  it('本地读取失败：错误框 + 重试再次调用', async () => {
    svc.GetPackageSnapshot.mockRejectedValue(new Error('PowerShell 超时'))
    svc.ListCachedPackages.mockResolvedValue([])
    svc.ListReleases.mockResolvedValue([])
    const { wrapper } = await mountView()
    expect(wrapper.find('.nanazip-state-box.error').text()).toContain('PowerShell 超时')
    const before = svc.GetPackageSnapshot.mock.calls.length
    await wrapper.find('.nanazip-state-box.error button').trigger('click')
    await flushMicrotasks()
    expect(svc.GetPackageSnapshot.mock.calls.length).toBe(before + 1)
    wrapper.unmount()
  })

  it('KeepAlive 复回触发 refreshLocal（无定时轮询形态）', async () => {
    stubDefaults(snapshot())
    const { wrapper, show } = await mountView()
    const before = svc.GetPackageSnapshot.mock.calls.length
    show.value = false
    await wrapper.vm.$nextTick()
    show.value = true
    await flushMicrotasks()
    expect(svc.GetPackageSnapshot.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })
})

describe('NanaZipView 版本关系矩阵与安装', () => {
  it('升级/已安装/降级/安装 四关系的按钮文案与禁用', async () => {
    stubDefaults(
      snapshot({ installed: true, version: '1.18.0' }),
      [release('1.19.0'), release('1.18.0'), release('1.17.0')],
    )
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    const buttons = wrapper.findAll('.nanazip-row-actions button')
    expect(buttons.map(b => b.text())).toEqual(['升级', '已安装', '降级'])
    expect(buttons[1].attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('安装 accepted 返回即进入 preflight 进度（现状：进度条仅安装管理 Tab 渲染，需切回查看）', async () => {
    stubDefaults(snapshot(), [release('1.19.0')])
    svc.InstallVersion.mockResolvedValue({ operationId: 'op-9', kind: 'install', message: '已进入队列' })
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    await wrapper.find('.nanazip-row-actions button').trigger('click')
    await flushMicrotasks()
    expect(svc.InstallVersion).toHaveBeenCalledWith('1.19.0', false)
    expect(wrapper.find('.nanazip-progress').exists()).toBe(false) // quirk 锁定：版本资源 Tab 看不到进度
    await wrapper.findAll('.main-tab-btn')[0].trigger('click')
    expect(wrapper.find('.nanazip-progress').text()).toContain('检查状态')
    expect(wrapper.find('.nanazip-progress').text()).toContain('已进入队列')
    wrapper.unmount()
  })

  it('降级先经全局确认（warning+双版本明细）；确认后携 allowDowngrade=true', async () => {
    stubDefaults(snapshot({ installed: true, version: '1.19.0' }), [release('1.18.0')])
    svc.InstallVersion.mockResolvedValue({ operationId: 'op-8', kind: 'install', message: '' })
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    await wrapper.find('.nanazip-row-actions button').trigger('click')
    await flushMicrotasks()
    expect(svc.InstallVersion).not.toHaveBeenCalled()
    expect(wrapper.find('.workbench-confirm').exists()).toBe(false) // 视图不再自挂弹层
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确认降级')
    expect(confirmState.options.confirmLabel).toBe('确认降级')
    expect(confirmState.options.tone).toBe('warning')
    expect(confirmState.options.description).toContain('ForceUpdateFromAnyVersion')
    expect(confirmState.options.details).toEqual([
      { label: '当前版本', value: '1.19.0' },
      { label: '目标版本', value: '1.18.0' },
    ])
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.InstallVersion).toHaveBeenCalledWith('1.18.0', true)
    wrapper.unmount()
  })

  it('降级确认取消：不发起安装', async () => {
    stubDefaults(snapshot({ installed: true, version: '1.19.0' }), [release('1.18.0')])
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    await wrapper.find('.nanazip-row-actions button').trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.InstallVersion).not.toHaveBeenCalled()
    expect(confirmState.open).toBe(false)
    wrapper.unmount()
  })

  it('卸载确认文案含「不会自动重启 Explorer」，确认后 Uninstall 并置 uninstalling 进度', async () => {
    stubDefaults(snapshot({ installed: true, version: '1.19.0' }))
    svc.Uninstall.mockResolvedValue({ operationId: 'op-7', kind: 'uninstall', message: 'Windows 正在卸载' })
    const { wrapper } = await mountView()
    const uninstallBtn = wrapper.findAll('.msix-actions button').find(b => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.Uninstall).not.toHaveBeenCalled()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('卸载 NanaZip')
    expect(confirmState.options.confirmLabel).toBe('卸载 NanaZip')
    expect(confirmState.options.tone).toBe('danger')
    expect(confirmState.options.description).toContain('不会自动重启 Explorer')
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.Uninstall).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.nanazip-progress').text()).toContain('Windows 正在卸载')
    wrapper.unmount()
  })
})

describe('NanaZipView 进度与快照事件', () => {
  it('本地无进行中操作时直接采纳事件（挂载错过开局场景）并换算百分比', async () => {
    stubDefaults(snapshot())
    const { wrapper } = await mountView()
    runtime.handlers['nanazip:operation-progress']({
      data: progress({ operationId: 'whoever', stage: 'downloading', done: 50, total: 100 }),
    })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.nanazip-progress').text()).toContain('下载安装包')
    expect(wrapper.find('.nanazip-progress-value').text()).toBe('50%')
    wrapper.unmount()
  })

  it('本地已发起操作后，异 operationId 事件不得覆盖', async () => {
    stubDefaults(snapshot(), [release('1.19.0')])
    svc.InstallVersion.mockResolvedValue({ operationId: 'op-1', kind: 'install', message: '' })
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    await wrapper.find('.nanazip-row-actions button').trigger('click')
    await flushMicrotasks()
    await wrapper.findAll('.main-tab-btn')[0].trigger('click') // 进度区在管理 Tab
    runtime.handlers['nanazip:operation-progress']({
      data: progress({ operationId: 'other', stage: 'installing', message: '别覆盖我' }),
    })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.nanazip-progress').text()).not.toContain('别覆盖我')
    wrapper.unmount()
  })

  it('error 阶段写入对应版本行内错误', async () => {
    stubDefaults(snapshot(), [release('1.19.0')])
    svc.InstallVersion.mockResolvedValue({ operationId: 'op-1', kind: 'install', message: '' })
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    await wrapper.find('.nanazip-row-actions button').trigger('click')
    await flushMicrotasks()
    runtime.handlers['nanazip:operation-progress']({
      data: progress({ operationId: 'op-1', targetVersion: '1.19.0', stage: 'error', terminal: true, success: false, message: '摘要校验失败' }),
    })
    await flushMicrotasks()
    expect(wrapper.text()).toContain('摘要校验失败')
    expect(svc.GetPackageSnapshot.mock.calls.length).toBeGreaterThan(1) // terminal 复刷本地
    wrapper.unmount()
  })

  it('快照事件低 revision 拒绝覆盖（乱序防护）', async () => {
    stubDefaults(snapshot({ installed: true, version: '1.19.0', revision: 5 }))
    const { wrapper } = await mountView()
    runtime.handlers['nanazip:package-snapshot']({ data: snapshot({ installed: false, revision: 4 }) })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.msix-state').text()).toBe('已安装')
    runtime.handlers['nanazip:package-snapshot']({ data: snapshot({ installed: false, revision: 6 }) })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.msix-state').text()).toBe('未安装')
    wrapper.unmount()
  })

  it('缓存移除：确认后 RemoveCachedPackage + toast + 复刷', async () => {
    stubDefaults(snapshot(), [], [{ version: '1.18.0', size: 2 * 1024 * 1024, architectures: ['x64'], verificationMode: 'sha256' }])
    svc.RemoveCachedPackage.mockResolvedValue(undefined)
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    expect(wrapper.find('.nanazip-resource-row').text()).toContain('2.0 MB') // formatBytes 基线（远离 1MB 边界，与统一 fmtSize 行为一致）
    await wrapper.find('.btn-danger-outline').trigger('click')
    await flushMicrotasks()
    expect(svc.RemoveCachedPackage).not.toHaveBeenCalled()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('移除安装包缓存')
    expect(confirmState.options.confirmLabel).toBe('移除缓存')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveCachedPackage).toHaveBeenCalledWith('1.18.0')
    wrapper.unmount()
  })

  it('stale 列表横幅提示上次缓存', async () => {
    stubDefaults(snapshot(), [release('1.19.0', 5 * 1024 * 1024, true)])
    const { wrapper } = await mountView()
    await wrapper.findAll('.main-tab-btn')[1].trigger('click')
    expect(wrapper.find('.banner-warn').exists()).toBe(true)
    wrapper.unmount()
  })
})
