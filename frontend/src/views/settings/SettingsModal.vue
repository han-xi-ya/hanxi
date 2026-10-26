<script setup lang="ts">
// 设置分区弹窗骨架：AiSection（MCP 接入向导）与 SnapshotSection（历史版本预览）
// 两家的弹窗皮逐字同构，此处收口遮罩、卡片壳与 head/body/actions 三段及 ✕ 钮；
// 标题、正文与动作按钮全部由宿主经插槽自撰——插槽内容归宿主作用域，宿主 scoped
// 样式（含 .modal-head h3 各自的字族）照常命中。
// 除宽度两档（640/680）外其余口径两处一致；modal-actions 的 surface-panel 底色
// 与卡片底同值，统一起见对两宿主皆无观感差。
// 不含 Esc 关闭：两个来源分区原本皆无该行为，此处不添行为（纪律）。
withDefaults(
  defineProps<{
    /** 卡片定宽 px（max-width 92vw 兜底）；缺省 640（向导档）。 */
    width?: number
  }>(),
  { width: 640 },
)

const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <div class="modal-card" :style="{ width: `${width}px` }">
      <div class="modal-head">
        <slot name="head" />
        <button class="btn-close" aria-label="关闭" @click="emit('close')">✕</button>
      </div>
      <div class="modal-body">
        <slot />
      </div>
      <div class="modal-actions">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal-backdrop {
  position: fixed; inset: 0; z-index: 100;
  background: var(--overlay-mask);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: var(--surface-panel); border-radius: var(--radius-element); max-width: 92vw;
  box-shadow: var(--shadow-panel); overflow: hidden; display: flex; flex-direction: column;
}
.modal-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 20px; border-bottom: 1px solid var(--color-border);
}
.btn-close { background: transparent; border: none; font-size: var(--text-lg); cursor: pointer; color: var(--color-text-muted); }
.modal-body { padding: 14px 20px; display: flex; flex-direction: column; gap: 10px; max-height: 62vh; overflow: auto; }
.modal-actions {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 12px 20px; border-top: 1px solid var(--color-border); background: var(--surface-panel);
}
</style>
