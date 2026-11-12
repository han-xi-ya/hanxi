<script setup lang="ts">
import { ref, shallowRef, onMounted, computed } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { LogFileInfo } from '../../bindings/hanxi/internal/app/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useConfirm } from '../composables/useConfirm'
import { usePolling } from '../composables/usePolling'
import { useClipboard } from '../composables/useClipboard'
import { useLogTails } from '../composables/useLogTails'

const { showToast, showErrorToast } = useToast()
const { confirm } = useConfirm()
const { copyWithToast } = useClipboard()

const logFiles = shallowRef<LogFileInfo[]>([])
const selectedFile = ref<string>('')
const logContent = shallowRef<string>('')
const loading = ref(false)
const autoRefresh = ref(true)
const searchKeyword = ref('')
const maxLines = ref(300)
// 尾跟随机构收编 useLogTails 单源（frpc 抽屉/ddns-go 同形制）：用户上滚即脱闩、
// 回底自动恢复；「跟随尾部」勾选框直绑 autoScroll，可显式强制跟尾。
const {
  autoScroll,
  bodyRef: logContainerRef,
  onBodyScroll,
  scrollToBottom,
} = useLogTails({ maxLines: maxLines.value })
// 读取/列表错误常驻上下文（error-box），不只走 toast 一闪而过
const logError = ref('')
// 清理历史双闸（JsonFmtView aiAsking 教训）：确认待决期与执行期各锁一段，
// 弹窗期间按钮同样禁用，连点不会叠出第二发确认或第二次删除。
const clearAsking = ref(false)
const clearing = ref(false)

let currentRequestId = 0

async function loadFileList() {
  try {
    const list = await AppAPI.AppService.ListLogFiles()
    logFiles.value = list ?? []
    // 列表刷新后校验选中项仍在册：被清理/轮转掉的文件回落首项，列表空则置空，
    // 不再继续读一个已不存在的文件（错误只走 toast 的旧病）。
    const stillListed = logFiles.value.some(f => f.name === selectedFile.value)
    const next = stillListed ? selectedFile.value : (logFiles.value[0]?.name ?? '')
    if (next !== selectedFile.value) {
      selectedFile.value = next
      logContent.value = ''
    }
  } catch (err: unknown) {
    logError.value = `获取日志列表失败: ${getErrorMessage(err)}`
    showErrorToast(logError.value)
  }
}

async function fetchContent() {
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
    logError.value = ''
    // 闩控贴底：autoScroll 开才落底——自动刷新不再无条件重置滚动位，
    // 用户上翻阅读时不被打断，回底即恢复跟随。
    void scrollToBottom()
  } catch (err: unknown) {
    if (reqId === currentRequestId) {
      logError.value = `读取日志内容失败: ${getErrorMessage(err)}`
      showErrorToast(logError.value)
    }
  } finally {
    if (reqId === currentRequestId) {
      loading.value = false
    }
  }
}

// 换文件重新跟尾（新文件从头读起，跟随默认开）
function onFileChange() {
  autoScroll.value = true
  void fetchContent()
}

async function copyLogs() {
  if (!logContent.value) return
  // 复制走 useClipboard 如实回执：writeText 失败不谎报"已复制"（不允许 fire-and-forget）
  await copyWithToast(logContent.value, '日志内容已复制到剪贴板')
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
  if (clearAsking.value || clearing.value) return
  clearAsking.value = true // 先闸后问：确认弹窗期间也不许第二下点进来
  try {
    const accepted = await confirm({
      title: '清理历史日志？',
      description: '将删除日志目录中当天以外的全部历史日志文件（当天的 app-<日期>.log 保留）。文件直接删除，不可恢复。',
      tone: 'danger',
      confirmLabel: '清理历史',
    })
    clearAsking.value = false
    if (!accepted) return
    clearing.value = true
    await AppAPI.AppService.ClearLogs()
    showToast('已清理历史日志')
    await loadFileList()
    await fetchContent()
  } catch (err: unknown) {
    showErrorToast(`清理日志失败: ${getErrorMessage(err)}`)
  } finally {
    clearAsking.value = false
    clearing.value = false
  }
}

const filteredLines = computed(() => {
  if (!logContent.value) return []
  const lines = logContent.value.split('\n')
  if (!searchKeyword.value.trim()) return lines
  const kw = searchKeyword.value.trim().toLowerCase()
  return lines.filter(l => l.toLowerCase().includes(kw))
})

