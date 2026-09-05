<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
// 结构对齐 CCSwitchView（纯托管双 Tab），差异在退出语义：上游"关闭窗口"行为用户可配，
// Quit 可能只换来"收入托盘/弹窗询问"——三态结果如实分叉，强杀走独立的「强制结束」。
import { ref, computed, onMounted } from 'vue'
import * as Bili23API from '../../bindings/hanxi/internal/modules/bili23/service'
import type { Bili23Release, Bili23VersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/bili23/version/models'
import type { ControlOutcome, QuitOutcome, Status } from '../../bindings/hanxi/internal/modules/bili23/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'

// ---------- 状态 ----------
const status = ref<Status | null>(null)
const releases = ref<Bili23Release[]>([])
const installed = ref<Bili23VersionInfo[]>([])
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

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/everything 同构）
const activeMainTab = ref<'console' | 'versions'>('console')
const mainTabs = [
  { key: 'console', label: '📺 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => status.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')
const windowVisible = computed(() => status.value?.windowVisible ?? false)

// 五态通用文案接 constants/status 单一来源；业务扩展话术视图自行覆写。
const stateText = computed(() => {
  if (state.value === 'running' && !windowVisible.value) return '运行中 · 已收入托盘'
  return toolStateMeta(state.value).text
})

const runningVersion = computed(() => status.value?.version ?? '')

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = state.value === 'running' && runningVersion.value ? runningVersion.value : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

// 条件提示条（互斥变体）；tone 对齐 UiBanner 语义
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部 Bili23 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 Bili23 窗口或其托盘操作。',
    } as const
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: status.value?.error || 'Bili23 异常退出' } as const
  }
  if (state.value === 'running' && !windowVisible.value) {
    return {
      tone: 'warn',
      text: 'Bili23 窗口已隐藏（收入其自身托盘），进程仍在运行、下载不中断。「打开窗口」可唤回。',
    } as const
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: 'Bili23 正在运行：解析与下载任务在其窗口内操作（配置与任务库存于 %APPDATA%\\Bili23 Downloader，跨版本共享）。',
    } as const
  }
  return null
})

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remote, local, active] = await Promise.all([
      Bili23API.ListReleases(),
      Bili23API.ListInstalledVersions(),
      Bili23API.GetActiveVersion(),
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
    status.value = await Bili23API.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('bili23 GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: Bili23Release): 'installed' | 'downloading' | 'error' | 'idle' {
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
    const out: ControlOutcome = await Bili23API.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitBili23() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await Bili23API.Quit()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`退出失败: ${getErrorMessage(e)}`)
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function forceStop() {
  // 危险操作经全局确认框：下载器强杀必须让用户知道代价（在途下载中断，可靠续传兜底）
  const accepted = await confirm({
    title: '强制结束 Bili23？',
    description: '立即终止进程，跳过优雅收尾：在途下载将中断（下次启动可断点续传），等待落盘的任务状态可能回退。\n建议优先使用「退出」或到 Bili23 窗口/托盘内正常退出。',
    tone: 'danger',
  })
  if (!accepted) return
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await Bili23API.ForceStop()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`强制结束失败: ${getErrorMessage(e)}`)
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

// ---------- 版本管理操作 ----------
async function download(rel: Bili23Release) {
  try {
    const res = await Bili23API.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: Bili23VersionInfo) {
  try {
    const ver = await Bili23API.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await Bili23API.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function openConfigDir() {
  try {
    await Bili23API.OpenConfigDir()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: Bili23VersionInfo) {
  const accepted = await confirm({
    title: `确定卸载 Bili23 ${v.version}？`,
    description: '该版本隔离目录（含内置 Python 运行时）将被删除，不可恢复。\n（你的下载任务与配置存于 %APPDATA%\\Bili23 Downloader，不受影响，后续版本继续共用）',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await Bili23API.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地 Bili23 Downloader',
    description: '提示：安装版（Program Files）或手动解压的便携版均可；整目录复制约 108MB，导入前请先退出正在运行的 Bili23',
    label: '安装目录完整路径（含 Bili23.exe 的目录，或其外层目录）',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await Bili23API.ImportLocal(path.trim())
    showToast(`已导入 Bili23 ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// 运行时长秒表（每秒从快照 startedAt 重算；KeepAlive 停用期间暂停）
function uptimeTick() {
  if (status.value?.state === 'running' && status.value.startedAt) {
    const started = new Date(status.value.startedAt).getTime()
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
    const [f, u] = await Promise.all([Bili23API.GetFollowOnExit(), Bili23API.RepositoryURL()])
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
    await Bili23API.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await Bili23API.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  const ok = await copy(repoUrl.value)
  showToast(ok ? '仓库地址已复制' : '复制失败')
}

async function openRepo() {
  try {
    await Bili23API.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 订阅与生命周期 ----------
useWailsEvent<DownloadProgress>('bili23:version-download', (t) => {
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

// 引擎快照事件不带窗口可见性（那是 service 层聚合视图的字段）：
// 收到迁移信号即回读一次 GetStatus，保持单一状态源。
useWailsEvent('bili23:instance-state', () => {
  refreshStatus()
})

// 轮询（usePolling 内置 KeepAlive 激活/停用/卸载契约；首跑即完成进页状态刷新）
usePolling(refreshStatus, 2500)
usePolling(uptimeTick, 1000)

onMounted(() => {
  loadVersions()
  loadExtras()
})
</script>

<template>
  <section class="page bili23-view">
    <PageHeader title="Bili23 下载" subtitle="托管开源 B 站视频下载器 Bili23 Downloader：版本管理、启停与窗口唤起。">
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
          <span class="b23-status-light" :class="[state, { tray: state === 'running' && !windowVisible }]"></span>
          <span class="status-word">{{ stateText }}</span>
          <template v-if="isRunningOrStarting && runningVersion">
            <span class="ver-pill">{{ runningVersion }}</span>
            <span v-if="status?.pid" class="mono pid-tag">PID {{ status.pid }}</span>
          </template>
          <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
        </div>
        <div class="control-btns">
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || state === 'starting'"
            :title="state === 'running' ? '唤起窗口（含从托盘唤回）' : state === 'starting' ? '启动中…' : '启动 Bili23 并打开窗口'"
            @click="openWindow"
          >🗔 打开窗口</button>
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || !isRunningOrStarting"
            title="请求优雅退出：结果取决于 Bili23 的「关闭窗口」设置（退出/托盘/询问），不会静默强杀"
            @click="quitBili23"
          >⏻ 退出</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="busy || (!isRunningOrStarting && !isExternal)"
            :title="isExternal ? '外部实例不在 Hanxi 管辖范围' : '立即终止进程（在途下载中断，可靠续传兜底）'"
            @click="forceStop"
          >⛔ 强制结束</button>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <UiBanner v-if="banner" :tone="banner.tone" class="slim">{{ banner.text }}</UiBanner>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未运行：点击「打开窗口」启动 Bili23，解析与下载任务在其窗口内完成。登录态、任务库与配置存于用户目录，与托管版本切换无关。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Bili23（Qt 首帧约 2~5 秒）…</div>

    <!-- 说明卡（可折叠） -->
    <details class="info-details">
      <summary class="info-summary">什么是 Bili23 Downloader</summary>
      <div class="info-body">
        <p>开源跨平台 B 站视频下载器（<a class="inline-link" href="https://github.com/ScottSloan/Bili23-Downloader" target="_blank" rel="noopener">ScottSloan/Bili23-Downloader</a>，GPL-3.0），支持投稿/番剧/课程/收藏夹批量解析、多线程下载、弹幕字幕、NFO 元数据与自定义命名规则。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
        <p class="hint-dim">便携包为"自带 Python 运行时 + 程序"整目录（展开约 108MB），无需任何本机依赖。退出语义说明：本应用的「退出」按你在 Bili23 设置中的"关闭窗口"行为执行（询问/最小化到托盘/直接退出），结果会如实提示；需要立即终结时用「强制结束」。</p>
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
        <div class="extras-btns">
          <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
          <button class="btn btn-secondary btn-small" title="打开 Bili23 的用户数据目录（配置 / 任务库 / 日志）" @click="openConfigDir">🗂 数据目录</button>
        </div>
      </div>
      <div class="repo-row">
        <span class="k">GitHub 仓库</span>
        <code class="mono repo-addr">{{ repoUrl }}</code>
        <button class="link-button" @click="copyRepo">复制</button>
        <button class="link-button" @click="openRepo">浏览器打开</button>
      </div>
      <div class="repo-row" v-if="openDirVersion">
        <span class="k">托管位置</span>
        <code class="mono repo-addr">{{ openDirVersion.dir }}</code>
        <button class="link-button" @click="openDir(openDirVersion.dir)">打开</button>
      </div>
    </div>
    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
    <div class="control-panel">
      <div class="meta-info">
        <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程版本 {{ releases.length }} 个</span>
        <span class="hint-dim">便携包下载自 GitHub Releases（windows_x64_portable.zip，官方 digest 校验）；或「导入本地安装」把你机器上已有的安装版/便携版整目录收纳进来</span>
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
      <p>尚未安装 Bili23 —— 下载官方便携版（约 43MB），或「导入本地安装」把现有 Bili23 收纳进来</p>
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
            <span v-if="activeVersion === v.version" class="badge badge-active">使用中</span>
            <span v-else-if="state === 'running' && runningVersion === v.version" class="badge badge-running">运行中</span>
            <span v-if="v.isImport" class="badge badge-import">本地导入</span>
            <span v-else class="badge badge-official">官方下载</span>
          </div>
        </div>
        <div class="inst-meta">
          <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.dir }}</code></div>
          <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
          <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
        </div>
        <div class="inst-actions">
          <button v-if="activeVersion !== v.version" class="btn btn-primary btn-small" @click="setActive(v)">设为使用</button>
          <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 打开位置</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="state === 'running' && runningVersion === v.version"
            :title="state === 'running' && runningVersion === v.version ? '请先退出 Bili23' : ''"
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
              <!-- 类名刻意用 b23-ver-status（含 b23- 前缀）——App.vue 全局样式有 .status-dot（7px），防碰撞压扁表格圆点 -->
              <span v-if="statusOf(rel) === 'installed'" class="b23-ver-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="b23-ver-status downloading">下载中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="b23-ver-status error">失败</span>
              <span v-else class="b23-ver-status idle">可安装</span>
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
              <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small">已安装</span>
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
.bili23-view { display: flex; flex-direction: column; gap: 10px; }
/* 页头/副标题/错误框/主选项卡：由 PageHeader、MainTabNav 与 components.css 全局原子接管 */
.tab-body { display: flex; flex-direction: column; gap: 10px; }
/* UiBanner 紧凑变体 */
.slim { padding: 8px 12px; font-size: 12px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 b23- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.b23-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.b23-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
/* 运行中但窗口收入托盘：绿灯降为琥珀警示，与文案"运行中 · 已收入托盘"呼应 */
.b23-status-light.running.tray { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.b23-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.b23-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.b23-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示与说明卡（banner-* 全局原子接管） ---------- */
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
/* .mono 基样式全局原子接管，此处仅留本视图紧凑字号 */
.mono { font-size: 11px; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格（.tbl 全局原子接管） ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.b23-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.b23-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.b23-ver-status.installed::before { background: var(--state-positive); }
.b23-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.b23-ver-status.error::before { background: var(--state-danger); }
.b23-ver-status.idle::before { background: var(--color-text-subtle); }

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
.extras-btns { display: flex; gap: 8px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
/* .link-button 全局原子接管 */
</style>
