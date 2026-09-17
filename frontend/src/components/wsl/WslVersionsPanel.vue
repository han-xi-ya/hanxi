<script setup lang="ts">
// WSL 本体版本页签（「🧩 本体版本」：GitHub 官方 Releases × 本机关系 × MSI 应用内下载，
// Phase 6 式自 WSLView 拆分）。内置 MSI 下载走 wsl:msi-download 事件回推——订阅随本组件挂载，
// 与拆分前同帧（页签为 v-show 常驻）；「🔄 立即更新」意图上抛视图走 runOp 白名单（与就绪页共用编排）。
// 行为逐字迁出：bindings 调用与确认/toast 序列未做任何改动。
import { onMounted, ref } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { Report } from '../../../bindings/hanxi/internal/modules/wsl/readiness/models'
import type { Asset, Overview, Release } from '../../../bindings/hanxi/internal/modules/wsl/releases/models'
import type { DownloadProgress } from '../../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../../composables/useToast'
import { useClipboard } from '../../composables/useClipboard'
import { useWailsEvent } from '../../composables/useWailsEvent'
import { getErrorMessage } from '../../utils/errors'
import { fmtSize } from '../../utils/format'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import UiProgressBar from '../ui/UiProgressBar.vue'

const props = defineProps<{
  report: Report | null
  busyAny: boolean
  // 版本落后横幅的「🔄 立即更新」与就绪页「🔄 更新」同走视图 updateWsl（runOp 编排）
  updateWsl: () => Promise<void>
}>()

const { showToast } = useToast()
const { copy } = useClipboard()

const overview = ref<Overview | null>(null)
const relLoading = ref(false)
const relError = ref('')

async function loadReleases() {
  relLoading.value = true
  relError.value = ''
  try {
    overview.value = await WSLAPI.GetReleases()
  } catch (e) {
    relError.value = `获取官方发布列表失败: ${getErrorMessage(e)}\n若本机一键安装也报「已禁止(403)」，是 GitHub API 被网络侧拦截：换网络/挂代理，或到官方发布页手动下载 MSI 离线安装。`
  } finally {
    relLoading.value = false
  }
}

// ---------- 内置 MSI 下载（应用内进度，不甩浏览器） ----------
const dl = ref<Record<string, DownloadProgress>>({})
const dlKey = (tag: string, platform: string) => `${tag}:${platform}`

useWailsEvent<DownloadProgress>('wsl:msi-download', (p) => {
  if (!p) return
  dl.value = { ...dl.value, [dlKey(p.tag, p.platform)]: p }
})

