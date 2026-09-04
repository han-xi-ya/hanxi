<script setup lang="ts">
// 果核看图托管工作台：控制台（唤窗/退出）+ 版本管理（官方发布接口 MD5 校验安装、
// 导入本地便携目录、卸载）。多实例上游："打开窗口"= 聚焦/唤回/另开三分支。
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated } from 'vue'
import { Events } from '@wailsio/runtime'
import * as GuoheViewAPI from '../../bindings/hanxi/internal/modules/guoheview/guoheviewservice'
import type { ViewRelease, ViewVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/guoheview/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/guoheview/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/guoheview/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<ViewRelease[]>([])
const installed = ref<ViewVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/piclite 同构）
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

// 条件提示条（三个变体互斥；文案承载多实例与更新器两条真实契约）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      cls: 'banner-warn',
      text: '检测到你在 Hanxi 之外打开的看图窗口（双击图片等）。「打开窗口」会唤回它或另开独立新窗口；这类窗口不受 Hanxi 管退。',
    }
  }
  if (state.value === 'failed') {
    return { cls: 'banner-error', text: snap.value?.error || '果核看图异常退出' }
  }
  if (state.value === 'running') {
    return {
      cls: 'banner-ok',
      text: '托管实例正在运行。浏览、缩放与色彩管理在 GuoheView 自有窗口完成（关窗即退出）；上游自带更新检查，版本升级请回这里走托管安装，避免内置更新覆写托管目录。',
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
      GuoheViewAPI.ListReleases(),
      GuoheViewAPI.ListInstalledVersions(),
      GuoheViewAPI.GetActiveVersion(),
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
    snap.value = await GuoheViewAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('guoheview GetStatus failed:', getErrorMessage(e))
  }
}

