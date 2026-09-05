<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import QRCode from 'qrcode'
import * as WechatAPI from '../../bindings/hanxi/internal/modules/wechat'
import type { WechatAccountState, QRInfo, InboundMessage } from '../../bindings/hanxi/internal/modules/wechat/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'

const { showToast } = useToast()
const { confirm } = useConfirm()

// 账号列表与当前选中账号
const accounts = ref<WechatAccountState[]>([])
const selectedAccountId = ref<string>('')

// 当前选中的账号对象
const currentAccount = computed<WechatAccountState | null>(() => {
  if (!accounts.value || accounts.value.length === 0) return null
  return accounts.value.find(a => a.id === selectedAccountId.value) || accounts.value[0]
})

// 聊天消息实体
export interface ChatMessage {
  id: string
  accountId: string
  time: string
  direction: 'in' | 'out' | 'sys'
  msgType: 'text' | 'image' | 'file' | 'system'
  senderName?: string
  content: string
  filePath?: string
  fileName?: string
  fileSize?: number
  attachmentId?: string
  downloadable?: boolean
  attachmentError?: string
  status?: 'sending' | 'sent' | 'failed'
  error?: string
}

// 消息列表（按会话隔离）
const chatMessages = ref<ChatMessage[]>([])
const messageContainer = ref<HTMLDivElement | null>(null)

// 输入框与发送状态
const inputText = ref('')
const isSending = ref(false)
const attachmentAction = ref<Record<string, 'opening' | 'saving' | undefined>>({})
const toUserIdInput = ref('')
const isEditingTargetUser = ref(false)

// 侧边栏折叠状态
const isSidebarCollapsed = ref(false)

// 规则横幅折叠状态
const showRulesBanner = ref(true)

// 扫码绑定弹窗状态
// 注：qrPollTimer 有意保留手管——它是"弹窗会话级"轮询（取码即启、scaned/confirmed/expired/
// 关闭多出口启停 + isChecking 防重入），与 usePolling 的 KeepAlive 页面级契约语义不同，不并轨。
const showBindModal = ref(false)
const bindRemarkName = ref('')
const qrInfo = ref<QRInfo | null>(null)
const qrDataUrl = ref('')
const qrLoading = ref(false)
const qrStatusText = ref('')
const qrStatusType = ref<'wait' | 'scaned' | 'confirmed' | 'expired' | 'error' | ''>('')
let qrPollTimer: ReturnType<typeof setInterval> | null = null

// 账号重命名模态状态
const showRenameModal = ref(false)
const renameTargetId = ref('')
const renameNewVal = ref('')

// 刷新状态
const isRefreshingToken = ref(false)

// 自动平滑滚动到底部
function scrollToBottom(smooth = false) {
  nextTick(() => {
    if (messageContainer.value) {
      messageContainer.value.scrollTo({
        top: messageContainer.value.scrollHeight,
        behavior: smooth ? 'smooth' : 'auto'
      })
    }
  })
}

// 加载账号列表
async function loadAccounts(autoSelectLatest = false) {
  try {
    const list = await WechatAPI.WechatService.ListAccounts()
    accounts.value = list || []

    if (accounts.value.length > 0) {
      if (autoSelectLatest) {
        selectedAccountId.value = accounts.value[accounts.value.length - 1].id
      } else if (!selectedAccountId.value || !accounts.value.some(a => a.id === selectedAccountId.value)) {
        selectedAccountId.value = accounts.value[0].id
      }
      syncTargetUserInput()
    } else {
      selectedAccountId.value = ''
    }
  } catch (err: unknown) {
    showToast(`获取账号列表失败: ${getErrorMessage(err)}`)
  }
}

function syncTargetUserInput() {
  if (currentAccount.value) {
    toUserIdInput.value = currentAccount.value.targetUserId || currentAccount.value.ilinkUserId || ''
  }
}

function handleSelectAccount(id: string) {
  selectedAccountId.value = id
  isEditingTargetUser.value = false
  syncTargetUserInput()
  scrollToBottom()
}

// 当前账号的消息列表
const currentMessages = computed(() => {
  if (!currentAccount.value) return []
  return chatMessages.value.filter(m => m.accountId === currentAccount.value?.id)
})

// 添加一条消息并滚动
function appendMessage(msg: ChatMessage) {
  chatMessages.value.push(msg)
  if (chatMessages.value.length > 300) {
    chatMessages.value = chatMessages.value.slice(-200)
  }
  scrollToBottom(true)
}

// 扫码绑定相关
function openBindModal() {
  bindRemarkName.value = `微信助手 ${accounts.value.length + 1}`
  qrInfo.value = null
  qrDataUrl.value = ''
  qrStatusText.value = ''
  qrStatusType.value = ''
  showBindModal.value = true
  fetchQRCode()
}

function closeBindModal() {
  stopQRPoll()
  showBindModal.value = false
}

async function fetchQRCode() {
  stopQRPoll()
  qrLoading.value = true
  qrStatusText.value = '正在向微信 iLink 请求登录二维码…'
  qrStatusType.value = 'wait'
  qrInfo.value = null
  qrDataUrl.value = ''

  try {
    const res = await WechatAPI.WechatService.GetLoginQRCode()
    if (res && (res.qrcodeUrl || res.qrcode)) {
      qrInfo.value = res
      qrStatusText.value = '请使用手机微信扫码授权绑定'

      const qrContent = res.qrcodeUrl || res.qrcode
      try {
        qrDataUrl.value = await QRCode.toDataURL(qrContent, {
          width: 200,
          margin: 1,
          color: { dark: '#1f2328', light: '#ffffff' }
        })
      } catch (qrErr) {
        console.error('Render QR error:', qrErr)
      }

      startQRPoll(res.qrcode)
    } else {
      qrStatusText.value = '获取二维码失败，请重试'
      qrStatusType.value = 'error'
    }
  } catch (err: unknown) {
    qrStatusText.value = `获取失败: ${getErrorMessage(err)}`
    qrStatusType.value = 'error'
  } finally {
    qrLoading.value = false
  }
}

