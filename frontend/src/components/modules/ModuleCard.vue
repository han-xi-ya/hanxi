<script setup lang="ts">
// 模块中心卡片（Wave 2）：静态身份（catalog）+ 四维投影（state）的展示单元。
// 契约纪律（ADR-0001 §1.2）：主操作只读 state.primaryAction、原因只读 state.reason，
// 卡片零 if/else 状态推断；文案全部取自 constants/status 词汇表。唯一例外是
// builtin-logical 的"安装"措辞收口（§1.5：交付形态强制"加入工作台，不释放宿主体积"），
// 属于词汇表在 delivery kind 维度的如实化，不是状态推断。
import { computed } from 'vue'
import type { Operation } from '../../../bindings/hanxi/internal/extapi/models'
import type { ModuleEntry } from '../../composables/useModuleCatalog'
import {
  PRIMARY_ACTION_META,
  SUMMARY_META,
  deliveryMeta,
  healthMeta,
  operationPhaseText,
  operationStatusMeta,
  runtimeMeta,
  type PrimaryActionMeta,
  type StateTone,
  type SummaryKeyValue,
} from '../../constants/status'
import { GROUP_META, MODULE_PRESENTATION, FALLBACK_MODULE_ICON } from '../../constants/navigation'
import { ICON_NAMES, type IconName } from '../../constants/icons'
import AppIcon from '../ui/AppIcon.vue'
import UiButton from '../ui/UiButton.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'

const props = withDefaults(defineProps<{
  entry: ModuleEntry
  /** 后端事务在途：全部动作钮禁用（维度稳定，不跳字）。 */
  busy?: boolean
  /**
   * 该模块的在途 Operation（Wave 4，来自 useOperations.activeOf，无则 null）。
   * 呈现复用：badge-row 增"在途操作"徽标 + busy 并入（操作期间动作钮禁用），
   * 不新增状态真相——activeOf 只是 useOperations 投影的只读筛选。
   */
  operation?: Operation | null
}>(), { busy: false, operation: null })

const emit = defineEmits<{
  /** 主操作（动作语义由父级按 state.primaryAction 分发，卡片不发起 RPC）。 */
  primary: [entry: ModuleEntry]
  /** 次操作：启用/停用（enabled 为目标态）。 */
  setEnabled: [entry: ModuleEntry, enabled: boolean]
  /** 次操作：卸载（确认流程在父级）。 */
  uninstall: [entry: ModuleEntry]
  /** 详情/诊断入口。 */
  detail: [entry: ModuleEntry]
}>()

const DELIVERY_KIND_BUILTIN_LOGICAL = 'builtin-logical'
/** 交付维度的瞬时/异常态：摘要短语未覆盖时用 DELIVERY_META 徽标补呈现。 */
const TRANSIENT_DELIVERY = new Set(['installing', 'updating', 'removing', 'repairing', 'orphaned'])

/** 摘要键 → 徽标色调：语义色仅叠加在文字之上，不替代文字。 */
const SUMMARY_TONE: Record<SummaryKeyValue, StateTone> = {
  'not-installed': 'neutral',
  'installed-disabled': 'neutral',
  'installed-enabled': 'neutral',
  running: 'positive',
  'running-update': 'information',
  blocked: 'danger',
  faulted: 'danger',
  'in-progress': 'information',
}

const state = computed(() => props.entry.state)
const deliveryKind = computed(() => String(props.entry.catalog.delivery))
const isBuiltinLogical = computed(() => deliveryKind.value === DELIVERY_KIND_BUILTIN_LOGICAL)

