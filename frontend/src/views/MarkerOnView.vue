<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, onMounted } from 'vue'
import * as MarkerAPI from '../../bindings/hanxi/internal/modules/markeron/markeronservice'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { DownloadProgress, MarkerRelease, MarkerVersionInfo } from '../../bindings/hanxi/internal/modules/markeron/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/markeron/instance/models'
import type { ToggleOutcome, StopOutcome } from '../../bindings/hanxi/internal/modules/markeron/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'

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
const { confirm } = useConfirm()
const { copy } = useClipboard()

// 顶层主选项卡：annotate = 标注开关，versions = 版本管理（与 frpc 的 projects/versions 同构）
const activeMainTab = ref<'annotate' | 'versions'>('annotate')
const mainTabs = [
  { key: 'annotate', label: '✎ 标注开关' },
  { key: 'versions', label: '📦 版本管理' },
]

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

// 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义
const banner = computed(() => {
  if (state.value === 'external') {
    return { tone: 'warn', text: '检测到外部 MarkerOn 实例（非 Hanxi 托管）。可切换标注；如需彻底退出请在 MarkerOn 托盘操作。' } as const
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'MarkerOn 异常退出' } as const
  }
  if (state.value === 'running' && drawing.value) {
    return { tone: 'ok', text: '标注已开启，可将屏幕涂画演示；再次点击开关或按 Ctrl+Shift+D 退出。' } as const
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
  // 危险操作统一走全局可访问确认框（useConfirm 单例，宿主在 App.vue），替代原生 window.confirm
  const accepted = await confirm({
    title: `确定卸载 MarkerOn ${versionShort}？`,
    description: '该版本隔离目录及其便携数据将被删除，不可恢复。',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await MarkerAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${versionShort}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

// 运行时长秒表（每秒从快照 startedAt 重算；KeepAlive 停用期间暂停，激活即补帧）
function uptimeTick() {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}


// ---------- 联动开关 / 桌面快捷方式 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([MarkerAPI.GetFollowOnExit(), MarkerAPI.RepositoryURL()])
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
    await MarkerAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await MarkerAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  // 剪贴板两级策略（clipboard API + execCommand 回退）已收编进 useClipboard
  const ok = await copy(repoUrl.value)
  showToast(ok ? '仓库地址已复制' : '复制失败')
}

async function openRepo() {
  try {
    await MarkerAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 订阅与生命周期 ----------
// 事件订阅（setup 期注册防丢早期推送，卸载自动注销——useWailsEvent 契约）
useWailsEvent<DownloadProgress>('markeron:version-download', (p) => {
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

useWailsEvent<Snapshot>('markeron:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

// 轮询（usePolling 内置 KeepAlive 激活/停用/卸载契约：启动幂等、切后台不空转）
usePolling(refreshStatus, 2500)
usePolling(uptimeTick, 1000)

// 初始装载（状态刷新由上方 usePolling 首跑覆盖）
onMounted(() => {
  loadVersions()
  loadExtras()
})
</script>

<template>
  <section class="page markeron-view">
    <PageHeader title="MarkerOn 桌面标注" subtitle="一键进入桌面标注态；版本与运行状态统一管理。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 标注开关 Tab -->
    <div v-show="activeMainTab === 'annotate'" class="tab-body">
    <!-- 紧凑控制台：结构与其他托管工具保持一致 -->
    <div class="control-bar">
      <div class="control-top">
        <div class="control-status">
          <span class="status-light" :class="state"></span>
          <span class="status-word">{{ stateText }}</span>
          <template v-if="isRunningOrStarting && snap?.version">
            <span class="ver-pill">v{{ snap.version }}</span>
            <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
          </template>
          <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
        </div>
        <div class="control-btns">
          <button
            class="btn btn-small annotate-toggle"
            :class="toggleClass"
            :disabled="toggleDisabled"
            :title="toggleHint || toggleSubLabel"
            @click="toggleAnnotate"
          >✎ {{ toggleLabel }}</button>
          <button
            v-if="isRunningOrStarting"
            class="btn btn-danger-outline btn-small"
            :disabled="busy"
            @click="stopAnnotate"
          >⏻ 停止</button>
          <button
            v-if="openDirVersion"
            class="btn btn-secondary btn-small"
            @click="openDir(openDirVersion!.dir)"
          >📂 打开位置</button>
        </div>
      </div>
      <div class="control-detail">
        <span v-if="state === 'running' && !drawing">MarkerOn 正在后台待命，点击「开启标注」显示桌面覆盖层。</span>
        <span v-else-if="state === 'running' && drawing">桌面覆盖层已开启，可直接进行屏幕标注。</span>
        <span v-else-if="state === 'stopped'">点击「启动 MarkerOn」后台运行，随后可开启桌面标注。</span>
        <span v-else-if="state === 'starting'">正在拉起 MarkerOn 主实例（约 1~3 秒）…</span>
        <span v-else-if="state === 'failed'">请确认已安装 WebView2 Runtime 后重试。</span>
        <span v-else-if="state === 'external'">非 Hanxi 托管的 MarkerOn 实例正在运行。</span>
      </div>
    </div>

    <!-- 条件提示条 -->
    <UiBanner v-if="banner" :tone="banner.tone" class="slim">{{ banner.text }}</UiBanner>

    <details class="info-details">
      <summary class="info-summary">快捷键与使用说明</summary>
      <div class="info-body">
        <div class="kbd-row">
          <span class="kbd-chip">Ctrl+Shift+D</span> 切换标注
          <span class="kbd-chip">Ctrl+Shift+C</span> 清空涂鸦
          <span class="kbd-chip">Ctrl+Shift+X</span> 穿透点击
        </div>
        <p class="hint-dim">按钮与快捷键等效；若状态与桌面实际不符，重新切换一次即可同步。</p>
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
.markeron-view { display: flex; flex-direction: column; gap: 10px; }
/* 页头/副标题/错误框/主选项卡：由 PageHeader、MainTabNav 与 components.css 全局原子接管 */
.tab-body { display: flex; flex-direction: column; gap: 10px; }
/* UiBanner 紧凑变体 */
.slim { padding: 8px 12px; font-size: 12px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 8px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }
.control-detail { font-size: 12px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.annotate-toggle { min-width: 108px; }
.btn-toggle-primary, .btn-toggle-active { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-on-primary); }
.btn-toggle-primary:hover:not(:disabled), .btn-toggle-active:hover:not(:disabled) { background: var(--color-primary-hover); }
.btn-toggle-outline { background: var(--surface-panel); border-color: var(--color-primary); color: var(--color-primary); }
.btn-toggle-outline:hover:not(:disabled) { background: var(--surface-selected); }
.btn-toggle-warn { background: var(--state-warning-soft); border-color: var(--state-warning); color: var(--state-warning); }
.btn-toggle-danger { background: var(--surface-panel); border-color: var(--state-danger); color: var(--state-danger); }

/* ---------- 状态与说明 ---------- */
.status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }

.info-details { border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 6px; }
.info-body p { margin: 0; line-height: 1.6; }
.kbd-row { font-size: 12px; color: var(--color-text-muted); display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.kbd-chip { font-family: var(--font-mono); font-size: 11px; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 4px; padding: 2px 8px; color: var(--color-text); }

/* 通用按钮 .btn 家族已由 components.css 全局原子提供 */

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: var(--radius-control); border: 1px dashed var(--color-border); }
/* .empty-state 空态全局原子接管（components.css） */

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 12px; }
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
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-newest { background: var(--surface-selected); color: var(--color-primary); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }
/* .mono 基样式全局原子接管（components.css），此处仅留本视图紧凑变体 */
.mono { font-size: 11px; }
.mono.short { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格（.tbl 全局原子接管） ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: var(--state-positive); }
.ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ver-status.error::before { background: var(--state-danger); }
.ver-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
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
/* .link-button 全局原子接管 */
</style>