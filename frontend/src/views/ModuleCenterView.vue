<script setup lang="ts">
// 模块中心（Wave 2 MVP）：完整 Catalog 与管理入口的一级核心页（ADR-0001 §1.8）。
// 接线纪律：
//   - 数据全部来自 useModuleCatalog 合并投影（后端 Registry 是唯一真相），
//     页头计数与筛选分桶只做展示派生，操作成功后不本地 patch state，
//     统一依赖 SetModuleEnabled/SetModuleInstalled 广播的 ext:changed 重拉；
//   - 主操作零状态推断（只读 state.primaryAction/reason，文案走 status 词表）；
//   - builtin-logical 的安装/卸载口径如实呈现（§1.5：只动入口与凭据，不释放宿主体积）；
//   - 当前路由正被停用/卸载的模块无需特判：本页挂在 CORE_ROUTES 豁免清单
//     （navigation.isCoreRoute，App.vue refreshNavs 消费），模块中心自身永不被弹回；
//     用户若正停留在某模块页，该页弹回逻辑已由 App.vue 收口，本页不重复实现。
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { ModuleEntry } from '../composables/useModuleCatalog'
import { useModuleCatalog } from '../composables/useModuleCatalog'
import { useOperations } from '../composables/useOperations'
import { useConfirm } from '../composables/useConfirm'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { PRIMARY_ACTION_META } from '../constants/status'
import { MODULE_PRESENTATION } from '../constants/navigation'
import PageContainer from '../components/ui/PageContainer.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'
import AppIcon from '../components/ui/AppIcon.vue'
import ModuleCard from '../components/modules/ModuleCard.vue'
import ModuleDetailPanel from '../components/modules/ModuleDetailPanel.vue'
import OperationBanner from '../components/modules/OperationBanner.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { entries, loading, error, loaded, refresh } = useModuleCatalog()
// 在途操作投影：卡片徽标与动作钮禁用的输入源（只读复用，不新增第二份真相）
const { activeOf } = useOperations()
const { confirm } = useConfirm()
const { showToast, showErrorToast } = useToast()

// —— 筛选与搜索（展示派生，不改写投影）——
const FILTER_TABS: Array<{ key: string; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'installed', label: '已安装' },
  { key: 'installable', label: '可安装' },
  { key: 'fault', label: '异常' },
  { key: 'running', label: '运行中' },
]
const activeTab = ref<string>('all')
const keyword = ref('')

const summaryOf = (e: ModuleEntry) => String(e.state?.summary ?? '')
const healthOf = (e: ModuleEntry) => String(e.state?.health ?? '')
const isNotInstalled = (e: ModuleEntry) => summaryOf(e) === 'not-installed'
const isRunning = (e: ModuleEntry) => summaryOf(e) === 'running' || summaryOf(e) === 'running-update'
const isFault = (e: ModuleEntry) =>
  ['blocked', 'faulted'].includes(summaryOf(e))
  || ['corrupt', 'revoked'].includes(healthOf(e))

function matchesTab(e: ModuleEntry): boolean {
  switch (activeTab.value) {
    case 'installed': return !isNotInstalled(e)
    case 'installable': return isNotInstalled(e)
    case 'fault': return isFault(e)
    case 'running': return isRunning(e)
    default: return true
  }
}

const visibleEntries = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  return entries.value.filter((e) => {
    if (!matchesTab(e)) return false
    if (!kw) return true
    return (
      e.catalog.name.toLowerCase().includes(kw)
      || e.catalog.id.toLowerCase().includes(kw)
      || e.catalog.description.toLowerCase().includes(kw)
    )
  })
})

// 页头计数摘要：数字来自合并投影实时计算（展示派生，非第二份真相）
const counts = computed(() => ({
  total: entries.value.length,
  running: entries.value.filter(isRunning).length,
  notInstalled: entries.value.filter(isNotInstalled).length,
}))

