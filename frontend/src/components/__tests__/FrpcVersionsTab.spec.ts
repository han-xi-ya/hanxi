// 特征测试（组 G4）：FrpcVersionsTab——frpc 版本管理子页。
// 基线锁定迁移前行为；迁移（useWailsEvent/useConfirm/useToast 全局化/format 收编）后除机制面外逐字保持。
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FrpcVersionsTab from '../../components/FrpcVersionsTab.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  DownloadVersion: vi.fn(),
  ImportLocalFrpc: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/frpc/frpcservice', () => svc)

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const release061 = { version: 'v0.61.1', size: 2 * 1024 * 1024 + 1024, published: '2026-07-01T08:00:00Z', isPre: false, sha256: 'a'.repeat(64) }
const installed061 = { version: '0.61.1', exePath: 'C:\\frp\\0.61.1\\frpc.exe', size: 2 * 1024 * 1024, installedAt: '2026-07-05', isImport: false, sha256: 'a'.repeat(64) }

function stubLoad() {
  svc.ListReleases.mockResolvedValue([release061])
  svc.ListInstalledVersions.mockResolvedValue([installed061])
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useToast().clearToast()
})

describe('FrpcVersionsTab 装载与展示', () => {
  it('初始并发拉取远程/本地列表并上报 version-changed', async () => {
    stubLoad()
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    expect(svc.ListReleases).toHaveBeenCalled()
    expect(svc.ListInstalledVersions).toHaveBeenCalled()
    expect(wrapper.emitted('version-changed')).toBeTruthy()
    // 展示：本地卡片 + 远程行、fmtSize MB 一位小数、fmtDate 前 10 位
    expect(wrapper.find('.installed-card').text()).toContain('0.61.1')
    expect(wrapper.find('.tbl tbody tr').text()).toContain('2.0 MB')
    expect(wrapper.find('.tbl tbody tr').text()).toContain('2026-07-01')
    wrapper.unmount()
  })

  it('已装判定做 v 前缀归一：installed=0.61.1 对 release=v0.61.1 → 已安装', async () => {
    stubLoad()
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    expect(wrapper.find('.frpc-version-status.installed').exists()).toBe(true)
    expect(wrapper.find('.frpc-version-status.idle').exists()).toBe(false)
    wrapper.unmount()
  })

  it('未安装版本 → idle + 下载安装按钮', async () => {
    svc.ListReleases.mockResolvedValue([{ ...release061, version: 'v0.62.0' }])
    svc.ListInstalledVersions.mockResolvedValue([])
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    expect(wrapper.find('.frpc-version-status.idle').text()).toBe('可安装')
    expect(wrapper.find('button').text()).toContain('刷新远程列表')
    wrapper.unmount()
  })
})

describe('FrpcVersionsTab 操作', () => {
  it('下载竞态已装：already-installed → toast 提示并重载列表', async () => {
    // 渲染时未装（idle 有下载按钮），点击时后端发现已装 → 'already-installed' 分支
    svc.ListReleases.mockResolvedValue([{ ...release061, version: 'v0.62.0' }])
    svc.ListInstalledVersions.mockResolvedValue([])
    svc.DownloadVersion.mockResolvedValue('already-installed')
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    const dlBtn = wrapper.findAll('.tbl tbody button').find((b) => b.text() === '下载安装')!
    await dlBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v0.62.0')
    // 迁移注记：局部 toast 升级为全局 useToast 单例（App.vue 统一渲染）
    expect(useToast().toastMsg.value).toContain('版本 v0.62.0 已安装')
    expect(svc.ListInstalledVersions.mock.calls.length).toBeGreaterThanOrEqual(2) // 已重载
    wrapper.unmount()
  })

  // 迁移注记：卸载确认由 window.confirm 收编至全局 useConfirm 单例（文案逐字拆 title/description）
  it('卸载必经确认对话框；确认后 RemoveVersion+toast，取消不动', async () => {
    stubLoad()
    svc.RemoveVersion.mockResolvedValue(undefined)
    const { confirmState, settleConfirm } = useConfirm()
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    const uninstallBtn = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 frpc 0.61.1？')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()

    await uninstallBtn.trigger('click')
    await flushMicrotasks()
    settleConfirm(true)
    await flushMicrotasks()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('0.61.1')
    expect(useToast().toastMsg.value).toContain('已卸载 0.61.1')
    wrapper.unmount()
  })

  it('打开位置按钮把 exePath 原样传给 OpenDir（现状：传文件路径而非目录）', async () => {
    stubLoad()
    svc.OpenDir.mockResolvedValue(undefined)
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    const openBtn = wrapper.findAll('.installed-card button').find((b) => b.text().includes('打开位置'))!
    await openBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.OpenDir).toHaveBeenCalledWith('C:\\frp\\0.61.1\\frpc.exe')
    wrapper.unmount()
  })

  it('导入本地：成功 toast 版本并重载；失败 toast 导入失败前缀', async () => {
    stubLoad()
    svc.ImportLocalFrpc.mockResolvedValue({ version: '0.60.0', exePath: 'C:\\x\\frpc.exe' })
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    const importBtn = wrapper.findAll('.btn-group button').find((b) => b.text().includes('导入本地'))!
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('已导入 frpc 0.60.0')

    svc.ImportLocalFrpc.mockRejectedValue(new Error('用户取消'))
    await importBtn.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('导入失败: 用户取消')
    wrapper.unmount()
  })
})

