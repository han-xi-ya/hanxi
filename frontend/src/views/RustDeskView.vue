<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期（骨架基于重构共享层：
// usePolling/useWailsEvent/useAsyncAction/useConfirm/usePrompt/useClipboard + utils/format）
import { ref, computed, onMounted, onDeactivated } from 'vue'
import * as RustDeskAPI from '../../bindings/hanxi/internal/modules/rustdesk/rustdeskservice'
import type { RDRelease, RDVersionInfo } from '../../bindings/hanxi/internal/modules/rustdesk/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/rustdesk/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/rustdesk/version/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<RDRelease[]>([])
const installed = ref<RDVersionInfo[]>([])
const activeVersion = ref('')
const activeForm = ref('') // portable / installed（两形态版本号可同值，成对判定）
const loading = ref(false)
const listError = ref('')
const uptimeSec = ref(0)

const { busy, run } = useAsyncAction()
const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 下载进度 map（按版本索引）：便携与安装版通道独立（kind 分流，互不覆盖）
const downloading = ref<Record<string, DownloadProgress>>({})
const installing = ref<Record<string, DownloadProgress>>({})

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/ccswitch 同构）
const activeMainTab = ref('console')
const mainTabs = [
  { key: 'console', label: '🌍 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 五态通用文案接 constants/status 单一来源（§9.5-5）；业务扩展话术视图自行覆写。
const stateText = computed(() => toolStateMeta(state.value).text)

const runningVersion = computed(() => snap.value?.version ?? '')

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = state.value === 'running' && runningVersion.value ? runningVersion.value : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

const isInstalledForm = computed(() => snap.value?.form === 'installed')

// 条件提示条（三个变体互斥）
const banner = computed<{ tone: 'ok' | 'warn' | 'error'; text: string } | null>(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部自行启动的 RustDesk 实例（便携或安装版客户端，非 Hanxi 托管）。可尝试唤起其窗口；驻留托盘无窗可唤时点击其托盘图标，或在其中自行退出。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'RustDesk 异常退出' }
  }
  if (state.value === 'running') {
    if (isInstalledForm.value) {
      return {
        tone: 'ok',
        text: 'RustDesk 安装版正在运行：系统服务在位时可无人值守被控（锁屏/登录界面亦可达）；「退出」仅终止 Hanxi 拉起的客户端进程树，服务不受影响、被控仍可达。',
      }
    }
    return {
      tone: 'ok',
      text: 'RustDesk 正在运行：被控端保持窗口或托盘驻留即可被设备 ID 接入（默认经官方公共信令，生产建议在其设置中改用自建服务器）；「退出」会终止整个进程树（断开进行中的会话）。',
    }
  }
  return null
})

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remote, local, active, form] = await Promise.all([
      RustDeskAPI.ListReleases(),
      RustDeskAPI.ListInstalledVersions(),
      RustDeskAPI.GetActiveVersion(),
      RustDeskAPI.GetActiveForm(),
    ])
    releases.value = remote ?? []
    installed.value = local ?? []
    activeVersion.value = active ?? ''
    activeForm.value = form ?? ''
  } catch (e) {
    listError.value = `获取版本列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function refreshStatus() {
  try {
    snap.value = await RustDeskAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('rustdesk GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: RDRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  // 形态精确匹配：仅系统安装版命中时，便携通道不应误显"已安装"
  const hit = installed.value.find(v => v.version === rel.version && v.form !== 'installed')
  return hit ? 'installed' : 'idle'
}

// 安装版通道状态（rel 无 installerName = 上游未提供 MSI，无此通道）
function installStatusOf(rel: RDRelease): 'installed' | 'downloading' | 'error' | 'idle' | 'unavailable' {
  if (!rel.installerName) return 'unavailable'
  const p = installing.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version && v.form === 'installed')
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  const r = await run(() => RustDeskAPI.OpenWindow())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await refreshStatus()
}

async function quitRustDesk() {
  if (busy.value) return
  const r = await run(() => RustDeskAPI.Quit())
  showToast(r.ok ? r.data.message : `退出失败: ${getErrorMessage(r.error)}`)
  await refreshStatus()
}

// ---------- 版本管理操作 ----------
async function download(rel: RDRelease) {
  try {
    const res = await RustDeskAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

// 安装版一键"下载并安装"：安装本体是 Windows Installer 前台向导（含 UAC），
// 进度（下载/校验）走事件，装机动作由用户在弹出的向导中完成。
async function install(rel: RDRelease) {
  try {
    const res = await RustDeskAPI.InstallVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`RustDesk 安装版 ${rel.version} 已在系统中`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`安装发起失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: RDVersionInfo) {
  try {
    const ver = await RustDeskAPI.SetActiveVersion(v.version, v.form)
    activeVersion.value = ver
    activeForm.value = v.form
    showToast(v.form === 'installed'
      ? `已将系统安装版 ${ver} 设为使用版本`
      : `已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

function isActiveCard(v: RDVersionInfo): boolean {
  return activeVersion.value === v.version && (activeForm.value || 'portable') === (v.form || 'portable')
}

async function openDir(path: string) {
  try {
    await RustDeskAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}


async function openConfigDir() {
  try {
    await RustDeskAPI.OpenConfigDir()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: RDVersionInfo) {
  const ok = await confirm({
    title: `卸载 RustDesk ${v.version}（便携版）`,
    description: '该版本隔离目录将被删除，不可恢复。',
    details: [{ label: '配置数据', value: '设备 ID、地址簿与设置恒存于 %APPDATA%\\RustDesk，后续版本继续共用' }],
    tone: 'danger',
    confirmLabel: '卸载',
  })
  if (!ok) return
  try {
    await RustDeskAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

// 安装版卸载引导：系统级卸载（连带移除服务）交还 Windows 原生入口，
// Hanxi 不代执行 msiexec——后端打开设置页，用户在「安装的应用」中操作。
async function uninstallGuidance(v: RDVersionInfo) {
  const ok = await confirm({
    title: `卸载 RustDesk 安装版 ${v.version}`,
    description: '安装版是系统级软件：卸载将连带移除 Windows 服务（无人值守被控随之失效），需在系统设置中完成。',
    details: [
      { label: '操作去向', value: '打开 Windows 设置「安装的应用」，搜索 RustDesk 后卸载' },
      { label: '配置数据', value: '卸载器默认保留 %APPDATA%\\RustDesk 设备 ID 与设置' },
    ],
    tone: 'danger',
    confirmLabel: '打开系统设置',
  })
  if (!ok) return
  try {
    await RustDeskAPI.OpenUninstallSettings()
    showToast('已打开系统「安装的应用」页面：搜索 RustDesk 完成卸载后回本页刷新')
  } catch (e) {
    showToast(`打开系统设置失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地便携版',
    label: 'exe 完整路径或所在目录',
    description: '官方形态文件名 rustdesk-版本-x86_64.exe；安装版/MSI 不支持导入。配置恒在 %APPDATA%\\RustDesk，与安装位置无关',
  })
  if (!path) return
  const r = await run(() => RustDeskAPI.ImportLocal(path.trim()))
  if (r.ok) {
    showToast(`已导入 RustDesk ${r.data.version}`)
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

// ---------- 联动开关 / 桌面快捷方式 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([RustDeskAPI.GetFollowOnExit(), RustDeskAPI.RepositoryURL()])
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
    await RustDeskAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await RustDeskAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
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
    await RustDeskAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件订阅（自动注销）与装载 ----------
useWailsEvent<DownloadProgress>('rustdesk:version-download', (t) => {
  if (!t || !t.version) return
  const isInstaller = t.kind === 'installer'
  const map = () => (isInstaller ? installing.value : downloading.value)
  const commit = (next: Record<string, DownloadProgress>) => {
    if (isInstaller) installing.value = next
    else downloading.value = next
  }
  const cur = { ...map() }
  cur[t.version] = t
  commit(cur)
  if (t.stage === 'done') {
    setTimeout(() => {
      const next = { ...map() }
      delete next[t.version]
      commit(next)
    }, 800)
    loadVersions()
  }
})

useWailsEvent<Snapshot>('rustdesk:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

onMounted(async () => {
  await Promise.all([refreshStatus(), loadVersions(), loadExtras()])
})
</script>

<template>
  <section class="page rustdesk-view">
    <PageHeader
      title="RustDesk 公网远程桌面"
      subtitle="托管自托管优先的开源远程桌面 RustDesk：版本管理、JobObject 托管启停与窗口唤起；跨公网 ID/中继接入，与「SubnetDesk 局域网」互补共存。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <!-- 顶部整合条：状态 + 启停按钮，一行内解决问题 -->
      <div class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <span class="rd-status-light" :class="state"></span>
            <span class="status-word">{{ stateText }}</span>
            <template v-if="isRunningOrStarting && runningVersion">
              <span class="ver-pill">{{ runningVersion }}</span>
              <span v-if="isInstalledForm" class="form-pill" title="MSI 系统安装版（Program Files + Windows 服务）">安装版</span>
              <span v-else class="form-pill form-pill-portable" title="隔离目录便携版（Hanxi 全托管）">便携版</span>
              <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
            </template>
            <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
          </div>
          <div class="control-btns">
            <button
              class="btn btn-secondary btn-small"
              :disabled="busy || state === 'starting'"
              :title="state === 'running' ? '唤起窗口（驻留托盘无窗时派生新窗口）' : state === 'starting' ? '启动中…' : '启动 RustDesk 并打开窗口'"
              @click="openWindow"
            >🗔 打开窗口</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部实例请在其托盘退出' : isInstalledForm ? '终止客户端进程树（系统服务不受影响，被控仍可达）' : '终止托管进程树（便携版无优雅退出，进行中的会话会断开）'"
              @click="quitRustDesk"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <!-- 条件提示条 / 引导行 -->
      <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「打开窗口」启动。被控端记下窗口显示的设备 ID 并设置密码，控制端输入对端 ID 发起连接（跨网经中继，局域网内可开启直连访问）。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 RustDesk（首次启动需解压内置负载，可能较慢）…</div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 RustDesk · 与 SubnetDesk 如何组合</summary>
        <div class="info-body">
          <p>自托管优先的开源远程桌面（<a class="inline-link" href="https://github.com/rustdesk/rustdesk" target="_blank" rel="noopener">rustdesk/rustdesk</a>，AGPL-3.0）：设备 ID + 信令/中继架构，公网开箱即用（默认官方公共服务器，可换自建 rendezvous/relay），支持文件传输、TCP 隧道与中继直连回退。版本下载自官方 GitHub Releases（官方 sha256 校验），启停受 JobObject 管控。</p>
          <p>组合分工：跨公网/跨 NAT 场景用 <strong>RustDesk</strong>（设备 ID 接入）；同局域网/VPN 场景推荐 <strong>SubnetDesk</strong>（其 LAN fork：IP 直连 + mDNS 发现，无服务器依赖）。两者<b>协议互不兼容</b>——RustDesk 不能接入 SubnetDesk 主机，反之亦然，两端需安装同一软件；端口（21116/21117 vs 21118）与配置目录互不冲突，可同机并行。</p>
          <p class="hint-dim">两形态分工：便携版（隔离目录全托管）不装 Windows 服务，被控仅在进程/托盘存活期间可被接入，UAC 与安全桌面场景受限；安装版（MSI 系统安装）自带 LocalSystem 服务，支持无人值守/锁屏被控——Hanxi 负责下载校验、发起安装向导与客户端纳管，服务与被控常驻不归 Hanxi 管辖（退出客户端后仍可达）。设备 ID、地址簿与设置恒存于 %APPDATA%\RustDesk，两形态多版本共享，不建议双客户端并行运行。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出完全不影响该工具；被控常驻场景建议关闭本项或改用系统安装版）</span></span>
        </label>
        <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
        <button class="btn btn-secondary btn-small" title="打开 RustDesk 用户数据目录（%APPDATA%\RustDesk，身份密钥 / 地址簿 / 设置）" @click="openConfigDir">🗂 数据目录</button>
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
          <span>本机可用 <strong>{{ installed.length }}</strong> 个版本 · 远程版本 {{ releases.length }} 个</span>
          <span class="hint-dim">便携包为官方单文件 packer exe（rustdesk-版本-x86_64.exe，GitHub digest 校验，下载即安装）；或「导入本地」收纳你机器上已有的便携版</span>
          <span class="hint-dim">安装版为官方 MSI（GitHub digest 校验）：下载后由 Windows Installer 向导装机（含 UAC），装入 Program Files 并注册系统服务，支持无人值守被控；识别与卸载交还系统</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地便携版</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

      <UiEmptyState v-if="installed.length === 0">
        <p>尚未安装 RustDesk —— 下载官方便携版 / 安装系统版，或「导入本地便携版」把现有下载收纳进来</p>
        <div class="empty-btns">
          <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
            下载便携版 {{ releases[0].version }}
          </button>
          <button v-if="releases.length && releases[0].installerName" class="btn btn-secondary" @click="install(releases[0])">
            安装系统版 {{ releases[0].version }}
          </button>
          <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
        </div>
      </UiEmptyState>

      <div class="installed-grid">
        <div v-for="v in installed" :key="`${v.form}-${v.version}`" class="installed-card" :class="{ 'card-active': isActiveCard(v) }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <span v-if="isActiveCard(v)" class="badge badge-active">使用中</span>
              <span v-else-if="state === 'running' && runningVersion === v.version" class="badge badge-running">运行中</span>
              <span v-if="v.form === 'installed'" class="badge badge-system" title="MSI 系统安装版：含 Windows 服务，卸载走系统设置">安装版</span>
              <span v-else-if="v.isImport" class="badge badge-import">本地导入</span>
              <span v-else class="badge badge-official">官方下载</span>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">形态</span><span>{{ v.form === 'installed' ? '安装版（Program Files + 系统服务）' : '便携版（隔离目录全托管）' }}</span></div>
            <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
            <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
            <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
          </div>
          <div class="inst-actions">
            <button v-if="!isActiveCard(v)" class="btn btn-primary btn-small" @click="setActive(v)">设为使用</button>
            <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 打开位置</button>
            <button
              v-if="v.form === 'installed'"
              class="btn btn-danger-outline btn-small"
              title="系统级卸载（连带移除服务）交还 Windows 设置完成，Hanxi 不代执行"
              @click="uninstallGuidance(v)"
            >系统卸载…</button>
            <button
              v-else
              class="btn btn-danger-outline btn-small"
              :disabled="state === 'running' && runningVersion === v.version"
              :title="state === 'running' && runningVersion === v.version ? '请先退出 RustDesk' : ''"
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
                <!-- 类名刻意用 rd-ver-status（含 rd- 前缀）——防与全局/其他视图状态点样式碰撞（markeron 垂直字体事故教训） -->
                <div class="dual-status">
                  <span class="ch-label">便携</span>
                  <span v-if="statusOf(rel) === 'installed'" class="rd-ver-status installed">已装</span>
                  <span v-else-if="statusOf(rel) === 'downloading'" class="rd-ver-status downloading">下载中</span>
                  <span v-else-if="statusOf(rel) === 'error'" class="rd-ver-status error">失败</span>
                  <span v-else class="rd-ver-status idle">可下载</span>
                </div>
                <div v-if="installStatusOf(rel) !== 'unavailable'" class="dual-status">
                  <span class="ch-label">安装版</span>
                  <span v-if="installStatusOf(rel) === 'installed'" class="rd-ver-status installed">已装</span>
                  <span v-else-if="installStatusOf(rel) === 'downloading'" class="rd-ver-status downloading">{{ installing[rel.version]!.stage === 'install' ? '装机中' : '下载中' }}</span>
                  <span v-else-if="installStatusOf(rel) === 'error'" class="rd-ver-status error">失败</span>
                  <span v-else class="rd-ver-status idle">可安装</span>
                </div>
              </td>
              <td>
                <div>{{ fmtSize(rel.size) }}</div>
                <div v-if="rel.installerName" class="hint-dim size-sub">MSI {{ fmtSize(rel.installerSize) }}</div>
              </td>
              <td>{{ fmtDate(rel.published) }}</td>
              <td>
                <!-- 便携通道 -->
                <div class="ch-actions">
                  <template v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'">
                    <div class="download-cell">
                      <div class="dl-bar-wrap">
                        <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
                      </div>
                      <span class="dl-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
                    </div>
                  </template>
                  <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
                    <span v-if="['verify', 'install'].includes(downloading[rel.version]!.stage)">校验安装中…</span>
                    <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
                  </div>
                  <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                    <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
                  </div>
                  <button v-if="statusOf(rel) === 'idle'" class="btn btn-primary btn-small" @click="download(rel)">下载便携版</button>
                  <span v-if="statusOf(rel) === 'installed'" class="installed-tag">便携已装</span>
                  <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="download(rel)">重试</a>
                </div>
                <!-- 安装版通道 -->
                <div v-if="installStatusOf(rel) !== 'unavailable'" class="ch-actions">
                  <template v-if="installStatusOf(rel) === 'downloading' && installing[rel.version]!.stage === 'downloading'">
                    <div class="download-cell">
                      <div class="dl-bar-wrap">
                        <div class="dl-bar-inner" :style="{ width: `${stepOf(installing[rel.version]!)}%` }"></div>
                      </div>
                      <span class="dl-percent">{{ stepOf(installing[rel.version]!) }}%</span>
                    </div>
                  </template>
                  <div v-else-if="installStatusOf(rel) === 'downloading' && installing[rel.version]!.stage === 'verify'" class="dl-meta-text">校验安装包…</div>
                  <div v-else-if="installStatusOf(rel) === 'downloading'" class="dl-meta-text" :title="installing[rel.version]!.message">
                    <span v-if="installing[rel.version]!.stage === 'install'">🛡 安装向导已弹出，请在其中完成装机</span>
                    <span v-else class="dl-error" :title="installing[rel.version]!.message">{{ installing[rel.version]!.message }}</span>
                  </div>
                  <div v-else-if="installStatusOf(rel) === 'error'" class="dl-meta-text">
                    <span class="dl-error" :title="installing[rel.version]!.message">{{ installing[rel.version]!.message }}</span>
                  </div>
                  <button
                    v-if="installStatusOf(rel) === 'idle'"
                    class="btn btn-secondary btn-small"
                    title="下载官方 MSI 并唤起 Windows 安装向导（含 UAC）；装机后支持无人值守被控"
                    @click="install(rel)"
                  >安装系统版</button>
                  <span v-if="installStatusOf(rel) === 'installed'" class="installed-tag">系统版已装</span>
                  <a v-if="installStatusOf(rel) === 'error'" class="retry-link" @click="install(rel)">重试</a>
                </div>
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
.rustdesk-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 rd- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.rd-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.rd-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.rd-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.rd-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.rd-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
/* 形态徽标：安装版（系统服务形态）与便携版区分，运行态一目了然 */
.form-pill { font-size: 11px; padding: 1px 7px; border-radius: var(--radius-pill); background: var(--state-information-soft); color: var(--state-information); font-weight: 600; }
.form-pill-portable { background: var(--surface-hover); color: var(--color-text-muted); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
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
.badge-system { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.rd-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.rd-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.rd-ver-status.installed::before { background: var(--state-positive); }
.rd-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.rd-ver-status.error::before { background: var(--state-danger); }
.rd-ver-status.idle::before { background: var(--color-text-subtle); }

/* 「已安装」表内标记：区别于全局 .btn-ghost（悬停幽灵按钮）的静态标签态 */
.installed-tag {
  display: inline-flex; align-items: center; padding: 4px 12px; font-size: 12px;
  border-radius: var(--radius-control); border: 1px solid var(--color-border);
  background: var(--surface-hover); color: var(--color-text-muted);
}

/* 双通道（便携/安装版）行内布局：状态两行、操作两行，通道标签对齐 */
.dual-status { display: flex; align-items: center; gap: 6px; }
.dual-status + .dual-status { margin-top: 3px; }
.ch-label { font-size: 11px; color: var(--color-text-subtle); width: 34px; flex-shrink: 0; }
.ch-actions { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; min-height: 26px; }
.ch-actions + .ch-actions { margin-top: 5px; padding-top: 5px; border-top: 1px dashed var(--color-border); }
.size-sub { font-size: 11px; margin-top: 2px; }
.empty-btns { display: flex; gap: 8px; flex-wrap: wrap; justify-content: center; }

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
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
</style>
