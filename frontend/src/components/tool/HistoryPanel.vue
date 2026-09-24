<script setup lang="ts">
// 通用历史记录面板（自取数组件，仿 FrpcVersionsTab）：按 funcType 桶自拉自刷新，
// 宿主视图只管挂载与接 apply 事件。契约与口径见 docs/plans/PLAN_HISTORY.md §2.3：
// 行内数据直用（Q6，应用/复制不回查）、清空仅作用当前桶（Q5）、删除走 ID。
// 搜索 350ms 防抖 + seq 过期丢弃（useEverythingSearch 同款），历史损坏时给只读降级提示。
import { onMounted, ref, watch } from 'vue'
import * as HistoryAPI from '../../../bindings/hanxi/internal/history/historyservice'
import type { Record as HistoryRecord } from '../../../bindings/hanxi/internal/history/models'
import { useToast } from '../../composables/useToast'
import { useClipboard } from '../../composables/useClipboard'
import { useConfirm } from '../../composables/useConfirm'
import { getErrorMessage } from '../../utils/errors'
import { fmtDateTimeSmart } from '../../utils/format'

// —— N20b 标记列中文化 ——
// extra 是各模块记录点写入的机器 token（竖线连拼，见 ocr/portkill/npmtool
// recordHistory），展示层翻成人话；词表外的未知 token **原样露出**——宁显机器串，
// 不瞎猜语义。title 恒留原始串供诊断/搜索对账。
const EXTRA_LABELS: Record<string, string> = {
  ui: '界面识别',
  snip: '框选识别',
  query: '端口查询',
  kill: '终止进程',
  elevated: '提权终止',
  'npm-install': 'npm 安装',
  'npm-update': 'npm 升级',
  'npm-remove': 'npm 卸载',
  nofull: '未存全文',
  fail: '失败',
  denied: '已拒绝',
}
const EXTRA_DANGER = new Set(['fail', 'denied'])
function extraChips(extra: string): { key: string; label: string; danger: boolean }[] {
  return (extra || '')
    .split('|')
    .filter((t) => t !== '')
    .map((t) => ({ key: t, label: EXTRA_LABELS[t] ?? t, danger: EXTRA_DANGER.has(t) }))
}

// 空态按桶说清"什么时候会有记录"（通用文案只说"暂无"等于没说）。
const EMPTY_HINTS: Record<string, string> = {
  ocr: '还没有识别记录——页面上「识别」、右键长按轮盘框选、热键剪贴板识图都会留档（成败同记）；每桶最多保留最近的记录，「清空本桶」只影响这里。',
  portkill: '还没有查询/终止记录——端口查询与进程终止都留档，被红线或 UAC 拒绝的终止也会记下并标「已拒绝」，方便回看当时为什么没杀掉。',
  envcheck: '还没有 npm 工具操作记录——环境体检页对 claude/codex 等受管工具的装、升、卸会留在这里。',
}

const props = withDefaults(
  defineProps<{
    /** 分桶键（"ocr" / "portkill" / "envcheck" …，对齐模块注册 ID） */
    funcType: string
    /** 列表区最大高度 */
    maxHeight?: string
    /** 是否显示"应用"动作（宿主无回填目标时置 false，envcheck 用） */
    showApply?: boolean
  }>(),
  { maxHeight: '320px', showApply: true },
)

const emit = defineEmits<{ apply: [rec: HistoryRecord] }>()

const { showToast } = useToast()
const { copy } = useClipboard()
const { confirm } = useConfirm()

const rows = ref<HistoryRecord[]>([])
const keyword = ref('')
const loading = ref(false)
const loadError = ref('')
const selected = ref<HistoryRecord | null>(null)

// 过期响应丢弃：搜索防抖连发时只认最后一次请求的结果
let seq = 0

async function load() {
  const my = ++seq
  loading.value = true
  try {
    const list = await HistoryAPI.List(props.funcType, keyword.value)
    if (my !== seq) return
    rows.value = list ?? []
    loadError.value = ''
    // 选中项随刷新对齐：按 ID 找回，找不回则落到最新一条
    const id = selected.value?.id
    selected.value = (id !== undefined ? rows.value.find((r) => r.id === id) : undefined) ?? rows.value[0] ?? null
  } catch (e: unknown) {
    if (my !== seq) return
    rows.value = []
    selected.value = null
    loadError.value = `读取历史记录失败（存量损坏时历史只读禁新增）: ${getErrorMessage(e)}`
  } finally {
    if (my === seq) loading.value = false
  }
}

// keyword 350ms 防抖（useEverythingSearch 同款节奏）
let debounceTimer: ReturnType<typeof setTimeout> | undefined
watch(keyword, () => {
  clearTimeout(debounceTimer)
  debounceTimer = setTimeout(load, 350)
})

async function removeRow(rec: HistoryRecord) {
  try {
    await HistoryAPI.Delete(rec.id)
    await load()
  } catch (e: unknown) {
    showToast(`删除失败: ${getErrorMessage(e)}`)
  }
}

