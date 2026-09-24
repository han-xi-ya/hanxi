<script setup lang="ts">
// 软件版本检测（F5，微信首个目标）：本机双口径（注册表×PE 并示标源）×
// 官方最新版（SSR 更新页解析，失配降级为打开官方页）、下载直链复制与
// 安装包直连下载（N38：只搬包到系统下载目录，不托管安装/启动），
// 以及安装/两代数据目录的空间勘察（异步扫描 + 可取消 + 结果缓存）。
// 定位边界：全量软件清单与卸载归 bcu，本页只是白名单跟踪对象的升级引导。
import { computed, onActivated, onMounted, ref, shallowRef } from 'vue'
import * as SoftverAPI from '../../bindings/hanxi/internal/modules/softver'
import type {
  DirSlot,
  InstallerFile,
  InstallerProgress,
  LocalInstall,
  ScanProgress,
  Snapshot,
} from '../../bindings/hanxi/internal/modules/softver/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useClipboard } from '../composables/useClipboard'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { fmtDate, fmtSize } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiButton from '../components/ui/UiButton.vue'

const snap = shallowRef<Snapshot | null>(null)
const loading = ref(true)
const errorMsg = ref('')
const fetchingOfficial = ref(false)
// 扫描运行态（槽位 ID → 最近进度）：终态事件到达即清；done 同时把结果写回快照。
const scanStates = ref<Record<string, ScanProgress>>({})
// 安装包下载运行态（单槽位）：downloading 事件更新、终态清空；
// 页面回访（KeepAlive）时快照的 downloading 标记兜底恢复"进行中"呈现。
const dlState = ref<InstallerProgress | null>(null)

const { copyWithToast } = useClipboard()
const { showToast, showErrorToast } = useToast()

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    snap.value = await SoftverAPI.SoftverService.Snapshot()
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

// 拉官方最新版：成功/失败都刷新快照（后端缓存读数与失败原因，快照统一回显）。
async function loadOfficial() {
  fetchingOfficial.value = true
  try {
    await SoftverAPI.SoftverService.RefreshOfficial()
  } catch {
    // 失败不弹窗刷屏——官方区改挂降级 banner（卡片口径：解析失败打开官方页）
  } finally {
    fetchingOfficial.value = false
    try {
      snap.value = await SoftverAPI.SoftverService.Snapshot()
    } catch { /* 快照失败留给下次显式动作 */ }
  }
}

useWailsEvent<ScanProgress>('softver:dir-scan', (p) => {
  const next = { ...scanStates.value }
  if (p.state === 'running') {
    next[p.id] = p
    scanStates.value = next
    return
  }
  delete next[p.id]
  scanStates.value = next
  if (p.state === 'done') {
    applyScannedSize(p)
    showToast('大小扫描完成')
  } else if (p.state === 'canceled') {
    showToast('已取消扫描，本次结果未缓存')
  } else if (p.state === 'error') {
    showErrorToast(`扫描失败：${p.message || '未知错误'}`)
  }
})

// 事件载荷直接写回快照槽位，省一次全量重探（路径口径与后端缓存一致）。
function applyScannedSize(p: ScanProgress) {
  const s = snap.value
  if (!s) return
  s.dirs = (s.dirs ?? []).map((d) =>
    d.id === p.id ? { ...d, size: { bytes: p.bytes, files: p.files, dirs: p.dirs, skipped: p.skipped, scannedAt: p.scannedAt || '' } } : d,
  )
  snap.value = { ...s }
}

