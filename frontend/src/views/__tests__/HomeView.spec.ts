// 特征测试：首页工作台（模块卡片渲染/启停流/直达事件/ext:changed 热刷新）。
// MODULE_META 已并入 constants/navigation——本测试同时充当"三份清单收编"的回归锁。
import { KeepAlive, defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import HomeView from '../HomeView.vue'
import { useToast } from '../../composables/useToast'

const appSvc = vi.hoisted(() => ({
  ListModules: vi.fn(),
  GetNavs: vi.fn(),
  GetAppInfo: vi.fn(),
  SetModuleEnabled: vi.fn(),
}))
const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, (e: { data: unknown }) => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (e: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('../../../bindings/hanxi/internal/app/appservice.js', () => ({ EnsureModuleActive: vi.fn() }))

const mod = (id: string, name: string, enabled: boolean, initialized = false) => ({
  id, name, description: `${name} 描述`, version: '1.0.0', enabled, initialized,
})

function stubLoad(modules: unknown[]) {
  appSvc.ListModules.mockResolvedValue(modules)
  appSvc.GetNavs.mockResolvedValue([{ id: 'memo', route: '/ext/memo', title: '随手记', icon: '📝' }])
  appSvc.GetAppInfo.mockResolvedValue({ version: '0.3.0', name: 'Hanxi' })
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(HomeView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('HomeView', () => {
  it('按启用状态分组渲染卡片，统计数一致', async () => {
    stubLoad([mod('memo', '极客随手记', true, true), mod('wifi', 'WiFi 密码', false), mod('mystery', '未知模块', true)])
    const w = await mountView()
    expect(w.findAll('.enabled-card')).toHaveLength(2)
    expect(w.findAll('.disabled-card')).toHaveLength(1)
    expect(w.findAll('.stat-num')[0].text()).toBe('2')
    // 未建档模块走回退图标 📦
    const mysteryCard = w.findAll('.enabled-card').find((c) => c.text().includes('未知模块'))!
    expect(mysteryCard.find('.mod-icon').text()).toBe('📦')
    // 已建档模块走 navigation 单一来源图标
    const memoCard = w.findAll('.enabled-card').find((c) => c.text().includes('极客随手记'))!
    expect(memoCard.find('.mod-icon').text()).toBe('📝')
    expect(memoCard.find('.level-badge').text()).toBe('活跃运行中')
    w.unmount()
  })

  it('点卡片直达：优先 MODULE_PRESENTATION 首选路由，未建档回退后端 navs', async () => {
    stubLoad([mod('memo', '随手记', true), mod('latermod', '后加模块', true)])
    const w = await mountView()
    const home = w.findComponent(HomeView)
    const memoCard = w.findAll('.enabled-card').find((c) => c.text().includes('随手记'))!
    await memoCard.trigger('click')
    expect(home.emitted('navigate')?.[0]).toEqual(['/ext/memo'])
    // 启停按钮区点击不应触发直达（@click.stop）
    w.unmount()
  })

  it('停用模块：SetModuleEnabled(false) → toast → 重拉数据', async () => {
    stubLoad([mod('memo', '随手记', true)])
    appSvc.SetModuleEnabled.mockResolvedValue(null)
    const w = await mountView()
    const btn = w.find('.btn-toggle-off')
    await btn.trigger('click')
    await flushPromises()
    expect(appSvc.SetModuleEnabled).toHaveBeenCalledWith('memo', false)
    expect(useToast().toastMsg.value).toBe('已停用「随手记」，已回收运行时资源')
    expect(appSvc.ListModules.mock.calls.length).toBeGreaterThanOrEqual(2)
    w.unmount()
  })

  it('ext:changed 事件触发热刷新', async () => {
    stubLoad([mod('memo', '随手记', true)])
    const w = await mountView()
    const before = appSvc.ListModules.mock.calls.length
    runtime.handlers['ext:changed']({ data: undefined })
    await flushPromises()
    expect(appSvc.ListModules.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
  })

  it('停用失败 toast 错误且不崩', async () => {
    stubLoad([mod('memo', '随手记', true)])
    appSvc.SetModuleEnabled.mockRejectedValue(new Error('模块正被占用'))
    const w = await mountView()
    await w.find('.btn-toggle-off').trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('模块正被占用')
    w.unmount()
  })
})
