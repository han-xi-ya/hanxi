<script setup lang="ts">
// 极客随手记（2026-09 整页重设计，参考 MooTool/Geek 系随手记的信息密度）：
// 「记」是本模块的绝对主角——顶部速记条把"想到即存"压到一次回车；完整编辑器
// （Markdown 预览）降级为次级入口；检索/标签过滤是读回旧条目的动作，参数化收进
// 一列工具条；列表按 置顶→今天→昨天→7天内→30天内→更早 时间分组，扫读不数日期；
// 阅读态详情浮层（Markdown 渲染）与编辑态分离，卡片只留摘要与就地微操作；
// 「一键全删」挂页头 danger 钮，强确认（useConfirm danger tone，明说条数与不可恢复）。
// 数据契约零改动：List/GetStats/Create/Update/Delete/TogglePin/ToggleMask 语义照旧，
// 本轮新增面仅 ClearAll（后端返回删除条数）。memo:changed 事件重拉纪律照旧。
// 敏感遮罩纪律原样：IsMasked 条目卡片/详情/预览三处都不落明文，揭示是显式动作。
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import * as MemoAPI from '../../bindings/hanxi/internal/modules/memo'
import type {
  MemoItem,
  MemoFilter,
  MemoStats,
} from '../../bindings/hanxi/internal/modules/memo/models'
import { getErrorMessage } from '../utils/errors'
import { fmtDate } from '../utils/format'
import { looksLikeMarkdown, renderMarkdown } from '../utils/markdown'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import UiClipboardField from '../components/ui/UiClipboardField.vue'
import UiButton from '../components/ui/UiButton.vue'
import PageHeader from '../components/ui/PageHeader.vue'

const { showToast } = useToast()
const { copyWithToast } = useClipboard()
const { confirm } = useConfirm()

// 状态定义
const memos = shallowRef<MemoItem[]>([])
const stats = ref<MemoStats>({
  totalCount: 0,
  pinnedCount: 0,
  tagCloud: {},
})

const searchKw = ref('')
const selectedTag = ref('')
const filterPinned = ref<boolean | null>(null)
const loading = ref(false)
const errorMsg = ref('')

// ---- 速记条（主角动作：想到即存）----
const quickText = ref('')
const quickTagsInput = ref('')
const quickSaving = ref(false)

// ---- 完整编辑器（次级入口：长文/代码/Markdown）----
const showEditor = ref(false)
const isEditing = ref(false)
const editMode = ref<'write' | 'preview'>('write')
const editForm = ref({
  id: '',
  title: '',
  content: '',
  tagInput: '',
  tags: [] as string[],
  colorTag: 'blue',
})

// ---- 阅读态详情浮层 ----
const detailItem = shallowRef<MemoItem | null>(null)

const COLOR_OPTIONS = [
  { name: '蓝色', val: 'blue', hex: '#3b82f6' },
  { name: '翡翠绿', val: 'emerald', hex: '#10b981' },
  { name: '琥珀橙', val: 'amber', hex: '#f59e0b' },
  { name: '玫瑰红', val: 'rose', hex: '#f43f5e' },
  { name: '紫罗兰', val: 'purple', hex: '#8b5cf6' },
]

// 搜索/过滤拉取带序号守卫（模式同 useEverythingSearch 的 searchSeq）：
// 新查询发起即失效旧请求，List+GetStats 的慢响应不得覆盖新状态（loading 亦只由最新请求收口）。
let loadSeq = 0
let searchTimer: ReturnType<typeof setTimeout> | null = null

async function loadMemos() {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  const seq = ++loadSeq
  loading.value = true
  errorMsg.value = '' // 横幅只反映最近一次尝试：成功即收，失败由 catch 重写
  try {
    const filter: MemoFilter = {
      keyword: searchKw.value,
      tag: selectedTag.value,
      pinned: filterPinned.value,
      sortBy: 'updated',
      sortDesc: true,
    }

    const [items, curStats] = await Promise.all([
      MemoAPI.MemoService.List(filter),
      MemoAPI.MemoService.GetStats(),
    ])

    if (seq !== loadSeq) return
    memos.value = items ?? []
    stats.value = curStats
    // 详情浮层跟着最新账走：条目被别端改/删就换身或收起，不留陈旧明文
    if (detailItem.value) {
      const fresh = (items ?? []).find((m) => m.id === detailItem.value?.id)
      if (!fresh) detailItem.value = null
      else detailItem.value = fresh
    }
  } catch (err: unknown) {
    if (seq !== loadSeq) return
    errorMsg.value = `加载备忘录失败: ${getErrorMessage(err)}`
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

// 搜索框输入：350ms 防抖；输入瞬间即失效在飞查询，仅最后一次停顿后的请求允许写回。
function onSearchInput() {
  ++loadSeq
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchTimer = null
    void loadMemos()
  }, 350)
}

