<script setup lang="ts">
// 历史版本·右栏文件时间线（N33 批 B 自 SnapshotSection 拆出 + §4 行级 diff 上体）：
// 每行一个"该文件有变化的版本"，行上直挂 对比/恢复（D 行=恢复被删内容）；
// 展开对比为 GitHub 式统一单栏（textdiff LCS 行级，删红加绿全 token），
// 连续未变段折叠为「… 省略 K 行 · 展开」（纯呈现层，不吞增删行），
// 超 2000 行只渲染前段 + 展开完整对比；截断态如实标注"仅对比现有部分"。
// 数据与恢复动作全在父级（SnapshotSection 编排），本件只管呈现与抛事件。
// 批 C：行上常驻恢复生效徽标（memo=即时 / config·state=重启，与清单行同源判组）；
// 头部给"共 M 条版本事件（跨改名沿用）"沿链口径整句（与清单窗内口径不硬统一）；
// 批 D：事件行旧式文件名串摘要按现有账回改显示（父级喂受保清单与模式标志），
// 账本原文常驻 tooltip，推不出一律原样，不伪造变化语义。
// ≤640px 事件行折行、说明行整行——动作钮与徽标同带，恢复入口一步可达。
import { computed, ref, watch } from 'vue'
import type { FileDiff, FileRevision, TrackedFile } from '../../../bindings/hanxi/internal/snapshot/models'
import { capRender, diffLines, diffStats, foldContext, type DiffSegment } from '../../utils/textdiff'
import {
  REVISION_WINDOW, backupSummaryNote, changedConfigKeys, decorateLegacySummary,
  fileDisplay, fmtTime, historyCountNote, restoreScopeFor, restoreScopeLabels,
  statusLabels, windowCapNote,
} from '../../constants/snapshotLabels'

const props = withDefaults(defineProps<{
  file: TrackedFile | null
  history: FileRevision[]
  loading: boolean
  diffOpen: string
  diffData: FileDiff | null
  diffLoading: boolean
  /** 受保清单（批 D 旧摘要回改的 memo 标题账来源；缺省空表=只走静态名表） */
  files?: TrackedFile[]
  /** 备份模式：摘要"N 个文件"补整拷贝限定语而非按文件名串回改 */
  backupMode?: boolean
}>(), {
  files: () => [],
  backupMode: false,
})

/** 事件行摘要呈现（批 D 旧摘要回改）：能按现有账推则推，推不出原样；tooltip 留原文。 */
function rowSummary(raw: string): string {
  return props.backupMode ? backupSummaryNote(raw) : decorateLegacySummary(raw, props.files)
}

const emit = defineEmits<{
  (e: 'toggleDiff', row: FileRevision): void
  (e: 'restore', row: FileRevision): void
}>()

// 观察窗如实标注：时间线满窗即可能被后端 maxListRevisions 截断（数字单源 REVISION_WINDOW）
const historyCapped = computed(() => props.history.length >= REVISION_WINDOW)

/** 所选文件的恢复生效徽标（行级常驻；白名单外文件 null → 不出，不猜语义）。 */
const scopeBadge = computed(() => {
  if (!props.file) return null
  const scope = restoreScopeFor(props.file.path)
  return scope ? restoreScopeLabels[scope] : null
})

// ---------- 行级 diff（呈现层状态，换 diffData 即复位） ----------

const expandedFolds = ref<Record<number, boolean>>({})
const renderAll = ref(false)
watch(() => props.diffData, () => {
  expandedFolds.value = {}
  renderAll.value = false
})

const diffLinesAll = computed(() =>
  props.diffData ? diffLines(props.diffData.old, props.diffData.new) : [],
)
const diffStatsView = computed(() => diffStats(diffLinesAll.value))
/** 零增删即"完全一致"（含双侧皆空），此时不渲染满屏 ctx 行。 */
const identical = computed(() => !!props.diffData && diffStatsView.value.added === 0 && diffStatsView.value.deleted === 0)
const cappedLines = computed(() =>
  renderAll.value ? { visible: diffLinesAll.value, hidden: 0 } : capRender(diffLinesAll.value),
)
const segments = computed<DiffSegment[]>(() => foldContext(cappedLines.value.visible))

