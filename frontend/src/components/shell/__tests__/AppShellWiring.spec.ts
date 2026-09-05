// 接线特征测试：Phase 6 外壳组件化后，App.vue 与 AppSidebar 的 props/emits 编排锁。
// mount 真实 App（bindings 与 Events 按既有范式 vi.mock），验证三条上抛通道逐字生效：
// 侧栏点击导航 → navigateTo（门禁与高亮更新）、通知入口 → toggleDrawer（抽屉开合）、
// 主题钮 → cycleThemeMode（三态文案循环）。vi.mock 相对路径范式同 views/__tests__。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../../../App.vue'
import { useNotification } from '../../../composables/useNotification'
import { useTheme } from '../../../composables/useTheme'

const appSvc = vi.hoisted(() => ({
  GetNavs: vi.fn(),
  ListModules: vi.fn(),
  GetAppInfo: vi.fn(),
  SetModuleEnabled: vi.fn(),
  GetTheme: vi.fn(),
  SetTheme: vi.fn(),
  SetWindowDarkMode: vi.fn(),
}))
const notifySvc = vi.hoisted(() => ({ GetHistory: vi.fn() }))
const runtime = vi.hoisted(() => ({ On: vi.fn(() => vi.fn()) }))

vi.mock('@wailsio/runtime', () => ({ Events: runtime }))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('../../../../bindings/hanxi/internal/app/appservice.js', () => ({ EnsureModuleActive: vi.fn() }))
vi.mock('../../../../bindings/hanxi/internal/notify', () => ({ NotificationService: notifySvc }))

async function mountApp() {
  appSvc.GetNavs.mockResolvedValue([
    { id: 'memo', route: '/ext/memo', title: '随手记', icon: '📝', section: 'ext', order: 0 },
  ])
  appSvc.ListModules.mockResolvedValue([])
  appSvc.GetAppInfo.mockResolvedValue({ version: '0.0.0', name: 'Hanxi' })
  appSvc.SetTheme.mockResolvedValue(null)
  appSvc.SetWindowDarkMode.mockResolvedValue(null)
  notifySvc.GetHistory.mockResolvedValue([])
  const w = mount(App, { attachTo: document.body })
  await flushPromises()
  return w
}

afterEach(() => {
  // 抽屉与主题为模块级单例，测试间复位防串扰（非 light 才回拨，避免无谓后端写）
  useNotification().closeDrawer()
  const { themeMode, setThemeMode } = useTheme()
  if (themeMode.value !== 'light') setThemeMode('light')
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('App.vue ↔ AppSidebar 接线', () => {
  it('侧栏渲染在 .layout 骨架内，active 高亮随 App 路由状态', async () => {
    const w = await mountApp()
    expect(w.find('.layout > aside.sidebar').exists()).toBe(true)
    expect(w.find('.nav-item.active .nav-text').text()).toBe('首页')
    w.unmount()
  })

  it('点击侧栏导航项：emit navigate 直达 App.navigateTo，高亮迁移', async () => {
    const w = await mountApp()
    const logsBtn = w.findAll('.nav-item').find((b) => b.text().includes('日志'))!
    await logsBtn.trigger('click')
    expect(w.find('.nav-item.active .nav-text').text()).toBe('日志')
    w.unmount()
  })

  it('点击通知中心入口：emit toggle-drawer 开合全局抽屉', async () => {
    const w = await mountApp()
    const btn = w.find('.notif-nav-btn')
    expect(document.querySelector('.notification-drawer')).toBeNull()
    await btn.trigger('click')
    expect(document.querySelector('.notification-drawer')).not.toBeNull()
    await btn.trigger('click')
    expect(document.querySelector('.notification-drawer')).toBeNull()
    w.unmount()
  })

  it('点击主题钮：emit cycle-theme 驱动 useTheme 三态循环（light → dark → system → light）', async () => {
    const w = await mountApp()
    const themeBtn = w.find('.theme-toggle')
    expect(themeBtn.find('.nav-text').text()).toBe('浅色主题')
    await themeBtn.trigger('click')
    expect(themeBtn.find('.nav-text').text()).toBe('深色主题')
    await themeBtn.trigger('click')
    expect(themeBtn.find('.nav-text').text()).toBe('跟随系统')
    await themeBtn.trigger('click')
    expect(themeBtn.find('.nav-text').text()).toBe('浅色主题')
    w.unmount()
  })
})