// 空态分诊：加载/无文件/读取失败/搜索无匹配/文件本身为空，各说各的话，
// 不再一句「暂无匹配的日志内容」混三种情况（搜索无匹配文案逐字保留，spec 钉死）。
const emptyText = computed(() => {
  if (loading.value) return '正在读取日志...'
  if (logError.value) return '日志读取失败——错误详情见上方提示，可点「刷新」重试。'
  if (logFiles.value.length === 0) return '暂无日志文件——应用运行后会自动生成，可点「目录」查看日志文件夹。'
  if (!selectedFile.value) return '未选择日志文件'
  if (searchKeyword.value.trim()) return '暂无匹配的日志内容'
  return '该日志文件暂无内容'
})

// 自动刷新轮询（usePolling 内置 KeepAlive 契约；immediateFirstRun:false 保持"2.5s 后才首次周期拉取"节奏）
usePolling(() => {
  if (autoRefresh.value && selectedFile.value) {
    fetchContent()
  }
}, 2500, { immediateFirstRun: false })

onMounted(async () => {
  await loadFileList()
  if (selectedFile.value) {
    await fetchContent()
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
        <select v-model="selectedFile" class="select-file" @change="onFileChange">
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
        <label class="auto-refresh-label">
          <input v-model="autoScroll" type="checkbox" />
          跟随尾部
        </label>
        <button class="btn btn-secondary btn-small" @click="fetchContent()">🔄 刷新</button>
        <button class="btn btn-secondary btn-small" @click="copyLogs">📋 复制</button>
        <button class="btn btn-secondary btn-small" @click="openLogFolder">📂 目录</button>
        <button class="btn btn-danger-outline btn-small" :disabled="clearAsking || clearing" @click="clearLogs">
          {{ clearAsking ? '等待确认…' : clearing ? '清理中…' : '🧹 清理历史' }}
        </button>
      </div>
    </div>

    <!-- 日志查看容器 -->
    <div class="log-viewer-container">
      <div class="viewer-header">
        <span class="file-name">{{ selectedFile || '未选择日志' }}</span>
        <span class="line-count">显示 {{ filteredLines.length }} 行日志</span>
      </div>
      <div v-if="logError" class="log-error-box" role="alert">{{ logError }}</div>
      <pre ref="logContainerRef" class="log-content" @scroll.passive="onBodyScroll"><code v-if="filteredLines.length > 0">{{ filteredLines.join('\n') }}</code><div v-else class="empty-logs">{{ emptyText }}</div></pre>
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
  font-size: var(--text-sm);
  background: var(--surface-soft);
  color: var(--color-text);
  font-family: var(--font-mono);
}

.input-search {
  padding: 5px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  font-size: var(--text-sm);
  background: var(--surface-soft);
  color: var(--color-text);
  width: 240px;
}
.input-search:focus-visible {
  /* 样式审计 C10 判定：此处 `outline: none` 已删——本规则专打 :focus-visible，
     留着会把全局焦点环按死在键盘态；换色边保留为附加强调。 */
  border-color: var(--color-primary);
}

.auto-refresh-label {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  margin-right: 4px;
}

/* 日志终端区：固定深底永不随主题反相（tokens.css --terminal-* 色板，蓝图 §7.3）。
   本区块字号（viewer-header/line-count/log-content）属终端豁免面，保留裸 px 不进 --text-* 阶梯。 */
.log-viewer-container {
  flex: 1;
  background: var(--terminal-bg);
  border-radius: var(--radius-control);
  border: 1px solid var(--terminal-border);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: var(--shadow-small);
  min-height: 400px;
}

.viewer-header {
  background: var(--terminal-head-bg);
  padding: 8px 14px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--terminal-border);
  font-size: 12px;
  color: var(--terminal-meta-fg);
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
  color: var(--terminal-row-fg);
  white-space: pre-wrap;
  word-break: break-all;
  user-select: text;
}

.empty-logs {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 200px;
  color: var(--terminal-faint-fg);
}

/* 常驻错误条：终端深底豁免面，前景取 --ansi-1（禁随主题 --state-* 铺深底），
   底色走 --terminal-error-bg 派生档；字号随本区终端豁免保留裸 px。 */
.log-error-box {
  padding: 6px 14px;
  background: var(--terminal-error-bg);
  color: var(--ansi-1);
  border-bottom: 1px solid var(--terminal-border);
  font-size: 12px;
  word-break: break-all;
}
</style>
