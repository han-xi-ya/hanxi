<script setup lang="ts">
// 状态 / 内嵌控制台入口 / 进程日志 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
// （骨架基于重构共享层；日志流面板与 Web 端口设置为 ddns-go 特有业务，原样保留）
import { ref, computed, onMounted, onDeactivated, nextTick, watch } from 'vue'
import * as DdnsGoAPI from '../../bindings/hanxi/internal/modules/ddnsgo/ddnsgoservice'
import type { DdnsRelease, DdnsVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/ddnsgo/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/ddnsgo/instance/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<DdnsRelease[]>([])
const installed = ref<DdnsVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const uptimeSec = ref(0)

const { busy, run } = useAsyncAction()
const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 下载进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

// 进程输出日志（事件流累积 + 挂载时拉最近 200 行）
const MAX_LOG_LINES = 400
const logLines = ref<string[]>([])
const logAutoScroll = ref(true)
const logBodyRef = ref<HTMLElement | null>(null)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/flclash 同构）
const activeMainTab = ref('console')
const mainTabs = [
  { key: 'console', label: '🌐 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 注：文案与 constants/status 的 TOOL_STATE_META 存在口径差（running 此族用「运行中」），
// 属托管家族既有话术，不强行套用单一表（迁移铁律 1），统一留待家族文案评审。
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
const banner = computed<{ tone: 'ok' | 'warn' | 'error'; text: string } | null>(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部 ddns-go 实例（自行启动或 Windows 服务，非 Hanxi 托管）。可直接打开其面板查看；托管启动需先退出外部实例。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'ddns-go 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
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
  const r = await run(() => DdnsGoAPI.Start())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await refreshStatus()
}

async function openConsole() {
  if (busy.value) return
  const r = await run(() => DdnsGoAPI.OpenConsole())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await refreshStatus()
}

async function quitDdns() {
  if (busy.value) return
  const r = await run(() => DdnsGoAPI.Quit())
  showToast(r.ok ? r.data.message : `退出失败: ${getErrorMessage(r.error)}`)
  await refreshStatus()
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
  const ok = await confirm({
    title: `卸载 ddns-go ${v.version}`,
    description: '该版本隔离目录将被删除，不可恢复。',
    details: [{ label: '域名配置', value: '~/.ddns_go_config.yaml 不受影响，后续版本继续共用' }],
    tone: 'danger',
    confirmLabel: '卸载',
  })
  if (!ok) return
  try {
    await DdnsGoAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地安装',
    label: 'ddns-go 所在目录完整路径',
    description: '含 ddns-go.exe 的官方下载解压目录即可。域名配置恒在 ~/.ddns_go_config.yaml，与 exe 位置无关',
  })
  if (!path) return
  const r = await run(() => DdnsGoAPI.ImportLocal(path.trim()))
  if (r.ok) {
    showToast(`已导入 ddns-go ${r.data.version}`)
    await loadVersions()
  } else {
    showToast(`导入失败: ${getErrorMessage(r.error)}`)
  }
}

// ---------- 时长 ticker 与轮询（usePolling 内置 KeepAlive 激活/停用契约） ----------
usePolling(refreshStatus, 2500) // 状态兜底轮询（事件推送之外）
usePolling(() => {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}, 1000)

// 停用即清零运行时长（对齐迁移前 stopTimers 语义）
onDeactivated(() => {
  uptimeSec.value = 0
})

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
  const next = !followOnExit.value
  followOnExit.value = next // 用户点击已将勾选框翻转，ref 同步跟进，保持绑定状态一致
  try {
    await DdnsGoAPI.SetFollowOnExit(next)
    showToast(next
      ? '已开启：Hanxi 退出时一并关闭 ddns-go'
      : '已关闭：Hanxi 退出不影响 ddns-go，继续独立解析')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
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
  if (await copy(repoUrl.value)) {
    showToast('仓库地址已复制')
  } else {
    showToast('复制失败')
  }
}

async function openRepo() {
  try {
    await DdnsGoAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件订阅（自动注销）与装载 ----------
useWailsEvent<DownloadProgress>('ddnsgo:version-download', (t) => {
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

useWailsEvent<Snapshot>('ddnsgo:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

useWailsEvent<{ line?: string }>('ddnsgo:instance-log', (entry) => {
  if (entry?.line) appendLog(entry.line)
})

onMounted(async () => {
  await Promise.all([refreshStatus(), loadVersions(), loadExtras(), loadLogHistory()])
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
    <PageHeader
      title="ddns-go"
      subtitle="托管动态域名解析工具：版本管理、JobObject 启停与内嵌 Web 控制台。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

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
      <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
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
        <button class="link-button" @click="copyRepo">复制</button>
        <button class="link-button" @click="openRepo">浏览器打开</button>
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

      <UiEmptyState v-if="installed.length === 0">
        <p>尚未安装 ddns-go —— 下载官方 Windows x64 包，或「导入本地」把现有解压目录收纳进来</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
          下载最新版 {{ releases[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </UiEmptyState>

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
                <!-- 类名刻意带 dd- 前缀——防与全局/其他视图状态点样式碰撞（markeron 垂直字体事故教训） -->
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
                <span v-if="statusOf(rel) === 'installed'" class="installed-tag">已安装</span>
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
/* 共享层已接管：.page/.header-row/.subtitle/.error-box/.btn 家族/.tbl/.mono/.link-button/
   .empty-state/.banner(UiBanner)/.main-tab-nav(MainTabNav)——此处只留本视图业务样式。 */
.ddnsgo-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 dd- 前缀，与全局样式隔离（markeron 垂直字体事故教训） */
.dd-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.dd-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.dd-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.dd-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.dd-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.addr-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示行与说明卡 ---------- */
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }
.info-details { border: 1px solid var(--color-border); border-radius: 8px; background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 进程输出日志面板（终端态：固定深底不随主题反相，tokens.css 设计决策；
   色阶由 --terminal-* 与 --ansi-* 派生，不再散落裸色） ---------- */
.dd-log-card { border: 1px solid color-mix(in srgb, var(--terminal-fg) 22%, transparent); border-radius: 8px; overflow: hidden; }
.dd-log-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 7px 12px; background: color-mix(in srgb, var(--terminal-fg) 8%, var(--terminal-bg));
  border-bottom: 1px solid color-mix(in srgb, var(--terminal-fg) 22%, transparent);
}
.dd-log-title { font-size: 12px; font-weight: 600; color: var(--terminal-fg); }
.dd-log-tools { display: flex; align-items: center; gap: 8px; }
.dd-auto-scroll { display: flex; align-items: center; gap: 4px; font-size: 12px; color: color-mix(in srgb, var(--terminal-fg) 65%, transparent); cursor: pointer; }
.dd-auto-scroll input { accent-color: var(--color-primary); }
.dd-log-body {
  height: 180px; overflow-y: auto; padding: 10px 12px; background: var(--terminal-bg);
  font-family: var(--font-mono); font-size: 12px; line-height: 1.55;
  user-select: text;
}
.dd-log-line { white-space: pre-wrap; word-break: break-all; color: color-mix(in srgb, var(--terminal-fg) 88%, transparent); }
.dd-log-warn { color: var(--ansi-3); }
.dd-log-empty { color: var(--ansi-8); text-align: center; padding: 28px 0; }

/* ---------- 版本区（业务专属壳） ---------- */
.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: 6px; border: 1px dashed var(--color-border); }

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color var(--motion-base) ease;
}
.installed-card.card-active { border-color: var(--color-primary); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.dd-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.dd-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.dd-ver-status.installed::before { background: var(--state-positive); }
.dd-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.dd-ver-status.error::before { background: var(--state-danger); }
.dd-ver-status.idle::before { background: var(--color-text-subtle); }

/* 「已安装」表内标记：区别于全局 .btn-ghost（悬停幽灵按钮）的静态标签态 */
.installed-tag {
  display: inline-flex; align-items: center; padding: 4px 12px; font-size: 12px;
  border-radius: var(--radius-control); border: 1px solid var(--color-border);
  background: var(--surface-hover); color: var(--color-text-muted);
}

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.retry-link { color: var(--color-primary); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.port-ctrl { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.dd-port-input { width: 84px; padding: 4px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); font-size: 12px; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
</style>
