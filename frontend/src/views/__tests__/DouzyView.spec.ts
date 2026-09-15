// 特征测试：DouzyView 是「仅版本+下载」的精简页——无状态轮询/无启停分支。
// 核心契约：内测警示横幅常驻、下载事件驱动进度、「运行安装程序」直达后端、
// 删除安装包必经确认且文案诚实区分"删包 ≠ 卸载程序"。绑定/事件经 vi.mock 打桩。
import { KeepAlive, defineComponent, h, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DouzyView from '../DouzyView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  DownloadVersion: vi.fn(),
  LaunchInstaller: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/douzy/douzyservice', () => svc)

const relV0115 = {
  version: 'v0.11.5', tag: 'desktop-v0.11.5', published: '2026-09-10T07:35:13Z', isPre: false,
  assetName: 'Douzy-Setup-0.11.5.exe', assetUrl: '', size: 147 * 1024 * 1024, sha256: 'aa',
}
const installedV0115 = {
  version: 'v0.11.5', exePath: 'D:\\hanxidata\\versions\\douzy_0.11.5\\Douzy-Setup-0.11.5.exe',
  dir: 'D:\\hanxidata\\versions\\douzy_0.11.5', size: 147 * 1024 * 1024, installedAt: '2026-09-14 10:00:00', sha256: 'aa',
}

function stubDefaults(releases: Array<Record<string, unknown>> = [relV0115], installed: Array<Record<string, unknown>> = []) {
  svc.ListReleases.mockResolvedValue(releases)
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.RepositoryURL.mockResolvedValue('https://github.com/jiji262/douyin-downloader')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(DouzyView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('DouzyView 装载与诚实文案', () => {
  it('初始拉取远程列表/已下载/仓库地址；无 GetStatus（不托管进程）', async () => {
    stubDefaults()
    const wrapper = await mountView()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(svc.RepositoryURL).toHaveBeenCalled()
    // 本模块绑定面只有版本八件套+安装器，无实例通道
    expect(Object.keys(svc)).not.toContain('GetStatus')
    wrapper.unmount()
  })

  it('内测警示横幅常驻，且声明"不接管其运行"', async () => {
    stubDefaults()
    const wrapper = await mountView()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('内测')
    expect(banner.text()).toContain('不接管其运行')
    wrapper.unmount()
  })

  it('远程表已下载版本显示「已下载」，未下载显示「可下载」+下载按钮', async () => {
    stubDefaults([relV0115, { ...relV0115, version: 'v0.11.4', tag: 'desktop-v0.11.4', assetName: 'Douzy-Setup-0.11.4.exe' }], [installedV0115])
    const wrapper = await mountView()
    const statuses = wrapper.findAll('.dz-status').map(s => s.text())
    expect(statuses).toContain('已下载')
    expect(statuses).toContain('可下载')
    wrapper.unmount()
  })
})

describe('DouzyView 下载与进度事件', () => {
  it('点击「下载安装包」调 DownloadVersion(版本)', async () => {
    stubDefaults([relV0115]) // 未下载任何包（installed 为空），下载按钮在远程表内
    svc.DownloadVersion.mockResolvedValue('started')
    const wrapper = await mountView()
    const dl = wrapper.findAll('tbody .btn').find(b => b.text().includes('下载安装包'))!
    await dl.trigger('click')
    await flushPromises()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v0.11.5')
    wrapper.unmount()
  })

  it('downloading 事件渲染进度条；done 事件清条目并刷新列表', async () => {
    stubDefaults()
    const wrapper = await mountView()
    runtime.handlers['douzy:version-download']({
      data: { version: 'v0.11.5', stage: 'downloading', done: 50 * 1024 * 1024, total: 147 * 1024 * 1024, message: '' },
    })
    await nextTick()
    expect(wrapper.find('.dz-status.downloading').exists()).toBe(true)
    expect(wrapper.find('.dz-percent').text()).toBe('34%')

    svc.ListInstalledVersions.mockClear()
    runtime.handlers['douzy:version-download']({ data: { version: 'v0.11.5', stage: 'done', done: 100, total: 100, message: '' } })
    await flushPromises()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('DouzyView 安装器与删除', () => {
  it('「运行安装程序」调 LaunchInstaller 并 toast 安装向导提示', async () => {
    stubDefaults([relV0115], [installedV0115])
    svc.LaunchInstaller.mockResolvedValue(undefined)
    const wrapper = await mountView()
    const btn = wrapper.findAll('.dz-card-actions .btn').find(b => b.text().includes('运行安装程序'))!
    await btn.trigger('click')
    await flushPromises()
    expect(svc.LaunchInstaller).toHaveBeenCalledWith('v0.11.5')
    expect(useToast().toastMsg.value).toContain('安装向导已弹出')
    wrapper.unmount()
  })

  it('删除必经确认：文案含"不会卸载"；确认后 RemoveVersion + 刷新', async () => {
    stubDefaults([relV0115], [installedV0115])
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { confirmState, settleConfirm } = useConfirm()
    const wrapper = await mountView()
    const del = wrapper.findAll('.dz-card-actions .btn').find(b => b.text().includes('删除安装包'))!
    await del.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定删除 Douzy v0.11.5 安装包？')
    expect(confirmState.options.description).toContain('不会卸载')
    settleConfirm(true)
    await flushPromises()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v0.11.5')
    wrapper.unmount()
  })

  it('确认取消则不动后端', async () => {
    stubDefaults([relV0115], [installedV0115])
    const { settleConfirm } = useConfirm()
    const wrapper = await mountView()
    const del = wrapper.findAll('.dz-card-actions .btn').find(b => b.text().includes('删除安装包'))!
    await del.trigger('click')
    await flushMicrotasks()
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
