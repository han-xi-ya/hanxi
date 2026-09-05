<script setup lang="ts">
import { ref, shallowRef, onMounted, computed, nextTick } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { LogFileInfo } from '../../bindings/hanxi/internal/app/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { usePolling } from '../composables/usePolling'
import { useClipboard } from '../composables/useClipboard'

const { showToast } = useToast()
const { copy } = useClipboard()

const logFiles = shallowRef<LogFileInfo[]>([])
const selectedFile = ref<string>('')
const logContent = shallowRef<string>('')
const loading = ref(false)
const autoRefresh = ref(true)
const searchKeyword = ref('')
const maxLines = ref(300)
const logContainerRef = ref<HTMLPreElement | null>(null)

let currentRequestId = 0

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

async function copyLogs() {
  if (!logContent.value) return
  // 修复说明：原实现 writeText 失败也谎报"已复制"（fire-and-forget），收编 useClipboard 后如实回执
  const ok = await copy(logContent.value)
  showToast(ok ? '日志内容已复制到剪贴板' : '复制失败')
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

// 自动刷新轮询（usePolling 内置 KeepAlive 契约；immediateFirstRun:false 对齐原"2.5s 后才首次周期拉取"节奏）
usePolling(() => {
  if (autoRefresh.value && selectedFile.value) {
    fetchContent(false)
  }
}, 2500, { immediateFirstRun: false })

onMounted(async () => {
  await loadFileList()
  if (selectedFile.value) {
    await fetchContent(true)
  }
})
</script>

<template>
  <section class="page logs-page">
    <div class="header-row">
      <div>
        <h1>运行日志</h1>
        <p class="subtitle">实时查看应用运行日志、脱敏记录与底层组件状态。</p>
      </div>
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
        <button class="btn btn-secondary btn-small" @click="fetchContent(true)">🔄 刷新</button>
        <button class="btn btn-secondary btn-small" @click="copyLogs">📋 复制</button>
        <button class="btn btn-secondary btn-small" @click="openLogFolder">📂 目录</button>
        <button class="btn btn-danger-outline btn-small" @click="clearLogs">🧹 清理历史</button>
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
  height: calc(100dvh - 48px);
}

/* .header-row/.subtitle/.btn 家族均由全局原子接管 */
.subtitle {
  margin: 4px 0 0;
}

.control-panel {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  padding: 10px 14px;
  border-radius: var(--radius-control);
  flex-wrap: wrap;
}

.left-controls, .right-controls {
  display: flex;
  align-items: center;
  gap: 8px;
}

.select-file {
  padding: 5px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  font-size: 12px;
  background: var(--surface-soft);
  color: var(--color-text);
  font-family: var(--font-mono);
}

.input-search {
  padding: 5px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  font-size: 12px;
  background: var(--surface-soft);
  color: var(--color-text);
  width: 240px;
}
.input-search:focus-visible {
  border-color: var(--color-primary);
  outline: none;
}

.auto-refresh-label {
  font-size: 12px;
  color: var(--color-text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  margin-right: 4px;
}

/* 日志终端区：固定深底永不随主题反相（tokens.css --terminal-* 色板，蓝图 §7.3） */
.log-viewer-container {
  flex: 1;
  background: var(--terminal-bg);
  border-radius: var(--radius-control);
  border: 1px solid color-mix(in srgb, var(--terminal-fg) 14%, var(--terminal-bg));
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: var(--shadow-small);
  min-height: 400px;
}

.viewer-header {
  background: color-mix(in srgb, var(--terminal-fg) 5%, var(--terminal-bg));
  padding: 8px 14px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid color-mix(in srgb, var(--terminal-fg) 14%, var(--terminal-bg));
  font-size: 12px;
  color: color-mix(in srgb, var(--terminal-fg) 55%, var(--terminal-bg));
}

.file-name {
  color: var(--ansi-4);
  font-family: var(--font-mono);
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
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  color: var(--terminal-fg);
  white-space: pre-wrap;
  word-break: break-all;
  user-select: text;
}

.empty-logs {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 200px;
  color: color-mix(in srgb, var(--terminal-fg) 45%, var(--terminal-bg));
}
</style>
