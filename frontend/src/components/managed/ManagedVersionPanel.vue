<!--
  托管版本管理 Tab 主体（Wave 5 · 批 0，黄金样本形态取自 CCSwitchView；
  共享契约增强批扩装）：meta 概览 + 导入/刷新 + 已装卡 grid + 远程 releases 表
  （内嵌下载进度单元格）+ 双空态。以 components/everything/{VersionCard,
  ReleaseTable} 的拆分语义为供体、ccswitch 内联 DOM 为逐字蓝本提炼通用版。

  增强批落点（默认全部缺席 = 现 ccswitch 逐字同形）：
   - 泛型 V（③）：已装记录方言字段包随 adapter 推断；#version-row-extra 具名槽
     以 { record: ManagedVersionRecord<V> } 作用域渲染进已装卡徽标区（paseo
     「未验证哈希」类方言徽标自此免 cast）；
   - 词面覆写全集（④）：区节标题/使用中徽标词/设为使用钮词/导入钮词/官方徽标词/
     下载钮词族/首用一键下载词/进行态词/阶段进度词映射/已装 chip 色调/常态卸载 title；
   - 等效钩子（⑤）：versions.statusOf/sameVersion/implicitActive/downloadBlock
     进面板（recordly 核心互认、paseo 隐式自动最新自此可迁入共享面板渲染）。
  能力自适应：getActive/setActive 缺省的单目录模块（recordly/paseo/papertodo）
  不渲染「使用中」徽标与「设为使用」钮；importLocal 缺省不渲染导入钮。
  store 缺省时自建（独立挂载/测试），组合进壳时由壳注入共享 store。
-->
<script setup lang="ts" generic="V extends object = ManagedVersionDialect">
import { computed } from 'vue'
import type {
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedSnapshot,
  ManagedVersionDialect,
  ManagedVersionRecord,
  NormalizedProgress,
} from './adapter'
import type { ManagedConsoleStore } from './store'
import { useManagedConsole } from './store'
import { fmtSize, fmtDate } from '../../utils/format'

const props = withDefaults(
  defineProps<{
    adapter: ManagedModuleAdapter<ManagedSnapshot, V>
    store?: ManagedConsoleStore<V>
  }>(),
  { store: undefined },
)

const store = props.store ?? useManagedConsole(props.adapter)
const copy = computed(() => props.adapter.copy)

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

/** 版本同一性（⑤）：缺省逐字符相等；recordly 经 sameVersion 落核心互认。 */
function versionEq(a: string, b: string): boolean {
  return props.adapter.versions.sameVersion ? props.adapter.versions.sameVersion(a, b) : a === b
}

/** 隐式使用版本（⑤）：active 为空且模块声明 implicitActive 时回退（paseo 自动最新）。 */
const implicitActive = computed(() => {
  if (store.activeVersion || !props.adapter.versions.implicitActive) return ''
  return props.adapter.versions.implicitActive(store.installed)
})

function statusOf(rel: ManagedReleaseRecord): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = ticketOf(rel)
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hook = props.adapter.versions.statusOf
  if (hook) return hook(rel, store.installed, store.activeVersion || implicitActive.value)
  return store.installed.some((v) => versionEq(v.version, rel.version)) ? 'installed' : 'idle'
}

function isActive(v: ManagedVersionRecord<V>): boolean {
  const eff = store.activeVersion || implicitActive.value
  return !!eff && versionEq(eff, v.version)
}

function isRunning(v: ManagedVersionRecord<V>): boolean {
  return store.state === 'running' && versionEq(store.runningVersion, v.version)
}

/** 卸载禁用 title：运行中/常态两档均经 adapter.copy 覆写（④）。 */
function removeTitle(v: ManagedVersionRecord<V>): string {
  if (isRunning(v)) return copy.value?.uninstallRunningHint ?? '请先退出该工具'
  return copy.value?.uninstallIdleHint ?? ''
}

/** meta 首句引导（④，如 paseo「使用版本 …」）；空=不渲染。 */
const metaLead = computed(() =>
  copy.value?.metaLead
    ? copy.value.metaLead({
        installed: store.installed,
        active: store.activeVersion,
        installedCount: store.installed.length,
        releasesCount: store.releases.length,
      })
    : '',
)

/** 下载钮封锁（⑤）：非空 title 即禁用远程表空闲行下载钮。 */
const downloadBlockedTitle = computed(() => props.adapter.versions.downloadBlock?.(store.state) ?? null)

/** 下载钮词族（④）：缺省「下载安装」。 */
function idleDownloadLabel(rel: ManagedReleaseRecord): string {
  return copy.value?.downloadLabel
    ? copy.value.downloadLabel({ release: rel, installedCount: store.installed.length, state: store.state })
    : '下载安装'
}

