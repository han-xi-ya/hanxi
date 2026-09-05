// 主题单例 composable（docs/FRONTEND.md §7.2）——主题的唯一读写入口。
// 真相源 = 后端 settings（AppSettings.Theme，随便携 data/ 迁移）；
// localStorage 仅作首帧缓存：mount 前同步应用防闪白，启动后以后端为准校正。
// 标题栏深色经 AppService.SetWindowDarkMode 桥到 Win32 DWM（前端管不到原生窗框）。
import { computed, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import * as AppAPI from '../../bindings/hanxi/internal/app'

export type ThemeMode = 'light' | 'dark' | 'system'

const CACHE_KEY = 'hanxi.theme'
const VALID_MODES: ThemeMode[] = ['light', 'dark', 'system']

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

function applyToDom(mode: ThemeMode, systemDark: boolean): 'light' | 'dark' {
  const resolved = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode
  const el = document.documentElement
  el.dataset.theme = resolved
  el.style.colorScheme = resolved
  // 原生标题栏跟随（启动早期窗口未就绪/非 Windows 平台时静默失败即可）
  AppAPI.AppService.SetWindowDarkMode(resolved === 'dark').catch(() => {
    /* DWM 桥降级：内容主题仍正确 */
  })
  return resolved
}

// 模块级单例状态（与 useToast/useNotification 同一模式）
const themeMode = ref<ThemeMode>(readCache() ?? 'light')
const systemDark = useMediaQuery('(prefers-color-scheme: dark)')
const resolvedTheme = computed<'light' | 'dark'>(() =>
  themeMode.value === 'system' ? (systemDark.value ? 'dark' : 'light') : themeMode.value,
)

let initialized = false

watch([themeMode, systemDark], () => {
  writeCache(themeMode.value)
  applyToDom(themeMode.value, systemDark.value)
})

/** 在 createApp 前调用：先用缓存同步定主题，再异步以后端为准校正。 */
export async function initTheme(): Promise<void> {
  if (initialized) return
  initialized = true
  applyToDom(themeMode.value, systemDark.value)
  try {
    const backend = await AppAPI.AppService.GetTheme()
    if (VALID_MODES.includes(backend as ThemeMode) && backend !== themeMode.value) {
      themeMode.value = backend as ThemeMode // 触发 watch 完成应用与回写缓存
      writeCache(themeMode.value)
    }
  } catch (err) {
    console.warn('[theme] 读取后端主题失败，沿用本地缓存:', err)
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

  return { themeMode, resolvedTheme, systemDark, setThemeMode, cycleThemeMode }
}
