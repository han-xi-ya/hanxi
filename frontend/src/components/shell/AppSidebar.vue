<script setup lang="ts">
// 应用外壳双栏侧栏：一级图标轨道（AppNavRail）+ 二级分组面板。
// 纯展示容器：根节点仍为单元素 aside.sidebar（App 布局骨架 .layout 的第一子元素，
// 模板根级不得添加兄弟节点，否则外壳 DOM 结构漂移）；rail 与 panel 并排于其内。
//
// props/emits 在单栏版签名上向后兼容扩展：新增 activeGroup / runningIds 均可选，
// 路由/门禁/单例编排留在 App.vue。activeGroup 缺省时面板分类由 activeRoute 反推
// （点 rail 分类钮仅切面板、不换路由，产生临时 override，路由变化即复位）。
//
// 设置语境：activeRoute 落在 /settings*（且未被分组预览占用）时，面板切换为
// 设置分区菜单（constants/navigation.SETTINGS_SECTIONS 单一来源），点击分区
// 上抛 navigate 换子路由；rail 点分类仍走既有 override 预览语义，互不干扰。
//
// 图标双轨制（AppIcon 阶段1，docs/FRONTEND.md §8）：nav.icon 为 `i:` 前缀时渲染
// 内联 SVG，裸字符/emoji 走文本回退分支（后端 Icon 全量改写 i: 名后回退分支自然退役）。
//
// 窄屏降级（布局仍 CSS media query；开合焦点行为为组件内 JS，无新依赖）：
//   ≤1100px 二级面板整体隐藏，仅留 64px rail；点分类以 flyout 浮出——保持非模态
//   （点遮罩/导航即收不抢焦点），仅 Esc 关闭时把焦点归还触发的 rail 分组钮；
//   ≤760px rail 也收成 overlay 抽屉：rail 定位固定、translateX 移出（150ms 位移动画），
//   左上角浮出 ☰ 把手钮，开抽屉出遮罩，点遮罩/导航后自动收回（railOpen 组件本地态）。
//   抽屉键盘可达：打开时焦点进 rail 首个按钮，Esc/关闭后焦点归还把手。
//   prefers-reduced-motion 经 useMediaQuery 翻成 reduced-motion 类，位移过渡归零
//   （base.css 全局块是最后兜底，组件级类钩子让 shell 行为独立且可测）。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import AppIcon from '../ui/AppIcon.vue'
import AppNavRail from './AppNavRail.vue'
import { appIconName, resolveIcon } from '../../constants/appIcons'
import type { IconName, RenderableIcon } from '../../constants/icons'
import type { NavEntry } from '../../../bindings/hanxi/internal/extapi/models'
import {
  GROUP_META,
  SETTINGS_SECTIONS,
  settingsSectionOf,
  type NavGroup,
} from '../../constants/navigation'
import {
  favNavsOf,
  groupIconName,
  groupNavs,
  groupOfNav,
  isNavRunning,
  loadRecentRoutes,
  navsOfRoutes,
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
}>()

// ── 窄屏 overlay 抽屉与面板分类的组件本地态 ──
const railOpen = ref(false)
const groupOverride = ref<ShellGroup | null>(null)

// ≤1100px 面板由 CSS 隐藏：此时点 rail 分类不静默改状态，而是把面板以 flyout
// 浮层贴在 rail 右缘弹出（否则用户无从浏览/切换分组——窄屏盲点修复）。
const isNarrow = useMediaQuery('(max-width: 1100px)')
const panelFlyout = ref(false)
watch(isNarrow, (narrow) => {
  if (!narrow) panelFlyout.value = false // 回到宽屏：面板回归常驻列，浮层语义失效
})

// ── 键盘可达性：抽屉开合焦点流转 + Esc 关闭浮层（组件内逻辑，无新依赖） ──
const reduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')

/** aside 根节点与 ☰ 把手模板引用：焦点查询/归还的锚点 */
const sidebarEl = ref<HTMLElement | null>(null)
const handleEl = ref<HTMLElement | null>(null)
/** 弹出 flyout 的 rail 按钮：Esc 收回时焦点归还于此（shallowRef：DOM 节点不做深响应） */
const flyoutTrigger = shallowRef<HTMLElement | null>(null)

