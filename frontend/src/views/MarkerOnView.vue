<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated } from 'vue'
import { Events } from '@wailsio/runtime'
import * as MarkerAPI from '../../bindings/hubkit/internal/modules/markeron/markeronservice'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { DownloadProgress, MarkerRelease, MarkerVersionInfo } from '../../bindings/hubkit/internal/modules/markeron/version/models'
import type { Snapshot } from '../../bindings/hubkit/internal/modules/markeron/instance/models'
import type { ToggleOutcome, StopOutcome } from '../../bindings/hubkit/internal/modules/markeron/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<MarkerRelease[]>([])
const installed = ref<MarkerVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const errorMsg = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map: version -> progress
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()

// 顶层主选项卡：annotate = 标注开关，versions = 版本管理（与 frpc 的 projects/versions 同构）
const activeMainTab = ref<'annotate' | 'versions'>('annotate')

let unlistenDownload: (() => void) | null = null
let unlistenState: (() => void) | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')
const drawing = computed(() => !!snap.value?.drawing)

const stateText = computed(() => {
  switch (state.value) {
    case 'running': return drawing.value ? '标注已开启' : '已启动（未标注）'
    case 'starting': return '启动中…'
    case 'failed': return '异常退出'
    case 'external': return '外部运行'
    default: return '未运行'
  }
})

// 主按钮六态矩阵（状态 × 文案 × 样式，见实施计划 §8.2）
const toggleLabel = computed(() => {
  if (busy.value) return '处理中…'
  switch (state.value) {
    case 'starting': return '启动中…'
    case 'running': return drawing.value ? '退出标注' : '开启标注'
    case 'external': return '切换标注'
    case 'failed': return '重试启动'
    default: return '启动 MarkerOn'
  }
})

const toggleSubLabel = computed(() => {
  if (busy.value) return '正在切换标注状态…'
  switch (state.value) {
    case 'running': return drawing.value ? '桌面覆盖层已开启，点击关闭' : 'MarkerOn 已在后台运行'
    case 'external': return '外部实例状态未知，点击切换'
    case 'failed': return '上次异常退出，点击重新启动'
    case 'starting': return '等待 MarkerOn 就绪（≤20 秒）'
    default: return '后台静默运行，不进入标注'
  }
})

const toggleClass = computed(() => {
  switch (state.value) {
    case 'running': return drawing.value ? 'btn-toggle-active' : 'btn-toggle-outline'
    case 'external': return 'btn-toggle-warn'
    case 'failed': return 'btn-toggle-danger'
    default: return 'btn-toggle-primary'
  }
})

const toggleDisabled = computed(() =>
  busy.value || state.value === 'starting' || (isExternal.value && installed.value.length === 0))

const toggleHint = computed(() => {
  if (isExternal.value && installed.value.length === 0) {
    return '外部实例无法代为切换（无可用信使程序），请使用快捷键 Ctrl+Shift+D'
  }
  return ''
})

// 条件提示条（三个变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return { cls: 'banner-warn', text: '检测到外部 MarkerOn 实例（非 HubKit 托管）。可切换标注；如需彻底退出请在 MarkerOn 托盘操作。' }
  }
  if (state.value === 'failed') {
    return { cls: 'banner-error', text: snap.value?.error || 'MarkerOn 异常退出' }
  }
  if (state.value === 'running' && drawing.value) {
    return { cls: 'banner-ok', text: '标注已开启，可将屏幕涂画演示；再次点击开关或按 Ctrl+Shift+D 退出。' }
  }
  return null
})

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = state.value === 'running' && snap.value?.version ? snap.value.version : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

