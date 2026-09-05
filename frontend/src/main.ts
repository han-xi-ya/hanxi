import { createApp } from 'vue'
import App from './App.vue'
import QuickMenuPopup from './views/QuickMenuPopup.vue'
import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'
import { initTheme } from './composables/useTheme'
import { useToast } from './composables/useToast'
import { getErrorMessage } from './utils/errors'

// 主题首帧同步应用（localStorage 缓存先行，后端 Settings 异步校正）
initTheme()

// 快捷菜单弹窗为独立 frameless 顶层窗口（后端以 URL "/#quickmenu" 加载同一前端产物）：
// 按 hash 分流只挂紧凑菜单视图，不加载工作台外壳（侧栏/通知/对话框）。
const isQuickMenuPopup = window.location.hash.startsWith('#quickmenu')
const app = createApp(isQuickMenuPopup ? QuickMenuPopup : App)

// 全局错误兜底：ErrorBoundary 未覆盖的泄漏路径（事件回调、异步流等）转 toast 可见，
// 不再静默 console + 白屏
app.config.errorHandler = (err) => {
  console.error('[app] 未捕获异常:', err)
  useToast().showToast(`运行时异常: ${getErrorMessage(err)}`)
}

app.mount('#app')