// —— 操作接线：busy 单卡防重复，成功 toast，状态刷新交给 ext:changed ——
const busyIds = ref<Record<string, boolean>>({})

async function act(entry: ModuleEntry, run: () => Promise<unknown>, okMessage: string) {
  const id = entry.catalog.id
  if (busyIds.value[id]) return
  busyIds.value = { ...busyIds.value, [id]: true }
  try {
    await run()
    showToast(okMessage)
  } catch (err: unknown) {
    showErrorToast(`操作失败: ${getErrorMessage(err)}`)
  } finally {
    const next = { ...busyIds.value }
    delete next[id]
    busyIds.value = next
  }
}

const isBuiltinLogical = (e: ModuleEntry) => String(e.catalog.delivery) === 'builtin-logical'

/** builtin-logical 卸载确认（§1.5 强制口径）：如实列明移除范围与体积事实。 */
async function confirmUninstall(entry: ModuleEntry): Promise<boolean> {
  const name = entry.catalog.name
  if (!isBuiltinLogical(entry)) {
    return confirm({
      title: `卸载「${name}」？`,
      description: `将移除「${name}」的本机交付资产与功能入口。`,
      confirmLabel: '卸载',
      tone: 'danger',
    })
  }
  return confirm({
    title: `卸载「${name}」？`,
    description: '内建模块的卸载是逻辑卸载，移除的是功能入口与安装凭据，模块将从工作台与导航中退出。',
    confirmLabel: '卸载',
    tone: 'danger',
    details: [
      { label: '移除内容', value: '功能入口与安装凭据（receipt）' },
      { label: '个人数据', value: '默认保留，不随卸载删除' },
      { label: '程序体积', value: '内建代码随宿主发布，卸载后 hanxi.exe 体积不变' },
    ],
  })
}

function openModule(entry: ModuleEntry) {
  emit('navigate', MODULE_PRESENTATION[entry.catalog.id]?.route || '/')
}

/** 主操作分发：只按 state.primaryAction 语义路由到后端事务，零状态推断。 */
function handlePrimary(entry: ModuleEntry) {
  const action = String(entry.state?.primaryAction ?? 'none')
  const name = entry.catalog.name
  switch (action) {
    case 'install':
      void act(entry, () => AppAPI.AppService.SetModuleInstalled(entry.catalog.id, true),
        isBuiltinLogical(entry) ? `已将「${name}」加入工作台` : `已安装「${name}」`)
      break
    case 'uninstall':
      void (async () => {
        if (!(await confirmUninstall(entry))) return
        await act(entry, () => AppAPI.AppService.SetModuleInstalled(entry.catalog.id, false),
          `已卸载「${name}」，个人数据默认保留`)
      })()
      break
    case 'enable':
      void act(entry, () => AppAPI.AppService.SetModuleEnabled(entry.catalog.id, true), `已启用「${name}」`)
      break
    case 'disable':
      void act(entry, () => AppAPI.AppService.SetModuleEnabled(entry.catalog.id, false),
        `已停用「${name}」，已回收运行时资源`)
      break
    case 'open':
      openModule(entry)
      break
    case 'none':
      break // 按钮恒禁用，正常不可达；防御空点
    default: {
      // update/repair/retry 等操作面属 Wave 4+（后端尚无对应事务），如实告知不虚装
      const label = ((PRIMARY_ACTION_META as Record<string, { label: string }>)[action ?? '']?.label) || '该操作'
      showErrorToast(`「${name}」的${label}操作将在后续版本开放`)
    }
  }
}

function handleSetEnabled(entry: ModuleEntry, enabled: boolean) {
  const name = entry.catalog.name
  void act(entry, () => AppAPI.AppService.SetModuleEnabled(entry.catalog.id, enabled),
    enabled ? `已启用「${name}」` : `已停用「${name}」，已回收运行时资源`)
}

