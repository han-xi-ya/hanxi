<script setup lang="ts">
// 账号重命名模态框（mini 卡）：新备注名输入 + 取消/确定。
// 自 WechatBotView.vue 随 DOM 逐字迁出的纯展示壳——保存动作（saveAccountRename）与
// 显隐开关（showRenameModal）留在视图编排层；备注名经受控 v-model 与父级 renameNewVal
// 同一 ref 双向代理，语义与拆分前模板直接 v-model 逐字等价。
// 注：模态壳层 CSS 家族与扫码绑定模态为跨组件共用，两侧各留一份逐字副本（§9.6 登记）。
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
/* 以下样式自 WechatBotView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
/* 4. 模态弹窗 */
.custom-modal-backdrop {
  position: fixed;
  top: 0;
  left: 0;
  width: 100vw;
  height: 100vh;
  background: var(--overlay-mask);
  backdrop-filter: blur(2px);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
}

.custom-modal-card {
  width: 360px;
  background: var(--surface-panel);
  border-radius: 10px;
  box-shadow: var(--shadow-panel);
  overflow: hidden;
  animation: modalIn 0.18s ease-out;
}

.custom-modal-card.mini {
  width: 300px;
}

@keyframes modalIn {
  from {
    opacity: 0;
    transform: scale(0.96);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.cmodal-header {
  padding: 12px 16px;
  border-bottom: 1px solid var(--color-border);
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.cmodal-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
  display: flex;
  align-items: center;
  gap: 6px;
}

.cmodal-close {
  background: transparent;
  border: none;
  font-size: 14px;
  color: var(--color-text-subtle);
  cursor: pointer;
}

.cmodal-body {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-item-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.form-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--color-text);
}

.form-text-input {
  padding: 6px 10px;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-size: 13px;
  outline: none;
  transition: border-color 0.15s ease;
}

.form-text-input:focus {
  border-color: var(--state-positive);
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
