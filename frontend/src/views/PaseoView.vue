<script setup lang="ts">
// 状态 / 版本管理 / 安装进度 / 时长 ticker / 生命周期
// 与 RecordlyView 的结构差异：多版本目录并存（官方 win zip 解压，非 NSIS 单目录）、
// "设为使用版本"选择、双数据目录入口（%APPDATA%\Paseo 与 ~/.paseo）、
// 共享数据模式文案（外部实例=用户自装实例，同数据同锁组）、自动更新平行副本预告。
import { ref, computed, watch, onMounted } from 'vue'
import * as PaseoAPI from '../../bindings/hanxi/internal/modules/paseo/paseoservice'
import type { PaseoRelease, PaseoVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/paseo/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/paseo/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/paseo/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<PaseoRelease[]>([])
const installed = ref<PaseoVersionInfo[]>([])
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)
const channel = ref<'stable' | 'beta'>('stable')
const activeVersion = ref('')

// 安装进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 recordly/vscode 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🐾 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 五态通用文案接 constants/status 单一来源（§9.5-5）；业务扩展话术视图自行覆写。
const stateText = computed(() => toolStateMeta(state.value).text)

const runningVersion = computed(() => snap.value?.version ?? '')

// 核心版本号（去预发布后缀）：升级判定用数值核心对比，不可解析退化字典序
function coreOf(v: string): string {
  return v.replace(/-.*$/, '')
}

function coreCompare(a: string, b: string): number {
  const pa = coreOf(a).split('.').map(Number)
  const pb = coreOf(b).split('.').map(Number)
  for (let i = 0; i < 3; i++) {
    const na = pa[i], nb = pb[i]
    if (Number.isNaN(na) || Number.isNaN(nb)) return coreOf(a) < coreOf(b) ? -1 : coreOf(a) > coreOf(b) ? 1 : 0
    if (na !== nb) return na > nb ? 1 : -1
  }
  return 0
}

// 最新已装规范版本（后端已新→旧排序，imported- 目录不参与升级判定）
const latestInstalled = computed(() =>
  installed.value.find(v => /^\d+\.\d+\.\d+/.test(v.version)) ?? null)

// 可升级：最新已装核心 < 当前通道最新核心
const upgradeAvailable = computed(() => {
  if (!latestInstalled.value || releases.value.length === 0) return false
  return coreCompare(latestInstalled.value.version, releases.value[0].version) < 0
})

// 冷启动将使用的版本（activeVersion 未设定时后端回退最新已装）
const launchVersion = computed(() => activeVersion.value || latestInstalled.value?.version || '')

// 条件提示条（变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn' as const,
      text: '检测到非 Hanxi 启动的 Paseo 实例（共享数据模式下同一实例锁组，全局仅一个桌面主实例）。可唤起其窗口；如需退出请在 Paseo 窗口内关闭。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error' as const, text: snap.value?.error || 'Paseo 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok' as const,
      text: 'Paseo 正在运行：agent 编排在其窗口内完成（数据在 %APPDATA%\\Paseo 与 ~/.paseo，与自装实例共享；无窗运行 ≠ 空闲，不设自动退出）。',
    }
  }
  return null
})

// 常驻风险提示：应用内"安装更新"会装出托管外的平行副本（上游无禁用开关）
const updaterNote = '上游无自动更新禁用开关：在 Paseo 界面内点「安装更新」会把新版装进 %LOCALAPPDATA%（托管目录外的平行副本），版本升级请统一走这里。'

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  const localTask = Promise.all([PaseoAPI.ListInstalledVersions(), PaseoAPI.GetReleaseChannel(), PaseoAPI.GetActiveVersion()])
    .then(([local, ch, act]) => {
      installed.value = local ?? []
      channel.value = ch === 'beta' ? 'beta' : 'stable'
      activeVersion.value = act ?? ''
    })
    .catch((error: unknown) => { listError.value = `读取本地版本失败: ${getErrorMessage(error)}` })
  void PaseoAPI.ListReleases()
    .then(remote => { releases.value = remote ?? [] })
    .catch((error: unknown) => { listError.value = `获取远程版本列表失败: ${getErrorMessage(error)}` })
    .finally(() => { loading.value = false })
  await localTask
}

async function switchChannel(target: 'stable' | 'beta') {
  if (target === channel.value) return
  try {
    await PaseoAPI.SetReleaseChannel(target)
    channel.value = target
    await loadVersions()
  } catch (e) {
    showToast(`切换通道失败: ${getErrorMessage(e)}`)
  }
}

