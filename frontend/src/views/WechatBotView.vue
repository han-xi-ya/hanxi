<script setup lang="ts">
import { ref, shallowRef, onMounted, onUnmounted } from 'vue'
import QRCode from 'qrcode'
import { Events } from '@wailsio/runtime'
import * as WechatAPI from '../../bindings/hubkit/internal/modules/wechat'
import type { WechatState, QRInfo, InboundMessage } from '../../bindings/hubkit/internal/modules/wechat/models'
import { getErrorMessage } from '../utils/errors'

const state = ref<WechatState>({
  isLoggedIn: false,
  botToken: '',
  ilinkBotId: '',
  ilinkUserId: '',
  contextToken: '',
  contextTokenUpdatedAt: '',
  targetUserId: '',
  isListening: false
})

// 登录二维码相关
const qrInfo = ref<QRInfo | null>(null)
const qrCanvas = ref<HTMLCanvasElement | null>(null)
const qrDataUrl = ref<string>('')
const qrLoading = ref(false)
const qrStatusText = ref('')
const qrStatusType = ref<'wait' | 'scaned' | 'confirmed' | 'expired' | 'error' | ''>('')
let qrPollTimer: ReturnType<typeof setInterval> | null = null

// 消息发送相关
const toUserId = ref('')
const textContent = ref('')
const imagePath = ref('')
const filePath = ref('')
const isSending = ref(false)
const sendNotice = ref<{ text: string; type: 'success' | 'error' | 'info' } | null>(null)

// 刷新与监听
const isRefreshingToken = ref(false)
const isTogglingListener = ref(false)

// 消息日志流
export interface MessageLogEntry {
  id: string
  time: string
  type: 'in' | 'out' | 'sys'
  text: string
  detail?: string
}

const messageLogs = shallowRef<MessageLogEntry[]>([])

let logSeq = 0
function addLog(type: 'in' | 'out' | 'sys', text: string, detail?: string) {
  const time = new Date().toLocaleTimeString()
  const entry: MessageLogEntry = { id: `${Date.now()}-${++logSeq}`, time, type, text, detail }
  messageLogs.value = [entry, ...messageLogs.value].slice(0, 100)
}

async function loadState() {
  try {
    const res = await WechatAPI.WechatService.GetState()
    if (res) {
      state.value = res
      if (res.targetUserId && !toUserId.value) {
        toUserId.value = res.targetUserId
      } else if (res.ilinkUserId && !toUserId.value) {
        toUserId.value = res.ilinkUserId
      }
    }
  } catch (err: unknown) {
    console.error('Failed to load state:', err)
  }
}

