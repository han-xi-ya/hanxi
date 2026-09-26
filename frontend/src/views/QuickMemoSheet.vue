<script setup lang="ts">
// 悬浮速记卡（N16 B 批，牌面重做批）：后端 MemoService 按全局热键唤出的独立
// frameless 透明置顶小窗内容（main.ts 按 #memosheet hash 分流挂载并打 .popup-shell，
// 卡体由本页自绘，透明窗纪律见踩坑 #50——窗口本体不允许有任何实底露出）。
//
// 交互契约（刻意做窄，重做只动呈现不动保存主路径）：
//   - 一行正文 + 可选标签：回车即存（走既有 Create 链路，空正文回车 = 收起）；
//   - Ctrl+Enter = 存并收起：成功创建后才关卡，失败保稿保窗（绝不吞数据）；
//   - Esc / 失焦 / Alt+F4 收起（Go 侧统一收口，收起后空闲 TTL 真销毁）；
//   - 每次唤出（memo:quicksheet:opening）清残稿并聚焦——速记卡不留上次草稿，
//     想到即存、没想好就 Esc，残稿语义是干扰不是便利；
//   - 连续速记：保存成功不关窗，清行保焦点，接着敲下一条；
//   - 粘贴含换行的多行文本：就地并为一条（行间以空格相连，已有草稿前置保留）
//     并轻提示行数；单行粘贴零干预走原生路径。速记卡是一行面，不做拆条；
//     接线收口轮（2026-09-26）本条与标签分词、Ctrl+Enter 保存语义同迁主窗速记行
//     （memoMetrics 单源共享），M3 遗留的两面漂移就此封死；
//   - 动效三件皆为 transform/opacity 微动效（≤10px、≤180ms，N37 纪律零 glow）：
//     opening 重播入场（is-enter 类钩子，窗口隐藏期挂载不白烧动画）、保存成功
//     原句化作 .sheet-ghost 飞走残影、提示行常驻占位防跳动；
//     prefers-reduced-motion 经组件级 + base.css 全局双兜底归零。
//   - IsMasked 纪律不在此面：速记恒为非敏感创建，页面不提供遮罩开关。
// 失败反馈走卡内轻提示（同 SnipCardView 族规：独立窗没有工作台 toast 宿主），
// 恒为 text-xs 小字、槽位常开，错误再长也不闪大字不顶动版面。
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import * as MemoAPI from '../../bindings/hanxi/internal/modules/memo'
import { getErrorMessage } from '../utils/errors'
import { looksLikeMarkdown } from '../utils/markdown'
import { useWailsEvent } from '../composables/useWailsEvent'
import { flattenPastedLines, parseTagTokens } from '../components/memo/memoMetrics'

const text = ref('')
const tagsInput = ref('')
const saving = ref(false)
const tip = ref('')
const tipKind = ref<'ok' | 'err'>('ok')
const ghost = ref('')
const entering = ref(false)
const inputEl = ref<HTMLInputElement | null>(null)

// 残影/入场都是"看一眼就走"的瞬时钩子，定时器只负责收尾拆除；
// 组件卸载统一 clear，不留悬空回调。
const FLY_MS = 220 // 与 CSS ghost-fly/sheet-in（180ms）留 ~40ms 余量
let flyTimer: number | undefined
let enterTimer: number | undefined

function showTip(msg: string, kind: 'ok' | 'err') {
  tip.value = msg
  tipKind.value = kind
}

function flyAway(snapshot: string) {
  window.clearTimeout(flyTimer)
  ghost.value = snapshot
  flyTimer = window.setTimeout(() => {
    ghost.value = ''
  }, FLY_MS)
}

// opening 重播入场动画：先摘钩子再挂回（nextTick 隔一帧让 animation 重新起算），
// 挂载首帧窗口还藏着，动画由首次 opening 负责，不做 mount 期白烧。
function replayEnter() {
  window.clearTimeout(enterTimer)
  entering.value = false
  void nextTick(() => {
    entering.value = true
    enterTimer = window.setTimeout(() => {
      entering.value = false
    }, FLY_MS)
  })
}

// 标签解析走 memoMetrics.parseTagTokens 单源（接线收口轮删去与主窗速记行的
// 逐字重复第二份）：空白/逗号顿号分词，自动补 #（后端 cleanTags 还有一道归一，
// 这里是所见即所得的第一道）。