function startQRPoll(qrcode: string) {
  let isChecking = false
  qrPollTimer = setInterval(async () => {
    if (isChecking) return
    isChecking = true
    try {
      const remark = bindRemarkName.value.trim()
      const res = await WechatAPI.WechatService.CheckQRStatus(qrcode, remark)
      if (res) {
        if (res.status === 'scaned') {
          qrStatusText.value = '已扫码，请在手机上确认授权'
          qrStatusType.value = 'scaned'
        } else if (res.status === 'confirmed') {
          qrStatusText.value = '绑定成功！已加入列表并启动监听'
          qrStatusType.value = 'confirmed'
          stopQRPoll()

          await loadAccounts(true)
          setTimeout(() => {
            closeBindModal()
            showToast('新微信机器人绑定成功！')
            if (currentAccount.value) {
              appendMessage({
                id: `${Date.now()}-bind-ok`,
                accountId: currentAccount.value.id,
                time: new Date().toLocaleTimeString(),
                direction: 'sys',
                msgType: 'system',
                content: `🎉 账号「${currentAccount.value.remarkName}」绑定成功！请在手机微信给机器人发送任意消息以激活首条会话 Token。`
              })
            }
          }, 800)
        } else if (res.status === 'expired') {
          qrStatusText.value = '二维码已过期，正在刷新…'
          qrStatusType.value = 'expired'
          stopQRPoll()
          fetchQRCode()
        }
      }
    } catch {
      // 轮询静默
    } finally {
      isChecking = false
    }
  }, 1500)
}

function stopQRPoll() {
  if (qrPollTimer) {
    clearInterval(qrPollTimer)
    qrPollTimer = null
  }
}

// 切换当前账号监听
async function toggleAccountListener(acc: WechatAccountState) {
  try {
    if (acc.isListening) {
      await WechatAPI.WechatService.StopAccountListener(acc.id)
      acc.isListening = false
      showToast(`已停止「${acc.remarkName}」的消息监听`)
    } else {
      await WechatAPI.WechatService.StartAccountListener(acc.id)
      acc.isListening = true
      showToast(`已启动「${acc.remarkName}」的后台监听`)
    }
    await loadAccounts()
  } catch (err: unknown) {
    showToast(`切换监听失败: ${getErrorMessage(err)}`)
  }
}

// 手动刷新指定账号的 Context Token
async function refreshContextToken() {
  if (!currentAccount.value) return
  isRefreshingToken.value = true
  try {
    const token = await WechatAPI.WechatService.RefreshAccountContextToken(currentAccount.value.id)
    await loadAccounts()
    showToast('Context Token 刷新成功！会话已激活')
    appendMessage({
      id: `${Date.now()}-token-refreshed`,
      accountId: currentAccount.value.id,
      time: new Date().toLocaleTimeString(),
      direction: 'sys',
      msgType: 'system',
      content: `🔄 Context Token 刷新成功: ${token.substring(0, 16)}...`
    })
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    showToast(`刷新失败: ${errMsg}`)
    appendMessage({
      id: `${Date.now()}-token-fail`,
      accountId: currentAccount.value.id,
      time: new Date().toLocaleTimeString(),
      direction: 'sys',
      msgType: 'system',
      content: `❌ Context Token 刷新失败: ${errMsg}`
    })
  } finally {
    isRefreshingToken.value = false
  }
}

// 保存修改的目标用户 ID
async function saveTargetUserId() {
  if (!currentAccount.value) return
  try {
    await WechatAPI.WechatService.UpdateAccount(
      currentAccount.value.id,
      currentAccount.value.remarkName,
      toUserIdInput.value.trim(),
      currentAccount.value.baseUrl
    )
    isEditingTargetUser.value = false
    await loadAccounts()
    showToast('默认目标用户 ID 更新成功')
  } catch (err: unknown) {
    showToast(`更新失败: ${getErrorMessage(err)}`)
  }
}

// 打开重命名弹窗
function openRenameModal(acc: WechatAccountState) {
  renameTargetId.value = acc.id
  renameNewVal.value = acc.remarkName
  showRenameModal.value = true
}

async function saveAccountRename() {
  if (!renameTargetId.value || !renameNewVal.value.trim()) return
  try {
    const target = accounts.value.find(a => a.id === renameTargetId.value)
    if (target) {
      await WechatAPI.WechatService.UpdateAccount(
        target.id,
        renameNewVal.value.trim(),
        target.targetUserId,
        target.baseUrl
      )
      showRenameModal.value = false
      await loadAccounts()
      showToast('账号备注修改成功')
    }
  } catch (err: unknown) {
    showToast(`重命名失败: ${getErrorMessage(err)}`)
  }
}

