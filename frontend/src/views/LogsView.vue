<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, nextTick } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { LogFileInfo } from '../../bindings/hubkit/internal/app/models'
import { getErrorMessage } from '../utils/errors'

const logFiles = ref<LogFileInfo[]>([])
const selectedFile = ref<string>('')
const logContent = ref<string>('')
const loading = ref(false)
const autoRefresh = ref(true)
const searchKeyword = ref('')
const maxLines = ref(300)
const toastMsg = ref('')
const logContainerRef = ref<HTMLPreElement | null>(null)

let timer: any = null
let currentRequestId = 0

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

async function loadFileList() {
  try {
    const list = await AppAPI.AppService.ListLogFiles()
    logFiles.value = list ?? []
    if (logFiles.value.length > 0 && !selectedFile.value) {
      selectedFile.value = logFiles.value[0].name
    }
  } catch (err: unknown) {
    showToast(`获取日志列表失败: ${getErrorMessage(err)}`)
  }
}

async function fetchContent(scrollBottom = false) {
  if (!selectedFile.value) return
  const reqId = ++currentRequestId
  const targetFile = selectedFile.value
  loading.value = true
  try {
    const text = await AppAPI.AppService.ReadLogContent(targetFile, maxLines.value)
    // 防竞态：丢弃过期的响应
    if (reqId !== currentRequestId || selectedFile.value !== targetFile) {
      return
    }
    logContent.value = text ?? ''
    if (scrollBottom) {
      nextTick(() => {
        if (logContainerRef.value) {
          logContainerRef.value.scrollTop = logContainerRef.value.scrollHeight
        }
      })
    }
  } catch (err: unknown) {
    if (reqId === currentRequestId) {
      showToast(`读取日志内容失败: ${getErrorMessage(err)}`)
    }
  } finally {
    if (reqId === currentRequestId) {
      loading.value = false
    }
  }
}

async function selectLog(name: string) {
  selectedFile.value = name
  await fetchContent(true)
}

function copyLogs() {
  if (!logContent.value) return
  navigator.clipboard.writeText(logContent.value)
  showToast('日志内容已复制到剪贴板')
}

async function openLogFolder() {
  try {
    const info = await AppAPI.AppService.GetAppInfo()
    if (info?.logsDir) {
      await AppAPI.AppService.OpenPath(info.logsDir)
    }
  } catch (err: unknown) {
    showToast(`打开日志目录失败: ${getErrorMessage(err)}`)
  }
}

async function clearLogs() {
  try {
    await AppAPI.AppService.ClearLogs()
    showToast('已清理历史日志')
    await loadFileList()
    await fetchContent(true)
  } catch (err: unknown) {
    showToast(`清理日志失败: ${getErrorMessage(err)}`)
  }
}

const filteredLines = computed(() => {
  if (!logContent.value) return []
  const lines = logContent.value.split('\n')
  if (!searchKeyword.value.trim()) return lines
  const kw = searchKeyword.value.trim().toLowerCase()
  return lines.filter(l => l.toLowerCase().includes(kw))
})

onMounted(async () => {
  await loadFileList()
  if (selectedFile.value) {
    await fetchContent(true)
  }
  timer = setInterval(() => {
    if (autoRefresh.value && selectedFile.value) {
      fetchContent(false)
    }
  }, 2500)
})

onUnmounted(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})
</script>

