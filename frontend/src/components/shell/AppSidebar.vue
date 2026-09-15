<script setup lang="ts">
// 应用外壳双栏侧栏：一级图标轨道（AppNavRail）+ 二级分组面板。
// 纯展示容器：根节点仍为单元素 aside.sidebar（App 布局骨架 .layout 的第一子元素，
// 模板根级不得添加兄弟节点，否则外壳 DOM 结构漂移）；rail 与 panel 并排于其内。
//
// props/emits 在单栏版签名上向后兼容扩展：新增 activeGroup / runningIds 均可选，
// 路由/门禁/单例编排留在 App.vue。activeGroup 缺省时面板分类由 activeRoute 反推
// （点 rail 分类钮仅切面板、不换路由，产生临时 override，路由变化即复位）。
//
// 图标双轨制（AppIcon 阶段1，docs/FRONTEND.md §8）：nav.icon 为 `i:` 前缀时渲染
// 内联 SVG，裸字符/emoji 走文本回退分支（后端 Icon 全量改写 i: 名后回退分支自然退役）。
//
// 窄屏降级（CSS media query，无 JS 断点探测）：
//   ≤1100px 二级面板整体隐藏，仅留 64px rail；
//   ≤760px rail 也收成 overlay 抽屉：rail 定位固定、translateX 移出（150ms 位移动画），
//   左上角浮出 ☰ 把手钮，开抽屉出遮罩，点遮罩/导航后自动收回（railOpen 组件本地态）。
import { computed, ref, watch } from 'vue'
import AppIcon from '../ui/AppIcon.vue'
import AppNavRail from './AppNavRail.vue'
import type { IconName } from '../../constants/icons'
import type { ThemeMode } from '../../composables/useTheme'
import type { NavEntry } from '../../../bindings/hanxi/internal/extapi/models'
import {
  GROUP_META,
  MODULE_PRESENTATION,
  type NavGroup,
} from '../../constants/navigation'
import {
  groupIconName,
  groupNavs,
  groupOfNav,
  isNavRunning,
  loadRecentRoutes,
  moduleIdOfNav,
  pushRecentRoute,
  runningCountOf,
  type NavEntryWithGroup,
  type ShellGroup,
} from './navGrouping'

const props = withDefaults(defineProps<{
  /** 后端注册表下发的工具模块导航项（扩展区） */
  navs: NavEntry[]
  /** 当前激活路由，用于导航项高亮与面板分类反推 */
  activeRoute: string
  /** 未读通知数，>0 时显示徽标 */
  unreadCount: number
  /** 主题三态：跟随系统 / 浅色 / 深色 */
  themeMode: ThemeMode
  /** 后端初始化完成标志，驱动底部状态条 */
  backendReady: boolean
  /** 受控面板分类（App.vue 接线位）：缺省时由 activeRoute 反推 + 本地 override */
  activeGroup?: ShellGroup | ''
  /** 运行中模块 ID 清单（状态嗅探接线前缺省空数组，不新增 API 调用） */
  runningIds?: string[]
}>(), {
  activeGroup: undefined,
  runningIds: () => [],
})

const emit = defineEmits<{
  (e: 'navigate', route: string): void
  (e: 'toggle-drawer'): void
  (e: 'cycle-theme'): void
}>()

// ── 窄屏 overlay 抽屉与面板分类的组件本地态 ──
const railOpen = ref(false)
const groupOverride = ref<ShellGroup | null>(null)

const navList = computed(() => props.navs as NavEntryWithGroup[])

// 当前路由所属分类：命中 navs → 其分组；首页/核心页/未知 → ''（面板走首页态）
const routeGroup = computed<ShellGroup | ''>(() => {
  const active = navList.value.find((n) => n.route === props.activeRoute)
  return active ? groupOfNav(active) : ''
})

// 面板实际展示的分类：prop 受控 > rail 临时 override > 路由反推
const shownGroup = computed<ShellGroup | ''>(() =>
  props.activeGroup ?? groupOverride.value ?? routeGroup.value,
)

// 路由一旦变化，rail 的临时预览分类即失效，面板回归当前路由语境
watch(() => props.activeRoute, () => { groupOverride.value = null })

