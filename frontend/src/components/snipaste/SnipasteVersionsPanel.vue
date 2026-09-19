<script setup lang="ts">
import type { SnipasteRelease, SnipasteVersionInfo } from '../../../bindings/hanxi/internal/modules/snipaste/version/models'
import SnipasteReleaseTable from './SnipasteReleaseTable.vue'
import SnipasteVersionCard from './SnipasteVersionCard.vue'
import type { SnipasteDownloadTicket } from '../../composables/useSnipasteDownloadTickets'

type ReleaseStatus = 'installed' | 'downloading' | 'error' | 'idle'

const props = defineProps<{
  installed: SnipasteVersionInfo[]
  releases: SnipasteRelease[]
  activeVersion: string
  localLoading: boolean
  remoteLoading: boolean
  localError: string
  remoteError: string
  stale: boolean
  busy: boolean
  rowErrors: Record<string, string>
  isRunningVersion: (version: string) => boolean
  cannotRemoveVersion: (item: SnipasteVersionInfo) => boolean
  removeDisabledTitle: (item: SnipasteVersionInfo) => string
  statusOf: (release: SnipasteRelease) => ReleaseStatus
  ticketOf: (version: string) => SnipasteDownloadTicket | undefined
  fmtSize: (bytes: number) => string
  verificationLabel: (item: SnipasteVersionInfo | SnipasteRelease) => string
  stageText: (progress?: SnipasteDownloadTicket) => string
  percent: (progress?: SnipasteDownloadTicket) => number | null
}>()

const emit = defineEmits<{
  import: []
  'refresh-local': []
  'refresh-remote': []
  'open-dir': [item: SnipasteVersionInfo]
  'set-active': [item: SnipasteVersionInfo]
  remove: [item: SnipasteVersionInfo]
  download: [release: SnipasteRelease]
}>()
</script>

<template>
  <div class="snipaste-versions-stack">
    <div class="control-panel versions-overview">
      <div>
        <h2>版本资源</h2>
        <p>已安装 {{ props.installed.length }} 个版本 · 官网可用 {{ props.releases.length }} 个版本</p>
        <span>官方包按大小、SHA-1（可用时）、ZIP CRC 与布局校验后原子安装。</span>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary" :disabled="props.busy" @click="emit('import')">导入本地</button>
        <button class="btn btn-secondary" :disabled="props.remoteLoading" @click="emit('refresh-remote')">{{ props.remoteLoading ? '刷新中…' : '刷新官网' }}</button>
      </div>
    </div>

    <div v-if="props.localError" class="state-box state-error" role="alert"><strong>读取本地版本失败</strong><span>{{ props.localError }}</span><button class="state-action" @click="emit('refresh-local')">重试</button></div>

    <div class="section-title-row"><div><h2>已安装版本</h2><p>版本相互隔离，当前使用和正在运行可能是不同版本。</p></div></div>
    <div v-if="props.localLoading && !props.installed.length" class="state-box">正在读取本地版本…</div>
    <div v-else-if="!props.installed.length" class="state-box state-empty"><strong>尚未安装 Snipaste</strong><span>可从下方官网下载，或导入已有便携目录。</span></div>
    <div v-else class="installed-grid">
      <SnipasteVersionCard
        v-for="item in props.installed"
        :key="item.version"
        :item="item"
        :active="item.version === props.activeVersion"
        :running="props.isRunningVersion(item.version)"
        :row-error="props.rowErrors[item.version]"
        :verification-label="props.verificationLabel(item)"
        :size-label="props.fmtSize(item.size)"
        :remove-disabled="props.cannotRemoveVersion(item)"
        :remove-title="props.removeDisabledTitle(item)"
        @open-dir="emit('open-dir', $event)"
        @set-active="emit('set-active', $event)"
        @remove="emit('remove', $event)"
      />
    </div>

    <div class="section-title-row remote-title"><div><h2>官网 Windows x64 免安装版</h2><p>仅从 Snipaste 官方域名下载，不使用第三方镜像。</p></div></div>
    <div v-if="props.remoteError" class="state-box state-error" role="alert"><strong>官网版本刷新失败</strong><span>{{ props.remoteError }}</span><button class="state-action" @click="emit('refresh-remote')">重试</button></div>
    <div v-if="props.stale" class="state-box state-warning"><strong>正在显示缓存数据</strong><span>官网暂时不可用，请留意版本信息可能不是最新。</span></div>
    <div v-if="props.remoteLoading && !props.releases.length" class="state-box">正在解析 Snipaste 官网版本…</div>
    <div v-else-if="!props.releases.length" class="state-box state-empty"><strong>没有可用的官网版本</strong><span>官网页面结构可能已变化，可稍后重试或导入本地版本。</span></div>
    <SnipasteReleaseTable
      v-else
      :releases="props.releases"
      :loading="props.remoteLoading"
      :status-of="props.statusOf"
      :ticket-of="props.ticketOf"
      :row-errors="props.rowErrors"
      :size-label="props.fmtSize"
      :verification-label="props.verificationLabel"
      :stage-text="props.stageText"
      :percent="props.percent"
      @download="emit('download', $event)"
    />
  </div>
</template>

<style scoped>
.snipaste-versions-stack { display: grid; gap: 14px; }
.control-panel { border-radius: var(--radius-element); padding: 16px; }
.control-panel h2, .section-title-row h2 { margin: 0; }
.control-panel p, .section-title-row p { margin: 4px 0 0; color: var(--color-text-muted); }
.versions-overview { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.btn-group { align-items: center; flex-wrap: wrap; }
.section-title-row { margin-top: 4px; }
.remote-title { margin-top: 10px; }
.installed-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
@media (max-width: 760px) { .versions-overview { align-items: stretch; flex-direction: column; } .installed-grid { grid-template-columns: 1fr; } .btn-group { justify-content: flex-start; } }
@media (max-width: 460px) { .btn-group .btn { flex: 1 1 auto; } }
</style>
