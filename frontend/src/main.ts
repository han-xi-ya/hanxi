import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'
import { initTheme } from './composables/useTheme'
import { useToast } from './composables/useToast'
import { getErrorMessage } from './utils/errors'

// 主题首帧同步应用（localStorage 缓存先行，后端 Settings 异步校正）
initTheme()

const app = createApp(App)

// 全局错误兜底：ErrorBoundary 未覆盖的泄漏路径（事件回调、异步流等）转 toast 可见，
// 不再静默 console + 白屏
app.config.errorHandler = (err) => {
  console.error('[app] 未捕获异常:', err)
  useToast().showToast(`运行时异常: ${getErrorMessage(err)}`)
}

app.mount('#app')
