<script setup lang="ts">
import { ref, shallowRef, onMounted } from 'vue'
import * as MemoAPI from '../../bindings/hanxi/internal/modules/memo'
import type {
  MemoItem,
  MemoFilter,
  MemoStats
} from '../../bindings/hanxi/internal/modules/memo/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useClipboard } from '../composables/useClipboard'

const { showToast } = useToast()
const { copy } = useClipboard()

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

// 编辑器弹窗状态
const showEditor = ref(false)
const isEditing = ref(false)
const editForm = ref({
  id: '',
  title: '',
  content: '',
  tagInput: '',
  tags: [] as string[],
  colorTag: 'blue',
})

const COLOR_OPTIONS = [
  { name: '蓝色', val: 'blue', hex: '#3b82f6' },
  { name: '翡翠绿', val: 'emerald', hex: '#10b981' },
  { name: '琥珀橙', val: 'amber', hex: '#f59e0b' },
  { name: '玫瑰红', val: 'rose', hex: '#f43f5e' },
  { name: '紫罗兰', val: 'purple', hex: '#8b5cf6' },
]

async function loadMemos() {
  loading.value = true
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

    memos.value = items ?? []
    stats.value = curStats
  } catch (err: unknown) {
    errorMsg.value = `加载备忘录失败: ${getErrorMessage(err)}`
  } finally {
    loading.value = false
  }
}

function handleSelectTag(tag: string) {
  if (selectedTag.value === tag) {
    selectedTag.value = ''
  } else {
    selectedTag.value = tag
  }
  loadMemos()
}

function openCreateModal() {
  isEditing.value = false
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
  editForm.value.tags = editForm.value.tags.filter(t => t !== tag)
}

