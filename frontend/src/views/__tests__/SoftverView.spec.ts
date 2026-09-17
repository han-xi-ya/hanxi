// 软件版本检测页（SoftverView）特征测试：双口径回显、官方读数成功/降级两态、
// 扫描按钮→槽位 ID 回传、dir-scan 事件驱动的进行中/完成态、打开目录外呼。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SoftverView from '../SoftverView.vue'

const api = vi.hoisted(() => ({
  Snapshot: vi.fn(),
  RefreshOfficial: vi.fn(),
  OpenUpdatesPage: vi.fn(),
  StartDirScan: vi.fn(),
  CancelDirScan: vi.fn(),
  RevealDir: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/softver', () => ({
  SoftverService: api,
}))

const INSTALL_SLOT_ID = 'install|e:\\program files\\weixin'
const DATA40_ID = 'data40|e:\\system\\文档\\xwechat_files'

function snapBase(over = {}) {
  return {
    probedAt: '2026-09-18T00:00:00+08:00',
    installs: [
      {
        id: 'weixin', generation: '4.x (Weixin)', displayName: '微信', publisher: '腾讯科技(深圳)有限公司',
        registryKey: 'HKEY_LOCAL_MACHINE\\SOFTWARE\\WOW6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\Weixin',
        displayVersion: '4.1.15.9', installDir: 'E:\\Program Files\\Weixin', installDirOrigin: 'registry',
        installDirExists: true, estimatedSizeKb: 893270, bestVersion: '4.1.15.9',
        sources: [
          { kind: 'registry', label: '注册表 Uninstall', field: 'DisplayVersion', value: '4.1.15.9', detail: 'HKLM\\...\\Weixin', primary: true },
          { kind: 'pe', label: 'PE 文件版本信息', field: 'FileVersion', value: '4.1.15.9', detail: 'E:\\Program Files\\Weixin\\Weixin.exe', primary: false },
        ],
      },
    ],
    dirs: [
      { id: INSTALL_SLOT_ID, kind: 'install', label: '安装目录 · 4.x (Weixin)', path: 'E:\\Program Files\\Weixin', origin: 'registry', exists: true, active: false },
      { id: DATA40_ID, kind: 'data40', label: '4.0 数据目录', path: 'E:\\System\\文档\\xwechat_files', origin: 'documents', exists: true, active: true },
      { id: 'data40|c:\\xwechat_files', kind: 'data40', label: '4.0 数据目录', path: 'C:\\xwechat_files', origin: 'driveRoot', exists: false, active: false },
    ],
    update: { status: 'noOfficial', localVersion: '4.1.15.9', officialVersion: '', message: '尚未获取官方最新版本' },
    scanning: [],
    ...over,
  }
}

const snapOfficial = () =>
  snapBase({
    official: {
      version: '4.1.15',
      downloadUrl: 'https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe',
      pageUrl: 'https://weixin.qq.com/updates?platform=windows',
      fetchedAt: '2026-09-18T00:10:00+08:00',
    },
    update: { status: 'latest', localVersion: '4.1.15.9', officialVersion: '4.1.15', message: '本机 4.1.15.9 不低于官方最新 4.1.15，已是最新' },
  })

function emit(name: string, data: unknown) {
  runtime.handlers[name]?.({ data })
}

