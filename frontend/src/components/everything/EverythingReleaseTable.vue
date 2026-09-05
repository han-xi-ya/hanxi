<script setup lang="ts">
// Everything 远程可用版本表：通道徽标 + 安装态（含下载进度/校验/失败详情）+ 操作列，
// 自 EverythingView 随 DOM 逐字迁出。statusOf/stepOf/channelLabel 为表格私有的纯派生函数；
// 下载安装动作回视图执行（DownloadVersion + 事件驱动的 downloading map 由视图编排）。
import type { EverythingRelease, EverythingVersionInfo } from '../../../bindings/hanxi/internal/modules/everything/version/models'
import type { DownloadTicket } from '../../../bindings/hanxi/internal/modules/everything/models'
import { fmtSize, fmtDate } from '../../utils/format'

const props = defineProps<{
  releases: EverythingRelease[]
  installed: EverythingVersionInfo[]
  downloading: Record<string, DownloadTicket>
  loading: boolean
}>()

const emit = defineEmits<{
  'download': [rel: EverythingRelease]
}>()

function stepOf(p: DownloadTicket): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: EverythingRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = props.downloading[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = props.installed.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

function channelLabel(channel: string): string {
  if (channel === 'stable') return '稳定'
  if (channel === 'beta') return '1.5 测试'
  return channel || '其他'
}
</script>

<template>
  <div class="table-container">
    <table class="tbl">
      <thead>
        <tr>
          <th style="width: 90px;">通道</th>
          <th style="width: 140px;">版本</th>
          <th style="width: 170px;">状态</th>
          <th style="width: 90px;">大小</th>
          <th style="width: 110px;">发布时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="rel in releases" :key="rel.version">
          <td>
            <span class="channel-badge" :class="{ 'ch-stable': rel.channel === 'stable', 'ch-beta': rel.channel !== 'stable' }">
              {{ channelLabel(rel.channel) }}
            </span>
          </td>
          <td>
            <strong class="ver-name">{{ rel.version }}</strong>
            <span v-if="rel.stale" class="badge badge-pre">快照</span>
          </td>
          <td>
            <span v-if="statusOf(rel) === 'installed'" class="ver-status installed">已安装</span>
            <span v-else-if="statusOf(rel) === 'downloading'" class="ver-status downloading">下载中</span>
            <span v-else-if="statusOf(rel) === 'error'" class="ver-status error">失败</span>
            <span v-else class="ver-status idle">可安装</span>
          </td>
          <td>{{ fmtSize(rel.size) }}</td>
          <td>{{ fmtDate(rel.published) }}</td>
          <td>
            <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="download-cell">
              <div class="dl-bar-wrap">
                <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
              </div>
              <span class="dl-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
            </div>
            <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
              <span v-if="['verify', 'extract'].includes(downloading[rel.version]!.stage)">校验解压安装…</span>
              <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
            </div>
            <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
              <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
            </div>
            <button
              v-if="statusOf(rel) === 'idle'"
              class="btn btn-primary btn-small"
              @click="emit('download', rel)"
            >下载安装</button>
            <span v-if="statusOf(rel) === 'installed'" class="installed-tag">已安装</span>
            <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="emit('download', rel)">重试</a>
          </td>
        </tr>
        <tr v-if="releases.length === 0 && !loading">
          <td colspan="6" class="empty-hint">无法加载远程版本列表（官网不可达），已尝试内置快照——可稍后点击「↻ 刷新远程列表」重试</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.channel-badge { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: var(--radius-pill); }
.ch-stable { background: var(--state-positive-soft); color: var(--state-positive); }
.ch-beta { background: var(--state-warning-soft); color: var(--state-warning); }

/* .badge 基形已上收 components.css（§9.6-2），以下仅本表格快照档位 */
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: var(--state-positive); }
.ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ver-status.error::before { background: var(--state-danger); }
.ver-status.idle::before { background: var(--color-text-subtle); }

/* 「已安装」表内标记：区别于全局 .btn-ghost（悬停幽灵按钮）的静态标签态 */
.installed-tag {
  display: inline-flex; align-items: center; padding: 4px 12px; font-size: 12px;
  border-radius: var(--radius-control); border: 1px solid var(--color-border);
  background: var(--surface-hover); color: var(--color-text-muted);
}

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.retry-link { color: var(--color-primary); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: 6px; border: 1px dashed var(--color-border); }
</style>