/** 次操作卸载（主操作未给 uninstall 时的行内入口），与主路径同一确认口径。 */
async function handleUninstall(entry: ModuleEntry) {
  if (!(await confirmUninstall(entry))) return
  await act(entry, () => AppAPI.AppService.SetModuleInstalled(entry.catalog.id, false),
    `已卸载「${entry.catalog.name}」，个人数据默认保留`)
}

// —— 详情抽屉：焦点进入/归还 + Esc/遮罩关闭（对齐 ConfirmDialog 基准）——
const selectedId = ref<string | null>(null)
const selectedEntry = computed(() =>
  entries.value.find((e) => e.catalog.id === selectedId.value) ?? null,
)
const drawer = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null

function openDetail(entry: ModuleEntry) {
  selectedId.value = entry.catalog.id
}
function closeDetail() {
  selectedId.value = null
}

function onDrawerKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    closeDetail()
    return
  }
  if (event.key !== 'Tab' || !drawer.value) return
  const focusable = Array.from(drawer.value.querySelectorAll<HTMLElement>(
    'button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
  ))
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

watch(selectedId, async (id) => {
  if (id) {
    previousFocus = document.activeElement as HTMLElement | null
    await nextTick()
    drawer.value?.focus()
    document.addEventListener('keydown', onDrawerKeydown)
  } else {
    document.removeEventListener('keydown', onDrawerKeydown)
    previousFocus?.focus()
    previousFocus = null
  }
})
onBeforeUnmount(() => document.removeEventListener('keydown', onDrawerKeydown))

const filtersDirty = computed(() => activeTab.value !== 'all' || keyword.value.trim() !== '')
function resetFilters() {
  activeTab.value = 'all'
  keyword.value = ''
}
</script>

<template>
  <PageContainer variant="workbench">
    <div class="modules-layout">
      <PageHeader
        title="模块中心"
        subtitle="完整模块目录与安装管理：统一呈现各模块的四维状态投影。"
      >
        <template #actions>
          <div class="header-actions">
            <span class="count-summary" role="status">{{ `共 ${counts.total} · 运行中 ${counts.running} · 未安装 ${counts.notInstalled}` }}</span>
            <UiButton small variant="ghost" :disabled="loading" title="重新拉取目录与状态投影" @click="refresh()">
              <AppIcon name="refresh-cw" /> 刷新
            </UiButton>
          </div>
        </template>
      </PageHeader>

      <!-- 首拉失败：错误框 + 重试（保留数据时的刷新失败走 stale 提示条） -->
      <div v-if="error && !loaded" class="error-box load-error" role="alert">
        <span>模块目录加载失败: {{ error }}</span>
        <UiButton small variant="secondary" @click="refresh()">重试</UiButton>
      </div>
      <div v-else-if="error && loaded" class="banner banner-error slim" role="alert">
        刷新失败，当前展示为上一次投影: {{ error }}
        <button type="button" class="state-action" @click="refresh()">重试</button>
      </div>

      <!-- 统一 Operation 呈现（Wave 4）：resumable 恢复条 / 在途进度条，位于错误框之后、筛选之前 -->
      <OperationBanner @navigate="(route) => emit('navigate', route)" />

      <div class="toolbar-row">
        <MainTabNav v-model="activeTab" :tabs="FILTER_TABS" label="模块状态筛选" />
        <div class="search-wrap">
          <AppIcon name="search" class="search-icon" />
          <input
            v-model="keyword"
            type="search"
            class="text-input search-input"
            placeholder="搜索名称 / ID / 描述"
            aria-label="按名称、ID 或描述搜索模块"
          />
        </div>
      </div>

      <!-- 骨架屏：仅首拉前展示，已有数据时刷新不闪骨架 -->
      <template v-if="loading && !loaded">
        <div class="cards-grid" aria-hidden="true">
          <div v-for="i in 6" :key="i" class="skeleton-card">
            <div class="sk-line sk-icon-row"><span class="sk-icon"></span><span class="sk-bar" style="width: 55%"></span></div>
            <span class="sk-bar" style="width: 90%"></span>
            <span class="sk-bar" style="width: 70%"></span>
            <span class="sk-bar sk-btn"></span>
          </div>
        </div>
        <p class="sr-only" role="status">正在加载模块目录…</p>
      </template>

      <template v-else-if="!error || loaded">
        <div v-if="visibleEntries.length" class="cards-grid">
          <ModuleCard
            v-for="entry in visibleEntries"
            :key="entry.catalog.id"
            :entry="entry"
            :busy="!!busyIds[entry.catalog.id]"
            :operation="activeOf(entry.catalog.id)"
            @primary="handlePrimary"
            @set-enabled="handleSetEnabled"
            @uninstall="handleUninstall"
            @detail="openDetail"
          />
        </div>

        <!-- 筛选/搜索无结果 -->
        <UiEmptyState v-else>
          <AppIcon name="search" :size="28" />
          <p>{{ filtersDirty ? '没有符合当前筛选或搜索条件的模块。' : '目录暂无模块。' }}</p>
          <UiButton v-if="filtersDirty" small variant="secondary" @click="resetFilters">清除筛选</UiButton>
        </UiEmptyState>
      </template>
    </div>

    <!-- 详情/诊断抽屉（模态面板，焦点收口） -->
    <Teleport to="body">
      <div v-if="selectedEntry" class="drawer-backdrop" @mousedown.self="closeDetail">
        <aside
          ref="drawer"
          class="detail-drawer"
          role="dialog"
          aria-modal="true"
          :aria-label="`模块详情：${selectedEntry.catalog.name}`"
          tabindex="-1"
        >
          <ModuleDetailPanel :entry="selectedEntry" @close="closeDetail" />
        </aside>
      </div>
    </Teleport>
  </PageContainer>
