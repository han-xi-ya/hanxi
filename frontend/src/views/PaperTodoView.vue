<script setup lang="ts">
// PaperTodo 便签托管工作台：控制台（唤窗/收拢/退出）+ 版本管理（双变体下载、导入本地、卸载保留数据）
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated } from 'vue'
import { Events } from '@wailsio/runtime'
import * as PaperAPI from '../../bindings/hanxi/internal/modules/papertodo/papertodoservice'
import type { PaperRelease, PaperVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/papertodo/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/papertodo/instance/models'
import type { ControlOutcome, QuitOutcome, RuntimeStatus } from '../../bindings/hanxi/internal/modules/papertodo/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

type Variant = 'self-contained' | 'no-runtime'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<PaperRelease[]>([])
const installed = ref<PaperVersionInfo | null>(null)
const variant = ref<Variant>('self-contained')
const runtime = ref<RuntimeStatus | null>(null)
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map（按版本索引；重试沿用最近一次的变体）
const downloading = ref<Record<string, DownloadProgress>>({})
const lastVariant = ref<Record<string, Variant>>({})

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/markeron 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

let unlistenDownload: (() => void) | null = null
let unlistenState: (() => void) | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')
const runningVersion = computed(() => snap.value?.version ?? '')

const stateText = computed(() => {
  switch (state.value) {
    case 'running': return '运行中'
    case 'starting': return '启动中…'
    case 'failed': return '异常退出'
    case 'external': return '外部运行'
    default: return '未运行'
  }
})

// 条件提示条（三个变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      cls: 'banner-warn',
      text: '检测到外部 PaperTodo 实例（非 Hanxi 托管）。可代为唤回/收拢纸片；退出请在其纸片顶栏或托盘菜单操作。',
    }
  }
  if (state.value === 'failed') {
    return { cls: 'banner-error', text: snap.value?.error || 'PaperTodo 异常退出' }
  }
  if (state.value === 'running') {
    return {
      cls: 'banner-ok',
      text: 'PaperTodo 正在运行：便签在它自己的桌面纸片上编辑，内容自动保存。便签数据存于托管目录（data.json），升级不丢、卸载保留。',
    }
  }
  return null
})

