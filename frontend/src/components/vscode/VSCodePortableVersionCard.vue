<script setup lang="ts">
import type { VersionInfo } from '../../../bindings/hanxi/internal/modules/vscode/version/models'
import { fmtSize } from '../../utils/format'

defineProps<{
  info: VersionInfo
  activeVersion: string
  runningVersion: string
  busy: boolean
}>()

const emit = defineEmits<{
  'set-active': [info: VersionInfo]
  'open-dir': [path: string]
  remove: [info: VersionInfo]
}>()
</script>

<template>
  <div class="installed-card" :class="{ 'card-active': activeVersion === info.version }">
    <div class="inst-card-top">
      <span class="ver-tag">{{ info.version }}</span>
      <div class="inst-badges">
        <span v-if="activeVersion === info.version" class="badge badge-active">使用中</span>
        <span v-else-if="runningVersion === info.version" class="badge badge-running">运行中</span>
        <span v-if="info.isImport" class="badge badge-import">本地导入</span>
        <span v-else class="badge" :class="info.verified ? 'badge-official' : 'badge-weak'">
          {{ info.verified ? '官方哈希' : '三层校验' }}
        </span>
      </div>
    </div>
    <div class="inst-meta">
      <div class="meta-line"><span class="k">路径</span><code class="mono">{{ info.exePath }}</code></div>
      <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(info.size) }} · 安装于 {{ info.installedAt }}</span></div>
      <div v-if="info.isImport && info.source" class="meta-line"><span class="k">来源</span><span class="hint-dim">{{ info.source }}</span></div>
    </div>
    <div class="inst-actions">
      <button v-if="activeVersion !== info.version" class="btn btn-primary btn-small" :disabled="busy" @click="emit('set-active', info)">设为使用</button>
      <button class="btn btn-secondary btn-small" :disabled="busy" @click="emit('open-dir', info.dir)">📂 位置</button>
      <button class="btn btn-secondary btn-small" :disabled="busy" title="打开便携数据目录 data\（user-data 与 extensions）" @click="emit('open-dir', info.dir + '\\data')">🗂 数据</button>
      <button
        class="btn btn-danger-outline btn-small"
        :disabled="busy || runningVersion === info.version"
        :title="runningVersion === info.version ? '请先退出该便携版实例' : ''"
        @click="emit('remove', info)"
      >卸载</button>
    </div>
  </div>
</template>

<style scoped>
.inst-card-top, .inst-badges, .inst-actions { flex-wrap: wrap; }
.inst-card-top { gap: 8px; }
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running, .badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-weak { background: var(--state-warning-soft); color: var(--state-warning); }
</style>
