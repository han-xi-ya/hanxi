<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import * as MangoDiskAPI from '../../bindings/hanxi/internal/modules/mangodisk/mangodiskservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/mangodisk/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/mangodisk/models'
import type { DownloadProgress, MangoDiskRelease, MangoDiskVersionInfo } from '../../bindings/hanxi/internal/modules/mangodisk/version/models'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'

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
const { confirm } = useConfirm()
const { prompt } = usePrompt()

const tabs = computed(() => [
  { key: 'console', label: '控制台' },
  { key: 'versions', label: `版本管理 ${installed.value.length}` },
])

const state = computed(() => snap.value?.state ?? 'stopped')
const isExternal = computed(() => state.value === 'external')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
// 文案口径保留本视图现状（"运行中"，与 TOOL_STATE_META.running="已启动" 存在家族级
// 文案分歧——待主线统一，此处不强行对齐以免破坏特征基线）。
const stateText = computed(() => toolStateMeta(state.value).text)
const currentVersion = computed(() => snap.value?.version || activeVersion.value || '自动选择')
const currentInstalled = computed(() => installed.value.find(item => item.version === currentVersion.value) ?? null)

const banner = computed(() => {
  if (currentInstalled.value?.integrity === 'drifted') return { tone: 'warn' as const, text: currentInstalled.value.integrityNote }
  if (currentInstalled.value?.integrity === 'invalid') return { tone: 'error' as const, text: currentInstalled.value.integrityNote }
  if (state.value === 'external') return { tone: 'warn' as const, text: '检测到安装版、portable 或其他版本的外部实例。Hanxi 可唤起窗口，但不会强制终止它。' }
  if (state.value === 'failed') return { tone: 'error' as const, text: snap.value?.error || 'MangoDisk 异常退出' }
  if (state.value === 'running') return { tone: 'ok' as const, text: 'MangoDisk 正在运行；磁盘扫描、清理与系统设置均在原版窗口内完成。' }
  return null
})

function integrityLabel(value: string): string {
  return ({ verified: '官方校验', 'local-baseline': '本地基线', drifted: '文件已漂移', invalid: '安装无效' } as Record<string, string>)[value] || '未知'
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
  // 原 window.confirm 收编至全局 useConfirm（文案逐字保留）
  const accepted = await confirm({
    title: `确定卸载 MangoDisk ${item.version}？`,
    description: '仅删除 Hanxi 版本目录，不会删除 %LOCALAPPDATA%\\app.mangodisk.desktop 中的数据。',
    confirmLabel: '卸载',
    tone: 'danger',
  })
  if (!accepted) return
  try { await MangoDiskAPI.RemoveVersion(item.version); showToast(`已卸载 ${item.version}`); await loadVersions() }
  catch (error) { showToast(`卸载失败：${getErrorMessage(error)}`) }
}