async function fetchQRCode() {
  stopQRPoll()
  qrLoading.value = true
  qrStatusText.value = '正在拉取微信二维码…'
  qrStatusType.value = 'wait'
  qrInfo.value = null
  qrDataUrl.value = ''

  try {
    const res = await WechatAPI.WechatService.GetLoginQRCode()
    if (res && (res.qrcodeUrl || res.qrcode)) {
      qrInfo.value = res
      qrStatusText.value = '请使用手机微信扫码授权'

      // 使用 QRCode 将 qrcode 字符串渲染为 base64 图片，确保免外网 CDN 或防盗链影响 100% 显示
      const qrContent = res.qrcodeUrl || res.qrcode
      try {
        qrDataUrl.value = await QRCode.toDataURL(qrContent, {
          width: 200,
          margin: 1,
          color: {
            dark: '#000000',
            light: '#ffffff'
          }
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
      const res = await WechatAPI.WechatService.CheckQRStatus(qrcode)
      if (res) {
        if (res.status === 'scaned') {
          qrStatusText.value = '已扫码，请在手机上点击确认登录'
          qrStatusType.value = 'scaned'
        } else if (res.status === 'confirmed') {
          qrStatusText.value = '登录成功！凭据已自动保存'
          qrStatusType.value = 'confirmed'
          stopQRPoll()
          await loadState()
          addLog('sys', '微信登录成功，已自动启动后台消息与 Token 监听', `Bot ID: ${res.ilinkBotId}`)
        } else if (res.status === 'expired') {
          qrStatusText.value = '二维码已过期，正在刷新…'
          qrStatusType.value = 'expired'
          stopQRPoll()
          fetchQRCode()
        }
      }
    } catch {
      // 轮询异常不打断
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

async function handleRefreshToken() {
  isRefreshingToken.value = true
  sendNotice.value = null
  try {
    const token = await WechatAPI.WechatService.RefreshContextToken()
    await loadState()
    sendNotice.value = { text: 'Context Token 刷新成功！会话已激活', type: 'success' }
    addLog('sys', 'Context Token 刷新成功', token.substring(0, 16) + '...')
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    sendNotice.value = { text: `刷新失败: ${errMsg}`, type: 'error' }
    addLog('sys', 'Context Token 刷新失败', errMsg)
  } finally {
    isRefreshingToken.value = false
  }
}

async function handleToggleListener() {
  isTogglingListener.value = true
  try {
    if (state.value.isListening) {
      await WechatAPI.WechatService.StopListener()
      addLog('sys', '已停止后台监听消息')
    } else {
      await WechatAPI.WechatService.StartListener()
      addLog('sys', '已启动后台长轮询监听')
    }
    await loadState()
  } catch (err: unknown) {
    sendNotice.value = { text: `切换监听失败: ${getErrorMessage(err)}`, type: 'error' }
  } finally {
    isTogglingListener.value = false
  }
}

async function handleSendText() {
  if (!textContent.value.trim()) return
  isSending.value = true
  sendNotice.value = null

  const target = toUserId.value.trim()
  const content = textContent.value.trim()

  try {
    await WechatAPI.WechatService.SendTextMessage(target, content)
    sendNotice.value = { text: '文字消息发送成功！', type: 'success' }
    addLog('out', `发送文字: ${content}`, `To: ${target}`)
    textContent.value = ''
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    sendNotice.value = { text: `发送失败: ${errMsg}`, type: 'error' }
    addLog('sys', `文字发送失败: ${errMsg}`)
  } finally {
    isSending.value = false
  }
}

async function handleSendImage() {
  if (!imagePath.value.trim()) return
  isSending.value = true
  sendNotice.value = null

  const target = toUserId.value.trim()
  const filePathVal = imagePath.value.trim()

  try {
    await WechatAPI.WechatService.SendImageMessage(target, filePathVal)
    sendNotice.value = { text: '图片加密上传并发送成功！', type: 'success' }
    addLog('out', `发送图片: ${filePathVal}`, `To: ${target}`)
    imagePath.value = ''
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    sendNotice.value = { text: `发送图片失败: ${errMsg}`, type: 'error' }
    addLog('sys', `图片发送失败: ${errMsg}`)
  } finally {
    isSending.value = false
  }
}

async function handleChooseImageFile() {
  try {
    const selectedPath = await WechatAPI.WechatService.PickImageDialog()
    if (selectedPath) {
      imagePath.value = selectedPath
    }
  } catch (err: unknown) {
    console.error('Pick image error:', err)
  }
}

async function handleChooseFile() {
  try {
    const selectedPath = await WechatAPI.WechatService.PickFileDialog()
    if (selectedPath) {
      filePath.value = selectedPath
    }
  } catch (err: unknown) {
    console.error('Pick file error:', err)
  }
}

async function handleSendFile() {
  if (!filePath.value.trim()) return
  isSending.value = true
  sendNotice.value = null

  const target = toUserId.value.trim()
  const targetPath = filePath.value.trim()

  try {
    await WechatAPI.WechatService.SendFileMessage(target, targetPath)
    sendNotice.value = { text: '文件加密上传并发送成功！', type: 'success' }
    addLog('out', `发送文件: ${targetPath}`, `To: ${target}`)
    filePath.value = ''
  } catch (err: unknown) {
    const errMsg = getErrorMessage(err)
    sendNotice.value = { text: `发送文件失败: ${errMsg}`, type: 'error' }
    addLog('sys', `文件发送失败: ${errMsg}`)
  } finally {
    isSending.value = false
  }
}

async function handleClearCredentials() {
  if (confirm('确定要清除已保存的微信登录凭据吗？')) {
    await WechatAPI.WechatService.ClearCredentials()
    await loadState()
    qrInfo.value = null
    qrStatusText.value = ''
    addLog('sys', '微信凭据已清除')
  }
}

let unlistenContextToken: (() => void) | null = null
let unlistenMessage: (() => void) | null = null

onMounted(async () => {
  await loadState()

  // 监听来自后端的事件
  unlistenContextToken = Events.On('wechat:context-token-updated', (ev: any) => {
    loadState()
    if (ev?.data?.contextToken) {
      addLog('sys', '接收到新 Context Token', ev.data.contextToken.substring(0, 16) + '...')
    }
  })

  unlistenMessage = Events.On('wechat:message-received', (ev: any) => {
    const msg = ev?.data as InboundMessage | undefined
    if (msg) {
      if (msg.type === 1) {
        addLog('in', `收到文字: ${msg.text}`, `From: ${msg.from}`)
      } else {
        addLog('in', `收到媒体消息 (type=${msg.type})`, `From: ${msg.from}`)
      }
    }
  })
})

onUnmounted(() => {
  stopQRPoll()
  if (unlistenContextToken) {
    unlistenContextToken()
    unlistenContextToken = null
  }
  if (unlistenMessage) {
    unlistenMessage()
    unlistenMessage = null
  }
})
</script>

<template>
  <div class="wechat-page">
    <div class="header-row">
      <div>
        <h1 class="page-title">💬 微信 ClawBot</h1>
        <p class="page-desc">基于腾讯 iLink Bot 协议的高性能微信网关，支持扫码授权、AES-128 加密图文推送与会话管理</p>
      </div>

      <div class="header-actions">
        <button
          v-if="state.isLoggedIn"
          class="btn-secondary danger"
          @click="handleClearCredentials"
        >
          退出/清除凭据
        </button>
      </div>
    </div>

    <!-- 顶部状态栏卡片 -->
    <div class="card status-overview">
      <div class="status-col">
        <span class="label">登录状态</span>
        <div class="value-badge" :class="{ success: state.isLoggedIn, offline: !state.isLoggedIn }">
          <span class="dot"></span>
          {{ state.isLoggedIn ? '已登录 iLink 微信' : '未登录' }}
        </div>
      </div>

      <div class="status-col">
        <span class="label">Bot ID / User ID</span>
        <span class="value-text monospace" :title="state.ilinkUserId">
          {{ state.ilinkBotId ? `${state.ilinkBotId} (${state.ilinkUserId.split('@')[0]})` : '无' }}
        </span>
      </div>

      <div class="status-col">
        <span class="label">会话 Context Token</span>
        <div class="token-row">
          <span class="value-text monospace" :title="state.contextToken">
            {{ state.contextToken ? `${state.contextToken.substring(0, 18)}…` : '尚未激活' }}
          </span>
          <button
            v-if="!state.isListening"
            class="btn-refresh"
            :disabled="!state.isLoggedIn || isRefreshingToken"
            @click="handleRefreshToken"
            title="拉取最新 Context Token"
          >
            {{ isRefreshingToken ? '拉取中…' : '↻ 刷新' }}
          </button>
        </div>
      </div>

      <div class="status-col">
        <span class="label">后台消息监听</span>
        <button
          class="btn-toggle"
          :class="{ active: state.isListening }"
          :disabled="!state.isLoggedIn || isTogglingListener"
          @click="handleToggleListener"
        >
          {{ state.isListening ? '● 监听中 (点击停止)' : '○ 启动监听' }}
        </button>
      </div>
    </div>

    <!-- 微信主动发消息激活引导提示 -->
    <div class="session-guide-card">
      <div class="guide-icon">💡</div>
      <div class="guide-body">
        <div class="guide-title">微信 ClawBot 协议与会话限制说明</div>
        <div class="guide-desc">
          <ul class="guide-list">
            <li><strong>绑定限制</strong>：初始化需要主动扫码绑定，且<strong>一个微信号只能绑定一个 ClawBot</strong>。</li>
            <li><strong>初次建联</strong>：绑定后需微信端<strong>主动发起一次对话</strong>（如发任意消息），才能下发消息。</li>
            <li><strong>频次限制</strong>：每下发 <strong>10 次消息</strong>后，需要微信端主动发起一次对话刷新会话。</li>
            <li><strong>有效周期</strong>：每隔 <strong>24 小时</strong>，也需要有一次主动对话以维持长效激活。</li>
          </ul>
        </div>
      </div>
    </div>

    <!-- 通知提示 -->
    <div v-if="sendNotice" class="notice-banner" :class="sendNotice.type">
      {{ sendNotice.text }}
    </div>

    <!-- 主交互区域：左侧登录与操作 / 右侧消息调试与日志 -->
    <div class="main-grid">
      <!-- 左侧：扫码登录与目标用户设置 -->
      <div class="left-pane">
        <!-- 扫码登录卡片 -->
        <div class="card login-card" v-if="!state.isLoggedIn || qrInfo">
          <div class="card-header">
            <h3>🔑 扫码登录微信机器人</h3>
            <button class="btn-primary small" :disabled="qrLoading" @click="fetchQRCode">
              {{ qrLoading ? '拉取中…' : (qrInfo ? '重新获取' : '获取登录二维码') }}
            </button>
          </div>

          <div class="qr-container">
            <div v-if="qrInfo" class="qr-box">
              <img
                v-if="qrDataUrl"
                :src="qrDataUrl"
                alt="微信登录二维码"
                class="qr-image"
              />
              <img
                v-else-if="qrInfo.qrcodeUrl"
                :src="qrInfo.qrcodeUrl"
                alt="微信登录二维码"
                class="qr-image"
              />
              <div class="qr-status-tag" :class="qrStatusType">
                {{ qrStatusText }}
              </div>
            </div>
            <div v-else class="qr-empty">
              <span class="qr-placeholder-icon">📱</span>
              <p>点击上方按钮获取登录二维码</p>
            </div>
          </div>
        </div>

        <!-- 发送消息卡片 -->
        <div class="card message-card">
          <div class="card-header">
            <h3>✉️ 发送微信消息</h3>
          </div>

          <div class="form-group">
            <label>目标微信用户 ID (To User ID)</label>
            <input
              type="text"
              v-model="toUserId"
              placeholder="例如: o9cq80181gxNW4KH9_X4EREc3-BM@im.wechat"
              class="input-text monospace"
            />
            <small class="tip">通常为你微信扫码登录后的 User ID，也可给其他已在对话列表中的微信用户发消息</small>
          </div>

          <!-- 发送文字 Tab -->
          <div class="message-section">
            <h4 class="sub-title">1. 发送文本消息</h4>
            <div class="text-send-box">
              <textarea
                v-model="textContent"
                placeholder="输入要发送给微信的文字内容 (支持换行与 Markdown / 代码片段)…"
                rows="3"
                class="input-textarea"
              ></textarea>
              <button
                class="btn-primary send-btn"
                :disabled="!state.isLoggedIn || !textContent.trim() || isSending"
                @click="handleSendText"
              >
                {{ isSending ? '发送中…' : '发送文字' }}
              </button>
            </div>
          </div>

          <!-- 发送图片 Tab -->
          <div class="message-section">
            <h4 class="sub-title">2. 发送图片消息 (AES-128-ECB 加密上传)</h4>
            <div class="image-send-box">
              <div class="file-picker-row">
                <input
                  type="text"
                  v-model="imagePath"
                  placeholder="本地图片绝对路径或点击右侧浏览选择"
                  class="input-text monospace"
                />
                <button type="button" class="btn-file-select" @click="handleChooseImageFile">
                  🖼️ 浏览…
                </button>
              </div>
              <div class="image-action-row">
                <small class="tip">微信图片媒体类型 (media_type=1)，经 AES 加密上传 Nova CDN</small>
                <button
                  class="btn-primary send-btn"
                  :disabled="!state.isLoggedIn || !imagePath.trim() || isSending"
                  @click="handleSendImage"
                >
                  {{ isSending ? '上传中…' : '加密上传并发送图片' }}
                </button>
              </div>
            </div>
          </div>

          <!-- 发送任意文件 Tab -->
          <div class="message-section">
            <h4 class="sub-title">3. 发送文件附件 (任意文档/压缩包/安装包)</h4>
            <div class="image-send-box">
              <div class="file-picker-row">
                <input
                  type="text"
                  v-model="filePath"
                  placeholder="本地文件绝对路径 (如 ZIP、PDF、TXT 等) 或点击右侧浏览选择"
                  class="input-text monospace"
                />
                <button type="button" class="btn-file-select" @click="handleChooseFile">
                  📁 浏览…
                </button>
              </div>
              <div class="image-action-row">
                <small class="tip">微信文件媒体类型 (media_type=3)，支持任意格式文件加密上传</small>
                <button
                  class="btn-primary send-btn"
                  :disabled="!state.isLoggedIn || !filePath.trim() || isSending"
                  @click="handleSendFile"
                >
                  {{ isSending ? '上传中…' : '加密上传并发送文件' }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 右侧：实时日志与事件流 -->
      <div class="right-pane">
        <div class="card log-card">
          <div class="card-header">
            <h3>📜 实时消息与事件流</h3>
            <button class="btn-clear" @click="messageLogs = []">清空</button>
          </div>

          <div class="log-stream">
            <div v-if="messageLogs.length === 0" class="log-empty">
              <span>暂无消息或事件记录</span>
            </div>

            <div
              v-for="log in messageLogs"
              :key="log.id"
              class="log-item"
              :class="log.type"
            >
              <div class="log-meta">
                <span class="log-time">{{ log.time }}</span>
                <span class="log-badge" :class="log.type">
                  {{ log.type === 'in' ? '接收' : (log.type === 'out' ? '发出' : '系统') }}
                </span>
              </div>
              <div class="log-content">
                <span class="log-text">{{ log.text }}</span>
                <span v-if="log.detail" class="log-detail">{{ log.detail }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wechat-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 1200px;
  margin: 0 auto;
}

.header-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.page-title {
  font-size: 20px;
  font-weight: 600;
  margin: 0 0 4px;
  color: var(--text-main);
}

.page-desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
}

.card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 16px 20px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.02);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.card-header h3 {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
  color: var(--text-main);
}

/* 顶部状态总览 */
.status-overview {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  padding: 12px 18px;
  background: #ffffff;
}

.status-col {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.status-col .label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-subtle);
  text-transform: uppercase;
}

.value-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 12px;
  width: fit-content;
}

.value-badge.success {
  background: rgba(26, 127, 55, 0.1);
  color: var(--success);
}

.value-badge.offline {
  background: rgba(140, 149, 159, 0.15);
  color: var(--text-muted);
}

.value-badge .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.value-text {
  font-size: 12px;
  color: var(--text-main);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.monospace {
  font-family: 'Consolas', 'Menlo', 'Monaco', monospace;
}

.token-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-refresh {
  padding: 2px 6px;
  font-size: 11px;
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  color: var(--accent);
  cursor: pointer;
}

.btn-toggle {
  padding: 4px 10px;
  font-size: 12px;
  font-weight: 500;
  border-radius: 4px;
  border: 1px solid var(--border-color);
  background: var(--bg-hover);
  color: var(--text-muted);
  cursor: pointer;
  width: fit-content;
}

.btn-toggle.active {
  background: rgba(47, 111, 237, 0.1);
  color: var(--accent);
  border-color: rgba(47, 111, 237, 0.3);
}

/* 主网格 */
.main-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  align-items: start;
}

.left-pane, .right-pane {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 扫码卡片 */
.qr-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 180px;
  background: var(--bg-app);
  border-radius: 6px;
  padding: 16px;
}

.qr-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.qr-image {
  width: 160px;
  height: 160px;
  border-radius: 6px;
  border: 1px solid var(--border-color);
  background: #ffffff;
  padding: 6px;
}

.qr-status-tag {
  font-size: 12px;
  font-weight: 500;
  padding: 4px 10px;
  border-radius: 12px;
  background: #eef2ff;
  color: var(--accent);
}

.qr-status-tag.scaned {
  background: #fff8e6;
  color: #b45309;
}

.qr-status-tag.confirmed {
  background: #def7ec;
  color: #03543f;
}

.qr-status-tag.expired, .qr-status-tag.error {
  background: #fde8e8;
  color: #9b1c1c;
}

.qr-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: var(--text-subtle);
  font-size: 12px;
}

.qr-placeholder-icon {
  font-size: 32px;
  opacity: 0.5;
}

/* 消息发送表单 */
.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 14px;
}

.form-group label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
}

