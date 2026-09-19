<script setup lang="ts">
// 一级图标轨道（双栏外壳左列，收起 64px / 展开 188px，结构参照
// docs/design/shell-redesign/mockup-b-icon-rail.html）。
// 纯展示组件：首页 / 模块中心（一级核心入口，Wave 1 占位页，目录计数徽标留待
// Wave 2）/ 六个分类按钮（GROUP_META 驱动，模块数角标 + 组内运行绿点）/
// rail-bottom（展开收起钮、通知中心徽标、设置）。日志与关于入口、主题三态切换
// 已按用户决策收进设置页（views/settings/WorkbenchSection + ThemeSection），
// rail 只保留高频导航职责。设置钮对 /settings 及其全部子分区路由点亮。
// 全部导航状态经 props 注入、动作以 emits 上抛；分类切换只上抛 select-group，
// 面板联动与路由编排留在 AppSidebar / App.vue。唯一本地态是展开/收起
// （纯视觉宽度开关，localStorage 键 hanxi.railExpanded 持久化，不参与业务）。
//
// 激活态：surface-selected 底 + 左侧 3px 主色条（::before），图标转主色。
// 无障碍：原生 button 可 Tab；title 中文悬浮提示；aria-label 中文名；
// 当前分类 aria-current="page"；展开钮 aria-expanded；:focus-visible 内收焦点环；
// prefers-reduced-motion 翻 reduced-motion 类，宽度位移动画归零。
// 运行绿点为附加信息，颜色外以 title 说明。
import { computed, ref } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import AppIcon from '../ui/AppIcon.vue'
import type { IconName } from '../../constants/icons'
import {
  GROUP_META,
  type NavGroup,
} from '../../constants/navigation'
import {
  groupIconName,
  groupOfNav,
  isNavRunning,
  orderedRailGroups,
  type NavEntryWithGroup,
  type ShellGroup,
} from './navGrouping'

const props = withDefaults(defineProps<{
  /** 当前二级面板展示的分类；首页/核心页/无分类时为 ''。兜底组 'other' 无 rail 按钮，不点亮任何分类。 */
  activeGroup: ShellGroup | ''
  /** 后端注册表下发的导航项（分类计数与绿点据此计算） */
  navs: NavEntryWithGroup[]
  /** 当前激活路由：首页与 rail-bottom 核心页高亮据此判断 */
  activeRoute: string
  /** 未读通知数，>0 时通知按钮显示徽标 */
  unreadCount: number
  /** 运行中模块 ID 清单（状态嗅探接线前缺省空数组） */
  runningIds?: string[]
  /** 二级分组面板是否已折叠（AppSidebar 本地态）：折叠时 rail 浮出「展开面板」钮 */
  panelCollapsed?: boolean
}>(), {
  runningIds: () => [],
  panelCollapsed: false,
})

const emit = defineEmits<{
  (e: 'select-group', group: NavGroup): void
  (e: 'navigate', route: string): void
  (e: 'toggle-drawer'): void
  (e: 'toggle-panel'): void
}>()

// 核心固定入口：rail 底部区仅保留设置（日志/关于/主题已收进设置页）
const BOTTOM_NAV = [
  { route: '/settings', title: '设置', icon: 'gear' },
] as const

// 设置已拆分为 /settings/<分区> 子路由：本体与全部子段均点亮设置钮
function isCoreActive(route: string): boolean {
  return props.activeRoute === route || props.activeRoute.startsWith(`${route}/`)
}

interface RailGroupItem {
  key: NavGroup
  title: string
  desc: string
  icon: IconName
  count: number
  hasRunning: boolean
}

// GROUP_META 六组按 order 排序；每组统计 navs 归属数量与是否有运行模块。
// 归属走 groupOfNav 统一规则（后端 group 优先、route/id 回落），与二级面板严格一致。
const railGroups = computed<RailGroupItem[]>(() => {
  const running = props.runningIds
  return orderedRailGroups().map((key) => {
    const meta = GROUP_META[key]
    const members = props.navs.filter((n) => groupOfNav(n) === key)
    return {
      key,
      title: meta.title,
      desc: meta.desc,
      icon: groupIconName(meta.icon) as IconName,
      count: members.length,
      hasRunning: members.some((n) => isNavRunning(n, running)),
    }
  })
})

