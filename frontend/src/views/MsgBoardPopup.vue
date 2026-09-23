<script setup lang="ts">
// 全屏留言牌（挂牌弹窗视图）：Go 侧 MsgBoardService 创建的透明全屏窗内容，
// main.ts 按 #msgboard hash 分流挂载并打 .popup-shell（html/body 全透明，见
// 踩坑 #50——牌体观感全部由本页自绘，窗口本体不允许有任何实底）。
//
// N6 重设计：整屏黑板 → "贴在屏幕上的便利贴"（牌面画法收进 BoardCard，与
// 管理页预览同源）；背景只留轻微压暗（既保"牌在屏幕上"的聚焦感，也保住
// 全屏点击撤牌的命中区）。撤牌提示改为限时淡出——前几秒给操作者看，之后
// 还给旁观者一张干净的牌；防休眠说明移回管理页，不再印在牌上。
// 交互契约不变：点击任意处或 Esc 撤牌；正文/字号经 msgboard:changed 热更。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import * as MsgBoardAPI from '../../bindings/hanxi/internal/modules/msgboard'
import type { BoardContent } from '../../bindings/hanxi/internal/modules/msgboard/models'
import BoardCard from '../components/msgboard/BoardCard.vue'
import { useWailsEvent } from '../composables/useWailsEvent'

const content = ref<BoardContent | null>(null)

async function pull() {
  try {
    content.value = await MsgBoardAPI.MsgBoardService.GetBoardContent()
  } catch { /* 拉取失败保持当前牌面；窗口销毁重建时页面本就全新加载 */ }
}

async function dismiss() {
  try {
    await MsgBoardAPI.MsgBoardService.Dismiss()
  } catch { /* 牌已无处可撤时静默 */ }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') void dismiss()
}

useWailsEvent<void>('msgboard:changed', () => { void pull() })

const text = computed(() => content.value?.text ?? '')
const fontSize = computed(() => content.value?.fontSize ?? 64)

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void pull()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div v-if="content" class="board" role="dialog" aria-label="留言牌" @click="dismiss">
    <BoardCard class="board-card" :text="text" :font-size="fontSize" />
    <div class="board-hint" aria-hidden="true">点击任意处或按 <kbd class="board-kbd">Esc</kbd> 撤牌</div>
  </div>
</template>

<style scoped>
/* 压暗层是全屏唯一"底"（透明窗规范 #50：除此之外不得有任何实底/边框露出）。
   轻微压暗而非黑板实底：便利贴悬浮在真实桌面上的隐喻成立的前提。 */
.board {
  position: fixed;
  inset: 0;
  height: 100vh;
  display: grid;
  place-items: center;
  background: rgba(6, 10, 14, 0.38);
  cursor: pointer;
  user-select: none;
  animation: board-in 150ms ease-out;
}
.board-card {
  /* 全屏牌的最大高度：留 18% 视口呼吸，超高裁剪纪律在 BoardCard 内部 */
  --bc-max-h: 74vh;
}
@keyframes board-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
/* 限时提示：给操作者的说明书，不是给旁观者的贴纸——驻留 5s 后 1s 淡出 */
.board-hint {
  position: absolute;
  bottom: 26px;
  left: 0;
  right: 0;
  text-align: center;
  font-size: var(--text-md);
  color: rgba(238, 246, 247, 0.62);
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  animation: hint-life 6s ease-in forwards;
  pointer-events: none;
}
@keyframes hint-life {
  0%, 76% { opacity: 1; }
  100% { opacity: 0; }
}
.board-kbd {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  border: 1px solid rgba(238, 246, 247, 0.4);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 6px;
}
@media (prefers-reduced-motion: reduce) {
  .board { animation: none; }
  /* 提示不播淡出而是直接常驻末尾透明态：无动效环境保留可见性 */
  .board-hint { animation: none; opacity: 1; }
}
@media (max-width: 720px) {
  .board-hint { font-size: var(--text-sm); }
}
</style>
