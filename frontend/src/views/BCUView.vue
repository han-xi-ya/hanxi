<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, watch, onMounted } from 'vue'
import * as BCUAPI from '../../bindings/hanxi/internal/modules/bcu/bcuservice'
import type { BCURelease, BCUVersionInfo } from '../../bindings/hanxi/internal/modules/bcu/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/bcu/instance/models'
import type { ControlOutcome, QuitOutcome, DotnetEnv } from '../../bindings/hanxi/internal/modules/bcu/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/bcu/version/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'

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
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与各工具模块同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🧹 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 注意：BCU 习惯文案"运行中"与 constants/status 的通用"已启动"不一致，
// 迁移纪律保持逐字现状不强凑（统一措辞需产品决策，已记迁移报告）。
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
      tone: 'warn' as const,
      text: '检测到外部 BCUninstaller 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 BCU 窗口内关闭。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error' as const, text: snap.value?.error || 'BCU 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok' as const,
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
  const ok = await confirm({
    title: `确定卸载 BCU ${v.version}？`,
    description: '该版本隔离目录（含卸载历史与设置数据）将被删除，不可恢复。',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await BCUAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地安装',
    label: '请输入 BCU 便携目录完整路径（含 BCUninstaller.exe）',
    description: '将整套迁入：exe + 运行时 + BCUninstaller.settings 卸载历史均保留',
    placeholder: 'C:\\Program Files\\BCUninstaller',
  })
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

// 停止轮询（切后台/卸载）时运行时长归零——对齐迁移前 stopTimers 语义
watch(statusPolling.isPolling, (running) => {
  if (!running) uptimeSec.value = 0
})

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
  const next = !followOnExit.value
  followOnExit.value = next // 用户点击已将勾选框翻转，ref 同步跟进，保持绑定状态一致
  try {
    await BCUAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
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
  if (!(await copy(repoUrl.value))) {
    showToast('复制失败: 剪贴板不可用')
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

// ---------- 事件订阅（useWailsEvent 自动注销）与初始装载 ----------
useWailsEvent<DownloadProgress>('bcu:version-download', (t) => {
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

useWailsEvent<Snapshot>('bcu:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

onMounted(() => {
  // 状态首帧由 usePolling 的 mounted 即触发（immediateFirstRun）
  void Promise.all([loadVersions(), loadDotnetEnv(), loadExtras()])
})
</script>

<template>
  <section class="page bcu-view">
    <PageHeader title="BC 卸载工具" subtitle="托管 Bulk Crap Uninstaller：版本管理、启停与窗口唤起，批量卸载干净又彻底。">
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
          <span class="bcu-status-light" :class="state"></span>
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
            :title="state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 BCU 并打开主窗口'"
            @click="openWindow"
          >🗔 打开窗口</UiButton>
          <UiButton
            variant="danger"
            small
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 BCU 窗口内关闭' : '关闭窗口消息（挂起时强杀兜底）'"
            @click="quitBCU"
          >⏻ 退出</UiButton>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
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
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出完全不影响该工具）</span></span>
        </label>
        <UiButton variant="secondary" small @click="createShortcut">🖥 创建桌面快捷方式</UiButton>
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
        <UiButton variant="secondary" small :disabled="busy" @click="importLocal">⇥ 导入本地安装</UiButton>
        <UiButton variant="secondary" small :disabled="loading" @click="loadVersions">
          {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
        </UiButton>
      </div>
    </div>

    <!-- 已安装版本 -->
    <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

    <UiEmptyState v-if="installed.length === 0" class="first-use">
      <p>尚未安装 BCU —— 下载官方便携版，或「导入本地安装」把现有 BCU 收纳进来</p>
      <UiButton v-if="releases.length && recommendedVariant" variant="primary" @click="download(releases[0], recommendedVariant)">
        下载最新版 {{ releases[0].version }}（{{ variantLabel(recommendedVariant) }}，推荐）
      </UiButton>
      <UiButton v-else-if="releases.length" variant="primary" @click="download(releases[0], 'portable')">
        下载最新版 {{ releases[0].version }}
      </UiButton>
      <UiButton v-else-if="!loading" variant="secondary" @click="loadVersions">↻ 刷新远程列表</UiButton>
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
          <UiButton v-if="activeVersion !== v.version" variant="primary" small @click="setActive(v)">设为使用</UiButton>
          <UiButton variant="secondary" small @click="openDir(v.dir)">📂 打开位置</UiButton>
          <UiButton
            variant="danger"
            small
            :disabled="state === 'running' && runningVersion === v.version"
            :title="state === 'running' && runningVersion === v.version ? '请先退出 BCU' : ''"
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
              <!-- 类名刻意用 bcu-status（含 bcu- 前缀）——全局原子有 .chip 族，防语义混淆 -->
              <span v-if="statusOverall(rel) === 'installed'" class="bcu-ver-status installed">已安装</span>
              <span v-else-if="statusOverall(rel) === 'downloading'" class="bcu-ver-status downloading">下载中</span>
              <span v-else-if="statusOverall(rel) === 'error'" class="bcu-ver-status error">失败</span>
              <span v-else class="bcu-ver-status idle">可安装</span>
            </td>
            <td>{{ fmtSize(rel.size) }}</td>
            <td>{{ fmtDate(rel.published) }}</td>
            <td>
              <template v-if="statusOf(rel, 'portable') === 'installed'">
                <span class="chip chip-positive">已安装</span>
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
/* 仅保留本视图独有的业务样式；
   .btn 家族 / .tbl / .header-row / .subtitle / .error-box / .empty-state / .mono /
   .chip / .banner / .link-button / main-tab-* / keyframes pulse 等通用形
   已上收 components.css 与 ui/* 组件，重复副本全部删除。 */
.bcu-view { display: flex; flex-direction: column; gap: 10px; }

/* UiBanner 沿用迁移前 slim 密度 */
.tab-body .banner { padding: 8px 12px; font-size: 12px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 bcu- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.bcu-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.bcu-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.bcu-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.bcu-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.bcu-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }

/* ---------- 折叠说明 ---------- */
.info-details { border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; gap: 4px; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-summary::after { content: '▸'; font-size: 10px; transition: transform var(--motion-base); margin-left: auto; }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
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
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
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
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.bcu-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.bcu-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.bcu-ver-status.installed::before { background: var(--state-positive); }
.bcu-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.bcu-ver-status.error::before { background: var(--state-danger); }
.bcu-ver-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.retry-link { color: var(--color-primary); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 双变体下载与 .NET 环境徽标 ---------- */
.variant-btns { display: flex; gap: 6px; align-items: center; }
.variant-progress { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.variant-progress .dl-meta-text { font-size: 11px; }
.dotnet-banner { font-size: 12px; padding: 4px 10px; border-radius: 6px; border: 1px solid transparent; max-width: 680px; }
.dotnet-banner.ok { background: var(--state-positive-soft); color: var(--state-positive); border-color: var(--state-positive-glow); }
.dotnet-banner.warn { background: var(--state-warning-soft); color: var(--state-warning); border-color: var(--state-warning-glow); }
.dotnet-banner b { font-weight: 700; }
</style>