describe('FrpcVersionsTab 下载进度事件', () => {
  it('downloading 事件 → 状态徽标+百分比进度条', async () => {
    svc.ListReleases.mockResolvedValue([{ ...release061, version: 'v0.62.0' }])
    svc.ListInstalledVersions.mockResolvedValue([])
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    runtime.handlers['frpc:version-download']({ data: { version: 'v0.62.0', stage: 'downloading', done: 50, total: 100 } })
    await nextTick()
    expect(wrapper.find('.frpc-version-status.downloading').text()).toBe('下载中')
    expect(wrapper.find('.dl-percent').text()).toBe('50%')
    wrapper.unmount()
  })

  it('hash 阶段显示"校验 SHA256…"文案', async () => {
    svc.ListReleases.mockResolvedValue([{ ...release061, version: 'v0.62.0' }])
    svc.ListInstalledVersions.mockResolvedValue([])
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    runtime.handlers['frpc:version-download']({ data: { version: 'v0.62.0', stage: 'hash' } })
    await nextTick()
    expect(wrapper.find('.dl-meta-text').text()).toContain('校验 SHA256…')
    wrapper.unmount()
  })

  it('error 阶段：状态格显示失败，操作列呈现错误详情与重试入口（§9.5-2 修复）', async () => {
    svc.ListReleases.mockResolvedValue([{ ...release061, version: 'v0.62.0' }])
    svc.ListInstalledVersions.mockResolvedValue([])
    svc.DownloadVersion.mockResolvedValue(undefined)
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    runtime.handlers['frpc:version-download']({ data: { version: 'v0.62.0', stage: 'error', message: 'SHA256 校验不符' } })
    await nextTick()
    expect(wrapper.find('.frpc-version-status.error').text()).toBe('失败')
    expect(wrapper.find('.dl-error').text()).toBe('SHA256 校验不符')
    const retry = wrapper.findAll('button').find((b) => b.text() === '重试')!
    await retry.trigger('click')
    await flushMicrotasks()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v0.62.0')
    wrapper.unmount()
  })

  it('卸载组件时注销事件监听', async () => {
    stubLoad()
    const wrapper = mount(FrpcVersionsTab)
    await flushMicrotasks()
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(1)
  })

  it('toast 2.5s 自动清除（迁移后为全局 useToast 单例语义）', async () => {
    vi.useFakeTimers()
    try {
      stubLoad()
      svc.OpenDir.mockRejectedValue(new Error('资源管理器炸了'))
      const wrapper = mount(FrpcVersionsTab)
      await vi.advanceTimersByTimeAsync(0)
      // 微任务排空（fake timers 下不用 flushPromises，见 §25）
      for (let i = 0; i < 50; i++) await Promise.resolve()
      const openBtn = wrapper.findAll('.installed-card button').find((b) => b.text().includes('打开位置'))!
      await openBtn.trigger('click')
      for (let i = 0; i < 50; i++) await Promise.resolve()
      expect(useToast().toastMsg.value).toContain('打开所在目录失败')
      await vi.advanceTimersByTimeAsync(2600)
      expect(useToast().toastMsg.value).toBe('')
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})

