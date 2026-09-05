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

const SYSTEM_NAV = [
  { route: '/', title: '首页', icon: '⌂' },
]

const BOTTOM_NAV = [
  { route: '/logs', title: '日志', icon: '📋' },
  { route: '/settings', title: '设置', icon: '⚙' },
  { route: '/about', title: '关于', icon: 'ⓘ' },
]

const themeModeLabel = computed(() => {
  switch (themeMode.value) {
    case 'system': return '跟随系统'
    case 'dark': return '深色主题'
    default: return '浅色主题'
  }
})

const themeGlyph = computed(() => {
  switch (themeMode.value) {
    case 'system': return '◐'
    case 'dark': return '☾'
    default: return '☀'
  }
})

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
    <!-- 左侧固定宽度侧边栏 -->
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark">HX</span>
        <div class="brand-info">
          <span class="brand-name">Hanxi</span>
          <span class="brand-desc">开源工具工作台</span>
        </div>
      </div>

      <!-- 中间模块导航列表 -->
      <nav class="nav-section main-nav">
        <button
          v-for="n in SYSTEM_NAV"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="navigateTo(n.route)"
        >
          <span class="nav-icon">{{ n.icon }}</span>
          <span class="nav-text">{{ n.title }}</span>
        </button>

        <div class="nav-divider" v-if="navs.length > 0"></div>
        <div class="nav-group-label" v-if="navs.length > 0">工具</div>

        <button
          v-for="n in navs"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="navigateTo(n.route)"
        >
          <span class="nav-icon">{{ n.icon }}</span>
          <span class="nav-text">{{ n.title }}</span>
        </button>
      </nav>

      <!-- 底部固定设置与状态 -->
      <nav class="nav-section bottom-nav">
        <!-- 通知中心入口按钮 -->
        <button
          class="nav-item notif-nav-btn"
          @click="toggleDrawer"
          title="通知中心"
        >
          <span class="nav-icon">🔔</span>
          <span class="nav-text">通知中心</span>
          <span v-if="unreadCount > 0" class="nav-badge">{{ unreadCount }}</span>
        </button>

        <button
          v-for="n in BOTTOM_NAV"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="navigateTo(n.route)"
        >
          <span class="nav-icon">{{ n.icon }}</span>
          <span class="nav-text">{{ n.title }}</span>
        </button>

        <!-- 主题循环切换：跟随系统 → 浅色 → 深色 -->
        <button
          class="nav-item theme-toggle"
          :title="`当前主题：${themeModeLabel}（点击循环切换）`"
          @click="cycleThemeMode"
        >
          <span class="nav-icon">{{ themeGlyph }}</span>
          <span class="nav-text">{{ themeModeLabel }}</span>
        </button>

        <div class="status-bar">
          <span class="status-dot" :class="{ online: backendReady }"></span>
          <span class="status-text">{{ backendReady ? '工作台已就绪' : '正在加载工作台…' }}</span>
        </div>
      </nav>
    </aside>

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
   .page 等页面骨架已上收到全局 components.css，不在本文件重复定义。 */
.layout {
  display: flex;
  height: 100vh;
  height: 100dvh;
  width: 100vw;
  background: var(--surface-page);
  color: var(--color-text);
  overflow: hidden;
}

/* 侧边栏布局与视觉 */
.sidebar {
  width: 220px;
  flex: 0 0 220px;
  background: var(--surface-panel);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  height: 100%;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 18px 14px;
  border-bottom: 1px solid var(--color-border);
}

.brand-mark {
  background: linear-gradient(135deg, var(--color-primary) 0%, var(--color-primary-hover) 100%);
  color: var(--color-on-primary);
  border-radius: var(--radius-control);
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 700;
  box-shadow: 0 2px 4px var(--color-primary-glow);
}

.brand-info {
  display: flex;
  flex-direction: column;
}

.brand-name {
  font-size: 15px;
  font-weight: 700;
  color: var(--color-text);
  line-height: 1.2;
}

.brand-desc {
  font-size: 11px;
  color: var(--color-text-subtle);
}

/* 导航区 */
.nav-section {
  padding: 8px 10px;
}

.main-nav {
  flex: 1;
  overflow-y: auto;
}

.bottom-nav {
  flex: 0 0 auto;
  border-top: 1px solid var(--color-border);
  padding-bottom: 6px;
}

.nav-divider {
  height: 1px;
  background: var(--color-border);
  margin: 8px 4px 6px;
}

.nav-group-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--color-text-subtle);
  padding: 4px 8px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 12px;
  margin-bottom: 3px;
  background: transparent;
  border: none;
  border-radius: var(--radius-control);
  color: var(--color-text-muted);
  font-size: 13px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: background var(--motion-base) ease, color var(--motion-base) ease;
}

.nav-item:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.nav-item.active {
  background: var(--surface-selected);
  color: var(--color-primary);
  font-weight: 600;
}

.nav-icon {
  width: 18px;
  font-size: 14px;
  text-align: center;
}

.nav-text {
  flex: 1;
}

.nav-badge {
  background: var(--state-danger);
  color: var(--surface-page);
  font-size: 10px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: var(--radius-pill);
  line-height: 1.2;
}

/* 状态条 */
.status-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px 4px;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-border-strong);
}

.status-dot.online {
  background: var(--state-positive);
  box-shadow: 0 0 0 2px var(--state-positive-glow);
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
