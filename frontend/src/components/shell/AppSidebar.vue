<script setup lang="ts">
// 应用外壳侧栏展示层（Phase 6 收尾：App.vue 进一步组件化）。
// 纯展示组件：导航分组渲染、当前路由高亮、通知入口+未读徽标、主题循环切换钮、
// 底部状态条——全部状态经 props 注入、全部动作以 emits 上抛，路由/门禁/单例
// 编排留在 App.vue。根节点保持单元素 aside.sidebar（App 布局骨架 .layout 的第一子元素，
// 模板根级不得添加兄弟节点，否则外壳 DOM 结构漂移）。
// 图标双轨制（AppIcon 阶段1，docs/FRONTEND.md §8）：
// `i:` 前缀值经 AppIcon 渲染为内联 SVG；裸字符/emoji 走文本回退分支，
// 模块图标（constants/navigation.ts）按阶段 2/3 计划逐批改写为 `i:` 名。
import { computed } from 'vue'
import AppIcon from '../ui/AppIcon.vue'
import type { IconName } from '../../constants/icons'
import type { ThemeMode } from '../../composables/useTheme'
import type { NavEntry } from '../../../bindings/hanxi/internal/extapi/models'

const props = defineProps<{
  /** 后端注册表下发的工具模块导航项（扩展区） */
  navs: NavEntry[]
  /** 当前激活路由，用于导航项高亮 */
  activeRoute: string
  /** 未读通知数，>0 时显示徽标 */
  unreadCount: number
  /** 主题三态：跟随系统 / 浅色 / 深色 */
  themeMode: ThemeMode
  /** 后端初始化完成标志，驱动底部状态条 */
  backendReady: boolean
}>()

const emit = defineEmits<{
  (e: 'navigate', route: string): void
  (e: 'toggle-drawer'): void
  (e: 'cycle-theme'): void
}>()

// 静态导航清单（仅本组件渲染使用；动态扩展区来自 navs prop）
const SYSTEM_NAV = [
  { route: '/', title: '首页', icon: 'i:home' },
]

const BOTTOM_NAV = [
  { route: '/logs', title: '日志', icon: 'i:file-text' },
  { route: '/settings', title: '设置', icon: 'i:gear' },
  { route: '/about', title: '关于', icon: 'i:info' },
]

const themeModeLabel = computed(() => {
  switch (props.themeMode) {
    case 'system': return '跟随系统'
    case 'dark': return '深色主题'
    default: return '浅色主题'
  }
})

const themeGlyph = computed<IconName>(() => {
  switch (props.themeMode) {
    case 'system': return 'monitor'
    case 'dark': return 'moon'
    default: return 'sun'
  }
})
</script>

<template>
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
        @click="emit('navigate', n.route)"
      >
        <span class="nav-icon"><AppIcon v-if="n.icon.startsWith('i:')" :name="n.icon.slice(2) as IconName" :size="16" /><template v-else>{{ n.icon }}</template></span>
        <span class="nav-text">{{ n.title }}</span>
      </button>

      <div class="nav-divider" v-if="navs.length > 0"></div>
      <div class="nav-group-label" v-if="navs.length > 0">工具</div>

      <button
        v-for="n in navs"
        :key="n.route"
        class="nav-item"
        :class="{ active: activeRoute === n.route }"
        @click="emit('navigate', n.route)"
      >
        <span class="nav-icon"><AppIcon v-if="n.icon.startsWith('i:')" :name="n.icon.slice(2) as IconName" :size="16" /><template v-else>{{ n.icon }}</template></span>
        <span class="nav-text">{{ n.title }}</span>
      </button>
    </nav>

    <!-- 底部固定设置与状态 -->
    <nav class="nav-section bottom-nav">
      <!-- 通知中心入口按钮 -->
      <button
        class="nav-item notif-nav-btn"
        @click="emit('toggle-drawer')"
        title="通知中心"
      >
        <span class="nav-icon"><AppIcon name="bell" :size="16" /></span>
        <span class="nav-text">通知中心</span>
        <span v-if="unreadCount > 0" class="nav-badge">{{ unreadCount }}</span>
      </button>

      <button
        v-for="n in BOTTOM_NAV"
        :key="n.route"
        class="nav-item"
        :class="{ active: activeRoute === n.route }"
        @click="emit('navigate', n.route)"
      >
        <span class="nav-icon"><AppIcon v-if="n.icon.startsWith('i:')" :name="n.icon.slice(2) as IconName" :size="16" /><template v-else>{{ n.icon }}</template></span>
        <span class="nav-text">{{ n.title }}</span>
      </button>

      <!-- 主题循环切换：跟随系统 → 浅色 → 深色 -->
      <button
        class="nav-item theme-toggle"
        :title="`当前主题：${themeModeLabel}（点击循环切换）`"
        @click="emit('cycle-theme')"
      >
        <span class="nav-icon"><AppIcon :name="themeGlyph" :size="16" /></span>
        <span class="nav-text">{{ themeModeLabel }}</span>
      </button>

      <div class="status-bar">
        <span class="status-dot" :class="{ online: backendReady }"></span>
        <span class="status-text">{{ backendReady ? '工作台已就绪' : '正在加载工作台…' }}</span>
      </div>
    </nav>
  </aside>
</template>

<style scoped>
/* 侧栏布局类样式随标记自 App.vue 迁入（Phase 6 外壳组件化），
   设计 token 见 styles/tokens.css，类名与层级逐字保持不变。 */
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
</style>