const grouped = computed(() => groupNavs(navList.value))

// 当前分类的模块清单（rail 点组但组内为空时呈现空态文案）
const shownNavs = computed<NavEntryWithGroup[]>(() =>
  shownGroup.value === '' ? [] : grouped.value.get(shownGroup.value) ?? [],
)

// 面板头：分类态取 GROUP_META（other 兜底组自配文案），首页态取"工作台"
const panelMeta = computed(() => {
  if (shownGroup.value === '') {
    return { title: '工作台', desc: '常用与最近使用的模块', icon: 'home' as IconName }
  }
  if (shownGroup.value === 'other') {
    return { title: '其他', desc: '未归类的扩展模块', icon: 'box' as IconName }
  }
  const meta = GROUP_META[shownGroup.value as NavGroup]
  return { title: meta.title, desc: meta.desc, icon: groupIconName(meta.icon) as IconName }
})

const groupRunningCount = computed(() => runningCountOf(shownNavs.value, props.runningIds))

// ── 首页态：常用（固定 3 项，模块未启用则跳过）+ 最近使用（localStorage）──
const FAV_MODULES = ['frpc', 'everything', 'snipaste'] as const

function navOfModuleId(id: string): NavEntryWithGroup | undefined {
  const route = MODULE_PRESENTATION[id]?.route
  return navList.value.find((n) => n.route === route) ?? navList.value.find((n) => moduleIdOfNav(n) === id)
}

const favNavs = computed(() =>
  FAV_MODULES.map((id) => navOfModuleId(id)).filter((n): n is NavEntryWithGroup => !!n),
)

const recentRoutes = ref<string[]>(loadRecentRoutes())
// 最近使用渲染时实时过滤：模块被禁用（navs 消失）后不再显示
const recentNavs = computed(() => {
  const byRoute = new Map(navList.value.map((n) => [n.route, n]))
  return recentRoutes.value
    .map((r) => byRoute.get(r))
    .filter((n): n is NavEntryWithGroup => !!n)
})

// 最近使用仅记录模块路由（首页与核心页不占位）；未启用/已下架模块不会进入。
function onNavigate(route: string) {
  if (navList.value.some((n) => n.route === route)) recentRoutes.value = pushRecentRoute(route)
  railOpen.value = false // 窄屏抽屉内导航后自动收回
  emit('navigate', route)
}

function onSelectGroup(group: NavGroup) {
  groupOverride.value = group
}

// 行内图标双轨：`i:` 前缀走 AppIcon，其余文本回退（同 rail/旧版约定）
function iconSvg(icon: string | undefined): IconName | null {
  return icon?.startsWith('i:') ? (icon.slice(2) as IconName) : null
}
</script>

