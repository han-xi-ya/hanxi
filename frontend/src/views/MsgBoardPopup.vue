<script setup lang="ts">
// 全屏留言牌（挂牌弹窗视图）：Go 侧 MsgBoardService 创建的透明全屏窗内容，
// main.ts 按 #msgboard hash 分流挂载并打 .popup-shell（html/body 全透明，见
// 踩坑 #50——牌体观感全部由本页自绘，窗口本体不允许有任何实底）。
// 交互：点击任意处或 Esc 撤牌；正文/字号经 msgboard:changed 事件热更新。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import * as MsgBoardAPI from '../../bindings/hanxi/internal/modules/msgboard'
import type { BoardContent } from '../../bindings/hanxi/internal/modules/msgboard/models'
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
const fontStyle = computed(() => ({ fontSize: `${content.value?.fontSize ?? 64}px` }))

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void pull()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div v-if="content" class="board" role="dialog" aria-label="留言牌" @click="dismiss">
    <div class="board-body">
      <div class="board-text" :style="fontStyle">{{ text }}</div>
      <div class="board-hint">点击任意处或按 <kbd class="board-kbd">Esc</kbd> 撤牌 · 挂出期间屏幕保持常亮</div>
    </div>
  </div>
</template>

<style scoped>
/* 牌体是"物理挂牌"而非工作台面板：深底白字为产品决定，不随亮/暗主题反转
   （离岗告示在任何主题下都该一眼可读且明显是"牌"）。rgba 字面值在此处是
   刻意豁免，非未迁移视图的裸色泄漏（区分见踩坑 #33 的教训——那类是漏改，
   此类是设计本体）。透明窗规范（#50）：本层半透深色即全屏唯一可见底，
   四周不再另叠边框/圆角，入场仅淡入、不缩放露底。 */
.board {
  position: fixed;
  inset: 0;
  height: 100vh;
  display: grid;
  place-items: center;
  background: rgba(6, 13, 16, 0.86);
  color: #eef6f7;
  cursor: pointer;
  user-select: none;
  font-family: var(--font-display);
  animation: board-in 150ms ease-out;
}
@keyframes board-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
.board-body {
  min-width: 0;
  max-width: min(86vw, 1200px);
  padding: 24px;
  text-align: center;
}
.board-text {
  font-weight: 600;
  line-height: 1.45;
  letter-spacing: 0.01em;
  white-space: pre-wrap; /* 自定义正文允许手工换行 */
  overflow-wrap: break-word;
  max-height: 70vh;
  overflow: hidden; /* 极端超长只裁不滚：牌是看的，不是读的 */
}
.board-hint {
  margin-top: 48px;
  font-size: var(--text-lg);
  color: rgba(238, 246, 247, 0.55);
}
.board-kbd {
  font-family: var(--font-mono);
  font-size: var(--text-md);
  border: 1px solid rgba(238, 246, 247, 0.4);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 6px;
}
@media (max-width: 720px) {
  .board-hint { margin-top: 24px; font-size: var(--text-md); }
}
</style>
