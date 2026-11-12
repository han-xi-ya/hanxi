<script setup lang="ts">
// 通知中心抽屉：遮罩模态，键盘契约走 useDialogA11y 单源——Esc/Tab 环困窗/回焦。
// CSS 用抽屉专属档 --z-drawer(940)/--z-drawer-panel(945)，低于 App 单例 confirm/prompt(1000)，
// 使「清空」确认框与错误回执浮于抽屉之上；z 参数取 underTop 仅作 Esc 让位语义判别（不写 CSS），
// escapeYield 在确认/输入框在场时让位——一次 Esc 只关最上层。
import { ref } from 'vue'
import { useNotification } from '../composables/useNotification'
import { DIALOG_Z, useDialogA11y } from '../composables/useDialogA11y'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { Notification } from '../../bindings/hanxi/internal/notify/models'
import { levelTone } from '../constants/status'
import AppIcon from './ui/AppIcon.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const {
  notifications,
  unreadCount,
  drawerVisible,
  historyError,
  loadHistory,
  closeDrawer,
  markAsRead,
  markAllAsRead,
  clearHistory,
} = useNotification()
const { confirm, confirmState } = useConfirm()
const { promptState } = usePrompt()

const drawerEl = ref<HTMLElement | null>(null)

// 焦点入窗落抽屉根（tabindex=-1，钩子缺省契约）；关窗回焦触发点（侧栏铃铛）。
// immediate：以 drawerVisible 翻真驱动，翻真后 nextTick 才挂监听/进焦点。
useDialogA11y({
  open: () => drawerVisible.value,
  dialog: drawerEl,
  z: DIALOG_Z.underTop,
  close: closeDrawer,
  escapeYield: () => confirmState.open || promptState.open,
  immediate: true,
})

function formatTime(ts: number) {
  if (!ts) return ''
  const d = new Date(ts)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 严重度指示灯：emoji 已退役（AppIcon 纪律），色点仅冗余强调。档位映射取
// constants/status.ts levelTone 单源（与 NotificationToast 同源），色板在 components.css。

function handleItemClick(item: Notification) {
  markAsRead(item.id)
  if (item.route) {
    emit('navigate', item.route)
    closeDrawer()
  }
}

// 清空不可逆，先经全局 useConfirm 确认（danger 档）；失败回执由 useNotification
// 的 showErrorToast 承载，抽屉不静默。
async function handleClear() {
  const accepted = await confirm({
    title: '清空所有通知',
    description: `将删除全部 ${notifications.value.length} 条通知历史，此操作不可撤销。确定要清空吗？`,
    confirmLabel: '清空',
    tone: 'danger',
  })
  if (accepted) await clearHistory()
}
</script>

<template>
  <Teleport to="body">
    <div>
      <!-- 遮罩 -->
      <Transition name="fade">
        <div v-if="drawerVisible" class="drawer-overlay" @click="closeDrawer"></div>
      </Transition>

      <!-- 抽屉主体 -->
      <Transition name="drawer-slide">
        <aside
          v-if="drawerVisible"
          ref="drawerEl"
          class="notification-drawer"
          role="dialog"
          aria-modal="true"
          aria-label="通知中心"
          tabindex="-1"
        >
          <div class="drawer-header">
            <div class="header-left">
              <h3><AppIcon name="bell" :size="15" /> 通知中心</h3>
              <span v-if="unreadCount > 0" class="badge-unread">{{ unreadCount }} 未读</span>
            </div>
            <div class="header-actions">
              <button
                v-if="notifications.length > 0"
                class="btn-text"
                @click="markAllAsRead"
                title="全部设为已读"
              >
                全部已读
              </button>
              <button
                v-if="notifications.length > 0"
                class="btn-text"
                @click="handleClear"
                title="清空所有通知"
              >
                清空
              </button>
              <button class="btn-close" aria-label="关闭通知中心" @click="closeDrawer">✕</button>
            </div>
          </div>

          <!-- 列表容器 -->
          <div class="drawer-body">
            <!-- 加载失败：保留可重试错误条（toast 之外的上下文内反馈），不静默 -->
            <div v-if="historyError" class="error-box drawer-error" role="alert">
              <p class="drawer-error-msg">通知历史加载失败：{{ historyError }}</p>
              <button class="btn btn-secondary btn-small" @click="loadHistory">重试</button>
            </div>
            <div v-else-if="notifications.length === 0" class="empty-state">
              <div class="empty-icon"><AppIcon name="inbox" :size="36" /></div>
              <p>暂无任何通知</p>
            </div>

            <div v-else class="notif-list">
              <!-- 行用 role=button+tabindex 补键盘可达（内含块级 div/p，不宜换真 button）；
                   Enter/Space 触发，Space preventDefault 防滚动。Tab 环由 useDialogA11y 困窗。 -->
              <div
                v-for="item in notifications"
                :key="item.id"
                class="notif-item"
                :class="{ unread: !item.read }"
                role="button"
                tabindex="0"
                @click="handleItemClick(item)"
                @keydown.enter="handleItemClick(item)"
                @keydown.space.prevent="handleItemClick(item)"
              >
                <div class="notif-top">
                  <div class="notif-meta">
                    <span class="level-icon sev-dot" :class="levelTone(item.level)" aria-hidden="true"></span>
                    <span class="mod-tag">[{{ item.moduleId }}]</span>
                    <strong class="title">{{ item.title }}</strong>
                  </div>
                  <span class="time">{{ formatTime(item.timestamp) }}</span>
                </div>
                <p class="msg">{{ item.message }}</p>
                <div v-if="!item.read" class="unread-dot" title="未读"></div>
              </div>
            </div>
          </div>
        </aside>
      </Transition>
    </div>
  </Teleport>
</template>

<style scoped>
.drawer-overlay {
  position: fixed;
  inset: 0;
  background: var(--overlay-mask);
  backdrop-filter: blur(1.5px);
  /* 背板专属档（低于本体）；抽屉整体降入对话框下档，confirm/prompt/snackbar 浮于其上 */
  z-index: var(--z-drawer);
}

.notification-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 360px;
  max-width: 90vw;
  background: var(--surface-panel);
  border-left: 1px solid var(--color-border);
  box-shadow: var(--shadow-drawer);
  /* 本体专属档 945 < App 单例对话框 1000：「清空」确认框须可点，错误回执亦可见 */
  z-index: var(--z-drawer-panel);
  display: flex;
  flex-direction: column;
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--color-border);
  background: var(--surface-page);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-left h3 {
  font-size: var(--text-md);
  font-weight: 600;
  margin: 0;
  color: var(--color-text);
}

