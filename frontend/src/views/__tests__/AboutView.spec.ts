// 特征测试：关于页（信息面板渲染/数据目录/模块清单/加载失败态）。
// F6 后"运行模式"文案退役，关于页不再呈现 mode 字样。
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
  mode: 'sibling', baseDir: 'E:\\hanxi\\hanxidata', description: '开源工具工作台',
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
  it('渲染版本/平台/数据目录与模块清单，不再出现运行模式字样', async () => {
    const w = await mountWith(INFO, MODULES)
    expect(w.find('.info-panel').text()).toContain('0.3.0')
    expect(w.find('.info-panel').text()).toContain('windows/amd64')
    expect(w.find('.info-panel').text()).toContain('E:\\hanxi\\hanxidata')
    expect(w.find('.info-panel').text()).not.toMatch(/便携模式|标准模式|运行模式/)
    expect(w.findAll('.module-row')).toHaveLength(2)
    expect(w.find('.module-count').text()).toBe('2 项')
    expect(w.findAll('.status-badge')[0].text()).toBe('已启用')
    expect(w.findAll('.status-badge')[1].text()).toBe('未启用')
  })

  it('绑定来源口径（mode=bound）同样不渲染模式字样 + 空模块占位', async () => {
    const w = await mountWith({ ...INFO, mode: 'bound' }, [])
    expect(w.find('.info-panel').text()).not.toMatch(/便携模式|标准模式|运行模式/)
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

  // N36 尾账：OFL「随副本分发」义务 App 内侧（?raw 内联全文进产物，dist 静态拷贝为另一保险）
  it('字体许可区渲染三份 OFL 全文折叠段，摘要含字族与许可名', async () => {
    const w = await mountWith(INFO, MODULES)
    const blocks = w.findAll('.license-details')
    expect(blocks).toHaveLength(3)
    for (const b of blocks) {
      expect(b.find('summary').text()).toContain('SIL OFL 1.1')
      expect(b.find('.license-text').text()).toMatch(/SIL OPEN FONT LICENSE/i)
      expect(b.find('.license-text').text().length).toBeGreaterThan(1000)
    }
    expect(blocks.map((b) => b.find('summary').text()).join('\n')).toContain('LXGW WenKai GB Screen')
    expect(blocks.map((b) => b.find('summary').text()).join('\n')).toContain('JetBrains Mono')
  })

  it('字体许可为静态合规内容：加载失败态下同样在位', async () => {
    appSvc.GetAppInfo.mockRejectedValue(new Error('绑定未就绪'))
    appSvc.ListModules.mockRejectedValue(new Error('绑定未就绪'))
    const w = mount(AboutView)
    await flushPromises()
    expect(w.find('.error-panel').exists()).toBe(true)
    expect(w.findAll('.license-details')).toHaveLength(3)
  })
})
