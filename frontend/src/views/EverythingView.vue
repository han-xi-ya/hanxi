<script setup lang="ts">
// 编排层：托管状态 / 版本数据与操作 / 事件订阅与轮询生命周期（骨架基于重构共享层）。
// 内嵌搜索与 ES 组件就绪收编 useEverythingSearch、结果表列宽记忆收编 useEverythingColumns，
// 控制台整合条 / 结果区 / 版本卡片 / 远程表格拆至 components/everything/* 子组件；
// DOM 结构、文案与 bindings 调用契约逐字保持，业务动作仍由本视图编排接线。
import { ref, computed, onMounted, onDeactivated } from 'vue'
import * as EverythingAPI from '../../bindings/hanxi/internal/modules/everything/everythingservice'
import type { EverythingRelease, EverythingVersionInfo } from '../../bindings/hanxi/internal/modules/everything/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/everything/instance/models'
import type { DownloadTicket } from '../../bindings/hanxi/internal/modules/everything/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useEverythingSearch } from '../composables/useEverythingSearch'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'
import EverythingConsoleBar from '../components/everything/EverythingConsoleBar.vue'
import EverythingResultsPanel from '../components/everything/EverythingResultsPanel.vue'
import EverythingVersionCard from '../components/everything/EverythingVersionCard.vue'
import EverythingReleaseTable from '../components/everything/EverythingReleaseTable.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<EverythingRelease[]>([])
const installed = ref<EverythingVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const uptimeSec = ref(0)

const { busy, run } = useAsyncAction()
const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()

// 内嵌搜索 + ES 组件就绪（逻辑收编 useEverythingSearch，视图只做 props/emits 接线）
const {
  keyword, searched, results, searching, searchError, truncated,
  esReady, esBusy, esProgress, composing,
  ensureTool, onKeywordInput, onKeywordEnter, onCompositionEnd, doSearch,
  openResult, revealResult, copyResult, handleEsTicket,
} = useEverythingSearch(busy)

// 下载进度 map（component=app 按版本索引；es 组件走 useEverythingSearch 的 esProgress）
const downloading = ref<Record<string, DownloadTicket>>({})

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron 同构）
const activeMainTab = ref('console')
const mainTabs = [
  { key: 'console', label: '🔎 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')

// §9.5-5 文案评审结论：通用五态接单一来源（running=「运行中」）；
// Everything 的 running 按托管模式细分「后台/窗口运行中」，属业务扩展话术，保留局部覆写。
const stateText = computed(() => {
  switch (state.value) {
    case 'running':
      return snap.value?.mode === 'background' ? '后台运行中' : '窗口运行中'
    case 'starting': return '启动中…'
    case 'failed': return '异常退出'
    case 'external': return '外部运行'
    default: return '未运行'
  }
})

const runningVersion = computed(() => snap.value?.version ?? '')

// 条件提示条（三个变体互斥）
const banner = computed<{ tone: 'ok' | 'warn' | 'error'; text: string } | null>(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部 Everything 实例（非 Hanxi 托管）。可唤起其搜索窗口；如需彻底退出请在 Everything 托盘操作。内嵌搜索对默认实例有效。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'Everything 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: snap.value?.mode === 'background'
        ? 'Everything 在后台驻留建索引：搜索窗口秒开、内嵌搜索可用。空闲 3 分钟将自动退出，再次搜索自动重启。'
        : 'Everything 正在运行。关闭搜索窗口不会退出实例（继续后台驻留）；空闲 3 分钟自动退出。',
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
      EverythingAPI.ListReleases(),
      EverythingAPI.ListInstalledVersions(),
      EverythingAPI.GetActiveVersion(),
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
    snap.value = await EverythingAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('everything GetStatus failed:', getErrorMessage(e))
  }
}

// ---------- 控制操作 ----------
async function startBackground() {
  if (busy.value) return
  const r = await run(() => EverythingAPI.StartBackground())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await refreshStatus()
}

async function openWindow() {
  if (busy.value) return
  const r = await run(() => EverythingAPI.OpenWindow())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await refreshStatus()
}

async function quitEverything() {
  if (busy.value) return
  const r = await run(() => EverythingAPI.Quit())
  showToast(r.ok ? r.data.message : `退出失败: ${getErrorMessage(r.error)}`)
  await refreshStatus()
}