// 删除账号——危险操作经全局 useConfirm 可访问对话框（原文案逐字进 title，tone 为设计纪律升级）
async function handleDeleteAccount(acc: WechatAccountState) {
  const accepted = await confirm({
    title: `确定要解绑并删除微信机器人「${acc.remarkName}」吗？`,
    description: '',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await WechatAPI.WechatService.DeleteAccount(acc.id)
    await loadAccounts()
    showToast(`账号「${acc.remarkName}」已删除`)
  } catch (err: unknown) {
    showToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

// 发送文字消息
async function handleSendText() {
  if (!currentAccount.value || !inputText.value.trim() || isSending.value) return
  const text = inputText.value.trim()
  const acc = currentAccount.value
  const targetUser = toUserIdInput.value.trim() || acc.targetUserId || acc.ilinkUserId

  if (!targetUser) {
    showToast('请先填写目标微信用户的 User ID')
    return
  }

  isSending.value = true
  const msgId = `${Date.now()}-out-text`
  inputText.value = ''

  appendMessage({
    id: msgId,
    accountId: acc.id,
    time: new Date().toLocaleTimeString(),
    direction: 'out',
    msgType: 'text',
    content: text,
    status: 'sending'
  })

  try {
    await WechatAPI.WechatService.SendTextMessage(acc.id, targetUser, text)
    const sent = chatMessages.value.find(m => m.id === msgId)
    if (sent) sent.status = 'sent'
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    const sent = chatMessages.value.find(m => m.id === msgId)
    if (sent) {
      sent.status = 'failed'
      sent.error = errMsg
    }
    appendMessage({
      id: `${Date.now()}-err`,
      accountId: acc.id,
      time: new Date().toLocaleTimeString(),
      direction: 'sys',
      msgType: 'system',
      content: `❌ 文字发送失败: ${errMsg}`
    })
  } finally {
    isSending.value = false
  }
}

// 发送图片消息
async function handleSendImage() {
  if (!currentAccount.value || isSending.value) return
  const acc = currentAccount.value
  const targetUser = toUserIdInput.value.trim() || acc.targetUserId || acc.ilinkUserId
  if (!targetUser) {
    showToast('请先填写目标微信用户的 User ID')
    return
  }

  try {
    const filePath = await WechatAPI.WechatService.PickImageDialog()
    if (!filePath) return

    isSending.value = true
    const msgId = `${Date.now()}-out-img`
    appendMessage({
      id: msgId,
      accountId: acc.id,
      time: new Date().toLocaleTimeString(),
      direction: 'out',
      msgType: 'image',
      content: `[图片] ${filePath}`,
      filePath,
      status: 'sending'
    })

    await WechatAPI.WechatService.SendImageMessage(acc.id, targetUser, filePath)
    const sent = chatMessages.value.find(m => m.id === msgId)
    if (sent) sent.status = 'sent'
    showToast('图片加密上传并下发成功')
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    showToast(`图片发送失败: ${errMsg}`)
    appendMessage({
      id: `${Date.now()}-err-img`,
      accountId: acc.id,
      time: new Date().toLocaleTimeString(),
      direction: 'sys',
      msgType: 'system',
      content: `❌ 图片下发失败: ${errMsg}`
    })
  } finally {
    isSending.value = false
  }
}

// 发送文件消息
async function handleSendFile() {
  if (!currentAccount.value || isSending.value) return
  const acc = currentAccount.value
  const targetUser = toUserIdInput.value.trim() || acc.targetUserId || acc.ilinkUserId
  if (!targetUser) {
    showToast('请先填写目标微信用户的 User ID')
    return
  }

  try {
    const filePath = await WechatAPI.WechatService.PickFileDialog()
    if (!filePath) return

    const fileName = filePath.split(/[/\\]/).pop() || filePath
    isSending.value = true
    const msgId = `${Date.now()}-out-file`
    appendMessage({
      id: msgId,
      accountId: acc.id,
      time: new Date().toLocaleTimeString(),
      direction: 'out',
      msgType: 'file',
      content: `[文件] ${fileName}`,
      fileName,
      filePath,
      status: 'sending'
    })

    await WechatAPI.WechatService.SendFileMessage(acc.id, targetUser, filePath)
    const sent = chatMessages.value.find(m => m.id === msgId)
    if (sent) sent.status = 'sent'
    showToast('文件加密上传并下发成功')
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    showToast(`文件发送失败: ${errMsg}`)
    appendMessage({
      id: `${Date.now()}-err-file`,
      accountId: acc.id,
      time: new Date().toLocaleTimeString(),
      direction: 'sys',
      msgType: 'system',
      content: `❌ 文件下发失败: ${errMsg}`
    })
  } finally {
    isSending.value = false
  }
}

async function handleInboundFileAction(msg: ChatMessage, action: 'open' | 'save') {
  if (!msg.attachmentId || !msg.downloadable || attachmentAction.value[msg.attachmentId]) return
  attachmentAction.value[msg.attachmentId] = action === 'open' ? 'opening' : 'saving'
  try {
    const result = action === 'open'
      ? await WechatAPI.WechatService.OpenInboundFile(msg.attachmentId)
      : await WechatAPI.WechatService.SaveInboundFile(msg.attachmentId)
    if (!result?.canceled) {
      showToast(action === 'open' ? '文件已打开' : `文件已保存到 ${result?.path || ''}`)
    }
  } catch (err: unknown) {
    showToast(`${action === 'open' ? '打开' : '保存'}文件失败: ${getErrorMessage(err)}`)
  } finally {
    attachmentAction.value[msg.attachmentId] = undefined
  }
}

// 有意保留本地实现：语义与 utils/format.fmtSize 不同（空值文案"微信接收附件"、
// 独立 B 档、KB/MB 一位小数、阈值 >=1MB），强并会改界面文案。
function formatFileSize(size?: number): string {
  if (!size || size <= 0) return '微信接收附件'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

// 键盘快捷键处理 (Enter 发送，Shift+Enter 换行)
function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    handleSendText()
  }
}

// 清屏当前会话消息
function clearCurrentChat() {
  if (!currentAccount.value) return
  chatMessages.value = chatMessages.value.filter(m => m.accountId !== currentAccount.value?.id)
  showToast('当前会话消息流已清空')
}

// 字符串生成头像颜色（美观柔和色板）
function getAvatarColor(str: string): string {
  if (!str) return '#10b981'
  const colors = [
    '#2563eb', '#059669', '#d97706', '#dc2626',
    '#7c3aed', '#0891b2', '#4f46e5', '#db2777'
  ]
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash)
  }
  return colors[Math.abs(hash) % colors.length]
}

// 事件订阅（useWailsEvent：setup 同步期注册、作用域销毁自动注销）
// 生命周期等价性：本视图常驻于 App.vue 的 KeepAlive(max=10)，原实现"onMounted 注册 +
// 仅 onUnmounted（缓存驱逐时）注销"= 停用不注销的常驻监听；useWailsEvent 的
// onScopeDispose 同样只在卸载触发 → 2 订阅/2 注销与常驻语义完全等价。
// 时序差异仅一处：注册点从"loadAccounts+离线补拉之后"提前到 setup 期（事件更早可达，
// handler 自带判空、无挂载前竞态依赖，属只赚不丢的收口）。
useWailsEvent<Record<string, string>>('wechat:context-token-updated', (payload) => {
  loadAccounts()
  if (payload?.accountId) {
    appendMessage({
      id: `${Date.now()}-ctx-upd`,
      accountId: payload.accountId,
      time: new Date().toLocaleTimeString(),
      direction: 'sys',
      msgType: 'system',
      content: `⚡ 自动捕获到最新 Context Token (用户 ${payload.fromUserId || '微信'}), 会话保持激活`
    })
  }
})

