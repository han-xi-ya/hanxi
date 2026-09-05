<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import * as AppAPI from '../bindings/hanxi/internal/app'
import { EnsureModuleActive } from '../bindings/hanxi/internal/app/appservice.js'
import type { NavEntry } from '../bindings/hanxi/internal/extapi/models'
import NotificationToast from './components/NotificationToast.vue'
import NotificationDrawer from './components/NotificationDrawer.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import UiPromptDialog from './components/ui/UiPromptDialog.vue'
import ErrorBoundary from './components/ui/ErrorBoundary.vue'
import AppSidebar from './components/shell/AppSidebar.vue'
import { moduleIdOf, routeComponent, placeholderComponent, fallbackComponent } from './constants/navigation'
import { useNotification } from './composables/useNotification'
import { useTheme } from './composables/useTheme'
import { useConfirm } from './composables/useConfirm'
import { usePrompt } from './composables/usePrompt'
import type { Notification } from '../bindings/hanxi/internal/notify/models'
import { useToast } from './composables/useToast'
import { getErrorMessage } from './utils/errors'

const { toastMsg, showToast } = useToast()
const { unreadCount, toggleDrawer, loadHistory, pushToast } = useNotification()
const { themeMode, cycleThemeMode } = useTheme()
const { confirmState, settleConfirm } = useConfirm()
const { promptState, settlePrompt } = usePrompt()

// 路由→组件与模块门禁清单已外移至 constants/navigation.ts（单一来源 + 视图异步化）。
// 侧栏展示层（导航分组/高亮/通知徽标/主题钮/状态条）已组件化至 components/shell/AppSidebar.vue，
// 本文件只保留路由与门禁编排。

const navs = ref<NavEntry[]>([])
const activeRoute = ref('/')
const backendReady = ref(false)
let unlistenExtChanged: (() => void) | null = null
let unlistenNotify: (() => void) | null = null
let unlistenTrayNav: (() => void) | null = null
let navigationRequestID = 0

async function refreshNavs() {
  try {
    const list = await AppAPI.AppService.GetNavs()
    navs.value = list ?? []

    // 如果当前所在路由对应的是已被禁用的扩展，则平滑重定向回首页
    const currentModID = moduleIdOf(activeRoute.value)
    if (currentModID) {
      const isRouteAvailable = navs.value.some(n => n.route === activeRoute.value)
      if (!isRouteAvailable && activeRoute.value !== '/' && activeRoute.value !== '/settings' && activeRoute.value !== '/logs' && activeRoute.value !== '/about') {
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

onMounted(async () => {
  try {
    await refreshNavs()
    await loadHistory()
  } finally {
    backendReady.value = true
  }

  // 监听后端模块开关与导航热更新事件
  unlistenExtChanged = Events.On('ext:changed', () => {
    refreshNavs()
  })

  // 监听全局统一通知事件（唯一顶层监听，承接全模块通知）
  // Wails runtime 回调恒为 { name, data } 包装，data 即后端 emit 载荷
  unlistenNotify = Events.On('notify:received', (event: { data?: Notification }) => {
    if (event?.data) {
      pushToast(event.data)
    }
  })

  // 监听托盘右键菜单的页面直达请求（载荷为前端路由）
  unlistenTrayNav = Events.On('tray:navigate', (event: { data?: string }) => {
    if (typeof event?.data === 'string' && event.data) {
      navigateTo(event.data)
    }
  })
})

onUnmounted(() => {
  if (unlistenExtChanged) {
    unlistenExtChanged()
    unlistenExtChanged = null
  }
  if (unlistenNotify) {
    unlistenNotify()
    unlistenNotify = null
  }
  if (unlistenTrayNav) {
    unlistenTrayNav()
    unlistenTrayNav = null
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
      :theme-mode="themeMode"
      :backend-ready="backendReady"
      @navigate="navigateTo"
      @toggle-drawer="toggleDrawer"
      @cycle-theme="cycleThemeMode"
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

/* 全局轻量 Toast 提示（深底浮层为刻意设计，两种主题下均成立） */
.global-toast {
  position: fixed;
  top: 20px;
  right: 24px;
  background: rgba(31, 35, 40, 0.92);
  backdrop-filter: blur(8px);
  color: #ffffff;
  padding: 8px 16px;
  border-radius: var(--radius-control);
  font-size: 13px;
  box-shadow: var(--shadow-panel);
  animation: toastFadeIn var(--motion-slow) cubic-bezier(0.16, 1, 0.3, 1);
  z-index: 9999;
  pointer-events: none;
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
