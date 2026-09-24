<script setup lang="ts">
// 模块详情/诊断面板（Wave 2）：四维逐行 + reason + 入口/能力/权限/owner/compatibility，
// 全部文案取自 constants/status 词汇表（含 ENTRYPOINT_META），缺词表值走"状态未知"兜底。
import { computed } from 'vue'
import type { ModuleEntry } from '../../composables/useModuleCatalog'
import {
  ENTRYPOINT_META,
  PRIMARY_ACTION_META,
  SUMMARY_META,
  deliveryMeta,
  healthMeta,
  policyMeta,
  runtimeMeta,
  updateAvailableText,
} from '../../constants/status'
import { MODULE_PRESENTATION, FALLBACK_MODULE_ICON } from '../../constants/navigation'
import { appIconName } from '../../constants/appIcons'
import type { RenderableIcon } from '../../constants/icons'
import AppIcon from '../ui/AppIcon.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'

const props = defineProps<{ entry: ModuleEntry }>()
const emit = defineEmits<{ close: [] }>()

const state = computed(() => props.entry.state)

/** 四维逐行：值 → META 文案（未知/缺失兜底"状态未知"，不抛错不推断）。 */
const dims = computed(() => {
  const s = state.value
  const health = healthMeta(s?.health ?? '')
  // update-available 行与卡片徽标同一口径：带投影里的上游新版本，无版本不编造。
  const healthText = String(s?.health ?? '') === 'update-available'
    ? updateAvailableText(s?.remoteVersion)
    : health.text
  return [
    { key: 'delivery', label: '交付', meta: deliveryMeta(s?.delivery ?? '') },
    { key: 'policy', label: '策略', meta: policyMeta(s?.policy ?? '') },
    { key: 'runtime', label: '运行', meta: runtimeMeta(s?.runtime ?? '') },
    { key: 'health', label: '健康', meta: { ...health, text: healthText } },
  ]
})

const summaryText = computed(() =>
  (SUMMARY_META as Record<string, string>)[String(state.value?.summary ?? '')] || '状态未知',
)
const actionLabel = computed(() =>
  (PRIMARY_ACTION_META as Record<string, { label: string }>)[String(state.value?.primaryAction ?? '')]?.label
  ?? '无可用操作',
)
const entrypointLabels = computed(() =>
  (props.entry.catalog.entrypoints ?? []).map(
    (v) => (ENTRYPOINT_META as Record<string, { label: string }>)[v]?.label ?? v,
  ),
)
const capabilities = computed(() => props.entry.catalog.capabilities ?? [])
const permissions = computed(() => props.entry.catalog.permissions ?? [])
const platformText = computed(() => (props.entry.catalog.compatibility?.platform ?? []).join('、') || '—')
const hostRangeText = computed(() => props.entry.catalog.compatibility?.hostRange || '—')

const iconName = computed<RenderableIcon | undefined>(() =>
  appIconName(MODULE_PRESENTATION[props.entry.catalog.id]?.icon || FALLBACK_MODULE_ICON),
)
</script>