<template>
  <section class="page logs-page">
    <div class="header-row">
      <div>
        <h1>运行日志</h1>
        <p class="subtitle">实时查看应用运行日志、脱敏记录与底层组件状态 (存储于 %APPDATA%/HubKit/logs)。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

    <!-- 顶部工具栏 -->
    <div class="control-panel">
      <div class="left-controls">
        <select v-model="selectedFile" class="select-file" @change="fetchContent(true)">
          <option v-for="f in logFiles" :key="f.name" :value="f.name">
            {{ f.name }} ({{ (f.size / 1024).toFixed(1) }} KB)
          </option>
          <option v-if="logFiles.length === 0" value="" disabled>暂无日志文件</option>
        </select>

        <input
          v-model="searchKeyword"
          type="text"
          class="input-search"
          placeholder="搜索关键词 (如 ERROR / wechat / frpc)..."
        />
      </div>

      <div class="right-controls">
        <label class="auto-refresh-label">
          <input v-model="autoRefresh" type="checkbox" />
          自动刷新 (2.5s)
        </label>
        <button class="btn btn-secondary btn-sm" @click="fetchContent(true)">🔄 刷新</button>
        <button class="btn btn-secondary btn-sm" @click="copyLogs">📋 复制</button>
        <button class="btn btn-secondary btn-sm" @click="openLogFolder">📂 目录</button>
        <button class="btn btn-danger-outline btn-sm" @click="clearLogs">🧹 清理历史</button>
      </div>
    </div>

    <!-- 日志查看容器 -->
    <div class="log-viewer-container">
      <div class="viewer-header">
        <span class="file-name">{{ selectedFile || '未选择日志' }}</span>
        <span class="line-count">显示 {{ filteredLines.length }} 行日志</span>
      </div>
      <pre ref="logContainerRef" class="log-content"><code v-if="filteredLines.length > 0">{{ filteredLines.join('\n') }}</code><div v-else class="empty-logs">{{ loading ? '正在读取日志...' : '暂无匹配的日志内容' }}</div></pre>
    </div>
  </section>
</template>

<style scoped>
.logs-page {
  display: flex;
  flex-direction: column;
  gap: 14px;
  height: calc(100vh - 48px);
}

.header-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.subtitle {
  color: var(--text-muted);
  font-size: 13px;
  margin: 4px 0 0;
}

.toast {
  background: var(--text-main);
  color: #fff;
  padding: 6px 14px;
  border-radius: 6px;
  font-size: 12px;
  animation: fadeIn 0.2s ease;
}

.control-panel {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  padding: 10px 14px;
  border-radius: 8px;
  flex-wrap: wrap;
}

.left-controls, .right-controls {
  display: flex;
  align-items: center;
  gap: 8px;
}

.select-file {
  padding: 5px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 12px;
  background: #fff;
  font-family: Consolas, monospace;
}

.input-search {
  padding: 5px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 12px;
  background: #fff;
  width: 240px;
}
.input-search:focus {
  border-color: var(--accent);
  outline: none;
}

.auto-refresh-label {
  font-size: 12px;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  margin-right: 4px;
}

.btn {
  padding: 5px 12px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  border: 1px solid transparent;
  transition: all 0.15s ease;
}

.btn-secondary {
  background: #fff;
  border-color: var(--border-color);
  color: var(--text-main);
}
.btn-secondary:hover {
  background: var(--bg-hover);
}

.btn-danger-outline {
  background: #fff;
  border-color: rgba(207, 34, 46, 0.3);
  color: var(--danger);
}
.btn-danger-outline:hover {
  background: #ffebe9;
}

.btn-sm {
  padding: 4px 10px;
}

.log-viewer-container {
  flex: 1;
  background: #1e1e1e;
  border-radius: 8px;
  border: 1px solid #333;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  min-height: 400px;
}

.viewer-header {
  background: #252526;
  padding: 8px 14px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #333;
  font-size: 12px;
  color: #858585;
}

.file-name {
  color: #9cdcfe;
  font-family: Consolas, monospace;
  font-weight: 600;
}

.line-count {
  font-size: 11px;
}

.log-content {
  flex: 1;
  margin: 0;
  padding: 12px 16px;
  overflow-y: auto;
  font-family: Consolas, 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: #d4d4d4;
  white-space: pre-wrap;
  word-break: break-all;
  user-select: text;
}

.empty-logs {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 200px;
  color: #666;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
