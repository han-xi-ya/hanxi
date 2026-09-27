<script setup lang="ts">
// 历史记录弹窗共享壳（N20 收尾）：Teleport 遮罩 + 对话框骨架 + 交互契约一体化——
// 原先 OcrView/PortKillView 两份逐字壳各自持有三处病灶（Esc 一键穿两层、z 序被
// App 单例 ConfirmDialog 倒挂、零焦点管理），修一次就要追两遍；收编为单件后
// 契约只有一份。类名沿用 hist-*（两视图特征测试锁定的选择器零断言成本迁移）。
//
// 交互契约（对齐全仓 ConfirmDialog 标杆语义，勿"好心"简化——机制本体在
// composables/useDialogA11y 单源，波 2C 接线；本件差异参数逐条对应下列契约）：
//  1) Esc 在 confirmState.open 时让位（escapeYield 参数）——面板内「清空本桶」确认
//     盖在本弹窗上，一次 Esc 只关最上层（document 级监听同场竞走是历史事故根源）；
//  2) z-index 950（z: DIALOG_Z.underTop，互锁进类型面）低于 App 单例的 1000——
//     宿主视图经 KeepAlive 懒挂载，Teleport 锚点 DOM 序天然晚于 App 单例，同层拼位置必输；
//  3) 开窗焦点入 dialog（tabindex=-1，focusOnOpen 缺省档）、Tab 环困在窗内
//     （选择器镜像 ConfirmDialog 并补 input/select/textarea——面板有搜索框，
//     即 useDialogA11y 的 DEFAULT_FOCUSABLE 最宽档）、关窗焦点回位触发点。
import { ref } from 'vue'
import { useConfirm } from '../../composables/useConfirm'
import { DIALOG_Z, useDialogA11y } from '../../composables/useDialogA11y'

const props = defineProps<{
  open: boolean
  /** 标题（同时作 aria-label 与头行文案） */
  title: string
  /** 一句话说明行：记录从哪来/何时产生/留多少（可省） */
  note?: string
}>()

const emit = defineEmits<{ close: [] }>()

const { confirmState } = useConfirm()
const dialogEl = ref<HTMLElement | null>(null)

// document 级监命的生命周期兜底（审查 P1）由 keepAliveRebind 承接：宿主页
// KeepAlive LRU 逐出/切页时弹窗可能仍开着——deactivate（切页未卸载）摘、
// activate（回访仍开着）补挂，杜绝异页按 Esc 幽灵关闭隐藏弹窗；卸载兜底恒在。
// immediate：宿主以 open=true 首挂（如未来持久化弹窗态）也要注册监听/进焦点；
// false 起步时走 else 分支完全幂等（removeEventListener 无害、previousFocus 为空）。
useDialogA11y({
  open: () => props.open,
  dialog: dialogEl,
  z: DIALOG_Z.underTop,
  close: () => emit('close'),
  escapeYield: () => confirmState.open, // 确认框在场时 Esc 归它
  escapePreventsDefault: false,
  immediate: true,
  keepAliveRebind: true,
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="hist-backdrop" @click.self="emit('close')">
      <div ref="dialogEl" class="hist-dialog" role="dialog" aria-modal="true" :aria-label="title" tabindex="-1">
        <div class="hist-head">
          <div class="hist-head-text">
            <h2>{{ title }}</h2>
            <p v-if="note" class="hist-note">{{ note }}</p>
          </div>
          <button class="btn btn-secondary btn-small" @click="emit('close')">关闭</button>
        </div>
        <slot />
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* 皮：两份壳原有微差，去重取 Ocr/N20 版（token 阴影/圆角，PortKill 侧观感
   统一为同一档，真机一并目检）；z 950 < ConfirmDialog 1000 */
.hist-backdrop { position: fixed; inset: 0; z-index: 950; display: grid; place-items: center; padding: 24px; background: var(--overlay-mask); }
.hist-dialog {
  width: min(720px, 100%); max-height: min(80vh, 640px); overflow: auto; display: flex; flex-direction: column; gap: 12px;
  padding: 16px 18px; background: var(--surface-panel); border: 1px solid var(--color-border);
  border-radius: var(--radius-element); box-shadow: var(--shadow-small);
}
.hist-dialog:focus { outline: none; }
.hist-dialog:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
.hist-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.hist-head-text { min-width: 0; }
.hist-head h2 { font-size: var(--text-md); font-weight: 600; margin: 0; }
.hist-note { margin: 4px 0 0; font-size: var(--text-xs); line-height: 1.6; color: var(--color-text-muted); }
</style>
