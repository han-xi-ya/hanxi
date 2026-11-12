<script setup lang="ts">
// 账号重命名模态框（mini 卡，拆自 WechatBotView 的纯展示壳）：新备注名输入 + 取消/确定。
// 保存动作（saveAccountRename）与显隐开关（showRenameModal）留在视图编排层；备注名经
// 受控 v-model 与父级 renameNewVal 同一 ref 双向代理。
// 注：模态壳层 CSS 家族系 components.css 共享原子（§9.6-8，与扫码绑定模态同源），
// 本组件 scoped 块只留 mini 卡与动作条真差异。
import { computed, onUnmounted, ref } from 'vue'
import { DIALOG_Z, useDialogA11y } from '../../composables/useDialogA11y'
import { useConfirm } from '../../composables/useConfirm'

const props = defineProps<{
  /** 新备注名（与父级 renameNewVal 同一 ref，受控回写）。 */
  renameNewVal: string
}>()

const emit = defineEmits<{
  'close': []
  'save': []
  'update:renameNewVal': [value: string]
}>()

// 受控输入：读写均代理父级 ref（宿主经 v-model:renameNewVal 接线）。
const nameModel = computed({
  get: () => props.renameNewVal,
  set: (v: string) => emit('update:renameNewVal', v),
})

const titleId = 'wechat-rename-dialog-title'
const nameId = 'wechat-rename-input'
const dialogRef = ref<HTMLElement | null>(null)
const nameRef = ref<HTMLInputElement | null>(null)
const { confirmState } = useConfirm()

// 本组件经视图 v-if 挂载（开着才创建）：useDialogA11y 的 watch(false) 回焦分支
// 不会走到，回焦由 onUnmounted 承接；快照取 setup 同步期（此刻焦点还在触发钮上）。
const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null

// 焦点入窗 / Tab 困窗 / Esc 关闭走单源；z 档 underTop + escapeYield 与 KeyOpsModal
// 同形制——确认框在场时一次 Esc 只关最上层。
useDialogA11y({
  open: () => true,
  dialog: dialogRef,
  z: DIALOG_Z.underTop,
  escapeYield: () => confirmState.open,
  close: () => emit('close'),
  immediate: true,
  focusOnOpen: () => nameRef.value?.focus(),
})

onUnmounted(() => { previousFocus?.focus() })

// 组字豁免：中文输入法回车选词不得被当作保存。
function onNameKeydown(e: KeyboardEvent) {
  if (e.isComposing || e.keyCode === 229) return
  if (e.key === 'Enter') {
    e.preventDefault()
    emit('save')
  }
}
</script>

<template>
  <!-- 账号重命名模态框 (Rename Modal) -->
  <div class="custom-modal-backdrop" @click.self="emit('close')">
    <div ref="dialogRef" class="custom-modal-card mini" role="dialog" aria-modal="true" :aria-labelledby="titleId">
      <div class="cmodal-header">
        <div :id="titleId" class="cmodal-title">
          <span aria-hidden="true">✏️</span> 修改账号备注
        </div>
        <button class="cmodal-close" type="button" aria-label="关闭" @click="emit('close')">✕</button>
      </div>
      <div class="cmodal-body">
        <div class="form-item-block">
          <label :for="nameId" class="form-label">新备注名称</label>
          <input
            :id="nameId"
            ref="nameRef"
            type="text"
            v-model="nameModel"
            placeholder="输入备注名称…"
            class="form-text-input"
            @keydown="onNameKeydown"
          />
        </div>
        <div class="cmodal-actions-bar">
          <button class="btn-cancel" type="button" @click="emit('close')">取消</button>
          <!-- 只挂 .btn-positive 取标准配色，刻意不挂 .btn 基座（本钮 padding 4px 14px
               比标准控件矮，挂基座会被注入 min-height 撑高）。 -->
          <button class="btn-positive btn-submit" type="button" @click="emit('save')">确定</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 模态壳层家族（custom-modal、cmodal、form 系列与 @keyframes modalIn）走
   components.css 共享原子（§9.6-8），以下仅本模态真差异。 */
.custom-modal-card.mini {
  width: 300px;
}

.cmodal-actions-bar {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}

.btn-cancel {
  background: var(--surface-soft);
  color: var(--color-text-muted);
  border: 1px solid var(--color-border);
  padding: 4px 12px;
  border-radius: var(--radius-chip);
  font-size: var(--text-sm);
  cursor: pointer;
}

/* 形制收编至全局 .btn-positive：底/前景/hover/按压/禁用一律走标准档，本块只留尺寸差异
 * （曾自写 background+借 text-inverse 作前景，属 on-color 契约禁令，见 PLAN_N37 §1b）。 */
.btn-submit {
  border: none; /* 必须自留：未挂 .btn 基座，.btn-positive 只给 border-color 不给边框样式 */
  padding: 4px 14px;
  border-radius: var(--radius-chip);
  font-size: var(--text-sm);
  font-weight: 500;
  cursor: pointer;
}
</style>
