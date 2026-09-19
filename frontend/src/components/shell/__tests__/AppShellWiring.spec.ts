// 接线特征测试：Phase 6 外壳组件化后，App.vue 与 AppSidebar 的 props/emits 编排锁。
// mount 真实 App（bindings 与 Events 按既有范式 vi.mock），验证三条上抛通道逐字生效：
// 侧栏点击导航 → navigateTo（门禁与高亮更新）、通知入口 → toggleDrawer（抽屉开合）。
// 主题切换与日志/关于入口已迁设置页（SettingsView.spec 覆盖），App 侧无 cycle-theme 接线。
// vi.mock 相对路径范式同 views/__tests__。
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
  // Wave 2 模块中心投影面（useModuleCatalog 冷加载消费）
  ListCatalog: vi.fn(),
  ListModuleStates: vi.fn(),
  // Wave 4 操作观察面（OperationBanner/useOperations 冷加载消费）
  ListOperations: vi.fn(),
  DismissResumable: vi.fn(),
}))
const notifySvc = vi.hoisted(() => ({ GetHistory: vi.fn() }))
const historySvc = vi.hoisted(() => ({ List: vi.fn() }))
const runtime = vi.hoisted(() => ({ On: vi.fn(() => vi.fn()) }))

vi.mock('@wailsio/runtime', () => ({ Events: runtime }))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('../../../../bindings/hanxi/internal/app/appservice.js', () => ({ EnsureModuleActive: vi.fn() }))
vi.mock('../../../../bindings/hanxi/internal/notify', () => ({ NotificationService: notifySvc }))
vi.mock('../../../../bindings/hanxi/internal/history/historyservice', () => ({ List: historySvc.List }))

async function mountApp() {
  appSvc.GetNavs.mockResolvedValue([
    { id: 'memo', route: '/ext/memo', title: '随手记', icon: '📝', section: 'ext', order: 0 },
  ])
  appSvc.ListModules.mockResolvedValue([])
  appSvc.ListCatalog.mockResolvedValue([])
  appSvc.ListModuleStates.mockResolvedValue([])
  appSvc.ListOperations.mockResolvedValue([])
  appSvc.DismissResumable.mockResolvedValue(null)
  appSvc.GetAppInfo.mockResolvedValue({ version: '0.0.0', name: 'Hanxi' })
  appSvc.SetTheme.mockResolvedValue(null)
  appSvc.SetWindowDarkMode.mockResolvedValue(null)
  notifySvc.GetHistory.mockResolvedValue([])
  historySvc.List.mockResolvedValue([])
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
  // 双栏改造适配：核心页入口已从单栏 nav 列表收进一级图标轨道（rail 收敛后仅剩设置），
  // 断言由「.nav-item.active .nav-text 文案」等价迁移到「rail 按钮 active 类 + title」。
  it('侧栏渲染在 .layout 骨架内，active 高亮随 App 路由状态', async () => {
    const w = await mountApp()
    expect(w.find('.layout > aside.sidebar').exists()).toBe(true)
    expect(w.find('.rail-home.active').attributes('title')).toBe('首页')
    w.unmount()
  })

  it('点击侧栏导航项：emit navigate 直达 App.navigateTo，高亮迁移', async () => {
    const w = await mountApp()
    const settingsBtn = w.findAll('.rail-core').find((b) => b.attributes('title') === '设置')!
    await settingsBtn.trigger('click')
    expect(w.find('.rail-core.active').attributes('title')).toBe('设置')
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

  it('rail 不再承载主题钮与日志/关于入口（已迁设置页）', async () => {
    const w = await mountApp()
    expect(w.find('.theme-toggle').exists()).toBe(false)
    const titles = w.findAll('.rail-core').map((b) => b.attributes('title'))
    expect(titles).toEqual(['设置'])
    w.unmount()
  })

  // 模块中心接线锁（Wave 2 起页面已是真实目录投影）：核心页无 moduleId →
  // navigateTo 不过 EnsureModuleActive 门禁、refreshNavs 的"禁用扩展弹回首页"
  // 豁免清单（navigation.CORE_ROUTES）含 /modules，即便后端 navs 里没有该 route
  // 也停在模块中心不弹回（高亮留在模块中心钮即为不弹回）。
  it('rail 点模块中心：直达 /modules 目录投影，navs 未登记不触发弹回', async () => {
    const w = await mountApp()
    await w.find('.rail-modules').trigger('click')
    // 换页落定：异步组件解析（微任务）+ Transition 帧驱动（happy-dom rAF 走真定时器）。
    // 全量并发跑时 worker 抢占会让双帧 out-in 超过固定预算,故用 waitFor 轮询到条件成立
    // （对负载不敏感,单跑/全量/CI 一致),而非"让出 N 轮固定 timer"。
    await vi.waitFor(() => {
      expect(w.find('.rail-modules.active').exists()).toBe(true)
      expect(w.find('.rail-home.active').exists()).toBe(false)
      expect(w.find('.content-area').text()).toContain('完整模块目录与安装管理')
    }, { timeout: 2000, interval: 20 })
    w.unmount()
  })
})
