<script setup lang="ts">
import { useNotification } from '../composables/useNotification'
import type { Notification } from '../../bindings/hubkit/internal/notify/models'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const {
  notifications,
  unreadCount,
  drawerVisible,
  closeDrawer,
  markAsRead,
  markAllAsRead,
  clearHistory,
} = useNotification()

function formatTime(ts: number) {
  if (!ts) return ''
  const d = new Date(ts)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function getLevelIcon(level: string) {
  switch (level) {
    case 'success':
      return '🟢'
    case 'warning':
      return '🟠'
    case 'error':
      return '🔴'
    default:
      return '🔵'
  }
}

function handleItemClick(item: Notification) {
  markAsRead(item.id)
  if (item.route) {
    emit('navigate', item.route)
    closeDrawer()
  }
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
        <aside v-if="drawerVisible" class="notification-drawer">
          <div class="drawer-header">
            <div class="header-left">
              <h3>🔔 通知中心</h3>
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
                @click="clearHistory"
                title="清空所有通知"
              >
                清空
              </button>
              <button class="btn-close" @click="closeDrawer">✕</button>
            </div>
          </div>

          <!-- 列表容器 -->
          <div class="drawer-body">
            <div v-if="notifications.length === 0" class="empty-state">
              <div class="empty-icon">📭</div>
              <p>暂无任何通知</p>
            </div>

            <div v-else class="notif-list">
              <div
                v-for="item in notifications"
                :key="item.id"
                class="notif-item"
                :class="{ unread: !item.read }"
                @click="handleItemClick(item)"
              >
                <div class="notif-top">
                  <div class="notif-meta">
                    <span class="level-icon">{{ getLevelIcon(item.level) }}</span>
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
  background: rgba(0, 0, 0, 0.35);
  backdrop-filter: blur(1.5px);
  z-index: 10001;
}

.notification-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 360px;
  max-width: 90vw;
  background: var(--bg-sidebar);
  border-left: 1px solid var(--border-color);
  box-shadow: -8px 0 24px rgba(0, 0, 0, 0.12);
  z-index: 10002;
  display: flex;
  flex-direction: column;
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-app);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-left h3 {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
  color: var(--text-main);
}

.badge-unread {
  background: var(--danger);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 10px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-text {
  background: transparent;
  border: none;
  font-size: 12px;
  color: var(--accent);
  cursor: pointer;
  padding: 2px 4px;
}

.btn-text:hover {
  text-decoration: underline;
}

.btn-close {
  background: transparent;
  border: none;
  font-size: 14px;
  color: var(--text-muted);
  cursor: pointer;
  padding: 2px 6px;
  border-radius: 4px;
}

.btn-close:hover {
  background: var(--bg-hover);
  color: var(--text-main);
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
  color: var(--text-subtle);
  gap: 8px;
}

.empty-icon {
  font-size: 32px;
}

.empty-state p {
  font-size: 13px;
  margin: 0;
}

.notif-list {
  display: flex;
  flex-direction: column;
}

.notif-item {
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-color);
  cursor: pointer;
  transition: background 0.15s ease;
  position: relative;
}

.notif-item:hover {
  background: var(--bg-hover);
}

.notif-item.unread {
  background: #f0f7ff;
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

.level-icon {
  font-size: 12px;
}

.mod-tag {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-subtle);
  text-transform: uppercase;
}

.title {
  font-size: 13px;
  color: var(--text-main);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.time {
  font-size: 11px;
  color: var(--text-subtle);
  flex-shrink: 0;
}

.msg {
  font-size: 12px;
  color: var(--text-muted);
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
  background: var(--accent);
}

/* 动效 */
.drawer-slide-enter-active,
.drawer-slide-leave-active {
  transition: transform 0.25s cubic-bezier(0.16, 1, 0.3, 1);
}

.drawer-slide-enter-from,
.drawer-slide-leave-to {
  transform: translateX(100%);
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
