<script setup lang="ts">
import { ref, computed, markRaw, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import * as AppAPI from '../bindings/hubkit/internal/app'
import type { NavEntry } from '../bindings/hubkit/internal/extapi/models'
import HomeView from './views/HomeView.vue'
import FrpcProjectsView from './views/FrpcProjectsView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'
import LogsView from './views/LogsView.vue'
import LanScannerView from './views/LanScannerView.vue'
import PublicIpView from './views/PublicIpView.vue'
import PortKillView from './views/PortKillView.vue'
import PortScanView from './views/PortScanView.vue'
import WechatBotView from './views/WechatBotView.vue'
import FileShareView from './views/FileShareView.vue'
import MemoView from './views/MemoView.vue'
import MarkerOnView from './views/MarkerOnView.vue'
import WifiView from './views/WifiView.vue'
import ExtPlaceholderView from './views/ExtPlaceholderView.vue'
import NotificationToast from './components/NotificationToast.vue'
import NotificationDrawer from './components/NotificationDrawer.vue'
import { useNotification } from './composables/useNotification'
import type { Notification } from '../bindings/hubkit/internal/notify/models'
import { useToast } from './composables/useToast'

const { toastMsg } = useToast()
const { unreadCount, toggleDrawer, loadHistory, pushToast } = useNotification()

// 核心与内置功能视图路由映射（使用 markRaw 避免深度响应式包装）
const CORE_VIEWS: Record<string, any> = {
  '/': markRaw(HomeView),
  '/frpc': markRaw(FrpcProjectsView),
  '/ext/fileshare': markRaw(FileShareView),
  '/ext/memo': markRaw(MemoView),
  '/ext/lan': markRaw(LanScannerView),
  '/ext/portscan': markRaw(PortScanView),
  '/ext/wechat': markRaw(WechatBotView),
  '/ext/publicip': markRaw(PublicIpView),
  '/ext/portkill': markRaw(PortKillView),
  '/ext/wifi': markRaw(WifiView),
  '/ext/markeron': markRaw(MarkerOnView),
  '/logs': markRaw(LogsView),
  '/settings': markRaw(SettingsView),
  '/about': markRaw(AboutView),
}

// 路由与后端模块 ID 对应关系（用于路由切换时的零开销按需懒加载）
const ROUTE_MODULE_MAP: Record<string, string> = {
  '/frpc': 'frpc',
  '/ext/fileshare': 'fileshare',
  '/ext/memo': 'memo',
  '/ext/lan': 'lan',
  '/ext/portscan': 'portscan',
  '/ext/wechat': 'wechat',
  '/ext/publicip': 'publicip',
  '/ext/portkill': 'portkill',
  '/ext/wifi': 'wifi',
  '/ext/markeron': 'markeron',
}

const SYSTEM_NAV = [
  { route: '/', title: '首页', icon: '⌂' },
]

const BOTTOM_NAV = [
  { route: '/logs', title: '日志', icon: '📋' },
  { route: '/settings', title: '设置', icon: '⚙' },
  { route: '/about', title: '关于', icon: 'ⓘ' },
]

const navs = ref<NavEntry[]>([])
const activeRoute = ref('/')
const backendReady = ref(false)
let unlistenExtChanged: (() => void) | null = null
let unlistenNotify: (() => void) | null = null

async function refreshNavs() {
  try {
    const list = await AppAPI.AppService.GetNavs()
    navs.value = list ?? []

    // 如果当前所在路由对应的是已被禁用的扩展，则平滑重定向回首页
    const currentModID = ROUTE_MODULE_MAP[activeRoute.value]
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
  activeRoute.value = route
  const modID = ROUTE_MODULE_MAP[route]
  if (modID) {
    try {
      if ((AppAPI.AppService as any).EnsureModuleActive) {
        await (AppAPI.AppService as any).EnsureModuleActive(modID)
      }
    } catch (e) {
      console.warn(`Lazy activate module ${modID} failed:`, e)
    }
  }
}

const currentView = computed(() => {
  const v = CORE_VIEWS[activeRoute.value]
  if (v) return v
  if (navs.value.some(n => n.route === activeRoute.value)) return ExtPlaceholderView
  return HomeView
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
})
</script>

<template>
  <div class="layout">
    <!-- 左侧固定宽度侧边栏 -->
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark">HK</span>
        <div class="brand-info">
          <span class="brand-name">HubKit</span>
          <span class="brand-desc">开发网络工具箱</span>
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
        <div class="nav-group-label" v-if="navs.length > 0">功能模块</div>

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

        <div class="status-bar">
          <span class="status-dot" :class="{ online: backendReady }"></span>
          <span class="status-text">{{ backendReady ? '核心服务在线' : '连接核心中…' }}</span>
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
    </main>
  </div>
</template>

<style>
/* 全局浅色主题变量 */
:root {
  color-scheme: light;
  --bg-app: #f4f6f9;
  --bg-sidebar: #ffffff;
  --bg-hover: #f0f2f5;
  --bg-active: #e8f0fe;
  --border-color: #e4e7eb;
  --text-main: #1f2328;
  --text-muted: #656d76;
  --text-subtle: #8c959f;
  --accent: #2f6fed;
  --accent-hover: #1e5cd8;
  --danger: #cf222e;
  --success: #1a7f37;
}

.layout {
  display: flex;
  height: 100vh;
  width: 100vw;
  background: var(--bg-app);
  color: var(--text-main);
  overflow: hidden;
}

/* 侧边栏布局与视觉 */
.sidebar {
  width: 220px;
  flex: 0 0 220px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
  height: 100%;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 18px 14px;
  border-bottom: 1px solid var(--border-color);
}

.brand-mark {
  background: linear-gradient(135deg, #2f6fed 0%, #1a56cc 100%);
  color: #ffffff;
  border-radius: 7px;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 700;
  box-shadow: 0 2px 4px rgba(47, 111, 237, 0.2);
}

.brand-info {
  display: flex;
  flex-direction: column;
}

.brand-name {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-main);
  line-height: 1.2;
}

.brand-desc {
  font-size: 11px;
  color: var(--text-subtle);
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
  border-top: 1px solid var(--border-color);
  padding-bottom: 6px;
}

.nav-divider {
  height: 1px;
  background: var(--border-color);
  margin: 8px 4px 6px;
}

.nav-group-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-subtle);
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
  border-radius: 6px;
  color: var(--text-muted);
  font-size: 13px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: all 0.15s ease;
}