useWailsEvent<InboundMessage | undefined>('wechat:message-received', (msg) => {
  if (msg) {
    const targetAccId = msg.accountId || selectedAccountId.value || (accounts.value[0]?.id ?? '')
    let msgType: ChatMessage['msgType'] = 'text'
    let content = msg.text || ''

    if (msg.type === 2) {
      msgType = 'image'
      content = '[图片消息]'
    } else if (msg.type === 4) {
      msgType = 'file'
      content = `[文件] ${msg.fileName || '未知文件'}`
    }

    appendMessage({
      id: `${Date.now()}-${Math.random()}`,
      accountId: targetAccId,
      time: msg.time || new Date().toLocaleTimeString(),
      direction: 'in',
      msgType,
      senderName: msg.from,
      content,
      fileName: msg.fileName,
      fileSize: msg.fileSize,
      attachmentId: msg.attachmentId,
      downloadable: msg.downloadable,
      attachmentError: msg.attachmentError
    })
  }
})

onMounted(async () => {
  await loadAccounts()

  // 补取后台监听期间（前端未挂载时）积累的离线消息
  for (const acc of accounts.value) {
    try {
      const pending = await WechatAPI.WechatService.GetPendingMessages(acc.id)
      if (!pending || pending.length === 0) continue
      for (const msg of pending) {
        let msgType: ChatMessage['msgType'] = 'text'
        let content = msg.text || ''
        if (msg.type === 2) { msgType = 'image'; content = '[图片消息]' }
        else if (msg.type === 4) { msgType = 'file'; content = `[文件] ${msg.fileName || '未知文件'}` }
        chatMessages.value.push({
          id: `${msg.time}-${Math.random()}`,
          accountId: msg.accountId || acc.id,
          time: msg.time || '',
          direction: 'in',
          msgType,
          senderName: msg.from,
          content,
          fileName: msg.fileName,
          fileSize: msg.fileSize,
          attachmentId: msg.attachmentId,
          downloadable: msg.downloadable,
          attachmentError: msg.attachmentError
        })
      }
    } catch { /* 静默，不影响主流程 */ }
  }
  scrollToBottom()
})

onUnmounted(() => {
  stopQRPoll() // QR 弹窗轮询为手管定时器（见上方语义注释），卸载兜底清理
})
</script>