function dlPercent(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (!p.total || p.total <= 0) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

async function downloadAsset(tag: string, name: string, platform: string) {
  const key = dlKey(tag, platform)
  if (dl.value[key]?.stage === 'downloading') return
  dl.value = { ...dl.value, [key]: { tag, platform, stage: 'downloading', done: 0, total: 0 } }
  try {
    const out = (await WSLAPI.DownloadMsi(tag, name)) as { message?: string }
    showToast(out.message || '开始下载')
  } catch (e) {
    dl.value = { ...dl.value, [key]: { tag, platform, stage: 'error', done: 0, total: 0, error: getErrorMessage(e) } }
  }
}

async function cancelDownload(key: string) {
  if (dl.value[key]?.stage !== 'downloading') return
  try {
    const out = (await WSLAPI.CancelMsiDownload()) as { message?: string }
    showToast(out.message || '已请求取消下载') // 终态以 wsl:msi-download error 事件为准回显
  } catch (e) {
    showToast(`取消下载失败: ${getErrorMessage(e)}`)
  }
}

async function revealDownload(rel: Release, name: string) {
  try {
    await WSLAPI.RevealDownload(rel.tag, name)
  } catch (e) {
    showToast(`打开位置失败: ${getErrorMessage(e)}`)
  }
}

// 只显示与本机架构匹配的资产：x64 机器列出 ARM 包毫无意义（异架构直接隐藏）；
// 体检未出结果（machineArch 未知）或列表里没有本机包时，兜底展示全部。
function isCurrentPlatform(platform: string): boolean {
  const m = props.report?.machineArch
  return !!m && platform.toLowerCase() === m.toLowerCase()
}
function assetsShown(rel: Release): Asset[] {
  const list = rel.assets ?? []
  const m = props.report?.machineArch
  if (!m) return list
  const mine = list.filter(a => a.platform.toLowerCase() === m.toLowerCase())
  return mine.length ? mine : list
}

async function openReleaseTag(tag: string) {
  try {
    await WSLAPI.OpenReleaseTag(tag)
  } catch (e) {
    showToast(`打开发行说明失败: ${getErrorMessage(e)}`)
  }
}

function relationTone(r: string): 'positive' | 'warning' | 'information' | 'neutral' {
  switch (r) {
    case 'latest': return 'positive'
    case 'update': return 'warning'
    case 'ahead': return 'information'
    default: return 'neutral'
  }
}

function relationText(r: string): string {
  switch (r) {
    case 'latest': return '已是最新'
    case 'update': return '可更新'
    case 'ahead': return '领先正式版'
    default: return '无法比较'
  }
}

async function openReleasesPage() {
  try {
    await WSLAPI.OpenReleasesPage()
  } catch (e) {
    showToast(`打开发布页失败: ${getErrorMessage(e)}`)
  }
}
async function openDocs() {
  try {
    await WSLAPI.OpenOfficialDocs()
  } catch (e) {
    showToast(`打开文档失败: ${getErrorMessage(e)}`)
  }
}

onMounted(() => {
  loadReleases()
})
</script>

<template>
  <div class="control-panel">
    <div class="meta-info">
      <span>
        本机 <strong>{{ overview?.localVersion || report?.wslVersion || '未安装' }}</strong>
        · 最新正式版 <strong class="mono">{{ overview?.latest || '—' }}</strong>
        <template v-if="overview?.fetchedAt"> · 拉取于 {{ overview.fetchedAt }}</template>
      </span>
      <span v-if="overview?.isStale" class="hint-dim">⚠ 网络拉取失败，当前为过期缓存（数据可能落后于官方）</span>
      <span v-else-if="overview?.fallback" class="hint-dim">⚠ api.github.com 被网络侧拦截（403），列表已自动降级为官方 Releases 订阅源——版本与直链完整，文件大小不可得故显示"—"</span>
      <span v-else class="hint-dim">MSI 由 Hanxi 直接下载到系统"下载"文件夹（应用内进度，不经过浏览器）；列表只显示本机架构（{{ report?.machineArch || '探测中…' }}）的安装包，异架构已自动隐藏</span>
    </div>
    <div class="btn-group">
      <button class="btn btn-secondary btn-small" :disabled="relLoading" @click="loadReleases">{{ relLoading ? '刷新中…' : '↻ 刷新列表' }}</button>
      <button class="btn btn-secondary btn-small" @click="openReleasesPage">🌐 官方发布页</button>
      <button class="btn btn-secondary btn-small" @click="openDocs">📖 官方文档</button>
    </div>
  </div>

  <UiBanner v-if="overview && overview.relation === 'update'" tone="warn" class="slim">
    {{ overview.relationDetail }}
    <button class="btn btn-primary btn-small update-inline" :disabled="busyAny" @click="updateWsl">🔄 立即更新</button>
  </UiBanner>
  <div v-else-if="overview?.relationDetail" class="hint-line">
    <UiStatusChip :tone="relationTone(overview.relation)">{{ relationText(overview.relation) }}</UiStatusChip>
    {{ overview.relationDetail }}
  </div>

  <div v-if="relError" class="error-box">{{ relError }}</div>

  <div class="section-title"><h3>官方发布 ({{ overview?.releases?.length ?? 0 }})</h3></div>
  <div v-if="relLoading && !overview" class="hint-line">正在加载 microsoft/WSL 发布列表…</div>
  <div v-else-if="overview && !overview.releases?.length && !relError" class="empty-state"><p>官方列表暂无含 Windows MSI 的发布条目。</p></div>
  <div v-else-if="overview?.releases?.length" class="table-container">
    <table class="tbl">
      <thead>
        <tr>
          <th style="width: 150px;">版本</th>
          <th style="width: 110px;">发布</th>
          <th>安装包资产</th>
          <th style="width: 110px;">说明</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="rel in overview.releases" :key="rel.tag">
          <td>
            <code class="mono">{{ rel.tag }}</code>
            <UiStatusChip v-if="rel.tag === overview.latest" tone="positive">最新</UiStatusChip>
            <UiStatusChip v-if="rel.prerelease" tone="warning">预发布</UiStatusChip>
          </td>
          <td class="mono dim">{{ rel.published }}</td>
          <td>
            <div v-for="a in assetsShown(rel)" :key="a.name" class="asset-line">
              <span class="plat-chip" :class="{ current: isCurrentPlatform(a.platform) }">{{ a.platform }}</span>
              <span v-if="isCurrentPlatform(a.platform)" class="badge-current">本机</span>
              <span class="mono dim">{{ fmtSize(a.size) }}</span>
              <template v-if="dl[dlKey(rel.tag, a.platform)]?.stage === 'downloading'">
                <span class="dl-bar"><UiProgressBar :percent="dlPercent(dl[dlKey(rel.tag, a.platform)]!)" /></span>
                <span class="mono dim">{{ dlPercent(dl[dlKey(rel.tag, a.platform)]!) }}%</span>
                <button class="link-button" @click="cancelDownload(dlKey(rel.tag, a.platform))">取消</button>
              </template>
              <template v-else-if="dl[dlKey(rel.tag, a.platform)]?.stage === 'done'">
                <UiStatusChip tone="positive">✓ 已下载</UiStatusChip>
                <button class="link-button" @click="revealDownload(rel, a.name)">📂 打开位置</button>
              </template>
              <template v-else-if="dl[dlKey(rel.tag, a.platform)]?.stage === 'error'">
                <span class="dl-error" :title="dl[dlKey(rel.tag, a.platform)]!.error">✕ 失败，可重试</span>
                <button class="btn btn-secondary btn-small" @click="downloadAsset(rel.tag, a.name, a.platform)">⬇ 重试</button>
              </template>
              <template v-else>
                <button
                  class="btn btn-small" :class="isCurrentPlatform(a.platform) ? 'btn-primary' : 'btn-secondary'"
                  :title="isCurrentPlatform(a.platform) ? `下载 ${a.platform} 安装包到系统下载文件夹（应用内进度）` : `非本机架构（你的机器是 ${report?.machineArch || '?'}），仅作备选`"
                  @click="downloadAsset(rel.tag, a.name, a.platform)"
                >⬇ 下载</button>
              </template>
              <button class="link-button" @click="copy(a.url)">复制直链</button>
            </div>
          </td>
          <td><button class="link-button" @click="openReleaseTag(rel.tag)">发行说明</button></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
/* 全局原子落 components.css；以下为本页签独有或有意补差（注释注明）。 */

/* 多行诊断文案（换行符保留）——对全局 .error-box 的补差白名单行，非同名副本 */
.error-box { white-space: pre-line; }
/* 基形（字号/颜色/左内距）落回全局 .hint-line；此处仅留 WSL 档补差两行 */
.hint-line { line-height: 1.7; white-space: pre-line; }
/* 全局 .hint-dim 只定义颜色，WSL 注记统一配 --text-sm 小字——补差，非副本 */
.hint-dim { font-size: var(--text-sm); }
/* .banner.slim 等值副本已删净：UiBanner 根元素挂 .banner + .slim，落回全局 :where(.banner.slim) */
/* .dim 非全局原子名，本控制台独有 */
.dim { color: var(--color-text-muted); font-size: var(--text-sm); }

/* 版本面板：底色/边框/内距/圆角/弹性布局落回全局 .control-panel；
   此处仅留 WSL 档补差（gap 与换行，全局标准形无——定档候选，见收编报告） */
.control-panel { gap: 10px; flex-wrap: wrap; }
/* 字号/颜色/列布局落回全局 .meta-info（strong 反色同为全局原子）；此处仅留收缩补差 */
.meta-info { min-width: 0; }
/* display/gap 落回全局 .btn-group；此处仅留换行补差 */
.btn-group { flex-wrap: wrap; }
.update-inline { margin-left: 10px; }

.asset-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 2px 0; }
.plat-chip { font-size: var(--text-xs); font-weight: 700; color: var(--color-primary); background: var(--color-primary-soft, var(--surface-hover)); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 0 8px; }
.plat-chip.current { color: var(--color-on-primary); background: var(--color-primary); border-color: var(--color-primary); }
.badge-current { font-size: var(--text-micro); font-weight: 700; color: var(--color-primary); }
.dl-bar { width: 110px; display: inline-flex; }
/* 色与字号落回全局 .dl-error；此处仅留加粗补差 */
.dl-error { font-weight: 600; }
</style>
