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
    expect(emitSpy).toHaveBeenCalledWith('theme:changed', { mode: 'dark', accent: 'teal' })
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
    expect(emitSpy).toHaveBeenCalledWith('theme:changed', { mode: 'light', accent: 'jade' })
    expect(rpc.SetAccent).toHaveBeenCalledWith('jade')
  })
})
