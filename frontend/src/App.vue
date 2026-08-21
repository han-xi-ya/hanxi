<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import * as AppAPI from '../bindings/hubkit/internal/app'
import type { NavEntry } from '../bindings/hubkit/internal/extapi/models'
import HomeView from './views/HomeView.vue'
import FrpcProjectsView from './views/FrpcProjectsView.vue'
import VersionsView from './views/VersionsView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'
import LanScannerView from './views/LanScannerView.vue'
import PublicIpView from './views/PublicIpView.vue'
import PortKillView from './views/PortKillView.vue'
import ExtPlaceholderView from './views/ExtPlaceholderView.vue'

// 核心与内置功能视图路由映射
const CORE_VIEWS: Record<string, unknown> = {
  '/': HomeView,
  '/frpc': FrpcProjectsView,
  '/versions': VersionsView,
  '/ext/lan': LanScannerView,
  '/ext/publicip': PublicIpView,
  '/ext/portkill': PortKillView,
  '/settings': SettingsView,
  '/about': AboutView,
}

const SYSTEM_NAV = [
  { route: '/', title: '首页', icon: '⌂' },
]

const BOTTOM_NAV = [
  { route: '/settings', title: '设置', icon: '⚙' },
  { route: '/about', title: '关于', icon: 'ⓘ' },
]

const navs = ref<NavEntry[]>([])
const activeRoute = ref('/')
const backendReady = ref(false)

const currentView = computed(() => {
  const v = CORE_VIEWS[activeRoute.value]
  if (v) return v
  if (navs.value.some(n => n.route === activeRoute.value)) return ExtPlaceholderView
  return HomeView
})

const currentExt = computed(() => navs.value.find(n => n.route === activeRoute.value))

onMounted(async () => {
  try {
    navs.value = (await AppAPI.AppService.GetNavs()) ?? []
  } catch (err) {
    console.error('Failed to load navigation:', err)
  } finally {
    backendReady.value = true
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
          @click="activeRoute = n.route"
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
          @click="activeRoute = n.route"
        >
          <span class="nav-icon">{{ n.icon }}</span>
          <span class="nav-text">{{ n.title }}</span>
        </button>
      </nav>

      <!-- 底部固定设置与状态 -->
      <nav class="nav-section bottom-nav">
        <button
          v-for="n in BOTTOM_NAV"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="activeRoute = n.route"
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
      <component :is="currentView" :title="currentExt?.title" />
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
</style>
