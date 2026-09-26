// useTheme 跨窗广播（N39）：发起窗"改+广播"、收端窗"回写不再广播"（无回环的
// 结构保证）、启动校正不广播（防每窗起手一场事件风暴）。模块级单例 + initTheme
// 一次性守卫，逐用例 resetModules 重装取干净实例。
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const handlers = new Map<string, (ev: unknown) => void>()
const emitSpy = vi.fn()
const rpc = {
  GetTheme: vi.fn().mockResolvedValue('light'),
  SetTheme: vi.fn().mockResolvedValue(undefined),
  GetAccent: vi.fn().mockResolvedValue('teal'),
  SetAccent: vi.fn().mockResolvedValue(undefined),
  GetFont: vi.fn().mockResolvedValue('kai'),
  SetFont: vi.fn().mockResolvedValue(undefined),
  SetWindowDarkMode: vi.fn().mockResolvedValue(undefined),
}

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (ev: unknown) => void) => {
      handlers.set(name, cb)
      return () => handlers.delete(name)
    },
    Emit: (...args: unknown[]) => emitSpy(...args),
  },
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: rpc }))

async function loadFresh() {
  vi.resetModules()
  emitSpy.mockClear()
  rpc.GetTheme.mockResolvedValue('light')
  rpc.SetTheme.mockClear()
  rpc.GetAccent.mockResolvedValue('teal')
  rpc.SetAccent.mockClear()
  rpc.GetFont.mockResolvedValue('kai')
  rpc.SetFont.mockClear()
  localStorage.clear()
  const mod = await import('../useTheme')
  await mod.initTheme()
  return mod
}

function fire(name: string, data: unknown) {
  handlers.get(name)?.({ data })
}

describe('useTheme 跨窗广播（N39）', () => {
  beforeEach(() => {
    handlers.clear()
  })

  it('initTheme 订阅 theme:changed；启动后端校正不广播', async () => {
    await loadFresh()
    expect(handlers.has('theme:changed')).toBe(true)
    // 再装一窗：后端 dark 与缓存 light 分歧 → 校正只应用 DOM，不 Emit
    // （防每窗起手都播一场事件风暴；真相已在后端，无需回声）
    vi.resetModules()
    emitSpy.mockClear()
    rpc.GetTheme.mockResolvedValueOnce('dark')
    const mod2 = await import('../useTheme')
    await mod2.initTheme()
    await nextTick()
    expect(document.documentElement.dataset.theme).toBe('dark') // systemDark=false → dark 直解
    expect(emitSpy).not.toHaveBeenCalled()
  })

  it('用户切换：本窗应用 + 双轴快照广播 + SetTheme 持久化', async () => {
    const mod = await loadFresh()
    const { setThemeMode } = mod.useTheme()
    emitSpy.mockClear()
    setThemeMode('dark')
    await nextTick()
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(emitSpy).toHaveBeenCalledWith('theme:changed', { mode: 'dark', accent: 'teal', font: 'kai' })
    expect(rpc.SetTheme).toHaveBeenCalledWith('dark')
  })

  it('收端回写：应用快照但不再广播（无回环）、不重复持久化', async () => {
    const mod = await loadFresh()
    const { themeMode } = mod.useTheme()
    localStorage.setItem('hanxi.theme', 'dark')
    // 主窗已切 dark（模拟），本窗停在 light → 收广播回写
    expect(themeMode.value).toBe('light')
    emitSpy.mockClear()
    rpc.SetTheme.mockClear()
    fire('theme:changed', { mode: 'dark', accent: 'iris' })
    await nextTick()
    expect(themeMode.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.dataset.accent).toBe('iris')
    expect(emitSpy).not.toHaveBeenCalled()
    expect(rpc.SetTheme).not.toHaveBeenCalled() // 收端是镜像不是二次写入
  })

  it('同值快照（发起窗自收）：ref 不动、watch 不触发', async () => {
    const mod = await loadFresh()
    const { setThemeMode, themeMode } = mod.useTheme()
    setThemeMode('dark')
    emitSpy.mockClear()
    fire('theme:changed', { mode: 'dark', accent: 'teal' })
    expect(themeMode.value).toBe('dark')
    expect(emitSpy).not.toHaveBeenCalled()
  })

  it('垃圾载荷拒收：非法 mode/accent 不回写', async () => {
    const mod = await loadFresh()
    const { themeMode, accent } = mod.useTheme()
    fire('theme:changed', { mode: 'hotpink', accent: 'rainbow' })
    expect(themeMode.value).toBe('light')
    expect(accent.value).toBe('teal')
    fire('theme:changed', undefined)
    expect(themeMode.value).toBe('light')
  })

  it('色板切换同样携带双轴快照广播', async () => {
    const mod = await loadFresh()
    const { setAccent } = mod.useTheme()
    emitSpy.mockClear()
    setAccent('jade')
    await nextTick()
    expect(document.documentElement.dataset.accent).toBe('jade')
    expect(emitSpy).toHaveBeenCalledWith('theme:changed', { mode: 'light', accent: 'jade', font: 'kai' })
    expect(rpc.SetAccent).toHaveBeenCalledWith('jade')
  })
})

