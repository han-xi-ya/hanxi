<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import * as EnvCheckAPI from '../../bindings/hanxi/internal/modules/envcheck/envcheckservice'
import type { ToolInfo } from '../../bindings/hanxi/internal/modules/envcheck/detect/models'
import type { Overview as GitOverview } from '../../bindings/hanxi/internal/modules/envcheck/gitversion/models'
import type { Overview as GoOverview } from '../../bindings/hanxi/internal/modules/envcheck/goversion/models'
import type { Overview as JavaOverview } from '../../bindings/hanxi/internal/modules/envcheck/javaversion/models'
import type { Overview as NodeOverview } from '../../bindings/hanxi/internal/modules/envcheck/nodeversion/models'
import type { Overview as PythonOverview } from '../../bindings/hanxi/internal/modules/envcheck/pythonversion/models'
import type { Channel } from '../../bindings/hanxi/internal/modules/envcheck/remoteversion/models'
import OfficialVersionsPanel from '../components/envcheck/OfficialVersionsPanel.vue'
import PackageManagerUpgradeHint from '../components/envcheck/PackageManagerUpgradeHint.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

type OfficialTool = 'git' | 'go' | 'node' | 'java' | 'python'
type NativeOverview = GoOverview | NodeOverview | JavaOverview | PythonOverview
interface PanelOverview { channels: Channel[]; isStale: boolean; fetchedAt?: string }
interface RemoteState { overview: PanelOverview | null; loading: boolean; error: string }

const tools = ref<ToolInfo[]>([])
const localLoading = ref(false)
const loadError = ref('')
const everLoaded = ref(false)
const { showToast } = useToast()

const remoteStates = reactive<Record<OfficialTool, RemoteState>>({
  git: { overview: null, loading: false, error: '' },
  go: { overview: null, loading: false, error: '' },
  node: { overview: null, loading: false, error: '' },
  java: { overview: null, loading: false, error: '' },
  python: { overview: null, loading: false, error: '' },
})

const OFFICIAL_META: Record<OfficialTool, { heading: string; downloadLabel: string }> = {
  git: { heading: 'Git for Windows 官网稳定版', downloadLabel: '打开 Git 官网下载页' },
  go: { heading: 'Go 官网支持版本', downloadLabel: '打开 Go 官网下载页' },
  node: { heading: 'Node.js 官网版本', downloadLabel: '打开 Node.js 官网下载页' },
  java: { heading: 'Eclipse Temurin 参考版本', downloadLabel: '打开 Temurin 下载页' },
  python: { heading: 'Python.org 官方版本', downloadLabel: '打开 Python 官网下载页' },
}

const loading = computed(() => localLoading.value || Object.values(remoteStates).some(state => state.loading))
const okCount = computed(() => tools.value.filter(tool => tool.status === 'installed').length)
const totalCount = computed(() => tools.value.length)

async function refresh() {
  if (loading.value) return
  localLoading.value = true
  loadError.value = ''
  const remotePromises = (['git', 'go', 'node', 'java', 'python'] as OfficialTool[]).map(tool => refreshOfficial(tool))
  try {
    tools.value = (await EnvCheckAPI.DetectAll()) ?? []
    everLoaded.value = true
  } catch (error) {
    loadError.value = `本机环境检测失败: ${getErrorMessage(error)}`
  } finally {
    localLoading.value = false
  }
  await Promise.allSettled(remotePromises)
}

async function refreshOfficial(tool: OfficialTool) {
  const state = remoteStates[tool]
  if (state.loading) return
  state.loading = true
  state.error = ''
  try {
    if (tool === 'git') {
      state.overview = adaptGitOverview(await EnvCheckAPI.GetGitForWindowsOverview())
    } else if (tool === 'go') {
      state.overview = adaptChannelOverview(await EnvCheckAPI.GetGoOverview())
    } else if (tool === 'node') {
      state.overview = adaptChannelOverview(await EnvCheckAPI.GetNodeOverview())
    } else if (tool === 'java') {
      state.overview = adaptChannelOverview(await EnvCheckAPI.GetJavaOverview())
    } else {
      state.overview = adaptChannelOverview(await EnvCheckAPI.GetPythonOverview())
    }
  } catch (error) {
    state.error = `官网版本查询失败: ${getErrorMessage(error)}`
  } finally {
    state.loading = false
  }
}

