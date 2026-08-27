<script setup lang="ts">
// 状态 / 内嵌搜索 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, reactive, computed, onMounted, onUnmounted, onActivated, onDeactivated } from 'vue'
import { Events } from '@wailsio/runtime'
import * as EverythingAPI from '../../bindings/hubkit/internal/modules/everything/everythingservice'
import type { EverythingRelease, EverythingVersionInfo } from '../../bindings/hubkit/internal/modules/everything/version/models'
import type { Snapshot } from '../../bindings/hubkit/internal/modules/everything/instance/models'
import type { ControlOutcome, QuitOutcome, DownloadTicket } from '../../bindings/hubkit/internal/modules/everything/models'
import type { Result } from '../../bindings/hubkit/internal/modules/everything/search/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<EverythingRelease[]>([])
const installed = ref<EverythingVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 内嵌搜索状态
const keyword = ref('')
const searched = ref('')
const results = ref<Result[]>([])
const searching = ref(false)
const searchError = ref('')
const truncated = ref(false)
const esReady = ref(false)
const esBusy = ref(false)
const esProgress = ref<DownloadTicket | null>(null)

// ---------- 结果表列宽（拖拽调整 + localStorage 记忆） ----------
type ColKey = 'name' | 'path' | 'size' | 'time' | 'action'
const DEFAULT_COLS: Record<ColKey, number> = { name: 320, path: 420, size: 70, time: 130, action: 118 }
const COL_STORAGE_KEY = 'hubkit-everything-result-cols'
const colWidths = reactive<Record<ColKey, number>>({ ...DEFAULT_COLS })
const totalColWidth = computed(() => Object.values(colWidths).reduce((a, b) => a + b, 0))

let saveColTimer: number | null = null
let dragCleanup: (() => void) | null = null

function loadColWidths() {
  try {
    const raw = localStorage.getItem(COL_STORAGE_KEY)
    if (!raw) return
    const saved = JSON.parse(raw) as Partial<Record<ColKey, number>>
    for (const k of Object.keys(DEFAULT_COLS) as ColKey[]) {
      const v = saved[k]
      if (typeof v === 'number' && v >= 60 && v <= 900) colWidths[k] = v
    }
  } catch {
    // 存储损坏/不可用：静默回退默认值
  }
}

function saveColWidths() {
  if (saveColTimer) clearTimeout(saveColTimer)
  saveColTimer = window.setTimeout(() => {
    try { localStorage.setItem(COL_STORAGE_KEY, JSON.stringify(colWidths)) } catch { /* 忽略写失败 */ }
  }, 300)
}

// 表头拖拽调宽：mousedown 挂 document 级 move/up，松手防抖落盘
function startResize(key: ColKey, e: MouseEvent) {
  e.preventDefault()
  const startX = e.clientX
  const startW = colWidths[key]
  const onMove = (ev: MouseEvent) => {
    colWidths[key] = Math.min(900, Math.max(60, Math.round(startW + (ev.clientX - startX))))
  }
  const onUp = () => {
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
    dragCleanup = null
    saveColWidths()
  }
  dragCleanup = onUp
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}

// 下载进度 map（component=app 按版本索引；es 组件单独 esProgress）
const downloading = ref<Record<string, DownloadTicket>>({})

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron 同构）
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
    case 'running':
      return snap.value?.mode === 'background' ? '后台运行中' : '窗口运行中'
    case 'starting': return '启动中…'
    case 'failed': return '异常退出'
    case 'external': return '外部运行'
    default: return '未运行'
  }
})

const runningVersion = computed(() => snap.value?.version ?? '')

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = state.value === 'running' && runningVersion.value ? runningVersion.value : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