async function importLocal() {
  // 原 window.prompt 收编至全局 usePrompt（说明文案逐字保留于 description）
  const path = await prompt({
    title: '导入本地 EXE',
    description: '请输入本地 MangoDisk EXE 完整路径。Hanxi 只导入该 EXE，不搬运用户数据。',
    label: 'EXE 完整路径',
  })
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

// 状态兜底轮询 2.5s + uptime 秒级 tick（KeepAlive 激活/停用生命周期由 usePolling 承载）
usePolling(refreshStatus, 2500, { immediateFirstRun: false })
usePolling(() => { if (state.value === 'running') uptimeSec.value++ }, 1000, { immediateFirstRun: false })

useWailsEvent<DownloadProgress>('mangodisk:version-download', (p) => {
  if (!p) return
  downloading.value = { ...downloading.value, [p.version]: p }
  if (p.stage === 'done') setTimeout(async () => { delete downloading.value[p.version]; await loadVersions() }, 800)
})
useWailsEvent<Snapshot>('mangodisk:instance-state', (data) => { if (data) snap.value = data })

onMounted(() => { void Promise.all([loadVersions(), refreshStatus()]) })
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

    <MainTabNav :tabs="tabs" :model-value="activeMainTab" aria-label="MangoDisk 功能" @update:model-value="activeMainTab = $event as 'console' | 'versions'" />

    <div v-show="activeMainTab === 'console'" class="md-stack">
      <UiBanner v-if="banner" :tone="banner.tone" role="status">{{ banner.text }}</UiBanner>
      <section class="md-panel md-control-panel">
        <div class="md-control-main">
          <div class="md-app-mark" aria-hidden="true">M</div>
          <div class="md-control-copy">
            <strong>{{ currentVersion }}</strong>
            <span>{{ isExternal ? '非 Hanxi 托管' : isRunningOrStarting ? `PID ${snap?.pid || '—'} · ${fmtDuration(uptimeSec)}` : '等待启动' }}</span>
          </div>
        </div>
        <div class="md-actions">
          <UiButton variant="primary" :disabled="busy" @click="openWindow">{{ state === 'running' || state === 'external' ? '打开窗口' : '启动 MangoDisk' }}</UiButton>
          <UiButton variant="danger" :disabled="busy || !isRunningOrStarting || isExternal" @click="quit">退出</UiButton>
        </div>
      </section>

      <section class="md-panel md-settings">
        <div><strong>退出联动</strong><span>Hanxi 退出时关闭自己托管的 MangoDisk；外部实例不受影响。</span></div>
        <div class="md-settings-actions">
          <UiButton :disabled="busy || !installed.length" @click="createShortcut">创建桌面快捷方式</UiButton>
          <button class="md-switch" :aria-pressed="followOnExit" @click="toggleFollow"><span></span>{{ followOnExit ? '已开启' : '已关闭' }}</button>
        </div>
      </section>

      <section class="md-panel md-notes">
        <h2>托管边界与数据说明</h2>
        <div class="md-note-grid">
          <div><strong>原版能力</strong><span>磁盘扫描、清理、卸载和系统设置继续在 MangoDisk 原版窗口中操作。</span></div>
          <div><strong>共享数据</strong><span class="md-mono">%LOCALAPPDATA%\app.mangodisk.desktop</span><span>卸载 Hanxi 托管版本不会删除该目录。</span></div>
          <div><strong>内置更新器</strong><span>MangoDisk 自带 updater。若它替换 EXE，Hanxi 会标记“文件已漂移”并阻止静默启动。</span></div>
          <div><strong>运行环境</strong><span>Windows x64 · WebView2 Runtime · Tauri 单实例。</span></div>
        </div>
        <div class="md-license">第三方软件 · <button @click="openRepository">harry0703/MangoDisk</button> · GPL-3.0-only · 运行时从上游 GitHub Releases 下载</div>
      </section>
    </div>

    <div v-show="activeMainTab === 'versions'" class="md-stack">
      <section class="md-toolbar">
        <div><strong>版本仓库</strong><span>{{ installed.length }} 个已安装 · {{ releases.length }} 个远程版本</span></div>
        <div class="md-actions"><UiButton @click="importLocal">导入本地 EXE</UiButton><UiButton :disabled="loading" @click="loadVersions">刷新</UiButton></div>
      </section>

      <div v-if="listError" class="md-state-box md-error"><strong>版本列表加载失败</strong><span>{{ listError }}</span><UiButton small @click="loadVersions">重试</UiButton></div>
      <UiEmptyState v-else-if="loading && !installed.length"><strong>正在读取 MangoDisk 版本</strong><span>正在检查 GitHub Releases 与本地安装完整性。</span></UiEmptyState>

      <section v-else class="md-panel">
        <div class="md-section-head"><div><h2>已安装版本</h2><p>每次加载和冷启动前复核 EXE 哈希与 PE 身份。</p></div></div>
        <UiEmptyState v-if="!installed.length"><strong>尚未安装 MangoDisk</strong><span>从下方远程列表下载官方 portable EXE，或导入本地 MangoDisk EXE。</span></UiEmptyState>
        <div v-else class="md-version-list">
          <article v-for="item in installed" :key="item.version" class="md-version-row">
            <div class="md-version-main">
              <div class="md-version-title"><strong>{{ item.version }}</strong><span v-if="activeVersion === item.version" class="md-pill md-pill-primary">使用中</span><span class="md-pill" :data-integrity="item.integrity">{{ integrityLabel(item.integrity) }}</span></div>
              <p>{{ item.integrityNote }}</p>
              <div class="md-meta"><span>{{ fmtSize(item.size) }}</span><span>{{ fmtDate(item.installedAt) }}</span><span>{{ item.isImport ? '本地导入' : '官方下载' }}</span><span class="md-mono">SHA {{ shortHash(item.currentSha256) }}</span></div>
              <div v-if="item.integrity === 'drifted' || item.integrity === 'invalid'" class="md-integrity-detail"><span>标称 {{ item.version }}</span><span>当前 FileVersion {{ item.fileVersion || '未知' }}</span></div>
            </div>
            <div class="md-row-actions"><UiButton :disabled="activeVersion === item.version || item.integrity === 'invalid'" @click="setActive(item)">设为使用</UiButton><UiButton @click="openDir(item.dir)">打开位置</UiButton><UiButton variant="danger" :disabled="snap?.version === item.version && isRunningOrStarting" @click="removeVersion(item)">卸载</UiButton></div>
          </article>
        </div>
      </section>

      <section class="md-panel">
        <div class="md-section-head"><div><h2>远程版本</h2><p>只接收带 GitHub 官方 SHA-256 digest 的 Windows x64 portable EXE。</p></div></div>
        <UiEmptyState v-if="!releases.length"><strong>暂无远程版本</strong><span>请检查网络后刷新；当前本地版本仍可继续使用。</span></UiEmptyState>
        <div v-else class="md-table-wrap">
          <table class="tbl">
            <thead><tr><th>版本</th><th>发布时间</th><th>大小</th><th>完整性</th><th>操作</th></tr></thead>
            <tbody><tr v-for="rel in releases" :key="rel.version"><td class="md-mono"><strong>{{ rel.version }}</strong><span v-if="rel.isPre" class="md-pill">预发布</span></td><td>{{ fmtDate(rel.published) }}</td><td>{{ fmtSize(rel.size) }}</td><td><span class="md-pill md-pill-ok">GitHub SHA-256</span></td><td><template v-if="downloading[rel.version]"><div class="md-progress" :aria-label="`${rel.version} 下载进度`" role="progressbar" :aria-valuenow="progressOf(downloading[rel.version])" aria-valuemin="0" aria-valuemax="100"><span :style="{ width: `${progressOf(downloading[rel.version])}%` }"></span></div><small>{{ downloading[rel.version].stage }} {{ progressOf(downloading[rel.version]) || '' }}</small></template><span v-else-if="installed.some(item => item.version === rel.version)" class="md-installed">已安装</span><UiButton v-else variant="primary" small @click="download(rel)">下载</UiButton></td></tr></tbody>
          </table>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
/* 迁移说明：按钮族/选项卡/横幅/表格/空态已由全局原子（components.css）与 ui/* 组件承载，
   本文件只留业务版式；颜色一律语义 token（docs/FRONTEND.md §7）。 */
.md-page { max-width: 1120px; margin: 0 auto; display: flex; flex-direction: column; gap: 14px; color: var(--color-text); }
.md-header, .md-toolbar, .md-control-panel, .md-settings, .md-version-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.md-header h1 { margin: 0; font-size: 21px; letter-spacing: -.02em; } .md-header p, .md-toolbar span, .md-settings span { margin: 4px 0 0; color: var(--color-text-muted); font-size: 12px; }
.md-state-badge, .md-pill { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 5px 10px; background: var(--surface-panel); font-size: 12px; white-space: nowrap; }
.md-state-light { width: 8px; height: 8px; border-radius: 50%; background: var(--color-text-subtle); }.md-state-badge[data-state="running"] .md-state-light { background: var(--state-positive); }.md-state-badge[data-state="starting"] .md-state-light { background: var(--color-primary); }.md-state-badge[data-state="external"] .md-state-light { background: var(--state-warning); }.md-state-badge[data-state="failed"] .md-state-light { background: var(--state-danger); }
.md-stack { display: flex; flex-direction: column; gap: 12px; }.md-panel, .md-toolbar { border: 1px solid var(--color-border); border-radius: var(--radius-element); background: var(--surface-panel); padding: 16px; }.md-control-panel { min-height: 88px; }.md-control-main { display: flex; align-items: center; gap: 12px; min-width: 0; }.md-app-mark { display: grid; place-items: center; width: 42px; height: 42px; border-radius: 11px; background: color-mix(in srgb, var(--color-primary) 12%, transparent); color: var(--color-primary); font-size: 19px; font-weight: 700; }.md-control-copy { min-width: 0; display: flex; flex-direction: column; gap: 4px; }.md-control-copy strong, .md-mono { font-family: var(--font-mono); }.md-control-copy span { color: var(--color-text-muted); font-size: 12px; }
.md-actions, .md-row-actions { display: flex; gap: 8px; align-items: center; }
.md-settings > div:first-child { display: flex; flex-direction: column; gap: 2px; }.md-settings-actions { display: flex; align-items: center; gap: 10px; }.md-switch { display: inline-flex; align-items: center; gap: 7px; border: 0; background: transparent; color: var(--color-text-muted); cursor: pointer; }.md-switch span { width: 32px; height: 18px; border-radius: var(--radius-pill); background: var(--color-border); position: relative; }.md-switch span::after { content: ''; position: absolute; top: 3px; left: 3px; width: 12px; height: 12px; border-radius: 50%; background: var(--color-text-inverse); transition: transform var(--motion-base); }.md-switch[aria-pressed="true"] span { background: var(--color-primary); }.md-switch[aria-pressed="true"] span::after { transform: translateX(14px); }
.md-notes h2, .md-section-head h2 { margin: 0; font-size: 15px; }.md-note-grid { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 10px; margin-top: 12px; }.md-note-grid > div { display: flex; flex-direction: column; gap: 5px; padding: 11px; border: 1px solid var(--color-border); border-radius: 9px; background: var(--surface-page); }.md-note-grid span { color: var(--color-text-muted); font-size: 12px; overflow-wrap: anywhere; }.md-license { margin-top: 12px; color: var(--color-text-subtle); font-size: 11px; }.md-license button { border: 0; background: none; padding: 0; color: var(--color-primary); cursor: pointer; }
.md-section-head { display: flex; justify-content: space-between; margin-bottom: 12px; }.md-section-head p { color: var(--color-text-muted); font-size: 12px; margin: 4px 0 0; }.md-version-list { display: flex; flex-direction: column; gap: 8px; }.md-version-row { padding: 12px; border: 1px solid var(--color-border); border-radius: 10px; background: var(--surface-page); }.md-version-main { min-width: 0; }.md-version-title { display: flex; gap: 7px; align-items: center; flex-wrap: wrap; }.md-version-main p { margin: 5px 0; color: var(--color-text-muted); font-size: 12px; }.md-meta, .md-integrity-detail { display: flex; flex-wrap: wrap; gap: 10px; color: var(--color-text-subtle); font-size: 11px; }.md-integrity-detail { margin-top: 6px; color: var(--state-danger); }.md-pill { padding: 2px 7px; font-size: 10px; }.md-pill-primary { color: var(--color-primary); border-color: color-mix(in srgb, var(--color-primary) 35%, var(--color-border)); }.md-pill-ok, .md-pill[data-integrity="verified"] { color: var(--state-positive); }.md-pill[data-integrity="drifted"] { color: var(--state-warning); }.md-pill[data-integrity="invalid"] { color: var(--state-danger); }
.md-state-box { display: flex; flex-direction: column; align-items: flex-start; gap: 7px; padding: 18px; border: 1px dashed var(--color-border); border-radius: 10px; background: var(--surface-page); color: var(--color-text-muted); font-size: 12px; }.md-state-box strong { color: var(--color-text); }.md-error strong { color: var(--state-danger); }
.md-table-wrap { overflow-x: auto; }.md-table-wrap table { min-width: 680px; }.md-table-wrap :where(td, th) { padding: 10px; }.md-installed { color: var(--state-positive); }.md-progress { width: 110px; height: 5px; border-radius: var(--radius-pill); background: var(--color-border); overflow: hidden; }.md-progress span { display: block; height: 100%; background: var(--color-primary); transition: width .1s linear; }.md-table-wrap small { color: var(--color-text-subtle); }
@media (max-width: 760px) { .md-header, .md-toolbar, .md-control-panel, .md-settings, .md-version-row { align-items: flex-start; flex-direction: column; }.md-actions, .md-row-actions, .md-settings-actions { width: 100%; flex-wrap: wrap; }.md-actions :deep(.btn), .md-row-actions :deep(.btn), .md-settings-actions :deep(.btn) { flex: 1 1 120px; }.md-note-grid { grid-template-columns: 1fr; } }
</style>