<template>
  <div class="wechat-page-root">
    <!-- 经典微信桌面端双栏工作台 -->
    <div class="chat-workbench">
      <!-- 1. 左侧：账号与会话管理栏 -->
      <aside class="sidebar-pane" :class="{ collapsed: isSidebarCollapsed }">
        <!-- 侧边栏头部 -->
        <div class="sidebar-header">
          <div v-if="!isSidebarCollapsed" class="sidebar-title">
            <span class="logo-emoji">🤖</span>
            <h3>微信机器人</h3>
          </div>
          <div class="sidebar-header-actions">
            <button
              v-if="!isSidebarCollapsed"
              class="btn-bind-account"
              @click="openBindModal"
              title="扫码绑定新微信账号"
            >
              <span class="plus-icon">+</span> 绑定账号
            </button>
            <button
              class="btn-toggle-sidebar"
              :title="isSidebarCollapsed ? '展开侧边栏' : '收起侧边栏'"
              @click="isSidebarCollapsed = !isSidebarCollapsed"
            >
              {{ isSidebarCollapsed ? '▶' : '◀' }}
            </button>
          </div>
        </div>

        <!-- 账号列表容器 -->
        <div class="account-list-scroll">
          <div v-if="accounts.length === 0" class="empty-accounts">
            <div class="empty-icon">📱</div>
            <div v-if="!isSidebarCollapsed" class="empty-text">暂无绑定的微信账号</div>
            <button class="btn-empty-bind" @click="openBindModal" :title="'立即扫码绑定'">
              {{ isSidebarCollapsed ? '+' : '立即扫码绑定' }}
            </button>
          </div>

          <div
            v-for="acc in accounts"
            :key="acc.id"
            class="account-item-card"
            :class="{ active: acc.id === currentAccount?.id, 'is-mini': isSidebarCollapsed }"
            :title="isSidebarCollapsed ? `${acc.remarkName} (${acc.isListening ? '监听中' : '未监听'})` : undefined"
            @click="handleSelectAccount(acc.id)"
          >
            <!-- 账号头像 (对应备注的微信号) -->
            <div class="bot-avatar" :style="{ backgroundColor: getAvatarColor(acc.remarkName || acc.id) }">
              {{ (acc.remarkName || '微').slice(0, 1) }}
              <span
                class="status-dot-badge"
                :class="{ online: acc.isListening, offline: !acc.isListening }"
                :title="acc.isListening ? '监听中' : '未监听'"
              ></span>
            </div>

            <!-- 账号信息摘要 (展开状态下显示) -->
            <template v-if="!isSidebarCollapsed">
              <div class="account-meta">
                <div class="meta-row-main">
                  <span class="bot-remark" :title="acc.remarkName">{{ acc.remarkName }}</span>
                  <span class="token-status-badge" :class="{ ready: !!acc.contextToken }">
                    {{ acc.contextToken ? '已建联' : '待激活' }}
                  </span>
                </div>
                <div class="meta-row-sub">
                  <span class="account-sub-id" :title="acc.ilinkUserId || acc.id">
                    {{ acc.ilinkUserId ? acc.ilinkUserId.split('@')[0] : acc.id }}
                  </span>
                </div>
              </div>

              <!-- 悬浮快捷操作按钮组 -->
              <div class="hover-actions-group" @click.stop>
                <button
                  class="action-icon-btn"
                  :title="acc.isListening ? '暂停监听' : '启动监听'"
                  @click="toggleAccountListener(acc)"
                >
                  {{ acc.isListening ? '⏸' : '▶' }}
                </button>
                <button class="action-icon-btn" title="重命名" @click="openRenameModal(acc)">✏️</button>
                <button class="action-icon-btn danger" title="删除账号" @click="handleDeleteAccount(acc)">🗑️</button>
              </div>
            </template>
          </div>
        </div>
      </aside>

      <!-- 2. 右侧：主聊天视口 -->
      <main class="chat-main-pane">
        <!-- 未选中任何账号占位 -->
        <div v-if="!currentAccount" class="no-selection-placeholder">
          <div class="ph-icon">💬</div>
          <h3>选择或绑定一个微信机器人</h3>
          <p>在左侧选择账号即可发起消息下发或查看会话流</p>
          <button class="btn-bind-account large" @click="openBindModal">+ 扫码绑定新微信</button>
        </div>

        <template v-else>
          <!-- 2.1 聊天窗口顶部导航栏 (Chat Header) -->
          <header class="chat-header">
            <div class="header-left">
              <button
                v-if="isSidebarCollapsed"
                class="btn-expand-header"
                title="展开侧边栏"
                @click="isSidebarCollapsed = false"
              >
                📑
              </button>
              <div class="header-avatar" :style="{ backgroundColor: getAvatarColor(currentAccount.remarkName) }">
                {{ (currentAccount.remarkName || '微').slice(0, 1) }}
              </div>
              <div class="header-info">
                <div class="header-title-line">
                  <span class="bot-name">{{ currentAccount.remarkName }}</span>
                  <span class="listen-badge" :class="{ online: currentAccount.isListening }">
                    <span class="listen-dot"></span>
                    {{ currentAccount.isListening ? '实时长轮询中' : '未开启监听' }}
                  </span>
                </div>
                <div class="header-target-bar">
                  <span class="target-label">目标用户:</span>
                  <template v-if="!isEditingTargetUser">
                    <span class="target-val" :title="toUserIdInput || '点击修改目标微信用户'">
                      {{ toUserIdInput || currentAccount.ilinkUserId || '未配置目标' }}
                    </span>
                    <button class="btn-edit-link" @click="isEditingTargetUser = true">修改</button>
                  </template>
                  <template v-else>
                    <input
                      type="text"
                      v-model="toUserIdInput"
                      class="target-input-inline"
                      placeholder="输入目标微信 User ID"
                      @keydown.enter="saveTargetUserId"
                    />
                    <button class="btn-save-inline" @click="saveTargetUserId">保存</button>
                    <button class="btn-cancel-inline" @click="isEditingTargetUser = false">取消</button>
                  </template>
                </div>
              </div>
            </div>

            <div class="header-right-actions">
              <button
                v-if="!currentAccount.isListening"
                class="btn-hdr-action"
                :disabled="isRefreshingToken"
                @click="refreshContextToken"
                title="手动请求最新 Context Token"
              >
                {{ isRefreshingToken ? '↻ 刷新中…' : '↻ 刷新凭据' }}
              </button>
              <button
                class="btn-hdr-action primary-toggle"
                :class="{ active: currentAccount.isListening }"
                @click="toggleAccountListener(currentAccount)"
              >
                {{ currentAccount.isListening ? '停止监听' : '启动监听' }}
              </button>
            </div>
          </header>

          <!-- 2.2 微信限制规则说明条 (可折叠) -->
          <div v-if="showRulesBanner" class="clawbot-rules-strip">
            <div class="rules-content">
              <span class="rules-tag">💡 微信协议规范</span>
              <span class="rule-text">① 单微信号限绑1个Bot</span>
              <span class="rule-sep">·</span>
              <span class="rule-text">② 初次需手机微信主动发任意消息建联</span>
              <span class="rule-sep">·</span>
              <span class="rule-text">③ 每下发10次或超24h需主动发消息续期会话</span>
            </div>
            <button class="btn-close-strip" @click="showRulesBanner = false" title="关闭提示">✕</button>
          </div>

          <!-- 2.3 气泡消息流区域 (Message Stream) -->
          <div ref="messageContainer" class="chat-flow-scroll">
            <div v-if="currentMessages.length === 0" class="flow-empty-box">
              <div class="empty-bubble-icon">💬</div>
              <div class="empty-title">当前会话暂无消息</div>
              <div class="empty-desc">可在下方输入文字、发送图片或文件，开始与微信端交互</div>
            </div>

            <template v-for="msg in currentMessages" :key="msg.id">
              <!-- 系统消息胶囊 -->
              <div v-if="msg.direction === 'sys'" class="bubble-row system-row">
                <div class="system-pill">
                  <span class="pill-text">{{ msg.content }}</span>
                  <span class="pill-time">{{ msg.time }}</span>
                </div>
              </div>

              <!-- 手机微信端发来的消息 (左侧灰色气泡：展示备注名与对应头像) -->
              <div v-else-if="msg.direction === 'in'" class="bubble-row inbound-row">
                <div
                  class="msg-avatar-icon peer-avatar"
                  :style="{ backgroundColor: getAvatarColor(currentAccount.remarkName) }"
                >
                  {{ (currentAccount.remarkName || '微').slice(0, 1) }}
                </div>
                <div class="bubble-column">
                  <div class="bubble-info-header">
                    <span class="sender-title">{{ currentAccount.remarkName || msg.senderName || '微信联系人' }}</span>
                    <span class="msg-timestamp">{{ msg.time }}</span>
                  </div>
                  <div class="bubble-box inbound-bubble">
                    <template v-if="msg.msgType === 'text'">
                      <p class="bubble-text">{{ msg.content }}</p>
                    </template>
                    <template v-else-if="msg.msgType === 'file'">
                      <div class="file-card-preview inbound-file-card">
                        <span class="file-icon-box">📁</span>
                        <div class="file-info-group">
                          <span class="file-main-name">{{ msg.fileName || '微信文件' }}</span>
                          <span class="file-sub-type" :title="msg.attachmentError">
                            {{ msg.downloadable ? formatFileSize(msg.fileSize) : (msg.attachmentError || '附件不可用') }}
                          </span>
                        </div>
                        <div class="file-actions">
                          <button
                            class="file-action-btn"
                            :disabled="!msg.downloadable || !msg.attachmentId || !!attachmentAction[msg.attachmentId || '']"
                            @click="handleInboundFileAction(msg, 'open')"
                          >
                            {{ attachmentAction[msg.attachmentId || ''] === 'opening' ? '打开中…' : '打开' }}
                          </button>
                          <button
                            class="file-action-btn"
                            :disabled="!msg.downloadable || !msg.attachmentId || !!attachmentAction[msg.attachmentId || '']"
                            @click="handleInboundFileAction(msg, 'save')"
                          >
                            {{ attachmentAction[msg.attachmentId || ''] === 'saving' ? '保存中…' : '保存' }}
                          </button>
                        </div>
                      </div>
                    </template>
                    <template v-else>
                      <p class="media-tip">📷 {{ msg.content }}</p>
                    </template>
                  </div>
                </div>
              </div>

              <!-- 电脑端 Hanxi 机器人下发出去的消息 (右侧微信经典绿气泡：电脑是机器人一方) -->
              <div v-else-if="msg.direction === 'out'" class="bubble-row outbound-row">
                <div class="bubble-column align-right">
                  <div class="bubble-info-header justify-end">
                    <span class="msg-timestamp">{{ msg.time }}</span>
                    <span class="sender-title robot-sender-title">🤖 Hanxi 机器人</span>
                  </div>
                  <div class="bubble-box outbound-bubble">
                    <template v-if="msg.msgType === 'text'">
                      <p class="bubble-text">{{ msg.content }}</p>
                    </template>
                    <template v-else-if="msg.msgType === 'image'">
                      <div class="media-card-preview">
                        <span class="media-icon-box">🖼️</span>
                        <div class="media-info-group">
                          <span class="media-main-name">图片消息 (AES加密下发)</span>
                          <span class="media-sub-path" :title="msg.filePath">{{ msg.filePath }}</span>
                        </div>
                      </div>
                    </template>
                    <template v-else-if="msg.msgType === 'file'">
                      <div class="file-card-preview out">
                        <span class="file-icon-box">📁</span>
                        <div class="file-info-group">
                          <span class="file-main-name">{{ msg.fileName || '文件附件' }}</span>
                          <span class="file-sub-path" :title="msg.filePath">{{ msg.filePath }}</span>
                        </div>
                      </div>
                    </template>
                  </div>
                  <div v-if="msg.status === 'sending'" class="msg-send-status sending">发送中…</div>
                  <div v-else-if="msg.status === 'failed'" class="msg-send-status failed" :title="msg.error">⚠️ 发送失败</div>
                </div>
                <div class="msg-avatar-icon my-avatar robot-avatar-box">
                  🤖
                </div>
              </div>
            </template>
          </div>

          <!-- 2.4 底部富交互输入区 (Chat Input Area) -->
          <footer class="chat-input-area">
            <!-- 顶部工具按钮条 -->
            <div class="input-toolbar-row">
              <button class="toolbar-btn" :disabled="isSending" @click="handleSendImage" title="选择本地图片并通过微信加密通道发送">
                <span class="tb-icon">🖼️</span> 发送图片
              </button>
              <button class="toolbar-btn" :disabled="isSending" @click="handleSendFile" title="选择任意本地文件并通过微信加密通道发送">
                <span class="tb-icon">📁</span> 发送文件
              </button>
              <div class="tb-spacer"></div>
              <button class="toolbar-btn text-danger" @click="clearCurrentChat" title="清空当前消息窗口">
                🧹 清屏
              </button>
            </div>

            <!-- 多行输入框 -->
            <div class="input-textarea-wrapper">
              <textarea
                v-model="inputText"
                class="wechat-textarea"
                placeholder="输入要下发给微信的消息，按 Enter 发送，Shift + Enter 换行…"
                rows="3"
                @keydown="handleKeydown"
              ></textarea>
            </div>

            <!-- 底部发送按钮栏 -->
            <div class="input-footer-row">
              <span class="shortcut-tip">按 Enter 发送 · Shift+Enter 换行</span>
              <button
                class="btn-send-message"
                :disabled="!inputText.trim() || isSending"
                @click="handleSendText"
              >
                {{ isSending ? '发送中…' : '发送 (S)' }}
              </button>
            </div>
          </footer>
        </template>
      </main>
    </div>

    <!-- 扫码绑定模态框 (QR Bind Modal) -->
    <div v-if="showBindModal" class="custom-modal-backdrop" @click.self="closeBindModal">
      <div class="custom-modal-card">
        <div class="cmodal-header">
          <div class="cmodal-title">
            <span>📱</span> 扫码绑定微信机器人
          </div>
          <button class="cmodal-close" @click="closeBindModal">✕</button>
        </div>

        <div class="cmodal-body">
          <div class="form-item-block">
            <label class="form-label">机器人备注名</label>
            <input
              type="text"
              v-model="bindRemarkName"
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
              <button class="btn-retry-qr" @click="fetchQRCode">重新获取</button>
            </div>
          </div>

          <div class="cmodal-hints">
            <div>1. 请使用待绑定的微信号扫码并授权登录；</div>
            <div>2. 授权成功后系统自动保存凭据并启动独立后台长轮询监听。</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 账号重命名模态框 (Rename Modal) -->
    <div v-if="showRenameModal" class="custom-modal-backdrop" @click.self="showRenameModal = false">
      <div class="custom-modal-card mini">
        <div class="cmodal-header">
          <div class="cmodal-title">
            <span>✏️</span> 修改账号备注
          </div>
          <button class="cmodal-close" @click="showRenameModal = false">✕</button>
        </div>
        <div class="cmodal-body">
          <div class="form-item-block">
            <label class="form-label">新备注名称</label>
            <input
              type="text"
              v-model="renameNewVal"
              placeholder="输入备注名称…"
              class="form-text-input"
              @keydown.enter="saveAccountRename"
            />
          </div>
          <div class="cmodal-actions-bar">
            <button class="btn-cancel" @click="showRenameModal = false">取消</button>
            <button class="btn-submit" @click="saveAccountRename">确定</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wechat-page-root {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 48px);
  position: relative;
  user-select: none;
}

