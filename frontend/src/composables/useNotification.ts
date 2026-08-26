import { ref, computed } from 'vue'
import * as NotifyAPI from '../../bindings/hubkit/internal/notify'
import type { Notification } from '../../bindings/hubkit/internal/notify/models'

export interface ToastItem {
  id: string
  moduleId: string
  title: string
  message: string
  level: string
  route: string
  timestamp: number
  read: boolean
  timer?: ReturnType<typeof setTimeout>
}

const activeToasts = ref<ToastItem[]>([])
const notifications = ref<Notification[]>([])
const drawerVisible = ref(false)

const unreadCount = computed(() => {
  return notifications.value.filter((n) => !n.read).length
})

export function useNotification() {
  async function loadHistory() {
    try {
      const list = await NotifyAPI.NotificationService.GetHistory()
      notifications.value = (list ?? []).filter(Boolean) as Notification[]
    } catch (e) {
      console.error('Failed to load notification history:', e)
    }
  }

  function pushToast(item: Notification) {
    if (!item) return

    // 补齐缺失字段
    if (!item.id) {
      item.id = `notif_${Date.now()}_${Math.random().toString(36).substring(2, 7)}`
    }
    if (!item.title) {
      item.title = '系统通知'
    }
    if (!item.level) {
      item.level = 'info'
    }

    // 检查历史列表中是否已有（防重复）
    const existsIndex = notifications.value.findIndex((existing) => existing.id === item.id)
    if (existsIndex >= 0) {
      notifications.value[existsIndex] = item
    } else {
      notifications.value.unshift(item)
      if (notifications.value.length > 100) {
        notifications.value.pop()
      }
    }

    // 构造前台浮动卡片 Toast
    const toast: ToastItem = {
      id: item.id,
      moduleId: item.moduleId || 'system',
      title: item.title,
      message: item.message || '',
      level: item.level || 'info',
      route: item.route || '',
      timestamp: item.timestamp || Date.now(),
      read: item.read || false,
    }
    toast.timer = setTimeout(() => {
      removeToast(toast.id)
    }, 5000)

    // 最多保留最新 4 条 Toast 浮窗
    activeToasts.value.unshift(toast)
    if (activeToasts.value.length > 4) {
      const popped = activeToasts.value.pop()
      if (popped?.timer) clearTimeout(popped.timer)
    }
  }

  function removeToast(id: string) {
    const idx = activeToasts.value.findIndex((t) => t.id === id)
    if (idx >= 0) {
      const item = activeToasts.value[idx]
      if (item.timer) clearTimeout(item.timer)
      activeToasts.value.splice(idx, 1)
    }
  }

  async function markAsRead(id: string) {
    try {
      await NotifyAPI.NotificationService.MarkAsRead(id)
      const item = notifications.value.find((n) => n.id === id)
      if (item) item.read = true
    } catch (e) {
      console.error('Failed to mark notification as read:', e)
    }
  }

  async function markAllAsRead() {
    try {
      await NotifyAPI.NotificationService.MarkAllAsRead()
      notifications.value.forEach((n) => (n.read = true))
    } catch (e) {
      console.error('Failed to mark all notifications as read:', e)
    }
  }

  async function clearHistory() {
    try {
      await NotifyAPI.NotificationService.ClearHistory()
      notifications.value = []
    } catch (e) {
      console.error('Failed to clear notification history:', e)
    }
  }

  function toggleDrawer() {
    drawerVisible.value = !drawerVisible.value
  }

  function closeDrawer() {
    drawerVisible.value = false
  }

  return {
    activeToasts,
    notifications,
    unreadCount,
    drawerVisible,
    loadHistory,
    pushToast,
    removeToast,
    markAsRead,
    markAllAsRead,
    clearHistory,
    toggleDrawer,
    closeDrawer,
  }
}
