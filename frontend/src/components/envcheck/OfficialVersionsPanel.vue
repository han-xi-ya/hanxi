<script setup lang="ts">
import type { Channel } from '../../../bindings/hanxi/internal/modules/envcheck/remoteversion/models'

defineProps<{
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
          {{ channel.relationDetail || relationText[channel.relation] || relationText.unknown }}
          <span v-if="stale">（基于缓存，仅供参考）</span>
        </p>
        <!-- 后端仍返回完整近期版本，展示层折叠为仅最新一条；本机所在版本线通道由后端排序置顶。 -->
        <ul v-if="channel.releases?.length" class="release-list">
          <li>
            <span class="release-rank">最新</span>
            <code class="mono release-version">{{ channel.releases[0].version }}</code>
            <time v-if="channel.releases[0].published" :datetime="channel.releases[0].published">{{ channel.releases[0].published }}</time>
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
.official-panel { margin-top: 3px; padding-top: 11px; border-top: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 8px; }
.panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.panel-heading strong { color: var(--color-text); font-size: 12px; }
.cache-chip { display: inline-block; margin-left: 7px; padding: 1px 6px; border-radius: 10px; background: var(--state-warning-soft); color: var(--state-warning); font-size: 10px; }
.panel-state, .fetched-at { margin: 0; color: var(--color-text-muted); font-size: 12px; }
.fetched-at { font-size: 10px; }
.panel-error { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; padding: 7px 8px; border-radius: 5px; background: var(--state-danger-soft); color: var(--state-danger); font-size: 12px; line-height: 1.5; }
.channel-list { display: flex; flex-direction: column; gap: 10px; }
.channel-block { display: flex; flex-direction: column; gap: 6px; }
.channel-block + .channel-block { padding-top: 9px; border-top: 1px dashed var(--color-border); }
.channel-heading { display: flex; align-items: baseline; gap: 7px; flex-wrap: wrap; }
.channel-label { color: var(--color-text); font-size: 12px; font-weight: 700; }
.channel-detail { color: var(--color-text-muted); font-size: 10px; }
.relation-text { margin: 0; padding: 6px 8px; border-radius: 5px; font-size: 12px; line-height: 1.45; }
.relation-ok { background: var(--state-positive-soft); color: var(--state-positive); }
.relation-update { background: var(--state-information-soft); color: var(--state-information); }
.relation-neutral { background: var(--surface-hover); color: var(--color-text-muted); }
.release-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 4px; }
.release-list li { display: grid; grid-template-columns: 50px minmax(0, 1fr) auto; align-items: baseline; gap: 8px; padding: 4px 2px; border-bottom: 1px dashed var(--color-border); }
.release-list li:last-child { border-bottom: 0; }
.release-rank { color: var(--color-text-subtle); font-size: 11px; }
.release-version { font-weight: 600; }
.release-list time, .date-empty { color: var(--color-text-muted); font-size: 11px; white-space: nowrap; }
.mono { font-family: var(--font-mono); color: var(--color-text); font-size: 11px; overflow-wrap: anywhere; }
.link-button { padding: 0; border: 0; background: none; color: var(--color-primary); cursor: pointer; font: inherit; white-space: nowrap; text-decoration: underline; }
/* .btn 基础/small/禁用/焦点环/coarse/reduced 由全局承载；保留面板特有"描边强调"变体与链接禁用态 */
.btn-secondary { background: transparent; color: var(--color-primary); border-color: var(--color-border); }
.btn-secondary:hover:not(:disabled) { border-color: var(--color-primary); background: var(--surface-soft); }
.link-button:disabled { opacity: 0.5; cursor: not-allowed; }
@media (max-width: 460px) {
  .release-list li { grid-template-columns: 50px minmax(0, 1fr); }
  .release-list time, .date-empty { grid-column: 2; }
  .panel-heading .btn { width: 100%; }
}
</style>
