<script setup lang="ts">
// QuickLook 空格预览托管工作台：控制台（启停 + 重载）+ 版本管理（便携 zip 解压安装、导入本地、卸载）
import { ref, computed, onMounted, onDeactivated, onUnmounted } from 'vue'
import * as QuickLookAPI from '../../bindings/hanxi/internal/modules/quicklook/quicklookservice'
import type { QuickLookRelease, QuickLookVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/quicklook/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/quicklook/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/quicklook/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<QuickLookRelease[]>([])
const installed = ref<QuickLookVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)
const uptimeSec = ref(0)

// 下载进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 keyviz/ccswitch/piclite 同构）
const activeMainTab = ref<string>('console')
const MAIN_TABS = [
  { key: 'console', label: '👁️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')
const isRunning = computed(() => state.value === 'running')

// 文案与迁移前逐字一致；'运行中' 与 constants/status 的 running.text（'已启动'）语义有差，
// 统一与否属主线文案决策，此处不强凑。
const stateText = computed(() => {
  switch (state.value) {
    case 'running': return '运行中'
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
      text: '检测到外部 QuickLook 实例（非 Hanxi 托管）。空格预览正在生效；退出与样式设置都在其托盘图标菜单中完成。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'QuickLook 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: 'QuickLook 正在运行：在资源管理器/文件对话框中选中文件按空格键即可即时预览。样式设置请左键点击系统托盘图标 → 设置（上游未提供程序化唤窗入口）。便携配置随 portable.lock 存于安装目录内。',
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
      QuickLookAPI.ListReleases(),
      QuickLookAPI.ListInstalledVersions(),
      QuickLookAPI.GetActiveVersion(),
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
    snap.value = await QuickLookAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('quicklook GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: QuickLookRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function startQuickLook() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await QuickLookAPI.StartQuickLook()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitQuickLook() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await QuickLookAPI.Quit()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`退出失败: ${getErrorMessage(e)}`)
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function reload() {
  if (busy.value) return
  busy.value = true
  try {
    const msg = await QuickLookAPI.Reload()
    showToast(msg)
  } catch (e) {
    showToast(`重载失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// ---------- 版本管理操作 ----------
async function download(rel: QuickLookRelease) {
  try {
    const res = await QuickLookAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`安装失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: QuickLookVersionInfo) {
  try {
    const ver = await QuickLookAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await QuickLookAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: QuickLookVersionInfo) {
  const ok = await confirm({
    title: `确定卸载 QuickLook ${v.version}？`,
    description: '该版本托管目录将被删除，不可恢复。\n（便携目录内的设置一并删除，不影响其它版本）',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await QuickLookAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '请输入本机 QuickLook 便携目录完整路径（含 QuickLook.exe 与 portable.lock，如手动解压的 QuickLook-4.5.0 文件夹）',
    description: '提示：整个目录会被收纳进托管，配置随便携标记存于目录内',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await QuickLookAPI.ImportLocal(path.trim())
    showToast(`已导入 QuickLook ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// ---------- 联动开关 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([QuickLookAPI.GetFollowOnExit(), QuickLookAPI.RepositoryURL()])
    followOnExit.value = f
    repoUrl.value = u
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  const next = !followOnExit.value
  followOnExit.value = next // 用户点击已将勾选框翻转，ref 同步跟进，保持绑定状态一致
  try {
    await QuickLookAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭 QuickLook（空格预览随即停用）' : '已关闭：Hanxi 退出不影响 QuickLook，其继续独立常驻运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  const ok = await copy(repoUrl.value)
  showToast(ok ? '仓库地址已复制' : '复制失败: 剪贴板不可用')
}

async function openRepo() {
  try {
    await QuickLookAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 生命周期 ----------
useWailsEvent<DownloadProgress>('quicklook:version-download', (t) => {
  if (!t || !t.version) return
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

useWailsEvent<Snapshot>('quicklook:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

// 状态兜底轮询 + 运行时长 ticker：KeepAlive 激活/停用/卸载生命周期由 usePolling 统一守护，
// 行为与迁移前 startTimers/stopTimers 等价（激活补一帧、停用即停、绝不双开）。
usePolling(refreshStatus, 2500)
usePolling(() => {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}, 1000, { immediateFirstRun: false })

// 原 stopTimers 附带归零运行时长（停用/卸载时）；定时器停止由 usePolling 负责
onDeactivated(() => { uptimeSec.value = 0 })
onUnmounted(() => { uptimeSec.value = 0 })

onMounted(async () => {
  await Promise.all([refreshStatus(), loadVersions(), loadExtras()])
})
</script>

<template>
  <section class="page quicklook-view">
    <PageHeader title="QuickLook 预览" subtitle="托管开源空格秒预览工具 QuickLook：官方便携 zip 解压安装、JobObject 启停、命名管道优雅退出与运行状态探测；样式设置在其托盘菜单完成。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <!-- 顶部整合条：状态 + 启停按钮，一行内解决问题 -->
      <div class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <span class="ql-status-light" :class="state"></span>
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
              :disabled="busy || isRunningOrStarting"
              :title="isRunningOrStarting ? '实例已在运行' : '启动 QuickLook：驻托盘，空格预览全局生效'"
              @click="startQuickLook"
            >▶ 启动托管</button>
            <button
              class="btn btn-secondary btn-small"
              :disabled="busy || !isRunning"
              :title="isRunning ? '请求运行中的实例重载配置（命名管道 Reload）' : '仅运行中可重载'"
              @click="reload"
            >↻ 重载配置</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部实例请在 QuickLook 托盘菜单退出' : '托管退出：优先管道优雅退出，宽限后强杀兜底（键盘钩子随进程自动摘除，零残渣）'"
              @click="quitQuickLook"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <!-- 条件提示条 / 引导行 -->
      <UiBanner v-if="banner" :tone="banner.tone" class="slim">{{ banner.text }}</UiBanner>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「启动托管」拉起 QuickLook，之后在资源管理器选中任意文件按空格键即可即时预览。样式、快捷键与插件在其托盘图标菜单中调整。配置随便携标记（portable.lock）存于安装目录内，与托管版本切换互不影响。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 QuickLook（约 1~3 秒）…</div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 QuickLook</summary>
        <div class="info-body">
          <p>开源的 Windows 版 macOS「快速查看」工具（<a class="inline-link" href="https://github.com/QL-Win/QuickLook" target="_blank" rel="noopener">QL-Win/QuickLook</a>，GPL-3.0，.NET WPF）：在资源管理器、桌面、通用文件对话框乃至 Directory Opus / Everything 等第三方文件管理器中选中文件按 <kbd>空格</kbd>，即时预览图片、文档、压缩包、代码、音视频等，无需真正打开应用。</p>
          <p class="hint-dim">「按空格」能力由其主进程内的全局低级键盘钩子实现（非注入资源管理器），故进程终止即钩子自动摘除、系统零残渣；托管安装走官方便携 zip 解压（免安装、不写注册表、不提权），启停受 JobObject 管控。设置窗口无程序化唤起入口（托盘图标左键即弹菜单），因此本控制台不设「打开窗口」按钮；退出优先经命名管道投递 Quit 优雅收尾，宽限后 JobObject 强杀兜底。默认随 Hanxi 退出，可在下方改为独立常驻。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 QuickLook 独立常驻，Hanxi 退出不影响；贴合其开机常驻本性）</span></span>
        </label>
      </div>
      <div class="repo-row">
        <span class="k">GitHub 仓库</span>
        <code class="mono repo-addr">{{ repoUrl }}</code>
        <button class="link-button" @click="copyRepo">复制</button>
        <button class="link-button" @click="openRepo">浏览器打开</button>
      </div>
    </div>

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程版本 {{ releases.length }} 个</span>
          <span class="hint-dim">安装源为官方 GitHub Releases 的便携 zip（官方 digest sha256 四层校验），保布局解压落进隔离目录，不触碰系统</span>
          <span class="hint-dim">「导入本地」可把你机器上手动解压的 QuickLook 便携目录整套收纳进托管</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地便携目录</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

      <div v-if="installed.length === 0" class="empty-state first-use">
        <p>尚未安装 QuickLook —— 下载官方便携 zip 解压安装，或「导入本地便携目录」把现有解压目录收纳进来</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
          安装最新版 {{ releases[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </div>

      <div class="installed-grid">
        <div v-for="v in installed" :key="v.version" class="installed-card" :class="{ 'card-active': activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <UiStatusChip v-if="activeVersion === v.version" tone="positive">使用中</UiStatusChip>
              <UiStatusChip v-else-if="state === 'running' && runningVersion === v.version" tone="information">运行中</UiStatusChip>
              <UiStatusChip v-if="v.isImport" tone="information">本地导入</UiStatusChip>
              <UiStatusChip v-else tone="neutral">官方 zip</UiStatusChip>
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
              :title="state === 'running' && runningVersion === v.version ? '请先退出 QuickLook' : ''"
              @click="removeVersion(v)"
            >卸载</button>
          </div>
        </div>
      </div>

      <!-- 远程可用版本 -->
      <div class="section-title"><h3>远程可用版本</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
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
                <strong class="ver-name">{{ rel.version }}</strong>
                <UiStatusChip v-if="rel.isPre" tone="warning">预发布</UiStatusChip>
              </td>
              <td>
                <span v-if="statusOf(rel) === 'installed'" class="ql-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="ql-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="ql-ver-status error">失败</span>
                <span v-else class="ql-ver-status idle">可安装</span>
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
                  <span v-if="downloading[rel.version]!.stage === 'verify'">哈希校验…</span>
                  <span v-else-if="downloading[rel.version]!.stage === 'extract'">解压安装…</span>
                  <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
                </div>
                <button
                  v-if="statusOf(rel) === 'idle'"
                  class="btn btn-primary btn-small"
                  @click="download(rel)"
                >下载安装</button>
                <UiStatusChip v-if="statusOf(rel) === 'installed'" tone="information">已安装</UiStatusChip>
                <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="download(rel)">重试</a>
              </td>
            </tr>
            <tr v-if="releases.length === 0 && !loading">
              <td colspan="5" class="empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 原子类（.page/.btn 家族/.tbl/.error-box/.empty-state/.mono/.banner/.chip/.link-button/.header-row/.subtitle）
   全部由全局 components.css 接管，此处只保留本视图业务样式。 */
.quicklook-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 10px; }
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 ql- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.ql-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.ql-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.ql-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.ql-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.ql-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示与说明卡 ---------- */
.banner.slim { padding: 8px 12px; font-size: 12px; }
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }
.info-details { border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.info-body kbd { font-family: var(--font-mono); font-size: 11px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 0 4px; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

.control-panel { display: flex; align-items: center; justify-content: space-between; background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control); gap: 10px; flex-wrap: wrap; }
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: 6px; border: 1px dashed var(--color-border); }

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color var(--motion-base) ease; }
.installed-card.card-active { border-color: var(--color-primary); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; }
.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; min-width: 0; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }
.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }
.ver-name + .chip { margin-left: 4px; }

.ql-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.ql-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ql-ver-status.installed::before { background: var(--state-positive); }
.ql-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ql-ver-status.error::before { background: var(--state-danger); }
.ql-ver-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width 0.2s ease; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.retry-link { color: var(--color-primary); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
</style>