function handleSelectTag(tag: string) {
  selectedTag.value = selectedTag.value === tag ? '' : tag
  loadMemos()
}

function clearFilters() {
  searchKw.value = ''
  selectedTag.value = ''
  filterPinned.value = null
  loadMemos()
}

const hasFilter = computed(
  () => searchKw.value !== '' || selectedTag.value !== '' || filterPinned.value !== null,
)

// ---- 时间分组（置顶由后端排序保证在前，这里按 updatedAt 再切自然日档）----
interface MemoGroup { key: string; label: string; items: MemoItem[] }

function dayBucket(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '更早'
  const now = new Date()
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const days = Math.round((midnight(now) - midnight(d)) / 86400000)
  if (days <= 0) return '今天'
  if (days === 1) return '昨天'
  if (days <= 7) return '7 天内'
  if (days <= 30) return '30 天内'
  return '更早'
}

const groupedMemos = computed<MemoGroup[]>(() => {
  const order = ['置顶', '今天', '昨天', '7 天内', '30 天内', '更早']
  const map = new Map<string, MemoItem[]>()
  for (const m of memos.value) {
    const key = m.isPinned ? '置顶' : dayBucket(m.updatedAt)
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(m)
  }
  return order
    .filter((k) => map.has(k))
    .map((k) => ({ key: k, label: k, items: map.get(k)! }))
})

function fmtAgo(iso: string): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const min = Math.floor((Date.now() - t) / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  if (min < 24 * 60) return `${Math.floor(min / 60)} 小时前`
  return fmtDate(iso)
}

// 卡片摘要：等宽纯文本前几行（代码/SQL 便签保排版；Markdown 全文渲染在详情浮层）
function cardPreview(content: string): string {
  const lines = content.split('\n').filter((l) => l.trim() !== '')
  if (lines.length === 0) return ''
  return lines.slice(0, 5).join('\n') + (lines.length > 5 ? '\n…' : '')
}

// ---- 速记 ----
function parseQuickTags(): string[] {
  const raw = quickTagsInput.value.split(/[\s,，、;；]+/).filter(Boolean)
  const set = new Set<string>()
  for (const t of raw) set.add(t.startsWith('#') ? t : `#${t}`)
  if (selectedTag.value) set.add(selectedTag.value)
  return [...set]
}

function onQuickEnter(e: KeyboardEvent) {
  // 中文输入法组字中的回车只结束Candidates，不该触发保存
  if (e.isComposing) return
  void commitQuick()
}

async function commitQuick() {
  const text = quickText.value.trim()
  if (!text || quickSaving.value) return
  quickSaving.value = true
  try {
    await MemoAPI.MemoService.Create('', text, parseQuickTags(), 'blue')
    quickText.value = ''
    quickTagsInput.value = ''
    showToast('已记下')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`记录失败: ${getErrorMessage(err)}`)
  } finally {
    quickSaving.value = false
  }
}

// ---- 编辑器 ----
function openCreateModal() {
  isEditing.value = false
  editMode.value = 'write'
  editForm.value = {
    id: '',
    title: '',
    content: '',
    tagInput: '',
    tags: selectedTag.value ? [selectedTag.value] : [],
    colorTag: 'blue',
  }
  showEditor.value = true
}

function openEditModal(item: MemoItem) {
  isEditing.value = true
  editMode.value = 'write'
  editForm.value = {
    id: item.id,
    title: item.title,
    content: item.content,
    tagInput: '',
    tags: item.tags ? [...item.tags] : [],
    colorTag: item.colorTag || 'blue',
  }
  showEditor.value = true
}

function addTagFromInput() {
  const val = editForm.value.tagInput.trim()
  if (!val) return
  const formatted = val.startsWith('#') ? val : '#' + val
  if (!editForm.value.tags.includes(formatted)) {
    editForm.value.tags.push(formatted)
  }
  editForm.value.tagInput = ''
}

function removeTag(tag: string) {
  editForm.value.tags = editForm.value.tags.filter((t) => t !== tag)
}

