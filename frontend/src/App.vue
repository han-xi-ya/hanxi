<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import * as AppAPI from '../bindings/hanxi/internal/app'
import { EnsureModuleActive } from '../bindings/hanxi/internal/app/appservice.js'
import type { NavEntry } from '../bindings/hanxi/internal/extapi/models'
import NotificationToast from './components/NotificationToast.vue'
import NotificationDrawer from './components/NotificationDrawer.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import UiPromptDialog from './components/ui/UiPromptDialog.vue'
import ErrorBoundary from './components/ui/ErrorBoundary.vue'
import AppSidebar from './components/shell/AppSidebar.vue'
import CommandPalette from './components/shell/CommandPalette.vue'
import { moduleIdOf, routeComponent, placeholderComponent, fallbackComponent, isCoreRoute } from './constants/navigation'
import { useNotification } from './composables/useNotification'
import { useConfirm } from './composables/useConfirm'
import { usePrompt } from './composables/usePrompt'
import type { Notification } from '../bindings/hanxi/internal/notify/models'
import { useToast } from './composables/useToast'
import { useWailsEvent } from './composables/useWailsEvent'
import { getErrorMessage } from './utils/errors'

const { toastMsg, showToast } = useToast()
const { unreadCount, toggleDrawer, loadHistory, pushToast } = useNotification()
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

// 路由→组件与模块门禁清单已外移至 constants/navigation.ts（单一来源 + 视图异步化）。
// 侧栏展示层（导航分组/高亮/通知徽标/状态条）已组件化至 components/shell/AppSidebar.vue，
// 本文件只保留路由与门禁编排。（原侧栏主题钮已下线，主题切换唯一入口在设置页「外观主题」。）

const navs = ref<NavEntry[]>([])
const activeRoute = ref('/')
const backendReady = ref(false)
let navigationRequestID = 0

async function refreshNavs() {
  try {
    const list = await AppAPI.AppService.GetNavs()
    navs.value = list ?? []

    // 如果当前所在路由对应的是已被禁用的扩展，则平滑重定向回首页
    //（核心页豁免清单单一来源 navigation.CORE_ROUTES，含首页与模块中心）
    const currentModID = moduleIdOf(activeRoute.value)
    if (currentModID) {
      const isRouteAvailable = navs.value.some(n => n.route === activeRoute.value)
      if (!isRouteAvailable && !isCoreRoute(activeRoute.value)) {
        activeRoute.value = '/'
      }
    }
  } catch (err) {
    console.error('Failed to load navigation:', err)
  }
}

async function navigateTo(route: string) {
  const requestID = ++navigationRequestID
  const modID = moduleIdOf(route)
  if (modID) {
    try {
      await EnsureModuleActive(modID)
    } catch (err: unknown) {
      if (requestID === navigationRequestID) {
        showToast(`模块初始化失败: ${getErrorMessage(err)}`)
      }
      return
    }
  }
  if (requestID === navigationRequestID) {
    activeRoute.value = route
  }
}

const currentView = computed(() => {
  const known = routeComponent(activeRoute.value)
  if (known) return known
  if (navs.value.some(n => n.route === activeRoute.value)) return placeholderComponent()
  return fallbackComponent()
})

const currentExt = computed(() => navs.value.find(n => n.route === activeRoute.value))

// 顶层事件订阅（useWailsEvent：setup 同步期注册、组件作用域卸载自动注销；
// 载荷已由 composable 统一拆 {data} 包装，handler 直收后端 emit 载荷本体。
// 相比旧「onMounted 内注册」，早期事件不再因挂载竞态丢失——handler 只触 ref/API，
// ext:changed 早到对 GetNavs 幂等无害，不加新闩保持现语义面）。
// 后端模块开关与导航热更新事件
useWailsEvent('ext:changed', () => {
  void refreshNavs()
})

// 全局统一通知事件（唯一顶层监听，承接全模块通知；空载荷防护保留）
useWailsEvent<Notification>('notify:received', (d) => {
  if (d) {
    pushToast(d)
  }
})

// 托盘右键菜单的页面直达请求（载荷为前端路由，非空字符串防护保留）
useWailsEvent<string>('tray:navigate', (d) => {
  if (typeof d === 'string' && d) {
    void navigateTo(d)
  }
})

