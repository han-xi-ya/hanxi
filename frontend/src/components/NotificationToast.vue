<script setup lang="ts">
import { useNotification, type ToastItem } from '../composables/useNotification'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { activeToasts, removeToast, markAsRead } = useNotification()

// 严重度指示灯：emoji 已退役（AppIcon 纪律）——色点仅作冗余强调，
// 语义由模块标签/标题文本承载，色板走 state token。
function levelTone(level: string) {
  switch (level) {
    case 'success':
      return 'sev-success'
    case 'warning':
      return 'sev-warning'
    case 'error':
      return 'sev-error'
    default:
      return 'sev-info'
  }
}

function handleToastClick(toast: ToastItem) {
  markAsRead(toast.id)
  removeToast(toast.id)
  if (toast.route) {
    emit('navigate', toast.route)
  }
}

function handleClose(e: MouseEvent, toast: ToastItem) {
  e.stopPropagation()
  removeToast(toast.id)
}
</script>

<template>
  <Teleport to="body">
    <div class="toast-stack" v-if="activeToasts.length > 0">
      <TransitionGroup name="toast-slide">
        <div
          v-for="t in activeToasts"
          :key="t.id"
          class="toast-card"
          :class="[`toast-${t.level}`]"
          @click="handleToastClick(t)"
        >
          <div class="toast-indicator">
            <span class="sev-dot" :class="levelTone(t.level)" aria-hidden="true"></span>
          </div>
          <div class="toast-body">
            <div class="toast-header">
              <span class="toast-module">[{{ t.moduleId }}]</span>
              <strong class="toast-title">{{ t.title }}</strong>
            </div>
            <p class="toast-msg">{{ t.message }}</p>
          </div>
          <button class="toast-close" @click="handleClose($event, t)" title="关闭">✕</button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-stack {
  position: fixed;
  top: 16px;
  right: 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  z-index: 999999;
  pointer-events: none;
  width: 320px;
  max-width: calc(100vw - 40px);
}

.toast-card {
  pointer-events: auto;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-left: 4px solid var(--color-primary);
  border-radius: var(--radius-control);
  box-shadow: var(--shadow-panel);
  display: flex;
  align-items: flex-start;
  padding: 12px 14px;
  gap: 12px;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  position: relative;
  overflow: hidden;
}

.toast-card.toast-success {
  border-left-color: var(--state-positive);
}

.toast-card.toast-warning {
  border-left-color: var(--state-warning);
}

.toast-card.toast-error {
  border-left-color: var(--state-danger);
}

.toast-card.toast-info {
  border-left-color: var(--state-information);
}

.toast-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow-panel);
}

.toast-indicator {
  flex-shrink: 0;
  display: flex;
  align-items: flex-start;
  margin-top: 5px;
}
.sev-dot { width: 9px; height: 9px; border-radius: var(--radius-pill); background: var(--state-information); }
.sev-success { background: var(--state-positive); }
.sev-warning { background: var(--state-warning); }
.sev-error { background: var(--state-danger); }

.toast-body {
  flex: 1;
  min-width: 0;
}

.toast-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 2px;
}

.toast-module {
  font-size: 11px;
  color: var(--color-text-subtle);
  text-transform: uppercase;
  font-weight: 600;
}

.toast-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.toast-msg {
  font-size: 12px;
  color: var(--color-text-muted);
  margin: 0;
  line-height: 1.4;
  word-break: break-all;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.toast-close {
  background: transparent;
  border: none;
  font-size: 12px;
  color: var(--color-text-subtle);
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 4px;
  align-self: flex-start;
}

.toast-close:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

/* 动效 */
.toast-slide-enter-active,
.toast-slide-leave-active {
  transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);
}

.toast-slide-enter-from {
  opacity: 0;
  transform: translateX(30px) scale(0.92);
}

.toast-slide-leave-to {
  opacity: 0;
  transform: scale(0.85);
}
</style>