// ── 展开/收起（rail 唯一本地态：图标轨 ↔ 图标+文字两列宽）──
const RAIL_EXPANDED_KEY = 'hanxi.railExpanded'

function loadRailExpanded(): boolean {
  try {
    return localStorage.getItem(RAIL_EXPANDED_KEY) === '1'
  } catch {
    return false // 隐私模式等存储异常按默认收起
  }
}

const expanded = ref(loadRailExpanded())

// prefers-reduced-motion 类钩子：rail 宽度过渡（64↔188 位移）归零。
// 与 AppSidebar 的 reduced-motion 钩子同一套语义，base.css 全局块仅作最后兜底。
const reduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')

function toggleExpanded() {
  expanded.value = !expanded.value
  try {
    localStorage.setItem(RAIL_EXPANDED_KEY, expanded.value ? '1' : '0')
  } catch {
    /* 存储不可用时仅本次会话生效 */
  }
}
</script>

<template>
  <nav class="rail" :class="{ expanded, 'reduced-motion': reduceMotion }" aria-label="主导航">
    <button
      class="rail-btn rail-home"
      :class="{ active: activeRoute === '/' }"
      title="首页"
      aria-label="首页"
      :aria-current="activeRoute === '/' ? 'page' : undefined"
      @click="emit('navigate', '/')"
    >
      <AppIcon name="home" :size="20" />
      <span class="rail-label">首页</span>
    </button>

    <!-- 模块中心：一级核心入口（Wave 1 占位页；目录计数徽标属 Wave 2，现在不加） -->
    <button
      class="rail-btn rail-modules"
      :class="{ active: isCoreActive('/modules') }"
      title="模块中心"
      aria-label="模块中心"
      :aria-current="isCoreActive('/modules') ? 'page' : undefined"
      @click="emit('navigate', '/modules')"
    >
      <AppIcon name="grid" :size="20" />
      <span class="rail-label">模块中心</span>
    </button>

    <div class="rail-sep" role="presentation"></div>

    <button
      v-for="g in railGroups"
      :key="g.key"
      class="rail-btn rail-group"
      :class="{ active: activeGroup === g.key }"
      :title="`${g.title}（${g.count} 个模块${g.hasRunning ? '，有模块运行中' : ''}）`"
      :aria-label="g.title"
      :aria-current="activeGroup === g.key ? 'page' : undefined"
      @click="emit('select-group', g.key)"
    >
      <AppIcon :name="g.icon" :size="20" />
      <span class="rail-label">{{ g.title }}</span>
      <span v-if="g.count > 0" class="rail-count">{{ g.count }}</span>
      <span v-if="g.hasRunning" class="rail-dot" title="有模块运行中"></span>
    </button>

    <div class="rail-bottom">
      <!-- 面板折叠后的唯一回展入口（展开态由面板头部「收起面板」钮负责折叠） -->
      <button
        v-if="panelCollapsed"
        class="rail-btn panel-toggle"
        title="展开分组面板"
        aria-label="展开分组面板"
        aria-expanded="true"
        @click="emit('toggle-panel')"
      >
        <AppIcon name="chevrons-right" :size="20" />
      </button>

      <button
        class="rail-btn rail-toggle"
        :title="expanded ? '收起导航栏' : '展开导航栏'"
        :aria-label="expanded ? '收起导航栏' : '展开导航栏'"
        :aria-expanded="expanded ? 'true' : 'false'"
        @click="toggleExpanded"
      >
        <AppIcon :name="expanded ? 'rail-collapse' : 'rail-expand'" :size="20" />
      </button>

      <button
        class="rail-btn notif-nav-btn"
        title="通知中心"
        aria-label="通知中心"
        @click="emit('toggle-drawer')"
      >
        <AppIcon name="bell" :size="20" />
        <span class="rail-label">通知中心</span>
        <span v-if="unreadCount > 0" class="nav-badge">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
      </button>

      <button
        v-for="n in BOTTOM_NAV"
        :key="n.route"
        class="rail-btn rail-core"
        :class="{ active: isCoreActive(n.route) }"
        :title="n.title"
        :aria-label="n.title"
        :aria-current="isCoreActive(n.route) ? 'page' : undefined"
        @click="emit('navigate', n.route)"
      >
        <AppIcon :name="n.icon" :size="20" />
        <span class="rail-label">{{ n.title }}</span>
      </button>
    </div>
  </nav>
