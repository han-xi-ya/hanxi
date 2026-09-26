<script setup lang="ts">
import type { SnipasteRelease } from '../../../bindings/hanxi/internal/modules/snipaste/version/models'
import type { SnipasteDownloadTicket } from '../../composables/useSnipasteDownloadTickets'
import { releaseFormWord } from '../managed/adapter'

type ReleaseStatus = 'installed' | 'downloading' | 'error' | 'idle'

const props = defineProps<{
  releases: SnipasteRelease[]
  loading: boolean
  statusOf: (release: SnipasteRelease) => ReleaseStatus
  ticketOf: (version: string) => SnipasteDownloadTicket | undefined
  rowErrors: Record<string, string>
  sizeLabel: (bytes: number) => string
  verificationLabel: (release: SnipasteRelease) => string
  stageText: (progress?: SnipasteDownloadTicket) => string
  percent: (progress?: SnipasteDownloadTicket) => number | null
}>()

const emit = defineEmits<{
  download: [release: SnipasteRelease]
}>()
</script>

<template>
  <div class="table-container">
    <table class="tbl">
      <thead><tr><th>版本</th><th>状态</th><th>大小</th><th>发布时间</th><th>校验</th><th class="action-col">操作</th></tr></thead>
      <tbody>
        <tr v-for="release in props.releases" :key="release.version">
          <td><div class="release-version"><strong>{{ release.version }}</strong><span v-if="release.isPre" class="chip chip-warning">预发布</span><span v-if="release.stale" class="chip chip-warning">缓存</span><!-- N13 统一形态 chip：本托管资产形态事实（后端 ListRemote 回填，纯展示），画法对齐 ManagedVersionPanel --><span
            v-if="release.form"
            class="chip chip-neutral form-chip"
            :title="`本托管形态（安装链事实）：${releaseFormWord(release.form)}`"
          >{{ releaseFormWord(release.form) }}</span></div></td>
          <td>
            <span v-if="props.statusOf(release) === 'installed'" class="snipaste-ver-status installed">已安装</span>
            <span v-else-if="props.statusOf(release) === 'error'" class="snipaste-ver-status error">失败</span>
            <span v-else-if="props.statusOf(release) === 'downloading'" class="snipaste-ver-status working">{{ props.stageText(props.ticketOf(release.version)) }}</span>
            <span v-else class="snipaste-ver-status idle">可安装</span>
          </td>
          <td class="mono-meta">{{ props.sizeLabel(release.size) }}</td>
          <td>{{ release.published || '未知' }}</td>
          <td>{{ props.verificationLabel(release) }}</td>
          <td class="action-col">
            <template v-if="props.statusOf(release) === 'downloading'">
              <div v-if="props.ticketOf(release.version)?.stage === 'downloading'" class="snipaste-progress-wrap">
                <div class="snipaste-progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="props.percent(props.ticketOf(release.version)) ?? undefined" :aria-valuetext="props.stageText(props.ticketOf(release.version))">
                  <span v-if="props.percent(props.ticketOf(release.version)) !== null" :style="{ width: `${props.percent(props.ticketOf(release.version))}%` }"></span>
                  <span v-else class="snipaste-progress-indeterminate"></span>
                </div>
                <small v-if="props.percent(props.ticketOf(release.version)) !== null">{{ props.percent(props.ticketOf(release.version)) }}%</small>
              </div>
              <span v-else class="download-stage">{{ props.stageText(props.ticketOf(release.version)) }}</span>
            </template>
            <button v-else-if="props.statusOf(release) === 'error'" class="link-button" @click="emit('download', release)">重试</button>
            <span v-else-if="props.statusOf(release) === 'installed'" class="installed-label">已安装</span>
            <button v-else class="btn btn-primary btn-small" @click="emit('download', release)">下载并安装</button>
            <p v-if="props.rowErrors[release.version]" class="snipaste-row-error" role="alert">{{ props.rowErrors[release.version] }}</p>
          </td>
        </tr>
        <tr v-if="!props.releases.length && !props.loading"><td colspan="6" class="empty-hint">没有可用的官网版本</td></tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.table-container { border-radius: var(--radius-element); }
.table-container .tbl { min-width: 830px; }
.action-col { width: 170px; }
.release-version { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
/* N13 形态 chip 密度对齐 ManagedVersionPanel.form-chip；行内容器已带 gap，不再加 margin */
.form-chip { font-size: var(--text-xs); padding: 1px 7px; white-space: nowrap; vertical-align: middle; }
.mono-meta { color: var(--color-text-muted); font-family: var(--font-mono); font-size: var(--text-sm); font-variant-numeric: tabular-nums; }
.snipaste-ver-status { display: inline-flex; align-items: center; width: fit-content; padding: 3px 7px; border-radius: var(--radius-pill); font-size: var(--text-xs); font-weight: 700; white-space: nowrap; }
.snipaste-ver-status.installed { color: var(--state-positive); background: color-mix(in srgb, var(--state-positive) 10%, transparent); }
.snipaste-ver-status.error { color: var(--state-danger); background: color-mix(in srgb, var(--state-danger) 9%, transparent); }
.snipaste-ver-status.working { color: var(--state-warning); background: color-mix(in srgb, var(--state-warning) 9%, transparent); }
.snipaste-ver-status.idle { color: var(--color-text-muted); background: var(--surface-hover); }
.snipaste-progress-wrap { display: flex; align-items: center; gap: 7px; min-width: 125px; }
.snipaste-progress { position: relative; flex: 1; height: 6px; overflow: hidden; border-radius: var(--radius-pill); background: var(--surface-hover); }
.snipaste-progress span { display: block; height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
.snipaste-progress-indeterminate { width: 35%; animation: snipaste-slide 1.1s ease-in-out infinite; }
.download-stage, .installed-label { color: var(--color-text-muted); font-size: var(--text-sm); }
.snipaste-row-error { margin: 0; font-size: var(--text-sm); line-height: 1.45; white-space: normal; color: var(--state-danger); }
@keyframes snipaste-slide { from { transform: translateX(-110%); } to { transform: translateX(390%); } }
</style>
