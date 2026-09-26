import { createApp } from 'vue'
import App from './App.vue'
import QuickMenuPopup from './views/QuickMenuPopup.vue'
import SnipCardView from './views/SnipCardView.vue'
import MsgBoardPopup from './views/MsgBoardPopup.vue'
import QuickMemoSheet from './views/QuickMemoSheet.vue'
import './styles/tokens.css'
import './styles/fonts.css'
import './styles/base.css'
import './styles/components.css'
import { initTheme } from './composables/useTheme'
import { useToast } from './composables/useToast'
import { getErrorMessage } from './utils/errors'

// 主题首帧同步应用（localStorage 缓存先行，后端 Settings 异步校正）
initTheme()

// 独立 frameless 顶层窗口按 hash 分流（同一前端产物多窗复用，不加载工作台外壳）：
//   #quickmenu → 右键长按轮盘；#ocrcard → 框选截屏识别悬浮结果卡；
//   #msgboard → 全屏留言牌（透明全屏窗，牌体由视图自绘）；
//   #memosheet → 悬浮速记卡（N16 全局热键唤出，卡体由视图自绘）。
const hash = window.location.hash
const rootComponent = hash.startsWith('#quickmenu')
  ? QuickMenuPopup
  : hash.startsWith('#ocrcard')
    ? SnipCardView
    : hash.startsWith('#msgboard')
      ? MsgBoardPopup
      : hash.startsWith('#memosheet')
        ? QuickMemoSheet
        : App
if (rootComponent !== App) {
  // 透明壳窗口：canvas（html/body）底色一并透明，否则近白 --surface-page 会在
  // 盘体外露出白底（见 base.css .popup-shell 规则）。
  document.documentElement.classList.add('popup-shell')
}
const app = createApp(rootComponent)

// 全局错误兜底：ErrorBoundary 未覆盖的泄漏路径（事件回调、异步流等）转 toast 可见，
// 不再静默 console + 白屏
app.config.errorHandler = (err) => {
  console.error('[app] 未捕获异常:', err)
  useToast().showToast(`运行时异常: ${getErrorMessage(err)}`)
}

app.mount('#app')