// —— 主操作：零状态推断，映射 PRIMARY_ACTION_META ——
const actionKey = computed(() => String(state.value?.primaryAction ?? 'none'))
const actionMeta = computed<PrimaryActionMeta>(
  () => (PRIMARY_ACTION_META as Record<string, PrimaryActionMeta>)[actionKey.value]
    ?? PRIMARY_ACTION_META.none,
)
const primaryLabel = computed(() => {
  if (actionKey.value === 'install' && isBuiltinLogical.value) return '加入工作台'
  return actionMeta.value.label
})
const primaryTitle = computed(() => {
  if (actionKey.value === 'none') return state.value?.reason || '当前无可用操作'
  if (actionKey.value === 'install' && isBuiltinLogical.value) return '加入工作台（不影响主程序体积）'
  return undefined
})
// busy 并入：在途 Operation 期间同样禁用全部动作钮（呈现复用，真相仍是 operation 投影）
const locked = computed(() => props.busy || !!props.operation)
const primaryDisabled = computed(() => actionMeta.value.disabled || locked.value)

// —— 状态区：摘要短语 + 次要徽标（runtime/health/瞬时 delivery），全部文字承载 ——
const summaryKey = computed(() => String(state.value?.summary ?? ''))
const summaryText = computed(() =>
  (SUMMARY_META as Record<string, string>)[summaryKey.value] || '状态未知',
)
const summaryTone = computed<StateTone>(() =>
  SUMMARY_TONE[summaryKey.value as SummaryKeyValue] ?? 'neutral',
)
const transientDelivery = computed(() => {
  const s = state.value
  if (!s) return null
  const delivery = String(s.delivery)
  return TRANSIENT_DELIVERY.has(delivery) ? deliveryMeta(delivery) : null
})
const runtimeBadge = computed(() => {
  const s = state.value
  return s && String(s.runtime) !== 'inactive' ? runtimeMeta(String(s.runtime)) : null
})
const healthBadge = computed(() => {
  const s = state.value
  return s && String(s.health) !== 'current' ? healthMeta(String(s.health)) : null
})
const reasonText = computed(() => state.value?.reason || '')

// —— 在途操作徽标（Wave 4）：有 active Operation → phase 短语 + 进度百分比，文字承载 ——
const operationBadge = computed(() => {
  const op = props.operation
  if (!op) return null
  const status = operationStatusMeta(String(op.status))
  const phase = String(op.phase ?? '')
  const label = phase ? operationPhaseText(phase) : status.text
  const pct = op.progress == null ? '' : ` ${Math.round(op.progress)}%`
  return { icon: status.icon, tone: status.tone, text: `${label}${pct}` }
})

// —— 次操作可见性（呈现规则，非主操作推断）：与主按钮动作重合时隐藏去重 ——
const toggleTo = computed<boolean | null>(() => {
  const s = state.value
  if (!s || String(s.delivery) !== 'installed') return null
  if (String(s.policy) === 'enabled') return false
  if (String(s.policy) === 'disabled') return true
  return null // mandatory/blocked/pending-consent 不提供本地开关入口
})
const showToggle = computed(() => {
  if (toggleTo.value === null) return false
  return !(toggleTo.value && actionKey.value === 'enable') && !(toggleTo.value === false && actionKey.value === 'disable')
})
const showUninstall = computed(() => {
  const s = state.value
  return !!s
    && String(s.delivery) === 'installed'
    && String(s.policy) !== 'mandatory'
    && actionKey.value !== 'uninstall'
})

// —— 展示身份：图标/分类（与首页共用 navigation 单一来源）——
const iconName = computed<IconName | undefined>(() => {
  const icon = MODULE_PRESENTATION[props.entry.catalog.id]?.icon || FALLBACK_MODULE_ICON
  if (!icon.startsWith('i:')) return undefined
  const name = icon.slice(2)
  return (ICON_NAMES as readonly string[]).includes(name) ? (name as IconName) : undefined
})
const categoryLabel = computed(() =>
  (GROUP_META as Partial<Record<string, { title: string }>>)[props.entry.catalog.category]?.title
  ?? props.entry.catalog.category,
)
</script>