/* 主双栏容器 */
.chat-workbench {
  display: flex;
  flex: 1;
  background: var(--surface-panel);
  border-radius: 8px;
  border: 1px solid var(--color-border);
  overflow: hidden;
  box-shadow: var(--shadow-small);
}

/* 1. 左侧账号栏 */
.sidebar-pane {
  width: 270px;
  background: var(--surface-soft);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  transition: width 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.sidebar-pane.collapsed {
  width: 64px;
}

.sidebar-header {
  padding: 12px 10px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--color-border);
  background: var(--surface-panel);
  min-height: 48px;
}

.sidebar-header-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.btn-toggle-sidebar {
  background: transparent;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  color: var(--color-text-muted);
  font-size: 11px;
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-toggle-sidebar:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.btn-expand-header {
  background: transparent;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  font-size: 13px;
  padding: 3px 6px;
  cursor: pointer;
  margin-right: 4px;
  transition: background 0.15s ease;
}

.btn-expand-header:hover {
  background: var(--surface-hover);
}

.sidebar-title {
  display: flex;
  align-items: center;
  gap: 6px;
}

.logo-emoji {
  font-size: 16px;
}

.sidebar-title h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
  margin: 0;
}

.btn-bind-account {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  transition: all 0.15s ease;
}

.btn-bind-account:hover {
  background: var(--state-positive);
}