async function dismiss() {
  try {
    await MemoAPI.MemoService.HideQuickSheet()
  } catch {
    /* 卡已无处可收（窗口竞态）时静默，Go 侧失焦/热键出口照旧 */
  }
}

async function commit(closeAfter = false) {
  const v = text.value.trim()
  if (!v) {
    await dismiss() // 空行回车 = 收起：没什么可记时就关卡（Ctrl+Enter 同义）
    return
  }
  if (saving.value) return
  saving.value = true
  try {
    await MemoAPI.MemoService.Create('', v, parseTagTokens(tagsInput.value), 'blue')
    text.value = ''
    tagsInput.value = ''
    if (closeAfter) {
      await dismiss() // 窗都要收了，残影与轻提示就不必演一场
      return
    }
    flyAway(v)
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
  void commit(e.ctrlKey || e.metaKey) // Ctrl/Cmd+Enter = 存并收起
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') void dismiss()
}

// 多行粘贴并条的判定与拼接走 memoMetrics.flattenPastedLines 单源（接线收口轮
// 主窗速记行对齐此语义，两面从此不分叉）；卡内反馈仍走轻提示槽位。
function onPaste(e: ClipboardEvent) {
  const merged = flattenPastedLines(e.clipboardData?.getData('text/plain') ?? '')
  if (!merged) return // 单行：零干预，走原生插入
  e.preventDefault()
  text.value = text.value.trim() ? `${text.value.trim()} ${merged.flat}` : merged.flat
  showTip(`已粘贴 ${merged.lineCount} 行并并为一条速记，回车即存`, 'ok')
}

useWailsEvent<void>('memo:quicksheet:opening', () => {
  text.value = ''
  tagsInput.value = ''
  tip.value = ''
  window.clearTimeout(flyTimer)
  ghost.value = '' // 残影也是残稿
  replayEnter()
  void nextTick(() => inputEl.value?.focus())
})

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void nextTick(() => inputEl.value?.focus())
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.clearTimeout(flyTimer)
  window.clearTimeout(enterTimer)
})
</script>

<template>
  <div class="sheet" role="dialog" aria-label="悬浮速记卡">
    <div class="sheet-card" :class="{ 'is-enter': entering }">
      <div class="sheet-line">
        <input
          ref="inputEl"
          v-model="text"
          class="text-input sheet-input"
          type="text"
          placeholder="敲一行就记一条，回车即存…"
          aria-label="速记内容"
          @keydown.enter="onEnter"
          @paste="onPaste"
        />
        <!-- 保存成功的飞走残影：纯装饰（aria-hidden），正文已被接走入库 -->
        <span v-if="ghost" class="sheet-ghost" aria-hidden="true">{{ ghost }}</span>
      </div>
      <div class="sheet-row">
        <label class="sheet-tagwrap">
          <span class="sheet-tag-glyph" aria-hidden="true">#</span>
          <input
            v-model="tagsInput"
            class="text-input sheet-tags"
            type="text"
            placeholder="标签，空格分隔（可选）"
            aria-label="速记标签"
            @keydown.enter="onEnter"
          />
        </label>
        <span v-if="text.trim() && looksLikeMarkdown(text)" class="chip chip-neutral sheet-md" title="含 Markdown 结构，入库后阅读态渲染">MD</span>
        <span class="sheet-count mono">
          {{ saving ? '记录中…' : '⏎ 存 · Ctrl+⏎ 存并收 · Esc 收' }}
        </span>
      </div>
      <!-- 提示行常驻占位：出现/消失不顶动版面；错误恒小字两行内消化，不闪大字 -->
      <p
        class="sheet-tip"
        :class="tip ? (tipKind === 'err' ? 'tip-err' : 'tip-ok') : ''"
        role="status"
      >{{ tip }}</p>
    </div>
  </div>
</template>

<style scoped>
/* 透明窗边距容纳卡体投影（窗口几何 620×196 DIP，Go 侧 sheetWidth/Height 同源，
   本轮重排全部在既有高度预算内完成）；卡体外观本页自绘，配色一律走全局 token。
   层次策略：正文行是主角（下划线式轻量边框、primary 光标、半档升字号），标签行
   降为胶囊副角（soft 底、透明描边、focus-within 才亮边），提示行是脚注。 */