<template>
  <section class="detail-panel" :aria-label="`模块详情：${entry.catalog.name}`">
    <header class="detail-head">
      <div class="icon-wrap">
        <AppIcon v-if="iconName" :name="iconName" :size="18" />
        <AppIcon v-else name="box" :size="18" />
      </div>
      <div class="head-text">
        <h2 :title="entry.catalog.name">{{ entry.catalog.name }}</h2>
        <span class="mod-id mono">{{ entry.catalog.id }}</span>
      </div>
      <button type="button" class="close-btn" aria-label="关闭详情" @click="emit('close')">
        <AppIcon name="x" :size="16" />
      </button>
    </header>

    <p v-if="entry.catalog.description" class="detail-desc">{{ entry.catalog.description }}</p>

    <h3 class="section-label">状态投影（四维）</h3>
    <dl class="dim-list">
      <div v-for="row in dims" :key="row.key" class="dim-row">
        <dt>{{ row.label }}</dt>
        <dd>
          <UiStatusChip :tone="row.meta.tone">
            <AppIcon :name="row.meta.icon" /> {{ row.meta.text }}
          </UiStatusChip>
        </dd>
      </div>
    </dl>
    <div class="kv-row">
      <span class="k">摘要</span><span class="v">{{ summaryText }}</span>
    </div>
    <div class="kv-row">
      <span class="k">主操作</span><span class="v">{{ actionLabel }}</span>
    </div>
    <div v-if="state?.reason" class="kv-row">
      <span class="k">说明</span><span class="v reason">{{ state.reason }}</span>
    </div>
    <div v-if="state" class="kv-row">
      <span class="k">契约版本</span><span class="v mono">schema v{{ state.schema }}</span>
    </div>

    <h3 class="section-label">目录信息</h3>
    <div class="kv-row">
      <span class="k">交付形态</span><span class="v mono">{{ entry.catalog.deliveryKind }}</span>
    </div>
    <div class="kv-row">
      <span class="k">维护 owner</span><span class="v">{{ entry.catalog.owner || '—' }}</span>
    </div>
    <div class="kv-row">
      <span class="k">兼容性</span>
      <span class="v">宿主 <span class="mono">{{ hostRangeText }}</span> · 平台 <span class="mono">{{ platformText }}</span></span>
    </div>

    <h3 class="section-label">暴露入口</h3>
    <ul v-if="entrypointLabels.length" class="tag-list">
      <li v-for="label in entrypointLabels" :key="label" class="tag-pill">{{ label }}</li>
    </ul>
    <p v-else class="none-hint">无</p>

    <h3 class="section-label">能力标记</h3>
    <ul v-if="capabilities.length" class="tag-list">
      <li v-for="cap in capabilities" :key="cap" class="tag-pill mono">{{ cap }}</li>
    </ul>
    <p v-else class="none-hint">无</p>

    <h3 class="section-label">受控权限</h3>
    <ul v-if="permissions.length" class="tag-list">
      <li v-for="perm in permissions" :key="perm" class="tag-pill mono">{{ perm }}</li>
    </ul>
    <p v-else class="none-hint">无声明</p>
  </section>
</template>

<style scoped>
.detail-panel {
  display: flex; flex-direction: column; gap: 6px;
  padding: 16px; overflow-y: auto; height: 100%;
  background: var(--surface-panel); color: var(--color-text);
}
.detail-head { display: flex; align-items: flex-start; gap: 10px; }
.icon-wrap {
  width: 34px; height: 34px; flex: none; border-radius: var(--radius-control);
  background: var(--surface-selected); color: var(--color-primary);
  display: flex; align-items: center; justify-content: center;
}
.head-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.head-text h2 {
  margin: 0; font-size: var(--text-lg); font-weight: 600;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.mod-id { font-size: var(--text-xs); color: var(--color-text-subtle); }
.close-btn {
  flex: none; width: 28px; height: 28px; border: 1px solid var(--color-border);
  border-radius: var(--radius-control); background: transparent; color: var(--color-text-muted);
  cursor: pointer; display: flex; align-items: center; justify-content: center;
  transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.close-btn:hover { background: var(--surface-hover); color: var(--color-text); }

.detail-desc { margin: 6px 0 0; font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.6; }

.section-label {
  margin: 14px 0 2px; font-size: var(--text-xs); font-weight: 600;
  color: var(--color-text-subtle); text-transform: uppercase; letter-spacing: 0.5px;
}
.dim-list { margin: 0; display: flex; flex-direction: column; gap: 6px; }
.dim-row { display: grid; grid-template-columns: 76px minmax(0, 1fr); gap: 10px; align-items: center; }
.dim-row dt { font-size: var(--text-sm); color: var(--color-text-muted); }
.dim-row dd { margin: 0; display: flex; flex-wrap: wrap; gap: 6px; }

.kv-row { display: flex; align-items: baseline; gap: 10px; padding: 4px 0; }
.kv-row .k { flex: none; width: 76px; font-size: var(--text-sm); color: var(--color-text-muted); }
.kv-row .v { font-size: var(--text-sm); color: var(--color-text); overflow-wrap: anywhere; }
.kv-row .v.reason { color: var(--color-text); }

.tag-list { list-style: none; margin: 2px 0 0; padding: 0; display: flex; flex-wrap: wrap; gap: 6px; }
.none-hint { margin: 2px 0 0; font-size: var(--text-sm); color: var(--color-text-subtle); }
</style>
