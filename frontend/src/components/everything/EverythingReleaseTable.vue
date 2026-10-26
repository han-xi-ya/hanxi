<script setup lang="ts">
// Everything 远程可用版本表：通道徽标 + 安装态（含下载进度/校验/失败详情）+ 操作列。
// statusOf/channelLabel 为表格私有的纯派生函数（stepOf 归 managedProgress 单源）；
// 下载安装动作回视图执行（DownloadVersion + 事件驱动的 downloading map 由视图编排）。
// 版本区换装 managed/ManagedVersionPanel 后本表不再挂应用视图，当前仅其 spec 引用（供体留存）。
import type { EverythingRelease, EverythingVersionInfo } from '../../../bindings/hanxi/internal/modules/everything/version/models'
import type { DownloadTicket } from '../../../bindings/hanxi/internal/modules/everything/models'
import { releaseFormWord } from '../managed/adapter'
import { stepOf, type ManagedRowState } from '../managed/managedProgress'
import ManagedVersionDot from '../managed/ManagedVersionDot.vue'
import { fmtSize, fmtDate } from '../../utils/format'

// 绑定已随 Go 侧 Form 字段再生（EverythingRelease 自带可选 form，hostfeed.Form 即字符串枚举）；
// 本 ReleaseRow 别名系再生前遗留的局部交截，与现绑定型已无差，可删回落绑定型。
type ReleaseRow = EverythingRelease & { form?: string }

const props = defineProps<{
  releases: ReleaseRow[]
  installed: EverythingVersionInfo[]
  downloading: Record<string, DownloadTicket>
  loading: boolean
}>()

const emit = defineEmits<{
  'download': [rel: EverythingRelease]
}>()

// 状态点四档词面：交 ManagedVersionDot 渲标准圆点，本表只供档位与词面（组件不做词面推断）。
const STATUS_WORDS: Record<ManagedRowState, string> = {
  installed: '已安装',
  downloading: '下载中',
  error: '失败',
  idle: '可安装',
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
          <td class="ver-cell">
            <strong class="ver-name">{{ rel.version }}</strong>
            <span v-if="rel.stale" class="badge badge-pre">快照</span><!-- 形态 chip：本托管资产形态事实（后端 ListRemote 回填，纯展示），画法对齐 SnipasteReleaseTable --><span
              v-if="rel.form"
              class="chip chip-neutral form-chip"
              :title="`本托管形态（安装链事实）：${releaseFormWord(rel.form)}`"
            >{{ releaseFormWord(rel.form) }}</span>
          </td>
          <td>
            <ManagedVersionDot :status="statusOf(rel)" :text="STATUS_WORDS[statusOf(rel)]" />
          </td>
          <td>{{ fmtSize(rel.size) }}</td>
          <td>{{ fmtDate(rel.published) }}</td>
          <td>
            <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="download-cell">
              <div
                class="dl-bar-wrap"
                role="progressbar"
                aria-valuemin="0"
                aria-valuemax="100"
                :aria-valuenow="stepOf(downloading[rel.version]!)"
                aria-label="下载进度"
              >
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
            <button v-if="statusOf(rel) === 'error'" type="button" class="retry-link" @click="emit('download', rel)">重试</button>
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
/* .table-container 留局系真差：radius 8px vs 全局 --radius-control */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; overflow-x: auto; }
/* 版本列弹性排布：与 Snipaste .release-version 同构（gap 供给间距，徽标/chip 换行不溢出） */
.ver-cell { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
/* 形态 chip 核心形在全局 .form-chip 原子；行内容器带 gap 无需补差，本表不挂副本 */

.channel-badge { font-size: var(--text-xs); font-weight: 600; padding: 2px 8px; border-radius: var(--radius-pill); }
.ch-stable { background: var(--state-positive-soft); color: var(--state-positive); }
.ch-beta { background: var(--state-warning-soft); color: var(--state-warning); }

/* .badge 基形在 components.css 全局原子（§9.6-2），以下仅本表格快照档位；
   版本列弹性排布间距由 gap 单源，本表不挂 margin-left 防双重补偿 */
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); }

/* 状态圆点形制归 managed/ManagedVersionDot.vue，本表不挂 .ver-status 规则，只供档位 + 词面。 */

/* 「已安装」表内标记：区别于全局 .btn-ghost（悬停幽灵按钮）的静态标签态 */
.installed-tag {
  display: inline-flex; align-items: center; padding: 4px 12px; font-size: var(--text-sm);
  border-radius: var(--radius-control); border: 1px solid var(--color-border);
  background: var(--surface-hover); color: var(--color-text-muted);
}

/* .download-cell/.dl-percent/.dl-meta-text/.dl-error/.ver-name 均不挂 scoped 副本，
   落回 components.css 托管家族块 :where 标准形；dl-bar 覆写档留局系真差
   （radius 3px vs 全局 --radius-pill、motion-fast linear vs motion-base ease），裁决未定 */
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
/* 重试是 <button> 而非无 href 的 <a>：键盘可达；外观维持链接形 */
.retry-link { appearance: none; background: none; border: none; padding: 0; font: inherit; color: var(--color-primary); font-size: var(--text-sm); cursor: pointer; margin-left: 8px; text-align: left; }
.retry-link:hover { text-decoration: underline; }
</style>