async function handleSaveMemo() {
  if (!editForm.value.title.trim() && !editForm.value.content.trim()) {
    showToast('标题或内容至少填写一项')
    return
  }
  addTagFromInput()

  try {
    if (isEditing.value) {
      await MemoAPI.MemoService.Update(
        editForm.value.id,
        editForm.value.title,
        editForm.value.content,
        editForm.value.tags,
        editForm.value.colorTag
      )
      showToast('备忘录已更新')
    } else {
      await MemoAPI.MemoService.Create(
        editForm.value.title,
        editForm.value.content,
        editForm.value.tags,
        editForm.value.colorTag
      )
      showToast('已新建备忘录')
    }
    showEditor.value = false
    await loadMemos()
  } catch (err: unknown) {
    showToast(`保存失败: ${getErrorMessage(err)}`)
  }
}

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
  try {
    await MemoAPI.MemoService.Delete(id)
    showToast('已删除便签')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

// 剪贴板两级策略已收编进 useClipboard（toast 文案与原实现逐字一致）
async function copyMemoContent(content: string) {
  const ok = await copy(content)
  showToast(ok ? '已复制便签内容到剪贴板' : '复制失败')
}

// memo:changed 由局域网投递/多端联动触发：setup 期订阅防丢早期事件，卸载自动注销
useWailsEvent('memo:changed', () => {
  loadMemos()
})

onMounted(() => {
  loadMemos()
})
</script>

<template>
  <div class="page memo-page">
    <!-- 页面顶栏 -->
    <header class="page-header">
      <div class="header-left">
        <h1 class="page-title">
          <span class="title-icon">📝</span>
          极客随手备忘录
        </h1>
        <p class="page-desc">
          本地自包含持久化存储（便携模式免丢失）。专为临时 SQL、Token、代码片段与待办碎片设计，支持标签云、敏感脱敏遮罩与手机端直连投递。
        </p>
      </div>
      <div class="header-actions">
        <button class="btn-primary" @click="openCreateModal">
          <span>+ 新建备忘便签</span>
        </button>
      </div>
    </header>

    <!-- 顶部检索与标签云过滤栏 -->
    <div class="card memo-toolbar-card mb-6">
      <div class="search-box mb-4">
        <input
          v-model="searchKw"
          type="text"
          class="input-control search-input"
          placeholder="🔍 搜索标题、文本内容、代码片段或标签..."
          @input="loadMemos"
        />
        <button v-if="searchKw" class="btn-clear-search" @click="searchKw = ''; loadMemos()">✕</button>
      </div>

      <!-- 标签云列表 -->
      <div class="tag-cloud-wrapper flex-between">
        <div class="tags-list">
          <button
            class="tag-chip"
            :class="{ active: selectedTag === '' }"
            @click="handleSelectTag('')"
          >
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
        <div class="tag-meta text-muted text-sm font-mono">
          置顶: {{ stats.pinnedCount }} / 总数: {{ stats.totalCount }}
        </div>
      </div>
    </div>

    <!-- 便签网格列表 -->
    <div v-if="loading && memos.length === 0" class="empty-state py-8">
      <p>正在读取本地便签数据库...</p>
    </div>

    <div v-else-if="memos.length === 0" class="empty-state py-12">
      <div class="empty-icon">📝</div>
      <h3>暂无符合条件的备忘录便签</h3>
      <p>点击右上角的「+ 新建备忘便签」，或通过局域网文件快传在手机端投递文本自动收录至此。</p>
    </div>

    <div v-else class="memo-grid">
      <div
        v-for="item in memos"
        :key="item.id"
        class="card memo-card"
        :class="[`border-tag-${item.colorTag || 'blue'}`, { 'is-pinned': item.isPinned }]"
      >
        <!-- 卡片顶栏 -->
        <div class="memo-card-header flex-between mb-2">
          <div class="memo-title font-bold truncate">
            <span v-if="item.isPinned" class="pin-badge" title="已置顶">📌</span>
            {{ item.title || '无标题便签' }}
          </div>
          <div class="memo-actions flex gap-1">
            <button
              class="btn-icon"
              :title="item.isMasked ? '揭示敏感信息' : '脱敏遮罩保护'"
              @click="handleToggleMask(item)"
            >
              {{ item.isMasked ? '👁️' : '🕶️' }}
            </button>
            <button
              class="btn-icon"
              :title="item.isPinned ? '取消置顶' : '固定置顶'"
              @click="handleTogglePin(item)"
            >
              📌
            </button>
            <button class="btn-icon" title="编辑" @click="openEditModal(item)">
              ✏️
            </button>
            <button class="btn-icon text-danger" title="删除" @click="handleDeleteMemo(item.id)">
              🗑️
            </button>
          </div>
        </div>

        <!-- 核心内容展示区 -->
        <div class="memo-content-box font-mono" :class="{ masked: item.isMasked }">
          <template v-if="item.isMasked">
            ••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••
          </template>
          <template v-else>
            {{ item.content }}
          </template>
        </div>

        <!-- 卡片底栏：标签与操作 -->
        <div class="memo-card-footer flex-between mt-3">
          <div class="memo-tags flex flex-wrap gap-1">
            <span
              v-for="t in item.tags"
              :key="t"
              class="tag-pill"
              @click="handleSelectTag(t)"
            >
              {{ t }}
            </span>
          </div>
          <div class="memo-footer-right flex-center gap-2">
            <span class="memo-time text-muted font-mono text-xs">
              {{ new Date(item.updatedAt).toLocaleDateString() }}
            </span>
            <button class="btn-copy-chip" @click="copyMemoContent(item.content)">
              📋 复制
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 新建/编辑便签 Modal 弹窗 -->
    <div v-if="showEditor" class="modal-overlay" @click.self="showEditor = false">
      <div class="modal-dialog">
        <div class="modal-header flex-between mb-4">
          <h3 class="modal-title font-bold">
            {{ isEditing ? '✏️ 编辑便签' : '📝 新建便签' }}
          </h3>
          <button class="btn-text" @click="showEditor = false">✕</button>
        </div>

        <div class="modal-body flex-col gap-4">
          <div class="form-group">
            <label>便签标题 (可选):</label>
            <input
              v-model="editForm.title"
              type="text"
              class="input-control"
              placeholder="例如：生产数据库连接串 / 正则表达式模板"
            />
          </div>

          <div class="form-group">
            <label>正文内容 (支持多行、代码、SQL 或密钥):</label>
            <textarea
              v-model="editForm.content"
              class="input-control textarea-control font-mono"
              rows="8"
              placeholder="在此粘贴文本、命令行、cURL、JWT 或备忘内容..."
            ></textarea>
          </div>

          <!-- 色彩标记 -->
          <div class="form-group">
            <label>色彩标识:</label>
            <div class="color-picker-row flex gap-3">
              <div
                v-for="c in COLOR_OPTIONS"
                :key="c.val"
                class="color-circle"
                :style="{ background: c.hex }"
                :class="{ selected: editForm.colorTag === c.val }"
                @click="editForm.colorTag = c.val"
              ></div>
            </div>
          </div>

          <!-- 标签管理 -->
          <div class="form-group">
            <label>分类标签 (输入后按回车添加):</label>
            <div class="tags-input-box">
              <div class="active-tags flex flex-wrap gap-1 mb-2">
                <span v-for="t in editForm.tags" :key="t" class="tag-pill tag-removable">
                  {{ t }} <span class="tag-remove" @click="removeTag(t)">✕</span>
                </span>
              </div>
              <input
                v-model="editForm.tagInput"
                type="text"
                class="input-control"
                placeholder="输入如 SQL, Token, 常用 并回车..."
                @keydown.enter.prevent="addTagFromInput"
              />
            </div>
          </div>
        </div>

        <div class="modal-footer flex-between mt-6">
          <button class="btn-secondary" @click="showEditor = false">取消</button>
          <button class="btn-primary" @click="handleSaveMemo">
            {{ isEditing ? '保存修改' : '立即创建' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 治理说明：本视图历史遗留的 text-muted、font-mono、mb-*、gap-* 等 utility 类名在本文件与全局
   均无定义（渲染即无操作），清理会引入视觉变化，留观——本次只动确有定义的规则。 */
.memo-page {
  padding: 24px 32px;
  max-width: 1360px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
}

.page-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--color-text);
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.page-desc {
  font-size: 13px;
  color: var(--color-text-muted);
  max-width: 720px;
  line-height: 1.5;
}

.memo-toolbar-card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  padding: 16px 20px;
}

.search-box {
  position: relative;
}

.search-input {
  width: 100%;
  padding-right: 32px;
}

.btn-clear-search {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  background: none;
  border: none;
  color: var(--color-text-subtle);
  cursor: pointer;
}

.tag-cloud-wrapper {
  align-items: center;
}

.tags-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag-chip {
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  color: var(--color-text-muted);
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  font-size: 12px;
  cursor: pointer;
  transition: background var(--motion-base), color var(--motion-base);
}

.tag-chip:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.tag-chip.active {
  background: var(--color-primary);
  color: var(--color-on-primary);
  border-color: var(--color-primary);
}

.tag-count {
  opacity: 0.8;
  font-size: 11px;
}

.memo-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 16px;
}

