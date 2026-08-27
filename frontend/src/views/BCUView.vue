<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated } from 'vue'
import { Events } from '@wailsio/runtime'
import * as BCUAPI from '../../bindings/hubkit/internal/modules/bcu/bcuservice'
import type { BCURelease, BCUVersionInfo } from '../../bindings/hubkit/internal/modules/bcu/version/models'
import type { Snapshot } from '../../bindings/hubkit/internal/modules/bcu/instance/models'
import type { ControlOutcome, QuitOutcome, DotnetEnv } from '../../bindings/hubkit/internal/modules/bcu/models'
import type { DownloadProgress } from '../../bindings/hubkit/internal/modules/bcu/version/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<BCURelease[]>([])
const installed = ref<BCUVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map（版本+变体复合索引，防同版本双变体互相覆盖）
const downloading = ref<Record<string, DownloadProgress>>({})

// .NET 桌面运行时环境（框架依赖变体的可用性与推荐依据）
const dotnetEnv = ref<DotnetEnv | null>(null)
const envLoading = ref(false)

// 推荐变体：有 .NET 8 桌面运行时 → 推荐精简版（省 ~60MB）；
// 无 → 推荐自包含便携版（免依赖永远能跑）。null 环境未加载完毕。
const recommendedVariant = computed<'portable' | 'fdd' | null>(() => {
  if (!dotnetEnv.value) return null
  return dotnetEnv.value.hasNet8 ? 'fdd' : 'portable'
})

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与各工具模块同构）
const activeMainTab = ref<'console' | 'versions'>('console')

let unlistenDownload: (() => void) | null = null
let unlistenState: (() => void) | null = null
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