<template>
  <aside class="sidebar" :class="{ 'rail-open': railOpen }">
    <!-- ≤760px 浮出的抽屉把手（CSS 控制显隐，宽屏零占位） -->
    <button
      class="rail-handle"
      :aria-label="railOpen ? '关闭导航' : '打开导航'"
      :aria-expanded="railOpen ? 'true' : 'false'"
      :title="railOpen ? '关闭导航' : '打开导航'"
      @click="railOpen = !railOpen"
    ><AppIcon name="menu" :size="16" aria-hidden="true" /></button>
    <div v-if="railOpen" class="rail-mask" @click="railOpen = false"></div>

    <!-- ① 一级图标轨道 -->
    <AppNavRail
      :active-group="shownGroup"
      :navs="navList"
      :active-route="activeRoute"
      :unread-count="unreadCount"
      :theme-mode="themeMode"
      :running-ids="runningIds"
      @select-group="onSelectGroup"
      @navigate="onNavigate"
      @toggle-drawer="emit('toggle-drawer')"
      @cycle-theme="emit('cycle-theme')"
    />

    <!-- ② 二级分组面板（238px；≤1100px 由 CSS 隐藏） -->
    <div class="nav-panel">
      <div class="panel-head">
        <span class="panel-head-icon"><AppIcon :name="panelMeta.icon" :size="16" /></span>
        <div class="panel-head-text">
          <div class="panel-title">{{ panelMeta.title }}</div>
          <div class="panel-sub">{{ panelMeta.desc }}</div>
        </div>
      </div>

      <div class="panel-list">
        <!-- 首页态：常用 + 最近使用 -->
        <template v-if="shownGroup === ''">
          <div class="panel-section-label">常用</div>
          <button
            v-for="n in favNavs"
            :key="`fav-${n.route}`"
            class="nav-item mod"
            :class="{ active: activeRoute === n.route }"
            :title="n.title"
            :aria-current="activeRoute === n.route ? 'page' : undefined"
            @click="onNavigate(n.route)"
          >
            <span class="mod-icon">
              <AppIcon v-if="iconSvg(n.icon)" :name="iconSvg(n.icon)!" :size="15" />
              <template v-else>{{ n.icon }}</template>
            </span>
            <span class="mod-main">
              <span class="nav-text mod-name">{{ n.title }}</span>
            </span>
            <span v-if="isNavRunning(n, runningIds)" class="mod-run" title="运行中"></span>
          </button>

          <div class="panel-section-label recent-label">最近使用</div>
          <button
            v-for="n in recentNavs"
            :key="`recent-${n.route}`"
            class="nav-item mod"
            :class="{ active: activeRoute === n.route }"
            :title="n.title"
            @click="onNavigate(n.route)"
          >
            <span class="mod-icon">
              <AppIcon v-if="iconSvg(n.icon)" :name="iconSvg(n.icon)!" :size="15" />
              <template v-else>{{ n.icon }}</template>
            </span>
            <span class="mod-main">
              <span class="nav-text mod-name">{{ n.title }}</span>
            </span>
            <span v-if="isNavRunning(n, runningIds)" class="mod-run" title="运行中"></span>
          </button>
          <div v-if="recentNavs.length === 0" class="panel-hint">切换模块后自动记录最近使用</div>
        </template>

        <!-- 分类态：当前组模块列表 -->
        <template v-else>
          <button
            v-for="n in shownNavs"
            :key="n.route"
            class="nav-item mod"
            :class="{ active: activeRoute === n.route }"
            :title="n.title"
            :aria-current="activeRoute === n.route ? 'page' : undefined"
            @click="onNavigate(n.route)"
          >
            <span class="mod-icon">
              <AppIcon v-if="iconSvg(n.icon)" :name="iconSvg(n.icon)!" :size="15" />
              <template v-else>{{ n.icon }}</template>
            </span>
            <span class="mod-main">
              <span class="nav-text mod-name">{{ n.title }}</span>
            </span>
            <span v-if="isNavRunning(n, runningIds)" class="mod-run" title="运行中"></span>
          </button>
          <div v-if="shownNavs.length === 0" class="panel-empty">
            <template v-if="!backendReady">正在加载模块清单…</template>
            <template v-else>该分类下暂无已启用模块，可在设置页开启</template>
          </div>
        </template>
      </div>

      <div class="panel-foot">
        <div class="foot-counts">
          <span>{{ shownGroup === '' ? navList.length : shownNavs.length }} 个模块</span>
          <span class="foot-run"><b>{{ shownGroup === '' ? runningCountOf(navList, runningIds) : groupRunningCount }}</b> 运行中</span>
        </div>
        <div class="status-bar">
          <span class="status-dot" :class="{ online: backendReady }"></span>
          <span class="status-text">{{ backendReady ? '工作台已就绪' : '正在加载工作台…' }}</span>
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/* 双栏容器骨架：rail(64px) + panel(238px) 并排于原 aside.sidebar 内，
   .layout 的"第一子元素"契约与 flex 语义不变（App.vue 零改动）。
   设计 token 见 styles/tokens.css。 */
/* 宽度由子列驱动（rail 可展开 64↔188，面板固定 238），容器不再锁死总宽 */
.sidebar {
  width: auto;
  flex: 0 0 auto;
  background: var(--surface-panel);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: row;
  height: 100%;
  position: relative;
}

/* ── 二级分组面板 ── */
.nav-panel {
  width: 238px;
  flex: 0 0 238px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
}

.panel-head {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 14px 10px;
}

