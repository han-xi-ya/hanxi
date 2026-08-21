<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import * as AppAPI from '../bindings/hubkit/internal/app'
import type { NavEntry } from '../bindings/hubkit/internal/extapi/models'
import HomeView from './views/HomeView.vue'
import FrpcProjectsView from './views/FrpcProjectsView.vue'
import VersionsView from './views/VersionsView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'
import ExtPlaceholderView from './views/ExtPlaceholderView.vue'

// 工具箱模块化：所有能力模块一律来自后端 GetNavs()，无核心/扩展之分，平等展示。
// 系统页（首页/设置/关于）是界面骨架，不是能力模块，锚定在侧边栏首尾。
const CORE_VIEWS: Record<string, unknown> = {
  '/': HomeView,
  '/frpc': FrpcProjectsView,
  '/versions': VersionsView,
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
  } finally {
    backendReady.value = true
  }
})
</script>

<template>
  <div class="layout">
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark">DNK</span>
        <span class="brand-name">HubKit</span>
      </div>

      <nav class="nav">
        <button
          v-for="n in SYSTEM_NAV"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="activeRoute = n.route"
        >
          <span class="nav-icon">{{ n.icon }}</span>{{ n.title }}
        </button>

        <button
          v-for="n in navs"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="activeRoute = n.route"
        >
          <span class="nav-icon">{{ n.icon }}</span>{{ n.title }}
        </button>
      </nav>

      <nav class="nav nav-bottom">
        <button
          v-for="n in BOTTOM_NAV"
          :key="n.route"
          class="nav-item"
          :class="{ active: activeRoute === n.route }"
          @click="activeRoute = n.route"
        >
          <span class="nav-icon">{{ n.icon }}</span>{{ n.title }}
        </button>
      </nav>

      <div class="nav-foot">
        {{ backendReady ? '后端已连接' : '连接后端…' }}
      </div>
    </aside>

    <main class="content">
      <component :is="currentView" :title="currentExt?.title" />
    </main>
  </div>
</template>

<style>
:root {
  color-scheme: light;
  --bg: #f5f6f8;
  --bg-2: #ffffff;
  --bg-3: #eceff3;
  --border: #e2e5ea;
  --fg: #24292f;
  --fg-dim: #6b7280;
  --accent: #2f6fed;
  --danger: #d93025;
  --ok: #1a7f37;
}
* { box-sizing: border-box; }
html, body, #app { height: 100%; margin: 0; }
body {
  font-family: "Segoe UI", "Microsoft YaHei UI", "Microsoft YaHei", system-ui, sans-serif;
  background: var(--bg);
  color: var(--fg);
  font-size: 14px;
}
.layout { display: flex; height: 100%; }
.sidebar {
  width: 200px; flex: 0 0 200px;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  display: flex; flex-direction: column;
}
.brand { display: flex; align-items: center; gap: 8px; padding: 14px 16px; }
.brand-mark {
  background: var(--accent); color: #fff; border-radius: 6px;
  width: 26px; height: 26px; display: grid; place-items: center;
  font-size: 11px; font-weight: 700;
}
.brand-name { font-weight: 600; }
.nav { flex: 1; overflow-y: auto; padding: 4px 8px; }
.nav-bottom { flex: 0 0 auto; border-top: 1px solid var(--border); }
.nav-item {
  display: flex; align-items: center; gap: 8px; width: 100%;
  background: none; border: none; color: var(--fg);
  padding: 8px 10px; border-radius: 6px; cursor: pointer; font-size: 13px; text-align: left;
}
.nav-item:hover { background: var(--bg-3); }
.nav-item.active { background: #e8f0fe; color: var(--accent); font-weight: 600; }
.nav-icon { width: 18px; text-align: center; opacity: .8; }
.nav-foot { padding: 10px 16px; font-size: 11px; color: var(--fg-dim); }
.content { flex: 1; overflow-y: auto; padding: 24px 28px; }
.page h1 { margin: 0 0 12px; font-size: 20px; }
.page p { color: var(--fg-dim); line-height: 1.6; }
code { background: var(--bg-3); padding: 1px 6px; border-radius: 4px; }
.ok { color: var(--ok); } .off { color: var(--fg-dim); }
</style>