</template>

<style scoped>
/* 尺寸/激活态/徽标几何取自 mockup-b 草图，颜色全部走 tokens.css 语义变量。 */
.rail {
  width: 64px;
  flex: 0 0 64px;
  /* 外壳层：与 DWM 标题栏同色（--surface-chrome 双轴联动），与内容区分离 */
  background: var(--surface-chrome);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 0 8px;
  height: 100%;
  overflow-y: auto;
  scrollbar-width: none; /* 窄轨不出滚动条，溢出时仍可滚 */
  transition: width var(--motion-base) ease, flex-basis var(--motion-base) ease;
}

/* 展开态：图标+文字一行（首页/分类/核心页/主题均有 .rail-label），计数与徽标右移 */
.rail.expanded {
  width: 188px;
  flex-basis: 188px;
}

.rail.expanded .rail-btn {
  width: auto;
  align-self: stretch;
  margin: 0 8px 6px;
  justify-content: flex-start;
  padding: 0 12px;
  gap: 11px;
}

.rail-label {
  display: none;
  flex: 1;
  min-width: 0;
  text-align: left;
  font-size: var(--text-base);
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.rail.expanded .rail-label {
  display: block;
}

.rail.expanded .rail-count {
  position: static;
  margin-left: auto;
}

.rail.expanded .nav-badge {
  position: static;
  margin-left: auto;
  box-shadow: none;
  height: 16px;
}

/* 展开态按钮左缘距 rail 为 8px，激活指示条随之贴边 */
.rail.expanded .rail-btn.active::before {
  left: -8px;
}

.rail-sep {
  width: 26px;
  height: 1px;
  flex: none;
  background: var(--color-border);
  margin: 0 0 10px;
}

.rail-btn {
  position: relative;
  width: 44px;
  height: 44px;
  flex: none;
  border-radius: var(--radius-element);
  border: none;
  background: transparent;
  color: var(--color-text-subtle);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  margin-bottom: 6px;
  transition: background var(--motion-base) ease, color var(--motion-base) ease;
}

.rail-btn:hover {
  background: var(--surface-chrome-hover);
  color: var(--color-text);
}

.rail-btn.active {
  background: var(--surface-selected);
  color: var(--color-primary);
}

/* 激活指示条：rail 左右内边距恰为 10px，-10px 即贴 rail 左缘 */
.rail-btn.active::before {
  content: '';
  position: absolute;
  left: -10px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 20px;
  border-radius: 0 3px 3px 0;
  background: var(--color-primary);
}

/* 模块数角标（右下） */
.rail-count {
  position: absolute;
  right: 3px;
  bottom: 2px;
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  font-weight: 700;
  color: var(--color-text-subtle);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  padding: 0 4px;
  line-height: 12px;
}

/* 组内运行绿点（左上） */
.rail-dot {
  position: absolute;
  left: 4px;
  top: 4px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--state-positive);
  box-shadow: 0 0 0 2.5px var(--surface-chrome), 0 0 6px var(--state-positive-glow);
}

/* 未读徽标（右上） */
.nav-badge {
  position: absolute;
  right: 2px;
  top: 2px;
  min-width: 15px;
  height: 15px;
  border-radius: var(--radius-pill);
  background: var(--state-danger);
  color: var(--color-text-inverse);
  font-size: var(--text-micro);
  font-weight: 800;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 4px;
  line-height: 1;
  box-shadow: 0 0 0 2.5px var(--surface-chrome);
}

.rail-bottom {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
}

/* 键盘焦点环：rail 仅 64px 且纵向可滚，offset 内收防被轨道边缘裁切 */
.rail-btn:focus-visible {
  outline: 2px solid var(--focus-ring, var(--color-primary));
  outline-offset: -2px;
}

/* reduced-motion 类钩子：64↔188 宽度位移动画归零（hover 变色属着色过渡，保留） */
.rail.reduced-motion {
  transition: none;
}
</style>
