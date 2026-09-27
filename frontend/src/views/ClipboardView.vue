<script setup lang="ts">
// 剪贴板内置 · 主界面（A4 线，契约 docs/plans/2026-09-26-clipboard-contract.md）。
//
// 空间语言承接随手记 v2「摊开的笔记本」：顶栏一行轻工具条（无 panel 容器）→
// 状态一行（条目/图片用量机器值，mono）→ 左右分栏——左历史索引贴线行，
// 右页面板"正在读的那一页"。分栏选型同 memo 论证：详情要容纳等宽长文本与
// 图片预览，塞进 ≤410px 的列宽会推挤账目；2560 下整页夹 --container-workbench、
// 正文版心再封顶 72ch。≤860 容器档叠放：索引在上、详情在下（先看账再翻页，
// 与 memo"先落笔"相反——剪贴板的主动作是读旧账不是写新页）。
//
// 后端访问纪律（契约 §7 kernel）：只经 @/adapters/clipboard 门面九方法与
// CLIP_EV 事件常量，类型只 import ../types/clipboard——本文件零 bindings/ 引用。
// 事件实时性：clipboard:updated 无过滤态就地顶置（去重同 id 先摘后插，不重拉
// 全量）；带过滤态重拉一次保查询语义；removed 摘行；paused 回灌状态灯——
// 托盘/热键侧的翻牌这里同步亮。
//
// 计数回填纪律（契约 §4"Get 记一次使用"）：打开详情后只拿 Get 返回值就地
// 刷新该行的 useCount/lastUsedAt/pinned，绝不补发全量 List（省一次整库读）。
// TogglePin 回执同谱就地换行；CreateText 回执直接顶置。
//
// 危险动作两闸：单条删除过 useConfirm danger 闸（图片连带删 blob，不可恢复）；
// 清空走强确认，话术明写"含图片 blob 全清"与条数。复制钮（Set）是回填系统
// 剪贴板不是浏览器复制——toast 按"可 Ctrl+V"语义播报。
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import type { ClipEntry, ClipPaused, ClipRemoved, ClipStatus } from '../types/clipboard'
import {
  CLIP_EV,
  clipboardClearAll,
  clipboardCreateText,
  clipboardDelete,
  clipboardGet,
  clipboardGetStatus,
  clipboardList,
  clipboardSet,
  clipboardSetPaused,
  clipboardTogglePin,
} from '../adapters/clipboard'
import { getErrorMessage } from '../utils/errors'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import PageHeader from '../components/ui/PageHeader.vue'
import ClipToolbar from '../components/clipboard/ClipToolbar.vue'
import ClipRow from '../components/clipboard/ClipRow.vue'
import ClipDetail from '../components/clipboard/ClipDetail.vue'
import { clipStatusLine, type ClipFilter } from '../components/clipboard/clipboardFormat'

