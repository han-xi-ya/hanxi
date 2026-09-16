<script setup lang="ts">
// 框选截屏识别的悬浮结果卡：后端 OcrService 创建的独立 frameless 真透明顶层
// 窗口内容（main.ts 按 #ocrcard hash 分流挂载，不加载工作台外壳）。
// 结果下发双保险：ocr:snip-result 事件即时推送 + 挂载时 GetSnipResult 拉取。
// 刻意不挂失焦关闭——用户要能在卡内手动选字复制；Esc/关闭钮/复制成功即收起。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import * as OcrAPI from '../../bindings/hanxi/internal/modules/ocr/ocrservice'
import type { SnipResult } from '../../bindings/hanxi/internal/modules/ocr/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'

const res = ref<SnipResult | null>(null)
const busy = ref(false)
const tip = ref('') // 卡内轻提示（复制失败等，保留卡片供手动复制）

function apply(r: SnipResult | null | undefined) {
  if (!r || !r.ok) return // 只认成功帧；空帧/取消帧不得顶掉已有内容
  res.value = r
  tip.value = ''
}

useWailsEvent<SnipResult>('ocr:snip-result', (r) => apply(r))

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') void dismiss()
}

async function dismiss() {
  try {
    await OcrAPI.SnipCardDismiss()
  } catch { /* 窗口侧兜底：卡已无处可收时静默 */ }
}

async function copyAll() {
  busy.value = true
  try {
    await OcrAPI.SnipCopyText() // Go 代理写剪贴板并收起卡片
  } catch (e) {
    tip.value = getErrorMessage(e) // 失败保卡：用户仍可手动选字
  } finally {
    busy.value = false
  }
}

const canCopy = computed(() => !!res.value?.ok && !!res.value?.text && !busy.value)

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void (async () => {
    try {
      const [r, found] = await OcrAPI.GetSnipResult()
      if (found) apply(r)
    } catch { /* 事件通道会补送，拉取失败不打扰 */ }
  })()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="snip-card" role="dialog" aria-label="截屏识别结果">
    <header class="snip-head">
      <b>识别结果</b>
      <span v-if="res?.ok" class="snip-meta">
        {{ res.lineCount }} 行 · {{ res.elapsedMs }} ms<template v-if="res.copied"> · 已复制</template>
      </span>
      <button class="snip-x" aria-label="关闭" @click="dismiss">✕</button>
    </header>

    <div v-if="res?.ok && res.text" class="snip-text" tabindex="0">{{ res.text }}</div>
    <div v-else-if="res?.ok" class="snip-state">未识别到文字 —— 框选含文本的区域再试</div>
    <div v-else class="snip-state">等待识别结果…</div>

    <footer class="snip-foot">
      <span v-if="tip" class="snip-tip" role="alert">{{ tip }}</span>
      <button class="btn btn-primary btn-small" :disabled="!canCopy" @click="copyAll">
        {{ busy ? '复制中…' : '复制全文' }}
      </button>
      <button class="btn btn-secondary btn-small" @click="dismiss">关闭</button>
    </footer>
  </div>
</template>

<style scoped>
/* 透明壳窗口内自绘实底面板（轮盘同款原则：卡片视觉边缘全部由页面绘制） */
.snip-card {
  margin: 8px; height: calc(100% - 16px); box-sizing: border-box;
  display: flex; flex-direction: column; gap: 8px;
  background: var(--surface-panel); border: 1px solid var(--color-border);
  border-radius: var(--radius-card, 12px); box-shadow: var(--shadow-float, 0 10px 32px rgba(0, 0, 0, .28));
  padding: 10px 12px; color: var(--color-text);
}
.snip-head { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
.snip-head b { font-size: 13px; }
.snip-meta { font-size: 11px; color: var(--color-text-subtle); font-variant-numeric: tabular-nums; flex: 1; }
.snip-x {
  border: none; background: none; color: var(--color-text-muted); cursor: pointer;
  font-size: 13px; line-height: 1; padding: 2px 4px; border-radius: 4px;
}
.snip-x:hover { color: var(--color-text); background: var(--surface-hover); }
.snip-text {
  flex: 1; min-height: 0; overflow: auto; white-space: pre-wrap; word-break: break-word;
  font-size: 13px; line-height: 1.55; user-select: text; /* 手动选字复制是核心场景 */
  padding: 2px;
}
.snip-state { flex: 1; display: flex; align-items: center; justify-content: center; font-size: 12px; color: var(--color-text-muted); }
.snip-foot { display: flex; align-items: center; gap: 8px; }
.snip-tip { font-size: 11px; color: var(--color-danger, #d33); flex: 1; min-width: 0; }
@media (prefers-reduced-motion: reduce) { .snip-card * { transition: none; } }
</style>
