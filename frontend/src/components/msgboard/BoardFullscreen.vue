<script setup lang="ts">
// BoardFullscreen：牌面全屏预览浮层（N30，v4 从 MsgBoardView 内联收编）。
//
// 纪律照旧，逐条锁死：
//   Teleport 到 body——躲开页面滚动容器与层叠上下文；
//   纯前端渲染——与真牌同一 BoardCard、同值压暗层、同 --bc-max-h:74vh 标定，
//   零后端调用：刻意不走挂牌链路（Toggle 是翻转语义，"预览"不该能撤真牌，
//   瞬时挂撤还会惊动 KeepAwake 登记与 N29 窗组账）；
//   关闭交互：点击任意处或 Esc，都只 emit('close')——开合态归宿主（v-if），
//   组件不自杀不藏态，宿主断言链路因此与旧内联版逐字等价（类名 .mbp-full
//   家族原样沿用，选择器零迁移）。
//
// Esc 监听生命周期（审查 #20，与 UiHistoryDialog #5 同族）：mounted 挂、
// unmount/deactivate 摘、activate 补挂——KeepAlive 切页时浮层若仍开着，
// window 级监听在场会让异页按 Esc 幽灵收起隐藏预览。
import { onActivated, onBeforeUnmount, onDeactivated, onMounted } from 'vue'
import BoardCard from './BoardCard.vue'

defineProps<{
  text: string
  fontSize: number
}>()

const emit = defineEmits<{
  /** Esc 或点击浮层任意处——宿主置 false 收层（点击浮层即关闭为既定契约） */
  (e: 'close'): void
}>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
function hookKey() {
  window.addEventListener('keydown', onKey)
}
function unhookKey() {
  window.removeEventListener('keydown', onKey)
}

// v-if 挂载即"浮层开着"：mounted/unmount 负责挂摘；KeepAlive 切页时组件
// 未必卸载，deactivate/activate 再补一道——与收编前视图里的 watch 纪律等价。
onMounted(hookKey)
onBeforeUnmount(unhookKey)
onDeactivated(unhookKey)
onActivated(hookKey)
</script>

<template>
  <Teleport to="body">
    <div
      class="mbp-full"
      role="dialog"
      aria-label="牌面全屏预览"
      @click="emit('close')"
    >
      <span class="mbp-full-badge" aria-hidden="true">预览浮层 · 非真实挂牌</span>
      <BoardCard class="mbp-full-card" :text="text" :font-size="fontSize" />
      <div class="mbp-full-hint" aria-hidden="true">
        这是全屏预览，不改变挂牌状态 · 点击任意处或按 <kbd class="mbp-full-kbd">Esc</kbd> 返回
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* 与 MsgBoardPopup 真牌同源观感——同值压暗层（rgba(6,10,14,.38) 字面量与
   真牌层逐字一致，属贴喻本体色不随主题反转，见 BoardCard 豁免纪律）、同
   74vh 牌高标定、同 150ms 入场淡入。差别只有两处：预览角标常驻（操作者
   要随时知道自己在预览），底部提示不做限时淡出（真牌淡出是给旁观者，
   这里没旁观者）。层级：高于通知抽屉(10002)，低于命令面板(100000)与
   Toast(999999)——预览不该劫持全局快捷键 UI。 */
.mbp-full {
  position: fixed;
  inset: 0;
  z-index: 99998;
  display: grid;
  place-items: center;
  background: rgba(6, 10, 14, 0.38);
  cursor: pointer;
  user-select: none;
  animation: mbp-full-in 150ms ease-out;
}
.mbp-full-card {
  --bc-max-h: 74vh;
}
@keyframes mbp-full-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}
.mbp-full-badge {
  position: absolute;
  top: 14px;
  left: 16px;
  padding: 2px 10px;
  border: 1px solid rgba(238, 246, 247, 0.25);
  border-radius: 999px;
  background: rgba(6, 10, 14, 0.55);
  color: rgba(238, 246, 247, 0.8);
  font-size: var(--text-sm);
}
.mbp-full-hint {
  position: absolute;
  bottom: 26px;
  left: 0;
  right: 0;
  text-align: center;
  font-size: var(--text-md);
  color: rgba(238, 246, 247, 0.62);
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  pointer-events: none;
}
.mbp-full-kbd {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  border: 1px solid rgba(238, 246, 247, 0.4);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 6px;
}
@media (prefers-reduced-motion: reduce) {
  .mbp-full {
    animation: none;
  }
}
</style>
