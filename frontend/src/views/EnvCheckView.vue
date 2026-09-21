<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import * as EnvCheckAPI from '../../bindings/hanxi/internal/modules/envcheck/envcheckservice'
import * as BCUAPI from '../../bindings/hanxi/internal/modules/bcu/bcuservice'
import type { ToolInfo } from '../../bindings/hanxi/internal/modules/envcheck/detect/models'
import type { ToolUsage } from '../../bindings/hanxi/internal/modules/envcheck/diskusage/models'
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
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import HistoryPanel from '../components/tool/HistoryPanel.vue'
import { useConfirm } from '../composables/useConfirm'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { envStatusMeta } from '../constants/status'

type OfficialTool = 'git' | 'go' | 'node' | 'java' | 'python' | 'dotnet'
type NativeOverview = GoOverview | NodeOverview | JavaOverview | PythonOverview | DotNetOverview
interface PanelOverview { channels: Channel[]; isStale: boolean; fetchedAt?: string }
interface RemoteState { overview: PanelOverview | null; loading: boolean; error: string }

const tools = ref<ToolInfo[]>([])
const activeMainTab = ref<'local' | 'versions' | 'history'>('local')
const MAIN_TABS = [
  { key: 'local', label: '本机环境' },
  { key: 'versions', label: '版本与工具' },
  { key: 'history', label: '历史记录' },
]
const OFFICIAL_TOOLS: OfficialTool[] = ['git', 'go', 'node', 'java', 'python', 'dotnet']
const localLoading = ref(false)
const loadError = ref('')
const everLoaded = ref(false)
const { showToast } = useToast()
const { confirm } = useConfirm()
const { copyWithToast } = useClipboard()

// ---------- 空间家底（N14）----------
// 按需测量（按钮触发，服务端 30s 整场预算）：本体安装目录 + 依赖/缓存目录
// 逐行呈现；Partial 行标"≥"下限，推导在场但磁盘缺席的行灰显"未落地"。
const usageRows = ref<ToolUsage[]>([])
const usageLoading = ref(false)
const usageAt = ref('')
const usageError = ref('')
const usageAnyPartial = computed(() =>
  usageRows.value.some((tu) => (tu.dirs ?? []).some((d) => d.partial)),
)