.btn-bind-account.large {
  padding: 8px 18px;
  font-size: 13px;
  margin-top: 14px;
}

.account-list-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.empty-accounts {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 32px 8px;
  text-align: center;
  color: var(--color-text-muted);
}

.empty-icon {
  font-size: 28px;
  margin-bottom: 6px;
}

.empty-text {
  font-size: 13px;
  margin-bottom: 12px;
}

.btn-empty-bind {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 5px 14px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

.account-item-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s ease;
  background: transparent;
  position: relative;
}

.account-item-card.is-mini {
  justify-content: center;
  padding: 8px 0;
}

.account-item-card:hover {
  background: var(--surface-hover);
}

.account-item-card.active {
  background: var(--surface-hover);
}

.bot-avatar {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  color: #ffffff; /* 功能例外：数据值头像底上恒白不随主题 */
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 15px;
  position: relative;
  flex-shrink: 0;
}

.status-dot-badge {
  position: absolute;
  bottom: -2px;
  right: -2px;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 2px solid var(--surface-panel);
}

.status-dot-badge.online {
  background: var(--state-positive);
}

.status-dot-badge.offline {
  background: var(--color-text-subtle);
}

.account-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.meta-row-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
}

.bot-remark {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.token-status-badge {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
  flex-shrink: 0;
}

.token-status-badge.ready {
  background: var(--state-positive-soft);
  color: var(--state-positive);
}

.meta-row-sub {
  display: flex;
  align-items: center;
}

.account-sub-id {
  font-size: 11px;
  color: var(--color-text-subtle);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}

.hover-actions-group {
  display: none;
  align-items: center;
  gap: 2px;
}

.account-item-card:hover .hover-actions-group {
  display: flex;
}

.action-icon-btn {
  background: transparent;
  border: none;
  font-size: 12px;
  padding: 3px 5px;
  border-radius: 4px;
  cursor: pointer;
  color: var(--color-text-muted);
  transition: all 0.1s;
}

.action-icon-btn:hover {
  background: var(--surface-hover);
}

.action-icon-btn.danger:hover {
  background: var(--state-danger-soft);
  color: var(--state-danger);
}

/* 2. 右侧主聊天区域 */
.chat-main-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--surface-soft);
  position: relative;
  min-width: 0;
}

.no-selection-placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
  text-align: center;
  padding: 32px;
}

.ph-icon {
  font-size: 44px;
  margin-bottom: 12px;
  opacity: 0.7;
}

.no-selection-placeholder h3 {
  font-size: 16px;
  font-weight: 600;
  color: var(--color-text);
  margin: 0 0 6px;
}

.no-selection-placeholder p {
  font-size: 13px;
  color: var(--color-text-subtle);
  margin: 0;
}

/* 聊天顶部栏 */
.chat-header {
  height: 54px;
  padding: 0 18px;
  background: var(--surface-panel);
  border-bottom: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-avatar {
  width: 34px;
  height: 34px;
  border-radius: 6px;
  color: #ffffff; /* 功能例外：数据值头像底上恒白不随主题 */
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 14px;
  flex-shrink: 0;
}

.header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.header-title-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bot-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
}

