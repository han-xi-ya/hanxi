<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { ToolOverview, OperationProgress } from '../../../bindings/hanxi/internal/modules/envcheck/npmtool/models'

const props = defineProps<{
  overview: ToolOverview
  operation: OperationProgress | null
  busyElsewhere: boolean
  logLines: string[]
}>()

const emit = defineEmits<{ install: []; upgrade: []; uninstall: []; retry: [] }>()

const relationText: Record<string, string> = {
  'not-installed': '未安装 · 可一键安装',
  'latest': '已是 npm registry 最新版',
  'update-available': '有可用更新',
  'ahead': '本机版本高于 registry 最新版',
  'unknown': '无法判断版本关系',
}

const status = computed(() => props.overview.local.status)
const relation = computed(() => props.overview.relation)
const busy = computed(() => !!props.operation || props.busyElsewhere)
const canInstall = computed(() => status.value === 'missing')
const canUpgrade = computed(() => status.value === 'installed' && relation.value === 'update-available')
const canUninstall = computed(() => status.value === 'installed')

function relationClass(): string {
  if (relation.value === 'latest') return 'relation-ok'
  if (relation.value === 'update-available') return 'relation-update'
  return 'relation-neutral'
}

const upgradeLabel = computed(() =>
  props.overview.latest?.version ? `升级到 ${props.overview.latest.version}` : '升级到最新版')

// 实时日志面板：自动滚到底，环形裁剪由父层负责。
const logRef = ref<HTMLElement | null>(null)
watch(() => props.logLines.length, async () => {
  await nextTick()
  if (logRef.value) logRef.value.scrollTop = logRef.value.scrollHeight
})
</script>

<template>
  <section class="npm-panel" :aria-busy="busy">
    <div class="panel-heading">
      <div>
        <strong>npm 全局工具</strong>
        <span v-if="overview.isStale" class="cache-chip">缓存数据</span>
      </div>
      <span v-if="overview.latest?.version" class="latest-version">
        最新版 <code class="mono">{{ overview.latest.version }}</code>
      </span>
    </div>

    <p v-if="overview.latestError" class="panel-error" role="alert">
      <span>{{ overview.latestError }}</span>
      <button class="link-button" @click="emit('retry')">重试</button>
    </p>
    <p v-else class="relation-text" :class="relationClass()">
      {{ relationText[relation] || relationText.unknown }}
      <span v-if="overview.isStale">（基于缓存，仅供参考）</span>
    </p>

    <p v-if="overview.relationDetail" class="detail-warn">{{ overview.relationDetail }}</p>

    <div v-if="busy" class="op-running">
      <span class="op-spinner" aria-hidden="true"></span>
      {{ operation?.message || '另一 npm 操作进行中，请等待' }}
    </div>

    <div class="npm-actions">
      <button
        v-if="canInstall"
        class="btn btn-primary btn-small"
        :disabled="busy"
        :aria-busy="busy"
        @click="emit('install')"
      >安装</button>
      <button
        v-else-if="canUpgrade"
        class="btn btn-primary btn-small"
        :disabled="busy"
        :aria-busy="busy"
        @click="emit('upgrade')"
      >{{ upgradeLabel }}</button>
      <button
        v-if="canUninstall"
        class="btn btn-uninstall btn-small"
        :disabled="busy"
        @click="emit('uninstall')"
      >卸载</button>
    </div>

    <pre
      v-if="operation || logLines.length"
      ref="logRef"
      class="op-log"
      role="log"
      aria-live="polite"
    >{{ logLines.join('\n') || '正在启动 npm 操作…' }}</pre>

    <p class="npm-footnote">
      由 Hanxi 在隐藏控制台执行 npm 官方命令（{{ overview.tool.package }}），不自动提权；卸载仅移除 npm 全局安装。
      若该工具由 nvm、Volta、Scoop 等管理，请优先使用原管理器。
    </p>
  </section>
</template>

<style scoped>
.npm-panel { margin-top: 3px; padding-top: 11px; border-top: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 8px; }
.panel-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.panel-heading strong { color: var(--color-text); font-size: 12px; }
.cache-chip { display: inline-block; margin-left: 7px; padding: 1px 6px; border-radius: 10px; background: var(--state-warning-soft); color: var(--state-warning); font-size: 10px; }
.latest-version { color: var(--color-text-muted); font-size: 11px; }
.relation-text { margin: 0; padding: 6px 8px; border-radius: 5px; font-size: 12px; line-height: 1.45; }
.relation-ok { background: var(--state-positive-soft); color: var(--state-positive); }
.relation-update { background: var(--state-information-soft); color: var(--state-information); }
.relation-neutral { background: var(--surface-hover); color: var(--color-text-muted); }
.panel-error { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; padding: 7px 8px; border-radius: 5px; background: var(--state-danger-soft); color: var(--state-danger); font-size: 12px; line-height: 1.5; margin: 0; }
.detail-warn { margin: 0; padding: 6px 8px; border-radius: 5px; background: var(--state-warning-soft); color: var(--state-warning); font-size: 11px; line-height: 1.5; }
.op-running { display: flex; align-items: center; gap: 7px; color: var(--color-text-muted); font-size: 12px; }
.op-spinner { width: 12px; height: 12px; border: 2px solid var(--color-border); border-top-color: var(--color-primary); border-radius: 50%; animation: npm-spin 0.8s linear infinite; flex: none; }
@keyframes npm-spin { to { transform: rotate(360deg); } }
.npm-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.op-log { margin: 0; max-height: 140px; overflow-y: auto; padding: 8px; border-radius: 6px; background: var(--surface-soft); border: 1px solid var(--color-border); font-family: var(--font-mono); font-size: 11px; line-height: 1.5; color: var(--color-text); white-space: pre-wrap; overflow-wrap: anywhere; }
.npm-footnote { margin: 0; color: var(--color-text-muted); font-size: 11px; line-height: 1.5; }
.mono { font-family: var(--font-mono); color: var(--color-text); font-size: 11px; }
/* .link-button 全局原子承载基础形；本面板保持常下划线与小字号语境 */
.link-button { padding: 0; border: 0; background: none; color: var(--color-primary); cursor: pointer; font: inherit; white-space: nowrap; text-decoration: underline; }
/* .btn 基础/primary/small、禁用态、焦点环、coarse-pointer 最小尺寸与减弱动效
   由 components.css / base.css 全局承载；本面板仅留特有"描边红"卸载变体 */
.btn-uninstall { background: transparent; color: var(--state-danger); border-color: var(--state-danger-glow); }
.btn-uninstall:hover:not(:disabled) { background: var(--state-danger-soft); border-color: var(--state-danger); }
@media (max-width: 460px) { .npm-actions .btn { flex: 1; } }
@media (prefers-reduced-motion: reduce) { .op-spinner { animation-duration: 2s; } }
</style>
