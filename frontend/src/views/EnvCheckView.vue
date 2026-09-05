<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import * as EnvCheckAPI from '../../bindings/hanxi/internal/modules/envcheck/envcheckservice'
import * as BCUAPI from '../../bindings/hanxi/internal/modules/bcu/bcuservice'
import type { ToolInfo } from '../../bindings/hanxi/internal/modules/envcheck/detect/models'
import type { Overview as DotNetOverview } from '../../bindings/hanxi/internal/modules/envcheck/dotnetversion/models'
import type { Overview as GitOverview } from '../../bindings/hanxi/internal/modules/envcheck/gitversion/models'
import type { Overview as GoOverview } from '../../bindings/hanxi/internal/modules/envcheck/goversion/models'
import type { Overview as JavaOverview } from '../../bindings/hanxi/internal/modules/envcheck/javaversion/models'
import type { Overview as NodeOverview } from '../../bindings/hanxi/internal/modules/envcheck/nodeversion/models'
import type { Overview as PythonOverview } from '../../bindings/hanxi/internal/modules/envcheck/pythonversion/models'
import type { Channel } from '../../bindings/hanxi/internal/modules/envcheck/remoteversion/models'
import type { Overview as NpmOverview, ToolOverview, OperationProgress, OperationLog } from '../../bindings/hanxi/internal/modules/envcheck/npmtool/models'
import OfficialVersionsPanel from '../components/envcheck/OfficialVersionsPanel.vue'
import PackageManagerUpgradeHint from '../components/envcheck/PackageManagerUpgradeHint.vue'
import NpmToolActions from '../components/envcheck/NpmToolActions.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { envStatusMeta } from '../constants/status'

type OfficialTool = 'git' | 'go' | 'node' | 'java' | 'python' | 'dotnet'
type NativeOverview = GoOverview | NodeOverview | JavaOverview | PythonOverview | DotNetOverview
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
  dotnet: { overview: null, loading: false, error: '' },
})

// npm 全局工具目录状态：后端按目录回传工具集合，前端不写死 claude/codex。
const npmOverview = ref<NpmOverview | null>(null)
const npmLoading = ref(false)
const npmError = ref('')
const npmLogs = reactive<Record<string, string[]>>({})
// 当前进行中的操作（全局锁保证同一时刻至多一个）。
const npmActive = ref<OperationProgress | null>(null)
const uninstallTarget = ref<ToolOverview | null>(null)
const uninstallBusy = ref(false)

const npmByName = computed(() => new Map((npmOverview.value?.tools ?? []).map(tool => [tool.local.name, tool])))
// npmBusy：进行中操作优先取实时事件，回退到 overview 快照（页面重挂载恢复忙碌态）。
const npmBusyOperation = computed<OperationProgress | null>(() => npmActive.value ?? npmOverview.value?.activeOperation ?? null)

function npmOperationFor(name: string): OperationProgress | null {
  const active = npmBusyOperation.value
  return active && active.toolId === name ? active : null
}
function npmBusyElsewhere(name: string): boolean {
  const active = npmBusyOperation.value
  return !!active && active.toolId !== name
}

const OFFICIAL_META: Record<OfficialTool, { heading: string; downloadLabel: string }> = {
  git: { heading: 'Git for Windows 官网稳定版', downloadLabel: '打开 Git 官网下载页' },
  go: { heading: 'Go 官网支持版本', downloadLabel: '打开 Go 官网下载页' },
  node: { heading: 'Node.js 官网版本', downloadLabel: '打开 Node.js 官网下载页' },
  java: { heading: 'Eclipse Temurin 参考版本', downloadLabel: '打开 Temurin 下载页' },
  python: { heading: 'Python.org 官方版本', downloadLabel: '打开 Python 官网下载页' },
  dotnet: { heading: '.NET 官方支持线', downloadLabel: '打开 .NET 官网下载页' },
}

const uninstallDetails = computed(() => {
  const tool = uninstallTarget.value
  if (!tool) return []
  return [
    { label: 'npm 包', value: tool.tool.package },
    { label: '当前版本', value: tool.local.version || '—' },
    { label: '影响范围', value: '仅移除 npm 全局安装；配置与登录态目录不受影响' },
  ]
})

const loading = computed(() => localLoading.value || npmLoading.value || Object.values(remoteStates).some(state => state.loading))
const okCount = computed(() => tools.value.filter(tool => tool.status === 'installed').length)
const totalCount = computed(() => tools.value.length)

