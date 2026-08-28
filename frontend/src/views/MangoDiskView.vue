<script setup lang="ts">
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import * as MangoDiskAPI from '../../bindings/hubkit/internal/modules/mangodisk/mangodiskservice'
import type { Snapshot } from '../../bindings/hubkit/internal/modules/mangodisk/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hubkit/internal/modules/mangodisk/models'
import type { DownloadProgress, MangoDiskRelease, MangoDiskVersionInfo } from '../../bindings/hubkit/internal/modules/mangodisk/version/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const snap = ref<Snapshot | null>(null)
const releases = ref<MangoDiskRelease[]>([])
const installed = ref<MangoDiskVersionInfo[]>([])
const activeVersion = ref('')
const followOnExit = ref(true)
const activeMainTab = ref<'console' | 'versions'>('console')
const downloading = ref<Record<string, DownloadProgress>>({})
const loading = ref(false)
const busy = ref(false)
const listError = ref('')
const uptimeSec = ref(0)
const { showToast } = useToast()

let unlistenDownload: (() => void) | null = null
let unlistenState: (() => void) | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

const state = computed(() => snap.value?.state ?? 'stopped')
const isExternal = computed(() => state.value === 'external')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const stateText = computed(() => ({ running: '运行中', starting: '启动中…', external: '外部运行', failed: '异常退出' }[state.value] ?? '未运行'))
const currentVersion = computed(() => snap.value?.version || activeVersion.value || '自动选择')
const currentInstalled = computed(() => installed.value.find(item => item.version === currentVersion.value) ?? null)

const banner = computed(() => {
  if (currentInstalled.value?.integrity === 'drifted') return { cls: 'md-banner-warn', text: currentInstalled.value.integrityNote }
  if (currentInstalled.value?.integrity === 'invalid') return { cls: 'md-banner-error', text: currentInstalled.value.integrityNote }
  if (state.value === 'external') return { cls: 'md-banner-warn', text: '检测到安装版、portable 或其他版本的外部实例。HubKit 可唤起窗口，但不会强制终止它。' }
  if (state.value === 'failed') return { cls: 'md-banner-error', text: snap.value?.error || 'MangoDisk 异常退出' }
  if (state.value === 'running') return { cls: 'md-banner-ok', text: 'MangoDisk 正在运行；磁盘扫描、清理与系统设置均在原版窗口内完成。' }
  return null
})

function integrityLabel(value: string): string {
  return ({ verified: '官方校验', 'local-baseline': '本地基线', drifted: '文件已漂移', invalid: '安装无效' } as Record<string, string>)[value] || '未知'
}

function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  return bytes >= 1024 * 1024 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : `${Math.round(bytes / 1024)} KB`
}
function fmtDate(value?: string): string { return value ? value.slice(0, 10) : '—' }
function fmtDuration(sec: number): string {
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}
function shortHash(value?: string): string { return value ? `${value.slice(0, 10)}…${value.slice(-6)}` : '—' }
function progressOf(item: DownloadProgress): number {
  if (item.stage === 'done') return 100
  if (item.stage !== 'downloading' || !item.total) return 0
  return Math.min(99, Math.round(item.done / item.total * 100))
}

async function loadVersions() {
  loading.value = true
  listError.value = ''
  try {
    const [remote, local, active, follow] = await Promise.all([
      MangoDiskAPI.ListReleases(), MangoDiskAPI.ListInstalledVersions(), MangoDiskAPI.GetActiveVersion(), MangoDiskAPI.GetFollowOnExit(),
    ])
    releases.value = remote ?? []
    installed.value = local ?? []
    activeVersion.value = active ?? ''
    followOnExit.value = follow
  } catch (error) {
    listError.value = `获取版本列表失败：${getErrorMessage(error)}`
  } finally {
    loading.value = false
  }
}

