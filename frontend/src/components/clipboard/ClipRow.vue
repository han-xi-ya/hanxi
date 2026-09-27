<script setup lang="ts">
// ClipRow：一条剪贴板历史的贴线索行（行式索引主体，形态承接 MemoCard list 档的
// "笔记本速记标"语言：平时无框、hover 染色、置顶描边）。
//
// 行结构：kind 图标 ｜ 主区（preview 两行截断 + 元数据行） ｜ 行尾快操作。
// 元数据行吃齐契约呈现面：autoTags 小 chip、pinned/manual 徽标、sensitive ⚠
// 徽标（提示"不进 AI 通道"）、来源窗口标题、相对时间（悬浮全量时间）、使用计数。
//
// 呈现纪律：
//   - 列表数据来自 List（不带 Text/BlobData），本件只吃 preview 等列表形态字段，
//     绝不诱导宿主把明文正文塞进行；
//   - 相对时间/摘要兜底/图标名全部走 clipboardFormat 纯函数单源（computed 一次
//     成型，模板零函数调用——500 条封顶的 v-for 不做重复计算）；
//   - 快操作钮（复制=回填系统剪贴板 / 置顶 / 删除）只发意图事件，Set/TogglePin
//     后端调用与删除确认闸全部归视图（危险逻辑不进展示件，MemoCard 同谱）。
import { computed } from 'vue'
import type { ClipEntry } from '../../types/clipboard'
import ClipGlyph from './ClipGlyph.vue'
import { fmtClipAgo, fmtClipDateTime, previewFallback, rowGlyph } from './clipboardFormat'

const props = defineProps<{
  entry: ClipEntry
  /** 右页正在看的是这个 id（宿主贴挂差分） */
  active?: boolean
}>()

const emit = defineEmits<{
  /** 点开右页详情（宿主负责 Get 拉全文并回填行内计数） */
  (e: 'select'): void
  /** 回填系统剪贴板（宿主走 Set(id)） */
  (e: 'copy'): void
  (e: 'togglePin'): void
  (e: 'delete'): void
}>()

const glyph = computed(() => rowGlyph(props.entry))
const preview = computed(() => previewFallback(props.entry))
const ago = computed(() => fmtClipAgo(props.entry.createdAt))
const timeTip = computed(() => `复制于 ${fmtClipDateTime(props.entry.createdAt)}`)
const tags = computed(() => props.entry.autoTags ?? [])
const useCount = computed(() => props.entry.useCount ?? 0)
</script>

<template>
  <article
    class="cr"
    :class="{ 'is-active': active, 'is-pinned': !!entry.pinned }"
  >
    <span class="cr-icon" aria-hidden="true"><ClipGlyph :name="glyph" :size="15" /></span>

    <div class="cr-main">
      <div
        class="cr-preview"
        role="button"
        tabindex="0"
        aria-label="打开详情"
        @click="emit('select')"
        @keydown.enter.prevent="emit('select')"
        @keydown.space.prevent="emit('select')"
      >{{ preview }}</div>
      <div class="cr-meta">
        <span v-for="t in tags" :key="t" class="tag-pill cr-tag">{{ t }}</span>
        <span v-if="entry.pinned" class="chip cr-badge cr-pin">
          <ClipGlyph name="pin" :size="11" /> 置顶
        </span>
        <span v-if="entry.manual" class="chip chip-information cr-badge">片段</span>
        <span
          v-if="entry.sensitive"
          class="chip chip-danger cr-badge"
          title="疑似含密钥/令牌：此条目不进 AI（MCP 检索）通道，明文只在本机 DPAPI 留底"
        >
          <ClipGlyph name="alert" :size="11" /> 敏感
        </span>
        <span v-if="useCount > 0" class="cr-count" :title="`本条已被使用 ${useCount} 次`">用 {{ useCount }} 次</span>
        <span v-if="entry.sourceApp" class="cr-src" :title="`复制时前台窗口：${entry.sourceApp}`">{{ entry.sourceApp }}</span>
        <time class="cr-time" :title="timeTip">{{ ago }}</time>
      </div>
    </div>

    <span class="cr-tools">
      <button
        type="button"
        class="cr-btn"
        title="回填到系统剪贴板（Ctrl+V 即贴）"
        aria-label="复制到剪贴板"
        @click="emit('copy')"
      ><ClipGlyph name="copy" :size="14" /></button>
      <button
        type="button"
        class="cr-btn"
        :class="{ 'cr-on': entry.pinned }"
        :title="entry.pinned ? '取消置顶' : '固定置顶（不被容量淘汰）'"
        :aria-label="entry.pinned ? '取消置顶' : '固定置顶'"
        @click="emit('togglePin')"
      ><ClipGlyph name="pin" :size="14" /></button>
      <button
        type="button"
        class="cr-btn cr-btn-del"
        title="删除此条（需确认）"
        aria-label="删除此条"
        @click="emit('delete')"
      ><ClipGlyph name="trash" :size="14" /></button>
    </span>
  </article>
</template>

<style scoped>
/* 贴线索行：平时透明底只留悬染与选中染色（memo 索引栏同谱） */
.cr {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 4px 8px;
  align-items: start;
  padding: 7px 10px;
  border: 1px solid transparent;
  border-left-width: 3px;
  border-left-color: transparent;
  border-radius: var(--radius-control);
  transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.cr:hover { background: var(--surface-soft); border-color: var(--color-border); border-left-color: var(--color-border); }
.cr.is-active { background: var(--surface-selected); border-color: var(--color-border); border-left-color: var(--color-border); }
/* 置顶档：左沿主色描边（文字徽标同挂，不做颜色单表意） */
.cr.is-pinned { border-left-color: var(--color-primary); }
.cr-icon { color: var(--color-text-muted); padding-top: 2px; }

.cr-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
/* preview 两行截断（后端已压到 ≤120 rune 首行摘要，这里兜超长粘贴） */
.cr-preview {
  font-size: var(--text-sm);
  line-height: 1.5;
  color: var(--color-text);
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  cursor: pointer;
  border-radius: var(--radius-micro);
}
.cr-preview:hover { color: var(--color-primary); }
.cr-preview:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }

.cr-meta { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; min-width: 0; }
.cr-tag { font-size: var(--text-micro); padding: 0 6px; flex: none; }
.cr-badge {
  font-size: var(--text-micro); padding: 0 6px; flex: none;
  display: inline-flex; align-items: center; gap: 3px;
}
.cr-pin { background: var(--color-primary-soft); color: var(--color-primary); }
.cr-count { font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; flex: none; }
.cr-src {
  font-size: var(--text-xs); color: var(--color-text-subtle);
  max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  min-width: 0;
}
.cr-time { margin-left: auto; flex: none; font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }

.cr-tools { display: flex; align-items: center; gap: 2px; flex: none; padding-top: 1px; }
.cr-btn {
  background: none; border: none; cursor: pointer;
  color: var(--color-text-muted); padding: 3px 5px; border-radius: var(--radius-micro);
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.cr-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.cr-btn:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
.cr-btn-del:hover { background: var(--btn-danger-outline-hover); color: var(--state-danger); }
.cr-on { color: var(--color-primary); }

/* 窄栏档：元数据行改横滚不折行（行高保持稳定，摘要是主角）。
   容器名 clip 由宿主视图声明（ClipboardView .clip-page），件内不自带命名。 */
@container clip (max-width: 420px) {
  .cr-meta { flex-wrap: nowrap; overflow-x: auto; scrollbar-width: none; }
  .cr-src { max-width: 96px; }
}
</style>
