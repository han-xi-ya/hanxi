<script setup lang="ts">
import { computed, onActivated, onMounted, ref } from 'vue'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/snipaste/instance/models'
import type { SnipasteRelease, SnipasteVersionInfo } from '../../bindings/hanxi/internal/modules/snipaste/version/models'
import { createSnipasteAdapter } from '../adapters/snipaste'
import { useSnipasteDownloadTickets, type SnipasteDownloadTicket } from '../composables/useSnipasteDownloadTickets'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import type { ManagedConsoleStore } from '../components/managed/store'
import SnipasteControlPanel from '../components/snipaste/SnipasteControlPanel.vue'
import SnipasteVersionsPanel from '../components/snipaste/SnipasteVersionsPanel.vue'

const { showToast } = useToast()
const installed = ref<SnipasteVersionInfo[]>([])
const releases = ref<SnipasteRelease[]>([])
const activeVersion = ref('')
const siteURL = ref('')
const localLoading = ref(false)
const remoteLoading = ref(false)
const localError = ref('')
const remoteError = ref('')
const actionBusy = ref(false)
const controlResult = ref<{ tone: 'info' | 'warning' | 'error'; text: string } | null>(null)

const tickets = useSnipasteDownloadTickets()
let storeRef: ManagedConsoleStore | null = null
const adapter = createSnipasteAdapter((progress) => {
  void tickets.handleProgress(progress, refreshLocal, isInstalled)
})

const selected = computed(() => installed.value.find((item) => item.version === activeVersion.value) ?? installed.value[0] ?? null)
const stale = computed(() => releases.value.some((item) => item.stale))

type ReleaseStatus = 'installed' | 'downloading' | 'error' | 'idle'

function bindStore(store: ManagedConsoleStore): void {
  storeRef = store
}

function snapshot(): Snapshot | null {
  return storeRef?.snap as Snapshot | null ?? null
}

function instanceState(): string {
  return snapshot()?.state ?? 'stopped'
}

function isInstalled(version: string): boolean {
  return installed.value.some((item) => item.version === version)
}

async function refreshLocal(): Promise<boolean> {
  localLoading.value = true
  localError.value = ''
  try {
    const [local, active] = await Promise.all([
      adapter.snipaste.listInstalled(),
      adapter.snipaste.getActive(),
    ])
    installed.value = local ?? []
    activeVersion.value = active ?? ''
    return true
  } catch (error) {
    localError.value = `读取本地版本失败：${getErrorMessage(error)}`
    return false
  } finally {
    localLoading.value = false
  }
}

async function refreshRemote(): Promise<boolean> {
  remoteLoading.value = true
  remoteError.value = ''
  try {
    releases.value = (await adapter.snipaste.listReleases()) ?? []
    return true
  } catch (error) {
    remoteError.value = `获取 Snipaste 官网版本失败：${getErrorMessage(error)}`
    return false
  } finally {
    remoteLoading.value = false
  }
}

async function loadSiteURL(): Promise<void> {
  try {
    siteURL.value = (await adapter.snipaste.officialSiteURL()) ?? ''
  } catch (error) {
    console.warn('load Snipaste site URL failed:', getErrorMessage(error))
  }
}

async function launch(store: ManagedConsoleStore): Promise<void> {
  if (actionBusy.value || !selected.value || ['starting', 'running', 'quitting'].includes(instanceState())) return
  actionBusy.value = true
  controlResult.value = null
  try {
    const outcome = await adapter.snipaste.launch()
    controlResult.value = { tone: 'info', text: outcome.message }
    showToast(outcome.message)
    await Promise.allSettled([refreshLocal(), store.refresh()])
  } catch (error) {
    const text = `启动失败：${getErrorMessage(error)}`
    controlResult.value = { tone: 'error', text }
    showToast(text)
  } finally {
    actionBusy.value = false
  }
}

async function quitProcess(store: ManagedConsoleStore): Promise<void> {
  if (actionBusy.value || !['starting', 'running', 'quitting'].includes(instanceState()) || instanceState() === 'quitting') return
  actionBusy.value = true
  controlResult.value = null
  try {
    const outcome = await adapter.snipaste.quit()
    if (!outcome) return
    controlResult.value = { tone: outcome.forced ? 'warning' : 'info', text: outcome.message }
    showToast(outcome.message)
    await store.refresh()
  } catch (error) {
    controlResult.value = { tone: 'error', text: `退出失败：${getErrorMessage(error)}` }
  } finally {
    actionBusy.value = false
  }
}