.panel-head-icon {
  width: 28px;
  height: 28px;
  flex: none;
  border-radius: var(--radius-control);
  background: var(--color-primary-soft);
  color: var(--color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
}

.panel-head-text {
  min-width: 0;
}

.panel-title {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.2px;
  color: var(--color-text);
  line-height: 1.3;
}

.panel-sub {
  font-size: 11px;
  color: var(--color-text-subtle);
  margin-top: 2px;
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.panel-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
  min-height: 0;
}

.panel-section-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--color-text-subtle);
  letter-spacing: 0.5px;
  padding: 8px 6px 4px;
}

.recent-label {
  margin-top: 6px;
  border-top: 1px solid var(--color-border);
}

/* 模块行：30px 图标盒 + 名称（描述槽位留待后端 desc 字段，先以 title 兜底） */
.mod {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  text-align: left;
  background: transparent;
  border: 1px solid transparent;
  border-radius: var(--radius-element);
  padding: 8px 10px;
  margin-bottom: 3px;
  color: var(--color-text-muted);
  cursor: pointer;
  transition: background var(--motion-base) ease, border-color var(--motion-base) ease, color var(--motion-base) ease;
}

.mod:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.mod.active {
  background: var(--surface-selected);
  border-color: color-mix(in srgb, var(--color-primary) 35%, transparent);
  color: var(--color-text);
}

.mod-icon {
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: var(--radius-control);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: var(--color-text-muted);
}

.mod.active .mod-icon {
  background: var(--color-primary-soft);
  border-color: color-mix(in srgb, var(--color-primary) 30%, transparent);
  color: var(--color-primary);
}

.mod-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.mod-name {
  font-size: 12.5px;
  font-weight: 600;
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 行内运行绿点（状态嗅探接线前一般不出现） */
.mod-run {
  margin-left: auto;
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--state-positive);
  box-shadow: 0 0 0 2.5px var(--surface-selected);
  align-self: center;
}

.panel-hint,
.panel-empty {
  font-size: 11px;
  color: var(--color-text-subtle);
  padding: 10px 8px;
  line-height: 1.5;
}

.panel-empty {
  border: 1px dashed var(--color-border-strong);
  border-radius: var(--radius-control);
  margin-top: 6px;
}

.panel-foot {
  flex: none;
  border-top: 1px solid var(--color-border);
  padding: 6px 14px 8px;
}

.foot-counts {
  display: flex;
  gap: 10px;
  font-size: 10.5px;
  color: var(--color-text-subtle);
  padding-top: 4px;
}

.foot-run b {
  color: var(--state-positive);
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

/* 状态条（自单栏版逐字保留） */
.status-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
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

/* ── 窄屏 overlay 抽屉件：宽屏零显示 ── */
.rail-handle {
  display: none;
}

.rail-mask {
  display: none;
}

/* ≤1100px：隐藏二级面板，仅留 64px rail */
@media (max-width: 1100px) {
  .sidebar {
    border-right: none; /* 分界由 rail 自带右边框呈现，避免双线；宽度随 rail 展开态自适应 */
  }

  .nav-panel {
    display: none;
  }
}

/* ≤760px：rail 收成 overlay 抽屉，☰ 把手浮出 */
@media (max-width: 760px) {
  .sidebar {
    width: 0;
    flex-basis: 0;
  }

  .rail-handle {
    position: fixed;
    top: 10px;
    left: 10px;
    z-index: 999;
    width: 36px;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-control);
    border: 1px solid var(--color-border);
    background: var(--surface-panel);
    color: var(--color-text-muted);
    font-size: 15px;
    cursor: pointer;
    box-shadow: var(--shadow-small);
    transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
  }

  .rail-handle:hover {
    background: var(--surface-hover);
    color: var(--color-text);
  }

  .rail-mask {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 999;
    background: var(--overlay-mask);
  }

  /* rail 子组件根节点：移出画布，开抽屉时滑入（AppNavRail scoped 样式选择器
     权重 0-1-0，父作用域 0-2-0 稳定覆盖；transform 动画 150ms=--motion-base） */
  .sidebar .rail {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    z-index: 1000;
    transform: translateX(-100%);
    transition: transform var(--motion-base) ease;
    box-shadow: var(--shadow-panel);
  }

  .sidebar.rail-open .rail {
    transform: translateX(0);
  }
}
</style>