async function refresh() {
  if (loading.value) return
  localLoading.value = true
  loadError.value = ''
  const remotePromises = (['git', 'go', 'node', 'java', 'python', 'dotnet'] as OfficialTool[]).map(tool => refreshOfficial(tool))
  remotePromises.push(refreshNpm())
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

async function refreshNpm() {
  if (npmLoading.value) return
  npmLoading.value = true
  npmError.value = ''
  try {
    npmOverview.value = await EnvCheckAPI.GetNpmToolsOverview()
  } catch (error) {
    npmError.value = `npm 工具信息获取失败: ${getErrorMessage(error)}`
  } finally {
    npmLoading.value = false
  }
}

// npm 操作：装/升直接发起，卸先弹二次确认；受理后由事件流推进度，终态再重取。
async function startNpmAction(kind: 'install' | 'upgrade', tool: ToolOverview) {
  if (npmBusyOperation.value) return
  npmLogs[tool.local.name] = []
  try {
    const accepted = kind === 'install'
      ? await EnvCheckAPI.InstallNpmTool(tool.tool.command)
      : await EnvCheckAPI.UpgradeNpmTool(tool.tool.command)
    npmActive.value = {
      operationId: accepted.operationId, toolId: tool.local.name, kind,
      stage: 'started', message: accepted.message, terminal: false, success: false,
    }
  } catch (error) {
    showToast(getErrorMessage(error))
  }
}

function requestUninstall(tool: ToolOverview) {
  if (npmBusyOperation.value) return
  uninstallTarget.value = tool
}

async function confirmUninstall() {
  const tool = uninstallTarget.value
  if (!tool) return
  uninstallBusy.value = true
  try {
    const accepted = await EnvCheckAPI.UninstallNpmTool(tool.tool.command)
    npmLogs[tool.local.name] = []
    npmActive.value = {
      operationId: accepted.operationId, toolId: tool.local.name, kind: 'uninstall',
      stage: 'started', message: accepted.message, terminal: false, success: false,
    }
    uninstallTarget.value = null
  } catch (error) {
    showToast(`卸载失败: ${getErrorMessage(error)}`)
  } finally {
    uninstallBusy.value = false
  }
}

function handleNpmOperation(progress: OperationProgress) {
  if (progress.terminal) {
    npmActive.value = null
    showToast(progress.message || (progress.success ? 'npm 操作完成' : 'npm 操作失败'))
    // 装/卸后本机状态与 registry 关系都会变，重取 overview 与卡片列表自然收敛。
    void refreshNpm()
    EnvCheckAPI.DetectAll().then(result => { tools.value = result ?? [] }).catch(() => {})
    return
  }
  if (!npmActive.value || npmActive.value.operationId === progress.operationId) {
    npmActive.value = progress
  }
}

function handleNpmLog(entry: OperationLog) {
  const lines = npmLogs[entry.toolId] ?? (npmLogs[entry.toolId] = [])
  lines.push(entry.line)
  if (lines.length > 200) lines.splice(0, lines.length - 200)
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
    } else if (tool === 'python') {
      state.overview = adaptChannelOverview(await EnvCheckAPI.GetPythonOverview())
    } else {
      state.overview = adaptChannelOverview(await EnvCheckAPI.GetDotNetOverview())
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

// openBCUForUninstall 委托 BCUninstaller 完成运行库卸载：Hanxi 只负责唤起它的窗口，
// 卸载目标选择与确认全部在 BCU 内完成；除受管 npm 全局工具外，本模块对其余工具链仍保持零执行面。
async function openBCUForUninstall() {
  try {
    await BCUAPI.OpenWindow()
    showToast('已打开 BCUninstaller：在列表中搜索 ".NET" 即可卸载对应运行时版本线')
  } catch (error) {
    showToast(getErrorMessage(error))
  }
}

async function revealPath(tool: ToolInfo) {
  try {
    await EnvCheckAPI.RevealToolPath(tool.name)
  } catch (error) {
    showToast(getErrorMessage(error))
  }
}

async function openDownloadPage(tool: OfficialTool) {
  try {
    if (tool === 'git') await EnvCheckAPI.OpenGitForWindowsDownloadPage()
    else if (tool === 'go') await EnvCheckAPI.OpenGoDownloadPage()
    else if (tool === 'node') await EnvCheckAPI.OpenNodeDownloadPage()
    else if (tool === 'java') await EnvCheckAPI.OpenJavaDownloadPage()
    else if (tool === 'python') await EnvCheckAPI.OpenPythonDownloadPage()
    else await EnvCheckAPI.OpenDotNetDownloadPage()
  } catch (error) {
    showToast(getErrorMessage(error))
  }
}

function isOfficialTool(name: string): name is OfficialTool {
  return name === 'git' || name === 'go' || name === 'node' || name === 'java' || name === 'python' || name === 'dotnet'
}

// 状态语义表已上收 constants/status（ENV_STATUS_META text/icon 逐字同源，cls 由 tone 取代）

// joinVersions 并列展示 .NET 并排安装的版本列表（后端已按版本升序去重）。
function joinVersions(versions?: string[] | null) {
  return (versions ?? []).join(' / ')
}

// dotnetExtraLines 返回除版本行首位版本线外、本机仍并排存在的其他 .NET 版本线，
// 避免"版本 10.0.400"被误读为机器上只有 10。
function dotnetExtraLines(tool: ToolInfo): string[] {
  const dotnet = tool.details?.dotnet
  if (!dotnet) return []
  const lineOf = (version: string) => /^(\d+\.\d+)/.exec(version)?.[1] ?? ''
  const primary = lineOf(tool.version || '')
  const lines = new Set<string>()
  for (const version of [...(dotnet.runtimes ?? []), ...(dotnet.sdks ?? [])]) {
    const line = lineOf(version)
    if (line && line !== primary) lines.add(line)
  }
  return [...lines].sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))
}