async function refreshStatus() {
  try { snap.value = await MangoDiskAPI.GetStatus() } catch (error) { console.warn('mangodisk status:', getErrorMessage(error)) }
}

async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await MangoDiskAPI.OpenWindow()
    showToast(out.message)
    await Promise.all([refreshStatus(), loadVersions()])
  } catch (error) {
    showToast(getErrorMessage(error))
    await loadVersions()
  } finally { busy.value = false }
}

async function quit() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await MangoDiskAPI.Quit()
    showToast(out.message)
    await refreshStatus()
  } catch (error) { showToast(`退出失败：${getErrorMessage(error)}`) } finally { busy.value = false }
}

async function download(rel: MangoDiskRelease) {
  try {
    const result = await MangoDiskAPI.DownloadVersion(rel.version)
    if (result === 'already-installed') { showToast(`${rel.version} 已安装`); await loadVersions() }
  } catch (error) { showToast(`下载失败：${getErrorMessage(error)}`) }
}

async function setActive(item: MangoDiskVersionInfo) {
  try { activeVersion.value = await MangoDiskAPI.SetActiveVersion(item.version); showToast(`已将 ${item.version} 设为使用版本`) }
  catch (error) { showToast(`设置失败：${getErrorMessage(error)}`) }
}

async function removeVersion(item: MangoDiskVersionInfo) {
  if (!window.confirm(`确定卸载 MangoDisk ${item.version}？\n仅删除 HubKit 版本目录，不会删除 %LOCALAPPDATA%\\app.mangodisk.desktop 中的数据。`)) return
  try { await MangoDiskAPI.RemoveVersion(item.version); showToast(`已卸载 ${item.version}`); await loadVersions() }
  catch (error) { showToast(`卸载失败：${getErrorMessage(error)}`) }
}

async function importLocal() {
  const path = window.prompt('请输入本地 MangoDisk EXE 完整路径。HubKit 只导入该 EXE，不搬运用户数据。')
  if (!path) return
  try { const item = await MangoDiskAPI.ImportLocal(path.trim()); showToast(`已导入 ${item.version}`); await loadVersions() }
  catch (error) { showToast(`导入失败：${getErrorMessage(error)}`) }
}

async function toggleFollow() {
  const next = !followOnExit.value
  try { await MangoDiskAPI.SetFollowOnExit(next); followOnExit.value = next; showToast(next ? '已启用退出联动（下次启动生效）' : '已关闭退出联动（下次启动生效）') }
  catch (error) { showToast(`设置失败：${getErrorMessage(error)}`) }
}

