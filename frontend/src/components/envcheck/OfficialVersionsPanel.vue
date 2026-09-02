<script setup lang="ts">
import type { Channel } from '../../../bindings/hanxi/internal/modules/envcheck/remoteversion/models'

const props = defineProps<{
  heading: string
  downloadLabel: string
  channels: Channel[]
  loading: boolean
  error: string
  stale: boolean
  fetchedAt?: string
}>()

const emit = defineEmits<{
  retry: []
  open: []
}>()

const relationText: Record<string, string> = {
  'not-installed': '本机未安装',
  'latest': '本机已是该通道最新版',
  'update-available': '该通道有可用更新',
  'ahead': '本机版本高于该通道最新版',
  'unknown': '无法判断版本关系',
}

function relationClass(relation: string) {
  if (relation === 'latest') return 'relation-ok'
  if (relation === 'update-available') return 'relation-update'
  return 'relation-neutral'
}
</script>

<template>
  <section class="official-panel" :aria-busy="loading">
    <div class="panel-heading">
      <div>
        <strong>{{ heading }}</strong>
        <span v-if="stale" class="cache-chip">缓存数据</span>
      </div>
      <button class="btn btn-secondary btn-small" @click="emit('open')">{{ downloadLabel }}</button>
    </div>

    <p v-if="loading && !channels.length" class="panel-state">正在查询官网版本…</p>
    <div v-if="error" class="panel-error" role="alert">
      <span>{{ error }}<template v-if="channels.length">，继续显示上次结果</template></span>
      <button class="link-button" :disabled="loading" @click="emit('retry')">重试</button>
    </div>

    <div v-if="channels.length" class="channel-list">
      <section v-for="channel in channels" :key="channel.key" class="channel-block">
        <div class="channel-heading">
          <span class="channel-label">{{ channel.label }}</span>
          <span v-if="channel.detail" class="channel-detail">{{ channel.detail }}</span>
        </div>
        <p class="relation-text" :class="relationClass(channel.relation)">
          {{ relationText[channel.relation] ?? relationText.unknown }}
          <span v-if="stale">（基于缓存，仅供参考）</span>
        </p>
        <ul v-if="channel.releases?.length" class="release-list">
          <li v-for="(release, index) in channel.releases" :key="release.version">
            <span class="release-rank">{{ index === 0 ? '最新' : `近期 ${index + 1}` }}</span>
            <code class="mono release-version">{{ release.version }}</code>
            <time v-if="release.published" :datetime="release.published">{{ release.published }}</time>
            <span v-else class="date-empty">发布日期未知</span>
          </li>
        </ul>
      </section>
    </div>
    <p v-else-if="!loading && !error" class="panel-state">官网响应中未找到可识别的稳定版本。</p>
    <p v-if="stale && fetchedAt" class="fetched-at">缓存获取时间：{{ fetchedAt }}</p>
  </section>
</template>

<style scoped>
.official-panel { margin-top: 3px; padding-top: 11px; border-top: 1px solid var(--border-color); display: flex; flex-direction: column; gap: 8px; }
.panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.panel-heading strong { color: var(--text-main); font-size: 12px; }
.cache-chip { display: inline-block; margin-left: 7px; padding: 1px 6px; border-radius: 10px; background: #fff8c5; color: #9a6700; font-size: 10px; }
.panel-state, .fetched-at { margin: 0; color: var(--text-muted); font-size: 12px; }
.fetched-at { font-size: 10px; }
.panel-error { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; padding: 7px 8px; border-radius: 5px; background: #ffebe9; color: var(--danger); font-size: 12px; line-height: 1.5; }
.channel-list { display: flex; flex-direction: column; gap: 10px; }
.channel-block { display: flex; flex-direction: column; gap: 6px; }
.channel-block + .channel-block { padding-top: 9px; border-top: 1px dashed var(--border-color); }
.channel-heading { display: flex; align-items: baseline; gap: 7px; flex-wrap: wrap; }
.channel-label { color: var(--text-main); font-size: 12px; font-weight: 700; }
.channel-detail { color: var(--text-muted); font-size: 10px; }
.relation-text { margin: 0; padding: 6px 8px; border-radius: 5px; font-size: 12px; line-height: 1.45; }
.relation-ok { background: #dafbe1; color: #1a7f37; }
.relation-update { background: #ddf4ff; color: #0969da; }
.relation-neutral { background: #eaeef2; color: #656d76; }
.release-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 4px; }
.release-list li { display: grid; grid-template-columns: 50px minmax(0, 1fr) auto; align-items: baseline; gap: 8px; padding: 4px 2px; border-bottom: 1px dashed var(--border-color); }
.release-list li:last-child { border-bottom: 0; }
.release-rank { color: var(--text-subtle); font-size: 11px; }
.release-version { font-weight: 600; }
.release-list time, .date-empty { color: var(--text-muted); font-size: 11px; white-space: nowrap; }
.mono { font-family: Consolas, monospace; color: var(--text-main); font-size: 11px; overflow-wrap: anywhere; }
.link-button { padding: 0; border: 0; background: none; color: var(--accent); cursor: pointer; font: inherit; white-space: nowrap; text-decoration: underline; }
.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled, .link-button:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-secondary { background: transparent; color: var(--accent); border-color: var(--border-color); }
.btn-secondary:hover:not(:disabled) { border-color: var(--accent); background: var(--bg-main); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn:focus-visible, .link-button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@media (max-width: 460px) {
  .release-list li { grid-template-columns: 50px minmax(0, 1fr); }
  .release-list time, .date-empty { grid-column: 2; }
  .panel-heading .btn { width: 100%; }
}
@media (pointer: coarse) { .btn, .link-button { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) { .btn { transition: none; } }
</style>
