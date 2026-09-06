<script setup lang="ts">
// 双形态控制台（便携版/安装版独立引擎快照）/ 版本管理 / 下载进度 / 时长 ticker
import { ref, computed, onMounted } from 'vue'
import * as VSCodeAPI from '../../bindings/hanxi/internal/modules/vscode/vscodeservice'
import type { Status, ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/vscode/models'
import type { Release, VersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/vscode/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/vscode/instance/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'

type Form = 'portable' | 'installer'

// ---------- 状态 ----------
const status = ref<Status | null>(null)
const releasesP = ref<Release[]>([])
const releasesI = ref<Release[]>([])
const installed = ref<VersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const remoteForm = ref<Form>('portable')

// 下载/安装进度 map（按 形态:版本 索引，两形态同名版本不串台）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

const activeMainTab = ref<'console' | 'versions'>('console')
const mainTabs = [
  { key: 'console', label: '💻 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const portSnap = computed<Snapshot | null>(() => status.value?.portable ?? null)
const instSnap = computed<Snapshot | null>(() => status.value?.installer ?? null)
const installedInfo = computed(() => status.value?.installed ?? null)

const uptimeP = ref(0)
const uptimeI = ref(0)

// 便携版"打开位置"目标：优先运行版本，其次 active，最后任一已装
const openDirVersion = computed(() => {
  const s = portSnap.value
  const prefer = s?.state === 'running' && s.version ? s.version : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

function bannerFor(snap: Snapshot | null, form: Form): { tone: 'warn' | 'error' | 'ok'; text: string } | null {
  if (!snap) return null
  if (snap.state === 'external') {
    return form === 'installer'
      ? { tone: 'warn', text: '检测到运行中的安装版 VS Code（与您日常使用同实例组，互斥体探测）。可唤起窗口；如需退出请在 VS Code 窗口内操作。' }
      : { tone: 'warn', text: '检测到外部启动的便携版实例（托管目录内、非 Hanxi 拉起）。可唤起其窗口；如需退出请在该实例内操作。' }
  }
  if (snap.state === 'failed') {
    return { tone: 'error', text: snap.error || 'VS Code 异常退出' } as const
  }
  if (snap.state === 'running' && form === 'installer') {
    return { tone: 'ok', text: '安装版正在运行：与您日常使用的 VS Code 共用配置（%APPDATA%\\Code）与实例组；其应用内自动更新可能领先托管记录，属已知行为。' }
  }
  if (snap.state === 'running' && form === 'portable') {
    return { tone: 'ok', text: '便携版正在运行：配置与扩展全部自包含于版本目录 data\\ 内，与日常 VS Code 完全隔离。' }
  }
  return null
}

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remoteP, remoteI, local, active] = await Promise.all([
      VSCodeAPI.ListRemoteVersions('portable'),
      VSCodeAPI.ListRemoteVersions('installer'),
      VSCodeAPI.ListInstalledVersions(),
      VSCodeAPI.GetActiveVersion(),
    ])
    releasesP.value = remoteP ?? []
    releasesI.value = remoteI ?? []
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
    status.value = await VSCodeAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('vscode GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function dlKey(form: Form, version: string) { return `${form}:${version}` }

function statusOf(form: Form, rel: Release): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[dlKey(form, rel.version)]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  if (form === 'portable' && installed.value.some(v => v.version === rel.version)) return 'installed'
  if (form === 'installer' && installedInfo.value?.version === rel.version) return 'installed'
  return 'idle'
}

// ---------- 控制操作 ----------
async function openWindow(form: Form) {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await VSCodeAPI.OpenWindow(form)
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitForm(form: Form) {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await VSCodeAPI.Quit(form)
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
async function download(form: Form, rel: Release) {
  try {
    let res = await VSCodeAPI.DownloadVersion(rel.version, form, false)
    // 安装版确认闸：运行中的实例（含用户日常窗口）会被 Inno 强关，须用户显式放行
    if (res.action === 'confirm-required') {
      const accepted = await confirm({
        title: `确认升级安装 VS Code ${rel.version}？`,
        description: res.message,
        tone: 'danger',
      })
      if (!accepted) return
      res = await VSCodeAPI.DownloadVersion(rel.version, form, true)
    }
    if (res.action === 'already-installed') {
      showToast(res.message)
      await loadVersions()
    } else if (res.message) {
      showToast(res.message)
    }
  } catch (e) {
    showToast(`操作失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: VersionInfo) {
  try {
    const ver = await VSCodeAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将便携版 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await VSCodeAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: VersionInfo) {
  const accepted = await confirm({
    title: `确定卸载便携版 VS Code ${v.version}？`,
    description: '该版本隔离目录（含 data\\ 内的配置与已装扩展）将被整体删除，不可恢复。\n（安装版与 %APPDATA%\\Code 的日常数据不受影响）',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await VSCodeAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地便携版 VS Code',
    description: '提示：整套迁移（data\\ 除外，导入即全新自包含环境）；安装版目录无需导入，会被自动感知',
    label: '便携版目录完整路径（含 Code.exe 与 bin\\code.cmd）',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await VSCodeAPI.ImportLocal(path.trim())
    showToast(`已导入便携版 ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// 运行时长秒表（两引擎各自从快照 startedAt 重算；KeepAlive 停用期间暂停）
function uptimeTick() {
  const calc = (s: Snapshot | null): number => {
    if (s?.state === 'running' && s.startedAt) {
      const started = new Date(s.startedAt).getTime()
      if (!Number.isNaN(started)) return Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
    return 0
  }
  uptimeP.value = calc(portSnap.value)
  uptimeI.value = calc(instSnap.value)
}

// ---------- 联动开关 / 桌面快捷方式 / 官网 ----------
const followOnExit = ref(false)
const siteUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([VSCodeAPI.GetFollowOnExit(), VSCodeAPI.RepositoryURL()])
    followOnExit.value = f
    siteUrl.value = u
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  const next = !followOnExit.value
  followOnExit.value = next
  try {
    await VSCodeAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭两形态托管实例' : '已关闭：Hanxi 退出不影响 VS Code，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await VSCodeAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用便携版）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
  }
}

async function copySite() {
  const ok = await copy(siteUrl.value)
  showToast(ok ? '官网地址已复制' : '复制失败')
}

async function openSite() {
  try {
    await VSCodeAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 订阅与生命周期 ----------
useWailsEvent<DownloadProgress>('vscode:version-download', (t) => {
  if (!t || !t.version || !t.form) return
  const key = dlKey(t.form as Form, t.version)
  downloading.value = { ...downloading.value, [key]: t }
  if (t.stage === 'done') {
    setTimeout(() => {
      const next = { ...downloading.value }
      delete next[key]
      downloading.value = next
    }, 800)
    loadVersions()
    refreshStatus()
  }
})

useWailsEvent<Snapshot>('vscode:instance-state', (s) => {
  if (!s || !status.value) return
  if (s.form === 'installer') {
    status.value = { ...status.value, installer: s }
    if (s.state !== 'running') uptimeI.value = 0
  } else {
    status.value = { ...status.value, portable: s }
    if (s.state !== 'running') uptimeP.value = 0
  }
})

usePolling(refreshStatus, 2500)
usePolling(uptimeTick, 1000)

onMounted(() => {
  loadVersions()
  loadExtras()
})

const remoteList = computed(() => (remoteForm.value === 'installer' ? releasesI.value : releasesP.value))
</script>

<template>
  <section class="page vscode-view">
    <PageHeader title="VS Code" subtitle="托管 Visual Studio Code：便携版隔离安装 + 安装版静默升级，双通道启停与窗口唤起。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab：两形态独立引擎 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <div v-for="form in (['portable', 'installer'] as Form[])" :key="form" class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <span class="form-tag" :class="form">{{ form === 'portable' ? '便携版' : '安装版' }}</span>
            <span class="vc-status-light" :class="(form === 'portable' ? portSnap?.state : instSnap?.state) || ''"></span>
            <span class="status-word">{{ toolStateMeta((form === 'portable' ? portSnap?.state : instSnap?.state) || '') .text }}</span>
            <template v-if="(form === 'portable' ? portSnap : instSnap)?.state === 'running' && (form === 'portable' ? portSnap?.version : instSnap?.version)">
              <span class="ver-pill">{{ form === 'portable' ? portSnap?.version : instSnap?.version }}</span>
              <span v-if="(form === 'portable' ? portSnap?.pid : instSnap?.pid)" class="mono pid-tag">PID {{ form === 'portable' ? portSnap?.pid : instSnap?.pid }}</span>
            </template>
            <span v-if="(form === 'portable' ? portSnap : instSnap)?.state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(form === 'portable' ? uptimeP : uptimeI) }}</span>
          </div>
          <div class="control-btns">
            <button
              class="btn btn-secondary btn-small"
              :disabled="busy || (form === 'portable' ? portSnap?.state : instSnap?.state) === 'starting'"
              :title="form === 'portable' ? '启动便携版并打开窗口（数据自包含于 data\\）' : '启动本机安装版 VS Code'"
              @click="openWindow(form)"
            >🗔 打开窗口</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || !['running', 'starting', 'external'].includes((form === 'portable' ? portSnap?.state : instSnap?.state) || '')"
              :title="['external'].includes((form === 'portable' ? portSnap?.state : instSnap?.state) || '') ? '外部实例不越权终止' : '关闭窗口消息，宽限后 JobObject 兜底'"
              @click="quitForm(form)"
            >⏻ 退出</button>
            <button
              v-if="form === 'portable' && openDirVersion"
              class="btn btn-ghost btn-small"
              @click="openDir(openDirVersion.dir)"
            >📂 位置</button>
          </div>
        </div>
        <UiBanner v-if="bannerFor(form === 'portable' ? portSnap : instSnap, form)" :tone="bannerFor(form === 'portable' ? portSnap : instSnap, form)!.tone" class="slim">
          {{ bannerFor(form === 'portable' ? portSnap : instSnap, form)!.text }}
        </UiBanner>
        <div v-else-if="form === 'portable' && (portSnap?.state ?? '') === 'stopped'" class="hint-line">
          便携版尚未运行：点击「打开窗口」启动（data\ 全自包含，与日常 VS Code 隔离）；未下载过请先到「版本管理」下载便携版或导入本地目录。
        </div>
        <div v-else-if="form === 'installer' && installedInfo?.installed && (instSnap?.state ?? '') === 'stopped'" class="hint-line">
          本机安装版 VS Code {{ installedInfo.version }}：可直接托管启停。注意与日常使用共实例组（唤窗互达），且应用内自动更新可能令此处版本记录漂移。
        </div>
        <div v-else-if="form === 'installer' && !installedInfo?.installed" class="hint-line">
          本机暂无安装版：可在「版本管理 → 安装版」通道静默安装（用户级、免 UAC）；日常只写代码建议直接用便携版托管。
        </div>
      </div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 VS Code 双形态托管</summary>
        <div class="info-body">
          <p>VS Code 是微软的开源代码编辑器（<a class="inline-link" href="https://code.visualstudio.com" target="_blank" rel="noopener">code.visualstudio.com</a>，MIT）。上游二进制仅微软官方 CDN 分发：便携版（ZIP 归档）解压即用，配置/扩展全部落在版本目录 data\ 内；安装版（User Installer）为免 UAC 用户级安装，位置跟随本机既有安装（以注册表为准）。</p>
          <p class="hint-dim">两形态实例组天然隔离可并行运行；便携版无应用内自动更新，版本由 Hanxi 统一管理。官方 sha256 仅对最新版可得，历史版本自动降级为字节数 + CRC32 + 布局三层校验（卡片会如实标注）。</p>
          <p class="hint-dim">不设空闲自动退出：VS Code 关窗即退，窗口开着说明你正在编辑，强退反需求。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（两形态共用开关） -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（两形态共用；关闭后 Hanxi 退出完全不影响 VS Code）</span></span>
        </label>
        <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
      </div>
      <div class="repo-row">
        <span class="k">官方网站</span>
        <code class="mono repo-addr">{{ siteUrl }}</code>
        <button class="link-button" @click="copySite">复制</button>
        <button class="link-button" @click="openSite">浏览器打开</button>
      </div>
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>便携版已装 <strong>{{ installed.length }}</strong> 个 · 远程便携 {{ releasesP.length }} 个 · 远程安装器 {{ releasesI.length }} 个</span>
          <span class="hint-dim">全部资产来自微软官方 CDN（update.code.visualstudio.com）；下载后自动补建 data\ 便携数据目录</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地便携版</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 安装版本机探测卡 -->
      <div class="section-title"><h3>本机安装版（注册表感知）</h3></div>
      <div v-if="installedInfo?.installed" class="installed-grid">
        <div class="installed-card">
          <div class="inst-card-top">
            <span class="ver-tag">{{ installedInfo.version }}</span>
            <div class="inst-badges">
              <span class="badge badge-official">User Installer</span>
              <span v-if="instSnap?.state === 'running'" class="badge badge-running">运行中</span>
              <span v-else-if="instSnap?.state === 'external'" class="badge badge-import">外部运行</span>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">位置</span><code class="mono">{{ installedInfo.dir }}</code></div>
            <div class="meta-line"><span class="k">托管</span><span class="hint-dim">与日常使用共实例组；升级请回远程表点「安装/升级」（运行中会先要求确认）</span></div>
          </div>
          <div class="inst-actions">
            <button class="btn btn-secondary btn-small" @click="openDir(installedInfo.dir)">📂 打开位置</button>
          </div>
        </div>
      </div>
      <div v-else class="empty-state">
        <p>本机未检测到安装版 VS Code（安装版仅本机一份；如需在 Hanxi 外自行安装亦会被自动感知）</p>
      </div>

      <!-- 已安装便携版 -->
      <div class="section-title"><h3>已安装便携版 ({{ installed.length }})</h3></div>
      <div v-if="installed.length === 0" class="empty-state first-use">
        <p>尚未安装 VS Code 便携版 —— 下载官方归档（zip），或「导入本地便携版」把已有目录收纳进来</p>
        <button v-if="releasesP.length" class="btn btn-primary" @click="download('portable', releasesP[0])">
          下载最新版 {{ releasesP[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </div>
      <div class="installed-grid">
        <div v-for="v in installed" :key="v.version" class="installed-card" :class="{ 'card-active': activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <span v-if="activeVersion === v.version" class="badge badge-active">使用中</span>
              <span v-else-if="portSnap?.state === 'running' && portSnap?.version === v.version" class="badge badge-running">运行中</span>
              <span v-if="v.isImport" class="badge badge-import">本地导入</span>
              <span v-else class="badge" :class="v.verified ? 'badge-official' : 'badge-weak'">{{ v.verified ? '官方哈希' : '三层校验' }}</span>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
            <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
            <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
          </div>
          <div class="inst-actions">
            <button v-if="activeVersion !== v.version" class="btn btn-primary btn-small" @click="setActive(v)">设为使用</button>
            <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 位置</button>
            <button class="btn btn-secondary btn-small" title="打开便携数据目录 data\（user-data 与 extensions）" @click="openDir(v.dir + '\\data')">🗂 数据</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="portSnap?.state === 'running' && portSnap?.version === v.version"
              :title="portSnap?.state === 'running' && portSnap?.version === v.version ? '请先退出该便携版实例' : ''"
              @click="removeVersion(v)"
            >卸载</button>
          </div>
        </div>
      </div>

      <!-- 远程可用版本（形态切换） -->
      <div class="section-title vc-remote-head">
        <h3>远程可用版本</h3>
        <div class="vc-form-switch">
          <button class="btn btn-small" :class="remoteForm === 'portable' ? 'btn-primary' : 'btn-ghost'" @click="remoteForm = 'portable'">便携 ZIP</button>
          <button class="btn btn-small" :class="remoteForm === 'installer' ? 'btn-primary' : 'btn-ghost'" @click="remoteForm = 'installer'">安装器 EXE</button>
        </div>
      </div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 130px;">版本</th>
              <th style="width: 170px;">状态</th>
              <th style="width: 90px;">大小</th>
              <th style="width: 110px;">Commit</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rel in remoteList" :key="rel.version">
              <td>
                <strong class="ver-name">{{ rel.version }}</strong>
                <span v-if="remoteForm === 'portable' && rel.version === releasesP[0]?.version && rel.sha256" class="badge badge-hash">官方哈希</span>
              </td>
              <td>
                <!-- 类名带 vc- 前缀，防 App 全局 .status-dot（7px）压扁表格圆点（markeron 事故教训） -->
                <span v-if="statusOf(remoteForm, rel) === 'installed'" class="vc-ver-status installed">已安装</span>
                <span v-else-if="statusOf(remoteForm, rel) === 'downloading'" class="vc-ver-status downloading">{{ remoteForm === 'installer' ? '安装中' : '下载中' }}</span>
                <span v-else-if="statusOf(remoteForm, rel) === 'error'" class="vc-ver-status error">失败</span>
                <span v-else class="vc-ver-status idle">{{ remoteForm === 'installer' ? '可安装' : '可下载' }}</span>
              </td>
              <td>{{ fmtSize(rel.size) }}</td>
              <td><code class="mono">{{ (rel.commit || '').slice(0, 10) }}</code></td>
              <td>
                <div v-if="statusOf(remoteForm, rel) === 'downloading' && downloading[dlKey(remoteForm, rel.version)]?.stage === 'downloading'" class="download-cell">
                  <div class="dl-bar-wrap">
                    <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[dlKey(remoteForm, rel.version)]!)}%` }"></div>
                  </div>
                  <span class="dl-percent">{{ stepOf(downloading[dlKey(remoteForm, rel.version)]!) }}%</span>
                </div>
                <div v-else-if="statusOf(remoteForm, rel) === 'downloading'" class="dl-meta-text">
                  <span v-if="['verify', 'extract', 'install'].includes(downloading[dlKey(remoteForm, rel.version)]!.stage)">
                    {{ downloading[dlKey(remoteForm, rel.version)]!.stage === 'install' ? '静默安装中…' : '校验解压中…' }}
                  </span>
                  <span v-else class="dl-error" :title="downloading[dlKey(remoteForm, rel.version)]!.message">{{ downloading[dlKey(remoteForm, rel.version)]!.message }}</span>
                </div>
                <div v-else-if="statusOf(remoteForm, rel) === 'error'" class="dl-meta-text">
                  <span class="dl-error" :title="downloading[dlKey(remoteForm, rel.version)]!.message">{{ downloading[dlKey(remoteForm, rel.version)]!.message }}</span>
                </div>
                <button
                  v-if="statusOf(remoteForm, rel) === 'idle'"
                  class="btn btn-primary btn-small"
                  @click="download(remoteForm, rel)"
                >{{ remoteForm === 'installer' ? '安装/升级' : '下载安装' }}</button>
                <span v-if="statusOf(remoteForm, rel) === 'installed'" class="btn btn-ghost btn-small">已安装</span>
                <a v-if="statusOf(remoteForm, rel) === 'error'" class="retry-link" @click="download(remoteForm, rel)">重试</a>
              </td>
            </tr>
            <tr v-if="remoteList.length === 0 && !loading">
              <td colspan="5" class="empty-hint">无法加载远程版本列表（官方端点不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="hint-line">
        {{ remoteForm === 'portable'
          ? '便携 ZIP：解压到 versions/vscode_X.Y.Z/ 隔离目录并补建 data\；多版本并存互不干扰。'
          : '安装器 EXE：官方 User Installer 静默安装（免 UAC），安装位置跟随本机既有安装；运行中的 VS Code（含您日常窗口）会被强制关闭后再安装——历史版本无官方哈希时按三层校验放行。' }}
      </div>
    </div>
  </section>
</template>

<style scoped>
.vscode-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }
.slim { padding: 8px 12px; font-size: 12px; }

/* ---------- 控制台（双形态整合条） ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 8px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.form-tag {
  font-size: 11px; font-weight: 700; padding: 2px 8px; border-radius: var(--radius-pill);
  background: var(--surface-hover); color: var(--color-text-muted); flex-shrink: 0;
}
.form-tag.installer { background: var(--state-information-soft); color: var(--state-information); }
/* 信号灯类名带 vc- 前缀，与全局样式隔离（markeron 垂直字体事故教训） */
.vc-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.vc-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.vc-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.vc-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.vc-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示与说明卡 ---------- */
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; line-height: 1.6; }
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

/* ---------- 版本管理 ---------- */
.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.vc-remote-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 10px; flex-wrap: wrap; }
.vc-form-switch { display: flex; gap: 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: var(--radius-control); border: 1px dashed var(--color-border); }

.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color var(--motion-base) ease;
}
.installed-card.card-active { border-color: var(--color-primary); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; flex-wrap: wrap; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-weak { background: var(--state-warning-soft); color: var(--state-warning); }
.badge-hash { background: var(--state-positive-soft); color: var(--state-positive); margin-left: 6px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }
.mono { font-size: 11px; }
.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; flex-wrap: wrap; }

.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.vc-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.vc-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.vc-ver-status.installed::before { background: var(--state-positive); }
.vc-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.vc-ver-status.error::before { background: var(--state-danger); }
.vc-ver-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
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
