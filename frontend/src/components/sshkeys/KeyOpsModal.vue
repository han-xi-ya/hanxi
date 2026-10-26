<script setup lang="ts">
// 密钥操作对话框的共用壳：模态家族原子（.custom-modal-*，§9.6-8 已上收
// components.css）+ useDialogA11y 单源（焦点入窗 / Tab 困窗 / Esc / 回焦）。
//
// 为什么必须走 useDialogA11y 而不是自己按 wechatbot 模态那样只挂遮罩点击：
// 这里的 Esc/取消/遮罩都不只是"关个窗"——部署与改口令对话框里活着一次性口令，
// 纪律要求**任一出口（成功、失败、取消、Esc、遮罩、卸载）都销毁口令**。把出口
// 收敛到一个 close() 上，销毁只写一处，四条出口才不会漏一条（漏的那条就是事故）。
//
// z 档取 DIALOG_Z.underTop（950，视图级对话框）：部署确认步要叠 App 单例的
// ConfirmDialog（1000 层），本壳必须让位——escapeYield 在确认框在场时把 Esc
// 交还给上层，一次 Esc 只关最上一层（document 级监听同场竞走是登记在案的历史
// 事故，useDialogA11y 头注）。
import { ref } from 'vue'
import { DIALOG_Z, useDialogA11y } from '../../composables/useDialogA11y'
import { useConfirm } from '../../composables/useConfirm'

const props = defineProps<{
  open: boolean
  title: string
}>()

const emit = defineEmits<{ close: [] }>()

const dialog = ref<HTMLElement | null>(null)
const { confirmState } = useConfirm()

function close() {
  emit('close')
}

// 宿主（对话框正文组件）需要把初始焦点落进第一个字段时，用 #body 里的元素自行
// 抢焦点；缺省落 dialog 根（本根挂 tabindex="-1"，正是该钩子的缺省契约）。
useDialogA11y({
  open: () => props.open,
  dialog,
  z: DIALOG_Z.underTop,
  escapeYield: () => confirmState.open,
  close,
  keepAliveRebind: true,
})
</script>

<template>
  <div v-if="open" class="custom-modal-backdrop keyops-backdrop" @mousedown.self="close">
    <section
      ref="dialog"
      class="custom-modal-card keyops-card"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
    >
      <div class="cmodal-header">
        <div class="cmodal-title">{{ title }}</div>
        <button type="button" class="cmodal-close" aria-label="关闭" @click="close">✕</button>
      </div>
      <!-- busy 门禁（在途不许关窗、免得手滑换目标）由宿主在 close 出口里自检；
           壳层只做形制与可访问性，不掺业务态。 -->
      <div class="cmodal-body">
        <slot />
      </div>
    </section>
  </div>
</template>

<style scoped>
/* 只补布局差异，壳层形制（底色/边框/圆角/动画）全部走全局 .custom-modal-* 原子。 */
.keyops-backdrop {
  /* z 与 DIALOG_Z.underTop 互锁（登记处见 useDialogA11y 头注）：本壳不 Teleport，
     层叠位置在页面流里，同 z 拼不过 App 单例确认框（Teleport 到 body 末尾）；
     950 档是按层阶判出来的视图级对话框档，不是随手写的数。 */
  z-index: var(--z-dialog-under);
}
.keyops-card {
  width: min(560px, 92vw);
  max-height: 86vh;
  overflow-y: auto;
}
</style>
