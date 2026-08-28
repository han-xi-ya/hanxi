import { getConfig, getStats, sendText } from './api.js'
import { createFileBrowser } from './file-browser.js'
import { createUploader } from './upload.js'
import { formatBytes, formatSpeed, setButtonBusy, showToast } from './ui.js'

const elements = {
  actionsGrid: document.getElementById('actionsGrid'),
  textDropCard: document.getElementById('textDropCard'),
  uploadCard: document.getElementById('uploadCard'),
  dropContent: document.getElementById('dropContent'),
  dropSubmitButton: document.getElementById('dropSubmitButton'),
  refreshButton: document.getElementById('refreshButton'),
  connectionBadge: document.getElementById('connectionBadge'),
  connectionText: document.getElementById('connectionText'),
  statsRegion: document.getElementById('statsRegion'),
  statsStatus: document.getElementById('statsStatus'),
}

let serverConfig = { allowUpload: true, allowTextDrop: true, maxUploadSizeMB: 0 }
let statsTimer = null
let statsInFlight = false

const browser = createFileBrowser({
  list: document.getElementById('fileList'),
  breadcrumb: document.getElementById('breadcrumb'),
  count: document.getElementById('itemCount'),
  uploadTarget: document.getElementById('uploadTarget'),
})

const uploader = createUploader({
  elements: {
    dropzone: document.getElementById('dropzone'),
    fileInput: document.getElementById('fileInput'),
    task: document.getElementById('uploadTask'),
    fileName: document.getElementById('uploadFileName'),
    percent: document.getElementById('uploadPercent'),
    progress: document.getElementById('uploadProgress'),
    progressFill: document.getElementById('uploadProgressFill'),
    status: document.getElementById('uploadStatus'),
    summary: document.getElementById('uploadSummary'),
    cancelButton: document.getElementById('cancelUploadButton'),
    limit: document.getElementById('uploadLimit'),
  },
  getCurrentPath: browser.getCurrentPath,
  getConfig: () => serverConfig,
  onComplete: async targetPath => {
    if (browser.getCurrentPath() === targetPath) await browser.refresh()
  },
})

function applyConfig(config) {
  serverConfig = { ...serverConfig, ...config }
  elements.uploadCard.classList.toggle('hidden', !serverConfig.allowUpload)
  elements.textDropCard.classList.toggle('hidden', !serverConfig.allowTextDrop)
  const visibleCards = Number(serverConfig.allowUpload) + Number(serverConfig.allowTextDrop)
  elements.actionsGrid.classList.toggle('hidden', visibleCards === 0)
  elements.actionsGrid.classList.toggle('single-column', visibleCards === 1)
  uploader.updateConfig(serverConfig)
}

async function loadConfig() {
  try {
    applyConfig(await getConfig())
  } catch (error) {
    showToast(`无法确认服务权限：${error.message || error}`, 'error')
  }
}

async function refreshStats() {
  if (statsInFlight) return
  statsInFlight = true
  try {
    const stats = await getStats()
    document.getElementById('statConn').textContent = String(stats.activeConnections || 0)
    document.getElementById('statUpRate').textContent = `↑ ${formatSpeed(stats.uploadRate)}`
    document.getElementById('statDownRate').textContent = `↓ ${formatSpeed(stats.downloadRate)}`
    document.getElementById('statUpBytes').textContent = `↑ ${formatBytes(stats.uploadBytes)}`
    document.getElementById('statDownBytes').textContent = `↓ ${formatBytes(stats.downloadBytes)}`
    document.getElementById('statCounts').textContent = `${stats.uploadCount || 0} / ${stats.downloadCount || 0}`
    elements.statsStatus.textContent = '实时在线'
    elements.statsRegion.classList.remove('is-stale')
    elements.connectionBadge.classList.remove('is-offline')
    elements.connectionText.textContent = '局域网直连'
  } catch {
    elements.statsStatus.textContent = '数据暂不可用'
    elements.statsRegion.classList.add('is-stale')
    elements.connectionBadge.classList.add('is-offline')
    elements.connectionText.textContent = '状态待确认'
  } finally {
    statsInFlight = false
  }
}

function startStatsPolling() {
  window.clearInterval(statsTimer)
  refreshStats()
  statsTimer = window.setInterval(refreshStats, 2000)
}

async function submitText() {
  const content = elements.dropContent.value.trim()
  if (!content) {
    showToast('请输入要投递的内容', 'error')
    elements.dropContent.focus()
    return
  }
  setButtonBusy(elements.dropSubmitButton, true, '正在发送…')
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 15000)
  try {
    await sendText(content, controller.signal)
    elements.dropContent.value = ''
    elements.dropContent.focus()
    showToast('投递成功，电脑端已即时接收', 'success')
  } catch (error) {
    if (error?.name === 'AbortError') {
      showToast('响应超时：内容可能已送达，请到电脑端确认', 'error')
    } else {
      showToast(`投递失败：${error.message || error}`, 'error')
    }
  } finally {
    window.clearTimeout(timeout)
    setButtonBusy(elements.dropSubmitButton, false)
  }
}

elements.dropSubmitButton.addEventListener('click', submitText)
elements.dropContent.addEventListener('keydown', event => {
  if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
    event.preventDefault()
    submitText()
  }
})
elements.refreshButton.addEventListener('click', async () => {
  setButtonBusy(elements.refreshButton, true, '刷新中…')
  try {
    await browser.refresh()
  } finally {
    setButtonBusy(elements.refreshButton, false)
  }
})

document.addEventListener('visibilitychange', () => {
  if (document.hidden) {
    window.clearInterval(statsTimer)
  } else {
    startStatsPolling()
  }
})

await Promise.all([loadConfig(), browser.load('')])
startStatsPolling()