onMounted(async () => {
  try {
    await refreshNavs()
    await loadHistory()
  } finally {
    backendReady.value = true
  }

  // 提权重启交接回航：初始 URL 携带 "#/路由"（后端 -route 注入）时直达重启前
  // 页面。只认已注册导航路由（含核心页），未知/禁用路由留在首页。
  const handoff = window.location.hash.replace(/^#/, '')
  if (handoff && handoff !== '/' && navs.value.some(n => n.route === handoff)) {
    await navigateTo(handoff)
  }
})
</script>

<template>
  <div class="layout">
    <!-- 左侧固定宽度侧边栏（展示层组件化：状态经 props 注入、动作以事件上抛） -->
    <AppSidebar
      :navs="navs"
      :active-route="activeRoute"
      :unread-count="unreadCount"
      :backend-ready="backendReady"
      @navigate="navigateTo"
      @toggle-drawer="toggleDrawer"
    />

    <!-- Ctrl/⌘+K 命令面板（自管理全局热键与浮层；宿主模态流程打开时禁用，
         导航动作经 navigateTo 走既有模块门禁链） -->
    <CommandPalette
      :navs="navs"
      :disabled="confirmState.open || promptState.open"
      @navigate="navigateTo"
    />

    <!-- 右侧内容主视口 -->
    <main class="content-area">
      <!-- 统一通知浮层 Toast -->
      <NotificationToast @navigate="navigateTo" />

      <!-- 统一通知抽屉 Drawer -->
      <NotificationDrawer @navigate="navigateTo" />

      <div v-if="toastMsg" class="global-toast">{{ toastMsg }}</div>
      <ErrorBoundary :reset-key="activeRoute">
        <Transition name="page-fade" mode="out-in">
          <KeepAlive :max="10">
            <component
              :is="currentView"
              :key="activeRoute"
              :title="currentExt?.title"
              @navigate="navigateTo"
            />
          </KeepAlive>
        </Transition>
      </ErrorBoundary>
    </main>

    <!-- 全局确认/输入对话框宿主（useConfirm / usePrompt 单例驱动，视图侧不再各挂各的） -->
    <ConfirmDialog
      :open="confirmState.open"
      :title="confirmState.options.title"
      :description="confirmState.options.description"
      :confirm-label="confirmState.options.confirmLabel"
      :cancel-label="confirmState.options.cancelLabel"
      :tone="confirmState.options.tone"
      :details="confirmState.options.details"
      @confirm="settleConfirm(true)"
      @cancel="settleConfirm(false)"
      @update:open="(v: boolean) => { if (!v) settleConfirm(false) }"
    />
    <UiPromptDialog
      :open="promptState.open"
      :title="promptState.options.title"
      :description="promptState.options.description"
      :label="promptState.options.label"
      :placeholder="promptState.options.placeholder"
      :initial-value="promptState.options.initialValue"
      :confirm-label="promptState.options.confirmLabel"
      :cancel-label="promptState.options.cancelLabel"
      @submit="settlePrompt"
      @cancel="settlePrompt(null)"
      @update:open="(v: boolean) => { if (!v) settlePrompt(null) }"
    />
  </div>
</template>

<style scoped>
/* 应用外壳样式（设计 token 见 styles/tokens.css；原子工具类见 styles/components.css）。
   .page 等页面骨架已上收到全局 components.css，不在本文件重复定义。
   侧栏布局类样式已随标记迁至 components/shell/AppSidebar.vue 的 scoped 块。 */
.layout {
  display: flex;
  height: 100vh;
  height: 100dvh;
  width: 100vw;
  background: var(--surface-page);
  color: var(--color-text);
  overflow: hidden;
}

/* 内容主区域 */
.content-area {
  flex: 1;
  height: 100%;
  overflow-y: auto;
  padding: 24px 32px;
  background: var(--surface-page);
}

/* 全局轻量 Toast 提示（深底浮层为刻意设计，两种主题下均成立）
   允许选中复制：错误串常含路径/报错文本，需可拖选；pointer-events:auto 为有意开放交互 */
.global-toast {
  position: fixed;
  top: 20px;
  right: 24px;
  max-width: 480px;
  background: rgba(31, 35, 40, 0.92);
  backdrop-filter: blur(8px);
  color: #ffffff;
  padding: 8px 16px;
  border-radius: var(--radius-control);
  font-size: var(--text-base);
  line-height: 1.6;
  overflow-wrap: anywhere;
  box-shadow: var(--shadow-panel);
  animation: toastFadeIn var(--motion-slow) cubic-bezier(0.16, 1, 0.3, 1);
  z-index: 9999;
  pointer-events: auto;
  user-select: text;
}

@keyframes toastFadeIn {
  from {
    opacity: 0;
    transform: translateY(-8px) scale(0.96);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

/* 页面切换平滑过渡动画 */
.page-fade-enter-active,
.page-fade-leave-active {
  transition: opacity var(--motion-base) ease, transform var(--motion-base) ease;
}

.page-fade-enter-from {
  opacity: 0;
  transform: translateY(4px);
}

.page-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
