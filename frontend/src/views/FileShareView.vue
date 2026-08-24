<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { Events } from '@wailsio/runtime'
import QRCode from 'qrcode'
import * as FileShareAPI from '../../bindings/hubkit/internal/modules/fileshare'
import type {
  ShareConfig,
  ServerStatus,
  NetworkEndpoint,
  DropItem,
  TransferEvent
} from '../../bindings/hubkit/internal/modules/fileshare/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const { showToast } = useToast()

// 状态定义
const status = ref<ServerStatus>({
  isRunning: false,
  port: 80,
  sharePath: '',
  activeConnections: 0,
  uploadCount: 0,
  downloadCount: 0,
  uploadBytes: 0,
  downloadBytes: 0,
  uploadRate: 0,
  downloadRate: 0,
  allowUpload: true,
  allowTextDrop: true,
  autoSaveToMemo: true,
  activeUrls: [],
  startedAt: '',
})

const configForm = ref<ShareConfig>({
  port: 80,
  sharePath: '',
  allowUpload: true,
  allowTextDrop: true,
  autoSaveToMemo: true,
  maxUploadSizeMB: 1024,
  authToken: '',
})

const endpoints = shallowRef<NetworkEndpoint[]>([])
const dropInbox = shallowRef<DropItem[]>([])
const transferLogs = ref<TransferEvent[]>([])
const loading = ref(false)
const errorMsg = ref('')
const activeTab = ref<'endpoints' | 'inbox' | 'logs'>('endpoints')

// 二维码 SVG 缓存 Map (url -> svg)
const qrMap = ref<Record<string, string>>({})

let unlistenStatus: (() => void) | null = null
let unlistenTransfer: (() => void) | null = null
let unlistenDrop: (() => void) | null = null
let pollTimer: number | null = null

async function loadData() {
  try {
    const [srvStatus, cfg, eps, inbox] = await Promise.all([
      FileShareAPI.FileShareService.GetServerStatus(),
      FileShareAPI.FileShareService.GetConfig(),
      FileShareAPI.FileShareService.GetNetworkEndpoints(),
      FileShareAPI.FileShareService.GetDropInbox(),
    ])

    status.value = srvStatus
    configForm.value = { ...cfg }
    endpoints.value = eps ?? []
    dropInbox.value = inbox ?? []

    await generateQRCodes()
  } catch (err: unknown) {
    errorMsg.value = `加载文件快传数据失败: ${getErrorMessage(err)}`
  }
}

async function generateQRCodes() {
  const map: Record<string, string> = {}
  for (const ep of endpoints.value) {
    try {
      map[ep.url] = await QRCode.toString(ep.url, {
        type: 'svg',
        margin: 1,
        width: 140,
        color: {
          dark: '#1e293b',
          light: '#ffffff',
        },
      })
    } catch (e) {
      console.warn('QR code generate error:', e)
    }
  }
  qrMap.value = map
}

async function handleToggleServer() {
  loading.value = true
  errorMsg.value = ''
  try {
    if (status.value.isRunning) {
      await FileShareAPI.FileShareService.StopServer()
      status.value.isRunning = false
      showToast('局域网文件快传服务已停止')
    } else {
      // 先保存最新配置再启动
      await FileShareAPI.FileShareService.SaveConfig(configForm.value)
      const res = await FileShareAPI.FileShareService.StartServer()
      status.value = res
      showToast(`局域网快传服务已成功启动于端口 :${res.port}`)
    }
    await loadData()
  } catch (err: unknown) {
    errorMsg.value = `服务操作失败: ${getErrorMessage(err)}`
    showToast(`操作失败: ${getErrorMessage(err)}`)
  } finally {
    loading.value = false
  }
}

async function handleSaveConfig() {
  try {
    await FileShareAPI.FileShareService.SaveConfig(configForm.value)
    showToast('快传共享设置已保存')
    if (status.value.isRunning) {
      showToast('设置已动态热应用到运行中的快传服务')
    }
  } catch (err: unknown) {
    showToast(`保存失败: ${getErrorMessage(err)}`)
  }
}