/** 截断态：后端按 512KB 各截断，对比只在现有部分内进行，不假装完整。 */
const truncNote = computed(() => !!props.diffData && (props.diffData.oldTruncated || props.diffData.newTruncated))
const sideNote = computed(() => {
  const d = props.diffData
  if (!d) return ''
  if (!d.new) return '该版本已删除此文件（下方为删除前内容）'
  if (!d.old) return '该版本新增了此文件'
  return ''
})
/** config.json 的顶层键变化标注（§5 v1 粒度：键名+计数，嵌套不逐项）。 */
const configNote = computed(() => {
  const d = props.diffData
  if (!d || d.path !== 'config.json') return ''
  return changedConfigKeys(d.old, d.new).join('、')
})

function expandFold(idx: number) {
  expandedFolds.value = { ...expandedFolds.value, [idx]: true }
}
</script>

<template>
  <div class="fa-detail">
    <div v-if="!file" class="hist-empty">左侧选择一个文件，查看它的历史时间线。</div>
    <div v-else-if="loading" class="hist-empty">读取时间线…</div>
    <template v-else>
      <div class="fa-detail-head">
        <span class="fa-title">{{ fileDisplay(file) }}</span>
        <code class="fa-path" :title="file.path">{{ file.path }}</code>
        <span v-if="history.length" class="fa-evcount">{{ historyCountNote(history.length) }}</span>
        <span v-if="historyCapped" class="chip chip-neutral">{{ windowCapNote() }}</span>
      </div>
      <div v-if="!history.length" class="hist-empty">该文件在观察窗内还没有历史版本。</div>
      <div v-for="row in history" :key="row.revisionId + row.status" class="fa-ev">
        <div class="fa-ev-row">
          <span class="file-st" :class="`st-${row.status}`">{{ statusLabels[row.status] ?? row.status }}</span>
          <span class="mono fa-time">{{ fmtTime(row.time) }}</span>
          <span class="fa-sum" :title="row.summary">{{ rowSummary(row.summary) }}</span>
          <span class="row-actions">
            <!-- 生效语义常驻在恢复钮旁（批 C）：点了才在确认框里读到，晚了 -->
            <span v-if="scopeBadge" class="chip fa-scope" :class="scopeBadge.chip">{{ scopeBadge.text }}</span>
            <button class="btn btn-ghost btn-small" @click="emit('toggleDiff', row)">{{ diffOpen === row.revisionId ? '收起对比' : '对比' }}</button>
            <button class="btn btn-secondary btn-small" @click="emit('restore', row)">{{ row.status === 'D' ? '恢复被删内容' : '恢复' }}</button>
          </span>
        </div>
        <div v-if="diffOpen === row.revisionId" class="fa-diff">
          <div v-if="diffLoading" class="hist-empty">读取新旧内容…</div>
          <div v-else-if="diffData" class="diff-panel">
            <div class="diff-meta">
              <span class="diff-stat diff-stat-add">+{{ diffStatsView.added }}</span>
              <span class="diff-stat diff-stat-del">−{{ diffStatsView.deleted }}</span>
              <span v-if="sideNote" class="chip chip-neutral diff-note-chip">{{ sideNote }}</span>
              <span v-if="truncNote" class="chip chip-warning preview-cut">已截断，仅对比现有部分</span>
            </div>
            <div v-if="configNote" class="diff-keys">变化设置：{{ configNote }}</div>
            <div v-if="identical" class="hist-empty">新旧内容完全一致，没有行级差异。</div>
            <div v-else class="diff-body">
              <template v-for="(seg, idx) in segments" :key="idx">
                <div v-if="seg.kind === 'line'" class="dl" :class="`dl-${seg.line.type}`">
                  <span class="dl-mark">{{ seg.line.type === 'add' ? '+' : seg.line.type === 'del' ? '−' : '' }}</span>
                  <span class="dl-text">{{ seg.line.text }}</span>
                </div>
                <template v-else>
                  <button v-if="!expandedFolds[idx]" class="dl-fold" @click="expandFold(idx)">… 省略 {{ seg.count }} 行 · 展开</button>
                  <div v-for="(l, li) in (expandedFolds[idx] ? seg.lines : [])" :key="`f${idx}-${li}`" class="dl dl-ctx">
                    <span class="dl-mark"></span>
                    <span class="dl-text">{{ l.text }}</span>
                  </div>
                </template>
              </template>
              <button v-if="cappedLines.hidden > 0" class="dl-fold" @click="renderAll = true">展开完整对比（另有 {{ cappedLines.hidden }} 行未渲染）</button>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* 皮与 SnapshotSection 批 A 语系逐字同构（拆分不动相）；diff 单栏为 §4 新增 */
