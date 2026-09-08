// 微信机器人（WechatBot）业务状态与操作单一来源。
// Phase 6 巨型孤本结构拆分时自 WechatBotView.vue 逐字抽出：bindings 调用序列、事件名、
// toast/系统消息文案、扫码轮询手管语义、消息环形缓冲（>300 裁至 200）与离线补拉顺序均未改动。
// 纯 UI 折叠/横幅开关（isSidebarCollapsed/showRulesBanner）留在视图本体；
// getAvatarColor/formatFileSize 属呈现函数，随消费组件就近安放（getAvatarColor 跨组件共用，
// 因 §9.6 拆分边界禁止新建非 useWechatBot 前缀的 composables 文件，故以纯函数名义 export 于本模块）。
//
// 唯一非逐字适配点：原 messageContainer 元素 ref 随气泡消息流迁出至
// WechatBotChatFlow.vue，本模块改以 chatFlowRef（组件 defineExpose 的 scrollToBottom
// 句柄）触达同一滚动容器；nextTick 时序与原实现逐字等价。
//
// 必须在组件 setup 同步期内调用：内部经 useWailsEvent 注册两个事件订阅
// （wechat:context-token-updated / wechat:message-received，2 订阅/2 注销配对，勿增删改同名订阅），
// 并经 onMounted/onUnmounted 完成账号装载、离线消息补拉与 QR 轮询定时器兜底清理。
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import QRCode from 'qrcode'
import * as WechatAPI from '../../bindings/hanxi/internal/modules/wechat'
import type { WechatAccountState, QRInfo, InboundMessage } from '../../bindings/hanxi/internal/modules/wechat/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from './useToast'
import { useWailsEvent } from './useWailsEvent'
import { useConfirm } from './useConfirm'

// WechatBotChatFlow 对外暴露的最小句柄契约（替代拆分前的 messageContainer 元素 ref）。
export interface ChatFlowHandle {
  scrollToBottom(smooth?: boolean): void
}

export interface OutgoingAttachmentDraft {
  path: string
  fileName: string
  fileSize: number
  isImage: boolean
  previewUrl?: string
  temporary?: boolean
}

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
  /** 图片气泡缩略预览 Data URL（出站走本地读取，入站走 CDN 下载解密）。 */
  previewUrl?: string
  /** 预览装载状态：缺省表示非图片或尚未发起。 */
  previewState?: 'loading' | 'ready' | 'failed'
}

