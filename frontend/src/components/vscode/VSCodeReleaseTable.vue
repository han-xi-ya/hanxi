<script setup lang="ts">
import type { Release } from '../../../bindings/hanxi/internal/modules/vscode/version/models'
import type { NormalizedProgress } from '../managed/adapter'
import type { VSCodeForm } from '../../adapters/vscode'
import { fmtSize } from '../../utils/format'
import { vscodeProgressKey } from '../../adapters/vscode'

const props = defineProps<{
  form: VSCodeForm
  releases: Release[]
  installedVersions: string[]
  installedAppVersion: string
  downloading: Record<string, NormalizedProgress>
  loading: boolean
}>()

const emit = defineEmits<{
  download: [release: Release]
}>()

function ticket(release: Release): NormalizedProgress | undefined {
  return props.downloading[vscodeProgressKey(props.form, release.version)]
}

function statusOf(release: Release): 'installed' | 'downloading' | 'error' | 'idle' {
  const progress = ticket(release)
  if (progress) return progress.stage === 'error' ? 'error' : 'downloading'
  if (props.form === 'portable' && props.installedVersions.includes(release.version)) return 'installed'
  if (props.form === 'installer' && props.installedAppVersion === release.version) return 'installed'
  return 'idle'
}

function stepOf(progress: NormalizedProgress): number {
  if (progress.stage === 'done') return 100
  if (progress.stage !== 'downloading' || !progress.total) return 0
  return Math.min(99, Math.round((progress.done / progress.total) * 100))
}
</script>

<template>
  <div class="table-container">
    <table class="tbl">
      <thead>
        <tr>
          <th style="width: 130px;">版本</th>
          <th style="width: 170px;">状态</th>
          <th style="width: 90px;">大小</th>
          <th style="width: 110px;">Commit</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="release in releases" :key="release.version">
          <td>
            <strong class="ver-name">{{ release.version }}</strong>
            <span v-if="form === 'portable' && release.version === releases[0]?.version && release.sha256" class="badge badge-hash">官方哈希</span>
          </td>
          <td>
            <span v-if="statusOf(release) === 'installed'" class="vc-ver-status installed">已安装</span>
            <span v-else-if="statusOf(release) === 'downloading'" class="vc-ver-status downloading">{{ form === 'installer' ? '安装中' : '下载中' }}</span>
            <span v-else-if="statusOf(release) === 'error'" class="vc-ver-status error">失败</span>
            <span v-else class="vc-ver-status idle">{{ form === 'installer' ? '可安装' : '可下载' }}</span>
          </td>
          <td>{{ fmtSize(release.size) }}</td>
          <td><code class="mono">{{ (release.commit || '').slice(0, 10) }}</code></td>
          <td>
            <div v-if="statusOf(release) === 'downloading' && ticket(release)?.stage === 'downloading'" class="download-cell">
              <div class="dl-bar-wrap" role="progressbar" :aria-valuenow="stepOf(ticket(release)!)" aria-valuemin="0" aria-valuemax="100">
                <div class="dl-bar-inner" :style="{ width: `${stepOf(ticket(release)!)}%` }"></div>
              </div>
              <span class="dl-percent">{{ stepOf(ticket(release)!) }}%</span>
            </div>
            <div v-else-if="statusOf(release) === 'downloading'" class="dl-meta-text">
              <span v-if="['verify', 'extract', 'install'].includes(ticket(release)!.stage)">
                {{ ticket(release)!.stage === 'install' ? '静默安装中…' : '校验解压中…' }}
              </span>
              <span v-else class="dl-error" :title="ticket(release)!.message">{{ ticket(release)!.message }}</span>
            </div>
            <div v-else-if="statusOf(release) === 'error'" class="dl-meta-text">
              <span class="dl-error" :title="ticket(release)!.message">{{ ticket(release)!.message }}</span>
            </div>
            <button v-if="statusOf(release) === 'idle'" class="btn btn-primary btn-small" @click="emit('download', release)">
              {{ form === 'installer' ? '安装/升级' : '下载安装' }}
            </button>
            <span v-if="statusOf(release) === 'installed'" class="btn btn-ghost btn-small">已安装</span>
            <a v-if="statusOf(release) === 'error'" class="retry-link" @click="emit('download', release)">重试</a>
          </td>
        </tr>
        <tr v-if="releases.length === 0 && !loading">
          <td colspan="5" class="empty-hint">无法加载远程版本列表（官方端点不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.badge-hash { margin-left: 6px; background: var(--state-positive-soft); color: var(--state-positive); }
.vc-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.vc-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.vc-ver-status.installed::before { background: var(--state-positive); }
.vc-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.vc-ver-status.error::before { background: var(--state-danger); }
.vc-ver-status.idle::before { background: var(--color-text-subtle); }
.dl-meta-text { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