// ---------- 格式化 ----------
function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(0)} KB`
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

function statusOf(rel: ViewRelease): 'installed' | 'downloading' | 'error' | 'idle' {
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
    const out: ControlOutcome = await GuoheViewAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitView() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await GuoheViewAPI.Quit()
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
async function download(rel: ViewRelease) {
  try {
    const res = await GuoheViewAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`安装失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: ViewVersionInfo) {
  try {
    const ver = await GuoheViewAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await GuoheViewAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: ViewVersionInfo) {
  if (!window.confirm(`确定卸载果核看图 ${v.version}？\n该版本托管目录（含其 config.ini 设置）将被删除，不可恢复。`)) return
  try {
    await GuoheViewAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = window.prompt('请输入本机果核看图便携目录完整路径（整个解压目录，含 GuoheView.exe）\n设置（config.ini）随目录一并收纳进托管；安装版目录也可导入，将自动转为便携模式')
  if (!path) return
  try {
    busy.value = true
    const info = await GuoheViewAPI.ImportLocal(path.trim())
    showToast(`已导入果核看图 ${info.version}`)
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

// ---------- 联动开关与官网入口 ----------
const followOnExit = ref(true)
const siteUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([GuoheViewAPI.GetFollowOnExit(), GuoheViewAPI.RepositoryURL()])
    followOnExit.value = f
    siteUrl.value = u
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  try {
    await GuoheViewAPI.SetFollowOnExit(!followOnExit.value)
    showToast(followOnExit.value ? '已开启：Hanxi 退出时一并关闭托管实例' : '已关闭：Hanxi 退出不影响该工具，托管实例继续独立运行（下次启动生效）')
  } catch (e) {
    showToast('设置失败: ' + getErrorMessage(e))
    followOnExit.value = !followOnExit.value // 失败回滚
  }
}

async function copySite() {
  const text = siteUrl.value
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
  showToast('官网地址已复制')
}

async function openSite() {
  try {
    await GuoheViewAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 生命周期 ----------
onMounted(async () => {
  unlistenDownload = Events.On('guoheview:version-download', (event) => {
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

  unlistenState = Events.On('guoheview:instance-state', (event) => {
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
  <section class="page guoheview-view">
    <div class="header-row">
      <div>
        <h1>果核看图</h1>
        <p class="subtitle">托管极速 RAW 看图器 GuoheView：官方发布接口便携版安装（MD5 校验）、JobObject 启停与窗口唤起；浏览操作在 GuoheView 自有窗口完成（多实例：双击图片的窗口不受影响）。</p>
      </div>
      <div class="main-tab-nav">
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'console' }"
          @click="activeMainTab = 'console'"
        >
          🏞️ 控制台
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
            <span class="gv-status-light" :class="state"></span>
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
              :title="state === 'running' ? '唤起托管实例窗口' : isExternal ? '唤回已有窗口，无窗可聚焦时另开独立新窗' : state === 'starting' ? '启动中…' : '启动托管实例并打开浏览窗口'"
              @click="openWindow"
            >🗔 打开窗口</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部窗口请在其窗口内关闭（Hanxi 不代关）' : '优雅退出：向托管窗口投递关窗消息，设置随窗口落盘'"
              @click="quitView"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <!-- 条件提示条 / 引导行 -->
      <div v-if="banner" class="hint-banner slim" :class="banner.cls">{{ banner.text }}</div>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「打开窗口」启动托管实例，浏览 RAW/HEIC/WebP 等格式在它自己的窗口内完成（关窗即退）。便携托管实例的设置保存在版本目录内的 config.ini。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起果核看图（极速内核，通常 1 秒内）…</div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="summary-text">什么是果核看图</summary>
        <div class="info-body">
          <p>果核（ghxi.com）出品的 Windows 极速 RAW 图片查看器（闭源免费软件，Certum 数字签名）：首帧 0.1 秒、自研解码内核覆盖 ARW/CR3/NEF/DNG/HEIC/WebP/TIFF 等格式，分块加载大图、ICC 色彩管理，纯净无广告。</p>
          <p class="hint-dim">上游闭源、发布挂官方自建接口（非 GitHub）：每次仅提供当前版本（stable/beta），无历史归档，旧版本可用「导入本地」收纳；完整性以官方 MD5 + 字节数 + zip CRC + 布局自检四层兜底。应用自带更新检查，升级建议一律回这里走托管安装。</p>
        </div>
      </details>
    </div>

    <!-- 联动与官网设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出完全不影响托管实例；你自行打开的看图窗口从不受影响）</span></span>
        </label>
      </div>
      <div class="repo-row">
        <span class="k">官网</span>
        <code class="mono repo-addr">{{ siteUrl }}</code>
        <button class="link-btn" @click="copySite">复制</button>
        <button class="link-btn" @click="openSite">浏览器打开</button>
      </div>
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 官方当前发布 {{ releases.length }} 个</span>
          <span class="hint-dim">安装源为果核官方发布接口的 Windows x64 便携 zip（官方 MD5 + 字节数 + CRC + 布局四层校验），解压进隔离目录，不触碰系统</span>
          <span class="hint-dim">上游只发布当前版本、无历史列表；「导入本地」可把你机器上已有的便携目录（含设置）收纳进托管</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地目录</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新发布接口' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

      <div v-if="installed.length === 0" class="empty-state first-use">
        <p>尚未安装果核看图 —— 下载官方当前版本，或「导入本地目录」把现有便携目录收纳进来</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
          安装最新版 {{ releases[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新发布接口</button>
      </div>

      <div class="installed-grid">
        <div v-for="v in installed" :key="v.version" class="installed-card" :class="{ 'card-active': activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <span v-if="activeVersion === v.version" class="badge badge-active">使用中</span>
              <span v-else-if="state === 'running' && runningVersion === v.version" class="badge badge-running">运行中</span>
              <span v-if="v.isImport" class="badge badge-import">本地导入</span>
              <span v-else class="badge badge-official">官方便携</span>
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
              :title="state === 'running' && runningVersion === v.version ? '请先退出托管实例' : ''"
              @click="removeVersion(v)"
            >卸载</button>
          </div>
        </div>
      </div>

      <!-- 官方当前发布版本 -->
      <div class="section-title"><h3>官方当前发布</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 150px;">版本</th>
              <th style="width: 80px;">通道</th>
              <th style="width: 170px;">状态</th>
              <th style="width: 90px;">大小</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rel in releases" :key="rel.version">
              <td><strong class="ver-name">{{ rel.version }}</strong></td>
              <td>
                <span v-if="rel.isPre" class="badge badge-pre">beta</span>
                <span v-else class="badge badge-stable">stable</span>
              </td>
              <td>
                <!-- 类名刻意用 gv- 前缀——App.vue 全局样式有 .status-dot（7px），防碰撞压扁表格圆点（markeron 垂直字体事故教训） -->
                <span v-if="statusOf(rel) === 'installed'" class="gv-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="gv-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="gv-ver-status error">失败</span>
                <span v-else class="gv-ver-status idle">可安装</span>
              </td>
              <td>{{ fmtSize(rel.size) }}</td>
              <td>
                <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="download-cell">
                  <div class="dl-bar-wrap">
                    <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
                  </div>
                  <span class="dl-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
                </div>
                <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
                  <span v-if="downloading[rel.version]!.stage === 'verify'">MD5 校验…</span>
                  <span v-else-if="downloading[rel.version]!.stage === 'extract'">解压安装…</span>
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
              <td colspan="5" class="empty-hint">官方发布接口暂不可达——可稍后点击「↻ 刷新发布接口」重试，或「导入本地目录」安装你已有的便携版</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.guoheview-view { display: flex; flex-direction: column; gap: 10px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 顶层主选项卡（与 CCSwitchView/PicLiteView 同款） */
.main-tab-nav { display: flex; background: var(--bg-hover); padding: 3px; border-radius: 8px; gap: 2px; }
.main-tab-btn { background: transparent; border: none; padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; color: var(--text-muted); cursor: pointer; transition: all 0.15s ease; }
.main-tab-btn:hover { color: var(--text-main); }
.main-tab-btn.active { background: var(--bg-app); color: var(--accent); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08); }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 10px; }
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 gv- 前缀，与全局样式隔离（markeron 垂直字体事故教训） */
.gv-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--text-subtle); flex-shrink: 0; }
.gv-status-light.running { background: var(--success); box-shadow: 0 0 0 3px rgba(26, 127, 55, 0.15); }
.gv-status-light.starting { background: var(--accent); animation: pulse 1s infinite; }
.gv-status-light.external { background: #9a6700; box-shadow: 0 0 0 3px rgba(154, 103, 0, 0.15); }
.gv-status-light.failed { background: var(--danger); box-shadow: 0 0 0 3px rgba(207, 34, 46, 0.15); }
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
.hint-line { font-size: 12px; color: var(--text-subtle); padding-left: 2px; line-height: 1.6; }
.info-details { border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-sidebar); overflow: hidden; }
.summary-text { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-details summary::-webkit-details-marker { display: none; }
.summary-text::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform 0.15s; }
.info-details[open] .summary-text { border-bottom: 1px solid var(--border-color); }
.info-details[open] .summary-text::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }

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

.control-panel { display: flex; align-items: center; justify-content: space-between; background: var(--bg-sidebar); border: 1px solid var(--border-color); padding: 10px 14px; border-radius: 8px; gap: 10px; flex-wrap: wrap; }
.meta-info { font-size: 13px; color: var(--text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--text-main); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--text-subtle); font-size: 13px; background: var(--bg-sidebar); border-radius: 6px; border: 1px dashed var(--border-color); }

/* 首次使用空态 */
.empty-state { text-align: center; padding: 24px; background: var(--bg-sidebar); border: 1px dashed var(--border-color); border-radius: 8px; display: flex; flex-direction: column; gap: 12px; align-items: center; }
.empty-state p { margin: 0; color: var(--text-muted); font-size: 13px; }

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color 0.15s ease; }
.installed-card.card-active { border-color: var(--accent); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: Consolas, monospace; font-size: 14px; font-weight: 700; color: var(--text-main); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
.badge-active { background: #dafbe1; color: #1a7f37; }
.badge-running { background: #ddf4ff; color: #0969da; }
.badge-import { background: #ddf4ff; color: #0969da; }
.badge-official { background: #eaeef2; color: #656d76; }
.badge-pre { background: #fff8c5; color: #9a6700; }
.badge-stable { background: #eaeef2; color: #656d76; }

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

.gv-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.gv-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.gv-ver-status.installed::before { background: #2da44e; }
.gv-ver-status.downloading::before { background: #0969da; animation: pulse 1s infinite; }
.gv-ver-status.error::before { background: #cf222e; }
.gv-ver-status.idle::before { background: #8c959f; }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: #e1e4e8; border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--accent); }
.dl-error { color: var(--danger); font-size: 11px; }
.retry-link { color: var(--accent); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 联动与官网设置卡 ---------- */
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