// N40 界面字体档：与明暗/色板同通道（缓存 + 后端真相 + theme:changed 广播搭车），
// 但 watch 不触 DWM——字体纯内容层，原生窗框无感。
describe('useTheme 界面字体档（N40）', () => {
  beforeEach(() => {
    handlers.clear()
  })

  it('首帧缺省回落 kai：缓存无值 + 后端 kai → data-font="kai"', async () => {
    const mod = await loadFresh()
    const { font } = mod.useTheme()
    await nextTick()
    expect(font.value).toBe('kai')
    expect(document.documentElement.dataset.font).toBe('kai')
  })

  it('启动后端校正：GetFont 返回 plain 与缓存分歧 → 应用不广播', async () => {
    localStorage.clear()
    vi.resetModules()
    emitSpy.mockClear()
    rpc.GetFont.mockResolvedValueOnce('plain')
    const mod = await import('../useTheme')
    await mod.initTheme()
    await nextTick()
    expect(document.documentElement.dataset.font).toBe('plain')
    expect(localStorage.getItem('hanxi.font')).toBe('plain')
    expect(emitSpy).not.toHaveBeenCalled()
    rpc.GetFont.mockResolvedValue('kai')
  })

  it('切换字体：DOM 落值 + 缓存镜像 + SetFont 持久化 + 三轴快照广播', async () => {
    const mod = await loadFresh()
    const { setFontMode } = mod.useTheme()
    emitSpy.mockClear()
    setFontMode('mono')
    await nextTick()
    expect(document.documentElement.dataset.font).toBe('mono')
    expect(localStorage.getItem('hanxi.font')).toBe('mono')
    expect(rpc.SetFont).toHaveBeenCalledWith('mono')
    expect(emitSpy).toHaveBeenCalledWith('theme:changed', { mode: 'light', accent: 'teal', font: 'mono' })
  })

  it('收端回写字体档：应用快照、不再广播、不重复持久化', async () => {
    const mod = await loadFresh()
    emitSpy.mockClear()
    rpc.SetFont.mockClear()
    fire('theme:changed', { mode: 'light', accent: 'teal', font: 'plain' })
    await nextTick()
    expect(mod.useTheme().font.value).toBe('plain')
    expect(document.documentElement.dataset.font).toBe('plain')
    expect(emitSpy).not.toHaveBeenCalled()
    expect(rpc.SetFont).not.toHaveBeenCalled()
  })

  it('非法值双闸门：垃圾档位拒设、后端垃圾值回落缓存/默认', async () => {
    const mod = await loadFresh()
    const { font, setFontMode } = mod.useTheme()
    setFontMode('comic' as never)
    fire('theme:changed', { mode: 'light', accent: 'teal', font: 'hotpink' })
    await nextTick()
    expect(font.value).toBe('kai')
    expect(rpc.SetFont).not.toHaveBeenCalled()
    // 后端返回垃圾值：initTheme 校正分支拒收，沿用默认
    vi.resetModules()
    rpc.GetFont.mockResolvedValueOnce('serif' as never)
    const mod2 = await import('../useTheme')
    await mod2.initTheme()
    await nextTick()
    expect(mod2.useTheme().font.value).toBe('kai')
    rpc.GetFont.mockResolvedValue('kai')
  })
})