function adaptGitOverview(overview: GitOverview): PanelOverview {
  return {
    channels: [{
      key: 'stable', label: 'Stable', detail: '', relation: overview.relation,
      releases: (overview.releases ?? []).map(release => ({ version: release.version, published: release.published })),
    }],
    isStale: overview.isStale,
  }
}

function adaptChannelOverview(overview: NativeOverview): PanelOverview {
  return { channels: overview.channels ?? [], isStale: overview.isStale, fetchedAt: overview.fetchedAt }
}

async function openDownloadPage(tool: OfficialTool) {
  try {
    if (tool === 'git') await EnvCheckAPI.OpenGitForWindowsDownloadPage()
    else if (tool === 'go') await EnvCheckAPI.OpenGoDownloadPage()
    else if (tool === 'node') await EnvCheckAPI.OpenNodeDownloadPage()
    else if (tool === 'java') await EnvCheckAPI.OpenJavaDownloadPage()
    else await EnvCheckAPI.OpenPythonDownloadPage()
  } catch (error) {
    showToast(getErrorMessage(error))
  }
}

function isOfficialTool(name: string): name is OfficialTool {
  return name === 'git' || name === 'go' || name === 'node' || name === 'java' || name === 'python'
}

const STATUS_META: Record<string, { text: string; icon: string; cls: string }> = {
  installed: { text: '已安装', icon: '✓', cls: 'chip-installed' },
  missing: { text: '未安装', icon: '○', cls: 'chip-missing' },
  error: { text: '检测失败', icon: '!', cls: 'chip-error' },
  'store-stub': { text: '商店存根', icon: '⚠', cls: 'chip-stub' },
}

function metaOf(tool: ToolInfo) {
  return STATUS_META[tool.status] ?? STATUS_META.error
}

onMounted(refresh)
</script>

