<script setup lang="ts">
// 框选截屏识别的悬浮结果卡：后端 OcrService 创建的独立 frameless Acrylic
// 顶层窗口内容（main.ts 按 #ocrcard hash 分流挂载，不加载工作台外壳；
// 页面底色必须透明透出 DWM backdrop，见 .popup-shell 规则）。
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
    <div v-if="res?.ok && res.text" class="snip-text" tabindex="0">{{ res.text }}</div>
    <div v-else-if="res?.ok" class="snip-state">未识别到文字 —— 框选含文本的区域再试</div>
    <div v-else class="snip-state"><span class="snip-pulse" aria-hidden="true"></span>等待识别结果…</div>

    <footer class="snip-foot">
      <span v-if="tip" class="snip-tip" role="alert">{{ tip }}</span>
      <span v-else-if="res?.ok" class="snip-meta">识别结果 · {{ res.lineCount }} 行 · {{ res.elapsedMs }} ms</span>
      <span v-else class="snip-meta">识别结果</span>
      <span v-if="res?.copied" class="chip chip-positive snip-copied">已复制</span>
      <button class="snip-x" aria-label="关闭（Esc）" title="关闭（Esc）" @click="dismiss">✕</button>
      <button class="btn btn-primary btn-small" :disabled="!canCopy" @click="copyAll">
        {{ busy ? '复制中…' : '复制全文' }}
      </button>
    </footer>
  </div>
</template>

<style scoped>
/* 方案 D · 原生 Acrylic：Go 侧以 BackgroundTypeTranslucent + BackdropType
   Acrylic 建窗，DWM 负责桌面模糊、圆角裁切与投影——**窗口即卡片**，页面不再
   自绘边框/圆角/阴影壳（透明 margin 在 acrylic 下会露成灰角）。页面只叠一层
   半透明主题调色，保证 Acrylic 系统着色与应用主题（及 Win10 无 tint 回退）
   下的文字对比度；1px 内描边确立边缘。无标题栏：元信息下沉到底部一行，
   "识别结果"由场景自明。 */
.snip-card {
  height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column;
  background: color-mix(in srgb, var(--surface-panel) 62%, transparent);
  box-shadow: inset 0 0 0 1px var(--color-border);
  color: var(--color-text); padding: 14px 16px 12px;
  font-family: var(--font-text);
}
.snip-text {
  flex: 1; min-height: 0; min-width: 0; overflow: auto;
  white-space: pre-wrap; word-break: break-word;
  font-size: var(--text-md); line-height: 1.65; user-select: text; /* 手动选字复制是核心场景 */
}
.snip-state {
  flex: 1; display: flex; align-items: center; justify-content: center; gap: 7px;
  font-size: var(--text-sm); color: var(--color-text-muted);
}
.snip-pulse {
  width: 6px; height: 6px; border-radius: 50%; background: var(--color-primary);
  box-shadow: 0 0 0 3px var(--color-primary-glow);
  animation: snip-pulse 1.2s ease-in-out infinite;
}
@keyframes snip-pulse { 0%, 100% { opacity: .45; } 50% { opacity: 1; } }
.snip-foot { display: flex; align-items: center; gap: 8px; margin-top: 12px; }
.snip-meta {
  flex: 1; min-width: 0; font-size: var(--text-xs); color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.snip-tip { flex: 1; min-width: 0; font-size: var(--text-xs); color: var(--state-danger); }
.snip-copied { flex: none; }
.snip-x {
  display: grid; place-items: center; width: 26px; height: 26px; flex: none;
  border: none; background: none; color: var(--color-text-muted); cursor: pointer;
  font-size: var(--text-base); line-height: 1; border-radius: var(--radius-control);
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.snip-x:hover { color: var(--color-text); background: var(--surface-hover); }
@media (prefers-reduced-motion: reduce) { .snip-card * { transition: none; animation: none; } }
</style>