.listen-badge {
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 10px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.listen-badge.online {
  background: var(--state-positive-soft);
  color: var(--state-positive);
}

.listen-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.header-target-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.target-label {
  color: var(--color-text-subtle);
}

.target-val {
  color: var(--color-text);
  font-family: var(--font-mono);
}

.btn-edit-link {
  background: transparent;
  border: none;
  color: var(--color-primary);
  cursor: pointer;
  padding: 0 2px;
  font-size: 11px;
}

.target-input-inline {
  padding: 2px 6px;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  font-size: 11px;
  font-family: var(--font-mono);
  width: 200px;
  outline: none;
}

.target-input-inline:focus {
  border-color: var(--color-primary);
}

.btn-save-inline {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
}

.btn-cancel-inline {
  background: var(--surface-hover);
  color: var(--color-text-muted);
  border: none;
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
}

.header-right-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-hdr-action {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  padding: 5px 12px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--color-text);
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-hdr-action:hover {
  background: var(--surface-hover);
}

.btn-hdr-action.primary-toggle.active {
  background: var(--state-positive-soft);
  border-color: var(--state-positive);
  color: var(--state-positive);
}

/* 微信规则条 */
.clawbot-rules-strip {
  background: var(--state-warning-soft);
  border-bottom: 1px solid var(--state-warning-soft);
  padding: 6px 14px;
  font-size: 12px;
  color: var(--state-warning);
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.rules-content {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.rules-tag {
  font-weight: 600;
  color: var(--state-warning);
}

.rule-sep {
  color: var(--state-warning);
}

.btn-close-strip {
  background: transparent;
  border: none;
  color: var(--state-warning);
  cursor: pointer;
  font-size: 12px;
}

/* 消息流主体 */
.chat-flow-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.flow-empty-box {
  margin: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
  text-align: center;
}

.empty-bubble-icon {
  font-size: 36px;
  margin-bottom: 6px;
  opacity: 0.5;
}

.empty-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text);
}

.empty-desc {
  font-size: 12px;
  color: var(--color-text-subtle);
  margin-top: 4px;
}

/* 消息行 */
.bubble-row {
  display: flex;
  gap: 10px;
  max-width: 80%;
}

.bubble-row.system-row {
  align-self: center;
  max-width: 90%;
  margin: 2px 0;
}

.system-pill {
  background: var(--surface-hover);
  padding: 4px 12px;
  border-radius: 12px;
  font-size: 12px;
  color: var(--color-text-muted);
  display: flex;
  align-items: center;
  gap: 6px;
}

.pill-time {
  font-size: 10px;
  color: var(--color-text-subtle);
}

.bubble-row.inbound-row {
  align-self: flex-start;
}

.bubble-row.outbound-row {
  align-self: flex-end;
  flex-direction: row;
}

.robot-sender-title {
  color: var(--color-primary);
  font-weight: 500;
}

.robot-avatar-box {
  background: var(--color-primary) !important;
  font-size: 16px;
}

.msg-avatar-icon {
  width: 34px;
  height: 34px;
  border-radius: 6px;
  color: #ffffff; /* 功能例外：数据值头像底上恒白不随主题 */
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 13px;
  flex-shrink: 0;
}

.peer-avatar {
  background: var(--color-text-muted);
}

.bubble-column {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.bubble-column.align-right {
  align-items: flex-end;
}

.bubble-info-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.bubble-info-header.justify-end {
  justify-content: flex-end;
}

.bubble-box {
  padding: 9px 13px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.5;
  word-break: break-word;
  user-select: text;
  box-shadow: var(--shadow-small);
}

.inbound-bubble {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  border-top-left-radius: 2px;
}

.outbound-bubble {
  background: #95ec69; /* 功能例外：微信品牌绿气泡 */
  color: #1a1a1a; /* 功能例外：品牌绿上深文 */
  border-top-right-radius: 2px;
}

.bubble-text {
  margin: 0;
  white-space: pre-wrap;
}

.file-card-preview {
  display: flex;
  align-items: center;
  gap: 8px;
  background: rgba(0, 0, 0, 0.04); /* 功能例外：气泡上中性深度覆层 */
  padding: 6px 10px;
  border-radius: 6px;
}

.file-card-preview.out {
  background: rgba(0, 0, 0, 0.06); /* 功能例外：同上绿气泡档 */
}

.inbound-file-card {
  min-width: 310px;
}

.file-actions {
  display: flex;
  gap: 6px;
  margin-left: auto;
}

.file-action-btn {
  border: 1px solid var(--color-border);
  background: var(--surface-panel);
  color: var(--color-text);
  border-radius: 5px;
  padding: 4px 9px;
  font-size: 11px;
  cursor: pointer;
}

.file-action-btn:hover:not(:disabled) {
  border-color: #07c160; /* 功能例外：微信品牌绿 hover */
  color: #07883f; /* 功能例外：微信品牌绿深文 */
}

.file-action-btn:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.file-icon-box {
  font-size: 22px;
}

.file-info-group {
  display: flex;
  flex-direction: column;
}

.file-main-name {
  font-weight: 600;
  font-size: 12px;
}

.file-sub-type {
  font-size: 11px;
  color: var(--color-text-muted);
}

.file-sub-path {
  font-size: 10px;
  color: var(--color-text-muted);
  font-family: var(--font-mono);
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.media-card-preview {
  display: flex;
  align-items: center;
  gap: 8px;
}

.media-icon-box {
  font-size: 20px;
}

.media-info-group {
  display: flex;
  flex-direction: column;
}

.media-main-name {
  font-weight: 600;
  font-size: 12px;
}

.media-sub-path {
  font-size: 10px;
  font-family: var(--font-mono);
  opacity: 0.8;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.media-tip {
  margin: 0;
}

.msg-send-status {
  font-size: 11px;
}

.msg-send-status.sending {
  color: var(--color-text-subtle);
}

.msg-send-status.failed {
  color: var(--state-danger);
}

/* 3. 底部输入区 */
.chat-input-area {
  height: 155px;
  background: var(--surface-panel);
  border-top: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.input-toolbar-row {
  padding: 6px 14px 2px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.toolbar-btn {
  background: transparent;
  border: none;
  font-size: 12px;
  color: var(--color-text);
  padding: 4px 8px;
  border-radius: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  transition: background 0.15s ease;
}

.toolbar-btn:hover {
  background: var(--surface-hover);
}

.toolbar-btn.text-danger:hover {
  color: var(--state-danger);
  background: var(--state-danger-soft);
}

.tb-icon {
  font-size: 13px;
}

.tb-spacer {
  flex: 1;
}

.input-textarea-wrapper {
  flex: 1;
  padding: 0 14px;
}

.wechat-textarea {
  width: 100%;
  height: 100%;
  border: none;
  outline: none;
  resize: none;
  font-size: 13px;
  color: var(--color-text);
  font-family: inherit;
  line-height: 1.5;
  background: transparent;
}

.input-footer-row {
  padding: 4px 14px 8px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.shortcut-tip {
  font-size: 11px;
  color: var(--color-text-subtle);
}

.btn-send-message {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 5px 16px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-send-message:hover {
  background: var(--state-positive);
}

.btn-send-message:disabled {
  background: var(--surface-hover);
  color: var(--color-text-subtle);
  cursor: not-allowed;
}

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