.sheet {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 16px;
  user-select: none;
}
.sheet-card {
  /* 高度预算锁死在 Go 侧 196 DIP（减去 16×2 透明边距与描边 = 164 内容盒）：
     36 正文行 + 24 标签胶囊 + ~17 提示槽 + 14×2 气口 + 16/12 上下衬 = 133，
     余量给投影视觉膨胀，全程无滚动无溢出 */
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 14px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  box-shadow: var(--shadow-panel);
  padding: 16px 18px 12px;
  transition: border-color var(--motion-base) ease;
}
/* 唤出/卡内任一处获得焦点：描边向 primary 混 30%（design-system 强调描边配方，
   实底+描边表达状态，按 N37 零 glow） */
.sheet-card:focus-within {
  border-color: color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
}
/* 入场动画由 opening 事件的 is-enter 钩子驱动（见脚本注释：挂载首帧窗还藏着） */
.sheet-card.is-enter {
  animation: sheet-in 160ms ease-out;
}
@keyframes sheet-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: none; }
}

.sheet-line {
  position: relative;
}
.sheet-input {
  width: 100%;
  min-height: var(--control-h-lg);
  padding: 7px 2px;
  font-size: var(--text-md);
  line-height: 1.5;
  color: var(--color-text);
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--color-border);
  border-radius: 0;
  caret-color: var(--color-primary);
  transition: border-color var(--motion-fast) ease;
}
.sheet-input::placeholder {
  color: var(--color-text-subtle);
}
/* 焦点态自绘（下划线转主色 + 卡体描边呼应），压掉全局 2px 环中环——
   单行输入的下划线变色即其可见焦点指示，双份反而噪 */
.sheet-input:focus,
.sheet-input:focus-visible {
  outline: none;
  border-bottom-color: color-mix(in srgb, var(--color-primary) 60%, var(--color-border));
}
/* 飞走残影：叠在原句位上向上一抹即散，transform+opacity 双属性，绝不吃版面 */
.sheet-ghost {
  position: absolute;
  left: 2px;
  right: 0;
  top: 7px;
  font-size: var(--text-md);
  line-height: 1.5;
  color: var(--color-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  pointer-events: none;
  animation: ghost-fly var(--motion-slow) ease-out both;
}
@keyframes ghost-fly {
  from { opacity: 0.8; transform: translateY(0); }
  to { opacity: 0; transform: translateY(-10px); }
}

.sheet-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
/* 标签胶囊：常态 soft 底 + 透明描边（比正文低一档存在度），焦点才亮边 */
.sheet-tagwrap {
  flex: 1 1 180px;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: var(--control-h-sm);
  padding: 2px 10px;
  background: var(--surface-soft);
  border: 1px solid transparent;
  border-radius: var(--radius-pill);
  transition: border-color var(--motion-fast) ease, background var(--motion-fast) ease;
}
.sheet-tagwrap:focus-within {
  background: var(--surface-panel);
  border-color: var(--color-border-strong);
}
.sheet-tag-glyph {
  flex: none;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  color: var(--color-text-subtle);
}
.sheet-tags {
  width: 100%;
  min-width: 0;
  padding: 0;
  font-size: var(--text-sm);
  background: transparent;
  border: none;
  color: var(--color-text);
}
.sheet-tags::placeholder {
  color: var(--color-text-subtle);
}
.sheet-tags:focus,
.sheet-tags:focus-visible {
  outline: none;
}
/* 输入面允许选中（.sheet 全局 user-select:none 的例外，正文随时可圈可复制） */
.sheet-input,
.sheet-tags {
  user-select: text;
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
  min-height: calc(var(--text-xs) * 1.5);
  font-size: var(--text-xs);
  line-height: 1.5;
  color: var(--color-text-subtle);
  word-break: break-word;
}
.sheet-tip.tip-ok {
  color: var(--state-positive);
}
.sheet-tip.tip-err {
  color: var(--state-danger);
}

/* 减弱动效组件级兜底（族规同 SnipCardView）：入场与飞走残影归零，
   base.css 全局块仍是最后防线 */
@media (prefers-reduced-motion: reduce) {
  .sheet-card,
  .sheet-card *,
  .sheet-card::before,
  .sheet-card::after {
    transition: none;
    animation: none;
  }
  /* 残影静止停 220ms 也是闪现，直接不演：成功反馈仍由空行 + 绿色提示完整承载 */
  .sheet-ghost {
    display: none;
  }
}
</style>
