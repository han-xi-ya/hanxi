<script setup lang="ts">
// 状态 / 版本管理 / 安装进度 / 时长 ticker / 生命周期
// 与 CCSwitchView 的结构差异：单版本托管目录（NSIS oneClick 语义，无"设为使用"）、
// stable/beta 双通道切换、安装器未签名风险文案。
import { ref, computed, watch, onMounted } from 'vue'
import * as RecordlyAPI from '../../bindings/hanxi/internal/modules/recordly/recordlyservice'
import type { RecordlyRelease, RecordlyVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/recordly/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/recordly/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/recordly/models'
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
const releases = ref<RecordlyRelease[]>([])
const installed = ref<RecordlyVersionInfo[]>([])
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)
const channel = ref<'stable' | 'beta'>('stable')

// 安装进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron/everything/ccswitch 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🎬 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 注意：习惯文案"运行中"与 constants/status 通用"已启动"不一致，迁移保持逐字现状。
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
const installedInfo = computed(() => installed.value[0] ?? null)

// 核心版本号（去预发布后缀）：NSIS 单目录语义下 beta 的 PE 版本与 tag 互认依据
function coreOf(v: string): string {
  return v.replace(/-.*$/, '')
}

// coreCompare：a>b 返回 1；不可解析退化为字典序比较
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

// 可升级：已装核心 < 当前通道最新核心
const upgradeAvailable = computed(() => {
  if (!installedInfo.value || releases.value.length === 0) return false
  return coreCompare(installedInfo.value.version, releases.value[0].version) < 0
})

// 条件提示条（变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn' as const,
      text: '检测到外部 Recordly 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 Recordly 窗口内关闭。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error' as const, text: snap.value?.error || 'Recordly 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok' as const,
      text: 'Recordly 正在运行：录制与剪辑在其窗口内操作（配置与录像存于 %APPDATA%\\Recordly，跨版本共享）。闲置 5 分钟自动退出。',
    }
  }
  return null
})