// 抽屉开：焦点进 rail 首个可交互按钮（把手之后的合理落点）；抽屉关：焦点归还把手。
// 遮罩/导航/点把手任意路径关闭均归还——关闭后 rail 移出画布，焦点若滞留即成暗礁。
watch(railOpen, async (open) => {
  if (open) {
    await nextTick() // 等 .rail-open 类落地再查询焦点目标
    sidebarEl.value?.querySelector<HTMLElement>('.rail button')?.focus()
  } else {
    handleEl.value?.focus()
  }
})

// flyout 经遮罩/导航等非 Esc 路径收回时清掉触发器引用，防陈旧节点滞留
watch(panelFlyout, (open) => {
  if (!open) flyoutTrigger.value = null
})

/** 按 aria-label 查 rail 内指定钮（分组钮/设置钮均可定位，标题无引号，选择器安全） */
function findRailButton(cls: string, label: string): HTMLElement | null {
  return sidebarEl.value?.querySelector<HTMLElement>(`.rail .${cls}[aria-label="${label}"]`) ?? null
}

function onGlobalKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' || e.isComposing) return
  if (e.defaultPrevented) return // 已被更上层浮层（如命令面板）消费则让行
  if (railOpen.value) {
    e.preventDefault()
    railOpen.value = false // 焦点归还把手由 railOpen watch 统一执行
  } else if (panelFlyout.value) {
    e.preventDefault()
    panelFlyout.value = false
    flyoutTrigger.value?.focus()
  }
}

onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKeydown))

// ── 二级面板折叠（纯视觉开关，localStorage 持久化，与 rail 展开态同哲学）──
const PANEL_COLLAPSED_KEY = 'hanxi.navPanelCollapsed'

function loadPanelCollapsed(): boolean {
  try {
    return localStorage.getItem(PANEL_COLLAPSED_KEY) === '1'
  } catch {
    return false // 存储异常按默认展开
  }
}

const panelCollapsed = ref(loadPanelCollapsed())

function togglePanel() {
  panelCollapsed.value = !panelCollapsed.value
  try {
    localStorage.setItem(PANEL_COLLAPSED_KEY, panelCollapsed.value ? '1' : '0')
  } catch {
    /* 存储不可用时仅本次会话生效 */
  }
}

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

// 设置分区语境：activeRoute 落在 /settings* 且面板未被 rail 分组预览占用时，
// 第二栏整体切换为设置分区菜单（SETTINGS_SECTIONS 单一来源）。
const settingsSection = computed(() => settingsSectionOf(props.activeRoute))
const settingsPanel = computed(() => settingsSection.value !== null && shownGroup.value === '')

const grouped = computed(() => groupNavs(navList.value))

// 当前分类的模块清单（rail 点组但组内为空时呈现空态文案）
const shownNavs = computed<NavEntryWithGroup[]>(() =>
  shownGroup.value === '' ? [] : grouped.value.get(shownGroup.value) ?? [],
)