<template>
  <section class="page env-view">
    <div class="header-row">
      <div>
        <h1>开发环境检测</h1>
        <p class="subtitle">
          探测本机开发工具链的安装路径与版本。Git、Go、Node.js、Java、Python 卡片同时查询官方或明确发行方版本：
          每个通道只展示最新版本，本机已安装的版本线自动排在最前。npm、pnpm 仅提供可复制的手动升级命令，Hanxi 不直接执行安装或升级。
        </p>
      </div>
      <div class="btn-group">
        <span v-if="everLoaded" class="stat-text">✓ {{ okCount }} / {{ totalCount }} 已安装</span>
        <button class="btn btn-primary btn-small" :disabled="loading" @click="refresh">
          {{ loading ? '检测中…' : '↻ 重新检测' }}
        </button>
      </div>
    </div>

    <div v-if="loadError" class="hint-banner banner-error" role="alert">{{ loadError }}</div>
    <div v-if="loading && !everLoaded" class="empty-state" aria-live="polite">
      <p>正在检测开发环境并查询官网版本…</p>
    </div>

    <div v-else-if="everLoaded" class="tool-grid" :aria-busy="localLoading">
      <div v-for="tool in tools" :key="tool.name" class="tool-card" :class="[`status-${tool.status}`, { 'local-refreshing': localLoading }]">
        <div class="tool-card-top">
          <span class="tool-name">{{ tool.display }}</span>
          <span class="status-chip" :class="metaOf(tool).cls">{{ metaOf(tool).icon }} {{ metaOf(tool).text }}</span>
        </div>
        <div class="inst-meta">
          <div class="meta-line"><span class="k">版本</span><code class="mono">{{ tool.version || '—' }}</code></div>
          <div class="meta-line"><span class="k">路径</span><code class="mono tool-path" :title="tool.path || undefined">{{ tool.path || '—' }}</code></div>
        </div>
        <div v-if="tool.details?.java" class="tool-details">
          <span>发行版：{{ tool.details.java.vendor || '未知' }}</span>
          <span v-if="tool.details.java.runtime">运行时：{{ tool.details.java.runtime }}</span>
        </div>
        <div v-if="tool.hint" class="tool-hint" :class="tool.status === 'store-stub' ? 'hint-warn' : 'hint-error'">{{ tool.hint }}</div>

        <OfficialVersionsPanel
          v-if="isOfficialTool(tool.name)"
          :heading="OFFICIAL_META[tool.name].heading"
          :download-label="OFFICIAL_META[tool.name].downloadLabel"
          :channels="remoteStates[tool.name].overview?.channels ?? []"
          :loading="remoteStates[tool.name].loading"
          :error="remoteStates[tool.name].error"
          :stale="remoteStates[tool.name].overview?.isStale ?? false"
          :fetched-at="remoteStates[tool.name].overview?.fetchedAt"
          @retry="refreshOfficial(tool.name)"
          @open="openDownloadPage(tool.name)"
        />
        <PackageManagerUpgradeHint
          v-if="tool.name === 'npm' || tool.name === 'pnpm'"
          :tool="tool.name"
          :installed="tool.status === 'installed'"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.env-view { display: flex; flex-direction: column; gap: 14px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.btn-group { display: flex; align-items: center; justify-content: flex-end; gap: 10px; flex-wrap: wrap; flex-shrink: 0; }
.stat-text { font-size: 12px; color: var(--text-muted); }
.hint-banner { padding: 10px 14px; border-radius: 6px; font-size: 13px; border: 1px solid transparent; }
.banner-error { background: #ffebe9; border-color: rgba(207, 34, 46, 0.25); color: var(--danger); }
.empty-state { text-align: center; padding: 40px 24px; background: var(--bg-sidebar); border: 1px dashed var(--border-color); border-radius: 8px; }
.empty-state p { margin: 0; color: var(--text-muted); font-size: 13px; }
.tool-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr)); gap: 12px; }
.tool-card { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-left: 3px solid var(--border-color); border-radius: 8px; padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: opacity 0.15s ease; min-width: 0; }
.tool-card.local-refreshing { opacity: 0.68; }
.tool-card.status-installed { border-left-color: #1a7f37; }
.tool-card.status-missing { border-left-color: #8c959f; }
.tool-card.status-error { border-left-color: #cf222e; }
.tool-card.status-store-stub { border-left-color: #9a6700; }
.tool-card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.tool-name { font-size: 14px; font-weight: 700; color: var(--text-main); }
.status-chip { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; white-space: nowrap; }
.chip-installed { background: #dafbe1; color: #1a7f37; }
.chip-missing { background: #eaeef2; color: #656d76; }
.chip-error { background: #ffebe9; color: #cf222e; }
.chip-stub { background: #fff8c5; color: #9a6700; }
.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--text-muted); align-items: baseline; min-width: 0; }
.meta-line .k { color: var(--text-subtle); width: 36px; flex-shrink: 0; }
.mono { font-family: Consolas, monospace; color: var(--text-main); font-size: 11px; overflow-wrap: anywhere; }
.tool-path { min-width: 0; }
.tool-hint { font-size: 12px; border-radius: 5px; padding: 6px 8px; line-height: 1.5; }
.tool-details { display: flex; flex-direction: column; gap: 2px; color: var(--text-muted); font-size: 11px; line-height: 1.45; }
.hint-warn { background: #fff8c5; color: #9a6700; }
.hint-error { background: #ffebe9; color: var(--danger); }
.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@media (max-width: 768px) { .header-row { flex-direction: column; } .btn-group { width: 100%; justify-content: space-between; } }
@media (pointer: coarse) { .btn { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) { .tool-card, .btn { transition: none; } }
</style>