async function createShortcut() {
  if (busy.value) return
  busy.value = true
  try {
    await MangoDiskAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (error) {
    showToast(`创建快捷方式失败：${getErrorMessage(error)}`)
  } finally {
    busy.value = false
  }
}

async function openDir(path: string) { try { await MangoDiskAPI.OpenDir(path) } catch (error) { showToast(getErrorMessage(error)) } }
async function openRepository() { try { await MangoDiskAPI.OpenRepository() } catch (error) { showToast(getErrorMessage(error)) } }

function startPolling() {
  if (!pollTimer) pollTimer = setInterval(refreshStatus, 2500)
  if (!tickTimer) tickTimer = setInterval(() => { if (state.value === 'running') uptimeSec.value++ }, 1000)
}
function stopPolling() {
  if (pollTimer) clearInterval(pollTimer)
  if (tickTimer) clearInterval(tickTimer)
  pollTimer = tickTimer = null
}

onMounted(async () => {
  unlistenDownload = Events.On('mangodisk:version-download', (event: { data?: DownloadProgress }) => {
    const p = event.data
    if (!p) return
    downloading.value = { ...downloading.value, [p.version]: p }
    if (p.stage === 'done') setTimeout(async () => { delete downloading.value[p.version]; await loadVersions() }, 800)
  })
  unlistenState = Events.On('mangodisk:instance-state', (event: { data?: Snapshot }) => { if (event.data) snap.value = event.data })
  await Promise.all([loadVersions(), refreshStatus()])
  startPolling()
})
onActivated(startPolling)
onDeactivated(stopPolling)
onUnmounted(() => { stopPolling(); unlistenDownload?.(); unlistenState?.() })
</script>

<template>
  <section class="page md-page">
    <header class="md-header">
      <div>
        <h1>MangoDisk</h1>
        <p>可信版本管理与原版 GUI 进程托管</p>
      </div>
      <div class="md-state-badge" :data-state="state"><span class="md-state-light"></span>{{ stateText }}</div>
    </header>

    <nav class="md-tabs" aria-label="MangoDisk 功能">
      <button :class="{ active: activeMainTab === 'console' }" @click="activeMainTab = 'console'">控制台</button>
      <button :class="{ active: activeMainTab === 'versions' }" @click="activeMainTab = 'versions'">版本管理 <span>{{ installed.length }}</span></button>
    </nav>

    <div v-show="activeMainTab === 'console'" class="md-stack">
      <div v-if="banner" class="md-banner" :class="banner.cls" role="status">{{ banner.text }}</div>
      <section class="md-panel md-control-panel">
        <div class="md-control-main">
          <div class="md-app-mark" aria-hidden="true">M</div>
          <div class="md-control-copy">
            <strong>{{ currentVersion }}</strong>
            <span>{{ isExternal ? '非 HubKit 托管' : isRunningOrStarting ? `PID ${snap?.pid || '—'} · ${fmtDuration(uptimeSec)}` : '等待启动' }}</span>
          </div>
        </div>
        <div class="md-actions">
          <button class="md-btn md-btn-primary" :disabled="busy" @click="openWindow">{{ state === 'running' || state === 'external' ? '打开窗口' : '启动 MangoDisk' }}</button>
          <button class="md-btn md-btn-danger" :disabled="busy || !isRunningOrStarting || isExternal" @click="quit">退出</button>
        </div>
      </section>

      <section class="md-panel md-settings">
        <div><strong>退出联动</strong><span>HubKit 退出时关闭自己托管的 MangoDisk；外部实例不受影响。</span></div>
        <div class="md-settings-actions">
          <button class="md-btn" :disabled="busy || !installed.length" @click="createShortcut">创建桌面快捷方式</button>
          <button class="md-switch" :aria-pressed="followOnExit" @click="toggleFollow"><span></span>{{ followOnExit ? '已开启' : '已关闭' }}</button>
        </div>
      </section>

      <section class="md-panel md-notes">
        <h2>托管边界与数据说明</h2>
        <div class="md-note-grid">
          <div><strong>原版能力</strong><span>磁盘扫描、清理、卸载和系统设置继续在 MangoDisk 原版窗口中操作。</span></div>
          <div><strong>共享数据</strong><span class="md-mono">%LOCALAPPDATA%\app.mangodisk.desktop</span><span>卸载 HubKit 托管版本不会删除该目录。</span></div>
          <div><strong>内置更新器</strong><span>MangoDisk 自带 updater。若它替换 EXE，HubKit 会标记“文件已漂移”并阻止静默启动。</span></div>
          <div><strong>运行环境</strong><span>Windows x64 · WebView2 Runtime · Tauri 单实例。</span></div>
        </div>
        <div class="md-license">第三方软件 · <button @click="openRepository">harry0703/MangoDisk</button> · GPL-3.0-only · 运行时从上游 GitHub Releases 下载</div>
      </section>
    </div>

    <div v-show="activeMainTab === 'versions'" class="md-stack">
      <section class="md-toolbar">
        <div><strong>版本仓库</strong><span>{{ installed.length }} 个已安装 · {{ releases.length }} 个远程版本</span></div>
        <div class="md-actions"><button class="md-btn" @click="importLocal">导入本地 EXE</button><button class="md-btn" :disabled="loading" @click="loadVersions">刷新</button></div>
      </section>

      <div v-if="listError" class="md-state-box md-error"><strong>版本列表加载失败</strong><span>{{ listError }}</span><button class="md-btn" @click="loadVersions">重试</button></div>
      <div v-else-if="loading && !installed.length" class="md-state-box"><strong>正在读取 MangoDisk 版本</strong><span>正在检查 GitHub Releases 与本地安装完整性。</span></div>

      <section v-else class="md-panel">
        <div class="md-section-head"><div><h2>已安装版本</h2><p>每次加载和冷启动前复核 EXE 哈希与 PE 身份。</p></div></div>
        <div v-if="!installed.length" class="md-state-box"><strong>尚未安装 MangoDisk</strong><span>从下方远程列表下载官方 portable EXE，或导入本地 MangoDisk EXE。</span></div>
        <div v-else class="md-version-list">
          <article v-for="item in installed" :key="item.version" class="md-version-row">
            <div class="md-version-main">
              <div class="md-version-title"><strong>{{ item.version }}</strong><span v-if="activeVersion === item.version" class="md-pill md-pill-primary">使用中</span><span class="md-pill" :data-integrity="item.integrity">{{ integrityLabel(item.integrity) }}</span></div>
              <p>{{ item.integrityNote }}</p>
              <div class="md-meta"><span>{{ fmtSize(item.size) }}</span><span>{{ fmtDate(item.installedAt) }}</span><span>{{ item.isImport ? '本地导入' : '官方下载' }}</span><span class="md-mono">SHA {{ shortHash(item.currentSha256) }}</span></div>
              <div v-if="item.integrity === 'drifted' || item.integrity === 'invalid'" class="md-integrity-detail"><span>标称 {{ item.version }}</span><span>当前 FileVersion {{ item.fileVersion || '未知' }}</span></div>
            </div>
            <div class="md-row-actions"><button class="md-btn" :disabled="activeVersion === item.version || item.integrity === 'invalid'" @click="setActive(item)">设为使用</button><button class="md-btn" @click="openDir(item.dir)">打开位置</button><button class="md-btn md-btn-danger" :disabled="snap?.version === item.version && isRunningOrStarting" @click="removeVersion(item)">卸载</button></div>
          </article>
        </div>
      </section>

      <section class="md-panel">
        <div class="md-section-head"><div><h2>远程版本</h2><p>只接收带 GitHub 官方 SHA-256 digest 的 Windows x64 portable EXE。</p></div></div>
        <div v-if="!releases.length" class="md-state-box"><strong>暂无远程版本</strong><span>请检查网络后刷新；当前本地版本仍可继续使用。</span></div>
        <div v-else class="md-table-wrap">
          <table class="md-table">
            <thead><tr><th>版本</th><th>发布时间</th><th>大小</th><th>完整性</th><th>操作</th></tr></thead>
            <tbody><tr v-for="rel in releases" :key="rel.version"><td class="md-mono"><strong>{{ rel.version }}</strong><span v-if="rel.isPre" class="md-pill">预发布</span></td><td>{{ fmtDate(rel.published) }}</td><td>{{ fmtSize(rel.size) }}</td><td><span class="md-pill md-pill-ok">GitHub SHA-256</span></td><td><template v-if="downloading[rel.version]"><div class="md-progress" :aria-label="`${rel.version} 下载进度`" role="progressbar" :aria-valuenow="progressOf(downloading[rel.version])" aria-valuemin="0" aria-valuemax="100"><span :style="{ width: `${progressOf(downloading[rel.version])}%` }"></span></div><small>{{ downloading[rel.version].stage }} {{ progressOf(downloading[rel.version]) || '' }}</small></template><span v-else-if="installed.some(item => item.version === rel.version)" class="md-installed">已安装</span><button v-else class="md-btn md-btn-primary md-btn-small" @click="download(rel)">下载</button></td></tr></tbody>
          </table>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.md-page { max-width: 1120px; margin: 0 auto; display: flex; flex-direction: column; gap: 14px; color: var(--text-main); }
.md-header, .md-toolbar, .md-control-panel, .md-settings, .md-version-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.md-header h1 { margin: 0; font-size: 21px; letter-spacing: -.02em; } .md-header p, .md-toolbar span, .md-settings span { margin: 4px 0 0; color: var(--text-muted); font-size: 12px; }
.md-state-badge, .md-pill { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--border-color); border-radius: 999px; padding: 5px 10px; background: var(--bg-sidebar); font-size: 12px; white-space: nowrap; }
.md-state-light { width: 8px; height: 8px; border-radius: 50%; background: var(--text-subtle); }.md-state-badge[data-state="running"] .md-state-light { background: var(--success); }.md-state-badge[data-state="starting"] .md-state-light { background: var(--accent); }.md-state-badge[data-state="external"] .md-state-light { background: #b7791f; }.md-state-badge[data-state="failed"] .md-state-light { background: var(--danger); }
.md-tabs { display: flex; gap: 4px; padding: 4px; width: fit-content; border: 1px solid var(--border-color); border-radius: 9px; background: var(--bg-sidebar); }.md-tabs button { border: 0; background: transparent; color: var(--text-muted); padding: 7px 14px; border-radius: 6px; cursor: pointer; }.md-tabs button.active { background: var(--bg-app); color: var(--text-main); box-shadow: 0 1px 3px rgba(0,0,0,.08); }.md-tabs span { margin-left: 4px; font-size: 11px; }
.md-stack { display: flex; flex-direction: column; gap: 12px; }.md-panel, .md-toolbar { border: 1px solid var(--border-color); border-radius: 12px; background: var(--bg-sidebar); padding: 16px; }.md-control-panel { min-height: 88px; }.md-control-main { display: flex; align-items: center; gap: 12px; min-width: 0; }.md-app-mark { display: grid; place-items: center; width: 42px; height: 42px; border-radius: 11px; background: color-mix(in srgb, var(--accent) 12%, transparent); color: var(--accent); font-size: 19px; font-weight: 700; }.md-control-copy { min-width: 0; display: flex; flex-direction: column; gap: 4px; }.md-control-copy strong, .md-mono { font-family: Consolas, monospace; }.md-control-copy span { color: var(--text-muted); font-size: 12px; }
.md-actions, .md-row-actions { display: flex; gap: 8px; align-items: center; }.md-btn { min-height: 36px; padding: 0 12px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-app); color: var(--text-main); cursor: pointer; transition: border-color .15s, background .15s; }.md-btn:hover:not(:disabled) { border-color: var(--accent); background: var(--bg-hover); }.md-btn:focus-visible, .md-tabs button:focus-visible, .md-switch:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }.md-btn:disabled { opacity: .48; cursor: not-allowed; }.md-btn-primary { border-color: var(--accent); background: var(--accent); color: white; }.md-btn-primary:hover:not(:disabled) { background: var(--accent-hover); }.md-btn-danger { color: var(--danger); border-color: color-mix(in srgb, var(--danger) 45%, var(--border-color)); }.md-btn-small { min-height: 30px; font-size: 12px; }
.md-banner { border: 1px solid var(--border-color); border-radius: 9px; padding: 10px 12px; font-size: 12px; }.md-banner-ok { color: var(--success); background: color-mix(in srgb, var(--success) 8%, var(--bg-sidebar)); }.md-banner-warn { color: #a3630a; background: color-mix(in srgb, #d18a20 9%, var(--bg-sidebar)); }.md-banner-error { color: var(--danger); background: color-mix(in srgb, var(--danger) 8%, var(--bg-sidebar)); }
.md-settings > div:first-child { display: flex; flex-direction: column; gap: 2px; }.md-settings-actions { display: flex; align-items: center; gap: 10px; }.md-switch { display: inline-flex; align-items: center; gap: 7px; border: 0; background: transparent; color: var(--text-muted); cursor: pointer; }.md-switch span { width: 32px; height: 18px; border-radius: 999px; background: var(--border-color); position: relative; }.md-switch span::after { content: ''; position: absolute; top: 3px; left: 3px; width: 12px; height: 12px; border-radius: 50%; background: white; transition: transform .15s; }.md-switch[aria-pressed="true"] span { background: var(--accent); }.md-switch[aria-pressed="true"] span::after { transform: translateX(14px); }
.md-notes h2, .md-section-head h2 { margin: 0; font-size: 15px; }.md-note-grid { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 10px; margin-top: 12px; }.md-note-grid > div { display: flex; flex-direction: column; gap: 5px; padding: 11px; border: 1px solid var(--border-color); border-radius: 9px; background: var(--bg-app); }.md-note-grid span { color: var(--text-muted); font-size: 12px; overflow-wrap: anywhere; }.md-license { margin-top: 12px; color: var(--text-subtle); font-size: 11px; }.md-license button { border: 0; background: none; padding: 0; color: var(--accent); cursor: pointer; }
.md-section-head { display: flex; justify-content: space-between; margin-bottom: 12px; }.md-section-head p { color: var(--text-muted); font-size: 12px; margin: 4px 0 0; }.md-version-list { display: flex; flex-direction: column; gap: 8px; }.md-version-row { padding: 12px; border: 1px solid var(--border-color); border-radius: 10px; background: var(--bg-app); }.md-version-main { min-width: 0; }.md-version-title { display: flex; gap: 7px; align-items: center; flex-wrap: wrap; }.md-version-main p { margin: 5px 0; color: var(--text-muted); font-size: 12px; }.md-meta, .md-integrity-detail { display: flex; flex-wrap: wrap; gap: 10px; color: var(--text-subtle); font-size: 11px; }.md-integrity-detail { margin-top: 6px; color: var(--danger); }.md-pill { padding: 2px 7px; font-size: 10px; }.md-pill-primary { color: var(--accent); border-color: color-mix(in srgb, var(--accent) 35%, var(--border-color)); }.md-pill-ok, .md-pill[data-integrity="verified"] { color: var(--success); }.md-pill[data-integrity="drifted"] { color: #a3630a; }.md-pill[data-integrity="invalid"] { color: var(--danger); }
.md-state-box { display: flex; flex-direction: column; align-items: flex-start; gap: 7px; padding: 18px; border: 1px dashed var(--border-color); border-radius: 10px; background: var(--bg-app); color: var(--text-muted); font-size: 12px; }.md-state-box strong { color: var(--text-main); }.md-error strong { color: var(--danger); }
.md-table-wrap { overflow-x: auto; }.md-table { width: 100%; min-width: 680px; border-collapse: collapse; font-size: 12px; }.md-table th, .md-table td { padding: 10px; text-align: left; border-bottom: 1px solid var(--border-color); }.md-table th { color: var(--text-subtle); font-weight: 600; }.md-installed { color: var(--success); }.md-progress { width: 110px; height: 5px; border-radius: 999px; background: var(--border-color); overflow: hidden; }.md-progress span { display: block; height: 100%; background: var(--accent); transition: width .1s linear; }.md-table small { color: var(--text-subtle); }
@media (max-width: 760px) { .md-header, .md-toolbar, .md-control-panel, .md-settings, .md-version-row { align-items: flex-start; flex-direction: column; }.md-actions, .md-row-actions, .md-settings-actions { width: 100%; flex-wrap: wrap; }.md-actions .md-btn, .md-row-actions .md-btn, .md-settings-actions .md-btn { flex: 1 1 120px; }.md-note-grid { grid-template-columns: 1fr; } }
@media (pointer: coarse) { .md-btn, .md-tabs button { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; } }
</style>