// 面板头：设置态取"设置"；分类态取 GROUP_META（other 兜底组自配文案）；首页态取"工作台"
const panelMeta = computed(() => {
  if (settingsPanel.value) {
    return { title: '设置', desc: '偏好 · 系统管理 · 数据与存储', icon: 'gear' as IconName }
  }
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

// ── 首页态：常用 + 最近使用（清单与解析规则单一来源在 navGrouping，本组件只消费）──
const favNavs = computed(() => favNavsOf(navList.value))

const recentRoutes = ref<string[]>(loadRecentRoutes())
// 最近使用渲染时实时过滤：模块被禁用（navs 消失）后不再显示
const recentNavs = computed(() => navsOfRoutes(navList.value, recentRoutes.value))

// 最近使用仅记录模块路由（首页与核心页不占位）；未启用/已下架模块不会进入。
function onNavigate(route: string) {
  if (navList.value.some((n) => n.route === route)) recentRoutes.value = pushRecentRoute(route)
  railOpen.value = false // 窄屏抽屉内导航后自动收回
  panelFlyout.value = false // flyout 内导航后同样即点即收
  // 窄屏进设置：分区菜单以 flyout 随页弹出（否则第二栏不可见，无从切换分区）；
  // 分区之间互切（/settings/xxx）不再弹，让位给内容区。
  if (isNarrow.value && route === '/settings') {
    flyoutTrigger.value = findRailButton('rail-core', '设置') // Esc 收回时焦点归设置钮
    panelFlyout.value = true
  }
  emit('navigate', route)
}

function onSelectGroup(group: NavGroup) {
  groupOverride.value = group
  if (isNarrow.value) {
    flyoutTrigger.value = findRailButton('rail-group', GROUP_META[group].title)
    panelFlyout.value = true
    railOpen.value = false // ≤760 抽屉语境下让位给 flyout，避免双浮层叠罗汉
  }
}

// 行内图标三轨（N27 批 B）：`i:` 走 AppIcon 矢量、`app:` 走真图标、
// 其余文本回退（解析单点在 constants/appIcons，各消费面共用）。
function iconSvg(icon: string | undefined): RenderableIcon | null {
  return appIconName(icon) ?? null
}

// 文本轨三分（审查 shell#1）：appIconName 把"未登记 i:"与"裸 emoji"双双折叠
// 为 undefined，旧模板直出 {{ n.icon }} 会把 "i:xxx" 字面量写上屏。改走
// resolveIcon 四分：text→裸文本、blank（未登记 i:/空）→静默占位不上字面量。
function iconText(icon: string | undefined): string {
  const r = resolveIcon(icon)
  return r.kind === 'text' ? r.text : ''
}
</script>

<template>
  <aside
    ref="sidebarEl"
    class="sidebar"
    :class="{
      'rail-open': railOpen,
      'panel-collapsed': panelCollapsed,
      'panel-flyout': panelFlyout,
      'reduced-motion': reduceMotion,
    }"
  >
    <!-- ≤760px 浮出的抽屉把手（CSS 控制显隐，宽屏零占位；焦点在抽屉关闭后归还于此） -->
    <button
      ref="handleEl"
      class="rail-handle"
      :aria-label="railOpen ? '关闭导航' : '打开导航'"
      :aria-expanded="railOpen ? 'true' : 'false'"
      :title="railOpen ? '关闭导航' : '打开导航'"
      @click="railOpen = !railOpen"
    ><AppIcon name="menu" :size="16" aria-hidden="true" /></button>
    <div v-if="railOpen" class="rail-mask" @click="railOpen = false"></div>
    <!-- 窄屏 flyout 的全屏点击收回层（透明，不遮暗——面板是暂驻预览而非模态） -->
    <div v-if="panelFlyout" class="flyout-mask" @click="panelFlyout = false"></div>

    <!-- ① 一级图标轨道 -->
    <AppNavRail
      :active-group="shownGroup"
      :navs="navList"
      :active-route="activeRoute"
      :unread-count="unreadCount"
      :running-ids="runningIds"
      :panel-collapsed="panelCollapsed"
      @select-group="onSelectGroup"
      @navigate="onNavigate"
      @toggle-drawer="emit('toggle-drawer')"
      @toggle-panel="togglePanel"
    />

    <!-- ② 二级分组面板（238px；≤1100px 或折叠态由 CSS 隐藏，rail「展开面板」钮回展） -->
    <div class="nav-panel">
      <div class="panel-head">
        <span class="panel-head-icon"><AppIcon :name="panelMeta.icon" :size="16" /></span>
        <div class="panel-head-text">
          <div class="panel-title">{{ panelMeta.title }}</div>
          <div class="panel-sub">{{ panelMeta.desc }}</div>
        </div>
        <button
          class="panel-collapse-btn"
          title="收起分组面板"
          aria-label="收起分组面板"
          aria-expanded="false"
          @click="togglePanel"
        ><AppIcon name="chevrons-left" :size="15" /></button>
      </div>

      <div class="panel-list">
        <!-- 设置态：分区菜单（点击上抛 navigate 换子路由，内容区随之换页） -->
        <template v-if="settingsPanel">
          <div class="panel-section-label">设置分区</div>
          <button
            v-for="s in SETTINGS_SECTIONS"
            :key="s.id"
            class="nav-item mod"
            :class="{ active: settingsSection === s.id }"
            :title="`${s.title} · ${s.desc}`"
            :aria-current="settingsSection === s.id ? 'page' : undefined"
            @click="onNavigate(s.route)"
          >
            <span class="mod-icon">
              <AppIcon :name="s.icon" :size="15" />
            </span>
            <span class="mod-main">
              <span class="nav-text mod-name">{{ s.title }}</span>
              <span class="mod-sub">{{ s.desc }}</span>
            </span>
          </button>
        </template>

        <!-- 首页态：常用 + 最近使用 -->
        <template v-else-if="shownGroup === ''">
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
              <template v-else>{{ iconText(n.icon) }}</template>
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
              <template v-else>{{ iconText(n.icon) }}</template>
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
              <template v-else>{{ iconText(n.icon) }}</template>
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
          <span>{{ settingsPanel ? `${SETTINGS_SECTIONS.length} 个分区` : (shownGroup === '' ? navList.length : shownNavs.length) + ' 个模块' }}</span>
          <span v-if="!settingsPanel" class="foot-run"><b>{{ shownGroup === '' ? runningCountOf(navList, runningIds) : groupRunningCount }}</b> 运行中</span>
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
  /* 外壳层：双栏共用壳底，与 DWM 标题栏同色（flyout/抽屉浮层仍用 panel 保持抬起感） */
  background: var(--surface-chrome);
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
  padding: 14px 10px 10px 14px;
}