// 条件提示条（三个变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      cls: 'banner-warn',
      text: '检测到外部 BCUninstaller 实例（非 HubKit 托管）。可唤起其窗口；如需彻底退出请在 BCU 窗口内关闭。',
    }
  }
  if (state.value === 'failed') {
    return { cls: 'banner-error', text: snap.value?.error || 'BCU 异常退出' }
  }
  if (state.value === 'running') {
    return {
      cls: 'banner-ok',
      text: 'BCU 正在运行：批量卸载在其窗口内完成，设置数据（BCUninstaller.settings）保存在版本目录内。',
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
      BCUAPI.ListReleases(),
      BCUAPI.ListInstalledVersions(),
      BCUAPI.GetActiveVersion(),
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
    snap.value = await BCUAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('bcu GetStatus failed:', getErrorMessage(e))
  }
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

function statusOf(rel: BCURelease, variant: 'portable' | 'fdd'): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[`${rel.version}|${variant}`]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  // 同版本任一形态已装则该版本整体视为已装（目录共享）
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// 状态列的整体判定：任一形态 downloading/error 即反映（双变体并存场景）
const VARIANTS = ['portable', 'fdd'] as const

function statusOverall(rel: BCURelease): 'installed' | 'downloading' | 'error' | 'idle' {
  for (const v of VARIANTS) {
    const s = statusOf(rel, v)
    if (s === 'downloading' || s === 'error') return s
  }
  return statusOf(rel, 'portable')
}

async function loadDotnetEnv() {
  envLoading.value = true
  try {
    dotnetEnv.value = await BCUAPI.GetDotnetEnvironment()
  } catch (e) {
    console.warn('bcu GetDotnetEnvironment failed:', getErrorMessage(e))
  } finally {
    envLoading.value = false
  }
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await BCUAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitBCU() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await BCUAPI.Quit()
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
function variantLabel(variant: string): string {
  return variant === 'fdd' ? '精简版' : '便携版'
}

async function download(rel: BCURelease, variant: string) {
  try {
    const res = await BCUAPI.DownloadVersion(rel.version, variant)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

// 列内推荐变体的下载按钮样式与提示
function isRecommended(rel: BCURelease, variant: string): boolean {
  return recommendedVariant.value === variant && !!rel.fddName
}

async function setActive(v: BCUVersionInfo) {
  try {
    const ver = await BCUAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await BCUAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: BCUVersionInfo) {
  if (!window.confirm(`确定卸载 BCU ${v.version}？\n该版本隔离目录（含卸载历史与设置数据）将被删除，不可恢复。`)) return
  try {
    await BCUAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = window.prompt('请输入 BCU 便携目录完整路径（含 BCUninstaller.exe）\n将整套迁入：exe + 运行时 + BCUninstaller.settings 卸载历史均保留')
  if (!path) return
  try {
    busy.value = true
    const info = await BCUAPI.ImportLocal(path.trim())
    showToast(`已导入 BCU ${info.version}（设置与数据一并迁移）`)
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


// ---------- 联动开关 / 桌面快捷方式 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [fo, u] = await Promise.all([BCUAPI.GetFollowOnExit(), BCUAPI.RepositoryURL()])
    followOnExit.value = fo
    repoUrl.value = u
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  try {
    await BCUAPI.SetFollowOnExit(!followOnExit.value)
    showToast(followOnExit.value ? '已开启：HubKit 退出时一并关闭该工具' : '已关闭：HubKit 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    showToast('设置失败: ' + getErrorMessage(e))
    followOnExit.value = !followOnExit.value // 失败回滚
  }
}

async function createShortcut() {
  try {
    await BCUAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
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
    await BCUAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 生命周期 ----------
onMounted(async () => {
  unlistenDownload = Events.On('bcu:version-download', (event: any) => {
    const t = event.data as DownloadProgress
    if (!t || !t.version) return
    const key = `${t.version}|${t.variant || 'portable'}`
    downloading.value = { ...downloading.value, [key]: t }
    if (t.stage === 'done') {
      setTimeout(() => {
        const next = { ...downloading.value }
        delete next[key]
        downloading.value = next
      }, 800)
      loadVersions()
    }
  })

  unlistenState = Events.On('bcu:instance-state', (event: any) => {
    const s = event.data as Snapshot
    if (!s) return
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
  })

  await Promise.all([refreshStatus(), loadVersions(), loadDotnetEnv(), loadExtras()])
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
  <section class="page bcu-view">
    <div class="header-row">
      <div>
        <h1>BC 卸载工具</h1>
        <p class="subtitle">托管 Bulk Crap Uninstaller：版本管理、启停与窗口唤起，批量卸载干净又彻底。</p>
      </div>
      <div class="main-tab-nav">
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'console' }"
          @click="activeMainTab = 'console'"
        >
          🧹 控制台
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
    <!-- 顶部整合条：状态 + 启停按钮，一行内解决问题 -->
    <div class="control-bar">
      <div class="control-top">
        <div class="control-status">
          <span class="bcu-status-light" :class="state"></span>
          <span class="status-word">{{ stateText }}</span>
          <template v-if="isRunningOrStarting && runningVersion">
            <span class="ver-pill">{{ runningVersion }}</span>
            <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
          </template>
          <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
        </div>
        <div class="control-btns">
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || state === 'starting'"
            :title="state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 BCU 并打开主窗口'"
            @click="openWindow"
          >🗔 打开窗口</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 BCU 窗口内关闭' : '关闭窗口消息（挂起时强杀兜底）'"
            @click="quitBCU"
          >⏻ 退出</button>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <div v-if="banner" class="hint-banner slim" :class="banner.cls">{{ banner.text }}</div>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未运行：点击「打开窗口」启动 BCU，批量卸载在其窗口内完成。便携包自含 .NET 运行时（约 76MB），无需系统预装。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 BCU（自包含 .NET 首次启动约 3~10 秒）…</div>

    <!-- 说明卡（可折叠） -->
    <details class="info-details">
      <summary class="info-summary">什么是 BCU</summary>
      <div class="info-body">
        <p>开源批量卸载工具（<a class="inline-link" href="https://github.com/BCUninstaller/Bulk-Crap-Uninstaller" target="_blank" rel="noopener">BCUninstaller/Bulk-Crap-Uninstaller</a>，Apache-2.0）：静默卸载、残留清理、孤儿检测、开机项管理一站式完成。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
        <p class="hint-dim">版本目录携带各自独立的 BCUninstaller.settings，「导入本地」整套搬入；「设为使用」在多版本间切换。</p>
      </div>
    </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 HubKit 一起关闭 <span class="hint-dim">（关闭后 HubKit 退出完全不影响该工具）</span></span>
        </label>
        <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
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
        <span class="hint-dim">两种形态：自包含便携版（内嵌 .NET 运行时，76MB）+ 精简版（框架依赖，12MB，需本机 .NET 8 桌面运行时）——均经官方 digest 四层校验</span>
        <span class="hint-dim">2024 年末前的旧版本无官方哈希不入列表（完整性第一优先）；6.1 起资产版本号与 tag 对齐校验防串版</span>
        <!-- .NET 环境徽标：决定推荐变体 -->
        <span v-if="envLoading" class="hint-dim">正在检测本机 .NET 桌面运行时…</span>
        <span v-else-if="dotnetEnv" class="dotnet-banner" :class="dotnetEnv.hasNet8 ? 'ok' : 'warn'">
          <template v-if="dotnetEnv.hasNet8">
            本机已装 .NET 8 桌面运行时（{{ dotnetEnv.desktopVersions?.join(' / ') }}）→ 推荐下载<b>精简版</b>（12MB，省约 60MB）
          </template>
          <template v-else>
            未检测到 .NET 8 桌面运行时 → 推荐下载<b>自包含便携版</b>（76MB，免依赖）。精简版安装后会无法启动
          </template>
        </span>
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
      <p>尚未安装 BCU —— 下载官方便携版，或「导入本地安装」把现有 BCU 收纳进来</p>
      <button
        v-if="releases.length && recommendedVariant"
        class="btn btn-primary"
        @click="download(releases[0], recommendedVariant)"
      >
        下载最新版 {{ releases[0].version }}（{{ variantLabel(recommendedVariant) }}，推荐）
      </button>
      <button v-else-if="releases.length" class="btn btn-primary" @click="download(releases[0], 'portable')">
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
            :title="state === 'running' && runningVersion === v.version ? '请先退出 BCU' : ''"
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
              <!-- 类名刻意用 bcu-status（含 bcu- 前缀）——App.vue 全局样式有 .status-dot（7px），防碰撞压扁徽标 -->
              <span v-if="statusOverall(rel) === 'installed'" class="bcu-ver-status installed">已安装</span>
              <span v-else-if="statusOverall(rel) === 'downloading'" class="bcu-ver-status downloading">下载中</span>
              <span v-else-if="statusOverall(rel) === 'error'" class="bcu-ver-status error">失败</span>
              <span v-else class="bcu-ver-status idle">可安装</span>
            </td>
            <td>{{ fmtSize(rel.size) }}</td>
            <td>{{ fmtDate(rel.published) }}</td>
            <td>
              <template v-if="statusOf(rel, 'portable') === 'installed'">
                <span class="btn btn-ghost btn-small">已安装</span>
              </template>
              <template v-else>
                <!-- 双变体下载：推荐项主按钮高亮 -->
                <div class="variant-btns">
                  <button
                    :class="['btn btn-small', isRecommended(rel, 'portable') || recommendedVariant === null ? 'btn-primary' : 'btn-secondary']"
                    :disabled="statusOf(rel, 'portable') === 'downloading' || (!rel.fddName && statusOf(rel, 'fdd') === 'downloading')"
                    @click="download(rel, 'portable')"
                  >便携版 {{ fmtSize(rel.size) }}</button>
                  <button
                    v-if="rel.fddName"
                    :class="['btn btn-small', isRecommended(rel, 'fdd') ? 'btn-primary' : 'btn-secondary']"
                    :disabled="statusOf(rel, 'fdd') === 'downloading' || statusOf(rel, 'portable') === 'downloading'"
                    @click="download(rel, 'fdd')"
                  >精简版 {{ rel.fddSize ? fmtSize(rel.fddSize) : '' }}</button>
                </div>
                <div v-if="statusOf(rel, 'portable') === 'downloading' || statusOf(rel, 'fdd') === 'downloading'" class="variant-progress">
                  <template v-for="v in VARIANTS" :key="v">
                    <div v-if="statusOf(rel, v) === 'downloading'" class="dl-meta-text">
                      <span v-if="['verify', 'extract'].includes(downloading[`${rel.version}|${v}`]!.stage)">
                        {{ variantLabel(v) }}校验解压安装…
                      </span>
                      <span v-else-if="downloading[`${rel.version}|${v}`]!.stage === 'downloading'" class="download-cell">
                        <div class="dl-bar-wrap">
                          <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[`${rel.version}|${v}`]!)}%` }"></div>
                        </div>
                        <span class="dl-percent">{{ stepOf(downloading[`${rel.version}|${v}`]!) }}%</span>
                      </span>
                      <span v-else class="dl-error" :title="downloading[`${rel.version}|${v}`]!.message">
                        {{ downloading[`${rel.version}|${v}`]!.message }}
                      </span>
                      <a class="retry-link" @click="download(rel, v)">重试</a>
                    </div>
                  </template>
                </div>
              </template>
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
.bcu-view { display: flex; flex-direction: column; gap: 10px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 顶层主选项卡（与各工具模块同款） */
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
/* 信号灯类名带 bcu- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.bcu-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--text-subtle); flex-shrink: 0; }
.bcu-status-light.running { background: var(--success); box-shadow: 0 0 0 3px rgba(26, 127, 55, 0.15); }
.bcu-status-light.starting { background: var(--accent); animation: pulse 1s infinite; }
.bcu-status-light.external { background: #9a6700; box-shadow: 0 0 0 3px rgba(154, 103, 0, 0.15); }
.bcu-status-light.failed { background: var(--danger); box-shadow: 0 0 0 3px rgba(207, 34, 46, 0.15); }
.status-word { font-size: 15px; font-weight: 700; color: var(--text-main); }
.ver-pill { font-family: Consolas, monospace; font-size: 12px; background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 4px; padding: 1px 8px; color: var(--text-main); }
.pid-tag { font-size: 11px; color: var(--text-subtle); }
.uptime-tag { font-size: 11px; color: var(--text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示条与说明卡 ---------- */
.hint-banner { padding: 10px 14px; border-radius: 6px; font-size: 13px; border: 1px solid transparent; }
.hint-banner.slim { padding: 8px 12px; font-size: 12px; }
.banner-warn { background: #fff8c5; border-color: rgba(191, 135, 0, 0.3); color: #9a6700; }
.banner-error { background: #ffebe9; border-color: rgba(207, 34, 46, 0.25); color: var(--danger); }
.banner-ok { background: #dafbe1; border-color: rgba(26, 127, 55, 0.2); color: #1a7f37; }
.hint-line { font-size: 12px; color: var(--text-subtle); padding-left: 2px; }
/* ---------- 折叠说明 ---------- */
.info-details { border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-sidebar); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; gap: 4px; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-details[open] .info-summary { border-bottom: 1px solid var(--border-color); }
.info-summary::after { content: '▸'; font-size: 10px; transition: transform 0.15s; margin-left: auto; }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--accent); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-main); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
.link-btn { background: transparent; border: none; color: var(--accent); font-size: 12px; cursor: pointer; padding: 0 2px; }
.link-btn:hover { text-decoration: underline; }

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

.bcu-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.bcu-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.bcu-ver-status.installed::before { background: #2da44e; }
.bcu-ver-status.downloading::before { background: #0969da; animation: pulse 1s infinite; }
.bcu-ver-status.error::before { background: #cf222e; }
.bcu-ver-status.idle::before { background: #8c959f; }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: #e1e4e8; border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--accent); }
.dl-error { color: var(--danger); font-size: 11px; }
.retry-link { color: var(--accent); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 双变体下载与 .NET 环境徽标 ---------- */
.variant-btns { display: flex; gap: 6px; align-items: center; }
.variant-progress { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.variant-progress .dl-meta-text { font-size: 11px; }
.dotnet-banner { font-size: 12px; padding: 4px 10px; border-radius: 6px; border: 1px solid transparent; max-width: 680px; }
.dotnet-banner.ok { background: #dafbe1; color: #1a7f37; border-color: rgba(26, 127, 55, 0.2); }
.dotnet-banner.warn { background: #fff8c5; color: #9a6700; border-color: rgba(191, 135, 0, 0.3); }
.dotnet-banner b { font-weight: 700; }

@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>
