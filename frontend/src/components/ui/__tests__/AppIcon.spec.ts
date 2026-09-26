// AppIcon 特征测试（§8 图标纪律基建，阶段1）：注册表完整性与无障碍语义。
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AppIcon from '../AppIcon.vue'
import { ICON_NAMES, ICON_PATHS, type IconName } from '../../../constants/icons'
import { APP_ICON_GENERIC_URL, APP_ICON_IDS, appIconUrl } from '../../../constants/appIcons'

// 运行期提取通道替身：直接驱动"已提取/未提取"两态，不起真实 wails 桥。
const runtimeStore = vi.hoisted(() => ({ urls: {} as Record<string, string>, ensured: [] as string[] }))
vi.mock('../../../constants/runtimeIcons', () => ({
  runtimeIconUrl: (id: string) => runtimeStore.urls[id],
  runtimeIconReady: (id: string) => runtimeStore.urls[id] !== undefined,
  runtimeIconFailed: (id: string) => false,
  ensureRuntimeIcon: (id: string) => {
    runtimeStore.ensured.push(id)
    return Promise.resolve()
  },
  __resetRuntimeIconsForTest: () => {},
}))

describe('AppIcon 注册表', () => {
  it('每个图标名均有非空 path 列表，d 以合法命令字母开头', () => {
    for (const name of ICON_NAMES) {
      const paths = ICON_PATHS[name]
      expect(paths.length, name).toBeGreaterThan(0)
      for (const d of paths) expect(d, `${name}:${d.slice(0, 12)}`).toMatch(/^[MmLlHhVvAaCcSsQqTt]/)
    }
  })

  it('键集合与 ICON_NAMES 一致（防散写漏登记）', () => {
    expect(new Set(Object.keys(ICON_PATHS))).toEqual(new Set(ICON_NAMES))
  })
})

describe('AppIcon 渲染', () => {
  it('缺省为装饰性：aria-hidden 且 path 数与注册表一致', () => {
    const w = mount(AppIcon, { props: { name: 'bell' as IconName } })
    const svg = w.find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('role')).toBeUndefined()
    expect(svg.findAll('path')).toHaveLength(ICON_PATHS.bell.length)
  })

  it('传 label 升格为语义图标：role=img + aria-label，不再隐藏', () => {
    const w = mount(AppIcon, { props: { name: 'gear' as IconName, label: '设置' } })
    const svg = w.find('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('设置')
    expect(svg.attributes('aria-hidden')).toBeUndefined()
  })

  it('size 数字转 px，字符串原样', () => {
    expect(mount(AppIcon, { props: { name: 'sun' as IconName, size: 16 } }).find('svg').attributes('style')).toContain('width: 16px')
    expect(mount(AppIcon, { props: { name: 'sun' as IconName, size: '1.2em' } }).find('svg').attributes('style')).toContain('width: 1.2em')
  })

  it('stroke 走 currentColor（双主题自动随文字色）', () => {
    const svg = mount(AppIcon, { props: { name: 'moon' as IconName } }).find('svg')
    expect(svg.attributes('stroke')).toBe('currentColor')
    expect(svg.attributes('fill')).toBe('none')
  })
})

describe('AppIcon 真图标第二来源（N27 批 A）', () => {
  it('注册表：通用徽标存在，真图标入库件（批 A 五枚 + 批 B-2 扩量十二枚 + N27 尾巴放行二枚 + 开源上游仓库取材四枚 + 尾巡补录三枚）在场', () => {
    expect(APP_ICON_GENERIC_URL).toBeTruthy()
    for (const id of [
      'ccswitch', 'keyviz', 'everything', 'snipaste', 'markeron',
      'bcu', 'douzy', 'flclash', 'mangodisk', 'papertodo', 'paseo',
      'piclite', 'quicklook', 'rufus', 'subnetdesk', 'translucenttb', 'windterm',
      'litemonitor', 'guoheview',
      'rustdesk', 'bili23', 'termora', 'nanazip',
      'ddnsgo', 'frpc', 'eartrumpet',
    ]) {
      expect(APP_ICON_IDS, id).toContain(id)
    }
  })

  it('appIconUrl 命中真图标；缺图回落通用徽标且永不空串', () => {
    expect(appIconUrl('ccswitch')).toBeTruthy()
    expect(appIconUrl('no-such-module')).toBe(APP_ICON_GENERIC_URL)
  })

  it('`app:` 名渲染位图 <img>，装饰态 aria-hidden、label 升格 img 语义', () => {
    const w = mount(AppIcon, { props: { name: 'app:ccswitch' } })
    const img = w.find('img')
    expect(img.exists()).toBe(true)
    expect(w.find('svg').exists()).toBe(false)
    expect(img.attributes('src')).toBeTruthy()
    expect(img.attributes('aria-hidden')).toBe('true')
    const named = mount(AppIcon, { props: { name: 'app:keyviz', label: 'Keyviz' } })
    expect(named.find('img').attributes('role')).toBe('img')
    expect(named.find('img').attributes('aria-label')).toBe('Keyviz')
  })

  it('`i:` 矢量分支零漂移：仍渲染 <svg>', () => {
    const w = mount(AppIcon, { props: { name: 'bell' as IconName } })
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('svg').exists()).toBe(true)
  })
})

describe('AppIcon 真图标第三来源：rt 运行期提取（N27 红线尾巴）', () => {
  beforeEach(() => {
    runtimeStore.urls = {}
    runtimeStore.ensured = []
  })

  it('提取未就绪：同步渲染 `|` 后声明的回落矢量，与旧 i:gauge 特征全等（观感零损）', () => {
    const w = mount(AppIcon, { props: { name: 'rt:rammap|i:gauge' } })
    expect(w.find('img').exists()).toBe(false)
    const svg = w.find('svg')
    expect(svg.exists()).toBe(true)
    expect(svg.findAll('path')).toHaveLength(ICON_PATHS.gauge.length)
    expect(svg.findAll('path')[0].attributes('d')).toBe(ICON_PATHS.gauge[0])
    // 挂载即幂等发起提取，且只解析出 moduleId（不带 fallback 段）
    expect(runtimeStore.ensured).toEqual(['rammap'])
  })

  it('提取成功：渲染 <img> 位图，src 为通道 data URL', () => {
    runtimeStore.urls.rammap = 'data:image/png;base64,AAAA'
    const w = mount(AppIcon, { props: { name: 'rt:rammap|i:gauge' } })
    const img = w.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('data:image/png;base64,AAAA')
    expect(w.find('svg').exists()).toBe(false)
  })

  it('fallback 缺失/未登记名：回落 box 矢量，绝不空白断链', () => {
    const bare = mount(AppIcon, { props: { name: 'rt:recordly' } })
    expect(bare.findAll('path')).toHaveLength(ICON_PATHS.box.length)
    const bogus = mount(AppIcon, { props: { name: 'rt:vscode|i:not-registered' } })
    expect(bogus.findAll('path')).toHaveLength(ICON_PATHS.box.length)
  })

  it('同串换组件互不干扰：app: 轨与 i: 轨行为零漂移', () => {
    const app = mount(AppIcon, { props: { name: 'app:ccswitch' } })
    expect(app.find('img').attributes('src')).toBe(appIconUrl('ccswitch'))
    const vec = mount(AppIcon, { props: { name: 'bell' as IconName } })
    expect(vec.findAll('path')).toHaveLength(ICON_PATHS.bell.length)
    expect(runtimeStore.ensured).toEqual([]) // 非 rt 名绝不打提取通道
  })
})