/* 面板头「收起」钮（折叠入口；回展入口在 rail-bottom） */
.panel-collapse-btn {
  margin-left: auto;
  flex: none;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: var(--radius-control);
  background: transparent;
  color: var(--color-text-subtle);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}

.panel-collapse-btn:hover {
  background: var(--surface-chrome-hover);
  color: var(--color-text);
}

/* 折叠态：面板整列收起，rail 的「展开分组面板」钮为唯一回展入口 */
.sidebar.panel-collapsed .nav-panel {
  display: none;
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
  font-size: var(--text-md);
  font-weight: 700;
  letter-spacing: 0.2px;
  color: var(--color-text);
  line-height: 1.3;
}

.panel-sub {
  font-size: var(--text-xs);
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
  font-size: var(--text-xs);
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
  background: var(--surface-chrome-hover);
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
  font-size: var(--text-base);
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
  font-size: var(--text-sm);
  font-weight: 600;
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 行副描述（设置分区行用；模块行留空零占位） */
.mod-sub {
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 行内运行绿点（状态嗅探接线前一般不出现）。
   N37②：原 `0 0 0 2.5px --surface-selected` 光环环系常态彩色 halo 同族（借
   surface token 兼职 halo 色，躲过 glow 关键词清查），按纪律废除——与
   .status-dot.online 同款收敛为实底 + inset 1px 提亮描边，两枚绿点口径归一。 */
.mod-run {
  margin-left: auto;
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--state-positive);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--state-positive) 45%, white);
  align-self: center;
}

.panel-hint,
.panel-empty {
  font-size: var(--text-xs);
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
  font-size: var(--text-micro);
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
  font-size: var(--text-xs);
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
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--state-positive) 45%, white);
}

/* ── 窄屏 overlay 抽屉件：宽屏零显示 ── */
.rail-handle {
  display: none;
}

.rail-mask {
  display: none;
}

/* flyout 收回层宽屏零占位（显隐与媒体查询内翻转为 block） */
.flyout-mask {
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

  /* 点分类后的 flyout：贴在 rail 右缘（sidebar 宽度由 rail 驱动，left:100% 自适应
     收起/展开两态），覆盖隐藏态；折叠钮不适用（flyout 即点即收，无需再折） */
  .sidebar.panel-flyout .nav-panel {
    display: flex;
    position: absolute;
    top: 0;
    bottom: 0;
    left: 100%;
    z-index: 1001;
    background: var(--surface-panel);
    border: 1px solid var(--color-border);
    border-left: none;
    border-radius: 0 var(--radius-element) var(--radius-element) 0;
    box-shadow: var(--shadow-panel);
  }

  .sidebar.panel-flyout .panel-collapse-btn {
    display: none;
  }

  .flyout-mask {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: transparent;
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
    font-size: var(--text-md);
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

/* coarse pointer：把手命中区提到 44px（与 rail-btn 同基线；base.css 的 button 全局
   min 兜底仍在，组件级显式声明防全局规则漂移）。置于 760 块之后：同权重后者优先） */
@media (pointer: coarse) {
  .rail-handle {
    width: 44px;
    height: 44px;
  }
}

/* 键盘可达性：抽屉/把手焦点环（覆盖 base.css 全局环为 --focus-ring 语义色；
   抽屉内 rail 钮焦点环在 AppNavRail 自身作用域） */
.rail-handle:focus-visible {
  outline: 2px solid var(--focus-ring, var(--color-primary));
  outline-offset: 2px;
}

/* reduced-motion 类钩子：抽屉滑入/面板开合的位移动画归零（useMediaQuery 驱动，
   可测且独立于 base.css 全局兜底）。0-3-0 权重稳定压过 760 块内 0-2-0 过渡声明 */
.sidebar.reduced-motion .rail,
.sidebar.reduced-motion .rail-handle {
  transition: none;
}
</style>
