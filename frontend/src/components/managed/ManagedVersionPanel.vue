<script setup lang="ts">
// 托管版本管理 Tab 主体（Wave 5 · 批 0，黄金样本形态取自 CCSwitchView）：
// meta 概览 + 导入/刷新 + 已装卡 grid + 远程 releases 表（内嵌下载进度单元格）
// + 双空态。以 components/everything/{VersionCard,ReleaseTable} 的拆分语义为
// 供体、ccswitch 内联 DOM 为逐字蓝本提炼通用版；everything 自身的通道列/
// 内置快照 hint 属方言表，留批 5 不并入。
//
// 能力自适应：getActive/setActive 缺省的单目录模块（recordly/paseo/papertodo）
// 不渲染「使用中」徽标与「设为使用」钮；importLocal 缺省不渲染导入钮；
// active 恒空串时卡片仅按 runningVersion 亮「运行中」。
// store 缺省时自建（独立挂载/测试），组合进壳时由壳注入共享 store。
import type { ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from './adapter'
import type { ManagedConsoleStore } from './store'
import { useManagedConsole } from './store'
import { fmtSize, fmtDate } from '../../utils/format'

const props = withDefaults(
  defineProps<{
    adapter: ManagedModuleAdapter
    store?: ManagedConsoleStore
  }>(),
  { store: undefined },
)

const store = props.store ?? useManagedConsole(props.adapter)

function stepOf(p: { stage: string; done: number; total: number }): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

/** 进度键归一（adapter 铁律②）：行→键，缺省版本串。 */
function ticketOf(rel: ManagedReleaseRecord) {
  return store.downloading[store.progressKeyOf(rel)]
}

function statusOf(rel: ManagedReleaseRecord): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = ticketOf(rel)
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = store.installed.find((v) => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

function isActive(v: ManagedVersionRecord): boolean {
  return !!store.activeVersion && store.activeVersion === v.version
}

function isRunning(v: ManagedVersionRecord): boolean {
  return store.state === 'running' && store.runningVersion === v.version
}

/** 卸载禁用 title：模块名变体经 adapter.copy 提供。 */
function removeTitle(v: ManagedVersionRecord): string {
  return isRunning(v) ? props.adapter.copy?.uninstallRunningHint ?? '请先退出该工具' : ''
}
</script>

<template>
  <div class="control-panel">
    <div class="meta-info">
      <span>
        已安装 <strong>{{ store.installed.length }}</strong> 个版本 ·
        {{ adapter.copy?.remoteSummary ? adapter.copy.remoteSummary(store.releases.length) : `远程版本 ${store.releases.length} 个` }}
      </span>
      <span v-for="(hintLine, i) in adapter.copy?.metaHints ?? []" :key="i" class="hint-dim">{{ hintLine }}</span>
    </div>
    <div class="btn-group">
      <button
        v-if="adapter.versions.importLocal"
        class="btn btn-secondary btn-small"
        :disabled="store.busy"
        @click="store.runImport()"
      >⇥ 导入本地安装</button>
      <button class="btn btn-secondary btn-small" :disabled="store.loading" @click="store.load()">
        {{ store.loading ? '刷新中…' : '↻ 刷新远程列表' }}
      </button>
    </div>
  </div>

  <!-- 已安装版本 -->
  <div class="section-title"><h3>已安装版本 ({{ store.installed.length }})</h3></div>

  <div v-if="store.installed.length === 0" class="empty-state first-use">
    <p v-if="adapter.copy?.firstUseEmpty">{{ adapter.copy.firstUseEmpty }}</p>
    <button v-if="store.releases.length" class="btn btn-primary" @click="store.runDownload(store.releases[0])">
      下载最新版 {{ store.releases[0].version }}
    </button>
    <button v-else-if="!store.loading" class="btn btn-secondary" @click="store.load()">↻ 刷新远程列表</button>
  </div>

  <div class="installed-grid">
    <div
      v-for="v in store.installed"
      :key="v.version"
      class="installed-card"
      :class="{ 'card-active': isActive(v) }"
    >
      <div class="inst-card-top">
        <span class="ver-tag">{{ v.version }}</span>
        <div class="inst-badges">
          <span v-if="isActive(v)" class="badge badge-active">使用中</span>
          <span v-else-if="isRunning(v)" class="badge badge-running">运行中</span>
          <span v-if="v.isImport" class="badge badge-import">本地导入</span>
          <span v-else class="badge badge-official">官方下载</span>
        </div>
      </div>
      <div class="inst-meta">
        <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
        <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
        <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
      </div>
      <div class="inst-actions">
        <button
          v-if="adapter.versions.setActive && !isActive(v)"
          class="btn btn-primary btn-small"
          @click="store.runSetActive(v)"
        >设为使用</button>
        <button class="btn btn-secondary btn-small" @click="store.runOpenDir(v)">📂 打开位置</button>
        <button
          class="btn btn-danger-outline btn-small"
          :disabled="isRunning(v)"
          :title="removeTitle(v)"
          @click="store.runRemove(v)"
        >卸载</button>
      </div>
    </div>
  </div>

  <!-- 远程可用版本 -->
  <div class="section-title"><h3>远程可用版本</h3></div>
  <div class="table-container">
    <table class="tbl">
      <thead>
        <tr>
          <th style="width: 140px;">版本</th>
          <th style="width: 170px;">状态</th>
          <th style="width: 90px;">大小</th>
          <th style="width: 110px;">发布时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="rel in store.releases" :key="rel.version">
          <td>
            <strong class="ver-name">{{ rel.version }}</strong>
            <span v-if="rel.isPre" class="badge badge-pre">预发布</span>
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
            <div v-if="statusOf(rel) === 'downloading' && ticketOf(rel)!.stage === 'downloading'" class="download-cell">
              <div class="dl-bar-wrap">
                <div class="dl-bar-inner" :style="{ width: `${stepOf(ticketOf(rel)!)}%` }"></div>
              </div>
              <span class="dl-percent">{{ stepOf(ticketOf(rel)!) }}%</span>
            </div>
            <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
              <span v-if="['verify', 'extract'].includes(ticketOf(rel)!.stage)">校验解压安装…</span>
              <span v-else class="dl-error" :title="ticketOf(rel)!.message">{{ ticketOf(rel)!.message }}</span>
            </div>
            <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
              <span class="dl-error" :title="ticketOf(rel)!.message">{{ ticketOf(rel)!.message }}</span>
            </div>
            <button
              v-if="statusOf(rel) === 'idle'"
              class="btn btn-primary btn-small"
              @click="store.runDownload(rel)"
            >下载安装</button>
            <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small">已安装</span>
            <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="store.runDownload(rel)">重试</a>
          </td>
        </tr>
        <tr v-if="store.releases.length === 0 && !store.loading">
          <td colspan="5" class="empty-hint">{{ adapter.copy?.remoteUnavailable ?? '无法加载远程版本列表——可稍后点击「↻ 刷新远程列表」重试' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
/* 版本行状态徽标标准形（收编各视图 {前缀}-ver-status 复制体；
   全局原子 .ver-status 不存在，App.vue 的 .status-dot 由 scoped 属性隔离） */
.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: var(--state-positive); }
.ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ver-status.error::before { background: var(--state-danger); }
.ver-status.idle::before { background: var(--color-text-subtle); }

/* 卡片/表内徽标档位色（基形 .badge 与其余家族由 components.css 全局原子接管） */
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }
</style>
