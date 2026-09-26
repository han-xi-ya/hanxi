// 主题单例 composable（docs/FRONTEND.md §7.2）——明暗轴（theme）、色板轴（accent）与
// 界面字体档（font，N40）的唯一读写入口。
// 真相源 = 后端 settings（AppSettings.Theme / AppSettings.Accent，随数据目录迁移）；
// localStorage 仅作首帧缓存：mount 前同步应用防闪白，启动后以后端为准校正。
// 标题栏经 AppService.SetWindowDarkMode 桥到 Win32 DWM（前端管不到原生窗框）：
// 明暗 + 色板双轴一并同步，标题栏底色对齐 --surface-chrome 外壳层。
import { computed, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { Events } from '@wailsio/runtime'
import * as AppAPI from '../../bindings/hanxi/internal/app'

// N39 主题跨窗广播：轮盘/挂牌/OCR 结果卡等浮窗是独立 webview，首帧经
// initTheme 各读各的缓存/后端，但**存活的浮窗**对主窗之后的切换无感（旧深色
// 残留即此病根）。走 Wails 事件总线——前端 Events.Emit 经内建通道进 Go
// EventManager 再广播到全部窗口（含发起窗自收，v3 messageprocessor
// EventsEmit→EmitEvent 实证）：用户动作窗发 `theme:changed`，各窗监听后只回写
// 本地 ref——应用统一由既有 watch 完成（DOM/缓存/DWM 一条路），收端同值赋值
// 不触发 watch，天然无回环。本事件是纯前端发起的跨窗信号（载荷为持久化前的
// 意图值，真相仍由 SetTheme/SetAccent RPC 落 settings），不入后端业务事件面。
const THEME_EVENT = 'theme:changed'

export type ThemeMode = 'light' | 'dark' | 'system'
export type AccentMode = 'teal' | 'sky' | 'iris' | 'jade' | 'onyx'
// N40 界面字体档：kai 默认文楷 / plain 系统朴素 / mono 等宽极客。
// 合法域与后端 settings_persist.go、fonts.css 档位覆写块三处字面一致。
export type FontMode = 'kai' | 'plain' | 'mono'

const CACHE_KEY = 'hanxi.theme'
const VALID_MODES: ThemeMode[] = ['light', 'dark', 'system']

const ACCENT_CACHE_KEY = 'hanxi.accent'
const VALID_ACCENTS: AccentMode[] = ['teal', 'sky', 'iris', 'jade', 'onyx']
const DEFAULT_ACCENT: AccentMode = 'teal'

const FONT_CACHE_KEY = 'hanxi.font'
const VALID_FONTS: FontMode[] = ['kai', 'plain', 'mono']
const DEFAULT_FONT: FontMode = 'kai'

function readCache(): ThemeMode | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    return VALID_MODES.includes(raw as ThemeMode) ? (raw as ThemeMode) : null
  } catch {
    return null
  }
}

function writeCache(mode: ThemeMode) {
  try {
    localStorage.setItem(CACHE_KEY, mode)
  } catch {
    /* 存储不可用时静默降级：缓存只是防闪优化，不是真相 */
  }
}

function readAccentCache(): AccentMode | null {
  try {
    const raw = localStorage.getItem(ACCENT_CACHE_KEY)
    return VALID_ACCENTS.includes(raw as AccentMode) ? (raw as AccentMode) : null
  } catch {
    return null
  }
}

function writeAccentCache(accent: AccentMode) {
  try {
    localStorage.setItem(ACCENT_CACHE_KEY, accent)
  } catch {
    /* 存储不可用时静默降级：与 theme 同理，缓存只是防闪优化 */
  }
}

function readFontCache(): FontMode | null {
  try {
    const raw = localStorage.getItem(FONT_CACHE_KEY)
    return VALID_FONTS.includes(raw as FontMode) ? (raw as FontMode) : null
  } catch {
    return null
  }
}

function writeFontCache(font: FontMode) {
  try {
    localStorage.setItem(FONT_CACHE_KEY, font)
  } catch {
    /* 存储不可用时静默降级：与 theme/accent 同理，缓存只是防闪优化 */
  }
}

function applyFontToDom(font: FontMode) {
  document.documentElement.dataset.font = font
}

function applyToDom(mode: ThemeMode, systemDark: boolean): 'light' | 'dark' {
  const resolved = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode
  const el = document.documentElement
  el.dataset.theme = resolved
  el.style.colorScheme = resolved
  syncWindowChrome(resolved)
  return resolved
}

/** 原生标题栏跟随双轴（启动早期窗口未就绪/非 Windows 平台时静默失败即可）。 */
function syncWindowChrome(resolved: 'light' | 'dark') {
  AppAPI.AppService.SetWindowDarkMode(resolved === 'dark', accent.value).catch(() => {
    /* DWM 桥降级：内容主题仍正确 */
  })
}

function applyAccentToDom(accent: AccentMode) {
  document.documentElement.dataset.accent = accent
}

/** 用户动作后向全员广播三轴当前值（自收为同值 no-op，见 THEME_EVENT 注释）。 */
function broadcastTheme() {
  Events.Emit(THEME_EVENT, { mode: themeMode.value, accent: accent.value, font: font.value })
}

