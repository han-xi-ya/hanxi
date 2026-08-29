<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import QRCode from 'qrcode'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import * as FileShareAPI from '../../bindings/hanxi/internal/modules/fileshare'
import type {
  ShareConfig,
  ServerStatus,
  NetworkEndpoint,
  DropItem,
  TransferEvent
} from '../../bindings/hanxi/internal/modules/fileshare/models'
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
  maxUploadSizeMB: 0,
  authToken: '',
})

const endpoints = shallowRef<NetworkEndpoint[]>([])
const dropInbox = shallowRef<DropItem[]>([])
const transferLogs = ref<TransferEvent[]>([])
const loading = ref(false)
const errorMsg = ref('')
const activeTab = ref<'endpoints' | 'inbox' | 'logs'>('endpoints')
const savedSharePath = ref('')

const normalizedSharePath = computed(() => configForm.value.sharePath.trim())
const canOpenShareDirectory = computed(() => {
  return Boolean(savedSharePath.value) && normalizedSharePath.value === savedSharePath.value
})

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
    savedSharePath.value = cfg.sharePath.trim()
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
    savedSharePath.value = normalizedSharePath.value
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

async function handleOpenShareDirectory() {
  if (!canOpenShareDirectory.value) return
  try {
    await AppAPI.AppService.OpenPath(savedSharePath.value)
  } catch (err: unknown) {
    showToast(`打开目录失败: ${getErrorMessage(err)}`)
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
    <header class="hero-panel">
      <div class="hero-copy">
        <div class="eyebrow">LOCAL TRANSFER HUB</div>
        <h1 class="page-title">
          <span class="title-icon" aria-hidden="true">⇄</span>
          局域网文件快传
        </h1>
        <p class="page-desc">
          零客户端依赖的局域网极速文件/文本分享站。手机电脑同一 Wi-Fi 扫码即用，支持大文件单次流式拖拽上传。
        </p>
        <div class="hero-meta">
          <span class="status-pill" :class="{ online: status.isRunning }">
            <span class="status-indicator" :class="{ online: status.isRunning }"></span>
            {{ status.isRunning ? '服务运行中' : '服务已停止' }}
          </span>
          <span class="hero-port font-mono">端口 :{{ status.port || configForm.port }}</span>
        </div>
      </div>
      <button
        type="button"
        class="btn-primary hero-action"
        :class="{ 'btn-danger': status.isRunning }"
        :disabled="loading"
        @click="handleToggleServer"
      >
        <span v-if="loading">处理中...</span>
        <span v-else-if="status.isRunning">■ 停止快传服务</span>
        <span v-else>▶ 启动快传服务</span>
      </button>
    </header>

    <div v-if="errorMsg" class="alert-banner error section-gap">
      <span>⚠️ {{ errorMsg }}</span>
      <button type="button" class="btn-text" aria-label="关闭错误提示" @click="errorMsg = ''">✕</button>
    </div>

    <section class="overview-grid section-gap" aria-label="快传服务概览">
      <article class="overview-card overview-card-primary">
        <div class="metric-icon" aria-hidden="true">⌁</div>
        <div>
          <div class="stat-label">服务状态</div>
          <div class="stat-val">{{ status.isRunning ? '在线可访问' : '等待启动' }}</div>
          <div class="stat-sub">{{ status.activeConnections }} 个活跃连接</div>
        </div>
      </article>
      <article class="overview-card">
        <div class="metric-icon metric-icon-green" aria-hidden="true">↕</div>
        <div>
          <div class="stat-label">实时传输速率</div>
          <div class="stat-val stat-val-compact font-mono">
            <span class="text-upload">↑ {{ formatSpeed(status.uploadRate) }}</span>
            <span class="text-download">↓ {{ formatSpeed(status.downloadRate) }}</span>
          </div>
          <div class="stat-sub">上传 / 下载</div>
        </div>
      </article>
      <article class="overview-card">
        <div class="metric-icon metric-icon-blue" aria-hidden="true">Σ</div>
        <div>
          <div class="stat-label">累计传输</div>
          <div class="stat-val stat-val-compact font-mono">
            {{ formatBytes(status.uploadBytes + status.downloadBytes) }}
          </div>
          <div class="stat-sub">{{ status.uploadCount }} 次上传 · {{ status.downloadCount }} 次下载</div>
        </div>
      </article>
      <article class="overview-card">
        <div class="metric-icon metric-icon-amber" aria-hidden="true">✦</div>
        <div>
          <div class="stat-label">跨端投递</div>
          <div class="stat-val">{{ dropInbox.length }} <span class="unit">条</span></div>
          <div class="stat-sub">文本与链接碎片</div>
        </div>
      </article>
    </section>

    <section class="config-card section-gap">
      <div class="section-header">
        <div>
          <div class="section-kicker">SHARING SETTINGS</div>
          <h2 class="section-title">共享服务设置</h2>
          <p class="section-desc">配置对外共享的位置、网络入口与访问权限。</p>
        </div>
        <div class="section-actions">
          <span v-if="status.isRunning" class="hot-apply-tip">● 保存后实时生效</span>
          <button type="button" class="btn-secondary" @click="handleSaveConfig">保存共享规则</button>
        </div>
      </div>

      <div class="settings-layout">
        <div class="setting-panel location-panel">
          <div class="setting-panel-title">
            <span class="setting-icon" aria-hidden="true">⌂</span>
            <div>
              <h3>共享位置</h3>
              <p>局域网设备只能访问此目录中的内容</p>
            </div>
          </div>
          <label class="field-label" for="share-path">PC 本地物理路径</label>
          <div class="path-input-group">
            <input
              id="share-path"
              v-model="configForm.sharePath"
              type="text"
              class="input-control font-mono"
              placeholder="请选择或输入要共享的物理文件夹路径..."
            />
            <button type="button" class="btn-secondary path-action" @click="handleChooseDirectory">
              选择目录
            </button>
            <button
              type="button"
              class="btn-secondary path-action open-action"
              :disabled="!canOpenShareDirectory"
              :title="canOpenShareDirectory ? '在系统资源管理器中打开共享目录' : '请先保存当前共享目录'"
              @click="handleOpenShareDirectory"
            >
              打开目录
            </button>
          </div>
          <div class="path-hints">
            <span>🔒 安全沙箱已开启，外部访客无法越界访问。</span>
            <span v-if="normalizedSharePath && !canOpenShareDirectory" class="unsaved-hint">当前路径尚未保存</span>
          </div>
        </div>

        <div class="setting-panel network-panel">
          <div class="setting-panel-title">
            <span class="setting-icon" aria-hidden="true">⌁</span>
            <div>
              <h3>网络与容量</h3>
              <p>控制访问端口与单文件大小</p>
            </div>
          </div>
          <div class="network-fields">
            <div class="form-group">
              <div class="field-row">
                <label class="field-label" for="share-port">监听端口</label>
                <div class="quick-ports">
                  <button
                    v-for="port in [80, 8080, 8888]"
                    :key="port"
                    type="button"
                    class="quick-port-chip"
                    :class="{ active: configForm.port === port }"
                    @click="setQuickPort(port)"
                  >
                    {{ port }}
                  </button>
                </div>
              </div>
              <input id="share-port" v-model.number="configForm.port" type="number" class="input-control font-mono" min="1" max="65535" />
              <span class="form-hint">端口 80 可直接通过 IP 访问</span>
            </div>
            <div class="form-group">
              <label class="field-label" for="upload-limit">单文件上传上限 (MB)</label>
              <input id="upload-limit" v-model.number="configForm.maxUploadSizeMB" type="number" class="input-control font-mono" min="0" step="1" placeholder="0" />
              <span class="form-hint">0 表示不限制上传大小</span>
            </div>
          </div>
        </div>

        <div class="setting-panel permissions-panel">
          <div class="setting-panel-title permissions-title">
            <span class="setting-icon" aria-hidden="true">✓</span>
            <div>
              <h3>访问权限</h3>
              <p>按需开放局域网交互能力</p>
            </div>
          </div>
          <div class="permission-list">
            <label class="permission-item">
              <span><strong>允许上传文件</strong><small>支持大文件单次流式上传</small></span>
              <input v-model="configForm.allowUpload" type="checkbox" />
            </label>
            <label class="permission-item">
              <span><strong>允许文本投递</strong><small>接收手机发送的文本与链接</small></span>
              <input v-model="configForm.allowTextDrop" type="checkbox" />
            </label>
            <label class="permission-item">
              <span><strong>同步到极客随手记</strong><small>自动保存移动端投递内容</small></span>
              <input v-model="configForm.autoSaveToMemo" type="checkbox" />
            </label>
          </div>
        </div>
      </div>
    </section>

    <section class="workspace-card">
      <div class="workspace-header">
        <div>
          <div class="section-kicker">LIVE WORKSPACE</div>
          <h2 class="section-title">访问与传输中心</h2>
        </div>
        <nav class="tab-nav" aria-label="快传工作区">
          <button type="button" class="tab-item" :class="{ active: activeTab === 'endpoints' }" @click="activeTab = 'endpoints'">
            接入点 <span class="tab-count">{{ endpoints.length }}</span>
          </button>
          <button type="button" class="tab-item" :class="{ active: activeTab === 'inbox' }" @click="activeTab = 'inbox'">
            投递箱 <span class="tab-count">{{ dropInbox.length }}</span>
          </button>
          <button type="button" class="tab-item" :class="{ active: activeTab === 'logs' }" @click="activeTab = 'logs'">
            传输审计 <span class="tab-count">{{ transferLogs.length }}</span>
          </button>
        </nav>
      </div>

      <div class="workspace-body">

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
    </section>
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

/* Refined dashboard layout */
.fileshare-page {
  width: 100%;
  max-width: 1440px;
  padding: 28px 36px 40px;
  overflow-x: hidden;
}

.section-gap {
  margin-bottom: 18px;
}

.hero-panel {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 28px;
  padding: 26px 28px;
  margin-bottom: 18px;
  overflow: hidden;
  background:
    radial-gradient(circle at 85% 0%, rgba(59, 130, 246, 0.14), transparent 36%),
    linear-gradient(135deg, var(--color-bg-card, #ffffff), var(--color-bg-input, #f8fafc));
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 16px;
  box-shadow: 0 12px 30px rgba(15, 23, 42, 0.06);
}

.hero-copy {
  min-width: 0;
}

.eyebrow,
.section-kicker {
  margin-bottom: 7px;
  color: #2563eb;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.16em;
}

.page-title {
  margin: 0 0 8px;
  gap: 10px;
  font-size: 27px;
  letter-spacing: -0.02em;
}

.title-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  color: #ffffff;
  font-size: 22px;
  background: linear-gradient(135deg, #2563eb, #0ea5e9);
  border-radius: 10px;
  box-shadow: 0 7px 16px rgba(37, 99, 235, 0.22);
}

.page-desc {
  max-width: 760px;
  margin: 0;
  font-size: 13px;
  line-height: 1.65;
}

.hero-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 15px;
}

.status-pill,
.hero-port {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 9px;
  color: var(--color-text-secondary, #64748b);
  font-size: 11px;
  font-weight: 600;
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 999px;
}

.status-pill.online {
  color: #047857;
  background: rgba(16, 185, 129, 0.09);
  border-color: rgba(16, 185, 129, 0.25);
}

.hero-action {
  min-width: 150px;
  padding: 10px 17px;
  border-radius: 9px;
  box-shadow: 0 7px 16px rgba(37, 99, 235, 0.2);
}

.overview-grid {
  display: grid;
  grid-template-columns: 1.2fr repeat(3, 1fr);
  gap: 12px;
}

.overview-card {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  padding: 16px;
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 13px;
  transition: transform 0.18s ease, border-color 0.18s ease, box-shadow 0.18s ease;
}

.overview-card:hover {
  transform: translateY(-1px);
  border-color: rgba(59, 130, 246, 0.35);
  box-shadow: 0 8px 22px rgba(15, 23, 42, 0.06);
}

.overview-card-primary {
  background: linear-gradient(135deg, rgba(37, 99, 235, 0.08), var(--color-bg-card, #ffffff));
  border-color: rgba(37, 99, 235, 0.22);
}

.metric-icon,
.setting-icon {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  color: #2563eb;
  font-size: 19px;
  font-weight: 700;
  background: rgba(37, 99, 235, 0.1);
  border-radius: 11px;
}

.metric-icon-green { color: #059669; background: rgba(16, 185, 129, 0.1); }
.metric-icon-blue { color: #0284c7; background: rgba(14, 165, 233, 0.1); }
.metric-icon-amber { color: #d97706; background: rgba(245, 158, 11, 0.11); }

.stat-label { margin-bottom: 4px; }
.stat-val { margin-bottom: 3px; font-size: 18px; }
.stat-val-compact {
  display: flex;
  flex-wrap: wrap;
  gap: 3px 10px;
  font-size: 13px;
}

.config-card,
.workspace-card {
  padding: 0;
  overflow: hidden;
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 16px;
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.045);
}

.section-header,
.workspace-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 22px;
  border-bottom: 1px solid var(--color-border, #e2e8f0);
}

.section-title {
  margin: 0;
  color: var(--color-text-primary, #0f172a);
  font-size: 18px;
  font-weight: 750;
}

.section-desc {
  margin: 5px 0 0;
  color: var(--color-text-secondary, #64748b);
  font-size: 12px;
}

.section-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.hot-apply-tip {
  color: #059669;
  font-size: 11px;
  font-weight: 600;
}

.settings-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(320px, 0.9fr);
  gap: 14px;
  padding: 16px;
  background: var(--color-bg-input, #f8fafc);
}

.setting-panel {
  min-width: 0;
  padding: 18px;
  background: var(--color-bg-card, #ffffff);
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 12px;
}

.location-panel { grid-column: 1; }
.network-panel { grid-column: 1; }
.permissions-panel { grid-column: 2; grid-row: 1 / span 2; }

.setting-panel-title {
  display: flex;
  align-items: center;
  gap: 11px;
  margin-bottom: 17px;
}

.setting-panel-title h3 {
  margin: 0 0 3px;
  color: var(--color-text-primary, #0f172a);
  font-size: 14px;
}

.setting-panel-title p {
  margin: 0;
  color: var(--color-text-muted, #94a3b8);
  font-size: 11px;
}

.setting-icon {
  width: 34px;
  height: 34px;
  font-size: 15px;
  border-radius: 9px;
}

.field-label {
  color: var(--color-text-primary, #1e293b);
  font-size: 12px;
  font-weight: 650;
}

.path-input-group {
  align-items: stretch;
  margin-top: 7px;
}

.path-input-group .input-control {
  flex: 1;
  min-width: 100px;
}

.path-action {
  flex: 0 0 auto;
  padding: 8px 12px;
  white-space: nowrap;
}

.open-action {
  color: #1d4ed8;
  border-color: rgba(37, 99, 235, 0.35);
}

.path-hints {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  margin-top: 8px;
  color: var(--color-text-muted, #94a3b8);
  font-size: 11px;
}

.unsaved-hint {
  flex: 0 0 auto;
  color: #d97706;
  font-weight: 600;
}

.network-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}

.field-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.quick-ports {
  display: flex;
  align-items: center;
  gap: 4px;
}

.quick-port-chip {
  padding: 2px 7px;
  border-radius: 999px;
}

.permission-list {
  display: flex;
  flex-direction: column;
  gap: 9px;
}

.permission-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 13px;
  cursor: pointer;
  background: var(--color-bg-input, #f8fafc);
  border: 1px solid transparent;
  border-radius: 10px;
  transition: border-color 0.18s, background 0.18s;
}

.permission-item:hover {
  border-color: rgba(59, 130, 246, 0.26);
}

.permission-item span {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.permission-item strong {
  color: var(--color-text-primary, #1e293b);
  font-size: 12px;
  font-weight: 650;
}

.permission-item small {
  color: var(--color-text-muted, #94a3b8);
  font-size: 10px;
  line-height: 1.4;
}

.permission-item input {
  width: 16px;
  height: 16px;
  accent-color: #2563eb;
}

.workspace-header {
  align-items: flex-end;
}

.tab-nav {
  gap: 4px;
  max-width: 100%;
  padding: 4px;
  overflow-x: auto;
  background: var(--color-bg-input, #f1f5f9);
  border: 0;
  border-radius: 10px;
}

.tab-item {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
  padding: 7px 11px;
  white-space: nowrap;
  border-radius: 7px;
}

.tab-item.active {
  color: #1d4ed8;
  background: var(--color-bg-card, #ffffff);
  box-shadow: 0 2px 7px rgba(15, 23, 42, 0.09);
}

.tab-count {
  min-width: 18px;
  padding: 1px 5px;
  color: inherit;
  font-size: 10px;
  text-align: center;
  background: rgba(100, 116, 139, 0.12);
  border-radius: 999px;
}

.workspace-body {
  min-height: 230px;
  padding: 18px;
  background: var(--color-bg-input, #f8fafc);
}

.workspace-body > .tab-content > .card,
.endpoint-card {
  border-radius: 12px;
  box-shadow: none;
}

.endpoint-grid {
  grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
  gap: 14px;
}

.endpoint-card {
  padding: 18px;
}

.ep-qr {
  min-width: 132px;
  width: 132px;
  height: 132px;
  box-shadow: inset 0 0 0 1px rgba(15, 23, 42, 0.03);
}

.inbox-item {
  border-radius: 10px;
}

.table-responsive {
  max-width: 100%;
  overflow-x: auto;
  border: 1px solid var(--color-border, #e2e8f0);
  border-radius: 9px;
}

.table {
  min-width: 720px;
}

.btn-primary,
.btn-secondary,
.quick-port-chip,
.tab-item {
  transition: transform 0.15s ease, border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.btn-primary:hover:not(:disabled),
.btn-secondary:hover:not(:disabled) {
  transform: translateY(-1px);
}

.btn-secondary:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.btn-primary:focus-visible,
.btn-secondary:focus-visible,
.quick-port-chip:focus-visible,
.tab-item:focus-visible,
.input-control:focus-visible {
  outline: 2px solid rgba(37, 99, 235, 0.55);
  outline-offset: 2px;
}

.empty-state {
  padding: 44px 20px;
  background: var(--color-bg-card, #ffffff);
  border: 1px dashed var(--color-border, #cbd5e1);
  border-radius: 12px;
}

.empty-state h3 {
  margin: 0 0 7px;
  color: var(--color-text-primary, #334155);
  font-size: 15px;
}

.empty-state p {
  max-width: 620px;
  margin: 0 auto;
  font-size: 12px;
  line-height: 1.6;
}

@media (max-width: 1100px) {
  .overview-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .settings-layout {
    grid-template-columns: 1fr;
  }

  .location-panel,
  .network-panel,
  .permissions-panel {
    grid-column: 1;
    grid-row: auto;
  }

  .permission-list {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
  }

  .permission-item {
    align-items: flex-start;
  }
}

@media (max-width: 760px) {
  .fileshare-page {
    padding: 18px 16px 30px;
  }

  .hero-panel,
  .section-header,
  .workspace-header {
    align-items: stretch;
    flex-direction: column;
  }

  .hero-action {
    width: 100%;
  }

  .overview-grid,
  .network-fields,
  .permission-list {
    grid-template-columns: 1fr;
  }

  .section-actions {
    justify-content: space-between;
  }

  .workspace-header {
    gap: 14px;
  }

  .tab-nav {
    width: 100%;
  }

  .path-input-group {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }

  .path-input-group .input-control {
    grid-column: 1 / -1;
    width: 100%;
  }

  .path-hints {
    align-items: flex-start;
    flex-direction: column;
  }

  .ep-body,
  .inbox-item {
    align-items: stretch;
    flex-direction: column;
  }

  .ep-qr {
    width: 100%;
  }

  .inbox-item-actions {
    justify-content: flex-end;
  }
}

@media (max-width: 460px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }

  .hero-meta,
  .section-actions {
    align-items: flex-start;
    flex-direction: column;
  }

  .section-actions .btn-secondary {
    width: 100%;
  }
}
</style>