// 字符串生成头像颜色（美观柔和色板）——跨侧栏/聊天头部/气泡三个子组件共用的呈现函数。
export function getAvatarColor(str: string): string {
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

export function useWechatBot() {
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

  // 消息列表（按会话隔离）
  const chatMessages = ref<ChatMessage[]>([])
  const chatFlowRef = ref<ChatFlowHandle | null>(null)

  // 输入框与发送状态
  const inputText = ref('')
  const isSending = ref(false)
  const attachmentDraft = ref<OutgoingAttachmentDraft | null>(null)
  const attachmentDraftError = ref('')
  const isPreparingAttachment = ref(false)
  let attachmentSession = 0
  const attachmentAction = ref<Record<string, 'opening' | 'saving' | undefined>>({})
  const toUserIdInput = ref('')
  const isEditingTargetUser = ref(false)

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
  let qrSuccessTimer: ReturnType<typeof setTimeout> | null = null
  let qrSession = 0
  let disposed = false

  function isQRSession(session: number): boolean {
    return !disposed && showBindModal.value && session === qrSession
  }

  // 账号重命名模态状态
  const showRenameModal = ref(false)
  const renameTargetId = ref('')
  const renameNewVal = ref('')

  // 刷新状态
  const isRefreshingToken = ref(false)

  // 自动平滑滚动到底部（经 ChatFlow 组件句柄触达滚动容器，nextTick 时序逐字保留）
  function scrollToBottom(smooth = false) {
    nextTick(() => {
      chatFlowRef.value?.scrollToBottom(smooth)
    })
  }

  // 加载账号列表
  async function loadAccounts(autoSelectLatest = false, valid: () => boolean = () => !disposed) {
    try {
      const list = await WechatAPI.WechatService.ListAccounts()
      if (!valid()) return
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
      if (valid()) showToast(`获取账号列表失败: ${getErrorMessage(err)}`)
    }
  }

  function syncTargetUserInput() {
    if (currentAccount.value) {
      toUserIdInput.value = currentAccount.value.targetUserId || currentAccount.value.ilinkUserId || ''
    }
  }

  function handleSelectAccount(id: string) {
    if (id !== selectedAccountId.value) clearAttachmentDraft()
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

  // 添加一条消息并滚动。返回数组中的响应式代理引用——ref 深层包装后，直改入参原始对象
  // 不会触发视图更新；预览异步回填等后置写入必须落在本返回值上（既有发送态 status 改写
  // 经 chatMessages.value.find 取代理，同源同理）。
  function appendMessage(msg: ChatMessage): ChatMessage {
    chatMessages.value.push(msg)
    if (chatMessages.value.length > 300) {
      chatMessages.value = chatMessages.value.slice(-200)
    }
    scrollToBottom(true)
    return chatMessages.value[chatMessages.value.length - 1]
  }

  // 发起图片气泡的缩略预览装载：出站本地文件经 GetImagePreview 直读，入站附件经
  // PreviewInboundImage 走 CDN 下载解密。失败静默回退占位卡片（不打扰、不弹 toast）。
  // 注意：入参必须是 appendMessage 返回的响应式代理，否则回填不触发渲染。
  function attachImagePreview(msg: ChatMessage) {
    if (msg.msgType !== 'image') return
    if (msg.direction === 'out' && msg.filePath) {
      msg.previewState = 'loading'
      WechatAPI.WechatService.GetImagePreview(msg.filePath)
        .then((url) => {
          msg.previewUrl = url
          msg.previewState = 'ready'
        })
        .catch(() => {
          msg.previewState = 'failed'
        })
    } else if (msg.direction === 'in' && msg.attachmentId) {
      msg.previewState = 'loading'
      WechatAPI.WechatService.PreviewInboundImage(msg.attachmentId)
        .then((url) => {
          msg.previewUrl = url
          msg.previewState = 'ready'
        })
        .catch(() => {
          msg.previewState = 'failed'
        })
    }
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
    ++qrSession
    stopQRPoll()
    stopQRSuccessTimer()
    qrLoading.value = false
    showBindModal.value = false
  }

  async function fetchQRCode() {
    if (disposed || !showBindModal.value) return
    const session = ++qrSession
    stopQRPoll()
    stopQRSuccessTimer()
    qrLoading.value = true
    qrStatusText.value = '正在向微信 iLink 请求登录二维码…'
    qrStatusType.value = 'wait'
    qrInfo.value = null
    qrDataUrl.value = ''

    try {
      const res = await WechatAPI.WechatService.GetLoginQRCode()
      if (!isQRSession(session)) return
      if (res && (res.qrcodeUrl || res.qrcode)) {
        qrInfo.value = res
        qrStatusText.value = '请使用手机微信扫码授权绑定'

        const qrContent = res.qrcodeUrl || res.qrcode
        try {
          const dataUrl = await QRCode.toDataURL(qrContent, {
            width: 200,
            margin: 1,
            color: { dark: '#1f2328', light: '#ffffff' }
          })
          if (!isQRSession(session)) return
          qrDataUrl.value = dataUrl
        } catch (qrErr) {
          if (!isQRSession(session)) return
          console.error('Render QR error:', qrErr)
        }

        startQRPoll(res.qrcode, session)
      } else {
        qrStatusText.value = '获取二维码失败，请重试'
        qrStatusType.value = 'error'
      }
    } catch (err: unknown) {
      if (!isQRSession(session)) return
      qrStatusText.value = `获取失败: ${getErrorMessage(err)}`
      qrStatusType.value = 'error'
    } finally {
      if (isQRSession(session)) qrLoading.value = false
    }
  }

  function startQRPoll(qrcode: string, session: number) {
    if (!isQRSession(session)) return
    stopQRPoll()
    let isChecking = false
    qrPollTimer = setInterval(async () => {
      if (isChecking || !isQRSession(session)) return
      isChecking = true
      try {
        const remark = bindRemarkName.value.trim()
        const res = await WechatAPI.WechatService.CheckQRStatus(qrcode, remark)
        if (!isQRSession(session)) return
        if (res) {
          if (res.status === 'scaned') {
            qrStatusText.value = '已扫码，请在手机上确认授权'
            qrStatusType.value = 'scaned'
          } else if (res.status === 'confirmed') {
            qrStatusText.value = '绑定成功！已加入列表并启动监听'
            qrStatusType.value = 'confirmed'
            stopQRPoll()

            await loadAccounts(true, () => isQRSession(session))
            if (!isQRSession(session)) return
            stopQRSuccessTimer()
            qrSuccessTimer = setTimeout(() => {
              qrSuccessTimer = null
              if (!isQRSession(session)) return
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
            void fetchQRCode()
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

  function stopQRSuccessTimer() {
    if (qrSuccessTimer) {
      clearTimeout(qrSuccessTimer)
      qrSuccessTimer = null
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

  async function releaseTemporaryDraft(draft: OutgoingAttachmentDraft | null) {
    if (!draft?.temporary) return
    try {
      await WechatAPI.WechatService.ReleaseOutgoingAttachment(draft.path)
    } catch { /* 临时文件由后端 Destroy 兜底清理 */ }
  }

  function clearAttachmentDraft() {
    const previous = attachmentDraft.value
    ++attachmentSession
    attachmentDraft.value = null
    attachmentDraftError.value = ''
    isPreparingAttachment.value = false
    void releaseTemporaryDraft(previous)
  }

  async function applyAttachmentDraft(task: () => Promise<OutgoingAttachmentDraft>) {
    if (isSending.value || isPreparingAttachment.value) return
    const session = ++attachmentSession
    const accountId = currentAccount.value?.id || ''
    attachmentDraftError.value = ''
    isPreparingAttachment.value = true
    try {
      const draft = await task()
      if (session !== attachmentSession || accountId !== currentAccount.value?.id) {
        await releaseTemporaryDraft(draft)
        return
      }
      const previous = attachmentDraft.value
      attachmentDraft.value = draft
      await releaseTemporaryDraft(previous)
    } catch (err: unknown) {
      if (session === attachmentSession) attachmentDraftError.value = getErrorMessage(err)
    } finally {
      if (session === attachmentSession) isPreparingAttachment.value = false
    }
  }

  async function handleChooseAttachment() {
    await applyAttachmentDraft(async () => {
      const filePath = await WechatAPI.WechatService.PickAttachmentDialog()
      if (!filePath) throw new Error('已取消选择附件')
      return WechatAPI.WechatService.InspectOutgoingAttachment(filePath)
    })
    if (attachmentDraftError.value === '已取消选择附件') attachmentDraftError.value = ''
  }

  async function handlePasteAttachment(file: File) {
    if (file.size <= 0) {
      attachmentDraftError.value = '剪贴板附件内容为空'
      return
    }
    await applyAttachmentDraft(async () => {
      const dataURL = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result || ''))
        reader.onerror = () => reject(reader.error || new Error('读取剪贴板附件失败'))
        reader.readAsDataURL(file)
      })
      return WechatAPI.WechatService.RegisterClipboardAttachment(file.name || '剪贴板附件.bin', dataURL)
    })
  }

  async function handleSendAttachment() {
    const draft = attachmentDraft.value
    if (!currentAccount.value || !draft || isSending.value) return
    const acc = currentAccount.value
    const targetUser = toUserIdInput.value.trim() || acc.targetUserId || acc.ilinkUserId
    if (!targetUser) {
      showToast('请先填写目标微信用户的 User ID')
      return
    }

    let outgoing: ChatMessage | null = null
    isSending.value = true
    attachmentDraftError.value = ''
    attachmentDraft.value = null
    ++attachmentSession
    try {
      const msgId = `${Date.now()}-out-${draft.isImage ? 'img' : 'file'}`
      outgoing = appendMessage({
        id: msgId,
        accountId: acc.id,
        time: new Date().toLocaleTimeString(),
        direction: 'out',
        msgType: draft.isImage ? 'image' : 'file',
        content: draft.isImage ? `[图片] ${draft.fileName}` : `[文件] ${draft.fileName}`,
        fileName: draft.fileName,
        fileSize: draft.fileSize,
        filePath: draft.temporary ? undefined : draft.path,
        previewUrl: draft.isImage ? draft.previewUrl : undefined,
        previewState: draft.isImage ? 'ready' : undefined,
        status: 'sending'
      })

      if (draft.isImage) {
        await WechatAPI.WechatService.SendImageMessage(acc.id, targetUser, draft.path)
      } else {
        await WechatAPI.WechatService.SendFileMessage(acc.id, targetUser, draft.path)
      }
      outgoing.status = 'sent'
      await releaseTemporaryDraft(draft)
      showToast(`${draft.isImage ? '图片' : '文件'}加密上传并下发成功`)
    } catch (err: unknown) {
      const errMsg = getErrorMessage(err)
      if (outgoing) {
        outgoing.status = 'failed'
        outgoing.error = errMsg
      }
      attachmentDraft.value = draft
      attachmentDraftError.value = errMsg
      showToast(`${draft.isImage ? '图片' : '文件'}发送失败: ${errMsg}`)
    } finally {
      isSending.value = false
    }
  }

  async function handleInboundFileAction(msg: ChatMessage, action: 'open' | 'save') {
    if (!msg.attachmentId || !msg.downloadable || attachmentAction.value[msg.attachmentId]) return
    attachmentAction.value[msg.attachmentId] = action === 'open' ? 'opening' : 'saving'
    try {
      const isImage = msg.msgType === 'image'
      const result = action === 'open'
        ? await WechatAPI.WechatService.OpenInboundFile(msg.attachmentId)
        : await WechatAPI.WechatService.SaveInboundFile(msg.attachmentId)
      if (!result?.canceled) {
        showToast(
          action === 'open'
            ? `${isImage ? '图片' : '文件'}已打开`
            : `${isImage ? '图片' : '文件'}已保存到 ${result?.path || ''}`
        )
      }
    } catch (err: unknown) {
      showToast(`${action === 'open' ? '打开' : '保存'}文件失败: ${getErrorMessage(err)}`)
    } finally {
      attachmentAction.value[msg.attachmentId] = undefined
    }
  }

  // 出站图片气泡的本地文件动作：打开图片（系统默认查看器）/ 在资源管理器中定位所在目录。
  // 失败才弹 toast——成功时系统窗口已可见，再 toast 是噪音。
  async function handleOpenLocalImage(msg: ChatMessage) {
    if (!msg.filePath) return
    try {
      await WechatAPI.WechatService.OpenLocalImage(msg.filePath)
    } catch (err: unknown) {
      showToast(`打开图片失败: ${getErrorMessage(err)}`)
    }
  }

  async function handleRevealLocalFile(msg: ChatMessage) {
    if (!msg.filePath) return
    try {
      await WechatAPI.WechatService.RevealLocalFile(msg.filePath)
    } catch (err: unknown) {
      showToast(`打开所在目录失败: ${getErrorMessage(err)}`)
    }
  }

  // 清屏当前会话消息
  function clearCurrentChat() {
    if (!currentAccount.value) return
    chatMessages.value = chatMessages.value.filter(m => m.accountId !== currentAccount.value?.id)
    showToast('当前会话消息流已清空')
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

      const stored = appendMessage({
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
      attachImagePreview(stored)
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
          if (msgType === 'image') {
            attachImagePreview(chatMessages.value[chatMessages.value.length - 1])
          }
        }
      } catch { /* 静默，不影响主流程 */ }
    }
    scrollToBottom()
  })

  onUnmounted(() => {
    disposed = true
    clearAttachmentDraft()
    ++qrSession
    stopQRPoll()
    stopQRSuccessTimer()
  })

  return {
    accounts,
    currentAccount,
    handleSelectAccount,
    // 聊天消息流
    chatFlowRef,
    currentMessages,
    attachmentAction,
    handleInboundFileAction,
    handleOpenLocalImage,
    handleRevealLocalFile,
    inputText,
    isSending,
    attachmentDraft,
    attachmentDraftError,
    isPreparingAttachment,
    handleChooseAttachment,
    handlePasteAttachment,
    handleSendAttachment,
    clearAttachmentDraft,
    handleSendText,
    clearCurrentChat,
    // 目标用户与 Token
    toUserIdInput,
    isEditingTargetUser,
    saveTargetUserId,
    refreshContextToken,
    isRefreshingToken,
    // 账号操作
    toggleAccountListener,
    openRenameModal,
    handleDeleteAccount,
    // 扫码绑定
    showBindModal,
    bindRemarkName,
    qrLoading,
    qrDataUrl,
    qrStatusText,
    qrStatusType,
    openBindModal,
    closeBindModal,
    fetchQRCode,
    // 重命名
    showRenameModal,
    renameNewVal,
    saveAccountRename,
  }
}