async function handleSaveMemo() {
  addTagFromInput()
  if (!editForm.value.title.trim() && !editForm.value.content.trim()) {
    showToast('标题或内容至少填写一项')
    return
  }

  try {
    if (isEditing.value) {
      await MemoAPI.MemoService.Update(
        editForm.value.id,
        editForm.value.title,
        editForm.value.content,
        editForm.value.tags,
        editForm.value.colorTag,
      )
      showToast('备忘录已更新')
    } else {
      await MemoAPI.MemoService.Create(
        editForm.value.title,
        editForm.value.content,
        editForm.value.tags,
        editForm.value.colorTag,
      )
      showToast('已新建备忘录')
    }
    showEditor.value = false
    await loadMemos()
  } catch (err: unknown) {
    showToast(`保存失败: ${getErrorMessage(err)}`)
  }
}

// 编辑器预览态：所见即详情浮层所得（同一渲染器同源）
const editorPreviewHtml = computed(() => renderMarkdown(editForm.value.content))

// ---- 详情浮层 ----
function openDetail(item: MemoItem) {
  detailItem.value = item
}
function closeDetail() {
  detailItem.value = null
}
// 详情浮层动作的 null 收窄在脚本侧完成（模板内联多语句表达式不做类型收窄）
function maskFromDetail() {
  if (detailItem.value) void handleToggleMask(detailItem.value)
}
function editFromDetail() {
  const cur = detailItem.value
  if (!cur) return
  detailItem.value = null
  openEditModal(cur)
}
const detailHtml = computed(() =>
  detailItem.value && !detailItem.value.isMasked && looksLikeMarkdown(detailItem.value.content)
    ? renderMarkdown(detailItem.value.content)
    : '',
)

// ---- 卡片微操作 ----
async function handleTogglePin(item: MemoItem) {
  try {
    const pinned = await MemoAPI.MemoService.TogglePin(item.id)
    showToast(pinned ? '已置顶便签' : '已取消置顶')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`操作失败: ${getErrorMessage(err)}`)
  }
}

async function handleToggleMask(item: MemoItem) {
  try {
    const masked = await MemoAPI.MemoService.ToggleMask(item.id)
    showToast(masked ? '已开启脱敏遮罩' : '已揭示明文')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`操作失败: ${getErrorMessage(err)}`)
  }
}

