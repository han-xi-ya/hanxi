// 特征测试：设置页主干——常规偏好读写回环、外观主题分段（联动后端 SetTheme + DOM data-theme）、
// 托盘菜单加载渲染。深层托盘编辑交互由后续回归覆盖，此处锁主链路。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SettingsView from '../SettingsView.vue'
import { useToast } from '../../composables/useToast'
import { useTheme } from '../../composables/useTheme'

const appSvc = vi.hoisted(() => ({
  GetAppInfo: vi.fn(),
  GetGeneralSettings: vi.fn(),
  SetGeneralSettings: vi.fn(),
  ListTrayMenuOptions: vi.fn(),
  GetTrayMenu: vi.fn(),
  SetTrayMenu: vi.fn(),
  GetTheme: vi.fn(),
  SetTheme: vi.fn().mockResolvedValue(undefined),
  SetWindowDarkMode: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))

function stubs() {
  appSvc.GetAppInfo.mockResolvedValue({ name: 'Hanxi', version: '0.3.0', mode: 'portable', baseDir: 'D:\\hx', logsDir: 'D:\\hx\\logs' })
  appSvc.GetGeneralSettings.mockResolvedValue({ autoStart: false, minimizeToTray: true, logRetainDays: 7 })
  appSvc.SetGeneralSettings.mockResolvedValue(undefined)
  appSvc.ListTrayMenuOptions.mockResolvedValue([{ type: 'command', ref: 'frpc/start', label: '启动 frpc', moduleName: 'frpc' }])
  appSvc.GetTrayMenu.mockResolvedValue([{ type: 'route', ref: '/logs', path: '', args: '', label: '日志', enabled: true }])
  appSvc.GetTheme.mockResolvedValue('light')
}

async function mountView() {
  const w = mount(SettingsView)
  await flushPromises()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
  document.documentElement.removeAttribute('data-theme')
})

describe('SettingsView', () => {
  it('加载常规偏好回填控件', async () => {
    stubs()
    const w = await mountView()
    const switches = w.findAll('.switch')
    expect((switches[0].element as HTMLInputElement).checked).toBe(false) // autoStart
    expect((switches[1].element as HTMLInputElement).checked).toBe(true) // minimizeToTray
    expect((w.find('.input-number').element as HTMLInputElement).value).toBe('7')
    expect(w.text()).toContain('托盘右键菜单')
  })

  it('切换开机自启：SetGeneralSettings 携带完整三字段', async () => {
    stubs()
    const w = await mountView()
    const autoStart = w.findAll('.switch')[0]
    await autoStart.setValue(true)
    await flushPromises()
    expect(appSvc.SetGeneralSettings).toHaveBeenCalledWith({ autoStart: true, minimizeToTray: true, logRetainDays: 7 })
    expect(useToast().toastMsg.value).toBe('常规偏好设置已更新')
  })

  it('保存失败回滚：重拉后端值', async () => {
    stubs()
    appSvc.SetGeneralSettings.mockRejectedValue(new Error('注册表写入失败'))
    const w = await mountView()
    const callsBefore = appSvc.GetGeneralSettings.mock.calls.length
    await w.findAll('.switch')[0].setValue(true)
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('注册表写入失败')
    expect(appSvc.GetGeneralSettings.mock.calls.length).toBeGreaterThan(callsBefore)
  })

  it('外观三分段存在且点击深色：后端持久化 + DOM/标题栏联动', async () => {
    stubs()
    const { themeMode, resolvedTheme } = useTheme()
    const w = await mountView()
    const segs = w.findAll('.theme-seg-btn')
    expect(segs).toHaveLength(3)
    expect(segs[2].text()).toBe('深色')
    await segs[2].trigger('click')
    await flushPromises()
    expect(themeMode.value).toBe('dark')
    expect(resolvedTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(appSvc.SetTheme).toHaveBeenCalledWith('dark')
    expect(appSvc.SetWindowDarkMode).toHaveBeenCalledWith(true)
  })
})
