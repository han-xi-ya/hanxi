// DbxView 特征测试（三线并行 · 冻结契约口径）：标准壳（ManagedConsoleShell）
// 装配下的账本漂移警示三处同源（warn 横幅 + 状态灯琥珀 + 页头徽标）、
// metaHints 五条如实披露、末版卸载预告复用（soleVersionUninstallNote）、
// followOnExit 开关（含「托管备份」注记）。绑定面经 vi.mock 打桩（真实生成物
// 落盘前由 vitest.config 的解析缝兜底）；事件经 @wailsio/runtime 打桩。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DbxView from '../DbxView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/dbx/dbxservice', () => svc)
vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

function statusOf(partial: Record<string, unknown>) {
  return { state: 'stopped', version: '', pid: 0, error: '', startedAt: '', drifted: false, driftNote: '', dataDir: 'D:\\hanxi-data\\dbx', ...partial }
}

const installed0930 = {
  version: '0.9.30',
  exePath: 'D:\\dbx\\0.9.30\\dbx.exe',
  dir: 'D:\\dbx\\0.9.30',
  size: 9 * 1024 * 1024,
  installedAt: '2026-09-10',
  isImport: false,
  source: '',
}

const release0932 = { version: '0.9.32', published: '2026-09-25T00:00:00Z', size: 10 * 1024 * 1024, form: 'portable' }

function stubDefaults(snap: Record<string, unknown>, installed: unknown[] = [installed0930], releases: unknown[] = [release0932]) {
  svc.GetStatus.mockResolvedValue(statusOf(snap))
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue((installed[0] as { version?: string })?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(false)
  svc.RepositoryURL.mockResolvedValue('https://github.com/example/dbx')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountAndOpenVersions() {
  const wrapper = mount(DbxView, { attachTo: document.body })
  await flushMicrotasks()
  const tabs = wrapper.findAll('.main-tab-btn')
  await tabs[1].trigger('click')
  await flushMicrotasks()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  useToast().clearToast()
  document.body.innerHTML = ''
})

describe('DbxView 账本漂移警示（三处同源）', () => {
  it('drifted：状态区 warn 横幅（含逐字口径与后端附言）+ 状态灯 warn 档 + 页头徽标', async () => {
    stubDefaults({
      state: 'running',
      version: '0.9.30',
      pid: 4242,
      drifted: true,
      driftNote: 'SHA256 与下载账目不符',
      startedAt: new Date().toISOString(),
    })
    const wrapper = await mountAndOpenVersions()
    const banner = wrapper.find('.banner')
    expect(banner.classes()).toContain('banner-warn')
    expect(banner.text()).toContain('二进制已被应用自更新替换，与下载账目不一致')
    expect(banner.text()).toContain('SHA256 与下载账目不符')
    expect(wrapper.find('.status-light').classes()).toContain('warn')
    expect(wrapper.find('.drift-badge').exists()).toBe(true)
    expect(wrapper.find('.drift-badge').text()).toBe('账本漂移')
    wrapper.unmount()
  })

  it('无漂移对照：running 走 ok 横幅、徽标缺席；remote 行「便携」形态 chip 经共享件自动出词', async () => {
    stubDefaults({ state: 'running', version: '0.9.30', pid: 4242, startedAt: new Date().toISOString() })
    const wrapper = await mountAndOpenVersions()
    expect(wrapper.find('.banner').classes()).toContain('banner-ok')
    expect(wrapper.find('.drift-badge').exists()).toBe(false)
    // N13：后端行带 form='portable'，releaseFormWord 出「便携」——adapter 零干预透传
    expect(wrapper.find('.tbl').text()).toContain('便携')
    wrapper.unmount()
  })

  it('metaHints 五条如实披露进版本页 meta-info（漂移徽章语义/日更/stable 建议/minisign 不验在列）', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = await mountAndOpenVersions()
    const hints = wrapper.findAll('.meta-info .hint-dim')
    expect(hints).toHaveLength(5)
    const joined = hints.map((h) => h.text()).join('\n')
    expect(joined).toContain('账本漂移')
    expect(joined).toContain('DBX_DATA_DIR')
    expect(joined).toContain('更新频繁，按需停留稳定版即可')
    expect(joined).toContain('minisign')
    expect(joined).toContain('托管备份')
    wrapper.unmount()
  })
})

describe('DbxView 卸载预告与联动开关', () => {
  it('末版卸载预告复用：确认文案含"最后一个版本"与数据目录不随删，确认后走 RemoveVersion + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installed0930])
    const wrapper = await mountAndOpenVersions()
    const { confirmState, settleConfirm } = useConfirm()
    svc.RemoveVersion.mockResolvedValue(undefined)

    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 DBX 0.9.30？')
    expect(confirmState.options.description).toContain('这是最后一个版本，卸载后将回到未安装状态')
    expect(confirmState.options.description).toContain('DBX_DATA_DIR')
    settleConfirm(true)
    await flushPromises()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('0.9.30')
    expect(useToast().toastMsg.value).toBe('已卸载 0.9.30')
    wrapper.unmount()
  })

  it('多版本对照：确认文案不现"最后一个版本"预告', async () => {
    stubDefaults({ state: 'stopped' }, [installed0930, { ...installed0930, version: '0.9.29' }])
    const wrapper = await mountAndOpenVersions()
    const { confirmState, settleConfirm } = useConfirm()

    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall.trigger('click')
    await flushMicrotasks()
    expect(confirmState.options.description).not.toContain('最后一个版本')
    settleConfirm(false)
    await flushMicrotasks()
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('followOnExit 开关：勾选置真送 SetFollowOnExit(true)，回执讲清强杀兜底，注记点名「托管备份」', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = mount(DbxView)
    await flushMicrotasks()

    expect(wrapper.find('.extras-card .toggle-label .hint-dim').text()).toContain('托管备份')
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    await wrapper.find('.extras-card .toggle-label input').setValue(true)
    await flushPromises()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(true)
    expect(useToast().toastMsg.value).toContain('Hanxi 退出时一并关闭')
    expect(useToast().toastMsg.value).toContain('强杀兜底')
    wrapper.unmount()
  })
})