async function handleDeleteMemo(id: string) {
  const accepted = await confirm({
    title: '确定删除这条便签吗？',
    description: '删除后无法恢复。',
    confirmLabel: '删除',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await MemoAPI.MemoService.Delete(id)
    if (detailItem.value?.id === id) detailItem.value = null
    showToast('已删除便签')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

// ---- 一键全删 ----
async function handleClearAll() {
  const total = stats.value.totalCount
  const accepted = await confirm({
    title: '清空全部便签？',
    description: `将一次删光全部 ${total} 条便签（含置顶与敏感遮罩条目），并同步销毁迁移留底、损坏取证副本等数据残片。此操作不可恢复。`,
    confirmLabel: `全删 ${total} 条`,
    tone: 'danger',
  })
  if (!accepted) return
  try {
    const deleted = await MemoAPI.MemoService.ClearAll()
    closeDetail()
    showToast(`已全删 ${deleted} 条便签`)
    await loadMemos()
  } catch (err: unknown) {
    showToast(`全删失败: ${getErrorMessage(err)}`)
  }
}

// 剪贴板两级策略已收编进 useClipboard（toast 文案与原实现逐字一致）
async function copyMemoContent(content: string) {
  await copyWithToast(content, '已复制便签内容到剪贴板')
}

// 浮层 Esc 出口（编辑/详情共用）
function onModalKey(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  if (showEditor.value) showEditor.value = false
  else if (detailItem.value) detailItem.value = null
}

// memo:changed 由局域网投递/多端联动触发：setup 期订阅防丢早期事件，卸载自动注销
useWailsEvent('memo:changed', () => {
  loadMemos()
})

onMounted(() => {
  loadMemos()
  window.addEventListener('keydown', onModalKey)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onModalKey)
  ++loadSeq // 作废在飞请求，卸载后不得回写状态
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
})
</script>

<template>
  <div class="page memo-page">
    <PageHeader
      title="极客随手记"
      subtitle="想到什么敲什么：速记一次回车即存；长文、代码与 Markdown 走「新建便签」。本地文件库持久化，随数据目录迁移不丢失，支持手机局域网投递。"
    >
      <template #actions>
        <div class="head-btns">
          <UiButton variant="primary" @click="openCreateModal">＋ 新建便签</UiButton>
          <UiButton
            variant="danger"
            :disabled="stats.totalCount === 0"
            title="一次删光全部便签（含敏感遮罩条目），需强确认"
            @click="handleClearAll"
          >
            一键全删{{ stats.totalCount ? ` (${stats.totalCount})` : '' }}
          </UiButton>
        </div>
      </template>
    </PageHeader>

    <!-- 动作失败/加载失败整页可见（带重试出口） -->
    <section v-if="errorMsg" class="banner banner-error" role="alert">
      {{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary retry-btn" @click="loadMemos">重试</button>
    </section>

    <!-- ① 主角：速记条 -->
    <section class="panel memo-hero" aria-label="速记">
      <div class="hero-row">
        <input
          v-model="quickText"
          class="text-input hero-input"
          type="text"
          placeholder="敲一行就记一条，回车即存…"
          aria-label="速记内容"
          @keydown.enter="onQuickEnter"
        />
        <input
          v-model="quickTagsInput"
          class="text-input hero-tags"
          type="text"
          placeholder="#标签 空格分隔"
          aria-label="速记标签"
          @keydown.enter="onQuickEnter"
        />
        <UiButton
          variant="primary"
          :disabled="!quickText.trim() || quickSaving"
          @click="commitQuick"
        >
          {{ quickSaving ? '记录中…' : '记录' }}
        </UiButton>
      </div>
      <p class="hero-hint">
        速记不带标题，正文即全貌；输入法组字中的回车只结束选词。
        <template v-if="selectedTag">当前正按 <b class="mono">{{ selectedTag }}</b> 过滤，速记会自动带上该标签。</template>
      </p>
    </section>

    <!-- ② 读回旧条目：检索与过滤参数条 -->
    <section class="panel memo-toolbar" aria-label="检索与过滤">
      <div class="tool-row">
        <div class="search-box">
          <input
            v-model="searchKw"
            type="text"
            class="text-input search-input"
            placeholder="搜索标题、正文、代码片段或标签…"
            aria-label="搜索便签"
            @input="onSearchInput"
          />
          <button v-if="searchKw" class="memo-clear-btn" title="清空搜索" @click="searchKw = ''; loadMemos()">✕</button>
        </div>
        <button
          class="tag-chip"
          :class="{ active: filterPinned === true }"
          title="只看置顶便签"
          @click="filterPinned = filterPinned === true ? null : true; loadMemos()"
        >
          📌 只看置顶
        </button>
        <button v-if="hasFilter" class="btn btn-ghost btn-small clear-filter-btn" @click="clearFilters">清除过滤</button>
        <span class="tool-count mono">{{ stats.pinnedCount }} 置顶 / {{ stats.totalCount }} 全库</span>
      </div>
      <div class="tags-list">
        <button class="tag-chip" :class="{ active: selectedTag === '' }" @click="handleSelectTag('')">
          全部 ({{ stats.totalCount }})
        </button>
        <button
          v-for="(count, tag) in stats.tagCloud"
          :key="tag"
          class="tag-chip"
          :class="{ active: selectedTag === tag }"
          @click="handleSelectTag(String(tag))"
        >
          {{ tag }} <span class="tag-count">({{ count }})</span>
        </button>
      </div>
    </section>

    <!-- ③ 列表：时间分组 + 便签卡 -->
    <div v-if="loading && memos.length === 0" class="state-box">正在读取本地便签库…</div>

    <div v-else-if="memos.length === 0" class="state-box">
      <template v-if="hasFilter">
        没有符合当前过滤条件的便签。
        <button type="button" class="state-action" @click="clearFilters">清除过滤</button>
      </template>
      <template v-else>
        还没有一条便签——在上面的速记条里敲一行、按回车就有了。
      </template>
    </div>

    <section v-for="g in groupedMemos" :key="g.key" class="memo-group" :aria-label="g.label">
      <h2 class="group-head">
        {{ g.label }}
        <span class="group-count mono">{{ g.items.length }}</span>
      </h2>
      <div class="memo-grid">
        <article
          v-for="item in g.items"
          :key="item.id"
          class="card memo-card"
          :class="[`border-tag-${item.colorTag || 'blue'}`, { 'is-pinned': item.isPinned }]"
        >
          <header class="memo-card-head">
            <h3
              class="memo-title"
              :class="{ 'title-void': !item.title.trim() }"
              :title="item.title || '无标题便签'"
              @click="openDetail(item)"
            >
              {{ item.title || '无标题便签' }}
            </h3>
            <span class="memo-actions">
              <span v-if="!item.isMasked && looksLikeMarkdown(item.content)" class="chip chip-information md-flag" title="含 Markdown 结构，点开渲染">MD</span>
              <button class="memo-icon-btn" :title="item.isMasked ? '揭示敏感信息' : '脱敏遮罩保护'" @click="handleToggleMask(item)">
                {{ item.isMasked ? '👁️' : '🕶️' }}
              </button>
              <button class="memo-icon-btn" :title="item.isPinned ? '取消置顶' : '固定置顶'" @click="handleTogglePin(item)">
                📌
              </button>
              <button class="memo-icon-btn" title="编辑" @click="openEditModal(item)">✏️</button>
              <button class="memo-icon-btn text-danger" title="删除" @click="handleDeleteMemo(item.id)">🗑️</button>
            </span>
          </header>

          <div
            class="memo-content-box"
            :class="{ masked: item.isMasked, mono: true }"
            :aria-label="item.isMasked ? '敏感信息已遮罩' : '便签内容摘要'"
            @click="openDetail(item)"
          >
            <template v-if="item.isMasked">••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••</template>
            <template v-else>{{ cardPreview(item.content) || '（空便签）' }}</template>
          </div>

          <footer class="memo-card-foot">
            <span class="memo-tags">
              <span
                v-for="t in item.tags"
                :key="t"
                class="tag-pill memo-tag"
                :class="{ 'memo-tag-active': selectedTag === t }"
                @click="handleSelectTag(t)"
              >
                {{ t }}
              </span>
            </span>
            <span class="foot-right">
              <time class="memo-time" :title="new Date(item.updatedAt).toLocaleString()">{{ fmtAgo(item.updatedAt) }}</time>
              <button v-if="!item.isMasked" class="memo-copy-btn" title="复制全文" @click="copyMemoContent(item.content)">📋</button>
            </span>
          </footer>
        </article>
      </div>
    </section>

    <!-- ④ 阅读态详情浮层（Markdown 渲染） -->
    <div v-if="detailItem" class="modal-overlay" @click.self="closeDetail">
      <div class="modal-dialog detail-dialog" role="dialog" aria-label="便签详情">
        <header class="dlg-head">
          <h3 class="dlg-title" :class="{ 'title-void': !detailItem.title.trim() }">
            {{ detailItem.title || '无标题便签' }}
          </h3>
          <button class="memo-x-btn" title="关闭" @click="closeDetail">✕</button>
        </header>
        <div class="dlg-meta">
          <span class="chip" :class="`chip-tag-${detailItem.colorTag || 'blue'}`" aria-hidden="true">■</span>
          <span v-if="detailItem.isPinned" class="chip chip-neutral">📌 已置顶</span>
          <span v-if="detailItem.isMasked" class="chip chip-danger">敏感遮罩中</span>
          <span class="mono meta-time">建 {{ fmtDate(detailItem.createdAt) }} · 改 {{ fmtDate(detailItem.updatedAt) }}</span>
        </div>
        <div class="dlg-body">
          <div v-if="detailItem.isMasked" class="memo-content-box masked mono">
            ••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••
          </div>
          <!-- v-html 安全前提：renderMarkdown 先整体转义再结构替换，链接 http/https 白名单，
               spec 锁 XSS 用例；masked 条目永不进渲染分支 -->
          <div v-else-if="detailHtml" class="md-preview" v-html="detailHtml" />
          <pre v-else class="memo-plain">{{ detailItem.content || '（空便签）' }}</pre>
        </div>
        <footer v-if="detailItem.tags && detailItem.tags.length" class="dlg-tags">
          <span
            v-for="t in detailItem.tags"
            :key="t"
            class="tag-pill memo-tag"
            @click="handleSelectTag(t); closeDetail()"
          >{{ t }}</span>
        </footer>
        <footer class="dlg-foot">
          <!-- 遮罩条目不给复制全文：遮罩语义是"明文不落眼也不落他人剪贴板" -->
          <UiButton v-if="!detailItem.isMasked" variant="secondary" small @click="copyMemoContent(detailItem.content)">📋 复制全文</UiButton>
          <UiButton variant="secondary" small @click="maskFromDetail">
            {{ detailItem.isMasked ? '👁️ 揭示明文' : '🕶️ 脱敏遮罩' }}
          </UiButton>
          <UiButton variant="primary" small @click="editFromDetail">✏️ 编辑</UiButton>
        </footer>
      </div>
    </div>

    <!-- ⑤ 编辑态浮层（书写 / Markdown 预览切换） -->
    <div v-if="showEditor" class="modal-overlay" @click.self="showEditor = false">
      <div class="modal-dialog editor-dialog" role="dialog" :aria-label="isEditing ? '编辑便签' : '新建便签'">
        <header class="dlg-head">
          <h3 class="dlg-title">{{ isEditing ? '编辑便签' : '新建便签' }}</h3>
          <div class="seg" role="group" aria-label="编辑/预览切换">
            <button class="seg-btn" :class="{ active: editMode === 'write' }" @click="editMode = 'write'">书写</button>
            <button class="seg-btn" :class="{ active: editMode === 'preview' }" @click="editMode = 'preview'">预览</button>
          </div>
          <button class="memo-x-btn" title="关闭" @click="showEditor = false">✕</button>
        </header>

        <div v-show="editMode === 'write'" class="modal-body">
          <label class="field-label">便签标题（可选，留空则以正文首行示人）</label>
          <input
            v-model="editForm.title"
            type="text"
            class="text-input"
            placeholder="例如：生产数据库连接串 / 正则表达式模板"
            aria-label="便签标题"
          />

          <label class="field-label">正文（支持 Markdown 子集：标题/粗斜体/代码块/列表/链接/引用）</label>
          <UiClipboardField
            v-model="editForm.content"
            mono
            :rows="10"
            placeholder="在此粘贴文本、命令行、cURL、SQL 或 JWT…"
          />

          <label class="field-label">色彩标识</label>
          <div class="color-row">
            <div
              v-for="c in COLOR_OPTIONS"
              :key="c.val"
              class="color-circle"
              :style="{ background: c.hex }"
              :class="{ selected: editForm.colorTag === c.val }"
              :title="c.name"
              @click="editForm.colorTag = c.val"
            ></div>
          </div>

          <label class="field-label">分类标签（输入后回车添加，自动补 #）</label>
          <div class="tags-input-box">
            <span v-for="t in editForm.tags" :key="t" class="tag-pill memo-tag memo-tag-removable">
              {{ t }} <span class="memo-tag-x" title="移除标签" @click="removeTag(t)">✕</span>
            </span>
            <input
              v-model="editForm.tagInput"
              type="text"
              class="text-input tag-entry"
              placeholder="例如 SQL, Token, 常用"
              aria-label="添加标签"
              @keydown.enter.prevent="addTagFromInput"
            />
          </div>
        </div>

        <div v-show="editMode === 'preview'" class="modal-body">
          <div v-if="editForm.content.trim()" class="md-preview" v-html="editorPreviewHtml" />
          <p v-else class="state-box preview-void">正文还是空的——先回「书写」写点东西。</p>
        </div>

        <footer class="dlg-foot">
          <UiButton variant="secondary" @click="showEditor = false">取消</UiButton>
          <UiButton variant="primary" @click="handleSaveMemo">
            {{ isEditing ? '保存修改' : '立即创建' }}
          </UiButton>
        </footer>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 治理说明（机主配色裁决 2026-09-26）：本视图 scoped 层【禁止】重写共享原子类
   （components.css :where 家族：btn 全族、text-input、tag-pill、chip、state-box、
   banner、mono、text-muted/text-danger 等）与全局 token 变量——跨页面观感一律
   以共享件为准。页面私有需求全部走 memo- 前缀私有类，与共享类同挂叠加组合
   （如 tag-pill + memo-tag：外观基座归全局，交互态/色差归私有）。 */
.memo-page { display: flex; flex-direction: column; gap: 14px; }

.head-btns { display: flex; gap: 8px; flex-wrap: wrap; }
.retry-btn { margin-left: 8px; }

/* ① 速记条：页面第一主角，输入框升半档高度 */
.memo-hero { display: flex; flex-direction: column; gap: 8px; }
.hero-row { display: flex; gap: 8px; align-items: stretch; flex-wrap: wrap; }
.hero-input { flex: 1 1 260px; min-width: 0; min-height: var(--control-h-lg); font-size: var(--text-md); }
.hero-tags { flex: 0 1 168px; min-width: 120px; min-height: var(--control-h-lg); }
.hero-hint { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.6; }

/* ② 参数条 */
.memo-toolbar { display: flex; flex-direction: column; gap: 10px; }
.tool-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.search-box { position: relative; flex: 1 1 260px; min-width: 0; }
.search-input { width: 100%; padding-right: 30px; min-height: var(--control-h-md); }
.memo-clear-btn {
  position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
  background: none; border: none; color: var(--color-text-subtle); cursor: pointer; font-size: var(--text-sm);
}
.clear-filter-btn { flex: none; }
.tool-count { margin-left: auto; font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; }
.tags-list { display: flex; flex-wrap: wrap; gap: 8px; }
.tag-chip {
  background: var(--surface-soft); border: 1px solid var(--color-border); color: var(--color-text-muted);
  padding: 3px 10px; border-radius: var(--radius-pill); font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.tag-chip:hover { background: var(--surface-hover); color: var(--color-text); }
.tag-chip.active { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-on-primary); }
.tag-count { opacity: 0.8; font-size: var(--text-xs); }

/* ③ 分组列表 */
.memo-group { display: flex; flex-direction: column; gap: 10px; }
.group-head {
  display: flex; align-items: center; gap: 8px; margin: 0;
  font-size: var(--text-sm); font-weight: 600; color: var(--color-text-subtle);
  text-transform: none; letter-spacing: 0.4px;
}
.group-count { font-size: var(--text-xs); color: var(--color-text-subtle); background: var(--surface-hover); border-radius: var(--radius-pill); padding: 0 7px; }
.memo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 12px; }

.memo-card {
  display: flex; flex-direction: column; gap: 8px; padding: 12px 14px;
  border-radius: var(--radius-control); transition: box-shadow var(--motion-fast) ease, transform var(--motion-fast) ease;
}
.memo-card:hover { box-shadow: var(--shadow-panel); transform: translateY(-1px); }
.memo-card.is-pinned { border-color: var(--color-primary); }

/* 色彩标记是用户数据值（colorTag 持久化进后端），非主题表面——palette 保留原始色板 */
.border-tag-blue { border-left: 4px solid #3b82f6; }
.border-tag-emerald { border-left: 4px solid #10b981; }
.border-tag-amber { border-left: 4px solid #f59e0b; }
.border-tag-rose { border-left: 4px solid #f43f5e; }
.border-tag-purple { border-left: 4px solid #8b5cf6; }

.memo-card-head { display: flex; align-items: center; gap: 6px; justify-content: space-between; }
.memo-title {
  margin: 0; font-size: var(--text-md); font-weight: 600; color: var(--color-text);
  min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer;
}
.memo-title:hover { color: var(--color-primary); }
.title-void { color: var(--color-text-subtle); font-weight: 400; font-style: italic; }
.memo-actions { display: flex; align-items: center; gap: 2px; flex: none; }
.memo-icon-btn {
  background: none; border: none; cursor: pointer; font-size: var(--text-base);
  padding: 2px 4px; border-radius: var(--radius-micro); transition: background var(--motion-fast) ease;
}
.memo-icon-btn:hover { background: var(--surface-hover); }
.md-flag { font-size: var(--text-micro); padding: 1px 6px; }

.memo-content-box {
  font-size: var(--text-sm); line-height: 1.55; color: var(--color-text);
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-micro);
  padding: 8px 10px; max-height: 150px; overflow: hidden; white-space: pre-wrap; word-break: break-word;
  cursor: pointer;
}
.memo-content-box.masked { color: var(--color-text-subtle); user-select: none; letter-spacing: 2px; }

.memo-card-foot { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
.memo-tags { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; }
/* 标签丸外观基座归全局 .tag-pill 原子，本层只叠加"可点选过滤丸"的交互差 */
.memo-tag { cursor: pointer; }
.memo-tag:hover { background: var(--surface-hover); }
.memo-tag-active { background: var(--color-primary-soft); color: var(--color-primary); }
.foot-right { display: flex; align-items: center; gap: 6px; flex: none; }
.memo-time { font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }
.memo-copy-btn {
  background: var(--surface-panel); border: 1px solid var(--color-border); color: var(--color-text-muted);
  padding: 1px 7px; border-radius: var(--radius-micro); font-size: var(--text-xs); cursor: pointer;
}
.memo-copy-btn:hover { background: var(--surface-hover); border-color: var(--color-border-strong); }

/* ④⑤ 浮层家族（详情/编辑共用壳；书写-预览分段钮互切内容区） */
.modal-overlay {
  position: fixed; inset: 0; background: var(--overlay-mask); backdrop-filter: blur(2px);
  display: flex; align-items: center; justify-content: center; z-index: 1000; padding: 20px;
}
.modal-dialog {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
  box-shadow: var(--shadow-panel); width: 100%; max-width: 640px; max-height: min(86vh, 860px);
  display: flex; flex-direction: column; gap: 12px; padding: 18px 20px;
}
.detail-dialog { max-width: 720px; }
.editor-dialog { max-width: 760px; }
.dlg-head { display: flex; align-items: center; gap: 12px; }
.dlg-title { margin: 0; font-size: var(--text-lg); font-weight: 700; color: var(--color-text); flex: 1; min-width: 0; overflow-wrap: anywhere; }
.memo-x-btn { background: none; border: none; color: var(--color-text-subtle); cursor: pointer; font-size: var(--text-md); padding: 2px 6px; border-radius: var(--radius-micro); }
.memo-x-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.dlg-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.meta-time { font-size: var(--text-xs); color: var(--color-text-subtle); }
.chip-tag-blue { background: #3b82f6; color: #fff; }
.chip-tag-emerald { background: #10b981; color: #fff; }
.chip-tag-amber { background: #f59e0b; color: #fff; }
.chip-tag-rose { background: #f43f5e; color: #fff; }
.chip-tag-purple { background: #8b5cf6; color: #fff; }
.dlg-body { overflow-y: auto; min-height: 0; }
.dlg-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.dlg-foot { display: flex; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }

.seg { display: flex; border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow: hidden; flex: none; }
.seg-btn {
  background: var(--surface-soft); border: none; color: var(--color-text-muted);
  padding: 4px 14px; font-size: var(--text-sm); cursor: pointer; transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.seg-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.seg-btn.active { background: var(--color-primary); color: var(--color-on-primary); }

.modal-body { display: flex; flex-direction: column; gap: 6px; overflow-y: auto; min-height: 0; }
.field-label { font-size: var(--text-sm); color: var(--color-text-muted); margin-top: 8px; }
.field-label:first-child { margin-top: 0; }
.color-row { display: flex; gap: 12px; padding: 2px 0 4px; }
.color-circle { width: 24px; height: 24px; border-radius: 50%; cursor: pointer; transition: transform var(--motion-fast) ease; }
.color-circle:hover { transform: scale(1.15); }
.color-circle.selected { outline: 2px solid var(--color-text); outline-offset: 2px; }
.tags-input-box {
  display: flex; flex-wrap: wrap; gap: 6px; align-items: center;
  border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 8px; background: var(--surface-soft);
}
.tag-entry { flex: 1 1 140px; min-width: 0; border: none; background: transparent; padding: 4px 2px; outline: none; color: var(--color-text); font-size: var(--text-base); }
.memo-tag-removable { display: inline-flex; align-items: center; gap: 4px; background: var(--surface-hover); }
.memo-tag-x { cursor: pointer; font-size: var(--text-micro); color: var(--color-text-subtle); }
.memo-tag-x:hover { color: var(--state-danger); }
.preview-void { background: transparent; border-style: dashed; }

/* Markdown 阅读/预览皮肤：全走 token，随主题联动 */
.md-preview { font-size: var(--text-base); line-height: 1.65; color: var(--color-text); overflow-wrap: anywhere; }
.md-preview :deep(h1), .md-preview :deep(h2), .md-preview :deep(h3),
.md-preview :deep(h4), .md-preview :deep(h5), .md-preview :deep(h6) {
  margin: 12px 0 6px; line-height: 1.4; color: var(--color-text);
}
.md-preview :deep(h1) { font-size: var(--text-lg); }
.md-preview :deep(h2) { font-size: var(--text-md); }
.md-preview :deep(h3), .md-preview :deep(h4), .md-preview :deep(h5), .md-preview :deep(h6) { font-size: var(--text-base); }
.md-preview :deep(h1:first-child), .md-preview :deep(h2:first-child), .md-preview :deep(h3:first-child) { margin-top: 0; }
.md-preview :deep(p) { margin: 6px 0; }
.md-preview :deep(ul), .md-preview :deep(ol) { margin: 6px 0; padding-left: 22px; }
.md-preview :deep(li) { margin: 2px 0; }
.md-preview :deep(code) {
  font-family: var(--font-mono); font-size: var(--text-sm); background: var(--surface-hover);
  border: 1px solid var(--color-border); border-radius: var(--radius-micro); padding: 1px 5px;
}
.md-preview :deep(pre) {
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px; overflow-x: auto; margin: 8px 0;
}
.md-preview :deep(pre code) { background: none; border: none; padding: 0; line-height: 1.55; }
.md-preview :deep(blockquote) {
  margin: 8px 0; padding: 2px 12px; border-left: 3px solid var(--color-border-strong);
  color: var(--color-text-muted); background: var(--surface-soft); border-radius: 0 var(--radius-control) var(--radius-control) 0;
}
.md-preview :deep(hr) { border: none; border-top: 1px solid var(--color-border); margin: 12px 0; }
.md-preview :deep(a) { color: var(--color-primary); }
.md-preview :deep(table) { border-collapse: collapse; }
.md-preview :deep(del) { color: var(--color-text-subtle); }

.memo-plain {
  margin: 0; font-family: var(--font-mono); font-size: var(--text-sm); line-height: 1.6;
  white-space: pre-wrap; word-break: break-word; color: var(--color-text);
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px;
}

@media (max-width: 720px) {
  .memo-grid { grid-template-columns: minmax(0, 1fr); }
  .tool-count { display: none; }
  .hero-tags { flex: 1 1 100%; }
}
</style>
