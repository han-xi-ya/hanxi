<script setup lang="ts">
// 状态 / 内嵌控制台入口 / 进程日志 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated, nextTick, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import * as DdnsGoAPI from '../../bindings/hanxi/internal/modules/ddnsgo/ddnsgoservice'
import type { DdnsRelease, DdnsVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/ddnsgo/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/ddnsgo/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/ddnsgo/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<DdnsRelease[]>([])
const installed = ref<DdnsVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

// 进程输出日志（事件流累积 + 挂载时拉最近 200 行）
const MAX_LOG_LINES = 400
const logLines = ref<string[]>([])
const logAutoScroll = ref(true)
const logBodyRef = ref<HTMLElement | null>(null)

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/flclash 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

let unlistenDownload: (() => void) | null = null
let unlistenState: (() => void) | null = null
let unlistenLog: (() => void) | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

const stateText = computed(() => {
  switch (state.value) {
    case 'running': return '运行中'
    case 'starting': return '启动中…'
    case 'failed': return '异常退出'
    case 'external': return '外部运行'
    default: return '未运行'
  }
})

const runningVersion = computed(() => snap.value?.version ?? '')
const listenAddr = computed(() => snap.value?.listenAddr ?? '')

// DDNS 更新日志的成败着色（上游输出含中英文两种语言）
function logWarnish(line: string): boolean {
  return /失败|错误|异常|error|fail|refused|timeout/i.test(line) && !/未变化|no change/i.test(line)
}

// 条件提示条（三个变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      cls: 'banner-warn',
      text: '检测到外部 ddns-go 实例（自行启动或 Windows 服务，非 Hanxi 托管）。可直接打开其面板查看；托管启动需先退出外部实例。',
    }
  }
  if (state.value === 'failed') {
    return { cls: 'banner-error', text: snap.value?.error || 'ddns-go 异常退出' }
  }
  if (state.value === 'running') {
    return {
      cls: 'banner-ok',
      text: `ddns-go 正在运行：Web 面板 ${listenAddr.value ? 'http://' + listenAddr.value : ''}（仅回环，不外露局域网）。DNS 服务商与域名配置在其页面内完成，保存即生效。`,
    }
  }
  return null
})

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remote, local, active] = await Promise.all([
      DdnsGoAPI.ListReleases(),
      DdnsGoAPI.ListInstalledVersions(),
      DdnsGoAPI.GetActiveVersion(),
    ])
    releases.value = remote ?? []
    installed.value = local ?? []
    activeVersion.value = active ?? ''
  } catch (e) {
    listError.value = `获取版本列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function refreshStatus() {
  try {
    snap.value = await DdnsGoAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('ddnsgo GetStatus failed:', getErrorMessage(e))
  }
}

async function loadLogHistory() {
  try {
    const lines = await DdnsGoAPI.Logs(200)
    if (Array.isArray(lines) && lines.length) {
      logLines.value = lines.slice(-MAX_LOG_LINES)
      scrollToBottom()
    }
  } catch (e) {
    console.warn('ddnsgo Logs failed:', getErrorMessage(e))
  }
}

function appendLog(line: string) {
  logLines.value.push(line)
  if (logLines.value.length > MAX_LOG_LINES) {
    logLines.value.splice(0, logLines.value.length - MAX_LOG_LINES)
  }
  void scrollToBottom()
}

async function scrollToBottom() {
  await nextTick()
  const el = logBodyRef.value
  if (el && logAutoScroll.value) el.scrollTop = el.scrollHeight
}

function clearLogs() {
  logLines.value = []
}

// ---------- 格式化 ----------
function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(0)} KB`
}

function fmtDate(s?: string): string {
  if (!s) return '—'
  return s.slice(0, 10)
}

function fmtDuration(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: DdnsRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function startDdns() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await DdnsGoAPI.Start()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function openConsole() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await DdnsGoAPI.OpenConsole()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitDdns() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await DdnsGoAPI.Quit()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`退出失败: ${getErrorMessage(e)}`)
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