<template>
  <article class="module-card" :class="{ 'is-busy': locked }">
    <div class="card-head">
      <div class="icon-wrap">
        <AppIcon v-if="iconName" :name="iconName" :size="18" />
        <AppIcon v-else name="box" :size="18" />
      </div>
      <div class="head-text">
        <h3 class="mod-name" :title="entry.catalog.name">{{ entry.catalog.name }}</h3>
        <span class="mod-category">{{ categoryLabel }}</span>
      </div>
      <UiStatusChip class="summary-chip" :tone="summaryTone">{{ summaryText }}</UiStatusChip>
    </div>

    <p class="mod-desc" :title="entry.catalog.description || entry.catalog.name">
      {{ entry.catalog.description || '（暂无描述）' }}
    </p>

    <div v-if="transientDelivery || runtimeBadge || healthBadge || operationBadge" class="badge-row">
      <UiStatusChip v-if="operationBadge" :tone="operationBadge.tone" title="该模块存在未收口的操作事务">
        <AppIcon :name="operationBadge.icon" /> {{ operationBadge.text }}
      </UiStatusChip>
      <UiStatusChip v-if="transientDelivery" :tone="transientDelivery.tone">
        <AppIcon :name="transientDelivery.icon" /> {{ transientDelivery.text }}
      </UiStatusChip>
      <UiStatusChip v-if="runtimeBadge" :tone="runtimeBadge.tone">
        <AppIcon :name="runtimeBadge.icon" /> {{ runtimeBadge.text }}
      </UiStatusChip>
      <UiStatusChip v-if="healthBadge" :tone="healthBadge.tone">
        <AppIcon :name="healthBadge.icon" /> {{ healthBadge.text }}
      </UiStatusChip>
    </div>

    <p v-if="reasonText" class="card-reason">{{ reasonText }}</p>

    <div class="card-actions">
      <UiButton
        class="primary-btn"
        small
        :variant="actionMeta.variant"
        :disabled="primaryDisabled"
        :title="primaryTitle"
        @click="emit('primary', entry)"
      >{{ primaryLabel }}</UiButton>
      <div class="secondary-row">
        <UiButton small variant="ghost" :disabled="locked" @click="emit('detail', entry)">详情</UiButton>
        <UiButton
          v-if="showToggle"
          small
          variant="secondary"
          :disabled="locked"
          @click="emit('setEnabled', entry, toggleTo === true)"
        >{{ toggleTo === true ? '启用' : '停用' }}</UiButton>
        <UiButton
          v-if="showUninstall"
          small
          variant="danger"
          :disabled="locked"
          @click="emit('uninstall', entry)"
        >卸载</UiButton>
      </div>
    </div>
  </article>
</template>

<style scoped>
.module-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  padding: 14px 16px;
  transition: border-color var(--motion-base) ease;
}
.module-card:hover { border-color: var(--color-border-strong); }
.module-card.is-busy { opacity: 0.75; }

.card-head { display: flex; align-items: flex-start; gap: 10px; }
.icon-wrap {
  width: 34px; height: 34px; flex: none;
  border-radius: var(--radius-control);
  background: var(--surface-selected);
  display: flex; align-items: center; justify-content: center;
  color: var(--color-primary);
}
.head-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
.mod-name {
  margin: 0; font-size: var(--text-md); font-weight: 600; color: var(--color-text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.mod-category { font-size: var(--text-xs); color: var(--color-text-subtle); }
.summary-chip { flex: none; margin-left: auto; }

.mod-desc {
  margin: 0; font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.5;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  min-height: calc(2 * 1.5 * var(--text-sm));
}

.badge-row { display: flex; flex-wrap: wrap; gap: 6px; }
.card-reason {
  margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.5;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}

.card-actions {
  display: flex; flex-direction: column; gap: 6px;
  margin-top: auto; padding-top: 10px; border-top: 1px solid var(--color-border);
}
.secondary-row { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
</style>