async function startScan(d: DirSlot) {
  try {
    await SoftverAPI.SoftverService.StartDirScan(d.id)
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

async function cancelScan(d: DirSlot) {
  try {
    await SoftverAPI.SoftverService.CancelDirScan(d.id)
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

async function reveal(d: DirSlot) {
  try {
    await SoftverAPI.SoftverService.RevealDir(d.id)
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

async function openOfficialPage() {
  try {
    await SoftverAPI.SoftverService.OpenUpdatesPage()
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

// ---- 下载安装包（N38：拿完包走人，不托管安装/启动） ----

async function downloadInstaller() {
  try {
    await SoftverAPI.SoftverService.StartInstallerDownload()
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

async function cancelDownload() {
  try {
    await SoftverAPI.SoftverService.CancelInstallerDownload()
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

async function revealInstaller() {
  try {
    await SoftverAPI.SoftverService.RevealInstallerFile()
  } catch (err) {
    showErrorToast(getErrorMessage(err))
  }
}

useWailsEvent<InstallerProgress>('softver:installer-download', (p) => {
  if (p.state === 'downloading') {
    dlState.value = p
    return
  }
  dlState.value = null
  if (p.state === 'done' && p.file) {
    applyDownloaded(p.file)
    showToast('安装包下载完成')
  } else if (p.state === 'canceled') {
    showToast('已取消下载，半截临时文件已清理')
  } else if (p.state === 'error') {
    showErrorToast(`下载失败：${p.message || '未知错误'}`)
  }
})

// done 事件直接写回快照成品记录（与 dir-scan 写回大小同构），下次 Snapshot
// 由后端缓存回显，两端同源。
function applyDownloaded(file: InstallerFile) {
  const s = snap.value
  if (!s) return
  snap.value = { ...s, downloaded: file, downloading: false }
}

// 进行中口径：事件流优先；KeepAlive 回访错过早期事件时按快照 downloading 标记
// 兜底（无字节读数，只如实显示"下载进行中"）。
const activeDownload = computed<InstallerProgress | null>(() => {
  if (dlState.value) return dlState.value
  if (snap.value?.downloading) {
    return { state: 'downloading', fileName: '', done: 0, total: 0 }
  }
  return null
})
const downloaded = computed(() => snap.value?.downloaded ?? null)

// 进度文案：有声明字节才给百分比（不造假进度），无声明只报已收字节。
function dlText(p: InstallerProgress): string {
  if (!p.fileName && !p.done) return '下载进行中…'
  if (p.total > 0) {
    return `下载中 ${Math.floor((p.done / p.total) * 100)}% · ${fmtDirSize(p.done)} / ${fmtDirSize(p.total)}`
  }
  return `下载中 ${fmtDirSize(p.done)}`
}

const installs = computed(() => snap.value?.installs ?? [])
const dirs = computed(() => snap.value?.dirs ?? [])
const dataSlots = computed(() => dirs.value.filter((d) => d.kind !== 'install'))
const liveSlots = computed(() => dataSlots.value.filter((d) => d.exists))
const ghostSlots = computed(() => dataSlots.value.filter((d) => !d.exists))
const official = computed(() => snap.value?.official ?? null)
const update = computed(() => snap.value?.update ?? null)

// 安装目录槽位按路径回填到对应安装卡（大小/扫描按钮共用槽位面）。
// 组装成视图模型一次配对，模板里不再反复 find + 非空断言（类型收窄友好）。
function installSlotOf(inl: LocalInstall): DirSlot | undefined {
  if (!inl.installDir) return undefined
  return dirs.value.find((d) => d.kind === 'install' && d.path.toLowerCase() === inl.installDir.toLowerCase())
}
const installVM = computed(() =>
  (snap.value?.installs ?? []).map((inl) => ({ install: inl, slot: installSlotOf(inl) })),
)

const updateTone = computed(() => {
  switch (update.value?.status) {
    case 'available': return 'warning'
    case 'latest': return 'positive'
    default: return 'neutral'
  }
})
const updateText = computed(() => {
  switch (update.value?.status) {
    case 'available': return `可更新 ${update.value.localVersion} → ${update.value.officialVersion}`
    case 'latest': return '已是最新'
    case 'noLocal': return '未检测到本机微信'
    default: return '官方版本未获取'
  }
})

function scanRunning(d: DirSlot): ScanProgress | undefined {
  return scanStates.value[d.id]
}

// fmtSize 上限只到 MB，数据目录常达数十 GB——升一档到 GB 呈现（WslDistroTable 同口径先例）。
function fmtDirSize(bytes?: number | null): string {
  if (!bytes) return '—'
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  return fmtSize(bytes)
}
function originText(o: string): string {
  const map: Record<string, string> = {
    registry: '注册表', hkcu: 'HKCU 登记', default: '默认路径',
    documents: '文档目录', driveRoot: '盘符根候选', config: '客户端记录',
  }
  return map[o] ?? o
}

onMounted(async () => {
  await refresh()
  void loadOfficial() // 版本对照是本页存在的意义：进入即拉一次官方读数
})

// KeepAlive 回访静默重探（微信可能在后台被升级）：首帧由 onMounted 承担；
// 官方读数不重拉（网络外呼只跟显式动作与首帧），扫描缓存原样回显。
let firstFrame = true
onActivated(async () => {
  if (firstFrame) {
    firstFrame = false
    return
  }
  try {
    snap.value = await SoftverAPI.SoftverService.Snapshot()
  } catch { /* 静默失败不打扰，旧数据可继续用 */ }
})
</script>

<template>
  <div class="page">
    <PageHeader title="软件版本" subtitle="日常装机软件的版本跟踪与升级引导（微信首个目标）：本机注册表×PE 双口径、官方最新版对照与安装包直连下载、目录空间勘察。">
      <template #actions>
        <div class="status-group">
          <UiStatusChip v-if="update" :tone="updateTone">{{ updateText }}</UiStatusChip>
          <UiButton small :disabled="loading" @click="refresh">重新探测</UiButton>
        </div>
      </template>
    </PageHeader>

    <div v-if="loading" class="state-box">正在探测本机微信安装与目录…</div>
    <div v-else-if="errorMsg" class="state-box state-error">
      探测失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <section class="panel">
        <div class="panel-top">
          <h2 class="sec-title">本机安装（双口径对照）</h2>
          <span class="sec-note-inline">注册表 = 安装器口径；PE = 安装内容口径；主口径参与官方对比。</span>
        </div>

        <div v-if="installs.length === 0" class="empty-hint">
          未在注册表 Uninstall 命中微信本体（Weixin/WeChat）。企业微信是另一产品，不在本页跟踪范围。
        </div>
        <div v-for="iv in installVM" :key="iv.install.id" class="install-card">
          <div class="install-head">
            <b>{{ iv.install.displayName || '微信' }}</b>
            <span class="chip">{{ iv.install.generation }}</span>
            <span class="ver-big mono">{{ iv.install.bestVersion || '版本未知' }}</span>
            <span v-if="iv.install.publisher" class="pub">{{ iv.install.publisher }}</span>
          </div>
          <table class="tbl src-tbl">
            <thead><tr><th>口径</th><th>字段</th><th>版本读数</th><th>证据</th></tr></thead>
            <tbody>
              <tr v-for="(src, i) in iv.install.sources ?? []" :key="i" :class="{ 'row-primary': src.primary }">
                <td>{{ src.primary ? '★ ' : '' }}{{ src.label }}</td>
                <td class="mono">{{ src.field }}</td>
                <td class="mono">{{ src.value }}</td>
                <td class="mono cell-path" :title="src.detail">{{ src.detail }}</td>
              </tr>
            </tbody>
          </table>
          <p v-for="(n, i) in iv.install.notes ?? []" :key="'n' + i" class="field-warn">{{ n }}</p>
          <div v-if="iv.slot" class="dir-row">
            <span class="dir-main">
              <span class="dir-name">安装目录</span>
              <span class="chip chip-neutral">{{ originText(iv.slot.origin) }}</span>
              <span class="mono cell-path" :title="iv.slot.path">{{ iv.slot.path }}</span>
              <span v-if="iv.install.estimatedSizeKb > 0" class="dim">安装器估计 {{ fmtDirSize(iv.install.estimatedSizeKb * 1024) }}</span>
            </span>
            <span class="dir-ops">
              <template v-if="scanRunning(iv.slot)">
                <span class="scan-live mono">
                  {{ fmtDirSize(scanRunning(iv.slot)!.bytes) }} · {{ scanRunning(iv.slot)!.files }} 文件
                </span>
                <UiButton small @click="cancelScan(iv.slot)">取消</UiButton>
              </template>
              <template v-else>
                <span v-if="iv.slot.size" class="size-text">
                  实占 <b class="mono">{{ fmtDirSize(iv.slot.size.bytes) }}</b>
                  · {{ iv.slot.size.files }} 文件
                  <span v-if="iv.slot.size.skipped > 0" class="field-warn-inline">（{{ iv.slot.size.skipped }} 项不可读，偏小）</span>
                  · {{ fmtDate(iv.slot.size.scannedAt) }}
                </span>
                <UiButton v-if="iv.slot.exists" small @click="startScan(iv.slot)">
                  {{ iv.slot.size ? '重扫大小' : '扫描大小' }}
                </UiButton>
                <UiButton v-if="iv.slot.exists" small variant="ghost" @click="reveal(iv.slot)">打开</UiButton>
              </template>
            </span>
          </div>
          <p v-else-if="!iv.install.installDirExists" class="field-warn">
            未解析到安装目录（注册表 InstallLocation 缺失或路径已失效）——目录勘察不可用。
          </p>
        </div>
      </section>

      <section class="panel">
        <div class="panel-top">
          <h2 class="sec-title">官方最新版</h2>
          <div class="panel-actions">
            <UiButton variant="primary" small :disabled="fetchingOfficial" @click="loadOfficial">
              {{ fetchingOfficial ? '获取中…' : '获取官方最新版' }}
            </UiButton>
          </div>
        </div>
        <template v-if="official">
          <div class="official-head">
            <span class="ver-big mono">{{ official.version }}</span>
            <span class="dim">发布于页面读数 · {{ fmtDate(official.fetchedAt) }} 获取</span>
          </div>
          <p v-if="official.parseNote" class="field-warn">{{ official.parseNote }}</p>
          <div v-if="official.downloadUrl" class="link-row">
            <span class="mono cell-path" :title="official.downloadUrl">{{ official.downloadUrl }}</span>
            <UiButton small @click="copyWithToast(official.downloadUrl, '已复制下载直链')">复制直链</UiButton>
            <template v-if="activeDownload">
              <span class="scan-live mono">{{ dlText(activeDownload) }}</span>
              <UiButton small @click="cancelDownload">取消</UiButton>
            </template>
            <UiButton v-else small variant="primary" @click="downloadInstaller">下载安装包</UiButton>
          </div>
          <p v-if="official.downloadUrl" class="field-warn">
            官方直链未提供校验值：下载只核对传输字节数与可执行文件头（MZ），不做 SHA-256 校验；
            安装包存到系统下载目录，本页不代安装、不托管启动。
          </p>
          <div v-if="downloaded" class="dir-row">
            <span class="dir-main">
              <span class="dir-name">已下载安装包</span>
              <span class="chip chip-neutral">官方 {{ downloaded.version }}</span>
              <span class="mono cell-path" :title="downloaded.path">{{ downloaded.fileName }}</span>
              <span class="size-text">{{ fmtDirSize(downloaded.bytes) }} · {{ fmtDate(downloaded.downloadedAt) }}</span>
            </span>
            <span class="dir-ops">
              <UiButton small @click="revealInstaller">打开位置</UiButton>
            </span>
          </div>
          <div class="panel-foot">
            <button type="button" class="link-button" @click="openOfficialPage">在浏览器打开官方更新页</button>
          </div>
        </template>
        <div v-else class="banner banner-warn" role="note">
          <span>{{ snap?.officialError ? `官方通道读取失败：${snap.officialError}` : '尚未获取官方最新版' }}</span>
          <button type="button" class="link-button" @click="openOfficialPage">打开官方更新页</button>
        </div>
      </section>

      <section class="panel">
        <h2 class="sec-title">数据目录勘察（两代布局）</h2>
        <p class="sec-note">
          4.0 数据目录为 <b class="mono">xwechat_files</b>、3.x 为 <b class="mono">WeChat Files</b>；
          存储位置可自定义盘符，默认路径、客户端记录与盘符根候选三路探测。
          数据目录常达几十 GB——大小按需扫描（可中途取消，成功结果缓存）。
        </p>
        <div v-if="liveSlots.length === 0" class="empty-hint">未在任何候选点发现两代数据目录。</div>
        <div v-for="d in liveSlots" :key="d.id" class="dir-row" :class="{ 'row-primary': d.active }">
          <span class="dir-main">
            <span class="dir-name">{{ d.label }}</span>
            <span class="chip chip-neutral">{{ originText(d.origin) }}</span>
            <span v-if="d.active" class="chip chip-positive">在用</span>
            <span class="mono cell-path" :title="d.path">{{ d.path }}</span>
          </span>
          <span class="dir-ops">
            <template v-if="scanRunning(d)">
              <span class="scan-live mono">
                {{ fmtDirSize(scanRunning(d)!.bytes) }} · {{ scanRunning(d)!.files }} 文件 ·
                <span class="cell-path" :title="scanRunning(d)!.current">{{ scanRunning(d)!.current }}</span>
              </span>
              <UiButton small @click="cancelScan(d)">取消</UiButton>
            </template>
            <template v-else>
              <span v-if="d.size" class="size-text">
                <b class="mono">{{ fmtDirSize(d.size.bytes) }}</b> · {{ d.size.files }} 文件
                <span v-if="d.size.skipped > 0" class="field-warn-inline">（{{ d.size.skipped }} 项不可读，偏小）</span>
                · {{ fmtDate(d.size.scannedAt) }}
              </span>
              <UiButton small @click="startScan(d)">{{ d.size ? '重扫大小' : '扫描大小' }}</UiButton>
              <UiButton small variant="ghost" @click="reveal(d)">打开</UiButton>
            </template>
          </span>
        </div>
        <p v-if="ghostSlots.length" class="dim ghost-list">
          未发现的候选点：{{ ghostSlots.map((g) => g.path).join('、') }}
        </p>
      </section>

      <section v-if="snap?.notes?.length" class="banner banner-info" role="note">
        <div v-for="(n, i) in snap.notes" :key="i">{{ n }}</div>
      </section>

      <section class="panel usage">
        <h2 class="sec-title">使用说明</h2>
        <ul class="usage-list">
          <li>双口径不一致时以版本段更全者为主口径（注册表 DisplayVersion 是安装器写入，PE 资源随文件走）；两者并示标源、都可核对证据。</li>
          <li>官方通道是页面抓取而非 API：改版即失配，此时只能"打开官方页"人工对照，工具不猜不编。页内 8.0.x 是移动端系列，已按口径过滤。</li>
          <li>直链（dldir1v6.qq.com）可"下载安装包"到系统下载目录（走浏览器同款代理出口），也可复制链接自行到浏览器下载；官方不提供校验值，工具只核字节数与可执行文件头并如实标注。本页不静默安装、不托管启动——安装由你双击完成（微信覆盖安装保数据）。</li>
          <li>全量软件清单与卸载归"软件卸载 (BCU)"；本页只跟踪白名单对象的版本与空间。</li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
.status-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
.sec-title { font-size: var(--text-md); font-weight: 600; margin: 0; color: var(--color-text); }
.sec-note { font-size: var(--text-sm); color: var(--color-text-muted); margin: 6px 0 12px; line-height: 1.6; }
.sec-note-inline { font-size: var(--text-xs); color: var(--color-text-muted); }
.panel-top { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.panel-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.panel-foot { display: flex; justify-content: flex-end; margin-top: 8px; }

.install-card { border: 1px solid var(--color-border); border-radius: var(--radius-element); padding: 12px; margin-top: 10px; }
.install-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ver-big { font-size: var(--text-lg); font-weight: 650; color: var(--color-text); font-variant-numeric: tabular-nums; }
.pub { font-size: var(--text-xs); color: var(--color-text-muted); }
.src-tbl { margin-top: 8px; }
.src-tbl td { font-size: var(--text-sm); }
.row-primary { background: var(--color-primary-soft); }
.cell-path { max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; vertical-align: bottom; }

.dir-row {
  display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap;
  padding: 8px 0; border-top: 1px dashed var(--color-border);
}
.dir-row.row-primary { border-radius: 8px; }
.dir-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }
.dir-name { font-size: var(--text-sm); font-weight: 600; color: var(--color-text); flex: none; }
.dir-ops { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.size-text { font-size: var(--text-sm); color: var(--color-text-muted); }
.scan-live { font-size: var(--text-xs); color: var(--color-text-muted); display: flex; align-items: center; gap: 6px; min-width: 0; animation: hx-pulse 1.2s ease-in-out infinite; }
.official-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; margin-top: 8px; }
.link-row { display: flex; align-items: center; gap: 8px; margin-top: 8px; min-width: 0; }
.dim { color: var(--color-text-muted); font-size: var(--text-sm); }
.ghost-list { margin: 8px 0 0; font-size: var(--text-xs); }
.field-warn { margin: 4px 0 0; font-size: var(--text-sm); color: var(--state-warning); }
.field-warn-inline { color: var(--state-warning); }

.usage { margin-top: 16px; }
.usage-list {
  margin: 8px 0 0; padding-left: 18px; display: flex; flex-direction: column; gap: 6px;
  font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.65;
}
@media (prefers-reduced-motion: reduce) {
  .scan-live { animation: none; }
}
</style>