function metaOf(tool: ToolInfo) {
  return envStatusMeta(tool.status)
}

// npm 事件流订阅（useWailsEvent：setup 期注册防丢早期推送，卸载自动注销）
useWailsEvent<OperationProgress>('envcheck:npm-tool-operation', (p) => p && handleNpmOperation(p))
useWailsEvent<OperationLog>('envcheck:npm-tool-log', (entry) => entry && handleNpmLog(entry))

onMounted(() => {
  void refresh()
})
</script>

<template>
  <section class="page env-view">
    <PageHeader
      title="开发环境检测"
      subtitle="探测本机开发工具链的安装路径与版本。Git、Go、Node.js、Java、Python、.NET 卡片同时查询官方或明确发行方版本： 每个通道只展示最新版本，本机已安装的版本线自动排在最前（.NET 卡片展示 SDK 优先版本，版本关系按运行时口径比较）。 npm、pnpm 本体仅提供可复制的手动升级命令；Claude Code、Codex 等受管 npm 全局工具支持一键安装/升级/卸载（卸载需二次确认）。"
    >
      <template #actions>
        <div class="btn-group">
          <span v-if="everLoaded" class="stat-text">✓ {{ okCount }} / {{ totalCount }} 已安装</span>
          <button class="btn btn-primary btn-small" :disabled="loading" @click="refresh">
            {{ loading ? '检测中…' : '↻ 重新检测' }}
          </button>
        </div>
      </template>
    </PageHeader>

    <div v-if="loadError" class="banner banner-error" role="alert">{{ loadError }}</div>
    <div v-if="loading && !everLoaded" class="empty-state" aria-live="polite">
      <p>正在检测开发环境并查询官网版本…</p>
    </div>

    <div v-else-if="everLoaded" class="tool-grid" :aria-busy="localLoading">
      <div v-for="tool in tools" :key="tool.name" class="tool-card" :class="[`status-${tool.status}`, { 'local-refreshing': localLoading }]">
        <div class="tool-card-top">
          <span class="tool-name">{{ tool.display }}</span>
          <span class="chip status-chip" :class="`chip-${metaOf(tool).tone}`">{{ metaOf(tool).icon }} {{ metaOf(tool).text }}</span>
        </div>
        <div class="inst-meta">
          <div class="meta-line">
            <span class="k">版本</span>
            <code class="mono">{{ tool.version || '—' }}</code>
            <span v-if="tool.name === 'dotnet' && dotnetExtraLines(tool).length" class="extra-lines">另装版本线 {{ dotnetExtraLines(tool).join('、') }}</span>
          </div>
          <div class="meta-line">
            <span class="k">路径</span>
            <button
              v-if="tool.path && tool.status === 'installed'"
              class="mono tool-path path-link"
              :title="`在资源管理器中定位 ${tool.path}`"
              @click="revealPath(tool)"
            >{{ tool.path }}</button>
            <code v-else class="mono tool-path">{{ tool.path || '—' }}</code>
          </div>
        </div>
        <div v-if="tool.details?.java" class="tool-details">
          <span>发行版：{{ tool.details.java.vendor || '未知' }}</span>
          <span v-if="tool.details.java.runtime">运行时：{{ tool.details.java.runtime }}</span>
        </div>
        <div v-if="tool.details?.dotnet" class="tool-details">
          <span>SDK：{{ joinVersions(tool.details.dotnet.sdks) || '未安装（仅运行时）' }}</span>
          <span>运行时：{{ joinVersions(tool.details.dotnet.runtimes) || '未知' }}</span>
          <span v-if="tool.details.dotnet.desktops?.length">桌面运行时：{{ joinVersions(tool.details.dotnet.desktops) }}</span>
          <span v-if="tool.details.dotnet.aspnet?.length">ASP.NET 运行时：{{ joinVersions(tool.details.dotnet.aspnet) }}</span>
        </div>
        <div v-if="tool.name === 'dotnet'" class="tool-actions">
          <button
            class="btn btn-secondary btn-small"
            title="打开 BCUninstaller 自行选择卸载目标；注意卸载 8.0 线会导致依赖它的 BCUninstaller 自身无法启动"
            @click="openBCUForUninstall"
          >🗑 用 BCUninstaller 卸载 / 搜索运行库</button>
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
        <NpmToolActions
          v-if="npmByName.has(tool.name)"
          :overview="npmByName.get(tool.name)!"
          :operation="npmOperationFor(tool.name)"
          :busy-elsewhere="npmBusyElsewhere(tool.name)"
          :log-lines="npmLogs[tool.name] ?? []"
          @install="startNpmAction('install', npmByName.get(tool.name)!)"
          @upgrade="startNpmAction('upgrade', npmByName.get(tool.name)!)"
          @uninstall="requestUninstall(npmByName.get(tool.name)!)"
          @retry="refreshNpm"
        />
      </div>
    </div>

    <ConfirmDialog
      :open="!!uninstallTarget"
      :title="`卸载 ${uninstallTarget?.tool.display ?? 'npm 工具'}`"
      description="将经 npm 全局卸载该命令行工具，需二次确认。用户配置目录与登录态不会被删除。"
      confirm-label="确认卸载"
      cancel-label="取消"
      tone="danger"
      :busy="uninstallBusy"
      :details="uninstallDetails"
      @confirm="confirmUninstall"
      @cancel="uninstallTarget = null"
      @update:open="(value: boolean) => { if (!value) uninstallTarget = null }"
    />
  </section>