.badge-unread {
  background: var(--state-danger);
  color: var(--color-on-accent);
  font-size: var(--text-xs);
  font-weight: 600;
  padding: 1px 6px;
  border-radius: var(--radius-pill);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-text {
  background: transparent;
  border: none;
  font-size: var(--text-sm);
  color: var(--color-primary);
  cursor: pointer;
  padding: 2px 4px;
}

.btn-text:hover {
  text-decoration: underline;
}

.btn-close {
  background: transparent;
  border: none;
  font-size: var(--text-md);
  color: var(--color-text-muted);
  cursor: pointer;
  padding: 2px 6px;
  border-radius: var(--radius-micro);
}

.btn-close:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 10px 0;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 240px;
  color: var(--color-text-subtle);
  gap: 8px;
}

.empty-icon {
  font-size: var(--text-3xl);
}

.empty-state p {
  font-size: var(--text-base);
  margin: 0;
}

/* 加载失败错误条：复用全局 .error-box 原子（软底/描边/字色），本处只补布局 */
.drawer-error {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  margin: 10px 16px;
}

.drawer-error-msg {
  margin: 0;
  word-break: break-all;
}

.notif-list {
  display: flex;
  flex-direction: column;
}

.notif-item {
  padding: 10px 16px;
  border-bottom: 1px solid var(--color-border);
  cursor: pointer;
  transition: background var(--motion-base) ease;
  position: relative;
}

.notif-item:hover {
  background: var(--surface-hover);
}

.notif-item.unread {
  background: var(--color-primary-soft);
}

/* 未读行 hover 混合档：特异性高于 .unread 与 :hover，压住恒定的 primary-soft 底 */
.notif-item.unread:hover {
  background: color-mix(in srgb, var(--color-primary-soft), var(--surface-hover));
}

.notif-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.notif-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

/* .sev-dot / .sev-{success,warning,error} 四条样式在 components.css（本抽屉的同形
   scoped 副本已删净）；.level-icon 为抽屉列表行独有的收缩补差，留本组件。 */
.level-icon { flex: none; }

.mod-tag {
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--color-text-subtle);
  text-transform: uppercase;
}

.title {
  font-size: var(--text-base);
  color: var(--color-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.time {
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  flex-shrink: 0;
}

.msg {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  margin: 0;
  line-height: 1.4;
  word-break: break-all;
}

.unread-dot {
  position: absolute;
  top: 12px;
  right: 6px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-primary);
}

/* 动效 */
.drawer-slide-enter-active,
.drawer-slide-leave-active {
  transition: transform var(--motion-slow) cubic-bezier(0.16, 1, 0.3, 1);
}

.drawer-slide-enter-from,
.drawer-slide-leave-to {
  transform: translateX(100%);
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity var(--motion-slow) ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
