<script setup lang="ts">
import type { Snapshot } from '../../../bindings/hanxi/internal/modules/snipaste/instance/models'
import type { SnipasteVersionInfo } from '../../../bindings/hanxi/internal/modules/snipaste/version/models'
import type { ManagedConsoleStore } from '../managed/store'
import { fmtDuration } from '../../utils/format'

const props = defineProps<{
  store: ManagedConsoleStore
  selected: SnipasteVersionInfo | null
  busy: boolean
  controlResult: { tone: 'info' | 'warning' | 'error'; text: string } | null
}>()

const emit = defineEmits<{
  launch: []
  quit: []
  'show-images': []
  'select-versions': []
}>()

function stateOf(snapshot: Snapshot | null): string {
  return snapshot?.state ?? 'stopped'
}

function ownedRunning(snapshot: Snapshot | null): boolean {
  return ['starting', 'running', 'quitting'].includes(stateOf(snapshot))
}

// quitAllowed：自有实例或外部实例（N3 分档接管）；quitting 进行中除外。
function quitAllowed(snapshot: Snapshot | null): boolean {
  const s = stateOf(snapshot)
  return (ownedRunning(snapshot) || s === 'external') && s !== 'quitting'
}

function isExternal(snapshot: Snapshot | null): boolean {
  return stateOf(snapshot) === 'external'
}
</script>

<template>
  <div class="snipaste-console-stack">
    <div class="control-panel snipaste-control-panel">
      <div class="snipaste-control-main">
        <div class="snipaste-control-state">
          <span class="snipaste-status" :data-state="stateOf(store.snap as Snapshot | null)">{{ store.stateText }}</span>
          <span v-if="store.snap?.pid" class="mono-meta">PID {{ store.snap.pid }} · {{ fmtDuration(store.uptimeSec) }}</span>
        </div>
        <strong class="version-value">{{ store.snap?.version || selected?.version || '尚未安装' }}</strong>
        <code v-if="(store.snap as Snapshot | null)?.exePath || selected" class="path-value">{{ (store.snap as Snapshot | null)?.exePath || selected?.exePath }}</code>
        <p v-else class="muted-copy">先下载官网免安装版，或导入已有的 Snipaste 便携目录。</p>
        <span v-if="store.snap?.error" class="snipaste-row-error" role="alert">{{ store.snap.error }}</span>
      </div>
      <div class="btn-group">
        <button v-if="!selected" class="btn btn-secondary" @click="emit('select-versions')">前往版本管理</button>
        <button
          class="btn btn-primary"
          :disabled="props.busy || !selected || ownedRunning(store.snap as Snapshot | null) || isExternal(store.snap as Snapshot | null)"
          :title="isExternal(store.snap as Snapshot | null) ? '外部实例运行中：可「显隐贴图」唤起，或「退出进程」后由 Hanxi 重新托管' : undefined"
          @click="emit('launch')"
        >
          {{ stateOf(store.snap as Snapshot | null) === 'starting' ? '正在启动…' : '启动 Snipaste' }}
        </button>
        <button
          class="btn btn-secondary"
          :disabled="props.busy || !(ownedRunning(store.snap as Snapshot | null) || isExternal(store.snap as Snapshot | null))"
          @click="emit('show-images')"
        >
          显隐贴图
        </button>
        <button class="btn btn-danger-outline" :disabled="props.busy || !quitAllowed(store.snap as Snapshot | null)" @click="emit('quit')">
          {{ stateOf(store.snap as Snapshot | null) === 'quitting' ? '正在退出…' : isExternal(store.snap as Snapshot | null) ? '退出外部实例' : '退出进程' }}
        </button>
      </div>
    </div>

    <div v-if="controlResult" class="state-box" :class="`state-${controlResult.tone}`" aria-live="polite">{{ controlResult.text }}</div>
  </div>
</template>

<style scoped>
.snipaste-console-stack { display: grid; gap: 14px; }
.control-panel { border-radius: var(--radius-element); padding: 16px; }
.snipaste-control-panel { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.snipaste-control-main { display: grid; gap: 6px; min-width: 0; }
.snipaste-control-state { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.btn-group { align-items: center; flex-wrap: wrap; }
.version-value, .path-value, .mono-meta { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.version-value { font-size: var(--text-xl); }
.path-value { display: block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-muted); font-size: var(--text-sm); line-height: 1.5; }
.mono-meta { color: var(--color-text-muted); font-size: var(--text-sm); }
.muted-copy { margin: 0; color: var(--color-text-muted); }
.snipaste-status { display: inline-flex; align-items: center; width: fit-content; padding: 3px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-pill); color: var(--color-text-muted); font-size: var(--text-xs); font-weight: 700; white-space: nowrap; }
.snipaste-status[data-state="running"] { color: var(--state-positive); border-color: color-mix(in srgb, var(--state-positive) 35%, var(--color-border)); }
.snipaste-status[data-state="starting"], .snipaste-status[data-state="quitting"] { color: var(--state-warning); }
.snipaste-status[data-state="external"] { color: var(--state-warning); border-color: color-mix(in srgb, var(--state-warning) 35%, var(--color-border)); }
.snipaste-status[data-state="failed"] { color: var(--state-danger); }
.snipaste-row-error { margin: 0; font-size: var(--text-sm); line-height: 1.45; white-space: normal; color: var(--state-danger); }
@media (max-width: 760px) { .snipaste-control-panel { align-items: stretch; flex-direction: column; } .btn-group { justify-content: flex-start; } }
@media (max-width: 460px) { .btn-group .btn { flex: 1 1 auto; } .path-value { white-space: normal; overflow-wrap: anywhere; } }
</style>
