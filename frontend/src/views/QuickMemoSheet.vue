<script setup lang="ts">
// 悬浮速记卡（N16 B 批）：后端 MemoService 按全局热键唤出的独立 frameless
// 透明置顶小窗内容（main.ts 按 #memosheet hash 分流挂载并打 .popup-shell，
// 卡体由本页自绘，透明窗纪律见踩坑 #50——窗口本体不允许有任何实底露出）。
//
// 交互契约（刻意做窄）：
//   - 一行正文 + 可选标签：回车即存（走既有 Create 链路，空正文回车 = 收起）；
//   - Esc / 失焦 / Alt+F4 收起（Go 侧统一收口，收起后空闲 TTL 真销毁）；
//   - 每次唤出（memo:quicksheet:opening）清残稿并聚焦——速记卡不留上次草稿，
//     想到即存、没想好就 Esc，残稿语义是干扰不是便利；
//   - 连续速记：保存成功不关窗，清行保焦点，接着敲下一条；
//   - IsMasked 纪律不在此面：速记恒为非敏感创建，页面不提供遮罩开关。
// 失败反馈走卡内轻提示（同 SnipCardView 族规：独立窗没有工作台 toast 宿主）。
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import * as MemoAPI from '../../bindings/hanxi/internal/modules/memo'
import { getErrorMessage } from '../utils/errors'
import { looksLikeMarkdown } from '../utils/markdown'
import { useWailsEvent } from '../composables/useWailsEvent'

const text = ref('')
const tagsInput = ref('')
const saving = ref(false)
const tip = ref('')
const tipKind = ref<'ok' | 'err'>('ok')
const inputEl = ref<HTMLInputElement | null>(null)

function showTip(msg: string, kind: 'ok' | 'err') {
  tip.value = msg
  tipKind.value = kind
}

// 标签解析与主窗速记条同口径：空白/逗号顿号分词，自动补 #（后端 cleanTags
// 还有一道归一，这里是所见即所得的第一道）。
function parseTags(): string[] {
  const raw = tagsInput.value.split(/[\s,，、;；]+/).filter(Boolean)
  const set = new Set<string>()
  for (const t of raw) set.add(t.startsWith('#') ? t : `#${t}`)
  return [...set]
}

async function dismiss() {
  try {
    await MemoAPI.MemoService.HideQuickSheet()
  } catch {
    /* 卡已无处可收（窗口竞态）时静默，Go 侧失焦/热键出口照旧 */
  }
}

async function commit() {
  const v = text.value.trim()
  if (!v) {
    await dismiss() // 空行回车 = 收起：没什么可记时就关卡
    return
  }
  if (saving.value) return
  saving.value = true
  try {
    await MemoAPI.MemoService.Create('', v, parseTags(), 'blue')
    text.value = ''
    tagsInput.value = ''
    showTip('已记下，可继续敲下一条', 'ok')
    await nextTick()
    inputEl.value?.focus()
  } catch (err: unknown) {
    showTip(`保存失败: ${getErrorMessage(err)}`, 'err') // 保稿：修一修还能回车重试
  } finally {
    saving.value = false
  }
}

function onEnter(e: KeyboardEvent) {
  // 中文输入法组字中的回车只结束选词，不该触发保存（主窗速记条同款豁免）
  if (e.isComposing) return
  void commit()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') void dismiss()
}

useWailsEvent<void>('memo:quicksheet:opening', () => {
  text.value = ''
  tagsInput.value = ''
  tip.value = ''
  void nextTick(() => inputEl.value?.focus())
})

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void nextTick(() => inputEl.value?.focus())
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="sheet" role="dialog" aria-label="悬浮速记卡">
    <div class="sheet-card">
      <input
        ref="inputEl"
        v-model="text"
        class="text-input sheet-input"
        type="text"
        placeholder="敲一行就记一条，回车即存，Esc 收起…"
        aria-label="速记内容"
        @keydown.enter="onEnter"
      />
      <div class="sheet-row">
        <input
          v-model="tagsInput"
          class="text-input sheet-tags"
          type="text"
          placeholder="#标签 空格分隔（可选）"
          aria-label="速记标签"
          @keydown.enter="onEnter"
        />
        <span v-if="text.trim() && looksLikeMarkdown(text)" class="chip sheet-md" title="含 Markdown 结构，入库后阅读态渲染">MD</span>
        <span class="sheet-count mono">{{ saving ? '记录中…' : 'Enter 存 · Esc 收' }}</span>
      </div>
      <p v-if="tip" class="sheet-tip" :class="tipKind === 'err' ? 'text-danger' : 'text-muted'" role="status">{{ tip }}</p>
    </div>
  </div>
</template>

<style scoped>
/* 透明窗边距容纳卡体投影（窗口几何 620×196 DIP，Go 侧 sheetWidth/Height 同源）；
   卡体外观全部本页自绘，配色一律走全局 token，随主题联动。 */
.sheet {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 16px;
  user-select: none;
}
.sheet-card {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  box-shadow: var(--shadow-panel);
  padding: 16px 18px 12px;
  animation: sheet-in 120ms ease-out;
}
@keyframes sheet-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: none; }
}
.sheet-input {
  width: 100%;
  min-height: var(--control-h-lg);
  font-size: var(--text-md);
}
.sheet-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sheet-tags {
  flex: 1 1 180px;
  min-width: 0;
}
.sheet-md {
  flex: none;
  font-size: var(--text-micro);
}
.sheet-count {
  flex: none;
  margin-left: auto;
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  white-space: nowrap;
}
.sheet-tip {
  margin: 0;
  font-size: var(--text-xs);
  line-height: 1.5;
  min-height: 1em;
}
</style>