.tip {
  font-size: 11px;
  color: var(--text-subtle);
}

.input-text, .input-textarea {
  width: 100%;
  box-sizing: border-box;
  padding: 8px 10px;
  font-size: 12px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: #ffffff;
  color: var(--text-main);
  outline: none;
}

.input-text:focus, .input-textarea:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px rgba(47, 111, 237, 0.15);
}

.sub-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  margin: 0 0 8px;
}

.message-section {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border-color);
}

.text-send-box {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.send-btn {
  align-self: flex-end;
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 500;
  background: var(--accent);
  color: #ffffff;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s ease;
}

.send-btn:hover:not(:disabled) {
  background: var(--accent-hover);
}

.send-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.file-picker-row {
  display: flex;
  gap: 8px;
}

.btn-file-select {
  flex: 0 0 auto;
  padding: 8px 12px;
  font-size: 12px;
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
}

.image-action-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
}

/* 按钮通用 */
.btn-primary {
  background: var(--accent);
  color: #ffffff;
  border: none;
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

.btn-primary.small {
  padding: 4px 8px;
  font-size: 11px;
}

.btn-secondary {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 12px;
  color: var(--text-muted);
  cursor: pointer;
}

.btn-secondary.danger {
  color: var(--danger);
}

.btn-clear {
  background: transparent;
  border: none;
  font-size: 11px;
  color: var(--text-subtle);
  cursor: pointer;
}

.btn-clear:hover {
  color: var(--danger);
}

/* 日志面板 */
.log-card {
  height: 520px;
  display: flex;
  flex-direction: column;
}

.log-stream {
  flex: 1;
  overflow-y: auto;
  background: #1e1e1e;
  border-radius: 6px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.log-empty {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
  color: #6e7681;
  font-size: 12px;
}

.log-item {
  font-size: 11px;
  font-family: 'Consolas', 'Menlo', 'Monaco', monospace;
  padding: 4px 6px;
  border-radius: 4px;
  background: #252526;
  border-left: 3px solid #6e7681;
}

.log-item.in {
  border-left-color: #3fb950;
}

.log-item.out {
  border-left-color: #58a6ff;
}

.log-item.sys {
  border-left-color: #d29922;
}

.log-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 2px;
}

.log-time {
  color: #8b949e;
}

.log-badge {
  font-size: 10px;
  padding: 1px 4px;
  border-radius: 3px;
  color: #ffffff;
  background: #484f58;
}

.log-badge.in {
  background: #238636;
}

.log-badge.out {
  background: #1f6feb;
}

.log-badge.sys {
  background: #9e6a03;
}

.log-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.log-text {
  color: #e6edf3;
  word-break: break-all;
}

.log-detail {
  color: #8b949e;
  font-size: 10px;
}

/* 会话激活提示卡片 */
.session-guide-card {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 12px 16px;
  background: #eff6ff;
  border: 1px solid #bfdbfe;
  border-radius: 8px;
}

.guide-icon {
  font-size: 20px;
  line-height: 1;
  flex-shrink: 0;
}

.guide-body {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.guide-title {
  font-size: 13px;
  font-weight: 600;
  color: #1e40af;
}

.guide-desc {
  font-size: 12px;
  line-height: 1.5;
  color: #1e3a8a;
}

.guide-list {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.guide-list li {
  line-height: 1.6;
}

.guide-desc strong {
  color: #1d4ed8;
}

/* 通知条 */
.notice-banner {
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
}

.notice-banner.success {
  background: #def7ec;
  color: #03543f;
  border: 1px solid #bcf0da;
}

.notice-banner.error {
  background: #fde8e8;
  color: #9b1c1c;
  border: 1px solid #fbd5d5;
}
</style>