function ticketOf(version: string): SnipasteDownloadTicket | undefined {
  return tickets.ticketOf(version)
}

function statusOf(release: SnipasteRelease): ReleaseStatus {
  const ticket = ticketOf(release.version)
  if (ticket?.stage === 'error') return 'error'
  if (ticket) return 'downloading'
  return isInstalled(release.version) ? 'installed' : 'idle'
}

async function download(release: SnipasteRelease): Promise<void> {
  if (tickets.isBusy(release.version)) return
  tickets.begin(release.version)
  try {
    const result = await adapter.snipaste.download(release.version)
    if (result === 'already-installed') {
      const refreshed = await refreshLocal()
      if (refreshed) tickets.clearTicket(release.version)
      else {
        tickets.setTicket({ key: release.version, stage: 'done', done: 100, total: 100, message: '版本已安装，本地列表刷新失败' })
        tickets.setRowError(release.version, '版本已安装，但本地列表刷新失败，请重试读取本地版本')
      }
    } else if (result === 'in-progress') {
      tickets.markInProgress(release.version)
      showToast(`Snipaste ${release.version} 已在下载中`)
    }
  } catch (error) {
    tickets.fail(release.version, getErrorMessage(error))
  }
}

async function setActive(item: SnipasteVersionInfo): Promise<void> {
  tickets.clearRowError(item.version)
  try {
    activeVersion.value = await adapter.snipaste.setActive(item.version)
    showToast(`已将 ${item.version} 设为启动版本`)
  } catch (error) {
    tickets.setRowError(item.version, getErrorMessage(error))
  }
}

function isRunningVersion(version: string): boolean {
  const snap = snapshot()
  return !!snap?.version && snap.version === version && ['starting', 'running', 'quitting'].includes(snap.state)
}

function cannotRemoveVersion(item: SnipasteVersionInfo): boolean {
  return item.version === activeVersion.value || isRunningVersion(item.version)
}

function removeDisabledTitle(item: SnipasteVersionInfo): string {
  if (isRunningVersion(item.version)) {
    if (instanceState() === 'starting') return '该版本正在启动'
    if (instanceState() === 'quitting') return '该版本正在退出'
    return '该版本正在运行，请先退出进程'
  }
  if (item.version === activeVersion.value) return '当前使用版本不可卸载，请先选择其他版本'
  return '卸载此版本'
}

async function removeVersion(item: SnipasteVersionInfo): Promise<void> {
  if (cannotRemoveVersion(item)) return
  try {
    const removed = await adapter.snipaste.remove(item, () => !cannotRemoveVersion(item))
    if (!removed) return
    tickets.clearRowError(item.version)
    showToast(`已卸载 Snipaste ${item.version}`)
    await refreshLocal()
  } catch (error) {
    tickets.setRowError(item.version, getErrorMessage(error))
  }
}

async function importLocal(): Promise<void> {
  actionBusy.value = true
  localError.value = ''
  try {
    const info = await adapter.snipaste.importLocal()
    if (!info) return
    showToast(`已导入 Snipaste ${info.version}`)
    await refreshLocal()
  } catch (error) {
    localError.value = `导入失败：${getErrorMessage(error)}`
  } finally {
    actionBusy.value = false
  }
}

async function openDir(item: SnipasteVersionInfo): Promise<void> {
  try { await adapter.snipaste.openDir(item.dir) } catch (error) { showToast(`打开目录失败：${getErrorMessage(error)}`) }
}

async function openSite(): Promise<void> {
  try { await adapter.snipaste.openOfficialSite() } catch (error) { showToast(`打开官网失败：${getErrorMessage(error)}`) }
}