.nav-item:hover {
  background: var(--bg-hover);
  color: var(--text-main);
}

.nav-item.active {
  background: var(--bg-active);
  color: var(--accent);
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
  background: var(--danger);
  color: #ffffff;
  font-size: 10px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: 10px;
  line-height: 1.2;
}

/* 状态条 */
.status-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px 4px;
  font-size: 11px;
  color: var(--text-subtle);
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #d0d7de;
}

.status-dot.online {
  background: var(--success);
  box-shadow: 0 0 0 2px rgba(26, 127, 55, 0.15);
}

/* 内容主区域 */
.content-area {
  flex: 1;
  height: 100%;
  overflow-y: auto;
  padding: 24px 32px;
  background: var(--bg-app);
}

/* 通用页面规范样式 */
.page {
  max-width: 1000px;
  margin: 0 auto;
}

.page h1 {
  font-size: 20px;
  font-weight: 600;
  margin: 0 0 16px;
  color: var(--text-main);
}

.page p {
  line-height: 1.6;
  color: var(--text-muted);
  font-size: 13px;
}

/* 全局轻量 Toast 提示 */
.global-toast {
  position: fixed;
  top: 20px;
  right: 24px;
  background: rgba(31, 35, 40, 0.92);
  backdrop-filter: blur(8px);
  color: #ffffff;
  padding: 8px 16px;
  border-radius: 6px;
  font-size: 13px;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.18);
  animation: toastFadeIn 0.2s cubic-bezier(0.16, 1, 0.3, 1);
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
  transition: opacity 0.15s ease, transform 0.15s ease;
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
