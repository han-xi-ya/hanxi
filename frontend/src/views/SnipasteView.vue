<script setup lang="ts">
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import * as SnipasteAPI from '../../bindings/hanxi/internal/modules/snipaste/snipasteservice'
import type { LaunchOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/snipaste/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/snipaste/instance/models'
import type { DownloadProgress, SnipasteRelease, SnipasteVersionInfo } from '../../bindings/hanxi/internal/modules/snipaste/version/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const { showToast } = useToast()
const activeMainTab = ref<'console' | 'versions'>('console')
const releases = ref<SnipasteRelease[]>([])
const installed = ref<SnipasteVersionInfo[]>([])
const activeVersion = ref('')
const siteURL = ref('')
const localLoading = ref(false)
const remoteLoading = ref(false)
const localError = ref('')
const remoteError = ref('')
const rowErrors = ref<Record<string, string>>({})
const busy = ref(false)
const snapshot = ref<Snapshot | null>(null)
const controlResult = ref<{ tone: 'info' | 'warning' | 'error'; text: string } | null>(null)
const uptimeSec = ref(0)
const downloading = ref<Record<string, DownloadProgress>>({})
const cleanupTimers = new Map<string, ReturnType<typeof setTimeout>>()
let unlistenDownload: (() => void) | null = null
let unlistenState: (() => void) | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

const selected = computed(() => installed.value.find(item => item.version === activeVersion.value) ?? installed.value[0] ?? null)
const stale = computed(() => releases.value.some(item => item.stale))
const instanceState = computed(() => snapshot.value?.state ?? 'stopped')
const ownedRunning = computed(() => ['starting', 'running', 'quitting'].includes(instanceState.value))
const stateText = computed(() => ({
  stopped: '本会话未托管', starting: '正在启动', running: '本会话实例运行中',
  quitting: '正在退出', failed: '实例操作失败',
}[instanceState.value] ?? '本会话未托管'))

type ReleaseStatus = 'installed' | 'downloading' | 'error' | 'idle'

async function refreshLocal(): Promise<boolean> {
  localLoading.value = true
  localError.value = ''
  try {
    const [local, active] = await Promise.all([
      SnipasteAPI.ListInstalledVersions(),
      SnipasteAPI.GetActiveVersion(),
    ])
    installed.value = local ?? []
    activeVersion.value = active ?? ''
    return true
  } catch (error) {
    localError.value = `读取本地版本失败：${getErrorMessage(error)}`
    return false
  } finally {
    localLoading.value = false
  }
}

async function refreshRemote(): Promise<boolean> {
  remoteLoading.value = true
  remoteError.value = ''
  try {
    releases.value = (await SnipasteAPI.ListReleases()) ?? []
    return true
  } catch (error) {
    remoteError.value = `获取 Snipaste 官网版本失败：${getErrorMessage(error)}`
    return false
  } finally {
    remoteLoading.value = false
  }
}

async function loadSiteURL() {
  try {
    siteURL.value = (await SnipasteAPI.OfficialSiteURL()) ?? ''
  } catch (error) {
    console.warn('load Snipaste site URL failed:', getErrorMessage(error))
  }
}

async function loadPageData() {
  await Promise.allSettled([refreshLocal(), refreshRemote(), loadSiteURL()])
}

async function refreshStatus() {
  try {
    snapshot.value = await SnipasteAPI.GetStatus()
  } catch (error) {
    console.warn('refresh Snipaste status failed:', getErrorMessage(error))
  }
}

async function launch() {
  if (busy.value || !selected.value || ownedRunning.value) return
  busy.value = true
  controlResult.value = null
  try {
    const outcome: LaunchOutcome = await SnipasteAPI.Launch()
    controlResult.value = { tone: 'info', text: outcome.message }
    showToast(outcome.message)
    await Promise.allSettled([refreshLocal(), refreshStatus()])
  } catch (error) {
    const text = `启动失败：${getErrorMessage(error)}`
    controlResult.value = { tone: 'error', text }
    showToast(text)
  } finally {
    busy.value = false
  }
}

async function quitProcess() {
  if (busy.value || !ownedRunning.value) return
  if (!window.confirm('Hanxi 会先向本会话启动的 Snipaste 发送关闭请求；若未在宽限期内退出，将自动强制结束。强制结束可能丢失未落盘状态。外部实例不受影响。')) return
  busy.value = true
  controlResult.value = null
  try {
    const outcome: QuitOutcome = await SnipasteAPI.Quit()
    controlResult.value = { tone: outcome.forced ? 'warning' : 'info', text: outcome.message }
    showToast(outcome.message)
    await refreshStatus()
  } catch (error) {
    controlResult.value = { tone: 'error', text: `退出失败：${getErrorMessage(error)}` }
  } finally {
    busy.value = false
  }
}

function setTicket(progress: DownloadProgress) {
  downloading.value = { ...downloading.value, [progress.version]: progress }
}

function clearTicket(version: string) {
  const next = { ...downloading.value }
  delete next[version]
  downloading.value = next
}

function cancelCleanup(version: string) {
  const timer = cleanupTimers.get(version)
  if (timer) clearTimeout(timer)
  cleanupTimers.delete(version)
}

function ticketOf(version: string): DownloadProgress | undefined {
  return downloading.value[version]
}

function statusOf(release: SnipasteRelease): ReleaseStatus {
  const ticket = ticketOf(release.version)
  if (ticket?.stage === 'error') return 'error'
  if (ticket) return 'downloading'
  return installed.value.some(item => item.version === release.version) ? 'installed' : 'idle'
}

function isDownloadBusy(version: string): boolean {
  const ticket = ticketOf(version)
  return !!ticket && ticket.stage !== 'error'
}

async function download(release: SnipasteRelease) {
  if (isDownloadBusy(release.version)) return
  cancelCleanup(release.version)
  rowErrors.value[release.version] = ''
  setTicket({ version: release.version, stage: 'pending', done: 0, total: 0, message: '正在创建下载任务' })
  try {
    const result = await SnipasteAPI.DownloadVersion(release.version)
    if (result === 'already-installed') {
      const refreshed = await refreshLocal()
      if (refreshed) clearTicket(release.version)
      else {
        setTicket({ version: release.version, stage: 'done', done: 100, total: 100, message: '版本已安装，本地列表刷新失败' })
        rowErrors.value[release.version] = '版本已安装，但本地列表刷新失败，请重试读取本地版本'
      }
    } else if (result === 'in-progress') {
      setTicket({ version: release.version, stage: 'pending', done: 0, total: 0, message: '已有下载任务正在进行' })
      showToast(`Snipaste ${release.version} 已在下载中`)
    }
  } catch (error) {
    const message = getErrorMessage(error)
    setTicket({ version: release.version, stage: 'error', done: 0, total: 0, message })
    rowErrors.value[release.version] = message
  }
}

async function handleDownloadProgress(progress: DownloadProgress) {
  cancelCleanup(progress.version)
  setTicket(progress)
  if (progress.stage === 'error') {
    rowErrors.value[progress.version] = progress.message || '安装失败'
    return
  }
  rowErrors.value[progress.version] = ''
  if (progress.stage !== 'done') return

  setTicket({ ...progress, message: '安装完成，正在同步本地版本' })
  const refreshed = await refreshLocal()
  const installedNow = installed.value.some(item => item.version === progress.version)
  if (!refreshed || !installedNow) {
    rowErrors.value[progress.version] = '安装已完成，但本地版本列表刷新失败，请重试读取本地版本'
    return
  }
  const timer = setTimeout(() => {
    if (ticketOf(progress.version)?.stage === 'done' && installed.value.some(item => item.version === progress.version)) {
      clearTicket(progress.version)
    }
    cleanupTimers.delete(progress.version)
  }, 900)
  cleanupTimers.set(progress.version, timer)
}

async function setActive(item: SnipasteVersionInfo) {
  rowErrors.value[item.version] = ''
  try {
    activeVersion.value = await SnipasteAPI.SetActiveVersion(item.version)
    showToast(`已将 ${item.version} 设为启动版本`)
  } catch (error) {
    rowErrors.value[item.version] = getErrorMessage(error)
  }
}

function isActiveVersion(version: string): boolean {
  return version === activeVersion.value
}

function isRunningVersion(version: string): boolean {
  return !!snapshot.value?.version && snapshot.value.version === version && ['starting', 'running', 'quitting'].includes(snapshot.value.state)
}

function cannotRemoveVersion(item: SnipasteVersionInfo): boolean {
  return isActiveVersion(item.version) || isRunningVersion(item.version)
}

function removeDisabledTitle(item: SnipasteVersionInfo): string {
  if (isRunningVersion(item.version)) {
    if (instanceState.value === 'starting') return '该版本正在启动'
    if (instanceState.value === 'quitting') return '该版本正在退出'
    return '该版本正在运行，请先退出进程'
  }
  if (isActiveVersion(item.version)) return '当前使用版本不可卸载，请先选择其他版本'
  return '卸载此版本'
}

async function removeVersion(item: SnipasteVersionInfo) {
  if (cannotRemoveVersion(item)) return
  if (!window.confirm(`确定卸载 Snipaste ${item.version}？\n将删除该版本的完整隔离目录，此操作不可恢复。`)) return
  if (cannotRemoveVersion(item)) return
  rowErrors.value[item.version] = ''
  try {
    await SnipasteAPI.RemoveVersion(item.version)
    showToast(`已卸载 Snipaste ${item.version}`)
    await refreshLocal()
  } catch (error) {
    rowErrors.value[item.version] = getErrorMessage(error)
  }
}

async function importLocal() {
  const path = window.prompt('请输入 Snipaste 免安装目录完整路径（目录内应包含 Snipaste.exe）')
  if (!path) return
  busy.value = true
  localError.value = ''
  try {
    const info = await SnipasteAPI.ImportLocal(path.trim())
    showToast(`已导入 Snipaste ${info.version}`)
    await refreshLocal()
  } catch (error) {
    localError.value = `导入失败：${getErrorMessage(error)}`
  } finally {
    busy.value = false
  }
}

async function openDir(path: string) {
  try { await SnipasteAPI.OpenDir(path) } catch (error) { showToast(`打开目录失败：${getErrorMessage(error)}`) }
}

async function openSite() {
  try { await SnipasteAPI.OpenOfficialSite() } catch (error) { showToast(`打开官网失败：${getErrorMessage(error)}`) }
}

function fmtSize(bytes: number): string {
  if (!bytes) return '未知'
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${Math.round(bytes / 1024)} KB`
}

function fmtDuration(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}

function stageText(progress?: DownloadProgress): string {
  if (!progress) return ''
  const names: Record<string, string> = {
    pending: '准备中', resolve: '解析官网版本', downloading: '下载中',
    'verify-size': '校验大小', 'verify-hash': '校验官方哈希',
    'verify-archive': '校验 ZIP 与布局', install: '完成安装', done: '安装完成', error: '安装失败',
  }
  return progress.message || names[progress.stage] || progress.stage
}

function percent(progress?: DownloadProgress): number | null {
  if (!progress || progress.stage !== 'downloading' || !progress.total) return null
  return Math.min(99, Math.round((progress.done / progress.total) * 100))
}

function verificationLabel(item: SnipasteVersionInfo | SnipasteRelease): string {
  if ('verificationMode' in item) {
    if (item.verificationMode.includes('official-sha1')) return '官方 SHA-1 + 大小 + ZIP CRC + 布局'
    if (item.verificationMode === 'local-import+layout') return '本地导入 + 布局检查'
    return '大小 + ZIP CRC + 布局'
  }
  return item.officialHash ? '官方 SHA-1' : '大小 + ZIP CRC + 布局'
}

function startTimers() {
  if (pollTimer) return
  pollTimer = setInterval(refreshStatus, 2500)
  tickTimer = setInterval(() => {
    if (snapshot.value?.state === 'running' && snapshot.value.startedAt) {
      const started = new Date(snapshot.value.startedAt).getTime()
      uptimeSec.value = Number.isNaN(started) ? 0 : Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }, 1000)
}

function stopTimers() {
  if (pollTimer) clearInterval(pollTimer)
  if (tickTimer) clearInterval(tickTimer)
  pollTimer = null
  tickTimer = null
  uptimeSec.value = 0
}

onMounted(() => {
  unlistenDownload = Events.On('snipaste:version-download', (event: { data?: DownloadProgress }) => {
    if (event?.data) void handleDownloadProgress(event.data)
  })
  unlistenState = Events.On('snipaste:instance-state', (event: { data?: Snapshot }) => {
    if (event?.data) snapshot.value = event.data
  })
  startTimers()
  void Promise.allSettled([loadPageData(), refreshStatus()])
})

onActivated(() => {
  startTimers()
  void Promise.allSettled([refreshLocal(), refreshStatus()])
})

onDeactivated(stopTimers)

onUnmounted(() => {
  stopTimers()
  unlistenDownload?.()
  unlistenState?.()
  unlistenDownload = null
  unlistenState = null
  cleanupTimers.forEach(clearTimeout)
  cleanupTimers.clear()
})
</script>

<template>
  <section class="page snipaste-view">
    <div class="header-row">
      <div class="snipaste-heading">
        <span class="snipaste-mark" aria-hidden="true">
          <svg viewBox="0 0 24 24"><path d="M9.4 7.7 5.8 4.1a2.6 2.6 0 1 0-1.7 4.5c.7 0 1.3-.3 1.8-.7l2.2 2.2m6.5-2.4 3.6-3.6a2.6 2.6 0 1 1 1.7 4.5c-.7 0-1.3-.3-1.8-.7L8.7 17.3a2.6 2.6 0 1 1-1.8-1.8L17 5.4M10 14l4 4" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
        </span>
        <div class="title-group">
          <h1>Snipaste</h1>
          <p class="subtitle">管理并启动官方 Windows x64 免安装版；原生截图、贴图、托盘和快捷键保持不变。</p>
        </div>
      </div>
      <div class="main-tab-nav" role="tablist" aria-label="Snipaste 页面">
        <button id="snipaste-console-tab" class="main-tab-btn" :class="{ active: activeMainTab === 'console' }" role="tab" aria-controls="snipaste-console-panel" :aria-selected="activeMainTab === 'console'" @click="activeMainTab = 'console'">控制台</button>
        <button id="snipaste-versions-tab" class="main-tab-btn" :class="{ active: activeMainTab === 'versions' }" role="tab" aria-controls="snipaste-versions-panel" :aria-selected="activeMainTab === 'versions'" @click="activeMainTab = 'versions'">版本管理</button>
      </div>
    </div>

    <div id="snipaste-console-panel" v-show="activeMainTab === 'console'" class="tab-body" role="tabpanel" aria-labelledby="snipaste-console-tab">
      <div class="control-panel snipaste-control-panel">
        <div class="snipaste-control-main">
          <div class="snipaste-control-state">
            <span class="snipaste-status" :data-state="instanceState">{{ stateText }}</span>
            <span v-if="snapshot?.pid" class="mono-meta">PID {{ snapshot.pid }} · {{ fmtDuration(uptimeSec) }}</span>
          </div>
          <strong class="version-value">{{ snapshot?.version || selected?.version || '尚未安装' }}</strong>
          <code v-if="snapshot?.exePath || selected" class="path-value">{{ snapshot?.exePath || selected?.exePath }}</code>
          <p v-else class="muted-copy">先下载官网免安装版，或导入已有的 Snipaste 便携目录。</p>
          <span v-if="snapshot?.error" class="snipaste-row-error" role="alert">{{ snapshot.error }}</span>
        </div>
        <div class="btn-group">
          <button v-if="!selected" class="btn btn-secondary" @click="activeMainTab = 'versions'">前往版本管理</button>
          <button class="btn btn-primary" :disabled="busy || !selected || ownedRunning" @click="launch">{{ instanceState === 'starting' ? '正在启动…' : '启动 Snipaste' }}</button>
          <button class="btn btn-danger-outline" :disabled="busy || !ownedRunning || instanceState === 'quitting'" @click="quitProcess">{{ instanceState === 'quitting' ? '正在退出…' : '退出进程' }}</button>
        </div>
      </div>

      <div v-if="localError" class="state-box state-error" role="alert"><strong>本地版本不可用</strong><span>{{ localError }}</span><button class="state-action" @click="refreshLocal">重新读取</button></div>
      <div v-if="controlResult" class="state-box" :class="`state-${controlResult.tone}`" aria-live="polite">{{ controlResult.text }}</div>

      <article class="info-panel">
        <h2>运行边界</h2>
        <ul>
          <li>Hanxi 只控制当前会话直接启动并成功登记的 Snipaste 进程。</li>
          <li>外部或上个 Hanxi 会话启动的实例不会被认领，也不会被退出。</li>
          <li>退出 Hanxi 或停用本模块后，Snipaste 仍会保留托盘与全局快捷键。</li>
          <li>页面退出会先发送尽力关闭请求，超时后强制结束。</li>
        </ul>
        <button class="text-link" @click="openSite">打开 Snipaste 官网<span v-if="siteURL"> · {{ siteURL }}</span></button>
      </article>
    </div>

    <div id="snipaste-versions-panel" v-show="activeMainTab === 'versions'" class="tab-body" role="tabpanel" aria-labelledby="snipaste-versions-tab">
      <div class="control-panel versions-overview">
        <div>
          <h2>版本资源</h2>
          <p>已安装 {{ installed.length }} 个版本 · 官网可用 {{ releases.length }} 个版本</p>
          <span>官方包按大小、SHA-1（可用时）、ZIP CRC 与布局校验后原子安装。</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary" :disabled="busy" @click="importLocal">导入本地</button>
          <button class="btn btn-secondary" :disabled="remoteLoading" @click="refreshRemote">{{ remoteLoading ? '刷新中…' : '刷新官网' }}</button>
        </div>
      </div>

      <div v-if="localError" class="state-box state-error" role="alert"><strong>读取本地版本失败</strong><span>{{ localError }}</span><button class="state-action" @click="refreshLocal">重试</button></div>

      <div class="section-title-row"><div><h2>已安装版本</h2><p>版本相互隔离，当前使用和正在运行可能是不同版本。</p></div></div>
      <div v-if="localLoading && !installed.length" class="state-box">正在读取本地版本…</div>
      <div v-else-if="!installed.length" class="state-box state-empty"><strong>尚未安装 Snipaste</strong><span>可从下方官网下载，或导入已有便携目录。</span></div>
      <div v-else class="installed-grid">
        <article v-for="item in installed" :key="item.version" class="installed-card">
          <div class="inst-card-top">
            <strong class="ver-tag">{{ item.version }}</strong>
            <div class="badge-group">
              <span v-if="isActiveVersion(item.version)" class="badge badge-active">当前使用</span>
              <span v-if="isRunningVersion(item.version)" class="badge badge-running">运行中</span>
              <span v-if="item.isImport" class="badge">本地导入</span>
            </div>
          </div>
          <code class="path-value">{{ item.dir }}</code>
          <p class="inst-meta">{{ fmtSize(item.size) }} · {{ verificationLabel(item) }}</p>
          <p v-if="rowErrors[item.version]" class="snipaste-row-error" role="alert">{{ rowErrors[item.version] }}</p>
          <div class="inst-actions">
            <button class="btn btn-ghost" @click="openDir(item.dir)">打开位置</button>
            <button v-if="!isActiveVersion(item.version)" class="btn btn-secondary" @click="setActive(item)">设为使用</button>
            <button class="btn btn-danger-outline" :disabled="cannotRemoveVersion(item)" :title="removeDisabledTitle(item)" @click="removeVersion(item)">卸载</button>
          </div>
        </article>
      </div>

      <div class="section-title-row remote-title"><div><h2>官网 Windows x64 免安装版</h2><p>仅从 Snipaste 官方域名下载，不使用第三方镜像。</p></div></div>
      <div v-if="remoteError" class="state-box state-error" role="alert"><strong>官网版本刷新失败</strong><span>{{ remoteError }}</span><button class="state-action" @click="refreshRemote">重试</button></div>
      <div v-if="stale" class="state-box state-warning"><strong>正在显示缓存数据</strong><span>官网暂时不可用，请留意版本信息可能不是最新。</span></div>
      <div v-if="remoteLoading && !releases.length" class="state-box">正在解析 Snipaste 官网版本…</div>
      <div v-else-if="!releases.length" class="state-box state-empty"><strong>没有可用的官网版本</strong><span>官网页面结构可能已变化，可稍后重试或导入本地版本。</span></div>
      <div v-else class="table-container">
        <table class="version-table">
          <thead><tr><th>版本</th><th>状态</th><th>大小</th><th>发布时间</th><th>校验</th><th class="action-col">操作</th></tr></thead>
          <tbody>
            <tr v-for="release in releases" :key="release.version">
              <td><div class="release-version"><strong>{{ release.version }}</strong><span v-if="release.isPre" class="badge badge-warning">预发布</span><span v-if="release.stale" class="badge badge-warning">缓存</span></div></td>
              <td>
                <span v-if="statusOf(release) === 'installed'" class="snipaste-ver-status installed">已安装</span>
                <span v-else-if="statusOf(release) === 'error'" class="snipaste-ver-status error">失败</span>
                <span v-else-if="statusOf(release) === 'downloading'" class="snipaste-ver-status working">{{ stageText(ticketOf(release.version)) }}</span>
                <span v-else class="snipaste-ver-status idle">可安装</span>
              </td>
              <td class="mono-meta">{{ fmtSize(release.size) }}</td>
              <td>{{ release.published || '未知' }}</td>
              <td>{{ verificationLabel(release) }}</td>
              <td class="action-col">
                <template v-if="statusOf(release) === 'downloading'">
                  <div v-if="ticketOf(release.version)?.stage === 'downloading'" class="snipaste-progress-wrap">
                    <div class="snipaste-progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="percent(ticketOf(release.version)) ?? undefined" :aria-valuetext="stageText(ticketOf(release.version))">
                      <span v-if="percent(ticketOf(release.version)) !== null" :style="{ width: `${percent(ticketOf(release.version))}%` }"></span>
                      <span v-else class="snipaste-progress-indeterminate"></span>
                    </div>
                    <small v-if="percent(ticketOf(release.version)) !== null">{{ percent(ticketOf(release.version)) }}%</small>
                  </div>
                  <span v-else class="download-stage">{{ stageText(ticketOf(release.version)) }}</span>
                </template>
                <button v-else-if="statusOf(release) === 'error'" class="retry-btn" @click="download(release)">重试</button>
                <span v-else-if="statusOf(release) === 'installed'" class="installed-label">已安装</span>
                <button v-else class="btn btn-primary btn-small" @click="download(release)">下载并安装</button>
                <p v-if="rowErrors[release.version]" class="snipaste-row-error" role="alert">{{ rowErrors[release.version] }}</p>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.snipaste-view { max-width: 1120px; margin: 0 auto; }
.header-row { display: flex; justify-content: space-between; align-items: flex-end; gap: 20px; margin-bottom: 18px; }
.snipaste-heading { display: flex; align-items: center; gap: 12px; min-width: 0; }
.title-group { min-width: 0; }
.title-group h1, .control-panel h2, .section-title-row h2, .info-panel h2 { margin: 0; }
.subtitle, .control-panel p, .section-title-row p { margin: 4px 0 0; color: var(--text-muted); }
.snipaste-mark { width: 42px; height: 42px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 10px; color: var(--accent); background: var(--bg-active); }
.snipaste-mark svg { width: 25px; height: 25px; }
.main-tab-nav { display: inline-flex; padding: 3px; border: 1px solid var(--border-color); border-radius: 9px; background: var(--bg-sidebar); }
.main-tab-btn { min-height: 34px; padding: 0 14px; border: 0; border-radius: 7px; background: transparent; color: var(--text-muted); cursor: pointer; }
.main-tab-btn.active { background: var(--bg-app); color: var(--accent); box-shadow: 0 1px 4px rgb(0 0 0 / 7%); }
.tab-body { display: grid; gap: 14px; }
.control-panel, .info-panel { border: 1px solid var(--border-color); border-radius: 12px; background: var(--bg-sidebar); padding: 16px; }
.snipaste-control-panel, .versions-overview { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.snipaste-control-main { display: grid; gap: 6px; min-width: 0; }
.snipaste-control-state, .btn-group, .badge-group, .inst-actions, .release-version { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.version-value, .ver-tag { font-family: ui-monospace, "Cascadia Mono", Consolas, monospace; font-variant-numeric: tabular-nums; }
.version-value { font-size: 20px; }
.path-value { display: block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); font: 12px/1.5 ui-monospace, "Cascadia Mono", Consolas, monospace; }
.mono-meta { color: var(--text-muted); font-family: ui-monospace, "Cascadia Mono", Consolas, monospace; font-variant-numeric: tabular-nums; font-size: 12px; }
.muted-copy { margin: 0; color: var(--text-muted); }
.btn { min-height: 36px; padding: 0 13px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-app); color: var(--text-main); font-weight: 600; cursor: pointer; }
.btn-primary { color: #fff; border-color: var(--accent); background: var(--accent); }
.btn-secondary { background: var(--bg-hover); }
.btn-ghost { color: var(--text-muted); background: transparent; }
.btn-danger-outline { color: var(--danger, #d64545); border-color: color-mix(in srgb, var(--danger, #d64545) 36%, var(--border-color)); background: transparent; }
.btn-small { min-height: 30px; padding: 0 10px; font-size: 12px; }
.btn:disabled { opacity: .46; cursor: not-allowed; }
.btn:not(:disabled):hover, .main-tab-btn:hover { border-color: color-mix(in srgb, var(--accent) 35%, var(--border-color)); }
.btn:focus-visible, .main-tab-btn:focus-visible, .text-link:focus-visible, .retry-btn:focus-visible, .state-action:focus-visible { outline: 3px solid color-mix(in srgb, var(--accent) 32%, transparent); outline-offset: 2px; }
.snipaste-status, .snipaste-ver-status, .badge { display: inline-flex; align-items: center; width: fit-content; border-radius: 999px; font-size: 11px; font-weight: 700; white-space: nowrap; }
.snipaste-status { padding: 3px 8px; border: 1px solid var(--border-color); color: var(--text-muted); }
.snipaste-status[data-state="running"] { color: var(--success, #08875d); border-color: color-mix(in srgb, var(--success, #08875d) 35%, var(--border-color)); }
.snipaste-status[data-state="starting"], .snipaste-status[data-state="quitting"] { color: var(--warning, #b66f08); }
.snipaste-status[data-state="failed"] { color: var(--danger, #d64545); }
.badge { padding: 2px 7px; color: var(--text-muted); background: var(--bg-hover); }
.badge-active { color: var(--accent); background: var(--bg-active); }
.badge-running { color: var(--success, #08875d); background: color-mix(in srgb, var(--success, #08875d) 10%, transparent); }
.badge-warning { color: var(--warning, #b66f08); background: color-mix(in srgb, var(--warning, #b66f08) 10%, transparent); }
.state-box { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; padding: 12px 14px; border: 1px dashed var(--border-color); border-radius: 9px; background: var(--bg-sidebar); color: var(--text-muted); }
.state-error, .snipaste-row-error { color: var(--danger, #d64545); }
.state-warning, .state-warning strong { color: var(--warning, #b66f08); }
.state-action { margin-left: auto; border: 0; background: transparent; color: var(--accent); cursor: pointer; font-weight: 600; }
.info-panel ul { margin: 10px 0 14px; padding-left: 20px; color: var(--text-muted); line-height: 1.7; }
.text-link, .retry-btn { border: 0; padding: 0; background: transparent; color: var(--accent); cursor: pointer; font-weight: 600; }
.section-title-row { margin-top: 4px; }
.remote-title { margin-top: 10px; }
.installed-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.installed-card { display: grid; gap: 10px; min-width: 0; padding: 14px; border: 1px solid var(--border-color); border-radius: 10px; background: var(--bg-sidebar); }
.inst-card-top { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.inst-meta { margin: 0; color: var(--text-muted); font-size: 12px; }
.inst-actions { margin-top: auto; }
.snipaste-row-error { margin: 0; font-size: 12px; line-height: 1.45; white-space: normal; }
.table-container { overflow-x: auto; border: 1px solid var(--border-color); border-radius: 10px; background: var(--bg-sidebar); }
.version-table { width: 100%; min-width: 830px; border-collapse: collapse; font-size: 13px; }
.version-table th, .version-table td { padding: 11px 12px; border-bottom: 1px solid var(--border-color); text-align: left; vertical-align: middle; }
.version-table th { color: var(--text-muted); background: var(--bg-hover); font-size: 11px; font-weight: 700; }
.version-table tbody tr:last-child td { border-bottom: 0; }
.action-col { width: 170px; }
.snipaste-ver-status { padding: 3px 7px; }
.snipaste-ver-status.installed { color: var(--success, #08875d); background: color-mix(in srgb, var(--success, #08875d) 10%, transparent); }
.snipaste-ver-status.error { color: var(--danger, #d64545); background: color-mix(in srgb, var(--danger, #d64545) 9%, transparent); }
.snipaste-ver-status.working { color: var(--warning, #b66f08); background: color-mix(in srgb, var(--warning, #b66f08) 9%, transparent); }
.snipaste-ver-status.idle { color: var(--text-muted); background: var(--bg-hover); }
.snipaste-progress-wrap { display: flex; align-items: center; gap: 7px; min-width: 125px; }
.snipaste-progress { position: relative; flex: 1; height: 6px; overflow: hidden; border-radius: 999px; background: var(--bg-hover); }
.snipaste-progress span { display: block; height: 100%; background: var(--accent); transition: width .1s linear; }
.snipaste-progress-indeterminate { width: 35%; animation: snipaste-slide 1.1s ease-in-out infinite; }
.download-stage, .installed-label { color: var(--text-muted); font-size: 12px; }
@keyframes snipaste-slide { from { transform: translateX(-110%); } to { transform: translateX(390%); } }
@media (max-width: 760px) { .header-row, .snipaste-control-panel, .versions-overview { align-items: stretch; flex-direction: column; } .main-tab-nav { align-self: flex-start; } .installed-grid { grid-template-columns: 1fr; } .btn-group { justify-content: flex-start; } }
@media (max-width: 460px) { .main-tab-nav { width: 100%; } .main-tab-btn { flex: 1; } .btn-group .btn { flex: 1 1 auto; } .path-value { white-space: normal; overflow-wrap: anywhere; } }
@media (pointer: coarse) { .btn, .main-tab-btn { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) { .snipaste-view *, .snipaste-view *::before, .snipaste-view *::after { animation-duration: .01ms !important; animation-iteration-count: 1 !important; transition-duration: .01ms !important; } }
</style>