.fa-detail { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.fa-detail-head {
  display: flex; align-items: center; gap: 8px;
  padding-bottom: 6px; border-bottom: 1px solid var(--color-border);
}
.fa-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text); flex: none; }
.fa-path {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0;
}
.fa-evcount { flex: none; font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; }
.fa-ev-row { display: flex; align-items: center; gap: 8px; padding: 5px 4px; min-width: 0; }
.fa-time { flex: none; }
/* 恢复生效常驻徽标：micro chip，与清单行同款语系，色走全局 chip token */
.fa-scope { flex: none; font-size: var(--text-xs); padding: 0 5px; }
.fa-sum {
  flex: 1; min-width: 0; font-size: var(--text-sm); color: var(--color-text-muted);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.row-actions { display: flex; gap: 6px; flex: none; }
.hist-empty { padding: 18px 4px; font-size: var(--text-sm); color: var(--color-text-muted); }
.mono { font-family: var(--font-mono); font-size: var(--text-xs); }
.file-st { flex: none; width: 40px; font-size: var(--text-xs); color: var(--color-text-muted); }
.file-st.st-A { color: var(--state-positive); }
.file-st.st-D { color: var(--state-danger); }

.fa-diff { padding: 2px 4px 10px; }
.diff-panel {
  border: 1px solid var(--color-border); border-radius: var(--radius-element); overflow: hidden;
  background: var(--surface-panel);
}
.diff-meta {
  display: flex; align-items: center; gap: 8px; padding: 6px 10px;
  background: var(--surface-chrome); border-bottom: 1px solid var(--color-border);
}
.diff-stat { font-family: var(--font-mono); font-size: var(--text-xs); font-weight: 600; }
.diff-stat-add { color: var(--state-positive); }
.diff-stat-del { color: var(--state-danger); }
.diff-note-chip { font-size: var(--text-xs); padding: 0 6px; }
.preview-cut { flex: none; font-size: var(--text-xs); }
.diff-keys {
  padding: 5px 10px; font-size: var(--text-xs); color: var(--color-text-muted);
  border-bottom: 1px solid var(--color-border); background: var(--surface-soft);
}
.diff-body { max-height: 260px; overflow: auto; }
.dl { display: flex; gap: 6px; padding: 0 10px; font-family: var(--font-mono); font-size: var(--text-xs); line-height: 1.55; }
.dl-mark { flex: none; width: 10px; user-select: none; color: var(--color-text-subtle); }
.dl-text { flex: 1; min-width: 0; white-space: pre-wrap; word-break: break-all; color: var(--color-text); }
/* 删红加绿取工作台状态色 soft 底（明暗双主题各配其值），文字色带角色前缀——色不是唯一通道 */
.dl-add { background: var(--state-positive-soft); }
.dl-add .dl-mark { color: var(--state-positive); }
.dl-del { background: var(--state-danger-soft); }
.dl-del .dl-mark { color: var(--state-danger); }
.dl-fold {
  display: block; width: 100%; text-align: left; cursor: pointer;
  padding: 3px 10px; border: none; border-top: 1px solid var(--color-border); border-bottom: 1px solid var(--color-border);
  background: var(--surface-chrome); color: var(--color-text-muted);
  font-size: var(--text-xs); font-family: inherit;
}
.dl-fold:hover { background: var(--surface-chrome-hover); color: var(--color-text); }

/* 批 C 窄屏档（≤640px，依托父级 .fa-body 720px 起的纵排）：头部与事件行折行，
   说明行让位整行、生效徽标与恢复钮同带留在首行——恢复入口一步可达；
   diff 长行由 .dl-text pre-wrap+break-all 消化，内容盒 min-width:0 链保持，
   390px 不撑爆页面；本档不加 transition，动效归零走 base.css 全局 reduced-motion 块 */
@media (max-width: 640px) {
  .fa-detail-head { flex-wrap: wrap; }
  .fa-ev-row { flex-wrap: wrap; }
  .fa-sum { flex-basis: 100%; order: 3; }
  .diff-meta { flex-wrap: wrap; }
}
</style>
