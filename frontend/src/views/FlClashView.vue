<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, onMounted, onActivated, onDeactivated } from 'vue'
import * as FlClashAPI from '../../bindings/hanxi/internal/modules/flclash/flclashservice'
import type { FlClashRelease, FlClashVersionInfo } from '../../bindings/hanxi/internal/modules/flclash/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/flclash/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/flclash/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/flclash/version/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useClipboard } from '../composables/useClipboard'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import UiBanner from '../components/ui/UiBanner.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<FlClashRelease[]>([])
const installed = ref<FlClashVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron/everything 同构）
const activeMainTab = ref('console')
const MAIN_TABS = [
  { key: 'console', label: '⚡ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 五态通用文案接 constants/status 单一来源（§9.5-5）；业务扩展话术视图自行覆写。
const stateText = computed(() => toolStateMeta(state.value).text)

const runningVersion = computed(() => snap.value?.version ?? '')

// （迁移注记：原"打开位置"派生 openDirVersion 在本视图模板从未引用，属死代码，已清除）

// 条件提示条（三个变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn' as const,
      text: '检测到外部 FlClash 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 FlClash 托盘操作。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error' as const, text: snap.value?.error || 'FlClash 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok' as const,
      text: 'FlClash 正在运行：代理配置在其窗口内操作（配置数据在 %APPDATA% 用户目录，各版本共享）。',
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
      FlClashAPI.ListReleases(),
      FlClashAPI.ListInstalledVersions(),
      FlClashAPI.GetActiveVersion(),
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
    snap.value = await FlClashAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('flclash GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: FlClashRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await FlClashAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitFlClash() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await FlClashAPI.Quit()
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
async function download(rel: FlClashRelease) {
  try {
    const res = await FlClashAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: FlClashVersionInfo) {
  try {
    const ver = await FlClashAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await FlClashAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: FlClashVersionInfo) {
  const accepted = await confirm({
    title: `确定卸载 FlClash ${v.version}？`,
    description: '该版本隔离目录将被删除，不可恢复。\n（%APPDATA% 下的代理配置不受影响，后续版本继续共用）',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await FlClashAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '请输入 FlClash 安装目录完整路径（便携目录，含 FlClash.exe）',
    description: '提示：代理配置在 %APPDATA%，与安装位置无关',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await FlClashAPI.ImportLocal(path.trim())
    showToast(`已导入 FlClash ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// ---------- 时长 ticker 与轮询（usePolling 内置 KeepAlive 激活/停用契约） ----------
function uptimeTick() {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}

// 状态兜底轮询（事件推送之外）；首帧不自动跑——保持与迁移前一致，
// 由下方 onMounted/onActivated 显式刷新，避免多一帧请求。
usePolling(refreshStatus, 2500, { immediateFirstRun: false })
usePolling(uptimeTick, 1000, { immediateFirstRun: false })

// 停用归零运行时长（业务状态复位，非定时器管理）
onDeactivated(() => {
  uptimeSec.value = 0
})


// ---------- 联动开关 / 桌面快捷方式 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([FlClashAPI.GetFollowOnExit(), FlClashAPI.RepositoryURL()])
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
    await FlClashAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await FlClashAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  if (await copy(repoUrl.value)) {
    showToast('仓库地址已复制')
  } else {
    showToast('复制失败: execCommand 不可用')
  }
}

async function openRepo() {
  try {
    await FlClashAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件订阅（setup 期注册即不丢早期推送，卸载自动注销） ----------
useWailsEvent<DownloadProgress>('flclash:version-download', (t) => {
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

useWailsEvent<Snapshot>('flclash:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

// ---------- 生命周期 ----------
onMounted(async () => {
  await Promise.all([refreshStatus(), loadVersions(), loadExtras()])
})

// KeepAlive：页面激活时立即刷新一帧（轮询由 usePolling 随激活恢复）
onActivated(() => {
  refreshStatus()
})
</script>

<template>
  <section class="page flclash-view">
    <PageHeader title="FlClash" subtitle="托管 Clash 系代理客户端 FlClash：版本管理、启停与窗口唤起。">
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
          <span class="fl-status-light" :class="state"></span>
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
            :title="state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 FlClash 并打开窗口'"
            @click="openWindow"
          >🗔 打开窗口</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 FlClash 托盘退出' : '关闭窗口消息（驻留托盘设置下会兜底强杀）'"
            @click="quitFlClash"
          >⏻ 退出</button>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未运行：点击「打开窗口」启动 FlClash，代理配置在它的窗口内完成。配置存于 %APPDATA% 用户目录，与版本切换无关。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 FlClash（约 1~3 秒）…</div>

    <!-- 说明卡（折叠） -->
    <details class="info-details">
      <summary class="info-summary">关于 FlClash</summary>
      <div class="info-body">
      <p>
        FlClash 是跨平台 Clash 系代理客户端（Flutter 构建，Windows 便携免安装）（上游 <a
          class="inline-link"
          href="https://github.com/chen08209/FlClash"
          target="_blank"
          rel="noopener"
        >chen08209/FlClash</a>，MIT）。本模块仅托管其运行：版本下载自官方 GitHub Releases（官方 sha256 四层校验），启停受 JobObject 管控。
      </p>
      <p class="hint-dim">
        已装多个版本时，在「版本管理」设为使用即可切换；代理配置数据（%APPDATA%）各版本共享，不影响你现有的订阅与节点。
      </p>
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
        <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
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
        <span class="hint-dim">便携包下载自 GitHub Releases（Windows-Portable.zip，官方 digest 校验）；或「导入本地」把你机器上已有的安装版/绿色版收纳进来</span>
        <span class="hint-dim">上游另有 SHA256SUMS 清单资产；本模块以 GitHub API digest 为校验主依据（四层完整性）</span>
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
      <p>尚未安装 FlClash —— 下载官方便携版，或「导入本地安装」把现有 FlClash 收纳进来</p>
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
            <UiStatusChip v-if="activeVersion === v.version" tone="positive">使用中</UiStatusChip>
            <UiStatusChip v-else-if="state === 'running' && runningVersion === v.version" tone="information">运行中</UiStatusChip>
            <UiStatusChip v-if="v.isImport" tone="information">本地导入</UiStatusChip>
            <UiStatusChip v-else tone="neutral">官方下载</UiStatusChip>
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
            :title="state === 'running' && runningVersion === v.version ? '请先退出 FlClash' : ''"
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
              <UiStatusChip v-if="rel.isPre" tone="warning">预发布</UiStatusChip>
            </td>
            <td>
              <!-- 类名刻意用 fl- 前缀（信号灯同款隔离教训），防与全局原子碰撞 -->
              <span v-if="statusOf(rel) === 'installed'" class="fl-ver-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="fl-ver-status downloading">下载中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="fl-ver-status error">失败</span>
              <span v-else class="fl-ver-status idle">可安装</span>
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
              <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <button
                v-if="statusOf(rel) === 'idle'"
                class="btn btn-primary btn-small"
                @click="download(rel)"
              >下载安装</button>
              <UiStatusChip v-if="statusOf(rel) === 'installed'" tone="positive">已安装</UiStatusChip>
              <button v-if="statusOf(rel) === 'error'" class="link-button" @click="download(rel)">重试</button>
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
/* 原子层已提供的类（.btn 家族/.tbl/.error-box/.empty-state/.mono/.link-button/
   .header-row/.subtitle/.chip）副本已全部删除，由 components.css 接管。 */
.flclash-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 fl- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.fl-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.fl-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.fl-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.fl-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.fl-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示与说明卡 ---------- */
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }
.info-details { border: 1px solid var(--color-border); border-radius: 8px; background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.info-title { font-weight: 600; color: var(--color-text); }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

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
.inst-card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; flex-wrap: wrap; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; min-width: 0; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; flex-wrap: wrap; }

/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; overflow-x: auto; }
.ver-name { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }

.fl-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.fl-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.fl-ver-status.installed::before { background: var(--state-positive); }
.fl-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.fl-ver-status.error::before { background: var(--state-danger); }
.fl-ver-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
</style>