</template>

<style scoped>
.env-view { display: flex; flex-direction: column; gap: 14px; }
/* 页头/副标题/错误横幅/空态/.btn 基础与 primary/small/.chip 家族/焦点环/减弱动效
   均由 PageHeader 与 components.css + base.css 全局承载 */
.btn-group { display: flex; align-items: center; justify-content: flex-end; gap: 10px; flex-wrap: wrap; flex-shrink: 0; }
.stat-text { font-size: 12px; color: var(--color-text-muted); }
.tool-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr)); gap: 12px; }
.tool-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-left: 3px solid var(--color-border); border-radius: var(--radius-control); padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: opacity var(--motion-base) ease; min-width: 0; }
.tool-card.local-refreshing { opacity: 0.68; }
.tool-card.status-installed { border-left-color: var(--state-positive); }
.tool-card.status-missing { border-left-color: var(--color-text-subtle); }
.tool-card.status-error { border-left-color: var(--state-danger); }
.tool-card.status-store-stub { border-left-color: var(--state-warning); }
.tool-card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.tool-name { font-size: 14px; font-weight: 700; color: var(--color-text); }
/* 本视图 chip 仅调图标间距；底色/形状走全局 .chip-{tone} */
.status-chip { gap: 5px; }
.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; min-width: 0; }
.meta-line .k { color: var(--color-text-subtle); width: 36px; flex-shrink: 0; }
.mono { font-size: 11px; overflow-wrap: anywhere; }
.tool-path { min-width: 0; }
.path-link { display: block; padding: 0; border: 0; background: none; text-align: left; cursor: pointer; font: inherit; overflow-wrap: anywhere; text-decoration: underline; text-decoration-color: var(--color-border); text-underline-offset: 2px; }
.path-link:hover { text-decoration-color: var(--color-primary); color: var(--color-primary); }
.tool-hint { font-size: 12px; border-radius: 5px; padding: 6px 8px; line-height: 1.5; }
.tool-details { display: flex; flex-direction: column; gap: 2px; color: var(--color-text-muted); font-size: 11px; line-height: 1.45; }
.hint-warn { background: var(--state-warning-soft); color: var(--state-warning); }
.hint-error { background: var(--state-danger-soft); color: var(--state-danger); }
/* envcheck 家族特有"描边强调"变体（非全局 .btn-secondary 的实底语义），保留并 token 化 */
.btn-secondary { background: transparent; color: var(--color-primary); border-color: var(--color-border); }
.btn-secondary:hover:not(:disabled) { border-color: var(--color-primary); background: var(--surface-soft); }
.tool-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.extra-lines { margin-left: auto; flex-shrink: 0; padding: 1px 7px; border-radius: var(--radius-pill); background: var(--state-information-soft); color: var(--state-information); font-size: 10px; }
@media (max-width: 768px) { .header-row { flex-direction: column; } .btn-group { width: 100%; justify-content: space-between; } }
</style>
