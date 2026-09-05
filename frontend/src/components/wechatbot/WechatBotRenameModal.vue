<script setup lang="ts">
// 账号重命名模态框（mini 卡）：新备注名输入 + 取消/确定。
// 自 WechatBotView.vue 随 DOM 逐字迁出的纯展示壳——保存动作（saveAccountRename）与
// 显隐开关（showRenameModal）留在视图编排层；备注名经受控 v-model 与父级 renameNewVal
// 同一 ref 双向代理，语义与拆分前模板直接 v-model 逐字等价。
// 注：模态壳层 CSS 家族与扫码绑定模态的 11 条逐字副本已上收 components.css 共享原子
// （§9.6-8 治理完成），本组件 scoped 块只留 mini 卡与动作条真差异。
import { computed } from 'vue'

const props = defineProps<{
  /** 新备注名（与父级 renameNewVal 同一 ref，受控回写）。 */
  renameNewVal: string
}>()

const emit = defineEmits<{
  'close': []
  'save': []
  'update:renameNewVal': [value: string]
}>()

// 受控输入：读写均代理父级 ref（等价于拆分前模板直接 v-model="renameNewVal"）。
const nameModel = computed({
  get: () => props.renameNewVal,
  set: (v: string) => emit('update:renameNewVal', v),
})
</script>

<template>
  <!-- 账号重命名模态框 (Rename Modal) -->
  <div class="custom-modal-backdrop" @click.self="emit('close')">
    <div class="custom-modal-card mini">
      <div class="cmodal-header">
        <div class="cmodal-title">
          <span>✏️</span> 修改账号备注
        </div>
        <button class="cmodal-close" @click="emit('close')">✕</button>
      </div>
      <div class="cmodal-body">
        <div class="form-item-block">
          <label class="form-label">新备注名称</label>
          <input
            type="text"
            v-model="nameModel"
            placeholder="输入备注名称…"
            class="form-text-input"
            @keydown.enter="emit('save')"
          />
        </div>
        <div class="cmodal-actions-bar">
          <button class="btn-cancel" @click="emit('close')">取消</button>
          <button class="btn-submit" @click="emit('save')">确定</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 模态壳层家族（custom-modal、cmodal、form 系列与 @keyframes modalIn）
   已上收 components.css（§9.6-8），以下仅本模态真差异。 */
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
  border-radius: 6px;
  font-size: 12px;
  cursor: pointer;
}

.btn-submit {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 4px 14px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
</style>