/** 阶段进度词映射（④）：缺省 verify/extract→「校验解压安装…」，空串=透出 message。 */
function stageText(p: NormalizedProgress): string {
  if (copy.value?.stageWord) return copy.value.stageWord(p.stage, p)
  return ['verify', 'extract'].includes(p.stage) ? '校验解压安装…' : ''
}

const installedChipClass = computed(() =>
  copy.value?.installedChipTone === 'positive' ? 'chip chip-positive' : 'btn btn-ghost btn-small',
)
</script>

<template>
  <div class="control-panel">
    <div class="meta-info">
      <span v-if="metaLead">{{ metaLead }}</span>
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
      >{{ copy?.importLabel ?? '⇥ 导入本地安装' }}</button>
      <button class="btn btn-secondary btn-small" :disabled="store.loading" @click="store.load()">
        {{ store.loading ? '刷新中…' : '↻ 刷新远程列表' }}
      </button>
    </div>
  </div>

  <!-- 已安装版本 -->
  <div class="section-title"><h3>{{ copy?.installedSectionTitle ?? '已安装版本' }} ({{ store.installed.length }})</h3></div>

  <div v-if="store.installed.length === 0" class="empty-state first-use">
    <p v-if="adapter.copy?.firstUseEmpty">{{ adapter.copy.firstUseEmpty }}</p>
    <button
      v-if="store.releases.length"
      class="btn btn-primary"
      :disabled="!!downloadBlockedTitle"
      :title="downloadBlockedTitle ?? undefined"
      @click="store.runDownload(store.releases[0])"
    >
      {{ copy?.firstUseDownloadLabel ? copy.firstUseDownloadLabel(store.releases[0]) : `下载最新版 ${store.releases[0].version}` }}
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
          <span v-if="isRunning(v)" class="badge badge-running">运行中</span>
          <span v-if="isActive(v)" class="badge badge-active">{{ copy?.activeBadge ?? '使用中' }}</span>
          <span v-if="v.isImport" class="badge badge-import">本地导入</span>
          <span v-else class="badge badge-official">{{ copy?.officialBadge ?? '官方下载' }}</span>
          <!-- 方言徽标位（③）：作用域 record 随泛型 V 携带方言字段（免视图 cast） -->
          <slot name="version-row-extra" :record="v" />
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
          :disabled="store.busy"
          :title="copy?.setActiveTitle"
          @click="store.runSetActive(v)"
        >{{ copy?.setActiveLabel ?? '设为使用' }}</button>
        <button class="btn btn-secondary btn-small" :disabled="store.busy" @click="store.runOpenDir(v)">📂 打开位置</button>
        <button
          class="btn btn-danger-outline btn-small"
          :disabled="store.busy || isRunning(v)"
          :title="removeTitle(v)"
          @click="store.runRemove(v)"
        >卸载</button>
      </div>
    </div>
  </div>

  <!-- 远程可用版本 -->
  <div class="section-title"><h3>{{ copy?.remoteSectionTitle ?? '远程可用版本' }}</h3></div>
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
            <span v-else-if="statusOf(rel) === 'downloading'" class="ver-status downloading">{{ copy?.downloadingWord ?? '下载中' }}</span>
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
              <span v-if="stageText(ticketOf(rel)!)">{{ stageText(ticketOf(rel)!) }}</span>
              <span v-else class="dl-error" :title="ticketOf(rel)!.message">{{ ticketOf(rel)!.message }}</span>
            </div>
            <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
              <span class="dl-error" :title="ticketOf(rel)!.message">{{ ticketOf(rel)!.message }}</span>
            </div>
            <button
              v-if="statusOf(rel) === 'idle'"
              class="btn btn-primary btn-small"
              :disabled="!!downloadBlockedTitle"
              :title="downloadBlockedTitle ?? undefined"
              @click="store.runDownload(rel)"
            >{{ idleDownloadLabel(rel) }}</button>
            <span v-if="statusOf(rel) === 'installed'" :class="installedChipClass">已安装</span>
            <a
              v-if="statusOf(rel) === 'error'"
              class="retry-link"
              :class="{ disabled: !!downloadBlockedTitle }"
              :aria-disabled="!!downloadBlockedTitle"
              :title="downloadBlockedTitle ?? undefined"
              @click="!downloadBlockedTitle && store.runDownload(rel)"
            >重试</a>
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
.retry-link.disabled { color: var(--color-text-subtle); cursor: not-allowed; text-decoration: none; }
</style>