// ---------- 版本管理操作 ----------
async function download(rel: DdnsRelease) {
  try {
    const res = await DdnsGoAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: DdnsVersionInfo) {
  try {
    const ver = await DdnsGoAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await DdnsGoAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: DdnsVersionInfo) {
  if (!window.confirm(`确定卸载 ddns-go ${v.version}？\n该版本隔离目录将被删除，不可恢复。\n（你的 ~/.ddns_go_config.yaml 域名配置不受影响，后续版本继续共用）`)) return
  try {
    await DdnsGoAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = window.prompt('请输入 ddns-go 所在目录完整路径（含 ddns-go.exe 的官方下载解压目录即可）\n提示：域名配置恒在 ~/.ddns_go_config.yaml，与 exe 位置无关')
  if (!path) return
  try {
    busy.value = true
    const info = await DdnsGoAPI.ImportLocal(path.trim())
    showToast(`已导入 ddns-go ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// ---------- 时长 ticker 与轮询管理 ----------
function startTimers() {
  if (pollTimer) return // 防重复开启（onMounted 后 onActivated 会再触发一次）
  pollTimer = setInterval(refreshStatus, 2500) // 状态兜底轮询（事件推送之外）
  tickTimer = setInterval(() => {
    if (snap.value?.state === 'running' && snap.value.startedAt) {
      const started = new Date(snap.value.startedAt).getTime()
      if (!Number.isNaN(started)) {
        uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
      }
    }
  }, 1000)
}

function stopTimers() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (tickTimer) { clearInterval(tickTimer); tickTimer = null }
  uptimeSec.value = 0
}

// ---------- 联动开关 / 端口 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')
const listenPort = ref(9876)
const portInput = ref(9876)
const portDirty = computed(() => String(portInput.value) !== String(listenPort.value))

async function loadExtras() {
  try {
    const [f, u, p] = await Promise.all([DdnsGoAPI.GetFollowOnExit(), DdnsGoAPI.RepositoryURL(), DdnsGoAPI.GetListenPort()])
    followOnExit.value = f
    repoUrl.value = u
    listenPort.value = p
    portInput.value = p
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  try {
    await DdnsGoAPI.SetFollowOnExit(!followOnExit.value)
    showToast(followOnExit.value
      ? '已开启：Hanxi 退出时一并关闭 ddns-go'
      : '已关闭：Hanxi 退出不影响 ddns-go，继续独立运行（下次启动生效）')
  } catch (e) {
    showToast('设置失败: ' + getErrorMessage(e))
    followOnExit.value = !followOnExit.value // 失败回滚
  }
}

async function applyPort() {
  const n = Number(portInput.value)
  if (!Number.isInteger(n) || n < 1024 || n > 65535) {
    showToast('端口需为 1024~65535 的整数')
    return
  }
  try {
    const res = await DdnsGoAPI.SetListenPort(n)
    listenPort.value = n
    showToast(res === 'pending'
      ? `端口已设为 ${n}（当前运行实例不变，下次启动生效）`
      : `监听端口已设为 ${n}`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
    portInput.value = listenPort.value
  }
}

async function copyRepo() {
  const text = repoUrl.value
  const fallback = (): boolean => {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  }
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
    } else if (!fallback()) {
      throw new Error('execCommand 不可用')
    }
  } catch (e) {
    showToast('复制失败: ' + getErrorMessage(e))
    return
  }
  showToast('仓库地址已复制')
}

async function openRepo() {
  try {
    await DdnsGoAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 生命周期 ----------
onMounted(async () => {
  unlistenDownload = Events.On('ddnsgo:version-download', (event: any) => {
    const t = event.data as DownloadProgress
    if (!t || !t.version) return
    downloading.value = { ...downloading.value, [t.version]: t }
    if (t.stage === 'done') {
      setTimeout(() => {
        const next = { ...downloading.value }
        delete next[t.version]
        downloading.value = next
      }, 800)
      loadVersions()
    }
  })

  unlistenState = Events.On('ddnsgo:instance-state', (event: any) => {
    const s = event.data as Snapshot
    if (!s) return
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
  })

  unlistenLog = Events.On('ddnsgo:instance-log', (event: any) => {
    const entry = event.data as { line?: string }
    if (entry?.line) appendLog(entry.line)
  })

  await Promise.all([refreshStatus(), loadVersions(), loadExtras(), loadLogHistory()])
})

// KeepAlive：页面激活时恢复轮询并立即刷新一帧，退后台时暂停避免空转
onActivated(() => {
  startTimers()
  refreshStatus()
})

onDeactivated(() => {
  stopTimers()
})

onUnmounted(() => {
  stopTimers()
  if (unlistenDownload) unlistenDownload()
  if (unlistenState) unlistenState()
  if (unlistenLog) unlistenLog()
})

// 状态从非运行切到 running 时清空旧进程日志（引擎环形缓冲同点重启）
watch(() => snap.value?.state, (now, prev) => {
  if (now === 'running' && prev !== 'starting' && prev !== 'running') {
    void loadLogHistory()
  }
})
</script>

<template>
  <section class="page ddnsgo-view">
    <div class="header-row">
      <div>
        <h1>ddns-go</h1>
        <p class="subtitle">托管动态域名解析工具：版本管理、JobObject 启停与内嵌 Web 控制台。</p>
      </div>
      <div class="main-tab-nav">
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'console' }"
          @click="activeMainTab = 'console'"
        >
          🌐 控制台
        </button>
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'versions' }"
          @click="activeMainTab = 'versions'"
        >
          📦 版本管理
        </button>
      </div>
    </div>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <!-- 顶部整合条：状态 + 控制按钮，一行内解决问题 -->
      <div class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <span class="dd-status-light" :class="state"></span>
            <span class="status-word">{{ stateText }}</span>
            <template v-if="isRunningOrStarting && runningVersion">
              <span class="ver-pill">{{ runningVersion }}</span>
              <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
            </template>
            <span v-if="state === 'running' && listenAddr" class="mono addr-tag">🖥 {{ listenAddr }}</span>
            <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
          </div>
          <div class="control-btns">
            <button
              class="btn btn-secondary btn-small"
              :disabled="busy || state === 'starting'"
              :title="state === 'running' ? '已运行，无需重复启动' : '后台拉起 ddns-go（不打开面板）'"
              @click="startDdns"
            >▶ 启动</button>
            <button
              class="btn btn-primary btn-small"
              :disabled="busy || (!isRunningOrStarting && !isExternal && state !== 'stopped' && state !== 'failed')"
              title="打开内嵌 Web 控制台（未运行时先自动启动）"
              @click="openConsole"
            >🖥 打开控制台</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部实例请在其面板或 Windows 服务中退出' : '终止托管实例（含配置写静默期保护）'"
              @click="quitDdns"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <!-- 条件提示条 / 引导行 -->
      <div v-if="banner" class="hint-banner slim" :class="banner.cls">{{ banner.text }}</div>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「打开控制台」启动并在窗口内配置 DNS 服务商与域名；域名配置恒存 ~/.ddns_go_config.yaml，与你自行运行的 ddns-go 无缝共享。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 ddns-go 并等待 Web 端口就绪…</div>

      <!-- 进程输出日志面板 -->
      <div class="dd-log-card">
        <div class="dd-log-head">
          <span class="dd-log-title">进程输出（更新动态 / 错误）</span>
          <div class="dd-log-tools">
            <label class="dd-auto-scroll"><input v-model="logAutoScroll" type="checkbox" />自动滚动</label>
            <button class="btn btn-secondary btn-small" @click="clearLogs">清屏</button>
          </div>
        </div>
        <div
          ref="logBodyRef"
          class="dd-log-body"
          @scroll.passive="logAutoScroll = ($event.target as HTMLElement).scrollTop + ($event.target as HTMLElement).clientHeight >= ($event.target as HTMLElement).scrollHeight - 40"
        >
          <template v-if="logLines.length">
            <div v-for="(line, i) in logLines" :key="i" class="dd-log-line" :class="{ 'dd-log-warn': logWarnish(line) }">{{ line }}</div>
          </template>
          <div v-else class="dd-log-empty">{{ state === 'running' ? '暂无输出（DDNS 按周期更新，静默即正常）' : '实例未运行，无进程输出' }}</div>
        </div>
      </div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 ddns-go</summary>
        <div class="info-body">
          <p>开源动态域名解析工具（<a class="inline-link" href="https://github.com/jeessy2/ddns-go" target="_blank" rel="noopener">jeessy2/ddns-go</a>，MIT）：家用宽带无固定公网 IP 时，自动把最新 IP 同步到阿里云 / DNSPod / Cloudflare 等域名解析，支持 IPv4/IPv6 与十余家服务商。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
          <p class="hint-dim">安全边界：Hanxi 托管实例固定绑定 127.0.0.1（仅本机可访问面板）；需要把面板暴露到局域网请自行运行原版。首次使用请在面板设置用户名/密码。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出不影响 ddns-go，继续独立解析）</span></span>
        </label>
        <span class="port-ctrl">
          <span class="hint-dim">Web 监听端口</span>
          <input v-model.number="portInput" class="dd-port-input mono" type="number" min="1024" max="65535" />
          <button class="btn btn-secondary btn-small" :disabled="!portDirty || busy" @click="applyPort">应用</button>
        </span>
      </div>
      <div class="repo-row">
        <span class="k">GitHub 仓库</span>
        <code class="mono repo-addr">{{ repoUrl }}</code>
        <button class="link-btn" @click="copyRepo">复制</button>
        <button class="link-btn" @click="openRepo">浏览器打开</button>
      </div>
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程版本 {{ releases.length }} 个</span>
          <span class="hint-dim">便携包下载自 GitHub Releases（ddns-go_*_windows_x86_64.zip，官方 digest 校验）；或「导入本地」把你机器上已有的解压目录收纳进来</span>
          <span class="hint-dim">域名配置（~/.ddns_go_config.yaml）各版本共享，升级/切换版本不影响现有解析设置</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地安装</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

      <div v-if="installed.length === 0" class="empty-state first-use">
        <p>尚未安装 ddns-go —— 下载官方 Windows x64 包，或「导入本地」把现有解压目录收纳进来</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
          下载最新版 {{ releases[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </div>

      <div class="installed-grid">
        <div v-for="v in installed" :key="v.version" class="installed-card" :class="{ 'card-active': activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <span v-if="activeVersion === v.version" class="badge badge-active">使用中</span>
              <span v-else-if="state === 'running' && runningVersion === v.version" class="badge badge-running">运行中</span>
              <span v-if="v.isImport" class="badge badge-import">本地导入</span>
              <span v-else class="badge badge-official">官方下载</span>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
            <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
            <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
          </div>
          <div class="inst-actions">
            <button v-if="activeVersion !== v.version" class="btn btn-primary btn-small" @click="setActive(v)">设为使用</button>
            <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 打开位置</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="state === 'running' && runningVersion === v.version"
              :title="state === 'running' && runningVersion === v.version ? '请先退出 ddns-go' : ''"
              @click="removeVersion(v)"
            >卸载</button>
          </div>
        </div>
      </div>

      <!-- 远程可用版本 -->
      <div class="section-title"><h3>远程可用版本</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 140px;">版本</th>
              <th style="width: 170px;">状态</th>
              <th style="width: 90px;">大小</th>
              <th style="width: 110px;">发布时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rel in releases" :key="rel.version">
              <td>
                <strong class="ver-name">{{ rel.version }}</strong>
                <span v-if="rel.isPre" class="badge badge-pre">预发布</span>
              </td>
              <td>
                <!-- 类名刻意带 dd- 前缀——App.vue 全局样式有 .status-dot（7px），防碰撞压扁徽标（markeron 垂直字体事故教训） -->
                <span v-if="statusOf(rel) === 'installed'" class="dd-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="dd-ver-status downloading">下载中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="dd-ver-status error">失败</span>
                <span v-else class="dd-ver-status idle">可安装</span>
              </td>
              <td>{{ fmtSize(rel.size) }}</td>
              <td>{{ fmtDate(rel.published) }}</td>
              <td>
                <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="download-cell">
                  <div class="dl-bar-wrap">
                    <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
                  </div>
                  <span class="dl-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
                </div>
                <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
                  <span v-if="['verify', 'extract'].includes(downloading[rel.version]!.stage)">校验解压安装…</span>
                  <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
                </div>
                <button
                  v-if="statusOf(rel) === 'idle'"
                  class="btn btn-primary btn-small"
                  @click="download(rel)"
                >下载安装</button>
                <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small">已安装</span>
                <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="download(rel)">重试</a>
              </td>
            </tr>
            <tr v-if="releases.length === 0 && !loading">
              <td colspan="5" class="empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.ddnsgo-view { display: flex; flex-direction: column; gap: 10px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 顶层主选项卡（与 CCSwitchView 同款） */
.main-tab-nav {
  display: flex;
  background: var(--bg-hover);
  padding: 3px;
  border-radius: 8px;
  gap: 2px;
}
.main-tab-btn {
  background: transparent;
  border: none;
  padding: 6px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}
.main-tab-btn:hover { color: var(--text-main); }
.main-tab-btn.active {
  background: var(--bg-app);
  color: var(--accent);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 dd- 前缀，与全局样式隔离（markeron 垂直字体事故教训） */
.dd-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--text-subtle); flex-shrink: 0; }
.dd-status-light.running { background: var(--success); box-shadow: 0 0 0 3px rgba(26, 127, 55, 0.15); }
.dd-status-light.starting { background: var(--accent); animation: pulse 1s infinite; }
.dd-status-light.external { background: #9a6700; box-shadow: 0 0 0 3px rgba(154, 103, 0, 0.15); }
.dd-status-light.failed { background: var(--danger); box-shadow: 0 0 0 3px rgba(207, 34, 46, 0.15); }
.status-word { font-size: 15px; font-weight: 700; color: var(--text-main); }
.ver-pill { font-family: Consolas, monospace; font-size: 12px; background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 4px; padding: 1px 8px; color: var(--text-main); }
.pid-tag { font-size: 11px; color: var(--text-subtle); }
.addr-tag { font-size: 11px; color: var(--text-subtle); }
.uptime-tag { font-size: 11px; color: var(--text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示条与说明卡 ---------- */
.hint-banner { padding: 10px 14px; border-radius: 6px; font-size: 13px; border: 1px solid transparent; }
.hint-banner.slim { padding: 8px 12px; font-size: 12px; }
.banner-warn { background: #fff8c5; border-color: rgba(191, 135, 0, 0.3); color: #9a6700; }
.banner-error { background: #ffebe9; border-color: rgba(207, 34, 46, 0.25); color: var(--danger); }
.banner-ok { background: #dafbe1; border-color: rgba(26, 127, 55, 0.2); color: #1a7f37; }
.hint-line { font-size: 12px; color: var(--text-subtle); padding-left: 2px; }
.info-details { border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-sidebar); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform 0.15s; }
.info-details[open] .info-summary { border-bottom: 1px solid var(--border-color); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--accent); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 进程输出日志面板（frpc 日志抽屉同款深底，类名带 dd- 前缀） ---------- */
.dd-log-card { border: 1px solid #334155; border-radius: 8px; overflow: hidden; }
.dd-log-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 7px 12px; background: #1e293b; border-bottom: 1px solid #334155;
}
.dd-log-title { font-size: 12px; font-weight: 600; color: #f1f5f9; }
.dd-log-tools { display: flex; align-items: center; gap: 8px; }
.dd-auto-scroll { display: flex; align-items: center; gap: 4px; font-size: 12px; color: #94a3b8; cursor: pointer; }
.dd-auto-scroll input { accent-color: var(--accent); }
.dd-log-body {
  height: 180px; overflow-y: auto; padding: 10px 12px; background: #0f172a;
  font-family: Consolas, monospace; font-size: 12px; line-height: 1.55;
  user-select: text;
}
.dd-log-line { white-space: pre-wrap; word-break: break-all; color: #cbd5e1; }
.dd-log-warn { color: #fde047; }
.dd-log-empty { color: #64748b; text-align: center; padding: 28px 0; }

/* ---------- 通用按钮 ---------- */
.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn-ghost { background: #f0f7ff; color: #0969da; border-color: #c8e1ff; }
.btn-danger-outline { background: #fff; border-color: #ff8170; color: var(--danger); }
.btn-danger-outline:hover:not(:disabled) { background: #ffebe9; }

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--bg-sidebar); border: 1px solid var(--border-color); padding: 10px 14px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--text-main); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--text-subtle); font-size: 13px; background: var(--bg-sidebar); border-radius: 6px; border: 1px dashed var(--border-color); }

/* 首次使用空态 */
.empty-state {
  text-align: center; padding: 24px; background: var(--bg-sidebar);
  border: 1px dashed var(--border-color); border-radius: 8px;
  display: flex; flex-direction: column; gap: 12px; align-items: center;
}
.empty-state p { margin: 0; color: var(--text-muted); font-size: 13px; }

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color 0.15s ease;
}
.installed-card.card-active { border-color: var(--accent); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: Consolas, monospace; font-size: 14px; font-weight: 700; color: var(--text-main); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
.badge-active { background: #dafbe1; color: #1a7f37; }
.badge-running { background: #ddf4ff; color: #0969da; }
.badge-import { background: #ddf4ff; color: #0969da; }
.badge-official { background: #eaeef2; color: #656d76; }
.badge-pre { background: #fff8c5; color: #9a6700; margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--text-muted); align-items: baseline; }
.meta-line .k { color: var(--text-subtle); width: 44px; flex-shrink: 0; }
.mono { font-family: Consolas, monospace; color: var(--text-main); font-size: 11px; word-break: break-all; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: #fff; border: 1px solid var(--border-color); border-radius: 8px; overflow-x: auto; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
.tbl th { background: var(--bg-sidebar); padding: 8px 12px; font-weight: 600; color: var(--text-muted); font-size: 12px; border-bottom: 1px solid var(--border-color); }
.tbl td { padding: 8px 12px; border-bottom: 1px solid var(--border-color); vertical-align: middle; }
.tbl tr:last-child td { border-bottom: none; }
.ver-name { font-family: Consolas, monospace; }

.dd-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.dd-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.dd-ver-status.installed::before { background: #2da44e; }
.dd-ver-status.downloading::before { background: #0969da; animation: pulse 1s infinite; }
.dd-ver-status.error::before { background: #cf222e; }
.dd-ver-status.idle::before { background: #8c959f; }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: #e1e4e8; border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--accent); }
.dl-error { color: var(--danger); font-size: 11px; }
.retry-link { color: var(--accent); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-main); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.port-ctrl { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.dd-port-input { width: 84px; padding: 4px 8px; border: 1px solid var(--border-color); border-radius: 6px; background: #fff; font-size: 12px; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
.link-btn { background: transparent; border: none; color: var(--accent); font-size: 12px; cursor: pointer; padding: 0 2px; }
.link-btn:hover { text-decoration: underline; }

@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>