// no-runtime 变体的运行时可用性提示（磁盘直读 Program Files\dotnet，不依赖 CLI）
const variantNote = computed(() => {
  if (variant.value !== 'no-runtime') return ''
  if (!runtime.value) return '桌面运行时探测中…'
  if (runtime.value.hasDesktop10) {
    const hit = (runtime.value.desktopRuntimes ?? []).filter(v => v.startsWith('10.'))
    return `已检测到 .NET 10 桌面运行时${hit.length ? `（${hit.join(' / ')}）` : ''}，精简版可直接运行`
  }
  return '未检测到 .NET 10 桌面运行时：精简版将启动失败，建议改用完整版，或先在「开发环境检测」页了解 .NET 运行时'
})

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remote, local, pref, rt] = await Promise.all([
      PaperAPI.ListReleases(),
      PaperAPI.GetInstalledVersion(),
      PaperAPI.GetVariant(),
      PaperAPI.GetRuntimeStatus(),
    ])
    releases.value = remote ?? []
    installed.value = local && local.version ? local : null
    variant.value = (pref === 'no-runtime' ? 'no-runtime' : 'self-contained') as Variant
    runtime.value = rt ?? null
  } catch (e) {
    listError.value = `获取版本列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function refreshStatus() {
  try {
    snap.value = await PaperAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('papertodo GetStatus failed:', getErrorMessage(e))
  }
}

// ---------- 格式化 ----------
function fmtSize(bytes?: number): string {
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

function statusOf(rel: PaperRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  if (installed.value?.version === rel.version) return 'installed'
  return 'idle'
}

// 单变体操作语义：已装同变体=不可点；已装另一变体=换装；未装=安装
function cellAction(rel: PaperRelease, v: Variant): 'installed' | 'switch' | 'download' {
  if (installed.value?.version === rel.version && installed.value?.variant === v) return 'installed'
  if (installed.value?.version === rel.version) return 'switch'
  return 'download'
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await PaperAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function hidePapers() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await PaperAPI.HidePapers()
    showToast(out.message)
  } catch (e) {
    showToast(getErrorMessage(e))
  } finally {
    busy.value = false
  }
}

async function quitPaperTodo() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await PaperAPI.Quit()
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
async function download(rel: PaperRelease, v: Variant) {
  lastVariant.value = { ...lastVariant.value, [rel.version]: v }
  try {
    const res = await PaperAPI.DownloadVersion(rel.version, v)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version}（${v === 'no-runtime' ? '精简版' : '完整版'}）已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

function retryDownload(rel: PaperRelease) {
  download(rel, lastVariant.value[rel.version] ?? variant.value)
}

async function removeInstalled() {
  const hadData = installed.value?.hasData
  if (!window.confirm(`确定卸载 PaperTodo？\n只删除程序本体与托管元信息——${hadData ? '你的便签数据（data.json、图片库、plugins）将原地保留' : '当前没有便签数据'}。\n下次安装或导入本地副本时数据自动接上。`)) return
  try {
    await PaperAPI.RemoveInstalled()
    showToast('已卸载 PaperTodo（便签数据已保留）')
    await loadVersions()
    await refreshStatus()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = window.prompt('请输入本机已有 PaperTodo 的目录完整路径（含 PaperTodo.exe）\n提示：便签数据（data.json 等）与 exe 同目录，导入时会一起收编进托管目录')
  if (!path) return
  try {
    busy.value = true
    const info = await PaperAPI.ImportLocal(path.trim())
    showToast(`已导入 PaperTodo ${info.version}${info.hasData ? '（便签数据随行）' : ''}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function openDir(path?: string) {
  if (!path) return
  try {
    await PaperAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

// ---------- 变体与联动偏好 ----------
async function chooseVariant(v: Variant) {
  if (v === variant.value) return
  const prev = variant.value
  variant.value = v
  try {
    await PaperAPI.SetVariant(v)
    showToast(v === 'no-runtime' ? '下载变体已切换为精简版（下次下载生效，不追溯已装版本）' : '下载变体已切换为完整版（下次下载生效）')
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
    variant.value = prev // 失败回滚
  }
}

const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([PaperAPI.GetFollowOnExit(), PaperAPI.RepositoryURL()])
    followOnExit.value = f
    repoUrl.value = u
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  try {
    await PaperAPI.SetFollowOnExit(!followOnExit.value)
    showToast(followOnExit.value ? '已开启：Hanxi 退出时一并关闭便签' : '已关闭：Hanxi 退出不影响便签，纸片继续常驻桌面（下次启动生效）')
  } catch (e) {
    showToast('设置失败: ' + getErrorMessage(e))
    followOnExit.value = !followOnExit.value // 失败回滚
  }
}

async function createShortcut() {
  try {
    await PaperAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向托管安装）')
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
    await PaperAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

async function openReleases() {
  try {
    await PaperAPI.OpenReleasesPage()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
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
  unlistenDownload = Events.On('papertodo:version-download', (event: any) => {
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

  unlistenState = Events.On('papertodo:instance-state', (event: any) => {
    const s = event.data as Snapshot
    if (!s) return
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
  })

  await Promise.all([refreshStatus(), loadVersions(), loadExtras()])
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
  <section class="page papertodo-view">
    <div class="header-row">
      <div>
        <h1>PaperTodo 便签</h1>
        <p class="subtitle">托管极简桌面便签 PaperTodo：官方绿色单文件双变体下载、单目录覆盖升级（便签数据永不迁移）、JobObject 启停；唤窗/收拢/退出走上游官方命令通道。</p>
      </div>
      <div class="main-tab-nav">
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'console' }"
          @click="activeMainTab = 'console'"
        >
          📄 控制台
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
      <div class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <span class="pt-status-light" :class="state"></span>
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
              :title="state === 'running' ? '唤回全部纸片（show 命令）' : state === 'starting' ? '启动中…' : '启动 PaperTodo 并把纸片放上桌面'"
              @click="openWindow"
            >🗔 唤回纸片</button>
            <button
              class="btn btn-secondary btn-small"
              :disabled="busy || (state !== 'running' && !isExternal)"
              title="收拢全部纸片（hide 命令，托盘与双击召回不受影响）"
              @click="hidePapers"
            >🗜 收拢纸片</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部实例请在其托盘/顶栏退出' : '发送 exit 命令优雅退出（宽限后强杀兜底）'"
              @click="quitPaperTodo"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <div v-if="banner" class="hint-banner slim" :class="banner.cls">{{ banner.text }}</div>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「唤回纸片」启动，便签直接在桌面纸片上书写。数据存于托管目录，Hanxi 不读取便签内容。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 PaperTodo（约 1~3 秒）…</div>

      <details class="info-details">
        <summary class="info-summary">什么是 PaperTodo</summary>
        <div class="info-body">
          <p>极简 Windows 桌面便签（<a class="inline-link" href="https://github.com/snownico0722/PaperTodo" target="_blank" rel="noopener">snownico0722/PaperTodo</a>，PolyForm Noncommercial 个人可用）：待办纸 + 笔记纸，每张纸独立悬浮窗口，内容自动保存，边缘胶囊收纳。WPF/.NET 原生，无账号无联网。</p>
          <p class="hint-dim">许可证禁止组织统一分发——Hanxi 仅托管"你这台机器直接从官方 Releases 下载原版"的流程，不内嵌、不再分发任何二进制。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出不影响便签，纸片继续常驻桌面）</span></span>
        </label>
        <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
      </div>
      <div class="repo-row">
        <span class="k">GitHub 仓库</span>
        <code class="mono repo-addr">{{ repoUrl }}</code>
        <button class="link-btn" @click="copyRepo">复制</button>
        <button class="link-btn" @click="openRepo">浏览器打开</button>
        <button class="link-btn" @click="openReleases">Releases 页</button>
      </div>
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>{{ installed ? `当前托管 ${installed.version} · ${installed.variant === 'no-runtime' ? '精简版' : '完整版'}` : '尚未安装托管版本' }} · 远程版本 {{ releases.length }} 个</span>
          <span class="hint-dim">单目录覆盖升级：便签数据原地保留；「卸载」只删程序不删数据；回滚旧版 = 在下方重新安装该版本</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地副本</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 变体选择卡 -->
      <div class="variant-card">
        <span class="k">下载变体</span>
        <label class="variant-opt">
          <input type="radio" name="pt-variant" :checked="variant === 'self-contained'" @change="chooseVariant('self-contained')" />
          <span>完整版 self-contained <span class="hint-dim">（内嵌 .NET 10 运行时，约 71MB，零依赖）</span></span>
        </label>
        <label class="variant-opt">
          <input type="radio" name="pt-variant" :checked="variant === 'no-runtime'" @change="chooseVariant('no-runtime')" />
          <span>精简版 no-runtime <span class="hint-dim">（约 2.4MB，需系统 .NET 10 桌面运行时）</span></span>
        </label>
        <span v-if="variantNote" class="variant-note" :class="{ warn: variant === 'no-runtime' && runtime && !runtime.hasDesktop10 }">{{ variantNote }}</span>
      </div>

      <!-- 当前托管卡 -->
      <div v-if="installed" class="installed-card">
        <div class="inst-card-top">
          <span class="ver-tag">{{ installed.version }}</span>
          <div class="inst-badges">
            <span v-if="state === 'running' && runningVersion === installed.version" class="badge badge-running">运行中</span>
            <span v-else-if="state === 'external'" class="badge badge-external">外部运行</span>
            <span v-if="installed.variant === 'no-runtime'" class="badge badge-variant">精简版</span>
            <span v-else-if="installed.variant === 'self-contained'" class="badge badge-variant">完整版</span>
            <span v-if="installed.isImport" class="badge badge-import">本地导入</span>
            <span v-else class="badge badge-official">官方下载</span>
            <span v-if="installed.hasData" class="badge badge-data">便签数据在册</span>
          </div>
        </div>
        <div class="inst-meta">
          <div class="meta-line"><span class="k">路径</span><code class="mono">{{ installed.exePath }}</code></div>
          <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(installed.size) }} · 安装于 {{ installed.installedAt }}</span></div>
          <div v-if="installed.assetSha256" class="meta-line"><span class="k">指纹</span><code class="mono hint-dim" :title="'下载时计算的 sha256（上游未发布官方哈希，此为审计基线）'">{{ installed.assetSha256 }}</code></div>
          <div v-if="installed.isImport && installed.source" class="meta-line"><span class="k">来源</span><span class="hint-dim">{{ installed.source }}</span></div>
        </div>
        <div class="inst-actions">
          <button class="btn btn-secondary btn-small" @click="openDir(installed.dir)">📂 打开位置</button>
          <button class="btn btn-danger-outline btn-small" :disabled="isRunningOrStarting" :title="isRunningOrStarting ? '请先退出 PaperTodo' : '只删程序，便签数据原地保留'" @click="removeInstalled">卸载（保留数据）</button>
        </div>
      </div>
      <div v-else class="empty-state first-use">
        <p>尚未安装 PaperTodo —— 下载官方绿色版，或「导入本地副本」把你机器上已有的 PaperTodo 目录（连同便签数据）收编进来</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0], variant)">
          下载最新版 {{ releases[0].version }}（{{ variant === 'no-runtime' ? '精简版' : '完整版' }}）
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </div>

      <!-- 远程可用版本 -->
      <div class="section-title"><h3>远程可用版本</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 120px;">版本</th>
              <th style="width: 96px;">状态</th>
              <th style="width: 210px;">完整版（内嵌运行时）</th>
              <th style="width: 210px;">精简版（需 .NET 10）</th>
              <th style="width: 100px;">发布时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rel in releases" :key="rel.version">
              <td><strong class="ver-name">{{ rel.version }}</strong></td>
              <td>
                <!-- 类名刻意用 pt- 前缀——App.vue 全局样式有 .status-dot（7px），防碰撞压扁徽标 -->
                <span v-if="statusOf(rel) === 'installed'" class="pt-ver-status installed">托管中</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="pt-ver-status downloading">下载中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="pt-ver-status error">失败</span>
                <span v-else class="pt-ver-status idle">可安装</span>
              </td>
              <td class="asset-cell">
                <template v-if="statusOf(rel) === 'downloading' && downloading[rel.version]">
                  <div v-if="downloading[rel.version]!.stage === 'downloading'" class="download-cell">
                    <div class="dl-bar-wrap"><div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div></div>
                    <span class="dl-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
                  </div>
                  <div v-else-if="downloading[rel.version]!.stage === 'verify'" class="dl-meta-text">校验安装中…</div>
                  <div v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</div>
                  <a v-if="downloading[rel.version]!.stage === 'error'" class="retry-link" @click="retryDownload(rel)">重试</a>
                </template>
                <template v-else>
                  <span class="hint-dim size-hint">{{ fmtSize(rel.selfContained?.size) }}</span>
                  <button v-if="cellAction(rel, 'self-contained') === 'download'" class="btn btn-primary btn-small" @click="download(rel, 'self-contained')">安装</button>
                  <button v-else-if="cellAction(rel, 'self-contained') === 'switch'" class="btn btn-secondary btn-small" title="覆盖安装为完整版变体（便签数据不动）" @click="download(rel, 'self-contained')">换装</button>
                  <span v-else class="btn btn-ghost btn-small">已安装</span>
                  <a v-if="installed && installed.version !== rel.version" class="retry-link" :title="`覆盖当前托管的 ${installed.version}，便签数据保留`" @click="download(rel, 'self-contained')">装此版</a>
                </template>
              </td>
              <td class="asset-cell">
                <template v-if="!(statusOf(rel) === 'downloading' && downloading[rel.version])">
                  <span class="hint-dim size-hint">{{ fmtSize(rel.noRuntime?.size) }}</span>
                  <button v-if="cellAction(rel, 'no-runtime') === 'download'" class="btn btn-primary btn-small" :class="{ 'btn-dim': runtime && !runtime.hasDesktop10 }" :title="runtime && !runtime.hasDesktop10 ? '未检测到 .NET 10 桌面运行时，装好后可能无法启动' : ''" @click="download(rel, 'no-runtime')">安装</button>
                  <button v-else-if="cellAction(rel, 'no-runtime') === 'switch'" class="btn btn-secondary btn-small" title="覆盖安装为精简版变体（便签数据不动）" @click="download(rel, 'no-runtime')">换装</button>
                  <span v-else class="btn btn-ghost btn-small">已安装</span>
                  <a v-if="installed && installed.version !== rel.version" class="retry-link" :title="`覆盖当前托管的 ${installed.version}，便签数据保留`" @click="download(rel, 'no-runtime')">装此版</a>
                </template>
                <span v-else class="hint-dim">—</span>
              </td>
              <td>{{ fmtDate(rel.published) }}</td>
            </tr>
            <tr v-if="releases.length === 0 && !loading">
              <td colspan="4" class="empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.papertodo-view { display: flex; flex-direction: column; gap: 10px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 顶层主选项卡（与 CCSwitchView 同款） */
.main-tab-nav { display: flex; background: var(--bg-hover); padding: 3px; border-radius: 8px; gap: 2px; }
.main-tab-btn { background: transparent; border: none; padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; color: var(--text-muted); cursor: pointer; transition: all 0.15s ease; }
.main-tab-btn:hover { color: var(--text-main); }
.main-tab-btn.active { background: var(--bg-app); color: var(--accent); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08); }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 10px; }
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 pt- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.pt-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--text-subtle); flex-shrink: 0; }
.pt-status-light.running { background: var(--success); box-shadow: 0 0 0 3px rgba(26, 127, 55, 0.15); }
.pt-status-light.starting { background: var(--accent); animation: pulse 1s infinite; }
.pt-status-light.external { background: #9a6700; box-shadow: 0 0 0 3px rgba(154, 103, 0, 0.15); }
.pt-status-light.failed { background: var(--danger); box-shadow: 0 0 0 3px rgba(207, 34, 46, 0.15); }
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

/* ---------- 通用按钮 ---------- */
.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn-ghost { background: #f0f7ff; color: #0969da; border-color: #c8e1ff; cursor: default; }
.btn-danger-outline { background: #fff; border-color: #ff8170; color: var(--danger); }
.btn-danger-outline:hover:not(:disabled) { background: #ffebe9; }
.btn-dim { opacity: 0.75; }

.control-panel { display: flex; align-items: center; justify-content: space-between; background: var(--bg-sidebar); border: 1px solid var(--border-color); padding: 10px 14px; border-radius: 8px; gap: 10px; flex-wrap: wrap; }
.meta-info { font-size: 13px; color: var(--text-muted); display: flex; flex-direction: column; gap: 2px; }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--text-subtle); }

/* ---------- 变体选择卡 ---------- */
.variant-card { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; padding: 8px 14px; font-size: 13px; }
.variant-card .k { color: var(--text-subtle); flex-shrink: 0; }
.variant-opt { display: flex; align-items: center; gap: 6px; cursor: pointer; color: var(--text-main); font-size: 12px; }
.variant-opt input { width: 14px; height: 14px; cursor: pointer; }
.variant-note { font-size: 12px; color: #1a7f37; }
.variant-note.warn { color: #9a6700; }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--text-subtle); font-size: 13px; background: var(--bg-sidebar); border-radius: 6px; border: 1px dashed var(--border-color); }

.empty-state { text-align: center; padding: 24px; background: var(--bg-sidebar); border: 1px dashed var(--border-color); border-radius: 8px; display: flex; flex-direction: column; gap: 12px; align-items: center; }
.empty-state p { margin: 0; color: var(--text-muted); font-size: 13px; }

/* ---------- 当前托管卡 ---------- */
.installed-card { background: var(--bg-sidebar); border: 1px solid var(--accent); border-radius: 8px; padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 6px; }
.ver-tag { font-family: Consolas, monospace; font-size: 14px; font-weight: 700; color: var(--text-main); }
.inst-badges { display: flex; gap: 6px; flex-wrap: wrap; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
.badge-running { background: #ddf4ff; color: #0969da; }
.badge-external { background: #fff8c5; color: #9a6700; }
.badge-variant { background: #f0f7ff; color: #0969da; }
.badge-import { background: #ddf4ff; color: #0969da; }
.badge-official { background: #eaeef2; color: #656d76; }
.badge-data { background: #dafbe1; color: #1a7f37; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--text-muted); align-items: baseline; min-width: 0; }
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

.asset-cell { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.size-hint { font-size: 12px; min-width: 56px; }

.pt-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.pt-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.pt-ver-status.installed::before { background: #2da44e; }
.pt-ver-status.downloading::before { background: #0969da; animation: pulse 1s infinite; }
.pt-ver-status.error::before { background: #cf222e; }
.pt-ver-status.idle::before { background: #8c959f; }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: #e1e4e8; border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--accent); }
.dl-error { color: var(--danger); font-size: 11px; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.retry-link { color: var(--accent); font-size: 12px; cursor: pointer; margin-left: 4px; }
.retry-link:hover { text-decoration: underline; }

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

@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>
