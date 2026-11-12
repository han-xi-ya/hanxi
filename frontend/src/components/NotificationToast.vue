<script setup lang="ts">
import { useNotification, type ToastItem } from '../composables/useNotification'
import { levelTone } from '../constants/status'
import AppIcon from './ui/AppIcon.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { activeToasts, removeToast, markAsRead } = useNotification()

// 严重度指示灯（AppIcon 纪律：emoji 不作图标）：色点仅作冗余强调，语义由模块标签/标题
// 文本承载。档位映射取 constants/status.ts levelTone 单源，.sev-* 色板在 components.css。

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
    <div class="toast-stack" v-if="activeToasts.length > 0" aria-live="polite" aria-relevant="additions">
      <TransitionGroup name="toast-slide">
        <!-- 外壳仅承担层级/进出场/离场绝对定位；可点主体与关闭钮为兄弟真 button，
             杜绝「钮套钮」。主体 button 承载卡片外观（左色条/底/影），关闭钮绝对定位右上。 -->
        <div v-for="t in activeToasts" :key="t.id" class="toast-item" :class="[`toast-${t.level}`]">
          <button class="toast-card" type="button" @click="handleToastClick(t)">
            <span class="toast-indicator">
              <span class="sev-dot" :class="levelTone(t.level)" aria-hidden="true"></span>
            </span>
            <span class="toast-body">
              <span class="toast-header">
                <span class="toast-module">[{{ t.moduleId }}]</span>
                <strong class="toast-title">{{ t.title }}</strong>
              </span>
              <span class="toast-msg">{{ t.message }}</span>
            </span>
          </button>
          <button class="toast-close" aria-label="关闭通知" @click="handleClose($event, t)" title="关闭">
            <AppIcon name="x" :size="13" />
          </button>
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
  z-index: var(--z-toast);
  pointer-events: none;
  width: 320px;
  max-width: calc(100vw - 40px);
}

/* 外壳：进出场/离场绝对定位载体；pointer-events:auto 让卡片与关闭钮都可点
   （栈容器 pointer-events:none 防空白区拦页面，卡片区域经此恢复）。卡片外观全在 .toast-card。 */
.toast-item {
  position: relative;
  width: 100%;
  pointer-events: auto;
  transition: transform var(--motion-base) cubic-bezier(0.16, 1, 0.3, 1);
}

.toast-card {
  width: 100%;
  text-align: left;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-left: 4px solid var(--color-primary);
  border-radius: var(--radius-control);
  box-shadow: var(--shadow-panel);
  display: flex;
  align-items: flex-start;
  padding: 12px 30px 12px 14px;
  gap: 12px;
  cursor: pointer;
  transition: box-shadow var(--motion-base) cubic-bezier(0.16, 1, 0.3, 1);
  overflow: hidden;
}

.toast-item.toast-success .toast-card {
  border-left-color: var(--state-positive);
}

.toast-item.toast-warning .toast-card {
  border-left-color: var(--state-warning);
}

.toast-item.toast-error .toast-card {
  border-left-color: var(--state-danger);
}

.toast-item.toast-info .toast-card {
  border-left-color: var(--state-information);
}

/* hover 整卡（含关闭钮）同步抬 2px，阴影提一档：--shadow-panel 叠中性 1px 提亮描边
   （稳态禁彩色发光）。移外壳保证关闭钮不脱卡；阴影仍落卡片本体。 */
.toast-item:hover {
  transform: translateY(-2px);
}
.toast-item:hover .toast-card {
  box-shadow: var(--shadow-panel), inset 0 0 0 1px var(--color-border-strong);
}

.toast-indicator {
  flex-shrink: 0;
  display: flex;
  align-items: flex-start;
  margin-top: 5px;
}
/* .sev-dot / .sev-{success,warning,error} 四条样式在 components.css 全局原子区
   （本组件与 Drawer 的同形 scoped 副本已删净）。 */

.toast-body {
  flex: 1;
  min-width: 0;
  display: block;
}

.toast-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 2px;
  min-width: 0;
}

.toast-module {
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  text-transform: uppercase;
  font-weight: 600;
}

.toast-title {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--color-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.toast-msg {
  font-size: var(--text-sm);
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
  position: absolute;
  top: 8px;
  right: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  color: var(--color-text-subtle);
  cursor: pointer;
  padding: 2px;
  border-radius: var(--radius-micro);
}

.toast-close:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

/* 动效：进场/离场作用于外壳；离场绝对定位脱离文档流，其余卡片经 move 过渡平滑补位 */
.toast-slide-enter-active,
.toast-slide-leave-active {
  transition: opacity var(--motion-slow) cubic-bezier(0.16, 1, 0.3, 1), transform var(--motion-slow) cubic-bezier(0.16, 1, 0.3, 1);
}

.toast-slide-leave-active {
  position: absolute;
  width: 100%;
}

.toast-slide-move {
  transition: transform var(--motion-slow) cubic-bezier(0.16, 1, 0.3, 1);
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