async function refreshStatus() {
  try {
    snap.value = await PaseoAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('paseo GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

// 安装状态判定：版本目录名与远程 tag（去 v）精确互等
function statusOf(rel: PaseoRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  return installed.value.some(v => v.version === rel.version) ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await PaseoAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitPaseo() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await PaseoAPI.Quit()
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
async function install(rel: PaseoRelease) {
  try {
    await PaseoAPI.DownloadVersion(rel.version)
  } catch (e) {
    showToast(`安装失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: PaseoVersionInfo) {
  try {
    activeVersion.value = await PaseoAPI.SetActiveVersion(v.version)
    showToast(`下次启动将使用 Paseo ${v.version}`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
    await loadVersions()
  }
}

async function openDir(path: string) {
  try {
    await PaseoAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function openElectronDataDir() {
  try {
    await PaseoAPI.OpenElectronDataDir()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function openDaemonHome() {
  try {
    await PaseoAPI.OpenDaemonHome()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: PaseoVersionInfo) {
  const ok = await confirm({
    title: `确定卸载 Paseo ${v.version}？`,
    description: '仅删除该托管版本目录。\n（%APPDATA%\\Paseo 与 ~/.paseo 中的设置、会话与手机配对数据为共享用户数据，不受影响）',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await PaseoAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地安装',
    label: '请输入 Paseo 程序目录完整路径（含 Paseo.exe 与 resources\\app.asar 的整套目录）',
    description: '常见位置：安装版 %LOCALAPPDATA%\\Programs\\Paseo 或自定义目录\n提示：数据恒在 %APPDATA%\\Paseo 与 ~/.paseo，与程序目录无关，导入不搬数据',
    placeholder: '%LOCALAPPDATA%\\Programs\\Paseo',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await PaseoAPI.ImportLocal(path.trim())
    showToast(`已导入 Paseo ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// ---------- 时长 ticker 与状态轮询（usePolling 内置 KeepAlive 生命周期契约） ----------
const statusPolling = usePolling(refreshStatus, 2500) // 状态兜底轮询（事件推送之外）
usePolling(() => {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}, 1000, { immediateFirstRun: false })

// 停止轮询（切后台/卸载）时运行时长归零——对齐 recordly 语义
watch(statusPolling.isPolling, (running) => {
  if (!running) uptimeSec.value = 0
})

// ---------- 联动开关 / 桌面快捷方式 / GitHub 仓库 ----------
const followOnExit = ref(false)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([PaseoAPI.GetFollowOnExit(), PaseoAPI.RepositoryURL()])
    followOnExit.value = f
    repoUrl.value = u
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  const next = !followOnExit.value
  followOnExit.value = next // 用户点击已将勾选框翻转，ref 同步跟进，保持绑定状态一致
  try {
    await PaseoAPI.SetFollowOnExit(next)
    showToast(next
      ? '已开启：Hanxi 退出将连带终止 Paseo 及其上正在运行的 agent 会话（下次启动生效）'
      : '已关闭：Hanxi 退出不影响 Paseo，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await PaseoAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  if (!(await copy(repoUrl.value))) {
    showToast('复制失败: 剪贴板不可用')
    return
  }
  showToast('仓库地址已复制')
}

async function openRepo() {
  try {
    await PaseoAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件订阅（useWailsEvent 自动注销）与初始装载 ----------
useWailsEvent<DownloadProgress>('paseo:version-download', (t) => {
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

useWailsEvent<Snapshot>('paseo:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

onMounted(() => {
  // 状态首帧由 usePolling 的 mounted 即触发（immediateFirstRun）
  void Promise.all([loadVersions(), loadExtras()])
})
</script>

<template>
  <section class="page paseo-view">
    <PageHeader title="Paseo" subtitle="托管开源 coding agent 编排器 Paseo：版本管理、启停与窗口唤起，agent 会话在其窗口与手机端继续运行。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
    <!-- 顶部整合条：状态 + 启停按钮，一行内解决问题 -->
    <div class="control-bar">
      <div class="control-top">
        <div class="control-status">
          <span class="ps-status-light" :class="state"></span>
          <span class="status-word">{{ stateText }}</span>
          <template v-if="isRunningOrStarting && runningVersion">
            <span class="ver-pill">{{ runningVersion }}</span>
            <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
          </template>
          <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
        </div>
        <div class="control-btns">
          <UiButton
            variant="secondary"
            small
            :disabled="busy || state === 'starting'"
            :title="state === 'running' ? '唤起已运行窗口' : state === 'starting' ? '启动中…' : '启动 Paseo 并打开窗口'"
            @click="openWindow"
          >🗔 打开窗口</UiButton>
          <UiButton
            variant="danger"
            small
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 Paseo 窗口内退出' : '关闭窗口优雅退出（daemon 清理不及会兜底强杀）'"
            @click="quitPaseo"
          >⏻ 退出</UiButton>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
    <div v-else-if="state === 'stopped' && launchVersion" class="hint-line">
      尚未运行：点击「打开窗口」启动 Paseo {{ launchVersion }}。agent 编排与手机配对在其窗口内操作；数据在 %APPDATA%\Paseo 与 ~/.paseo，与托管版本目录无关。
    </div>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未安装：请到「版本管理」在线安装或导入本地副本。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Paseo（Electron 冷启动 + 内置 daemon 拉起，约 2~15 秒）…</div>

    <UiBanner v-if="upgradeAvailable && latestInstalled" tone="warn">
      发现可升级版本 {{ releases[0].version }}（当前最新已装 {{ latestInstalled.version }}）——到「版本管理」一键安装。
    </UiBanner>

    <UiBanner tone="info">{{ updaterNote }}</UiBanner>

    <!-- 说明卡（可折叠） -->
    <details class="info-details">
      <summary class="info-summary">什么是 Paseo</summary>
      <div class="info-body">
        <p>开源 coding agent 编排器（<a class="inline-link" href="https://github.com/getpaseo/paseo" target="_blank" rel="noopener">getpaseo/paseo</a>，Apache-2.0）：本机跑 daemon，桌面/手机/Web 统一调度 Claude Code、Codex 等 agent CLI。Hanxi 仅做官方便携 zip 的下载托管与启停管理，不内嵌不打包其代码。</p>
        <p class="hint-dim">共享数据模式（与 cc-switch 同构）：托管实例与自装实例同数据同锁组，全局至多一个桌面主实例；「外部」状态即你的自装实例在场。唤窗优先直接唤起已有窗口（上游二次拉起语义是"再开新窗"而非聚焦）。无窗运行 ≠ 空闲——其 agent 会话可能正在进行，本模块不设空闲自动退出。</p>
        <p class="hint-dim">开启「随 Hanxi 一起关闭」后，Hanxi 退出会连带终止 Paseo 及其 daemon 上正在运行的全部 agent 会话，请谨慎。</p>
      </div>
    </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（默认关闭：Hanxi 退出不影响 Paseo 及其 agent 会话）</span></span>
        </label>
        <UiButton variant="secondary" small @click="createShortcut">🖥 创建桌面快捷方式</UiButton>
        <button class="btn btn-secondary btn-small" title="打开 Electron 数据目录（%APPDATA%\Paseo：窗口状态与桌面设置）" @click="openElectronDataDir">🗂 Electron 数据</button>
        <button class="btn btn-secondary btn-small" title="打开 daemon 数据主目录（~/.paseo：持久配置、会话与手机配对）" @click="openDaemonHome">🐾 daemon 数据</button>
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
        <span>
          使用版本 <strong>{{ activeVersion || (latestInstalled ? `自动最新（${latestInstalled.version}）` : '未安装') }}</strong> · {{ channel === 'beta' ? 'beta 通道（含预发布）' : 'stable 通道' }} · 已装 {{ installed.length }} 版 · 远程版本 {{ releases.length }} 个
        </span>
        <span class="hint-dim">官方 Windows 便携 zip 解压进独立版本目录（GitHub digest sha256 + 字节数 + zip CRC + 布局四层校验），多版本共存，数据共享不受版本增删影响</span>
      </div>
      <div class="btn-group">
        <UiButton variant="secondary" small :disabled="busy" @click="importLocal">⇥ 导入本地安装</UiButton>
        <UiButton variant="secondary" small :disabled="loading" @click="loadVersions">
          {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
        </UiButton>
      </div>
    </div>

    <!-- 更新通道切换 -->
    <div class="channel-row">
      <span class="k">更新通道</span>
      <div class="channel-seg">
        <button :class="{ active: channel === 'stable' }" @click="switchChannel('stable')">Stable 稳定</button>
        <button :class="{ active: channel === 'beta' }" @click="switchChannel('beta')">Beta 预发布</button>
      </div>
      <span v-if="channel === 'beta'" class="beta-warn">beta 为上游预发布版，仅供尝鲜</span>
    </div>

    <!-- 已安装 -->
    <div class="section-title"><h3>托管版本</h3></div>

    <UiEmptyState v-if="installed.length === 0" class="first-use">
      <p>尚未安装 Paseo —— 在线安装官方便携 zip 解压版，或「导入本地安装」把机器上已有的程序目录收编进来</p>
      <UiButton v-if="releases.length" variant="primary" @click="install(releases[0])">
        安装最新版 {{ releases[0].version }}（约 {{ fmtSize(releases[0].size) }}）
      </UiButton>
      <UiButton v-else-if="!loading" variant="secondary" @click="loadVersions">↻ 刷新远程列表</UiButton>
    </UiEmptyState>

    <div v-else class="installed-grid">
      <div
        v-for="v in installed" :key="v.version"
        class="installed-card" :class="{ 'card-active': activeVersion === v.version || (!activeVersion && latestInstalled?.version === v.version) }"
      >
        <div class="inst-card-top">
          <span class="ver-tag">{{ v.version }}</span>
          <div class="inst-badges">
            <span v-if="state === 'running' && runningVersion === v.version" class="badge badge-running">运行中</span>
            <span v-else-if="(activeVersion || latestInstalled?.version) === v.version" class="badge badge-active">使用版本</span>
            <span v-if="!v.verifiedHash && !v.isImport" class="badge badge-import">未验证哈希</span>
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
          <UiButton
            v-if="(activeVersion || latestInstalled?.version) !== v.version"
            variant="primary" small
            :disabled="busy"
            title="下次启动使用该版本（运行中的实例不受影响）"
            @click="setActive(v)"
          >设为使用</UiButton>
          <UiButton variant="secondary" small @click="openDir(v.dir)">📂 打开位置</UiButton>
          <UiButton
            variant="danger"
            small
            :disabled="state === 'running' && runningVersion === v.version"
            :title="state === 'running' && runningVersion === v.version ? '请先退出该版本' : '仅删本版本目录，共享数据不受影响'"
            @click="removeVersion(v)"
          >卸载</UiButton>
        </div>
      </div>
    </div>

    <!-- 远程可用版本 -->
    <div class="section-title"><h3>远程可用版本</h3></div>
    <div class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 160px;">版本</th>
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
              <!-- 类名刻意用 ps- 前缀与全局原子族隔离（markeron 垂直字体事故教训） -->
              <span v-if="statusOf(rel) === 'installed'" class="ps-ver-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="ps-ver-status downloading">安装中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="ps-ver-status error">失败</span>
              <span v-else class="ps-ver-status idle">可安装</span>
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
                <span v-if="['verify', 'extract'].includes(downloading[rel.version]!.stage)">校验并解压…</span>
                <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <UiButton
                v-if="statusOf(rel) === 'idle'"
                variant="primary"
                small
                @click="install(rel)"
              >安装</UiButton>
              <span v-if="statusOf(rel) === 'installed'" class="chip chip-positive">已安装</span>
              <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="install(rel)">重试</a>
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
/* 仅保留本视图独有的业务样式；.btn 家族、.tbl、.header-row、.subtitle、.error-box、
   .empty-state、.mono、.chip、.banner、.link-button、main-tab 族、keyframes pulse 已上收。 */
.paseo-view { display: flex; flex-direction: column; gap: 10px; }

/* UiBanner 沿用迁移前 slim 密度 */
.tab-body .banner { padding: 8px 12px; font-size: 12px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 ps- 前缀，与远程表格徽标/全局样式隔离 */
.ps-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.ps-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.ps-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.ps-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.ps-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }
.info-details { border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }

/* ---------- 更新通道切换 ---------- */
.channel-row { display: flex; align-items: center; gap: 10px; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 8px 14px; font-size: 13px; flex-wrap: wrap; }
.channel-row .k { color: var(--color-text-subtle); }
.channel-seg { display: flex; background: var(--surface-hover); border-radius: 6px; padding: 2px; gap: 2px; }
.channel-seg button { border: none; background: transparent; padding: 4px 12px; font-size: 12px; border-radius: 5px; color: var(--color-text-muted); cursor: pointer; transition: color var(--motion-base) ease, background var(--motion-base) ease; }
.channel-seg button.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
.beta-warn { font-size: 12px; color: var(--state-warning); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: 6px; border: 1px dashed var(--color-border); }

/* ---------- 托管版本卡片（多版本并存） ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color var(--motion-base) ease;
}
.installed-card.card-active { border-color: var(--color-primary); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.ps-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.ps-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ps-ver-status.installed::before { background: var(--state-positive); }
.ps-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ps-ver-status.error::before { background: var(--state-danger); }
.ps-ver-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.retry-link { color: var(--color-primary); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
</style>
