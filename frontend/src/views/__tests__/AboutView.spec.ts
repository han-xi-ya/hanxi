// 特征测试：关于页（信息面板渲染/运行模式文案/模块清单/加载失败态）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AboutView from '../AboutView.vue'

const appSvc = vi.hoisted(() => ({
  GetAppInfo: vi.fn(),
  ListModules: vi.fn(),
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

const INFO = {
  name: 'Hanxi', version: '0.3.0', goos: 'windows', goarch: 'amd64',
  mode: 'portable', baseDir: 'E:\\hanxi\\data', description: '开源工具工作台',
}
const MODULES = [
  { id: 'frpc', name: 'frpc 穿透', description: '多实例', version: '1.0', enabled: true, initialized: true },
  { id: 'memo', name: '随手记', description: '备忘', version: '1.0', enabled: false, initialized: false },
]

async function mountWith(info: unknown, mods: unknown) {
  appSvc.GetAppInfo.mockResolvedValue(info)
  appSvc.ListModules.mockResolvedValue(mods)
  const w = mount(AboutView)
  await flushPromises()
  return w
}

afterEach(() => vi.restoreAllMocks())

describe('AboutView', () => {
  it('渲染版本/平台/便携模式/数据目录与模块清单', async () => {
    const w = await mountWith(INFO, MODULES)
    expect(w.find('.info-panel').text()).toContain('0.3.0')
    expect(w.find('.info-panel').text()).toContain('windows/amd64')
    expect(w.find('.info-panel').text()).toContain('便携模式')
    expect(w.findAll('.module-row')).toHaveLength(2)
    expect(w.find('.module-count').text()).toBe('2 项')
    expect(w.findAll('.status-badge')[0].text()).toBe('已启用')
    expect(w.findAll('.status-badge')[1].text()).toBe('未启用')
  })

  it('标准模式口径 + 空模块占位', async () => {
    const w = await mountWith({ ...INFO, mode: 'standard' }, [])
    expect(w.find('.info-panel').text()).toContain('标准模式')
    expect(w.find('.state-panel').text()).toBe('暂无已注册工具。')
  })

  it('加载失败：错误面板显示归一后的错误文案', async () => {
    appSvc.GetAppInfo.mockRejectedValue(new Error('绑定未就绪'))
    appSvc.ListModules.mockRejectedValue(new Error('绑定未就绪'))
    const w = mount(AboutView)
    await flushPromises()
    expect(w.find('.error-panel').exists()).toBe(true)
    expect(w.find('.error-panel').text()).toContain('绑定未就绪')
  })
})