// ---------- 数据加载 ----------
async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remote, local, ch] = await Promise.all([
      RecordlyAPI.ListReleases(),
      RecordlyAPI.ListInstalledVersions(),
      RecordlyAPI.GetReleaseChannel(),
    ])
    releases.value = remote ?? []
    installed.value = local ?? []
    channel.value = ch === 'beta' ? 'beta' : 'stable'
  } catch (e) {
    listError.value = `获取版本列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function switchChannel(target: 'stable' | 'beta') {
  if (target === channel.value) return
  try {
    await RecordlyAPI.SetReleaseChannel(target)
    channel.value = target
    await loadVersions()
  } catch (e) {
    showToast(`切换通道失败: ${getErrorMessage(e)}`)
  }
}

async function refreshStatus() {
  try {
    snap.value = await RecordlyAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('recordly GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

// 安装状态判定：精确 tag 命中，或数值核心一致（PE 版本抹掉 -beta 后缀的互认场）
function statusOf(rel: RecordlyRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version || coreCompare(v.version, rel.version) === 0)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await RecordlyAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitRecordly() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await RecordlyAPI.Quit()
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
async function install(rel: RecordlyRelease) {
  try {
    const res = await RecordlyAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`安装失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await RecordlyAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: RecordlyVersionInfo) {
  const ok = await confirm({
    title: `确定卸载 Recordly ${v.version}？`,
    description: '托管安装目录将被删除。\n（%APPDATA%\\Recordly 中的配置与录像不受影响）',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await RecordlyAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地安装',
    label: '请输入 Recordly 安装目录完整路径（含 Recordly.exe 与 resources\\app.asar 的整套目录）',
    description: '默认安装位置：%LOCALAPPDATA%\\Programs\\Recordly\n提示：配置与录像恒在 %APPDATA%\\Recordly，与安装位置无关',
    placeholder: '%LOCALAPPDATA%\\Programs\\Recordly',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await RecordlyAPI.ImportLocal(path.trim())
    showToast(`已导入 Recordly ${info.version}`)
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
    const [f, u] = await Promise.all([RecordlyAPI.GetFollowOnExit(), RecordlyAPI.RepositoryURL()])
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
    await RecordlyAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await RecordlyAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向托管安装）')
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
    await RecordlyAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件订阅（useWailsEvent 自动注销）与初始装载 ----------
useWailsEvent<DownloadProgress>('recordly:version-download', (t) => {
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

useWailsEvent<Snapshot>('recordly:instance-state', (s) => {
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
  <section class="page recordly-view">
    <PageHeader title="Recordly" subtitle="托管开源录屏工具 Recordly：版本管理、静默安装、启停与窗口唤起。">
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
          <span class="rd-status-light" :class="state"></span>
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
            :title="state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 Recordly 并打开窗口'"
            @click="openWindow"
          >🗔 打开窗口</UiButton>
          <UiButton
            variant="danger"
            small
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 Recordly 窗口内退出' : '关闭其窗口（多窗口收尾不及会兜底强杀）'"
            @click="quitRecordly"
          >⏻ 退出</UiButton>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
    <div v-else-if="state === 'stopped' && installedInfo" class="hint-line">
      尚未运行：点击「打开窗口」启动 Recordly {{ installedInfo.version }}，录制与剪辑在其窗口内完成。配置恒存于 %APPDATA%\Recordly，与托管版本无关。
    </div>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未安装：请到「版本管理」在线安装或导入本地副本。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Recordly（Electron 冷启动约 2~10 秒）…</div>

    <UiBanner v-if="upgradeAvailable && installedInfo" tone="warn">
      发现可升级版本 {{ releases[0].version }}（当前 {{ installedInfo.version }}）——到「版本管理」一键安装，安装期间请先退出运行中的实例。
    </UiBanner>

    <!-- 说明卡（可折叠） -->
    <details class="info-details">
      <summary class="info-summary">什么是 Recordly</summary>
      <div class="info-body">
        <p>开源演示录屏与自动剪辑工具（<a class="inline-link" href="https://github.com/webadderallorg/Recordly" target="_blank" rel="noopener">webadderallorg/Recordly</a>，AGPL-3.0）。Hanxi 仅做官方原版安装器的下载托管与启停管理，不内嵌不打包其代码。</p>
        <p class="hint-dim">上游无 Windows 免安装包：在线安装使用官方 NSIS 安装器静默装进 Hanxi 托管目录，安装器自动更新已由官方开关禁用，版本升级统一走此处版本管理。安装器未数字签名，托管直装不触发 SmartScreen；若杀毒软件误拦请把托管目录加入白名单。</p>
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
        <span>
          当前托管 <strong>{{ installedInfo ? installedInfo.version : '未安装' }}</strong> · {{ channel === 'beta' ? 'beta 通道（含预发布）' : 'stable 通道' }} · 远程版本 {{ releases.length }} 个
        </span>
        <span class="hint-dim">Windows 仅提供 NSIS 在线安装器（GitHub digest + SHA256SUMS 双源校验），安装/升级统一静默落进托管目录，多版本不共存为上游安装器语义所限</span>
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
      <span v-if="channel === 'beta'" class="beta-warn">beta 版上游标注"可能不稳定"，仅供尝鲜</span>
    </div>

    <!-- 已安装 -->
    <div class="section-title"><h3>托管安装</h3></div>

    <UiEmptyState v-if="!installedInfo" class="first-use">
      <p>尚未安装 Recordly —— 在线安装官方版本，或「导入本地安装」把机器上已有的安装目录收编进来</p>
      <UiButton v-if="releases.length" variant="primary" @click="install(releases[0])">
        安装最新版 {{ releases[0].version }}（约 {{ fmtSize(releases[0].size) }}）
      </UiButton>
      <UiButton v-else-if="!loading" variant="secondary" @click="loadVersions">↻ 刷新远程列表</UiButton>
    </UiEmptyState>

    <div v-else class="installed-grid">
      <div class="installed-card card-active">
        <div class="inst-card-top">
          <span class="ver-tag">{{ installedInfo.version }}</span>
          <div class="inst-badges">
            <span v-if="state === 'running' && coreCompare(runningVersion, installedInfo.version) === 0" class="badge badge-running">运行中</span>
            <span v-if="installedInfo.isImport" class="badge badge-import">本地导入</span>
            <span v-else class="badge badge-official">官方下载</span>
          </div>
        </div>
        <div class="inst-meta">
          <div class="meta-line"><span class="k">路径</span><code class="mono">{{ installedInfo.exePath }}</code></div>
          <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(installedInfo.size) }} · 安装于 {{ installedInfo.installedAt }}</span></div>
          <div class="meta-line" v-if="installedInfo.isImport && installedInfo.source"><span class="k">来源</span><span class="hint-dim">{{ installedInfo.source }}</span></div>
        </div>
        <div class="inst-actions">
          <UiButton variant="secondary" small @click="openDir(installedInfo.dir)">📂 打开位置</UiButton>
          <UiButton
            variant="danger"
            small
            :disabled="state === 'running'"
            :title="state === 'running' ? '请先退出 Recordly' : ''"
            @click="removeVersion(installedInfo)"
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
              <!-- 类名刻意用 rd-ver-status（含 rd- 前缀）与全局原子族隔离 -->
              <span v-if="statusOf(rel) === 'installed'" class="rd-ver-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="rd-ver-status downloading">安装中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="rd-ver-status error">失败</span>
              <span v-else class="rd-ver-status idle">可安装</span>
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
                <span v-if="['verify', 'install'].includes(downloading[rel.version]!.stage)">校验并静默安装…</span>
                <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <UiButton
                v-if="statusOf(rel) === 'idle'"
                variant="primary"
                small
                :disabled="isRunningOrStarting"
                :title="isRunningOrStarting ? '请先退出运行中的 Recordly' : ''"
                @click="install(rel)"
              >{{ installedInfo ? '覆盖安装' : '安装' }}</UiButton>
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
.recordly-view { display: flex; flex-direction: column; gap: 10px; }

/* UiBanner 沿用迁移前 slim 密度 */
.tab-body .banner { padding: 8px 12px; font-size: 12px; }

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

/* ---------- 托管安装卡片 ---------- */
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

.rd-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.rd-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.rd-ver-status.installed::before { background: var(--state-positive); }
.rd-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.rd-ver-status.error::before { background: var(--state-danger); }
.rd-ver-status.idle::before { background: var(--color-text-subtle); }

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