const { showToast, showErrorToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()

/** 契约容量钳制 500 条封顶：一次拉满，列表不上虚拟列表的前提（A4 任务书裁决） */
const LIST_LIMIT = 500

const entries = shallowRef<ClipEntry[]>([])
const stats = ref<ClipStatus | null>(null)
const searchKw = ref('')
const kindSel = ref<ClipFilter>('all')
const loading = ref(false)
const errorMsg = ref('')
const paused = ref(false)
const pausing = ref(false)

// 右页面板：正在看的一条（空=闲页）
const detailId = ref('')
const detail = ref<ClipEntry | null>(null)
const detailLoading = ref(false)
const detailError = ref('')

const hasFilter = computed(() => searchKw.value.trim() !== '' || kindSel.value !== 'all')
const clearDisabled = computed(() => (stats.value?.entryCount ?? entries.value.length) === 0)
// 状态文案 computed 一次成型（模板零函数调用纪律——统计行只随 stats 变化重算）
const statusLine = computed(() => (stats.value ? clipStatusLine(stats.value) : ''))

/** 列表行只留列表形态字段：明文正文与 blob 不进账本（内存纪律，行组件不消费） */
function toRowShape(e: ClipEntry): ClipEntry {
  const copy: ClipEntry = { ...e }
  delete copy.text
  delete copy.blobData
  return copy
}

// 搜索/过滤拉取带序号守卫（随手记 loadSeq 同谱）：新查询发起即失效在飞请求，
// 慢响应不得覆盖新状态；卸载同样作废。
let loadSeq = 0
let detailSeq = 0
let searchTimer: ReturnType<typeof setTimeout> | null = null

async function loadList() {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  const seq = ++loadSeq
  loading.value = true
  errorMsg.value = '' // 横幅只反映最近一次尝试：成功即收，失败由 catch 重写
  try {
    // 「片段」是前端合成档：后端 kind 无此值，拉无过滤档后按 manual 过滤行
    const kindParam = kindSel.value === 'manual' ? 'all' : kindSel.value
    let items = (await clipboardList(searchKw.value, kindParam, LIST_LIMIT)) ?? []
    if (kindSel.value === 'manual') items = items.filter((e) => e.manual)
    if (seq !== loadSeq) return
    entries.value = items
  } catch (err: unknown) {
    if (seq !== loadSeq) return
    errorMsg.value = `加载剪贴板历史失败: ${getErrorMessage(err)}`
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

// 搜索框输入即时透传（ClipToolbar 零防抖），350ms 防抖留视图侧；
// 输入瞬间即失效在飞查询，仅最后一次停顿后的请求允许写回。回车立查同拍清抖。
function onSearchInput(value: string) {
  searchKw.value = value
  ++loadSeq
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchTimer = null
    void loadList()
  }, 350)
}

function onSearchEnter() {
  void loadList()
}

function selectKind(k: ClipFilter) {
  if (k === kindSel.value) return
  kindSel.value = k
  void loadList()
}

function clearFilters() {
  searchKw.value = ''
  kindSel.value = 'all'
  void loadList()
}

async function refreshStatus() {
  try {
    const s = await clipboardGetStatus()
    stats.value = s
    paused.value = s.paused // 实况为准（托盘侧改过开关，重启后前端对齐）
  } catch {
    // 状态条是辅助信息：拉不到整条收起，不给主列表判死
  }
}

function mergeTop(e: ClipEntry) {
  const row = toRowShape(e)
  entries.value = [row, ...entries.value.filter((x) => x.id !== row.id)].slice(0, LIST_LIMIT)
}

function removeRow(id: string) {
  entries.value = entries.value.filter((x) => x.id !== id)
}

function patchRow(id: string, patch: Partial<ClipEntry>) {
  entries.value = entries.value.map((x) => (x.id === id ? { ...x, ...patch } : x))
}

// ---- 工具条动作 ----
async function togglePaused() {
  if (pausing.value) return
  pausing.value = true
  const next = !paused.value
  try {
    await clipboardSetPaused(next)
    paused.value = next
    showToast(next ? '已暂停记录：期间的复制不入库' : '已恢复记录')
  } catch (err: unknown) {
    showErrorToast(`暂停开关失败: ${getErrorMessage(err)}`)
  } finally {
    pausing.value = false
  }
}

async function createSnippet() {
  const text = await prompt({
    title: '新建固定片段',
    description: '片段作为常驻条目入库（不被容量淘汰），只存本机（DPAPI 加密）。',
    placeholder: '输入或粘贴要常备的文本…',
    confirmLabel: '存入片段',
  })
  if (text === null) return
  const trimmed = text.trim()
  if (!trimmed) {
    showToast('片段内容为空，未保存')
    return
  }
  try {
    const created = await clipboardCreateText(trimmed)
    showToast('已新建固定片段')
    if (created?.id) mergeTop(created)
    void refreshStatus()
  } catch (err: unknown) {
    showErrorToast(`新建片段失败: ${getErrorMessage(err)}`)
  }
}

async function clearAll() {
  const total = stats.value?.entryCount ?? entries.value.length
  const accepted = await confirm({
    title: '清空全部剪贴板历史？',
    description:
      `将一次擦除全部 ${total} 条历史（含置顶与固定片段），图片 blob 文件一并从本机删除，不可恢复。` +
      '正在系统剪贴板里的内容不受影响。',
    confirmLabel: `清空 ${total} 条`,
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await clipboardClearAll()
    entries.value = []
    closeDetail()
    showToast(`已清空 ${total} 条剪贴板历史`)
    void refreshStatus()
  } catch (err: unknown) {
    showErrorToast(`清空失败: ${getErrorMessage(err)}`)
  }
}

// ---- 行/页动作 ----
async function copyEntry(id: string) {
  if (!id) return
  try {
    await clipboardSet(id)
    showToast('已回填到系统剪贴板，Ctrl+V 即贴')
  } catch (err: unknown) {
    showErrorToast(`复制失败: ${getErrorMessage(err)}`)
  }
}

async function togglePin(id: string) {
  if (!id) return
  try {
    const upd = await clipboardTogglePin(id)
    patchRow(id, { pinned: !!upd.pinned, manual: !!upd.manual })
    if (detailId.value === id && detail.value) detail.value = { ...detail.value, pinned: !!upd.pinned }
    showToast(upd.pinned ? '已置顶' : '已取消置顶')
  } catch (err: unknown) {
    showErrorToast(`操作失败: ${getErrorMessage(err)}`)
  }
}

async function deleteEntry(id: string) {
  if (!id) return
  const accepted = await confirm({
    title: '删除这条剪贴板历史？',
    description: '删除后无法恢复；图片条目会一并销毁其 blob 文件。',
    confirmLabel: '删除',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await clipboardDelete(id)
    removeRow(id)
    if (detailId.value === id) closeDetail()
    showToast('已删除')
    void refreshStatus()
  } catch (err: unknown) {
    showErrorToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

// ---- 详情（Get 记一次使用：回值就地回填行内计数，不发全量 List） ----
async function openDetail(id: string) {
  detailId.value = id
  detail.value = null
  detailError.value = ''
  detailLoading.value = true
  const seq = ++detailSeq
  try {
    const full = await clipboardGet(id)
    if (seq !== detailSeq) return
    detail.value = full
    patchRow(id, {
      useCount: full.useCount,
      lastUsedAt: full.lastUsedAt,
      pinned: !!full.pinned,
      preview: full.preview,
    })
  } catch (err: unknown) {
    if (seq !== detailSeq) return
    detailError.value = `读取详情失败: ${getErrorMessage(err)}`
  } finally {
    if (seq === detailSeq) detailLoading.value = false
  }
}

function closeDetail() {
  detailId.value = ''
  detail.value = null
  detailError.value = ''
  detailLoading.value = false
  ++detailSeq // 作废在飞详情请求
}

function retryDetail() {
  if (detailId.value) void openDetail(detailId.value)
}

// ---- 事件面（契约 §5；useWailsEvent 卸载自动注销） ----
useWailsEvent<ClipEntry>(CLIP_EV.updated, (e) => {
  if (!e || !e.id) return
  if (hasFilter.value) void loadList() // 过滤态下顶置会破坏查询语义：重拉一次保真
  else mergeTop(e)
  void refreshStatus()
})
useWailsEvent<ClipRemoved>(CLIP_EV.removed, ({ id }) => {
  removeRow(id)
  if (detailId.value === id) closeDetail()
})
useWailsEvent<ClipPaused>(CLIP_EV.paused, (p) => {
  paused.value = !!p?.paused
})

// Esc 出口：收起当前详情页（工具条/列表无浮层，一级阶梯即完）
function onGlobalKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && detailId.value) closeDetail()
}

onMounted(() => {
  void loadList()
  void refreshStatus()
  window.addEventListener('keydown', onGlobalKey)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onGlobalKey)
  ++loadSeq // 作废在飞请求，卸载后不得回写状态
  ++detailSeq
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
})
</script>

<template>
  <div class="page-workbench clip-page">
    <PageHeader
      title="剪贴板"
      subtitle="本机剪贴板历史：文本、截图与文件列表全量留痕，明文只存本机（DPAPI 加密）；密码管理器不入库，敏感条目不进 AI 检索通道。"
    />

    <!-- 动作失败/加载失败整页可见（带重试出口，不白屏） -->
    <section v-if="errorMsg" class="banner banner-error" role="alert">
      {{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="loadList">重试</button>
    </section>

    <ClipToolbar
      :search="searchKw"
      :kind="kindSel"
      :paused="paused"
      :pausing="pausing"
      :clear-disabled="clearDisabled"
      @update:search="onSearchInput"
      @search-enter="onSearchEnter"
      @update:kind="selectKind"
      @toggle-paused="togglePaused"
      @create-snippet="createSnippet"
      @clear-all="clearAll"
    />

    <div v-if="stats" class="clip-stats">
      <span class="clip-stats-line">{{ statusLine }}</span>
      <span
        class="clip-stats-excl"
        :title="`以下应用的复制内容不入库：${stats.excludedExes.join('、') || '（无）'}`"
      >排除应用 {{ stats.excludedExes.length }} 个</span>
    </div>

    <div class="clip-desk">
      <nav class="clip-index" :aria-busy="loading ? 'true' : 'false'" aria-label="剪贴板历史索引">
        <div v-if="loading && entries.length === 0" class="state-box" role="status">
          正在读取本机剪贴板历史…
        </div>

        <div v-else-if="entries.length === 0" class="state-box">
          <template v-if="hasFilter">
            没有符合当前检索/过滤的历史条目。
            <button type="button" class="state-action" @click="clearFilters">清除过滤</button>
          </template>
          <template v-else>
            还没有记录到任何内容——复制点什么（Ctrl+C）即会入册；
            或点上方「新片段」常备一条文本。
          </template>
        </div>

        <ClipRow
          v-for="e in entries"
          :key="e.id"
          class="clip-row"
          :entry="e"
          :active="e.id === detailId"
          @select="openDetail(e.id)"
          @copy="copyEntry(e.id)"
          @toggle-pin="togglePin(e.id)"
          @delete="deleteEntry(e.id)"
        />
      </nav>

      <section class="clip-pane" aria-label="条目详情">
        <ClipDetail
          v-if="detailId"
          :entry="detail"
          :loading="detailLoading"
          :error="detailError"
          @close="closeDetail"
          @retry="retryDetail"
          @copy="copyEntry(detailId)"
          @toggle-pin="togglePin(detailId)"
          @delete="deleteEntry(detailId)"
        />
        <div v-else class="clip-idle">
          <p class="clip-idle-note">
            左边是账，这里翻到正在看的那一页。
            点任意一条即取全文（图片出预览、文件列清单）——打开即记一次使用。
          </p>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 治理说明（随手记 v2 同谱）：本视图 scoped 层不重写共享原子类（btn 全族、
   text-input、chip、tag-pill、state-box、banner、mono 等）与全局 token——
   跨页面观感以共享件为准；页面私有需求全走 clip-/ct-/cr-/cd- 私有类叠加。 */
.clip-page { display: flex; flex-direction: column; gap: 10px; container: clip / inline-size; }

/* 状态一行：机器值 mono（私有类挂 font 家族，不覆写全局 .mono 原子） */
.clip-stats { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--color-text-subtle); min-width: 0; }
.clip-stats-line { font-family: var(--font-mono); font-variant-numeric: tabular-nums; color: var(--color-text-muted); }
.clip-stats-excl { cursor: help; }

/* 摊开的两页（随手记 .memo-desk 同构；索引列比便签略宽——行内徽标多） */
.clip-desk {
  display: grid;
  grid-template-columns: minmax(300px, 420px) minmax(0, 1fr);
  gap: 16px;
  align-items: stretch;
  min-width: 0;
}
.clip-index {
  display: flex; flex-direction: column; gap: 3px; min-width: 0;
  max-height: calc(100dvh - 236px); min-height: 300px;
  overflow-y: auto; padding-right: 2px; scrollbar-width: thin;
}
.clip-pane { display: flex; min-width: 0; max-height: calc(100dvh - 236px); min-height: 300px; }

/* 闲页（未选中任何一条的安静版心） */
.clip-idle {
  flex: 1; display: flex; flex-direction: column; align-items: flex-start; justify-content: center; gap: 12px;
  border: 1px dashed var(--color-border); border-radius: var(--radius-element); padding: 24px; min-height: 200px;
}
.clip-idle-note { margin: 0; max-width: 46ch; font-size: var(--text-sm); color: var(--color-text-subtle); line-height: 1.8; }

/* 叠放档：索引在上、详情在下（读旧账动线：先见账、后翻页） */
@container clip (max-width: 860px) {
  .clip-desk { grid-template-columns: minmax(0, 1fr); }
  .clip-index { max-height: 44dvh; }
  .clip-pane { max-height: none; }
}
</style>
