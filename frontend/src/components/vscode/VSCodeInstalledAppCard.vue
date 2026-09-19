<script setup lang="ts">
import type { Status } from '../../../bindings/hanxi/internal/modules/vscode/models'
import type { Snapshot } from '../../../bindings/hanxi/internal/modules/vscode/instance/models'

defineProps<{
  info: Status['installed'] | null
  snap: Snapshot | null
  busy: boolean
}>()

const emit = defineEmits<{
  'open-dir': [path: string]
}>()
</script>

<template>
  <div v-if="info?.installed" class="installed-grid">
    <div class="installed-card">
      <div class="inst-card-top">
        <span class="ver-tag">{{ info.version }}</span>
        <div class="inst-badges">
          <span class="badge badge-official">User Installer</span>
          <span v-if="snap?.state === 'running'" class="badge badge-running">运行中</span>
          <span v-else-if="snap?.state === 'external'" class="badge badge-import">外部运行</span>
        </div>
      </div>
      <div class="inst-meta">
        <div class="meta-line"><span class="k">位置</span><code class="mono">{{ info.dir }}</code></div>
        <div class="meta-line"><span class="k">托管</span><span class="hint-dim">与日常使用共实例组；升级请回远程表点「安装/升级」（运行中会先要求确认）</span></div>
      </div>
      <div class="inst-actions">
        <button class="btn btn-secondary btn-small" :disabled="busy" @click="emit('open-dir', info.dir)">📂 打开位置</button>
      </div>
    </div>
  </div>
  <div v-else class="empty-state">
    <p>本机未检测到安装版 VS Code（安装版仅本机一份；如需在 Hanxi 外自行安装亦会被自动感知）</p>
  </div>
</template>

<style scoped>
.inst-card-top, .inst-badges, .inst-actions { flex-wrap: wrap; }
.inst-card-top { gap: 8px; }
.badge-running, .badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
</style>
