// N40 界面字体档特征测试：外观分区「界面字体」三分段选择器 → documentElement
// data-font 落值 → 后端 SetFont 持久化/GetFont 校正回读（含失败回落默认）。
// 独立成篇不并入 SettingsSections.spec.ts：该文件正被存储/系统分区改造路触碰，避免串路编辑踩踏。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { rpc, emitSpy } = vi.hoisted(() => ({
  rpc: {
    GetTheme: vi.fn(),
    SetTheme: vi.fn(),
    GetAccent: vi.fn(),
    SetAccent: vi.fn(),
    GetFont: vi.fn(),
    SetFont: vi.fn(),
    SetWindowDarkMode: vi.fn(),
  },
  emitSpy: vi.fn(),
}))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: rpc }))
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (_name: string, _cb: unknown) => vi.fn(),
    Emit: (...args: unknown[]) => emitSpy(...args),
  },
}))

/** 干净环境重装 useTheme 单例 + initTheme（后端值由参数定），再挂起外观分区。 */
async function mountSection(backendFont: string) {
  localStorage.clear()
  vi.resetModules()
  emitSpy.mockClear()
  rpc.GetTheme.mockResolvedValue('light')
  rpc.GetAccent.mockResolvedValue('teal')
  rpc.GetFont.mockReset().mockResolvedValue(backendFont)
  rpc.SetFont.mockReset().mockResolvedValue(undefined)
  rpc.SetWindowDarkMode.mockResolvedValue(undefined)
  const theme = await import('../../../composables/useTheme')
  await theme.initTheme()
  const { default: ThemeSection } = await import('../ThemeSection.vue')
  const w = mount(ThemeSection)
  await flushPromises()
  return { w, theme }
}

const fontSegs = (w: ReturnType<typeof mount>) => w.findAll('.seg-font .theme-seg-btn')

beforeEach(() => {
  rpc.SetTheme.mockResolvedValue(undefined)
  rpc.SetAccent.mockResolvedValue(undefined)
})

afterEach(() => {
  localStorage.clear()
  for (const key of ['theme', 'accent', 'font']) {
    document.documentElement.removeAttribute(`data-${key}`)
  }
})

describe('外观分区·界面字体（N40）', () => {
  it('三分段存在且默认档 active：initTheme 后 data-font 落值', async () => {
    const { w } = await mountSection('kai')
    const segs = fontSegs(w)
    expect(segs.map((s) => s.text())).toEqual(['默认·文楷', '系统朴素', '等宽极客'])
    expect(segs[0].classes()).toContain('active')
    expect(segs[0].attributes('aria-checked')).toBe('true')
    expect(document.documentElement.dataset.font).toBe('kai')
  })

  it('点击切换：data-font 即时落值 + 缓存镜像 + SetFont 持久化 + 三轴广播', async () => {
    const { w } = await mountSection('kai')
    await fontSegs(w)[2].trigger('click')
    await flushPromises()
    expect(document.documentElement.dataset.font).toBe('mono')
    expect(localStorage.getItem('hanxi.font')).toBe('mono')
    expect(rpc.SetFont).toHaveBeenCalledWith('mono')
    expect(emitSpy).toHaveBeenCalledWith('theme:changed', { mode: 'light', accent: 'teal', font: 'mono' })
    expect(fontSegs(w)[2].classes()).toContain('active') // 选择器回显与 DOM 同源
    // hint 行文案跟随当前生效档
    expect(w.find('.setting-row:nth-child(3) .setting-desc').text()).toContain('JetBrains Mono')
  })

  it('持久化读回环：后端值为 plain 时挂载即回显该档（重启保持语义）', async () => {
    const { w, theme } = await mountSection('plain')
    expect(theme.useTheme().font.value).toBe('plain')
    expect(fontSegs(w)[1].classes()).toContain('active')
    expect(document.documentElement.dataset.font).toBe('plain')
  })

  it('后端读取失败/垃圾值：回落默认文楷档，不留空属性', async () => {
    localStorage.setItem('hanxi.font', 'hotpink') // 脏缓存同样被合法性闸门拦下
    const { w, theme } = await mountSection('serif')
    expect(theme.useTheme().font.value).toBe('kai')
    expect(document.documentElement.dataset.font).toBe('kai')
    expect(fontSegs(w)[0].classes()).toContain('active')
    expect(rpc.SetFont).not.toHaveBeenCalled() // 回落不是用户动作，不回写后端
  })
})