// 远程最新稳定版（首个非预发布）
const latestVersion = computed(() => releases.value.find(r => !r.isPre)?.version ?? '')

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  errorMsg.value = ''
  try {
    const [remote, local, active] = await Promise.all([
      MarkerAPI.ListReleases(),
      MarkerAPI.ListInstalledVersions(),
      MarkerAPI.GetActiveVersion(),
    ])
    releases.value = remote ?? []
    installed.value = local ?? []
    activeVersion.value = active ?? ''
  } catch (e) {
    errorMsg.value = `获取版本列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function refreshStatus() {
  try {
    snap.value = await MarkerAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('markeron GetStatus failed:', getErrorMessage(e))
  }
}

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

function statusOf(rel: MarkerRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version.replace(/^v/, '') === rel.version.replace(/^v/, ''))
  return hit ? 'installed' : 'idle'
}

// ---------- 操作 ----------
async function toggleAnnotate() {
  if (toggleDisabled.value || busy.value) return
  busy.value = true
  try {
    const out: ToggleOutcome = await MarkerAPI.ToggleAnnotate()
    showToast(out.message)
    await Promise.all([refreshStatus(), loadVersions()])
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function stopAnnotate() {
  busy.value = true
  try {
    const out: StopOutcome = await MarkerAPI.StopAnnotate()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`停止失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// 下载最新稳定版（首次使用空态入口；渲染与点击之间远程列表可能刷新，故动态查找）
function downloadLatest() {
  const target = releases.value.find(r => r.version === latestVersion.value)
  if (!target) return
  download(target)
}

async function download(rel: MarkerRelease) {
  try {
    const res = await MarkerAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: MarkerVersionInfo) {
  try {
    const ver = await MarkerAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

// 打开安装目录：必须传「目录」路径而非 exe——explorer.exe 收文件参数会执行文件
// （对 MarkerOn.exe 会直接拉起实例），收目录才稳定打开文件夹窗口
async function openDir(path: string) {
  try {
    await AppAPI.AppService.OpenPath(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: MarkerVersionInfo) {
  const versionShort = v.version.replace(/^v/, '')
  if (!window.confirm(`确定卸载 MarkerOn ${versionShort}？\n该版本隔离目录及其便携数据将被删除，不可恢复。`)) return
  try {
    await MarkerAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${versionShort}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
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

// ---------- 生命周期 ----------
onMounted(async () => {
  unlistenDownload = Events.On('markeron:version-download', (event: any) => {
    const p = event.data as DownloadProgress
    if (!p || !p.version) return
    downloading.value = { ...downloading.value, [p.version]: p }
    if (p.stage === 'done') {
      setTimeout(() => {
        const next = { ...downloading.value }
        delete next[p.version]
        downloading.value = next
      }, 800)
      loadVersions()
    }
  })

  unlistenState = Events.On('markeron:instance-state', (event: any) => {
    const s = event.data as Snapshot
    if (!s) return
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
  })

  await Promise.all([refreshStatus(), loadVersions()])
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
})
</script>

<template>
  <section class="page markeron-view">
    <div class="header-row">
      <div>
        <h1>MarkerOn 桌面标注</h1>
        <p class="subtitle">一键进入桌面标注态；版本与运行状态统一管理。</p>
      </div>
      <div class="main-tab-nav">
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'annotate' }"
          @click="activeMainTab = 'annotate'"
        >
          ✎ 标注开关
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

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 标注开关 Tab -->
    <div v-show="activeMainTab === 'annotate'" class="tab-body">
    <!-- 标注状态卡（核心） -->
    <div class="annotate-card">
      <!-- 左：开关按钮 + 副操作 -->
      <div class="action-zone">
        <button
          class="btn-toggle-big"
          :class="toggleClass"
          :disabled="toggleDisabled"
          :title="toggleHint"
          @click="toggleAnnotate"
        >
          <span class="toggle-icon">✎</span>
          <span class="toggle-main">{{ toggleLabel }}</span>
          <span class="toggle-sub">{{ toggleSubLabel }}</span>
        </button>
        <div class="action-row">
          <button
            v-if="isRunningOrStarting"
            class="btn btn-secondary btn-small"
            :disabled="busy"
            @click="stopAnnotate"
          >停止标注</button>
          <button
            v-if="openDirVersion"
            class="btn btn-secondary btn-small"
            @click="openDir(openDirVersion!.dir)"
          >打开安装目录</button>
        </div>
      </div>

      <!-- 右：详情区 -->
      <div class="detail-zone">
        <div class="status-badge-row">
          <span class="status-light" :class="state"></span>
          <span class="status-word">{{ stateText }}</span>
          <template v-if="isRunningOrStarting && snap?.version">
            <span class="ver-pill">v{{ snap.version }}</span>
            <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
          </template>
        </div>

        <div class="detail-line" v-if="state === 'running'">
          已运行 <strong>{{ fmtDuration(uptimeSec) }}</strong>
          <span v-if="!drawing" class="hint-dim">—— 后台待命，点击「开启标注」显示覆盖层</span>
        </div>
        <div class="detail-line" v-else-if="state === 'stopped'">
          点击「启动 MarkerOn」后台运行；随后用「开启标注」或 Ctrl+Shift+D 进入标注态
        </div>
        <div class="detail-line" v-else-if="state === 'starting'">
          正在拉起 MarkerOn 主实例（约 1~3 秒）
        </div>
        <div class="detail-line" v-else-if="state === 'failed'">
          请查看上方提示条；确认已安装 WebView2 Runtime 后重试
        </div>
        <div class="detail-line" v-else-if="state === 'external'">
          非 HubKit 托管的 MarkerOn 实例正在运行
        </div>

        <div class="kbd-row">
          <span class="kbd-chip">Ctrl+Shift+D</span> 切换标注 ·
          <span class="kbd-chip">Ctrl+Shift+C</span> 清空涂鸦 ·
          <span class="kbd-chip">Ctrl+Shift+X</span> 穿透点击
        </div>
        <div class="hint-dim honesty-hint">按钮与快捷键等效；若状态与桌面实际不符（可能经快捷键直接操作过），再按一次开关即可。</div>
      </div>
    </div>

    <!-- 条件提示条 -->
    <div v-if="banner" class="hint-banner" :class="banner.cls">{{ banner.text }}</div>
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
    <!-- 版本区 -->
    <div class="control-panel">
      <div class="meta-info">
        <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程可用 {{ releases.length }} 个版本</span>
        <span class="hint-dim">便携包下载自上游 ifer47/markeron；SmartScreen 拦截时选「更多信息 → 仍要运行」</span>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
          {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
        </button>
      </div>
    </div>

    <!-- 已安装版本 -->
    <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

    <!-- 首次使用空态：一键下载最新稳定版 -->
    <div v-if="installed.length === 0 && latestVersion" class="empty-state first-use">
      <p>尚未安装 MarkerOn —— 下载官方便携版后即可一键标注</p>
      <button class="btn btn-primary" @click="downloadLatest()">
        下载最新版 {{ latestVersion }}
      </button>
    </div>
    <div v-else-if="installed.length === 0" class="empty-hint">
      尚未安装任何 MarkerOn 版本，且远程列表暂不可用——请稍后点击「↻ 刷新远程列表」重试
    </div>

    <div class="installed-grid">
      <div v-for="v in installed" :key="v.version" class="installed-card" :class="{ 'card-active': activeVersion === v.version }">
        <div class="inst-card-top">
          <span class="ver-tag">{{ v.version }}</span>
          <div class="inst-badges">
            <span v-if="activeVersion === v.version" class="badge badge-active">使用中</span>
            <span v-else-if="state === 'running' && snap?.version === v.version" class="badge badge-running">运行中</span>
            <span v-else class="badge badge-official">官方下载</span>
          </div>
        </div>
        <div class="inst-meta">
          <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
          <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
          <div class="meta-line" v-if="v.sha256"><span class="k">SHA256</span><code class="mono short">{{ v.sha256.slice(0, 12) }}…</code></div>
        </div>
        <div class="inst-actions">
          <button v-if="activeVersion !== v.version" class="btn btn-primary btn-small" @click="setActive(v)">设为使用</button>
          <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 打开位置</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="state === 'running' && snap?.version === v.version"
            :title="state === 'running' && snap?.version === v.version ? '请先停止标注' : ''"
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
            <th style="width: 130px;">版本</th>
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
              <span v-if="rel.version === latestVersion" class="badge badge-newest">最新</span>
              <span v-if="rel.isPre" class="badge badge-pre">预发布</span>
            </td>
            <td>
              <!-- 注意：类名刻意用 ver-status 而非 status-dot——App.vue 全局样式里已有 .status-dot（7px 圆点），会压扁表格徽标 -->
              <span v-if="statusOf(rel) === 'installed'" class="ver-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="ver-status downloading">下载中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="ver-status error">失败</span>
              <span v-else class="ver-status idle">可安装</span>
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
                <span v-if="downloading[rel.version]!.stage === 'extract'">解压安装…</span>
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
            <td colspan="5" class="empty-hint">无法加载远程版本列表（GitHub 可能被限流），可稍后点击「↻ 刷新远程列表」重试</td>
          </tr>
        </tbody>
      </table>
    </div>
    </div>
  </section>
</template>

<style scoped>
.markeron-view { display: flex; flex-direction: column; gap: 14px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 顶层主选项卡（与 FrpcProjectsView 同款） */
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
.tab-body { display: flex; flex-direction: column; gap: 16px; }

/* ---------- 标注状态卡 ---------- */
.annotate-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px;
  padding: 20px; display: flex; gap: 28px; flex-wrap: wrap;
}

/* 左：大开关按钮 */
.action-zone { display: flex; flex-direction: column; gap: 10px; align-items: center; width: 220px; flex-shrink: 0; }
.btn-toggle-big {
  width: 200px; height: 96px; border-radius: 10px; border: 1px solid transparent;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px;
  cursor: pointer; transition: all 0.15s ease; position: relative;
}
.btn-toggle-big:disabled { opacity: 0.55; cursor: not-allowed; }
.toggle-icon { font-size: 20px; line-height: 1; }
.toggle-main { font-size: 16px; font-weight: 600; }
.toggle-sub { font-size: 11px; color: var(--text-muted); }

.btn-toggle-primary { background: var(--accent); color: #fff; }
.btn-toggle-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-toggle-primary .toggle-sub { color: rgba(255,255,255,.85); }

.btn-toggle-outline { background: #fff; border-color: var(--accent); color: var(--accent); }
.btn-toggle-outline:hover:not(:disabled) { background: var(--bg-active); }

.btn-toggle-active { background: var(--accent); color: #fff; box-shadow: 0 0 0 3px rgba(47, 111, 237, 0.15); }
.btn-toggle-active:hover:not(:disabled) { background: var(--accent-hover); }
.btn-toggle-active .toggle-sub { color: rgba(255,255,255,.85); }

.btn-toggle-warn { background: #fff8c5; border-color: #bf8700; color: #9a6700; }
.btn-toggle-warn:hover:not(:disabled) { background: #fff3a8; }

.btn-toggle-danger { background: #fff; border-color: var(--danger); color: var(--danger); }
.btn-toggle-danger:hover:not(:disabled) { background: #ffebe9; }

.action-row { display: flex; gap: 8px; }

/* 右：详情区 */
.detail-zone { flex: 1; min-width: 300px; display: flex; flex-direction: column; gap: 10px; justify-content: center; }
.status-badge-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 注意：此处是状态卡信号灯，类名刻意用 status-light，与远程表格徽标 status-dot 隔离——同页共用一类名会互相污染样式 */
.status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--text-subtle); flex-shrink: 0; }
.status-light.running { background: var(--success); box-shadow: 0 0 0 3px rgba(26, 127, 55, 0.15); }
.status-light.starting { background: var(--accent); animation: pulse 1s infinite; }
.status-light.external { background: #9a6700; box-shadow: 0 0 0 3px rgba(154, 103, 0, 0.15); }
.status-light.failed { background: var(--danger); box-shadow: 0 0 0 3px rgba(207, 34, 46, 0.15); }
.status-word { font-size: 16px; font-weight: 700; color: var(--text-main); }
.ver-pill { font-family: Consolas, monospace; font-size: 12px; background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 4px; padding: 1px 8px; color: var(--text-main); }
.pid-tag { font-size: 11px; color: var(--text-subtle); }

.detail-line { font-size: 13px; color: var(--text-muted); }
.detail-line strong { color: var(--text-main); font-family: Consolas, monospace; }

.kbd-row { font-size: 12px; color: var(--text-muted); display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.kbd-chip {
  font-family: Consolas, monospace; font-size: 11px; background: #fff;
  border: 1px solid var(--border-color); border-radius: 4px; padding: 2px 8px; color: var(--text-main);
}
.honesty-hint { font-size: 11px; }

/* ---------- 提示条 ---------- */
.hint-banner { padding: 10px 14px; border-radius: 6px; font-size: 13px; border: 1px solid transparent; }
.banner-warn { background: #fff8c5; border-color: rgba(191, 135, 0, 0.3); color: #9a6700; }
.banner-error { background: #ffebe9; border-color: rgba(207, 34, 46, 0.25); color: var(--danger); }
.banner-ok { background: #dafbe1; border-color: rgba(26, 127, 55, 0.2); color: #1a7f37; }

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
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 12px; }
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
.badge-official { background: #eaeef2; color: #656d76; }
.badge-newest { background: var(--bg-active); color: var(--accent); }
.badge-pre { background: #fff8c5; color: #9a6700; margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--text-muted); align-items: baseline; }
.meta-line .k { color: var(--text-subtle); width: 44px; flex-shrink: 0; }
.mono { font-family: Consolas, monospace; color: var(--text-main); font-size: 11px; word-break: break-all; }
.mono.short { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: #fff; border: 1px solid var(--border-color); border-radius: 8px; overflow-x: auto; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
.tbl th { background: var(--bg-sidebar); padding: 8px 12px; font-weight: 600; color: var(--text-muted); font-size: 12px; border-bottom: 1px solid var(--border-color); }
.tbl td { padding: 8px 12px; border-bottom: 1px solid var(--border-color); vertical-align: middle; }
.tbl tr:last-child td { border-bottom: none; }
.ver-name { font-family: Consolas, monospace; }

.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: #2da44e; }
.ver-status.downloading::before { background: #0969da; animation: pulse 1s infinite; }
.ver-status.error::before { background: #cf222e; }
.ver-status.idle::before { background: #8c959f; }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: #e1e4e8; border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--accent); }
.dl-error { color: var(--danger); font-size: 11px; }
.retry-link { color: var(--accent); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>