// ---------- 版本管理操作 ----------
async function download(rel: EverythingRelease) {
  try {
    const res = await EverythingAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: EverythingVersionInfo) {
  try {
    const ver = await EverythingAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

// 打开安装目录：必须传「目录」路径——explorer.exe 收文件参数会执行文件（markeron 教训），
// 此处统一走本模块 OpenTarget（目录语义 = 资源管理器打开目录）
async function openDir(path: string) {
  try {
    await EverythingAPI.OpenTarget(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: EverythingVersionInfo) {
  const ok = await confirm({
    title: `卸载 Everything ${v.version}`,
    description: '该版本隔离目录及其配置与索引库将被删除，不可恢复。',
    tone: 'danger',
    confirmLabel: '卸载',
  })
  if (!ok) return
  try {
    await EverythingAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地安装',
    label: 'Everything 安装目录完整路径',
    description: '将整套迁移 exe/配置/语言包/索引库',
  })
  if (!path) return
  const r = await run(() => EverythingAPI.ImportLocal(path.trim()))
  if (r.ok) {
    showToast(`已导入 Everything ${r.data.version}（含配置与索引库，无需重建索引）`)
    await loadVersions()
  } else {
    showToast(`导入失败: ${getErrorMessage(r.error)}`)
  }
}

// ---------- 时长 ticker 与轮询（usePolling 内置 KeepAlive 激活/停用契约） ----------
usePolling(refreshStatus, 2500) // 状态兜底轮询（事件推送之外）
usePolling(() => {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}, 1000)

// 停用即清零运行时长（对齐迁移前 stopTimers 语义）
onDeactivated(() => {
  uptimeSec.value = 0
})

// ---------- 事件订阅（自动注销）与装载 ----------
// 单订阅双分发：es 组件进度交 useEverythingSearch（handleEsTicket），app 版本进度进 downloading map
useWailsEvent<DownloadTicket>('everything:download', (t) => {
  if (!t || !t.component) return
  if (t.component === 'es') {
    handleEsTicket(t)
    return
  }
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

useWailsEvent<Snapshot>('everything:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

onMounted(async () => {
  await Promise.all([refreshStatus(), loadVersions(), ensureTool()])
})
</script>

<template>
  <section class="page everything-view">
    <PageHeader
      title="Everything 搜索"
      subtitle="托管 Everything 后台索引与搜索窗口；Hanxi 内直接秒搜文件。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <!-- 顶部整合条：状态 + 启停按钮 + 搜索框，一行内解决问题 -->
      <EverythingConsoleBar
        :state="state"
        :state-text="stateText"
        :busy="busy"
        :running-version="runningVersion"
        :pid="snap?.pid"
        :uptime-sec="uptimeSec"
        :searching="searching"
        :es-ready="esReady"
        :es-busy="esBusy"
        :es-progress="esProgress"
        :keyword="keyword"
        @update:keyword="keyword = $event"
        @keyword-input="onKeywordInput"
        @keyword-enter="onKeywordEnter"
        @composition-start="composing = true"
        @composition-end="onCompositionEnd"
        @search="doSearch"
        @install-es="ensureTool()"
        @start-background="startBackground"
        @open-window="openWindow"
        @quit="quitEverything"
      />

      <!-- 条件提示条 / 引导行 -->
      <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：上方直接输入关键词即会搜索（首次自动拉起后台实例，约 1~3 秒）；「启动后台」常驻索引、「打开窗口」直接现搜。空闲 3 分钟自动退出。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Everything 实例（约 1~3 秒）…</div>

      <div v-if="searchError" class="error-box slim">{{ searchError }}</div>

      <!-- 结果区 -->
      <EverythingResultsPanel
        :results="results"
        :searched="searched"
        :truncated="truncated"
        :searching="searching"
        @open="openResult"
        @reveal="revealResult"
        @copy="copyResult"
      />
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程槽位 {{ releases.length }} 个（稳定 + 1.5 测试）</span>
          <span class="hint-dim">便携包下载自官网 voidtools（官方 sha256 校验）；或导入本机已有安装连配置与索引库一起收纳</span>
          <span class="hint-dim">托管启动会自动隐藏 Everything 托盘图标；注意：手动直接运行版本目录里的 exe 将没有托盘，退出需用任务管理器</span>
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

      <UiEmptyState v-if="installed.length === 0">
        <p>尚未安装 Everything —— 下载官方便携版，或「导入本地安装」把现有配置与索引库整套搬进来（免重建索引）</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
          下载 {{ releases[0].channel === 'stable' ? '稳定版' : '最新版' }} {{ releases[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </UiEmptyState>

      <div class="installed-grid">
        <EverythingVersionCard
          v-for="v in installed"
          :key="v.version"
          :info="v"
          :is-active="activeVersion === v.version"
          :is-running="state === 'running' && runningVersion === v.version"
          @set-active="setActive"
          @open-dir="openDir($event.dir)"
          @remove="removeVersion"
        />
      </div>

      <!-- 远程可用槽位 -->
      <div class="section-title"><h3>远程可用版本</h3></div>
      <EverythingReleaseTable
        :releases="releases"
        :installed="installed"
        :downloading="downloading"
        :loading="loading"
        @download="download"
      />
    </div>
  </section>
</template>

<style scoped>
/* 共享层已接管：.page/.header-row/.subtitle/.error-box/.btn 家族/.tbl/.mono/.link-button/
   .empty-state/.banner(UiBanner)/.main-tab-nav(MainTabNav)；搜索控制台/结果表/版本卡片/
   远程表格的业务样式随 DOM 迁入 components/everything/* 子组件 scoped——此处只留编排层骨架样式。 */
.everything-view { display: flex; flex-direction: column; gap: 14px; }
.tab-body { display: flex; flex-direction: column; gap: 16px; }
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }
.error-box.slim { padding: 8px 12px; font-size: 12px; }

/* ---------- 版本区（业务壳） ---------- */
.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }
.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
</style>