function fmtSize(bytes: number): string {
  if (!bytes) return '未知'
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${Math.round(bytes / 1024)} KB`
}

function stageText(progress?: SnipasteDownloadTicket): string {
  if (!progress) return ''
  const names: Record<string, string> = {
    pending: '准备中', resolve: '解析官网版本', downloading: '下载中',
    'verify-size': '校验大小', 'verify-hash': '校验官方哈希',
    'verify-archive': '校验 ZIP 与布局', install: '完成安装', done: '安装完成', error: '安装失败',
  }
  return progress.message || names[progress.stage] || progress.stage
}

function percent(progress?: SnipasteDownloadTicket): number | null {
  if (!progress || progress.stage !== 'downloading' || !progress.total) return null
  return Math.min(99, Math.round((progress.done / progress.total) * 100))
}

function verificationLabel(item: SnipasteVersionInfo | SnipasteRelease): string {
  if ('verificationMode' in item) {
    if (item.verificationMode.includes('official-sha1')) return '官方 SHA-1 + 大小 + ZIP CRC + 布局'
    if (item.verificationMode === 'local-import+layout') return '本地导入 + 布局检查'
    return '大小 + ZIP CRC + 布局'
  }
  return item.officialHash ? '官方 SHA-1' : '大小 + ZIP CRC + 布局'
}

onMounted(() => {
  void Promise.allSettled([refreshLocal(), refreshRemote(), loadSiteURL()])
})

onActivated(() => {
  void refreshLocal()
  void storeRef?.refresh()
})
</script>

<template>
  <ManagedConsoleShell
    class="snipaste-view"
    :adapter="adapter"
    title="Snipaste"
    subtitle="管理并启动官方 Windows x64 免安装版；原生截图、贴图、托盘和快捷键保持不变。"
    console-tab-label="控制台"
    versions-tab-label="版本管理"
    tab-id-prefix="snipaste"
    tab-label="Snipaste 页面"
  >
    <template #icon>
      <span class="snipaste-mark" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M9.4 7.7 5.8 4.1a2.6 2.6 0 1 0-1.7 4.5c.7 0 1.3-.3 1.8-.7l2.2 2.2m6.5-2.4 3.6-3.6a2.6 2.6 0 1 1 1.7 4.5c-.7 0-1.3-.3-1.8-.7L8.7 17.3a2.6 2.6 0 1 1-1.8-1.8L17 5.4M10 14l4 4" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg></span>
    </template>

    <template #control-bar="{ store, selectTab }">
      <SnipasteControlPanel
        :ref="() => bindStore(store)"
        :store="store"
        :selected="selected"
        :busy="actionBusy || store.busy"
        :control-result="controlResult"
        @launch="launch(store)"
        @quit="quitProcess(store)"
        @select-versions="selectTab('versions')"
      />
    </template>

    <article class="info-panel">
      <h2>运行边界</h2>
      <ul>
        <li>Hanxi 只控制当前会话直接启动并成功登记的 Snipaste 进程。</li>
        <li>外部或上个 Hanxi 会话启动的实例不会被认领，也不会被退出。</li>
        <li>退出 Hanxi 或停用本模块后，Snipaste 仍会保留托盘与全局快捷键。</li>
        <li>页面退出会先发送尽力关闭请求，超时后强制结束。</li>
      </ul>
      <button class="link-button" @click="openSite">打开 Snipaste 官网<span v-if="siteURL"> · {{ siteURL }}</span></button>
    </article>

    <template #versions-body="{ store }">
      <SnipasteVersionsPanel
        :ref="() => bindStore(store)"
        :installed="installed"
        :releases="releases"
        :active-version="activeVersion"
        :local-loading="localLoading"
        :remote-loading="remoteLoading"
        :local-error="localError"
        :remote-error="remoteError"
        :stale="stale"
        :busy="actionBusy || store.busy"
        :row-errors="tickets.rowErrors.value"
        :is-running-version="isRunningVersion"
        :cannot-remove-version="cannotRemoveVersion"
        :remove-disabled-title="removeDisabledTitle"
        :status-of="statusOf"
        :ticket-of="ticketOf"
        :fmt-size="fmtSize"
        :verification-label="verificationLabel"
        :stage-text="stageText"
        :percent="percent"
        @import="importLocal"
        @refresh-local="refreshLocal"
        @refresh-remote="refreshRemote"
        @open-dir="openDir"
        @set-active="setActive"
        @remove="removeVersion"
        @download="download"
      />
    </template>
  </ManagedConsoleShell>
</template>

<style scoped>
.snipaste-view { max-width: 1120px; margin: 0 auto; }
.snipaste-mark { width: 42px; height: 42px; display: grid; place-items: center; flex: 0 0 auto; border-radius: var(--radius-control); color: var(--color-primary); background: var(--surface-selected); }
.snipaste-mark svg { width: 25px; height: 25px; }
.info-panel { border: 1px solid var(--color-border); border-radius: var(--radius-element); background: var(--surface-panel); padding: 16px; }
.info-panel h2 { margin: 0; }
.info-panel ul { margin: 10px 0 14px; padding-left: 20px; color: var(--color-text-muted); line-height: 1.7; }
</style>