// 条件提示条（三个变体互斥）
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      cls: 'banner-warn',
      text: '检测到外部 Everything 实例（非 HubKit 托管）。可唤起其搜索窗口；如需彻底退出请在 Everything 托盘操作。内嵌搜索对默认实例有效。',
    }
  }
  if (state.value === 'failed') {
    return { cls: 'banner-error', text: snap.value?.error || 'Everything 异常退出' }
  }
  if (state.value === 'running') {
    return {
      cls: 'banner-ok',
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

async function ensureTool(): Promise<boolean> {
  if (esReady.value) return true
  esBusy.value = true
  try {
    await EverythingAPI.EnsureSearchTool()
    esReady.value = true
    return true
  } catch (e) {
    showToast(`搜索组件就绪失败: ${getErrorMessage(e)}`)
    return false
  } finally {
    esBusy.value = false
  }
}

function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(0)} KB`
}

function fmtDate(s?: string): string {
  if (!s) return '—'
  return s.slice(0, 10)
}

function fmtDuration(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}

function stepOf(p: DownloadTicket): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: EverythingRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

function channelLabel(channel: string): string {
  if (channel === 'stable') return '稳定'
  if (channel === 'beta') return '1.5 测试'
  return channel || '其他'
}

function resultFullPath(r: Result): string {
  const sep = '\\'
  if (!r.path) return r.name
  return r.path.endsWith(sep) ? r.path + r.name : r.path + sep + r.name
}

// ---------- 控制操作 ----------
async function startBackground() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await EverythingAPI.StartBackground()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function openWindow() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await EverythingAPI.OpenWindow()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitEverything() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await EverythingAPI.Quit()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`退出失败: ${getErrorMessage(e)}`)
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

// ---------- 内嵌搜索 ----------
// 实时搜索：输入停顿 350ms 自动触发；中文输入法组合期间不触发（composition 守卫）
let debounceTimer: number | null = null
let searchSeq = 0 // 过期响应丢弃：最新一次搜索的序号才允许落盘结果
const composing = ref(false)

function onKeywordInput() {
  if (composing.value) return
  const q = keyword.value.trim()
  if (!q) {
    if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null }
    results.value = []
    searched.value = ''
    searchError.value = ''
    truncated.value = false
    return
  }
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = window.setTimeout(() => { doSearch() }, 350)
}

function onKeywordEnter() {
  if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null }
  doSearch()
}

function onCompositionEnd() {
  composing.value = false
  onKeywordInput() // 中文上屏后补触发一次
}

async function doSearch() {
  const q = keyword.value.trim()
  if (!q || searching.value || busy.value) return
  const seq = ++searchSeq
  searching.value = true
  searchError.value = ''
  truncated.value = false
  try {
    if (!(await ensureTool())) return // 组件缺失且安装失败时给出 toast 后中断（由 finally 复位状态）
    const list = await EverythingAPI.Search(q, 300)
    if (seq !== searchSeq) return // 已有更新的关键词在途，丢弃过期响应
    results.value = list ?? []
    searched.value = q
    truncated.value = results.value.length >= 300
    if (results.value.length === 0) showToast(`「${q}」无匹配结果`)
  } catch (e) {
    if (seq !== searchSeq) return
    searchError.value = getErrorMessage(e)
  } finally {
    if (seq === searchSeq) searching.value = false
  }
}

async function openResult(r: Result) {
  try {
    await EverythingAPI.OpenTarget(resultFullPath(r))
  } catch (e) {
    showToast(`打开失败: ${getErrorMessage(e)}`)
  }
}

async function revealResult(r: Result) {
  try {
    await EverythingAPI.RevealTarget(resultFullPath(r))
  } catch (e) {
    showToast(`定位失败: ${getErrorMessage(e)}`)
  }
}

// 点击名称/路径单元格 = 复制完整路径（clipboard API 失败时退回 execCommand 兼容路径）
async function copyResult(r: Result) {
  const text = resultFullPath(r)
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
    showToast(`复制失败: ${getErrorMessage(e)}`)
    return
  }
  showToast('已复制完整路径')
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
  if (!window.confirm(`确定卸载 Everything ${v.version}？\n该版本隔离目录及其配置与索引库将被删除，不可恢复。`)) return
  try {
    await EverythingAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = window.prompt('请输入 Everything 安装目录完整路径（将整套迁移 exe/配置/语言包/索引库）：')
  if (!path) return
  try {
    busy.value = true
    const info = await EverythingAPI.ImportLocal(path.trim())
    showToast(`已导入 Everything ${info.version}（含配置与索引库，无需重建索引）`)
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

// ---------- 生命周期 ----------
onMounted(async () => {
  loadColWidths()
  unlistenDownload = Events.On('everything:download', (event: any) => {
    const t = event.data as DownloadTicket
    if (!t || !t.component) return
    if (t.component === 'es') {
      esProgress.value = t
      if (t.stage === 'done') {
        esReady.value = true
        setTimeout(() => { esProgress.value = null }, 800)
      }
      if (t.stage === 'error') esProgress.value = null
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

  unlistenState = Events.On('everything:instance-state', (event: any) => {
    const s = event.data as Snapshot
    if (!s) return
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
  })

  await Promise.all([refreshStatus(), loadVersions(), ensureTool()])
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
  if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null }
  if (saveColTimer) { clearTimeout(saveColTimer); saveColTimer = null }
  if (dragCleanup) dragCleanup() // 拖拽中途卸载：移除 document 级监听，防句柄泄漏
  if (unlistenDownload) unlistenDownload()
  if (unlistenState) unlistenState()
})
</script>

<template>
  <section class="page everything-view">
    <div class="header-row">
      <div>
        <h1>Everything 搜索</h1>
        <p class="subtitle">托管 Everything 后台索引与搜索窗口；HubKit 内直接秒搜文件。</p>
      </div>
      <div class="main-tab-nav">
        <button
          class="main-tab-btn"
          :class="{ active: activeMainTab === 'console' }"
          @click="activeMainTab = 'console'"
        >
          🔎 控制台
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
    <!-- 顶部整合条：状态 + 启停按钮 + 搜索框，一行内解决问题 -->
    <div class="control-bar">
      <div class="control-top">
        <div class="control-status">
          <span class="ev-status-light" :class="state"></span>
          <span class="status-word">{{ stateText }}</span>
          <template v-if="isRunningOrStarting && runningVersion">
            <span class="ver-pill">{{ runningVersion }}</span>
            <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
          </template>
          <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
        </div>
        <div class="control-btns">
          <button
            class="btn btn-primary btn-small"
            :disabled="busy || state === 'running' || state === 'starting' || isExternal"
            :title="state === 'running' ? '已在运行' : isExternal ? '外部实例已在运行' : '后台驻留建索引（不弹窗）'"
            @click="startBackground"
          >▶ 启动后台</button>
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || state === 'starting'"
            :title="state === 'running' ? '唤起搜索窗口' : state === 'starting' ? '启动中…' : '直接启动并打开搜索窗口'"
            @click="openWindow"
          >🗔 打开窗口</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 Everything 托盘退出' : '优雅落盘索引库后退出'"
            @click="quitEverything"
          >⏻ 退出</button>
        </div>
      </div>

      <div class="search-line">
        <input
          v-model="keyword"
          class="search-input"
          type="text"
          placeholder="输入即搜 —— 支持 Everything 语法（如 *.go | D:\项目）"
          @input="onKeywordInput"
          @keyup.enter="onKeywordEnter"
          @compositionstart="composing = true"
          @compositionend="onCompositionEnd"
        />
        <button class="btn btn-primary btn-small search-go" :disabled="searching || busy" @click="doSearch">
          {{ searching ? '搜索中…' : '⇥ 搜索' }}
        </button>
        <span class="ev-status-pill" :class="esReady ? 'ready' : 'idle'">
          {{ esProgress ? '组件安装中…' : esReady ? 'ES 就绪' : 'ES 未装' }}
        </span>
        <button
          v-if="!esReady && !esProgress"
          class="link-btn"
          :disabled="esBusy"
          @click="ensureTool()"
        >安装</button>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <div v-if="banner" class="hint-banner slim" :class="banner.cls">{{ banner.text }}</div>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未运行：上方直接输入关键词即会搜索（首次自动拉起后台实例，约 1~3 秒）；「启动后台」常驻索引、「打开窗口」直接现搜。空闲 3 分钟自动退出。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Everything 实例（约 1~3 秒）…</div>

    <div v-if="searchError" class="search-error">{{ searchError }}</div>

    <!-- 结果区 -->
    <div class="results-wrap">
      <div v-if="results.length" class="results-meta">
        共 {{ results.length }} 条结果（关键词「{{ searched }}」）
        <span v-if="truncated" class="warn-text">已达单次 300 条上限——请细化关键词</span>
        <span class="hint-dim">点击名称/路径单元格复制完整路径</span>
      </div>
      <div class="result-scroll" v-if="results.length">
        <table class="tbl result-tbl resizable" :style="{ minWidth: totalColWidth + 'px' }">
          <thead>
            <tr>
              <th :style="{ width: colWidths.name + 'px' }">名称<span class="col-resizer" @mousedown="startResize('name', $event)"></span></th>
              <th :style="{ width: colWidths.path + 'px' }">路径<span class="col-resizer" @mousedown="startResize('path', $event)"></span></th>
              <th :style="{ width: colWidths.size + 'px' }">大小<span class="col-resizer" @mousedown="startResize('size', $event)"></span></th>
              <th :style="{ width: colWidths.time + 'px' }">修改时间<span class="col-resizer" @mousedown="startResize('time', $event)"></span></th>
              <th :style="{ width: colWidths.action + 'px' }">操作<span class="col-resizer" @mousedown="startResize('action', $event)"></span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in results" :key="i">
              <!-- table-layout:fixed 的列宽以首行单元格为准；首行与表头同步绑定，保证列对齐 -->
              <td
                class="result-name copyable"
                :class="{ 'is-dir': r.isDir }"
                :style="i === 0 ? { width: colWidths.name + 'px' } : undefined"
                :title="`点击复制：${resultFullPath(r)}`"
                @click="copyResult(r)"
              >
                {{ r.isDir ? '📁' : '📄' }} {{ r.name }}
              </td>
              <td
                class="mono result-path copyable"
                :style="i === 0 ? { width: colWidths.path + 'px' } : undefined"
                :title="`点击复制：${resultFullPath(r)}`"
                @click="copyResult(r)"
              >{{ r.path }}</td>
              <td :style="i === 0 ? { width: colWidths.size + 'px' } : undefined">{{ r.isDir ? '—' : fmtSize(r.size) }}</td>
              <td class="result-time" :style="i === 0 ? { width: colWidths.time + 'px' } : undefined">{{ r.modified }}</td>
              <td :style="i === 0 ? { width: colWidths.action + 'px' } : undefined">
                <div class="row-actions">
                  <button class="link-btn" @click="openResult(r)">打开</button>
                  <button class="link-btn" @click="revealResult(r)">定位</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else-if="searched && !searching" class="empty-hint">「{{ searched }}」无匹配结果</div>
      <div v-else-if="searching" class="empty-hint">搜索中…（首次搜索会自动启动后台实例）</div>
      <div v-else class="empty-hint idle-hint">输入即搜；结果可打开 / 在资源管理器中定位</div>
    </div>
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

    <div v-if="installed.length === 0" class="empty-state first-use">
      <p>尚未安装 Everything —— 下载官方便携版，或「导入本地安装」把现有配置与索引库整套搬进来（免重建索引）</p>
      <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
        下载 {{ releases[0].channel === 'stable' ? '稳定版' : '最新版' }} {{ releases[0].version }}
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
            :title="state === 'running' && runningVersion === v.version ? '请先退出 Everything' : ''"
            @click="removeVersion(v)"
          >卸载</button>
        </div>
      </div>
    </div>

    <!-- 远程可用槽位 -->
    <div class="section-title"><h3>远程可用版本</h3></div>
    <div class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 90px;">通道</th>
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
              <span class="channel-badge" :class="{ 'ch-stable': rel.channel === 'stable', 'ch-beta': rel.channel !== 'stable' }">
                {{ channelLabel(rel.channel) }}
              </span>
            </td>
            <td>
              <strong class="ver-name">{{ rel.version }}</strong>
              <span v-if="rel.stale" class="badge badge-pre">快照</span>
            </td>
            <td>
              <!-- 类名刻意用 ev-status（含 ev- 前缀）——App.vue 全局样式有 .status-dot（7px），防碰撞压扁徽标 -->
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
                <span v-if="['verify', 'extract'].includes(downloading[rel.version]!.stage)">校验解压安装…</span>
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
            <td colspan="6" class="empty-hint">无法加载远程版本列表（官网不可达），已尝试内置快照——可稍后点击「↻ 刷新远程列表」重试</td>
          </tr>
        </tbody>
      </table>
    </div>
    </div>
  </section>
</template>

<style scoped>
.everything-view { display: flex; flex-direction: column; gap: 14px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 顶层主选项卡（与 FrpcProjectsView 同款） */
.main-tab-nav {
  display: flex;
  background: var(--bg-hover);
  padding: 3px;
  border-radius: 8px;
  gap: 2px;
}
.main-tab-btn {
  background: transparent;
  border: none;
  padding: 6px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}
.main-tab-btn:hover { color: var(--text-main); }
.main-tab-btn.active {
  background: var(--bg-app);
  color: var(--accent);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}
.tab-body { display: flex; flex-direction: column; gap: 16px; }

/* ---------- 顶部整合控制条（状态 + 按钮 + 搜索框，紧凑单卡） ---------- */
.control-bar {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 注意：状态卡信号灯类名带 ev- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.ev-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--text-subtle); flex-shrink: 0; }
.ev-status-light.running { background: var(--success); box-shadow: 0 0 0 3px rgba(26, 127, 55, 0.15); }
.ev-status-light.starting { background: var(--accent); animation: pulse 1s infinite; }
.ev-status-light.external { background: #9a6700; box-shadow: 0 0 0 3px rgba(154, 103, 0, 0.15); }
.ev-status-light.failed { background: var(--danger); box-shadow: 0 0 0 3px rgba(207, 34, 46, 0.15); }
.status-word { font-size: 15px; font-weight: 700; color: var(--text-main); }
.ver-pill { font-family: Consolas, monospace; font-size: 12px; background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 4px; padding: 1px 8px; color: var(--text-main); }
.pid-tag { font-size: 11px; color: var(--text-subtle); }
.uptime-tag { font-size: 11px; color: var(--text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示条 ---------- */
.hint-banner { padding: 10px 14px; border-radius: 6px; font-size: 13px; border: 1px solid transparent; }
.hint-banner.slim { padding: 8px 12px; font-size: 12px; }
.banner-warn { background: #fff8c5; border-color: rgba(191, 135, 0, 0.3); color: #9a6700; }
.banner-error { background: #ffebe9; border-color: rgba(207, 34, 46, 0.25); color: var(--danger); }
.banner-ok { background: #dafbe1; border-color: rgba(26, 127, 55, 0.2); color: #1a7f37; }

/* ---------- 搜索行与提示 ---------- */
.search-line { display: flex; gap: 8px; align-items: center; }
.search-go { flex-shrink: 0; }
.ev-status-pill { font-size: 11px; padding: 2px 8px; border-radius: 12px; white-space: nowrap; }
.ev-status-pill.ready { background: #dafbe1; color: #1a7f37; }
.ev-status-pill.idle { background: #eaeef2; color: #656d76; }
.hint-line { font-size: 12px; color: var(--text-subtle); padding-left: 2px; }
.copyable { cursor: pointer; }
.copyable:hover { background: var(--bg-hover); }
.search-input {
  flex: 1; padding: 8px 12px; border: 1px solid var(--border-color); border-radius: 6px;
  font-size: 13px; background: var(--bg-app); color: var(--text-main); outline: none;
}
.search-input:focus { border-color: var(--accent); box-shadow: 0 0 0 2px rgba(47, 111, 237, 0.12); }
.search-error { padding: 8px 12px; background: #ffebe9; color: var(--danger); border-radius: 6px; font-size: 12px; }

.results-wrap { display: flex; flex-direction: column; gap: 8px; }
.results-meta { font-size: 12px; color: var(--text-muted); display: flex; gap: 12px; align-items: center; }
.warn-text { color: #9a6700; }
.result-scroll { overflow-x: auto; }
.result-tbl { max-height: 428px; display: block; overflow-y: auto; }
.result-tbl thead, .result-tbl tbody { display: table; width: 100%; table-layout: fixed; }
.resizable th { position: relative; }
.col-resizer {
  position: absolute; top: 0; right: 0; width: 6px; height: 100%;
  cursor: col-resize; transition: background 0.1s ease;
}
.col-resizer:hover { background: rgba(47, 111, 237, 0.35); }
.result-tbl td { font-size: 12px; }
.result-name { font-weight: 500; color: var(--text-main); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.result-name.is-dir { color: var(--accent); }
.result-path { font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; color: var(--text-subtle); }
.result-time { font-size: 11px; color: var(--text-subtle); white-space: nowrap; }
.row-actions { display: flex; gap: 4px; }
.link-btn { background: none; border: none; color: var(--accent); font-size: 12px; cursor: pointer; padding: 2px 4px; border-radius: 4px; }
.link-btn:hover:not(:disabled) { background: var(--bg-active); text-decoration: underline; }
.link-btn:disabled { opacity: 0.5; cursor: not-allowed; }

/* ---------- 通用按钮 ---------- */
.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn-ghost { background: #f0f7ff; color: #0969da; border-color: #c8e1ff; }
.btn-danger-outline { background: #fff; border-color: #ff8170; color: var(--danger); }
.btn-danger-outline:hover:not(:disabled) { background: #ffebe9; }

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--bg-sidebar); border: 1px solid var(--border-color); padding: 10px 14px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--text-main); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--text-subtle); font-size: 13px; background: var(--bg-sidebar); border-radius: 6px; border: 1px dashed var(--border-color); }
.idle-hint { color: var(--text-subtle); }

/* 首次使用空态 */
.empty-state {
  text-align: center; padding: 24px; background: var(--bg-sidebar);
  border: 1px dashed var(--border-color); border-radius: 8px;
  display: flex; flex-direction: column; gap: 12px; align-items: center;
}
.empty-state p { margin: 0; color: var(--text-muted); font-size: 13px; }

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color 0.15s ease;
}
.installed-card.card-active { border-color: var(--accent); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: Consolas, monospace; font-size: 14px; font-weight: 700; color: var(--text-main); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
.badge-active { background: #dafbe1; color: #1a7f37; }
.badge-running { background: #ddf4ff; color: #0969da; }
.badge-import { background: #ddf4ff; color: #0969da; }
.badge-official { background: #eaeef2; color: #656d76; }
.badge-pre { background: #fff8c5; color: #9a6700; margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--text-muted); align-items: baseline; }
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

.channel-badge { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 12px; }
.ch-stable { background: #dafbe1; color: #1a7f37; }
.ch-beta { background: #fff8c5; color: #9a6700; }

.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: #2da44e; }
.ver-status.downloading::before { background: #0969da; animation: pulse 1s infinite; }
.ver-status.error::before { background: #cf222e; }
.ver-status.idle::before { background: #8c959f; }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: #e1e4e8; border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--accent); }
.dl-error { color: var(--danger); font-size: 11px; }
.retry-link { color: var(--accent); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>