</template>

<style scoped>
.modules-layout {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header-actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; justify-content: flex-end; }
.count-summary {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  font-variant-numeric: tabular-nums;
}

.toolbar-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.search-wrap { position: relative; display: flex; align-items: center; flex: 1; min-width: 200px; max-width: 320px; }
.search-icon {
  position: absolute; left: 10px; pointer-events: none; color: var(--color-text-subtle);
}
.search-input { padding-left: 32px; background: var(--surface-panel); }
.search-input::-webkit-search-cancel-button { cursor: pointer; }

.load-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }

/* 响应式卡片网格：宽屏 3 列 / 中屏 2 列 / 窄屏 1 列（auto-fill 自然降档） */
.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
}
@media (max-width: 680px) {
  .cards-grid { grid-template-columns: 1fr; }
}

/* 骨架屏：复用全局 hx-pulse 活体动画（仅加载态允许持续动画） */
.skeleton-card {
  display: flex; flex-direction: column; gap: 10px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  padding: 14px 16px;
  animation: hx-pulse 1.8s ease-in-out infinite;
}
.sk-line { display: flex; align-items: center; gap: 10px; }
.sk-icon {
  width: 34px; height: 34px; border-radius: var(--radius-control);
  background: var(--surface-hover); flex: none;
}
.sk-bar { display: block; height: 12px; border-radius: var(--radius-pill); background: var(--surface-hover); }
.sk-btn { height: 28px; width: 96px; margin-top: 6px; border-radius: var(--radius-control); }

.sr-only {
  position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0;
  overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0;
}

/* 详情抽屉：右缘全高面板，细边框先行、阴影兜底 */
.drawer-backdrop {
  position: fixed; inset: 0; z-index: 1000;
  background: var(--overlay-mask);
  display: flex; justify-content: flex-end;
}
.detail-drawer {
  width: min(440px, 100%); height: 100%;
  border-left: 1px solid var(--color-border);
  box-shadow: var(--shadow-panel);
  outline: none;
  animation: drawerIn 160ms ease-out;
}
@keyframes drawerIn {
  from { transform: translateX(12px); opacity: 0.6; }
  to { transform: translateX(0); opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
  .skeleton-card { animation: none; }
  .detail-drawer { animation: none; }
}
</style>
