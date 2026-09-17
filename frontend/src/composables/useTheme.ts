// 主题单例 composable（docs/FRONTEND.md §7.2）——明暗轴（theme）与色板轴（accent）的唯一读写入口。
// 真相源 = 后端 settings（AppSettings.Theme / AppSettings.Accent，随便携 data/ 迁移）；
// localStorage 仅作首帧缓存：mount 前同步应用防闪白，启动后以后端为准校正。
// 标题栏经 AppService.SetWindowDarkMode 桥到 Win32 DWM（前端管不到原生窗框）：
// 明暗 + 色板双轴一并同步，标题栏底色对齐 --surface-chrome 外壳层。
import { computed, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import * as AppAPI from '../../bindings/hanxi/internal/app'

export type ThemeMode = 'light' | 'dark' | 'system'
export type AccentMode = 'teal' | 'sky' | 'iris' | 'jade' | 'onyx'

const CACHE_KEY = 'hanxi.theme'
const VALID_MODES: ThemeMode[] = ['light', 'dark', 'system']

const ACCENT_CACHE_KEY = 'hanxi.accent'
const VALID_ACCENTS: AccentMode[] = ['teal', 'sky', 'iris', 'jade', 'onyx']
const DEFAULT_ACCENT: AccentMode = 'teal'

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

// 模块级单例状态（与 useToast/useNotification 同一模式）
const themeMode = ref<ThemeMode>(readCache() ?? 'light')
const accent = ref<AccentMode>(readAccentCache() ?? DEFAULT_ACCENT)
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

/** 在 createApp 前调用：先用缓存同步定主题（明暗 + 色板），再异步以后端为准校正。 */
export async function initTheme(): Promise<void> {
  if (initialized) return
  initialized = true
  applyToDom(themeMode.value, systemDark.value)
  applyAccentToDom(accent.value)
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
}

export function useTheme() {
  /** 切换主题模式并持久化到后端（失败仅告警：DOM 预览已生效，下次启动以后端为准）。 */
  function setThemeMode(mode: ThemeMode) {
    if (!VALID_MODES.includes(mode)) return
    themeMode.value = mode
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
    AppAPI.AppService.SetAccent(next).catch((err: unknown) => {
      console.warn('[theme] 色板持久化失败:', err)
    })
  }

  return { themeMode, resolvedTheme, systemDark, accent, setThemeMode, cycleThemeMode, setAccent }
}
