<script setup lang="ts">
// 工作台首页（Wave 2，UI 专项 §7.2/§7.4 + ADR-0001 §1.8）：
// 职责 = 运行摘要 + 常用入口 + 最近任务，不再承担完整模块目录与启停（归模块中心）。
// 数据铁律：全部区块只消费后端真实投影，无来源的分区隐藏并注释说明，禁止样例数据。
//   · 正在运行   AppService.ListModules()，initialized ∧ enabled = 已分配运行资源；
//   · 摘要四项   ListModules 真实计数 + 可用更新计数（W2b 点亮：useModuleCatalog
//                健康维度 health==='update-available' 真实投影；「待处理」仍无独立
//                来源，继续隐藏不占位）；
//   · 可用更新   update-available 条目合并列表行（有则显示、无则整区隐藏）；页头
//                「检查更新」触发 AppService.RefreshUpdates()（阻塞式一轮感知），
//                投影刷新由 updates:checked 驱动 useModuleCatalog 节流重拉，本页不重复拉；
//   · 常用入口   固定常用（navGrouping.FAV_MODULE_IDS 单一来源）+ 最近使用（loadRecentRoutes），
//                经后端 navs 实时过滤可见性（模块停用即消失），最多 4 个直达；
//   · 最近任务   HistoryService.List 三桶（ocr/portkill/envcheck）+ 统一 Operation
//                recentFinished 两源合并、按时间降序取最近 5 条，无数据整区隐藏（Wave 4）。
import { ref, shallowRef, computed, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import * as HistoryAPI from '../../bindings/hanxi/internal/history/historyservice'
import type { ModuleInfo, NavEntry, Operation } from '../../bindings/hanxi/internal/extapi/models'
import type { Record as HistoryRecord } from '../../bindings/hanxi/internal/history/models'
import type { AppInfo } from '../../bindings/hanxi/internal/app/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useOperations } from '../composables/useOperations'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useModuleCatalog, type ModuleEntry } from '../composables/useModuleCatalog'
import { MODULE_PRESENTATION, FALLBACK_MODULE_ICON } from '../constants/navigation'
import { ICON_NAMES, type IconName } from '../constants/icons'
import {
  SUMMARY_META,
  healthMeta,
  operationKindMeta,
  operationPhaseText,
  operationStatusMeta,
  updateAvailableText,
} from '../constants/status'
import {
  loadRecentRoutes,
  mergeFavRecentNavs,
  moduleIdOfNav,
  type NavEntryWithGroup,
} from '../components/shell/navGrouping'
import AppIcon from '../components/ui/AppIcon.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import PageContainer from '../components/ui/PageContainer.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { showToast, showErrorToast } = useToast()
// 统一 Operation 观察面（模块级单例）：首页"最近任务"取终态收口记录，
// 与历史三桶合并呈现；在途（queued/running）不进本列表（归模块中心在途条）。
const { recentFinished } = useOperations()

const modules = shallowRef<ModuleInfo[]>([])
const navs = shallowRef<NavEntry[]>([])
const appInfo = ref<AppInfo | null>(null)
const loading = ref(true)
const loadError = ref<string | null>(null)
const recentTasks = shallowRef<HistoryRecord[]>([])
const recentRoutes = ref<string[]>(loadRecentRoutes())

// ── 展示元数据工具（图标 `i:` 前缀双轨：注册过的走 AppIcon SVG，其余文本回退）──

function iconNameFromIconString(icon: string | undefined): IconName | undefined {
  if (!icon || !icon.startsWith('i:')) return undefined
  const name = icon.slice(2)
  return (ICON_NAMES as readonly string[]).includes(name) ? (name as IconName) : undefined
}

function getModuleIcon(id: string): string {
  return MODULE_PRESENTATION[id]?.icon || FALLBACK_MODULE_ICON
}

function moduleIconName(id: string): IconName | undefined {
  return iconNameFromIconString(getModuleIcon(id))
}

function getModuleRoute(id: string): string {
  if (MODULE_PRESENTATION[id]?.route) {
    return MODULE_PRESENTATION[id].route
  }
  const match = navs.value.find((n) => n.id === id || n.route.includes(id))
  return match?.route || '/'
}

function enterModule(m: ModuleInfo) {
  const route = getModuleRoute(m.id)
  if (route !== '/') emit('navigate', route)
}

// ── 核心投影：ListModules / GetNavs / GetAppInfo（ext:changed 热刷新复用同链路）──

async function loadCore() {
  try {
    const [mods, navList, info] = await Promise.all([
      AppAPI.AppService.ListModules(),
      AppAPI.AppService.GetNavs(),
      AppAPI.AppService.GetAppInfo()
    ])
    modules.value = mods ?? []
    navs.value = navList ?? []
    appInfo.value = info
    loadError.value = null
  } catch (err: unknown) {
    loadError.value = getErrorMessage(err)
    showToast(`获取工作台信息失败: ${getErrorMessage(err)}`)
  } finally {
    loading.value = false
  }
}

// 正在运行 = 已启用且已分配运行时资源（initialized）。单一口径，不在前端推导第二份状态。
const runningModules = computed(() => modules.value.filter((m) => m.enabled && m.initialized))
const enabledCount = computed(() => modules.value.filter((m) => m.enabled).length)

// ── 可用更新（W2b 更新感知链点亮）：唯一口径 = useModuleCatalog（ListModuleStates）
// 健康维度 health==='update-available' 真实投影（updatewatch 调度器裁决写入，
// 无真实来源时计数为 0、列表区整区隐藏，不放占位）。文案全部取自 status 词表。
const { entries: catalogEntries } = useModuleCatalog()
const updateEntries = computed(() =>
  catalogEntries.value.filter((e) => String(e.state?.health ?? '') === 'update-available'),
)
/** 健康维度词表条目：text「有可用更新」+ tone + icon（禁自造状态词）。 */
const updateHealth = healthMeta('update-available')

/** 更新行徽标短语：投影带 remoteVersion 时"有可用更新 → 新版本号"（与卡片
 * 健康徽标同一口径 updateAvailableText），无版本不编造、只显词表原文。 */
function updateChipText(e: ModuleEntry): string {
  return updateAvailableText(e.state?.remoteVersion)
}

/** 更新行直达：有登记路由进模块，否则回落模块中心（更新操作在模块中心/模块页处理）。 */
function gotoUpdate(e: ModuleEntry) {
  const route = getModuleRoute(e.catalog.id)
  emit('navigate', route !== '/' ? route : '/modules')
}

/** 更新行的运行/更新合并短语：直接取派生摘要键词表（SUMMARY_META），零本地推断。 */
function updateRowSummary(e: ModuleEntry): string {
  return (SUMMARY_META as Record<string, string>)[String(e.state?.summary ?? '')] ?? '状态未知'
}

// 页头「检查更新」：手动触发（或加入）一轮全量感知，阻塞式 RPC 不阻塞 UI——
// busy 防重入（后端 TTL 缓存 + single-flight 已不放大网络，这里只防连点态错乱）；
// 回执只报"本轮成功判定模块数"（后端如实口径），不推断有无更新；
// 投影刷新交给 updates:checked → useModuleCatalog 节流重拉，此处不重复拉。
const { busy: checkingUpdates, run: runCheckUpdates } = useAsyncAction()
async function checkUpdates() {
  if (checkingUpdates.value) return // 在途重放直接吞掉（disabled 已挡用户点击，此为防御性守卫）
  const res = await runCheckUpdates(() => AppAPI.AppService.RefreshUpdates())
  if (res.ok) {
    showToast(`检查更新完成：本轮成功判定 ${res.data} 个模块`)
  } else {
    showErrorToast(`检查更新失败: ${getErrorMessage(res.error)}`)
  }
}

// ── 最近任务：历史服务分桶 RPC（后端 Save 的 FuncType 与模块注册 ID 对齐，
// 当前写入方为识别 ocr / 查杀 portkill / 环境检测 npm envcheck 三桶）。
// TODO(Wave 4+)：后端统一 Operation 投影（PLAN_OFFICIAL_MODULE_DISTRIBUTION）落地后
// 改接统一真相源，新增历史桶需同步本清单（RPC 无跨桶列取方法，只能逐桶合并）。
const HISTORY_BUCKETS = ['ocr', 'portkill', 'envcheck']
const RECENT_TASKS_MAX = 5

async function loadRecentTasks() {
  const lists = await Promise.all(HISTORY_BUCKETS.map((bucket) => HistoryAPI.List(bucket, '').catch(() => null)))
  // 部分桶失败按空桶处理（局部数据缺失不炸首页）；id 全局单调递增，降序即最新在前。
  const merged = lists.flatMap((l) => l ?? [])
  merged.sort((a, b) => b.id - a.id)
  recentTasks.value = merged.slice(0, RECENT_TASKS_MAX)
}

function taskTitle(rec: HistoryRecord): string {
  return rec.summary || rec.input || '历史记录'
}

function taskIconName(rec: HistoryRecord): IconName {
  // 桶键对齐模块注册 ID，图标取自 MODULE_PRESENTATION；未登记桶回落 box（不引入 emoji 轨道）
  return iconNameFromIconString(MODULE_PRESENTATION[rec.funcType]?.icon) ?? 'box'
}

/** 紧凑任务时间：当天 HH:mm，同年 MM-DD HH:mm，更早仅日期。 */
function fmtTaskTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso.slice(0, 10)
  const pad = (n: number) => String(n).padStart(2, '0')
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  const now = new Date()
  if (d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()) {
    return hm
  }
  const md = `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  return d.getFullYear() === now.getFullYear() ? `${md} ${hm}` : `${d.getFullYear()}-${md}`
}

// ── 最近任务合并呈现（Wave 4）：历史三桶 + 统一 Operation 终态记录两源，
// 按时间降序取 5。历史行为不动；operation 行如实取后端投影（模块名回落 ID、
// 错误回落阶段短语），error.message 原文透出，不本地推断成败。
interface TaskRow {
  key: string
  iconName: IconName
  title: string
  meta: string
  time: string
}

function moduleNameOf(id: string): string {
  return modules.value.find((m) => m.id === id)?.name || id
}

function operationTitle(op: Operation): string {
  return `${moduleNameOf(op.moduleId)} · ${operationKindMeta(String(op.kind)).text}`
}

function operationMeta(op: Operation): string {
  if (op.error?.message) return op.error.message
  const phase = String(op.phase ?? '')
  return phase ? operationPhaseText(phase) : operationStatusMeta(String(op.status)).text
}

const taskRows = computed<TaskRow[]>(() => {
  const historyRows: TaskRow[] = recentTasks.value.map((rec) => ({
    key: `h-${rec.id}`,
    iconName: taskIconName(rec),
    title: taskTitle(rec),
    meta: rec.funcType,
    time: rec.createdAt,
  }))
  const operationRows: TaskRow[] = recentFinished(RECENT_TASKS_MAX).map((op) => ({
    key: `o-${op.id}`,
    iconName: operationStatusMeta(String(op.status)).icon,
    title: operationTitle(op),
    meta: operationMeta(op),
    time: op.finishedAt || op.startedAt,
  }))
  return [...historyRows, ...operationRows]
    .sort((a, b) => Date.parse(b.time) - Date.parse(a.time))
    .slice(0, RECENT_TASKS_MAX)
})

// ── 常用入口：固定常用 + 最近使用合并去重、按可见 navs 过滤，最多 4 个直达。
// 清单与合并规则单一来源在 navGrouping（mergeFavRecentNavs，AppSidebar 首页态同源），
// 本页仅做展示层富化（描述/图标回退），不得复制字面量。
const SHORTCUT_MAX = 4

interface ShortcutEntry {
  route: string
  title: string
  desc: string
  iconName?: IconName
  iconText: string
}

const shortcuts = computed<ShortcutEntry[]>(() =>
  mergeFavRecentNavs(navs.value as NavEntryWithGroup[], recentRoutes.value, SHORTCUT_MAX).map(
    (nav) => {
      const mod = modules.value.find((m) => m.id === moduleIdOfNav(nav))
      return {
        route: nav.route,
        title: nav.title,
        desc: mod?.description ?? '',
        iconName: iconNameFromIconString(nav.icon) ?? moduleIconName(mod?.id ?? ''),
        iconText: nav.icon || FALLBACK_MODULE_ICON,
      }
    },
  ),
)

useWailsEvent('ext:changed', () => {
  loadCore()
  loadRecentTasks()
})

onMounted(async () => {
  await loadCore()
  loadRecentTasks()
})
</script>

<template>
  <PageContainer variant="workbench">
    <section class="home-dashboard">
      <!-- 紧凑页头：eyebrow + 标题 + 一句现态说明；全局动作仅"管理模块" -->
      <header class="home-header">
        <div class="header-copy">
          <p class="eyebrow">本机工具概览</p>
          <div class="title-line">
            <h1>工作台</h1>
            <span v-if="appInfo" class="version-tag mono">v{{ appInfo.version }}</span>
          </div>
          <p class="page-note">聚合运行状态、最近任务与常用入口；完整模块管理请进入模块中心。</p>
        </div>
        <div class="header-actions">
          <UiButton
            class="check-updates-btn"
            :disabled="checkingUpdates"
            :title="checkingUpdates ? '上一轮检查仍在进行' : '手动触发一轮全量可用更新感知'"
            @click="checkUpdates"
          >
            <AppIcon name="refresh-cw" :size="14" /> {{ checkingUpdates ? '检查中…' : '检查更新' }}
          </UiButton>
          <UiButton class="modules-entry-btn" @click="emit('navigate', '/modules')">管理模块</UiButton>
        </div>
      </header>

      <!-- 摘要区：三项 ListModules 真实计数 + 可用更新计数（useModuleCatalog
           健康维度 update-available 真实投影，W2b 更新感知链点亮）。
           「待处理」仍无独立真实来源，继续隐藏不占位（UI 专项 §7.4）。 -->
      <div class="summary-grid" role="group" aria-label="工作台摘要">
        <div class="summary-card">
          <span class="summary-icon run"><AppIcon name="activity" :size="17" /></span>
          <span class="summary-label">正在运行</span>
          <strong class="summary-value mono">{{ runningModules.length }}</strong>
        </div>
        <div class="summary-card">
          <span class="summary-icon total"><AppIcon name="grid" :size="17" /></span>
          <span class="summary-label">模块总数</span>
          <strong class="summary-value mono">{{ modules.length }}</strong>
        </div>
        <div class="summary-card">
          <span class="summary-icon enabled"><AppIcon name="check-square" :size="17" /></span>
          <span class="summary-label">已启用</span>
          <strong class="summary-value mono">{{ enabledCount }}</strong>
        </div>
        <div class="summary-card">
          <span class="summary-icon update"><AppIcon :name="updateHealth.icon" :size="17" /></span>
          <span class="summary-label">可用更新</span>
          <strong class="summary-value mono">{{ updateEntries.length }}</strong>
        </div>
      </div>

      <div class="workspace-grid">
        <!-- 左列：正在运行（initialized ∧ enabled） -->
        <section class="wb-panel running-panel" aria-labelledby="running-title">
          <header class="wb-head">
            <h2 class="wb-title" id="running-title">正在运行</h2>
            <span class="count-tag mono">{{ runningModules.length }}</span>
          </header>

          <div v-if="loading" class="wb-state">正在加载工作台数据…</div>
          <div v-else-if="loadError" class="wb-state error">
            <span>工作台数据获取失败：{{ loadError }}</span>
            <UiButton small variant="ghost" @click="loadCore()">重试</UiButton>
          </div>
          <ul v-else-if="runningModules.length > 0" class="running-list">
            <li v-for="m in runningModules" :key="m.id">
              <button class="running-row" type="button" :title="`进入「${m.name}」`" @click="enterModule(m)">
                <span class="row-icon">
                  <AppIcon v-if="moduleIconName(m.id)" :name="moduleIconName(m.id)!" :size="16" />
                  <template v-else>{{ getModuleIcon(m.id) }}</template>
                </span>
                <span class="row-copy">
                  <span class="row-name">{{ m.name }}</span>
                  <span class="row-desc">{{ m.description }}</span>
                </span>
              </button>
            </li>
          </ul>
          <UiEmptyState v-else class="running-empty">
            <p>当前没有运行中的模块</p>
            <UiButton small variant="ghost" @click="emit('navigate', '/modules')">进入模块中心</UiButton>
          </UiEmptyState>
        </section>

        <!-- 右列：常用入口（固定 + 最近，最多 4，直达 navigate） -->
        <section class="wb-panel shortcuts-panel" aria-labelledby="shortcuts-title">
          <header class="wb-head">
            <h2 class="wb-title" id="shortcuts-title">常用入口</h2>
            <span class="wb-sub">固定与最近使用 · 最多 {{ SHORTCUT_MAX }} 个</span>
          </header>
          <nav v-if="shortcuts.length > 0" class="shortcut-grid" aria-label="常用工具">
            <button
              v-for="s in shortcuts"
              :key="s.route"
              class="shortcut"
              type="button"
              :title="`打开 ${s.title}`"
              @click="emit('navigate', s.route)"
            >
              <span class="sc-icon">
                <AppIcon v-if="s.iconName" :name="s.iconName" :size="16" />
                <template v-else>{{ s.iconText }}</template>
              </span>
              <span class="sc-name">{{ s.title }}</span>
              <span class="sc-desc">{{ s.desc }}</span>
              <span class="sc-hint">打开工具<AppIcon name="goto" :size="12" /></span>
            </button>
          </nav>
          <div v-else class="wb-state">暂无可用入口：启用模块或访问功能页后自动出现</div>
        </section>
      </div>

      <!-- 可用更新（W2b 点亮）：条目 = useModuleCatalog 中 health==='update-available'
           的真实投影（updatewatch 感知链裁决，updates:checked 驱动重拉）。
           行=模块名+派生摘要合并短语（SUMMARY_META：正在运行/有更新等口径）+健康徽标
           （投影带 remoteVersion 时含"→ 新版本号"短语，与卡片同口径）+直达；
           无条目整区隐藏，不放占位。 -->
      <section v-if="updateEntries.length > 0" class="wb-panel updates-panel" aria-labelledby="updates-title">
        <header class="wb-head">
          <h2 class="wb-title" id="updates-title">可用更新</h2>
          <span class="wb-sub">版本感知判定存在兼容更新，点击前往处理</span>
          <span class="count-tag mono">{{ updateEntries.length }}</span>
        </header>
        <ul class="update-list">
          <li v-for="e in updateEntries" :key="e.catalog.id">
            <button
              class="running-row update-row"
              type="button"
              :title="`前往「${e.catalog.name}」查看并更新`"
              @click="gotoUpdate(e)"
            >
              <span class="row-icon">
                <AppIcon v-if="moduleIconName(e.catalog.id)" :name="moduleIconName(e.catalog.id)!" :size="16" />
                <template v-else>{{ getModuleIcon(e.catalog.id) }}</template>
              </span>
              <span class="row-copy">
                <span class="row-name">{{ e.catalog.name }}</span>
                <span class="row-desc">{{ updateRowSummary(e) }}</span>
              </span>
              <UiStatusChip
                class="update-chip"
                :tone="updateHealth.tone"
                :title="updateChipText(e) !== updateHealth.text ? updateChipText(e) : undefined"
              >
                <AppIcon :name="updateHealth.icon" />
                <span class="update-chip-text">{{ updateChipText(e) }}</span>
              </UiStatusChip>
              <span class="row-goto"><AppIcon name="goto" :size="14" /></span>
            </button>
          </li>
        </ul>
      </section>

      <!-- 最近任务（Wave 4）：历史三桶 + 统一 Operation 终态两源按时间降序合并取 5；
           无数据（两源皆空或全部不可用）时整区隐藏，不放占位样例。
           完整任务历史与诊断归模块中心/各模块页。 -->
      <section v-if="taskRows.length > 0" class="wb-panel tasks-panel" aria-labelledby="tasks-title">
        <header class="wb-head">
          <h2 class="wb-title" id="tasks-title">最近任务</h2>
          <span class="wb-sub">操作与识别 · 端口查杀 · 环境检测记录，最新 {{ taskRows.length }} 条</span>
        </header>
        <ol class="task-list">
          <li v-for="row in taskRows" :key="row.key" class="task-row">
            <span class="task-icon"><AppIcon :name="row.iconName" :size="15" /></span>
            <span class="task-copy">
              <span class="task-title">{{ row.title }}</span>
              <span class="task-meta">{{ row.meta }}</span>
            </span>
            <time class="task-time mono" :datetime="row.time">{{ fmtTaskTime(row.time) }}</time>
          </li>
        </ol>
      </section>
    </section>
  </PageContainer>
</template>

<style scoped>
.home-dashboard {
  display: flex;
  flex-direction: column;
  gap: 16px;
  /* 宽度归 PageContainer（--container-workbench），本页不再私有 max-width */
}

/* ── 紧凑页头 ── */
.home-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

.header-copy {
  min-width: 0;
}

.eyebrow {
  margin: 0 0 2px;
  color: var(--color-text-subtle);
  font-size: var(--text-xs);
  font-weight: 650;
  letter-spacing: 0.04em;
}

.title-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.home-header h1 {
  font-size: var(--text-xl);
  font-weight: 700;
  margin: 0;
  letter-spacing: -0.01em;
}

.version-tag {
  font-size: var(--text-xs);
  color: var(--color-text-muted);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  padding: 2px 8px;
}

.page-note {
  margin: 4px 0 0;
  color: var(--color-text-muted);
  font-size: var(--text-sm);
  line-height: 1.5;
}

/* ── 页头动作组（检查更新 + 管理模块） ── */
.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

/* ── 摘要卡（四项：三项 ListModules 计数 + 可用更新健康投影计数；待处理仍隐藏） ── */
.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.summary-card {
  min-width: 0;
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 13px 14px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  box-shadow: var(--shadow-small);
}

.summary-icon {
  width: 34px;
  height: 34px;
  border-radius: var(--radius-control);
  display: flex;
  align-items: center;
  justify-content: center;
}

.summary-icon.run {
  background: var(--color-primary-soft);
  color: var(--color-primary);
}
.summary-icon.total {
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  color: var(--color-text-muted);
}
.summary-icon.enabled {
  background: var(--state-information-soft);
  color: var(--state-information);
}
/* 可用更新：tone 跟随 HEALTH_META['update-available']（information），文字标签承载语义 */
.summary-icon.update {
  background: var(--state-information-soft);
  color: var(--state-information);
}

.summary-label {
  color: var(--color-text-muted);
  font-size: var(--text-xs);
  font-weight: 650;
}

.summary-value {
  font-size: var(--text-2xl);
  font-weight: 700;
  line-height: 1;
}

/* ── 双列工作区：左=正在运行，右=常用入口；窄窗口单列 ── */
.workspace-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(280px, 0.75fr);
  gap: 16px;
  align-items: start;
}

.wb-panel {
  min-width: 0;
  overflow: hidden;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
  box-shadow: var(--shadow-small);
}

.wb-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--color-border);
}

.wb-title {
  margin: 0;
  font-size: var(--text-md);
  font-weight: 700;
  line-height: 1.3;
}

.wb-sub {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-subtle);
  font-size: var(--text-xs);
}

.count-tag {
  margin-left: auto;
  padding: 1px 8px;
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  color: var(--color-text-muted);
  font-size: var(--text-xs);
}

/* 加载 / 错误 / 局部空态共用虚线盒（.state-box 原子的面板内变体） */
.wb-state {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
  margin: 12px 14px;
  padding: 12px 14px;
  border: 1px dashed var(--color-border-strong);
  border-radius: var(--radius-control);
  background: var(--surface-soft);
  color: var(--color-text-muted);
  font-size: var(--text-sm);
}

.wb-state.error {
  border-color: var(--state-danger-glow);
  color: var(--state-danger);
}

/* ── 正在运行列表 ── */
.running-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.running-list li {
  border-bottom: 1px solid var(--color-border);
}

.running-list li:last-child {
  border-bottom: 0;
}

.running-row {
  width: 100%;
  min-height: 60px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  text-align: left;
  background: transparent;
  border: 0;
  cursor: pointer;
  color: var(--color-text);
  transition: background var(--motion-base) ease;
}

.running-row:hover {
  background: var(--surface-soft);
}

.row-icon {
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: var(--radius-control);
  background: var(--color-primary-soft);
  color: var(--color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--text-base);
}

.row-copy {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
}

.row-name {
  font-size: var(--text-sm);
  font-weight: 650;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-desc {
  margin-top: 2px;
  font-size: var(--text-xs);
  color: var(--color-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── 可用更新列表（行钮复用 .running-row 语法，右侧健康徽标 + 直达箭头） ── */
.update-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.update-list li {
  border-bottom: 1px solid var(--color-border);
}

.update-list li:last-child {
  border-bottom: 0;
}

.update-list .running-row {
  gap: 12px;
}

.update-chip {
  flex: none;
  max-width: 46%;
}
/* 徽标文字段（可能带"→ 新版本号"短语）：超长截断，全文由 title 承载 */
.update-chip-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-goto {
  flex: none;
  display: flex;
  align-items: center;
  color: var(--color-text-subtle);
}

.running-empty {
  margin: 14px;
  width: auto;
}
.running-empty p {
  margin: 0;
  color: var(--color-text-muted);
  font-size: var(--text-sm);
}

/* ── 常用入口 2×2 ── */
.shortcut-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  padding: 12px;
}

.shortcut {
  min-width: 0;
  min-height: 96px;
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 12px;
  text-align: left;
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  color: var(--color-text);
  cursor: pointer;
  transition:
    background var(--motion-base) ease,
    border-color var(--motion-base) ease;
}

.shortcut:hover {
  background: var(--surface-hover);
  border-color: var(--color-border-strong);
}

.sc-icon {
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: var(--radius-control);
  background: var(--color-primary-soft);
  color: var(--color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--text-base);
}

.sc-name {
  font-size: var(--text-sm);
  font-weight: 700;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sc-desc {
  font-size: var(--text-xs);
  color: var(--color-text-muted);
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.sc-hint {
  margin-top: auto;
  display: flex;
  align-items: center;
  gap: 3px;
  color: var(--color-primary);
  font-size: var(--text-xs);
  font-weight: 650;
}

/* ── 最近任务 ── */
.task-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.task-row {
  display: grid;
  grid-template-columns: 30px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  min-height: 52px;
  padding: 9px 14px;
  border-bottom: 1px solid var(--color-border);
}

.task-list li:last-child {
  border-bottom: 0;
}

.task-row:hover {
  background: var(--surface-soft);
}

.task-icon {
  width: 30px;
  height: 30px;
  border-radius: var(--radius-control);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  color: var(--color-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--text-base);
}

.task-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.task-title {
  font-size: var(--text-sm);
  font-weight: 650;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-meta {
  margin-top: 1px;
  color: var(--color-text-subtle);
  font-size: var(--text-xs);
}

.task-time {
  color: var(--color-text-subtle);
  font-size: var(--text-xs);
  white-space: nowrap;
}

/* ── 响应式重排：双列→单列；摘要三卡→一列；200% 缩放按同规则降级 ── */
@media (max-width: 1100px) {
  .workspace-grid {
    grid-template-columns: minmax(0, 1.3fr) minmax(260px, 0.8fr);
    gap: 14px;
  }
}

@media (max-width: 900px) {
  .workspace-grid {
    grid-template-columns: 1fr;
  }
  /* 摘要四卡中屏降为 2×2（KPI 4→2→1 档位） */
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .home-header {
    align-items: flex-start;
    flex-wrap: wrap;
  }
  .summary-grid {
    grid-template-columns: 1fr;
    gap: 8px;
  }
  .summary-card {
    min-height: 56px;
    padding: 10px 12px;
  }
}

@media (max-width: 520px) {
  .running-row {
    min-height: 54px;
    padding: 9px 12px;
  }
  .task-row {
    grid-template-columns: 30px minmax(0, 1fr);
    row-gap: 2px;
  }
  .task-time {
    grid-column: 2;
    justify-self: start;
  }
  .shortcut-grid {
    padding: 10px;
    gap: 8px;
  }
}

/* 粗指针：入口卡与行钮保持 44px+ 命中区 */
@media (pointer: coarse) {
  .shortcut {
    min-height: 116px;
  }
  .running-row {
    min-height: 64px;
  }
}
</style>
