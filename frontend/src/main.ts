import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'
import { initTheme } from './composables/useTheme'

// 主题首帧同步应用（localStorage 缓存先行，后端 Settings 异步校正）
initTheme()

createApp(App).mount('#app')