function fmtSize(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${bytes} B`
}

async function loadDiskUsage() {
  if (usageLoading.value) return
  usageLoading.value = true
  usageError.value = ''
  try {
    usageRows.value = (await EnvCheckAPI.GetDiskUsage()) ?? []
    usageAt.value = new Date().toLocaleTimeString()
  } catch (e: unknown) {
    usageRows.value = []
    usageAt.value = ''
    usageError.value = `空间家底测量失败: ${getErrorMessage(e)}`
  } finally {
    usageLoading.value = false
  }
}

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

const toolsByName = computed(() => new Map(tools.value.map(tool => [tool.name, tool])))
const officialTools = computed(() => OFFICIAL_TOOLS.map(name => ({ name, local: toolsByName.value.get(name) })))
const packageManagers = computed(() => (['npm', 'pnpm'] as const).map(name => ({ name, local: toolsByName.value.get(name) })))
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

const TOOL_LABELS: Record<OfficialTool | 'npm' | 'pnpm', string> = {
  git: 'Git',
  go: 'Go',
  node: 'Node.js',
  java: 'Java',
  python: 'Python',
  dotnet: '.NET',
  npm: 'npm',
  pnpm: 'pnpm',
}

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

// 卸载先经全局 useConfirm 单例二次确认（原视图自挂 ConfirmDialog 已收编）；
// 受理成功后由事件流推进度，失败以 toast 回执、可重新发起。
async function requestUninstall(tool: ToolOverview) {
  if (npmBusyOperation.value) return
  const accepted = await confirm({
    title: `卸载 ${tool.tool.display}`,
    description: '将经 npm 全局卸载该命令行工具，需二次确认。用户配置目录与登录态不会被删除。',
    confirmLabel: '确认卸载',
    tone: 'danger',
    details: [
      { label: 'npm 包', value: tool.tool.package },
      { label: '当前版本', value: tool.local.version || '—' },
      { label: '影响范围', value: '仅移除 npm 全局安装；配置与登录态目录不受影响' },
    ],
  })
  if (!accepted) return
  if (npmBusyOperation.value) return // 确认期间可能已有其它 npm 操作受理，二次门禁
  try {
    const result = await EnvCheckAPI.UninstallNpmTool(tool.tool.command)
    npmLogs[tool.local.name] = []
    npmActive.value = {
      operationId: result.operationId, toolId: tool.local.name, kind: 'uninstall',
      stage: 'started', message: result.message, terminal: false, success: false,
    }
  } catch (error) {
    showToast(`卸载失败: ${getErrorMessage(error)}`)
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

// 复制环境报告本体（PLAN_CLIPBOARD §2.2：报告此前无复制入口，仅子组件有"复制升级
// 命令"）：一行一工具，Tab 分隔"名称/版本/路径"，未安装如实标注。
function copyReport() {
  if (!everLoaded.value || tools.value.length === 0) {
    showToast('尚未完成本机检测，暂无报告可复制')
    return
  }
  const stamp = new Date().toLocaleString('zh-CN', { hour12: false })
  const body = tools.value
    .map((t) => [t.display, t.version || '未安装', t.path || '—'].join('\t'))
    .join('\n')
  void copyWithToast(`Hanxi 开发环境报告（${stamp}）\n${body}`, '环境报告已复制')
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
      subtitle="检测本机开发工具链并对照官方版本；受管 npm 全局工具可在页内安装、升级或卸载。"
    >
      <template #actions>
        <MainTabNav
          v-model="activeMainTab"
          :tabs="MAIN_TABS"
          id-prefix="envcheck"
          label="开发环境检测页面"
        />
      </template>
    </PageHeader>

    <div class="status-toolbar" :aria-busy="loading">
      <div class="status-summary">
        <strong>{{ everLoaded ? `本机已安装 ${okCount} / ${totalCount} 项` : '尚未完成本机检测' }}</strong>
        <span>{{ loading ? '正在刷新本机环境、官方版本与 npm 工具信息…' : '一次刷新同步更新两个标签中的数据。' }}</span>
      </div>
      <span class="env-actions">
        <button class="btn btn-secondary btn-small copy-report-button" :disabled="!everLoaded" title="复制全部工具的版本与路径" @click="copyReport">
          复制报告
        </button>
        <button class="btn btn-primary btn-small refresh-button" :disabled="loading" @click="refresh">
          {{ loading ? '检测中…' : '↻ 重新检测' }}
        </button>
      </span>
    </div>

    <div v-if="loadError" class="banner banner-error" role="alert">{{ loadError }}</div>

    <div
      id="envcheck-local-panel"
      v-show="activeMainTab === 'local'"
      class="tab-body"
      role="tabpanel"
      aria-labelledby="envcheck-local-tab"
    >
      <div v-if="localLoading && !everLoaded" class="empty-state" aria-live="polite">
        <p>正在检测本机开发工具链…</p>
      </div>
      <div v-else-if="everLoaded && tools.length" class="tool-grid" :aria-busy="localLoading">
        <article v-for="tool in tools" :key="tool.name" class="tool-card" :class="[`status-${tool.status}`, { 'local-refreshing': localLoading }]">
          <div class="tool-card-top">
            <span class="tool-name">{{ tool.display }}</span>
            <span class="chip status-chip" :class="`chip-${metaOf(tool).tone}`">{{ metaOf(tool).icon }} {{ metaOf(tool).text }}</span>
          </div>
          <div class="inst-meta">
            <div class="meta-line">
              <span class="k">版本</span>
              <code class="mono">{{ tool.version || '—' }}</code>
              <button v-if="tool.version" class="link-button meta-copy" :aria-label="`复制 ${tool.display} 版本`"
                @click="copyWithToast(tool.version, `已复制 ${tool.display} 版本`)">复制</button>
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
              <button v-if="tool.path" class="link-button meta-copy" :aria-label="`复制 ${tool.display} 路径`"
                @click="copyWithToast(tool.path, `已复制 ${tool.display} 路径`)">复制</button>
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
              class="btn btn-accent-outline btn-small"
              title="打开 BCUninstaller 自行选择卸载目标；注意卸载 8.0 线会导致依赖它的 BCUninstaller 自身无法启动"
              @click="openBCUForUninstall"
            >用 BCUninstaller 卸载 / 搜索运行库</button>
          </div>
          <div v-if="tool.hint" class="tool-hint" :class="tool.status === 'store-stub' ? 'hint-warn' : 'hint-error'">{{ tool.hint }}</div>
        </article>
      </div>
      <div v-else-if="everLoaded" class="empty-state">
        <p>未返回可识别的开发工具，请重新检测。</p>
      </div>

      <!-- 空间家底（N14）：本体目录 + 依赖/缓存占用总览 -->
      <section class="usage-panel" aria-label="空间家底">
        <header class="usage-head">
          <span class="usage-title">空间家底 <span class="chip chip-neutral">安装目录 · 依赖缓存</span></span>
          <span class="usage-sub" aria-live="polite">
            <template v-if="usageLoading">正在测量（整场 30 秒封顶，超时项按"≥"下限呈现）…</template>
            <template v-else-if="usageError">{{ usageError }}</template>
            <template v-else-if="usageAt">测量于 {{ usageAt }}{{ usageAnyPartial ? ' · 含下限估算项' : '' }}</template>
            <template v-else>测量各开发工具的本体与缓存目录，看清磁盘肥瘦</template>
          </span>
          <button class="btn btn-secondary btn-small" :disabled="usageLoading" @click="loadDiskUsage">
            ↻ {{ usageRows.length ? '重新测量' : '测量空间家底' }}
          </button>
        </header>
        <div v-for="tu in usageRows" :key="tu.tool" class="usage-tool">
          <div class="usage-tool-name">{{ tu.display }}</div>
          <div v-for="d in (tu.dirs ?? [])" :key="d.label" class="usage-line" :class="{ 'usage-offline': !d.exists }">
            <span class="usage-kind" :class="d.kind === 'install' ? 'kind-install' : 'kind-cache'">{{ d.kind === 'install' ? '本体' : '缓存' }}</span>
            <span class="usage-label">{{ d.label }}</span>
            <code class="mono usage-path" :title="d.path">{{ d.path || '未能推导' }}</code>
            <span v-if="!d.exists" class="usage-muted">未落地</span>
            <span v-else class="mono usage-size" :class="{ 'usage-partial': d.partial }" :title="`${d.bytes.toLocaleString()} 字节${d.partial ? '（下限）' : ''}`">
              {{ d.partial ? '≥ ' : '' }}{{ fmtSize(d.bytes) }}
            </span>
          </div>
        </div>
        <p v-if="usageAt && !usageError && !usageRows.length" class="usage-sub">未识别到可测量家底的本体/缓存目录。</p>
      </section>
    </div>

    <div
      id="envcheck-versions-panel"
      v-show="activeMainTab === 'versions'"
      class="tab-body version-workspace"
      role="tabpanel"
      aria-labelledby="envcheck-versions-tab"
    >
      <section class="workspace-section">
        <div class="section-heading">
          <div>
            <h2>官方版本对照</h2>
            <p>按官方或明确发行方通道对照本机版本；.NET 卡片展示 SDK 优先版本，版本关系按运行时口径比较。</p>
          </div>
        </div>
        <div class="management-grid">
          <article v-for="item in officialTools" :key="item.name" class="management-card">
            <div class="local-summary">
              <div>
                <span class="tool-name">{{ TOOL_LABELS[item.name] }}</span>
                <span v-if="item.local" class="chip status-chip" :class="`chip-${metaOf(item.local).tone}`">{{ metaOf(item.local).text }}</span>
              </div>
              <code class="mono">本机 {{ item.local?.version || '未安装' }}</code>
            </div>
            <OfficialVersionsPanel
              :heading="OFFICIAL_META[item.name].heading"
              :download-label="OFFICIAL_META[item.name].downloadLabel"
              :channels="remoteStates[item.name].overview?.channels ?? []"
              :loading="remoteStates[item.name].loading"
              :error="remoteStates[item.name].error"
              :stale="remoteStates[item.name].overview?.isStale ?? false"
              :fetched-at="remoteStates[item.name].overview?.fetchedAt"
              @retry="refreshOfficial(item.name)"
              @open="openDownloadPage(item.name)"
            />
          </article>
        </div>
      </section>

      <section class="workspace-section">
        <div class="section-heading">
          <div>
            <h2>包管理器升级指引</h2>
            <p>只提供可复制的安全指引，不在 Hanxi 内代为执行 npm 或 pnpm 自升级。</p>
          </div>
        </div>
        <div class="management-grid compact-grid">
          <article v-for="item in packageManagers" :key="item.name" class="management-card">
            <div class="local-summary">
              <div>
                <span class="tool-name">{{ TOOL_LABELS[item.name] }}</span>
                <span v-if="item.local" class="chip status-chip" :class="`chip-${metaOf(item.local).tone}`">{{ metaOf(item.local).text }}</span>
              </div>
              <code class="mono">本机 {{ item.local?.version || '未安装' }}</code>
            </div>
            <PackageManagerUpgradeHint :tool="item.name" :installed="item.local?.status === 'installed'" />
          </article>
        </div>
      </section>

      <section class="workspace-section">
        <div class="section-heading">
          <div>
            <h2>受管 npm 全局工具</h2>
            <p>工具目录由后端配置驱动；安装、升级与卸载共用一个全局操作锁。</p>
          </div>
          <button v-if="npmError" class="btn btn-secondary btn-small" :disabled="npmLoading" @click="refreshNpm">重试 npm 信息</button>
        </div>
        <div v-if="npmError" class="banner banner-error section-banner" role="alert">{{ npmError }}<template v-if="npmOverview?.tools?.length">；继续显示上次结果。</template></div>
        <div v-if="npmLoading && !npmOverview" class="empty-state" aria-live="polite">
          <p>正在读取受管 npm 工具信息…</p>
        </div>
        <div v-else-if="npmOverview?.tools?.length" class="management-grid compact-grid">
          <article v-for="overview in npmOverview.tools" :key="overview.local.name" class="management-card">
            <div class="local-summary">
              <div>
                <span class="tool-name">{{ overview.tool.display }}</span>
                <span class="chip status-chip" :class="`chip-${metaOf(overview.local).tone}`">{{ metaOf(overview.local).text }}</span>
              </div>
              <code class="mono">本机 {{ overview.local.version || '未安装' }}</code>
            </div>
            <NpmToolActions
              :overview="overview"
              :operation="npmOperationFor(overview.local.name)"
              :busy-elsewhere="npmBusyElsewhere(overview.local.name)"
              :log-lines="npmLogs[overview.local.name] ?? []"
              @install="startNpmAction('install', overview)"
              @upgrade="startNpmAction('upgrade', overview)"
              @uninstall="requestUninstall(overview)"
              @retry="refreshNpm"
            />
          </article>
        </div>
        <div v-else-if="!npmLoading && !npmError" class="empty-state">
          <p>当前没有配置可由 Hanxi 管理的 npm 全局工具。</p>
        </div>
      </section>
    </div>

    <!-- 历史记录（Q2 口径：本模块只记 npm 装升卸动作终态，版本查询不入库；
         无自由输入框故关"应用"，回填语义=定位对应工具卡自行查看） -->
    <div
      id="envcheck-history-panel"
      v-show="activeMainTab === 'history'"
      class="tab-body"
      role="tabpanel"
      aria-labelledby="envcheck-history-tab"
    >
      <HistoryPanel func-type="envcheck" :show-apply="false" />
    </div>
  </section>
</template>

<style scoped>
.env-view { display: flex; flex-direction: column; gap: 14px; }
/* 页头/选项卡/错误横幅/空态/.btn 基础与 primary/small/.chip 家族/焦点环/减弱动效
   均由 PageHeader、MainTabNav 与 components.css + base.css 全局承载 */
.status-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 11px 14px; border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-soft); }
.status-summary { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.status-summary strong { color: var(--color-text); font-size: var(--text-base); font-variant-numeric: tabular-nums; }
.status-summary span { color: var(--color-text-muted); font-size: var(--text-xs); line-height: 1.45; }
.refresh-button { min-width: 96px; flex: 0 0 auto; }
.env-actions { display: inline-flex; align-items: center; gap: 8px; flex: 0 0 auto; }
.copy-report-button { flex: 0 0 auto; }
.tab-body { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
.tool-grid, .management-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr)); gap: 12px; }
.compact-grid { grid-template-columns: repeat(auto-fit, minmax(min(340px, 100%), 1fr)); }
.tool-card, .management-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.usage-panel { margin-top: 12px; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
.usage-head { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.usage-head .btn { margin-left: auto; }
.usage-title { font-weight: 700; }
.usage-sub { color: var(--color-text-muted); font-size: var(--text-sm); }
.usage-tool { display: flex; flex-direction: column; gap: 4px; }
.usage-tool-name { font-weight: 600; font-size: var(--text-sm); color: var(--color-text-subtle); }
.usage-line { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
.usage-line.usage-offline { opacity: 0.6; }
.usage-kind { flex: 0 0 auto; font-size: var(--text-xs); font-weight: 700; padding: 1px 6px; border-radius: var(--radius-pill); border: 1px solid var(--color-border); }
.usage-kind.kind-install { color: var(--state-positive); }
.usage-kind.kind-cache { color: var(--state-warning); }
.usage-label { flex: 0 0 auto; font-size: var(--text-sm); }
.usage-path { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-subtle); font-size: var(--text-xs); }
.usage-size { flex: 0 0 auto; font-variant-numeric: tabular-nums; font-size: var(--text-sm); color: var(--color-text-muted); }
.usage-size.usage-partial { color: var(--state-warning); }
.usage-muted { flex: 0 0 auto; font-size: var(--text-xs); color: var(--color-text-subtle); }
.tool-card { border-left: 3px solid var(--color-border); transition: opacity var(--motion-base) ease; }
.tool-card.local-refreshing { opacity: 0.68; }
.tool-card.status-installed { border-left-color: var(--state-positive); }
.tool-card.status-missing { border-left-color: var(--color-text-subtle); }
.tool-card.status-error { border-left-color: var(--state-danger); }
.tool-card.status-store-stub { border-left-color: var(--state-warning); }
.version-workspace { gap: 18px; }
.workspace-section { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.workspace-section + .workspace-section { padding-top: 17px; border-top: 1px solid var(--color-border); }
.section-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.section-heading h2 { margin: 0; color: var(--color-text); font-size: var(--text-md); line-height: 1.3; }
.section-heading p { margin: 3px 0 0; max-width: 760px; color: var(--color-text-muted); font-size: var(--text-sm); line-height: 1.5; }
.section-banner { margin: 0; }
.local-summary { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; min-width: 0; }
.local-summary > div { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; min-width: 0; }
.local-summary > code { flex: 0 1 auto; color: var(--color-text-muted); text-align: right; }
.tool-card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.tool-name { font-size: var(--text-md); font-weight: 700; color: var(--color-text); }
/* 本视图 chip 仅调图标间距；底色/形状走全局 .chip-{tone} */
.status-chip { gap: 5px; }
/* .inst-meta 与托管家族上收的全局原子逐字等值，副本删净落回 */
/* display/gap/色/基线落回全局 .meta-line；此处仅留收缩补差 */
.meta-line { min-width: 0; }
/* 色与收缩落回全局 .meta-line .k；仅标签列宽散差（36 vs 全局 44）暂留本地——定档候选，见收编报告 */
.meta-line .k { width: 36px; }
/* 原 .mono 同名 scoped 副本（11px + overflow-wrap）已删净：落回全局 .mono（--text-sm +
   word-break: break-all），字号差一档为设计归一（登记 §9.6 式微差，真机目视项） */
.tool-path { min-width: 0; }
.path-link { display: block; padding: 0; border: 0; background: none; text-align: left; cursor: pointer; font: inherit; overflow-wrap: anywhere; text-decoration: underline; text-decoration-color: var(--color-border); text-underline-offset: 2px; }
.path-link:hover { text-decoration-color: var(--color-primary); color: var(--color-primary); }
/* 行内复制钮：卡面静默、悬停/聚焦浮现（与 OcrView 逐行复制同交互族） */
.meta-copy { opacity: 0; flex-shrink: 0; transition: opacity var(--motion-base) ease; }
.tool-card:hover .meta-copy, .tool-card:focus-within .meta-copy { opacity: 1; }
.tool-hint { font-size: var(--text-sm); border-radius: 5px; padding: 6px 8px; line-height: 1.5; }
.tool-details { display: flex; flex-direction: column; gap: 2px; color: var(--color-text-muted); font-size: var(--text-xs); line-height: 1.45; }
.hint-warn { background: var(--state-warning-soft); color: var(--state-warning); }
.hint-error { background: var(--state-danger-soft); color: var(--state-danger); }
/* .btn-accent-outline 等值副本删净落回全局 :where(.btn-accent-outline)（含 hover 行，2026-09 治理裁决） */
.tool-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.extra-lines { margin-left: auto; flex-shrink: 0; padding: 1px 7px; border-radius: var(--radius-pill); background: var(--state-information-soft); color: var(--state-information); font-size: var(--text-micro); }
@media (max-width: 768px) {
  .status-toolbar { align-items: stretch; flex-direction: column; gap: 10px; }
  .refresh-button { width: 100%; }
  .local-summary { align-items: flex-start; flex-direction: column; }
  .local-summary > code { text-align: left; }
}
</style>
