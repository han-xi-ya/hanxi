<script setup lang="ts">
import type { SnipasteVersionInfo } from '../../../bindings/hanxi/internal/modules/snipaste/version/models'

const props = defineProps<{
  item: SnipasteVersionInfo
  active: boolean
  running: boolean
  rowError?: string
  verificationLabel: string
  sizeLabel: string
  removeDisabled: boolean
  removeTitle: string
}>()

const emit = defineEmits<{
  'open-dir': [item: SnipasteVersionInfo]
  'set-active': [item: SnipasteVersionInfo]
  remove: [item: SnipasteVersionInfo]
}>()
</script>

<template>
  <article class="installed-card">
    <div class="inst-card-top">
      <strong class="ver-tag">{{ props.item.version }}</strong>
      <div class="badge-group">
        <span v-if="props.active" class="chip chip-information">当前使用</span>
        <span v-if="props.running" class="chip chip-positive">运行中</span>
        <span v-if="props.item.isImport" class="chip chip-neutral">本地导入</span>
      </div>
    </div>
    <code class="path-value">{{ props.item.dir }}</code>
    <p class="inst-meta">{{ props.sizeLabel }} · {{ props.verificationLabel }}</p>
    <p v-if="props.rowError" class="snipaste-row-error" role="alert">{{ props.rowError }}</p>
    <div class="inst-actions">
      <button class="btn btn-ghost" @click="emit('open-dir', props.item)">打开位置</button>
      <button v-if="!props.active" class="btn btn-secondary" @click="emit('set-active', props.item)">设为使用</button>
      <button class="btn btn-danger-outline" :disabled="props.removeDisabled" :title="props.removeTitle" @click="emit('remove', props.item)">卸载</button>
    </div>
  </article>
</template>

<style scoped>
.installed-card { display: grid; gap: 10px; min-width: 0; padding: 14px; border-radius: var(--radius-element); }
.inst-card-top { align-items: flex-start; gap: 10px; }
.badge-group { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ver-tag { font-variant-numeric: tabular-nums; }
.path-value { display: block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-muted); font-family: var(--font-mono); font-size: var(--text-sm); line-height: 1.5; }
.inst-meta { margin: 0; color: var(--color-text-muted); }
.inst-actions { align-items: center; flex-wrap: wrap; margin-top: auto; }
.snipaste-row-error { margin: 0; font-size: var(--text-sm); line-height: 1.45; white-space: normal; color: var(--state-danger); }
@media (max-width: 460px) { .path-value { white-space: normal; overflow-wrap: anywhere; } }
</style>