// 模块级单例状态（与 useToast/useNotification 同一模式）
const themeMode = ref<ThemeMode>(readCache() ?? 'light')
const accent = ref<AccentMode>(readAccentCache() ?? DEFAULT_ACCENT)
const font = ref<FontMode>(readFontCache() ?? DEFAULT_FONT)
const systemDark = useMediaQuery('(prefers-color-scheme: dark)')
const resolvedTheme = computed<'light' | 'dark'>(() =>
  themeMode.value === 'system' ? (systemDark.value ? 'dark' : 'light') : themeMode.value,
)

let initialized = false

watch([themeMode, systemDark], () => {
  writeCache(themeMode.value)
  applyToDom(themeMode.value, systemDark.value)
})

watch(accent, (value) => {
  writeAccentCache(value)
  applyAccentToDom(value)
  // 标题栏外壳配色随色板联动（壳底 --surface-chrome 各色板不同）
  syncWindowChrome(resolvedTheme.value)
})

// 字体档与窗框/配色无涉（DWM 只管颜色），watch 只做缓存 + DOM 覆写两件事
watch(font, (value) => {
  writeFontCache(value)
  applyFontToDom(value)
})

/** 收端回写：只动 ref，应用统一走既有 watch（同值不触发 watch——无回环的前提）。 */
function onThemeBroadcast(ev: { data?: { mode?: unknown; accent?: unknown; font?: unknown } }) {
  const d = ev?.data
  if (!d) return
  if (VALID_MODES.includes(d.mode as ThemeMode) && d.mode !== themeMode.value) {
    themeMode.value = d.mode as ThemeMode
  }
  if (VALID_ACCENTS.includes(d.accent as AccentMode) && d.accent !== accent.value) {
    accent.value = d.accent as AccentMode
  }
  if (VALID_FONTS.includes(d.font as FontMode) && d.font !== font.value) {
    font.value = d.font as FontMode
  }
}

/** 在 createApp 前调用：先用缓存同步定主题（明暗 + 色板 + 字体档），再异步以后端为准校正。 */
export async function initTheme(): Promise<void> {
  if (initialized) return
  initialized = true
  // 每个 webview（主窗/浮窗）各自订阅一次；订阅前错过的切换由首帧缓存/后端校正兜底
  Events.On(THEME_EVENT, onThemeBroadcast)
  applyToDom(themeMode.value, systemDark.value)
  applyAccentToDom(accent.value)
  applyFontToDom(font.value)
  try {
    const backend = await AppAPI.AppService.GetTheme()
    if (VALID_MODES.includes(backend as ThemeMode) && backend !== themeMode.value) {
      themeMode.value = backend as ThemeMode // 触发 watch 完成应用与回写缓存
      writeCache(themeMode.value)
    }
  } catch (err) {
    console.warn('[theme] 读取后端主题失败，沿用本地缓存:', err)
  }
  try {
    const backend = await AppAPI.AppService.GetAccent()
    if (VALID_ACCENTS.includes(backend as AccentMode) && backend !== accent.value) {
      accent.value = backend as AccentMode // 触发 watch 完成应用与回写缓存
      writeAccentCache(accent.value)
    }
  } catch (err) {
    console.warn('[theme] 读取后端色板失败，沿用本地缓存:', err)
  }
  try {
    const backend = await AppAPI.AppService.GetFont()
    if (VALID_FONTS.includes(backend as FontMode) && backend !== font.value) {
      font.value = backend as FontMode // 触发 watch 完成应用与回写缓存
      writeFontCache(font.value)
    }
  } catch (err) {
    console.warn('[theme] 读取后端字体档失败，沿用本地缓存:', err)
  }
}

export function useTheme() {
  /** 切换主题模式并持久化到后端（失败仅告警：DOM 预览已生效，下次启动以后端为准）。 */
  function setThemeMode(mode: ThemeMode) {
    if (!VALID_MODES.includes(mode)) return
    themeMode.value = mode
    broadcastTheme()
    AppAPI.AppService.SetTheme(mode).catch((err: unknown) => {
      console.warn('[theme] 主题持久化失败:', err)
    })
  }

  /** 侧栏快捷钮用：system → light → dark 循环。 */
  function cycleThemeMode() {
    const next: ThemeMode =
      themeMode.value === 'system' ? 'light' : themeMode.value === 'light' ? 'dark' : 'system'
    setThemeMode(next)
  }

  /** 切换色板轴并持久化到后端（失败仅告警：DOM 预览已生效，下次启动以后端为准）。 */
  function setAccent(next: AccentMode) {
    if (!VALID_ACCENTS.includes(next)) return
    accent.value = next
    broadcastTheme()
    AppAPI.AppService.SetAccent(next).catch((err: unknown) => {
      console.warn('[theme] 色板持久化失败:', err)
    })
  }

  /** 切换界面字体档（N40）并持久化到后端（失败仅告警：DOM 已即时换装预览，下次启动以后端为准）。 */
  function setFontMode(next: FontMode) {
    if (!VALID_FONTS.includes(next)) return
    font.value = next
    broadcastTheme()
    AppAPI.AppService.SetFont(next).catch((err: unknown) => {
      console.warn('[theme] 字体档持久化失败:', err)
    })
  }

  return { themeMode, resolvedTheme, systemDark, accent, font, setThemeMode, cycleThemeMode, setAccent, setFontMode }
}
