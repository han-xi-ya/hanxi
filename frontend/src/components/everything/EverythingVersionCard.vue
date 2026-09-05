<script setup lang="ts">
// Everything 单个已安装版本卡片：版本徽标组 + 元信息 + 操作按钮，自 EverythingView 随 DOM
// 逐字迁出。「使用中/运行中」判定与卸载禁用条件由视图算好经 isActive/isRunning 传入；
// 设为使用/打开位置/卸载三个动作回视图执行（卸载确认 useConfirm 等业务逻辑不在此层）。
import type { EverythingVersionInfo } from '../../../bindings/hanxi/internal/modules/everything/version/models'
import { fmtSize } from '../../utils/format'

defineProps<{
  info: EverythingVersionInfo
  isActive: boolean
  isRunning: boolean
}>()

const emit = defineEmits<{
  'set-active': [v: EverythingVersionInfo]
  'open-dir': [v: EverythingVersionInfo]
  'remove': [v: EverythingVersionInfo]
}>()
</script>

<template>
  <div class="installed-card" :class="{ 'card-active': isActive }">
    <div class="inst-card-top">
      <span class="ver-tag">{{ info.version }}</span>
      <div class="inst-badges">
        <span v-if="isActive" class="badge badge-active">使用中</span>
        <span v-else-if="isRunning" class="badge badge-running">运行中</span>
        <span v-if="info.isImport" class="badge badge-import">本地导入</span>
        <span v-else class="badge badge-official">官方下载</span>
      </div>
    </div>
    <div class="inst-meta">
      <div class="meta-line"><span class="k">路径</span><code class="mono">{{ info.exePath }}</code></div>
      <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(info.size) }} · 安装于 {{ info.installedAt }}</span></div>
      <div class="meta-line" v-if="info.isImport && info.source"><span class="k">来源</span><span class="hint-dim">{{ info.source }}</span></div>
    </div>
    <div class="inst-actions">
      <button v-if="!isActive" class="btn btn-primary btn-small" @click="emit('set-active', info)">设为使用</button>
      <button class="btn btn-secondary btn-small" @click="emit('open-dir', info)">📂 打开位置</button>
      <button
        class="btn btn-danger-outline btn-small"
        :disabled="isRunning"
        :title="isRunning ? '请先退出 Everything' : ''"
        @click="emit('remove', info)"
      >卸载</button>
    </div>
  </div>
</template>

<style scoped>
.installed-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color var(--motion-base) ease;
}
.installed-card.card-active { border-color: var(--color-primary); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; }
/* .badge 基形与 .hint-dim 已上收 components.css（§9.6-2），以下仅本卡片档位色 */
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }
</style>
