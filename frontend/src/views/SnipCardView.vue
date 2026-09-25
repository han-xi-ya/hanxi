<script setup lang="ts">
// 框选截屏识别的悬浮结果卡：后端 OcrService 创建的独立 frameless Acrylic
// 顶层窗口内容（main.ts 按 #ocrcard hash 分流挂载，不加载工作台外壳；
// 页面底色必须透明透出 DWM backdrop，见 .popup-shell 规则）。
// 结果下发双保险：ocr:snip-result 事件即时推送 + 挂载时 GetSnipResult 拉取。
// 刻意不挂失焦关闭——用户要能在卡内手动选字复制；Esc/关闭钮/复制成功即收起。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as OcrAPI from '../../bindings/hanxi/internal/modules/ocr/ocrservice'
import type { SnipResult } from '../../bindings/hanxi/internal/modules/ocr/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'

const res = ref<SnipResult | null>(null)
const busy = ref(false)
const tip = ref('') // 卡内轻提示（复制失败等，保留卡片供手动复制）

// —— 内容字号缩放（N42③）——读小字费劲的主诉求。与窗口缩放（②）正交独立：
// 只管 `.snip-text` 一处 CSS 变量；档位记忆走 localStorage（卡窗同源共享，
// 与 hanxi.theme 缓存同策略），非法/越界值装载即钳制。
const SNIP_FS_KEY = 'hanxi.snipcard.fs'
const SNIP_FS_MIN = 12
const SNIP_FS_MAX = 32
const SNIP_FS_DEFAULT = 14 // 与 --text-md 同值：默认档**不注入**变量，CSS 回退复活（审查 #22）

function loadFontSize(): number {
  try {
    const raw = localStorage.getItem(SNIP_FS_KEY)
    if (raw !== null && raw !== '') {
      const n = Number(raw)
      if (Number.isFinite(n)) return Math.min(SNIP_FS_MAX, Math.max(SNIP_FS_MIN, Math.round(n)))
    }
  } catch {
    /* 存储不可用：默认档继续 */
  }
  return SNIP_FS_DEFAULT
}

const fontSize = ref(loadFontSize())
watch(fontSize, (v) => {
  try {
    localStorage.setItem(SNIP_FS_KEY, String(v))
  } catch {
    /* 静默：缓存只是偏好，不是真相 */
  }
})

/** ± 步进（2px 一档）；Ctrl+滚轮同口径（上滚放大）。 */
function bumpFontSize(delta: number) {
  fontSize.value = Math.min(SNIP_FS_MAX, Math.max(SNIP_FS_MIN, fontSize.value + delta))
}

function onCardWheel(e: WheelEvent) {
  if (!e.ctrlKey) return // 无修饰的滚轮留给正文滚动
  e.preventDefault() // 拦下 WebView 原生页面缩放，字号步进单点归本卡
  bumpFontSize(e.deltaY < 0 ? 2 : -2)
}

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

// 跟手手势会话公共件（拖拽/缩放同族）：Wails beta.10 无拖拽/缩放区 API，
// Go 侧原生轮询跟手。mousedown 起手、window mouseup 显式结束；指针移出窗口
// 丢失 mouseup 时，Go 侧左键态检测兜底（双通道收口）。尽力而为，异常静默。
function beginGesture(start: () => Promise<unknown>, end: () => Promise<unknown>) {
  void start().catch(() => { /* 门拒绝/后端不可用：手势不可得，卡片仍可正常用 */ })
  const stop = () => {
    window.removeEventListener('mouseup', stop)
    void end().catch(() => {})
  }
  window.addEventListener('mouseup', stop)
}

// 拖拽把手（元信息条）
function startDrag(e: MouseEvent) {
  if (e.button !== 0) return
  e.preventDefault() // 抑制把手上的文本选中
  beginGesture(OcrAPI.CardDragStart, OcrAPI.CardDragEnd)
}

// 缩放手柄（右下角）：改窗口宽高并记忆（N42②，尺寸持久化在后端 store）
function startResize(e: MouseEvent) {
  if (e.button !== 0) return
  e.preventDefault() // 抑制 WebView 文本选中/原生缩放手势
  beginGesture(OcrAPI.CardResizeStart, OcrAPI.CardResizeEnd)
}