async function clearBucket() {
  const accepted = await confirm({
    title: '清空本功能的历史记录？',
    description: '仅清空当前列表（本桶），其它功能的历史不受影响；清空后无法找回。',
    confirmLabel: '清空',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await HistoryAPI.Clear(props.funcType)
    await load()
  } catch (e: unknown) {
    showToast(`清空失败: ${getErrorMessage(e)}`)
  }
}

function applyRow(rec: HistoryRecord) {
  emit('apply', rec)
}

async function copyField(field: 'input' | 'output') {
  const v = selected.value?.[field] || ''
  if (!v) {
    showToast('该字段为空')
    return
  }
  const ok = await copy(v)
  showToast(ok ? '已复制到剪贴板' : '复制失败')
}

function tailText(v: string, n = 200) {
  return v.length > n ? `${v.slice(0, n)}…` : v
}

onMounted(load)
defineExpose({ reload: load })
</script>

<template>
  <div class="history-panel">
    <div class="hp-toolbar">
      <input
        v-model="keyword"
        class="hp-search"
        type="search"
        placeholder="搜索摘要 / 输入 / 输出 / 标记…"
        aria-label="搜索历史记录"
      />
      <button class="btn btn-secondary btn-small" :disabled="loading" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
      <button class="btn btn-danger-outline btn-small" :disabled="!rows.length" @click="clearBucket">
        清空本桶
      </button>
    </div>

    <div v-if="loadError" class="error-box" role="alert">{{ loadError }}</div>

    <div class="hp-list" :style="{ maxHeight }">
      <table v-if="rows.length" class="tbl">
        <thead>
          <tr>
            <th>摘要</th>
            <th style="width: 168px;">时间</th>
            <th style="width: 120px;">标记</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="rec in rows"
            :key="rec.id"
            :class="{ 'hp-row-active': selected?.id === rec.id }"
            tabindex="0"
            @click="selected = rec"
            @keydown.enter="selected = rec"
            @dblclick="showApply && applyRow(rec)"
          >
            <td class="hp-sum" :title="rec.summary">{{ rec.summary || '—' }}</td>
            <td class="hp-time" :title="rec.createdAt" :data-ts="rec.createdAt">{{ fmtDateTimeSmart(rec.createdAt) }}</td>
            <td class="hp-extra" :title="rec.extra" :data-raw-extra="rec.extra">
              <template v-if="extraChips(rec.extra).length">
                <span v-for="c in extraChips(rec.extra)" :key="c.key" class="hp-tag" :class="{ 'hp-tag-danger': c.danger }">{{ c.label }}</span>
              </template>
              <template v-else>—</template>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else-if="!loading" class="empty-state hp-empty">
        <p>{{ EMPTY_HINTS[funcType] ?? '暂无历史记录——用几次本功能后回来看看。' }}</p>
      </div>
    </div>

    <div v-if="selected" class="hp-detail">
      <div class="hp-detail-head">
        <span class="hp-detail-title mono">{{ selected.input || '（无输入留档）' }}</span>
        <button class="btn btn-danger-outline btn-micro" @click="removeRow(selected)">删除本条</button>
      </div>
      <pre v-if="selected.output" class="hp-output" :title="selected.output">{{ tailText(selected.output, 2000) }}</pre>
      <div class="hp-actions">
        <button v-if="showApply" class="btn btn-primary btn-small" @click="applyRow(selected)">应用</button>
        <button class="btn btn-secondary btn-small" :disabled="!selected.input" @click="copyField('input')">复制输入</button>
        <button class="btn btn-secondary btn-small" :disabled="!selected.output" @click="copyField('output')">复制输出</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 全 var() token + components.css 原子（.btn/.tbl/.error-box/.empty-state），禁新造颜色 */
.history-panel { display: flex; flex-direction: column; gap: 10px; min-width: 0; }

.hp-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.hp-search {
  flex: 1; min-width: 160px; padding: 6px 10px; font-size: var(--text-sm);
  border: 1px solid var(--color-border); border-radius: var(--radius-control);
  background: var(--surface-soft); color: var(--color-text); outline: none;
}
.hp-search:focus { border-color: var(--color-primary); }

.hp-list { overflow: auto; border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); }
.hp-list table { margin: 0; }
.hp-row-active { background: var(--surface-selected); }
.hp-list tr { cursor: pointer; }
.hp-sum { max-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: var(--text-sm); }
.hp-time { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--text-xs); color: var(--color-text-muted); white-space: nowrap; }
.hp-extra { font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
/* N20b 标记 chips：中性灰底，失败/拒绝升警示色（颜色之外字面已自明） */
.hp-tag { display: inline-block; margin-right: 4px; padding: 0 6px; border: 1px solid var(--color-border); border-radius: var(--radius-pill, 999px); font-size: var(--text-micro, 10px); color: var(--color-text-muted); background: var(--surface-soft); }
.hp-tag-danger { color: var(--state-warning); border-color: var(--state-warning); }
.hp-empty { border: none; }

.hp-detail {
  display: flex; flex-direction: column; gap: 8px; padding: 10px 12px;
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
.hp-detail-head { display: flex; align-items: center; gap: 8px; justify-content: space-between; }
.hp-detail-title { font-size: var(--text-xs); color: var(--color-text-muted); overflow-wrap: anywhere; }
.hp-output {
  margin: 0; padding: 8px 10px; max-height: 140px; overflow: auto; white-space: pre-wrap; word-break: break-word;
  font-size: var(--text-sm); line-height: 1.5; color: var(--color-text);
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
}
.hp-actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