async function mountView(officialOk = true) {
  const OFFICIAL_ERR = '官方更新页未能解析出微信 Windows 版本（页面可能已改版）'
  api.Snapshot.mockImplementation(async () => {
    if (officialFetched) return snapOfficial()
    return officialOk ? snapBase() : { ...snapBase(), officialError: OFFICIAL_ERR }
  })
  api.RefreshOfficial.mockImplementation(async () => {
    if (!officialOk) throw new Error(OFFICIAL_ERR)
    officialFetched = true
    return { version: '4.1.15' }
  })
  api.StartDirScan.mockResolvedValue(undefined)
  api.CancelDirScan.mockResolvedValue(undefined)
  api.RevealDir.mockResolvedValue(undefined)
  api.OpenUpdatesPage.mockResolvedValue(undefined)
  const wrapper = mount(SoftverView, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

let officialFetched = false

beforeEach(() => {
  officialFetched = false
  runtime.handlers = {}
})

afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

describe('SoftverView', () => {
  it('挂载探测并回显双口径读数、安装目录与数据槽位', async () => {
    const wrapper = await mountView()
    expect(api.Snapshot).toHaveBeenCalled()
    expect(wrapper.text()).toContain('4.1.15.9')
    expect(wrapper.text()).toContain('注册表 Uninstall')
    expect(wrapper.text()).toContain('PE 文件版本信息')
    expect(wrapper.text()).toContain('E:\\Program Files\\Weixin')
    expect(wrapper.text()).toContain('4.0 数据目录')
    expect(wrapper.text()).toContain('在用')
    // 不存在的盘符根候选折叠为一行清单，不占卡片
    expect(wrapper.find('.ghost-list').text()).toContain('C:\\xwechat_files')
    wrapper.unmount()
  })

  it('进入页面自动拉官方读数；成功后回显版本、直链与"已是最新"对比', async () => {
    const wrapper = await mountView()
    expect(api.RefreshOfficial).toHaveBeenCalled()
    expect(wrapper.text()).toContain('4.1.15')
    expect(wrapper.text()).toContain('WeChatWin_4.1.15.exe')
    expect(wrapper.text()).toContain('已是最新')
    wrapper.unmount()
  })

  it('官方解析失败：挂降级 banner 并可打开官方页（不猜不编）', async () => {
    const wrapper = await mountView(false)
    const banner = wrapper.find('.banner-warn')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('官方通道读取失败')
    expect(banner.text()).toContain('打开官方更新页')
    await banner.find('.link-button').trigger('click')
    expect(api.OpenUpdatesPage).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('点扫描按槽位 ID 请求；running 事件出取消钮与实时读数，done 事件写回大小', async () => {
    const wrapper = await mountView()
    const dataRow = wrapper.findAll('.dir-row').find((r) => r.text().includes('xwechat_files'))!
    await dataRow.findAll('button').find((b) => b.text().includes('扫描大小'))!.trigger('click')
    expect(api.StartDirScan).toHaveBeenCalledWith(DATA40_ID)

    emit('softver:dir-scan', { id: DATA40_ID, state: 'running', bytes: 1024 ** 3 * 2, files: 7, dirs: 1, skipped: 0, current: 'E:\\x\\a' })
    await flushPromises()
    const live = wrapper.find('.scan-live')
    expect(live.text()).toContain('2.0 GB')
    expect(live.text()).toContain('7 文件')
    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    expect(api.CancelDirScan).toHaveBeenCalledWith(DATA40_ID)

    emit('softver:dir-scan', { id: DATA40_ID, state: 'done', bytes: 1024 ** 3 * 40, files: 9, dirs: 2, skipped: 3, current: '', scannedAt: '2026-09-18T00:20:00+08:00' })
    await flushPromises()
    const row = wrapper.findAll('.dir-row').find((r) => r.text().includes('xwechat_files'))!
    expect(row.text()).toContain('40.0 GB')
    expect(row.text()).toContain('9 文件')
    expect(row.text()).toContain('3 项不可读，偏小')
    wrapper.unmount()
  })

  it('打开按钮走 RevealDir（后端按槽位解析路径），取消的半成品不进快照', async () => {
    const wrapper = await mountView()
    const installRow = wrapper.findAll('.dir-row').find((r) => r.text().includes('安装目录'))!
    await installRow.findAll('button').find((b) => b.text() === '打开')!.trigger('click')
    expect(api.RevealDir).toHaveBeenCalledWith(INSTALL_SLOT_ID)

    emit('softver:dir-scan', { id: INSTALL_SLOT_ID, state: 'canceled', bytes: 123, files: 1, dirs: 0, skipped: 0, current: '', message: '已取消' })
    await flushPromises()
    const row = wrapper.findAll('.dir-row').find((r) => r.text().includes('安装目录'))!
    expect(row.text()).not.toContain('KB') // 取消读数未被写回
    wrapper.unmount()
  })

  it('扫描失败事件出错误回执且不污染槽位', async () => {
    const wrapper = await mountView()
    emit('softver:dir-scan', { id: DATA40_ID, state: 'error', bytes: 0, files: 0, dirs: 0, skipped: 0, current: '', message: '目录被占用' })
    await flushPromises()
    const row = wrapper.findAll('.dir-row').find((r) => r.text().includes('xwechat_files'))!
    expect(row.text()).not.toContain('GB')
    wrapper.unmount()
  })
})