.memo-card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  padding: 16px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  transition: transform var(--motion-base), box-shadow var(--motion-base);
}

.memo-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow-small);
}

/* 色彩标记是用户数据值（colorTag 持久化进后端），非主题表面——palette 保留原始色板 */
.border-tag-blue { border-left: 4px solid #3b82f6; }
.border-tag-emerald { border-left: 4px solid #10b981; }
.border-tag-amber { border-left: 4px solid #f59e0b; }
.border-tag-rose { border-left: 4px solid #f43f5e; }
.border-tag-purple { border-left: 4px solid #8b5cf6; }

.memo-title {
  font-size: 14px;
  color: var(--color-text);
  max-width: 220px;
}

.pin-badge {
  font-size: 12px;
}

.btn-icon {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 13px;
  padding: 2px 4px;
  border-radius: 4px;
  transition: background var(--motion-base);
}

.btn-icon:hover {
  background: var(--surface-hover);
}

.memo-content-box {
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  padding: 10px 12px;
  max-height: 160px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
}

.memo-content-box.masked {
  color: var(--color-text-subtle);
  user-select: none;
  letter-spacing: 2px;
}

.tag-pill {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--surface-soft);
  color: var(--color-text-muted);
  cursor: pointer;
}

.tag-pill:hover {
  background: var(--surface-hover);
}

.tag-removable {
  display: flex;
  align-items: center;
  gap: 4px;
}

.tag-remove {
  cursor: pointer;
  font-size: 10px;
  color: var(--color-text-subtle);
}

.tag-remove:hover {
  color: var(--state-danger);
}

.btn-copy-chip {
  background: var(--surface-panel);
  border: 1px solid var(--color-border-strong);
  color: var(--color-text-muted);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
}

.btn-copy-chip:hover {
  background: var(--surface-soft);
  border-color: var(--color-text-subtle);
}

.modal-overlay {
  position: fixed;
  inset: 0;
  background: var(--overlay-mask);
  backdrop-filter: blur(2px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-dialog {
  background: var(--surface-panel);
  border-radius: var(--radius-element);
  width: 90%;
  max-width: 580px;
  padding: 24px;
  box-shadow: var(--shadow-panel);
}

.color-circle {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  cursor: pointer;
  transition: transform var(--motion-base);
}

.color-circle:hover {
  transform: scale(1.15);
}

.color-circle.selected {
  outline: 2px solid var(--color-text);
  outline-offset: 2px;
}

.flex-between { display: flex; justify-content: space-between; align-items: center; }
.flex-center { display: flex; align-items: center; }
.flex-col { display: flex; flex-direction: column; }
.gap-1 { gap: 4px; }
.gap-2 { gap: 8px; }
.gap-3 { gap: 12px; }
.gap-4 { gap: 16px; }

.btn-primary {
  background: var(--color-primary);
  color: var(--color-on-primary);
  border: none;
  border-radius: var(--radius-control);
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}

.btn-secondary {
  background: var(--surface-panel);
  border: 1px solid var(--color-border-strong);
  color: var(--color-text-muted);
  border-radius: var(--radius-control);
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
}

.input-control {
  background: var(--surface-soft);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
  padding: 8px 12px;
  font-size: 13px;
  outline: none;
  color: var(--color-text);
}

.textarea-control {
  resize: vertical;
}

/* 本视图空态为纯文本形态，与全局 dashed 卡片 .empty-state 原子不同形——不强凑，保留局部定义 */
.empty-state {
  text-align: center;
  color: var(--color-text-subtle);
}

.empty-icon {
  font-size: 36px;
  margin-bottom: 8px;
}
</style>