async function handleChooseDirectory() {
  try {
    const selected = await FileShareAPI.FileShareService.ChooseDirectory()
    if (selected) {
      configForm.value.sharePath = selected
      showToast(`已选择共享目录: ${selected}`)
      // 若服务已在运行，自动热同步生效
      if (status.value.isRunning) {
        await handleSaveConfig()
      }
    }
  } catch (err: unknown) {
    showToast(`选择目录失败: ${getErrorMessage(err)}`)
  }
}

function setQuickPort(port: number) {
  configForm.value.port = port
}

function copyToClipboard(text: string, tip = '已复制访问链接') {
  navigator.clipboard.writeText(text).then(() => {
    showToast(tip)
  }).catch(() => {
    showToast('复制到剪贴板失败')
  })
}

async function handleDeleteDrop(id: string) {
  try {
    await FileShareAPI.FileShareService.DeleteDropItem(id)
    dropInbox.value = dropInbox.value.filter(item => item.id !== id)
    showToast('已删除投递内容')
  } catch (err: unknown) {
    showToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

async function handleClearInbox() {
  if (dropInbox.value.length === 0) return
  try {
    await FileShareAPI.FileShareService.ClearDropInbox()
    dropInbox.value = []
    showToast('投递箱已清空')
  } catch (err: unknown) {
    showToast(`清空失败: ${getErrorMessage(err)}`)
  }
}

onMounted(async () => {
  await loadData()

  unlistenStatus = Events.On('fileshare:status', (ev: any) => {
    const s: ServerStatus = ev.data || ev
    status.value = s
  })

  unlistenTransfer = Events.On('fileshare:transfer', (ev: any) => {
    const e: TransferEvent = ev.data || ev
    transferLogs.value = [e, ...transferLogs.value.slice(0, 49)]
  })

  unlistenDrop = Events.On('fileshare:text-dropped', (ev: any) => {
    const item: DropItem = ev.data || ev
    dropInbox.value = [item, ...dropInbox.value]
    showToast(`收到来自移动端的文本投递: ${item.content.slice(0, 24)}...`)
  })

  // 每 2 秒轮询服务状态，保证活跃连接与实时速率持续刷新
  pollTimer = window.setInterval(async () => {
    if (!status.value.isRunning) return
    try {
      status.value = await FileShareAPI.FileShareService.GetServerStatus()
    } catch {
      /* 状态获取失败静默，等待下次轮询 */
    }
  }, 2000)
})

onUnmounted(() => {
  if (pollTimer) window.clearInterval(pollTimer)
  if (unlistenStatus) unlistenStatus()
  if (unlistenTransfer) unlistenTransfer()
  if (unlistenDrop) unlistenDrop()
})

// 字节格式化: 1536 -> "1.5 KB"
function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const v = bytes / Math.pow(1024, i)
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + ' ' + units[i]
}

// 速率格式化: 1.5 KB/s
function formatSpeed(bps: number): string {
  return formatBytes(bps) + '/s'
}
</script>

<template>
  <div class="page fileshare-page">
    <!-- 页面顶栏 -->
    <header class="page-header">
      <div class="header-left">
        <h1 class="page-title">
          <span class="title-icon">📁</span>
          局域网文件快传
        </h1>
        <p class="page-desc">
          零客户端依赖的局域网极速文件/文本分享站。手机电脑同一 Wi-Fi 扫码即用，支持断点续传与大文件流式拖拽上传。
        </p>
      </div>
      <div class="header-actions">
        <button
          class="btn-primary"
          :class="{ 'btn-danger': status.isRunning }"
          :disabled="loading"
          @click="handleToggleServer"
        >
          <span v-if="loading">处理中...</span>
          <span v-else-if="status.isRunning">⏹ 停止快传服务</span>
          <span v-else>▶ 启动快传服务</span>
        </button>
      </div>
    </header>

    <div v-if="errorMsg" class="alert-banner error mb-4">
      <span>⚠️ {{ errorMsg }}</span>
      <button class="btn-text" @click="errorMsg = ''">✕</button>
    </div>

    <!-- 核心概览卡片区 -->
    <div class="stats-grid mb-6">
      <div class="stat-card">
        <div class="stat-label">服务运行状态</div>
        <div class="stat-val flex-center gap-2">
          <span class="status-indicator" :class="{ online: status.isRunning }"></span>
          <span :class="status.isRunning ? 'text-success' : 'text-muted'">
            {{ status.isRunning ? '运行中' : '已停止' }}
          </span>
        </div>
        <div class="stat-sub">绑定端口 :{{ status.port || configForm.port }}</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">实时传输速率</div>
        <div class="stat-val font-mono text-sm">
          <span class="text-upload">↑ {{ formatSpeed(status.uploadRate) }}</span>
          <span class="text-divider">/</span>
          <span class="text-download">↓ {{ formatSpeed(status.downloadRate) }}</span>
        </div>
        <div class="stat-sub">当前活跃连接 {{ status.activeConnections }} 个</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">累计传输统计</div>
        <div class="stat-val font-mono text-sm">
          <span class="text-upload">↑ {{ formatBytes(status.uploadBytes) }}</span>
          <span class="text-divider">/</span>
          <span class="text-download">↓ {{ formatBytes(status.downloadBytes) }}</span>
        </div>
        <div class="stat-sub">接收上传 {{ status.uploadCount }} 个 / 提供下载 {{ status.downloadCount }} 个文件</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">跨端文本投递箱</div>
        <div class="stat-val font-mono">{{ dropInbox.length }} <span class="unit">条</span></div>
        <div class="stat-sub">手机与外设投递碎片内容</div>
      </div>
    </div>

    <!-- 配置与权限控制栏 -->
    <div class="card config-card mb-6">
      <div class="card-header flex-between">
        <h3 class="card-title">⚙ 共享服务参数与权限控制</h3>
        <button class="btn-secondary btn-sm" @click="handleSaveConfig">保存共享规则</button>
      </div>
      <div class="config-form-grid">
        <div class="form-group">
          <label>共享根目录 (PC 本地物理路径):</label>
          <div class="path-input-group">
            <input
              v-model="configForm.sharePath"
              type="text"
              class="input-control font-mono flex-1"
              placeholder="请选择或输入要共享的物理文件夹路径..."
            />
            <button class="btn-secondary btn-choose-dir" @click="handleChooseDirectory">
              📂 浏览 / 选择目录
            </button>
          </div>
          <span class="form-hint">安全沙箱已开启：外部访客无法越界访问此目录以外的磁盘文件。</span>
        </div>

        <div class="form-group">
          <div class="flex-between">
            <label>监听端口 (Port):</label>
            <div class="quick-ports flex gap-1">
              <span class="quick-port-tip text-muted text-xs">快捷:</span>
              <button
                type="button"
                class="quick-port-chip"
                :class="{ active: configForm.port === 80 }"
                @click="setQuickPort(80)"
              >
                80 (默认)
              </button>
              <button
                type="button"
                class="quick-port-chip"
                :class="{ active: configForm.port === 8080 }"
                @click="setQuickPort(8080)"
              >
                8080
              </button>
              <button
                type="button"
                class="quick-port-chip"
                :class="{ active: configForm.port === 8888 }"
                @click="setQuickPort(8888)"
              >
                8888
              </button>
            </div>
          </div>
          <input
            v-model.number="configForm.port"
            type="number"
            class="input-control font-mono"
            min="1"
            max="65535"
            placeholder="80"
          />
          <span class="form-hint">默认为 80（移动端浏览器直接输 IP 即可访问，免输端口）。</span>
        </div>

        <div class="form-switches-group">
          <label class="checkbox-label">
            <input v-model="configForm.allowUpload" type="checkbox" />
            <span>允许局域网客户端上传文件 (支持大文件分块流式上传)</span>
          </label>
          <label class="checkbox-label">
            <input v-model="configForm.allowTextDrop" type="checkbox" />
            <span>允许文本/链接极速投递 (手机端直接投递到 PC 屏幕)</span>
          </label>
          <label class="checkbox-label">
            <input v-model="configForm.autoSaveToMemo" type="checkbox" />
            <span>自动将移动端投递的文本同步存入【极客随手记】</span>
          </label>
        </div>
      </div>
    </div>

    <!-- 选项卡导航 -->
    <div class="tab-nav mb-4">
      <button
        class="tab-item"
        :class="{ active: activeTab === 'endpoints' }"
        @click="activeTab = 'endpoints'"
      >
        📱 局域网扫码与访问接入点 ({{ endpoints.length }})
      </button>
      <button
        class="tab-item"
        :class="{ active: activeTab === 'inbox' }"
        @click="activeTab = 'inbox'"
      >
        📥 跨端投递箱 ({{ dropInbox.length }})
      </button>
      <button
        class="tab-item"
        :class="{ active: activeTab === 'logs' }"
        @click="activeTab = 'logs'"
      >
        📋 实时传输审计 ({{ transferLogs.length }})
      </button>
    </div>

    <!-- 接入点与二维码卡片 -->
    <div v-if="activeTab === 'endpoints'" class="tab-content">
      <div v-if="!status.isRunning" class="empty-state py-8">
        <div class="empty-icon">⏹</div>
        <h3>快传服务当前处于停止状态</h3>
        <p>点击右上角的「▶ 启动快传服务」后即可生成各局域网网卡的访问地址与专属二维码。</p>
      </div>

      <div v-else class="endpoint-grid">
        <div v-for="ep in endpoints" :key="ep.url" class="card endpoint-card">
          <div class="card-header flex-between">
            <div class="ep-name font-bold">
              {{ ep.interfaceName }}
              <span v-if="ep.isDefault" class="tag-pill tag-primary ml-2">主通信网卡</span>
            </div>
            <span class="ep-ip font-mono text-muted">{{ ep.ip }}</span>
          </div>

          <div class="ep-body flex-between">
            <div class="ep-qr" v-html="qrMap[ep.url] || '生成中...'"></div>
            <div class="ep-info">
              <div class="ep-url-box">
                <span class="url-text font-mono">{{ ep.url }}</span>
              </div>
              <p class="ep-tip">
                📱 手机/平板连接同局域网后，打开相机或浏览器扫码秒开共享站。
              </p>
              <div class="ep-actions flex gap-2">
                <button class="btn-secondary btn-sm" @click="copyToClipboard(ep.url)">
                  📋 复制访问链接
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 跨端投递箱 -->
    <div v-if="activeTab === 'inbox'" class="tab-content">
      <div class="card">
        <div class="card-header flex-between">
          <h3 class="card-title">📱 手机投递文本与链接记录</h3>
          <button
            v-if="dropInbox.length > 0"
            class="btn-secondary btn-sm"
            @click="handleClearInbox"
          >
            🗑️ 清空投递箱
          </button>
        </div>
        <div v-if="dropInbox.length === 0" class="empty-state py-8">
          <div class="empty-icon">📭</div>
          <h3>暂无投递内容</h3>
          <p>在手机扫码打开的网页中输入任意文本或链接，点击「立即投递」即可秒级传送到此处。</p>
        </div>
        <div v-else class="inbox-list">
          <div v-for="item in dropInbox" :key="item.id" class="inbox-item">
            <div class="inbox-main">
              <div class="inbox-header flex-between mb-1">
                <div class="inbox-source text-muted">
                  来自 {{ item.senderIp }}
                  <span v-if="item.isUrl" class="tag-pill tag-blue ml-2">🌐 网址链接</span>
                  <span v-else class="tag-pill ml-2">📝 纯文本</span>
                </div>
                <div class="inbox-time font-mono text-muted text-sm">
                  {{ new Date(item.createdAt).toLocaleTimeString() }}
                </div>
              </div>
              <div class="inbox-content font-mono select-all">
                {{ item.content }}
              </div>
            </div>
            <div class="inbox-item-actions flex gap-2">
              <button
                class="btn-secondary btn-sm"
                @click="copyToClipboard(item.content, '已复制投递内容')"
              >
                📋 复制
              </button>
              <button
                class="btn-secondary btn-sm text-danger"
                @click="handleDeleteDrop(item.id)"
              >
                🗑️ 删除
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 实时传输审计日志 -->
    <div v-if="activeTab === 'logs'" class="tab-content">
      <div class="card">
        <div class="card-header flex-between">
          <h3 class="card-title">📋 局域网访问与传输审计</h3>
          <span class="text-muted text-sm font-mono">保留最新 50 条</span>
        </div>
        <div v-if="transferLogs.length === 0" class="empty-state py-8">
          <div class="empty-icon">📜</div>
          <h3>暂无传输事件</h3>
          <p>客户端下载或上传文件时，此处将实时展示 IP、文件名、传输大小与状态。</p>
        </div>
        <div v-else class="table-responsive">
          <table class="table">
            <thead>
              <tr>
                <th>时间</th>
                <th>类型</th>
                <th>文件名 / 内容</th>
                <th>大小</th>
                <th>客户端 IP</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(log, idx) in transferLogs" :key="idx">
                <td class="font-mono text-xs">{{ new Date(log.timestamp).toLocaleTimeString() }}</td>
                <td>
                  <span
                    class="tag-pill"
                    :class="{
                      'tag-success': log.type === 'upload',
                      'tag-blue': log.type === 'download',
                      'tag-amber': log.type === 'drop',
                    }"
                  >
                    {{ log.type === 'upload' ? '↑ 上传' : (log.type === 'download' ? '↓ 下载' : '投递') }}
                  </span>
                </td>
                <td class="font-mono text-sm max-w-xs truncate" :title="log.filename">
                  {{ log.filename }}
                </td>
                <td class="font-mono text-xs">{{ log.size > 0 ? formatBytes(log.size) : '-' }}</td>
                <td class="font-mono text-xs">{{ log.clientIp }}</td>
                <td>
                  <span :class="log.success ? 'text-success' : 'text-danger'">
                    {{ log.success ? '✓ 成功' : '✕ 失败' }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.fileshare-page {
  padding: 24px 32px;
  max-width: 1360px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
}

.page-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--color-text-primary, #0f172a);
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.page-desc {
  font-size: 13px;
  color: var(--color-text-secondary, #64748b);
  max-width: 720px;
  line-height: 1.5;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.stat-card {
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 10px;
  padding: 16px;
}

.stat-label {
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
  margin-bottom: 6px;
}

.stat-val {
  font-size: 20px;
  font-weight: 700;
  color: var(--color-text-primary, #0f172a);
  margin-bottom: 4px;
}

.stat-val.text-sm {
  font-size: 15px;
}

.stat-val .unit {
  font-size: 13px;
  font-weight: normal;
  color: var(--color-text-secondary, #64748b);
}

.stat-sub {
  font-size: 11px;
  color: var(--color-text-muted, #94a3b8);
}

.text-upload {
  color: #10b981;
}

.text-download {
  color: #3b82f6;
}

.text-divider {
  color: var(--color-border, #cbd5e1);
  margin: 0 4px;
}

.status-indicator {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #ef4444;
}

.status-indicator.online {
  background: #10b981;
  box-shadow: 0 0 8px rgba(16, 185, 129, 0.4);
}

.config-card {
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 10px;
  padding: 16px 20px;
}

.config-form-grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 16px;
  margin-top: 14px;
}

.form-switches-group {
  grid-column: span 2;
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  padding-top: 8px;
  border-top: 1px dashed var(--color-border, #e2e8f0);
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-group label {
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text-primary, #1e293b);
}

.path-input-group {
  display: flex;
  gap: 8px;
  align-items: center;
}

.btn-choose-dir {
  white-space: nowrap;
  padding: 8px 14px;
  font-size: 12px;
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 4px;
}

.quick-ports {
  align-items: center;
}

.quick-port-chip {
  background: var(--color-bg-input, #f1f5f9);
  border: 1px solid var(--color-border, #cbd5e1);
  color: var(--color-text-secondary, #475569);
  font-size: 11px;
  font-family: monospace;
  padding: 1px 6px;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.15s;
}

.quick-port-chip:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.quick-port-chip.active {
  background: #2563eb;
  color: #ffffff;
  border-color: #2563eb;
}

.input-control {
  background: var(--color-bg-input, #f8fafc);
  border: 1px solid var(--color-border, #cbd5e1);
  border-radius: 6px;
  padding: 8px 12px;
  font-size: 13px;
  color: var(--color-text-primary, #0f172a);
  outline: none;
  transition: border-color 0.2s;
}

.input-control:focus {
  border-color: #3b82f6;
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.15);
}

.form-hint {
  font-size: 11px;
  color: var(--color-text-muted, #94a3b8);
}

.checkbox-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--color-text-primary, #1e293b);
  cursor: pointer;
}

.tab-nav {
  display: flex;
  gap: 8px;
  border-bottom: 1px solid var(--color-border, #e2e8f0);
  padding-bottom: 8px;
}

.tab-item {
  background: none;
  border: none;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text-secondary, #64748b);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.tab-item:hover {
  background: var(--color-bg-hover, #f1f5f9);
  color: var(--color-text-primary, #0f172a);
}

.tab-item.active {
  background: #3b82f6;
  color: #ffffff;
}

.endpoint-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(420px, 1fr));
  gap: 16px;
}

.endpoint-card {
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 10px;
  padding: 16px;
}

.ep-body {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-top: 12px;
}

.ep-qr {
  background: #ffffff;
  padding: 8px;
  border-radius: 8px;
  border: 1px solid var(--color-border, #e2e8f0);
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 140px;
  height: 140px;
}

.ep-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ep-url-box {
  background: var(--color-bg-input, #f8fafc);
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid var(--color-border, #cbd5e1);
  word-break: break-all;
}

.url-text {
  font-size: 13px;
  font-weight: 600;
  color: #2563eb;
}

.ep-tip {
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
  line-height: 1.4;
}

.inbox-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 12px;
}

.inbox-item {
  background: var(--color-bg-input, #f8fafc);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 8px;
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.inbox-main {
  flex: 1;
}

.inbox-content {
  background: #ffffff;
  padding: 8px 12px;
  border-radius: 6px;
  border: 1px solid var(--color-border, #e2e8f0);
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 140px;
  overflow-y: auto;
}

.table {
  width: 100%;
  border-collapse: collapse;
}

.table th,
.table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid var(--color-border, #e2e8f0);
}

.table th {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-secondary, #64748b);
  background: var(--color-bg-input, #f8fafc);
}

.flex-between { display: flex; justify-content: space-between; align-items: center; }
.flex-center { display: flex; align-items: center; }
.gap-1 { gap: 4px; }
.gap-2 { gap: 8px; }
.gap-3 { gap: 12px; }
.ml-2 { margin-left: 8px; }
.mb-1 { margin-bottom: 4px; }
.mb-4 { margin-bottom: 16px; }
.mb-6 { margin-bottom: 24px; }
.py-8 { padding-top: 32px; padding-bottom: 32px; }

.btn-primary {
  background: #2563eb;
  color: #ffffff;
  border: none;
  border-radius: 6px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-danger {
  background: #ef4444;
}

.btn-secondary {
  background: #ffffff;
  border: 1px solid var(--color-border, #cbd5e1);
  color: var(--color-text-primary, #334155);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 12px;
  cursor: pointer;
}

.btn-secondary:hover {
  background: var(--color-bg-input, #f8fafc);
}

.btn-sm {
  padding: 4px 8px;
  font-size: 11px;
}

.tag-pill {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  background: #f1f5f9;
  color: #475569;
}

.tag-primary { background: #dbeafe; color: #1d4ed8; }
.tag-blue { background: #e0f2fe; color: #0284c7; }
.tag-success { background: #dcfce7; color: #15803d; }
.tag-amber { background: #fef3c7; color: #b45309; }

.text-danger { color: #ef4444; }
.text-success { color: #10b981; }
.text-muted { color: var(--color-text-muted, #94a3b8); }

.empty-state {
  text-align: center;
  color: var(--color-text-muted, #94a3b8);
}

.empty-icon {
  font-size: 36px;
  margin-bottom: 8px;
}
</style>