async function copyAll() {
  busy.value = true
  try {
    // 有意例外（PLAN_CLIPBOARD §3.2C）：走 Go 代理写剪贴板绕开 webview 安全上下文
    // 限制并收起卡片，不经 useClipboard——悬浮卡窗体常驻隐藏，登记勿改。
    await OcrAPI.SnipCopyText()
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
  <div class="snip-card" role="dialog" aria-label="截屏识别结果" :style="fontSize !== SNIP_FS_DEFAULT ? { '--snip-fs': fontSize + 'px' } : {}" @wheel="onCardWheel">
    <div v-if="res?.ok && res.text" class="snip-text" tabindex="0">{{ res.text }}</div>
    <div v-else-if="res?.ok" class="snip-state">未识别到文字 —— 框选含文本的区域再试</div>
    <div v-else class="snip-state"><span class="snip-pulse" aria-hidden="true"></span>等待识别结果…</div>

    <footer class="snip-foot">
      <span v-if="tip" class="snip-tip" role="alert">{{ tip }}</span>
      <span v-else-if="res?.ok" class="snip-meta snip-grip" title="按住拖动" @mousedown="startDrag">识别结果 · {{ res.lineCount }} 行 · {{ res.elapsedMs }} ms</span>
      <span v-else class="snip-meta snip-grip" title="按住拖动" @mousedown="startDrag">识别结果</span>
      <span class="snip-fs" role="group" aria-label="正文字号">
        <button class="snip-fs-btn" :disabled="fontSize <= SNIP_FS_MIN" aria-label="缩小字号" title="缩小字号（或 Ctrl+滚轮）" @click="bumpFontSize(-2)">A−</button>
        <button class="snip-fs-btn" :disabled="fontSize >= SNIP_FS_MAX" aria-label="放大字号" title="放大字号（或 Ctrl+滚轮）" @click="bumpFontSize(2)">A+</button>
      </span>
      <span v-if="res?.copied" class="chip chip-positive snip-copied">已复制</span>
      <button class="snip-x" aria-label="关闭（Esc）" title="关闭（Esc）" @click="dismiss">✕</button>
      <button class="btn btn-primary btn-small" :disabled="!canCopy" @click="copyAll">
        {{ busy ? '复制中…' : '复制全文' }}
      </button>
    </footer>

    <!-- 右下角缩放手柄（N42②）：与拖拽把手同族 Go 侧跟手，尺寸即时记忆 -->
    <span class="snip-resize" title="拖动缩放（大小会记住）" aria-hidden="true" @mousedown="startResize"></span>
  </div>
</template>

<style scoped>
/* 方案 D · 原生 Acrylic：Go 侧以 BackgroundTypeTranslucent + BackdropType
   Acrylic 建窗，DWM 负责桌面模糊与圆角裁切——**窗口即卡片**，页面不再自绘
   边框/圆角/阴影壳（透明 margin 在 acrylic 下会露成灰角）。页面叠 78% 半透明
   主题调色：透明度每降一分，系统着色对文字对比的污染就多一分（62% 实测小字
   不可读），78% 兼顾毛玻璃质感与 token 设计对比度；1px 内描边确立边缘。
   无标题栏：元信息下沉底行兼作拖拽把手（.snip-grip），"识别结果"由场景自明。 */
.snip-card {
  position: relative; /* 缩放手柄锚定窗口右下角 */
  height: 100%; box-sizing: border-box;
  display: flex; flex-direction: column;
  background: color-mix(in srgb, var(--surface-panel) 78%, transparent);
  box-shadow: inset 0 0 0 1px var(--color-border);
  color: var(--color-text); padding: 14px 16px 12px;
  font-family: var(--font-text);
}
.snip-text {
  flex: 1; min-height: 0; min-width: 0; overflow: auto;
  white-space: pre-wrap; word-break: break-word;
  font-size: var(--snip-fs, var(--text-md)); /* N42③:字号档位经根 CSS 变量下发 */
  line-height: 1.65; user-select: text; /* 手动选字复制是核心场景 */
}
/* 字号步进器（N42③）：贴元信息尾部的紧凑 A∓ 对，随档位到边界自动禁用 */
.snip-fs { display: inline-flex; gap: 2px; flex: none; }
.snip-fs-btn {
  display: grid; place-items: center; height: 22px; min-width: 24px; padding: 0 4px;
  border: 1px solid var(--color-border); border-radius: var(--radius-control);
  background: none; color: var(--color-text-muted); cursor: pointer;
  font-size: var(--text-xs); font-weight: 600; line-height: 1;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.snip-fs-btn:hover:not(:disabled) { color: var(--color-text); background: var(--surface-hover); }
.snip-fs-btn:disabled { opacity: 0.4; cursor: default; }
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
.snip-grip { cursor: grab; user-select: none; }
.snip-grip:active { cursor: grabbing; }
.snip-tip { flex: 1; min-width: 0; font-size: var(--text-xs); color: var(--state-danger); }
.snip-copied { flex: none; }
.snip-x {
  display: grid; place-items: center; width: 26px; height: 26px; flex: none;
  border: none; background: none; color: var(--color-text-muted); cursor: pointer;
  font-size: var(--text-base); line-height: 1; border-radius: var(--radius-control);
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.snip-x:hover { color: var(--color-text); background: var(--surface-hover); }
/* 缩放手柄（N42③ 的窗口搭档②）：右下角 16px 热区，三道斜纹示意可拖；
   贴在 body 底 padding 带上，不侵入正文滚动区与按钮行 */
.snip-resize {
  position: absolute; right: 1px; bottom: 1px; width: 16px; height: 16px;
  cursor: nwse-resize; user-select: none; touch-action: none;
  background:
    linear-gradient(45deg, transparent 46%, var(--color-text-subtle) 46%, var(--color-text-subtle) 54%, transparent 54%),
    linear-gradient(45deg, transparent 64%, var(--color-text-subtle) 64%, var(--color-text-subtle) 72%, transparent 72%),
    linear-gradient(45deg, transparent 82%, var(--color-text-subtle) 82%, var(--color-text-subtle) 90%, transparent 90%);
  opacity: 0.55; border-bottom-right-radius: var(--radius-control, 6px);
}
.snip-resize:hover { opacity: 1; }
@media (prefers-reduced-motion: reduce) { .snip-card * { transition: none; animation: none; } }
</style>
