<script setup lang="ts">
// 扫码绑定微信机器人模态框：备注名输入 + 二维码渲染/状态胶囊 + 重试与提示。
// 自 WechatBotView.vue 随 DOM 逐字迁出的纯展示壳——取码/轮询/关窗等业务动作仍由
// useWechatBot 编排（本组件仅渲染 qr 状态并上抛 close/retry 与备注名回写）。
// 显隐开关（showBindModal）留在视图编排层 v-if，与原模板条件位置等价。
// 注：模态壳层 CSS（.custom-modal-backdrop/.custom-modal-card/@keyframes modalIn/
// .cmodal-*/.form-* 家族）与重命名模态为跨组件共用，按「标记与样式同迁、不做语义
// 改动」原则两侧各留一份逐字副本（§9.6 登记）。
import { computed } from 'vue'

const props = defineProps<{
  /** 机器人备注名（与父级 bindRemarkName 同一 ref，受控回写）。 */
  bindRemarkName: string
  /** 取码请求进行中。 */
  qrLoading: boolean
  /** 二维码 dataURL（空串走失败态盒）。 */
  qrDataUrl: string
  /** 扫码状态文案。 */
  qrStatusText: string
  /** 扫码状态档位（驱动状态胶囊配色类）。 */
  qrStatusType: 'wait' | 'scaned' | 'confirmed' | 'expired' | 'error' | ''
}>()

const emit = defineEmits<{
  'close': []
  'retry': []
  'update:bindRemarkName': [value: string]
}>()

// 受控输入：读写均代理父级 ref（等价于拆分前模板直接 v-model="bindRemarkName"）。
const remarkModel = computed({
  get: () => props.bindRemarkName,
  set: (v: string) => emit('update:bindRemarkName', v),
})
</script>

<template>
  <!-- 扫码绑定模态框 (QR Bind Modal) -->
  <div class="custom-modal-backdrop" @click.self="emit('close')">
    <div class="custom-modal-card">
      <div class="cmodal-header">
        <div class="cmodal-title">
          <span>📱</span> 扫码绑定微信机器人
        </div>
        <button class="cmodal-close" @click="emit('close')">✕</button>
      </div>

      <div class="cmodal-body">
        <div class="form-item-block">
          <label class="form-label">机器人备注名</label>
          <input
            type="text"
            v-model="remarkModel"
            placeholder="例如: 告警通知号 / 客户群机器人"
            class="form-text-input"
          />
        </div>

        <div class="qr-render-container">
          <div v-if="qrLoading" class="qr-state-box">
            <span class="qr-spinner">⏳</span>
            <p>正在生成微信登录二维码…</p>
          </div>
          <div v-else-if="qrDataUrl" class="qr-success-box">
            <img :src="qrDataUrl" alt="微信登录二维码" class="qr-canvas-img" />
            <div class="qr-badge-pill" :class="qrStatusType">
              {{ qrStatusText }}
            </div>
          </div>
          <div v-else class="qr-state-box">
            <p class="qr-err-text">{{ qrStatusText || '拉取二维码失败' }}</p>
            <button class="btn-retry-qr" @click="emit('retry')">重新获取</button>
          </div>
        </div>

        <div class="cmodal-hints">
          <div>1. 请使用待绑定的微信号扫码并授权登录；</div>
          <div>2. 授权成功后系统自动保存凭据并启动独立后台长轮询监听。</div>
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

.qr-render-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  background: var(--surface-soft);
  border-radius: 8px;
  border: 1px dashed var(--color-border);
  padding: 12px;
}

.qr-state-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
  font-size: 12px;
  gap: 8px;
}

.qr-spinner {
  font-size: 24px;
}

.qr-success-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.qr-canvas-img {
  width: 170px;
  height: 170px;
  border-radius: 6px;
  border: 1px solid var(--color-border);
}

.qr-badge-pill {
  font-size: 11px;
  padding: 3px 10px;
  border-radius: 12px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
}

.qr-badge-pill.scaned {
  background: var(--surface-selected);
  color: var(--color-primary);
}

.qr-badge-pill.confirmed {
  background: var(--state-positive-soft);
  color: var(--state-positive);
}

.qr-badge-pill.expired,
.qr-badge-pill.error {
  background: var(--state-danger-soft);
  color: var(--state-danger);
}

.btn-retry-qr {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
}

.cmodal-hints {
  font-size: 11px;
  color: var(--color-text-subtle);
  line-height: 1.5;
}
</style>
