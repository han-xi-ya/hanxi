<script setup lang="ts">
// Keyviz 键显托管工作台：控制台（启停）+ 版本管理（MSI 管理提取安装、导入本地、卸载）
import { ref, computed, onMounted, onDeactivated, onUnmounted } from 'vue'
import * as KeyvizAPI from '../../bindings/hanxi/internal/modules/keyviz/keyvizservice'
import type { KeyvizRelease, KeyvizVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/keyviz/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/keyviz/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/keyviz/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { loadManagedVersions } from '../composables/loadManagedVersions'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<KeyvizRelease[]>([])
const installed = ref<KeyvizVersionInfo[]>([])
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

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/piclite 同构）
const activeMainTab = ref<string>('console')
const MAIN_TABS = [
  { key: 'console', label: '⌨️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 五态通用文案接 constants/status 单一来源（§9.5-5）；业务扩展话术视图自行覆写。
const stateText = computed(() => toolStateMeta(state.value).text)

const runningVersion = computed(() => snap.value?.version ?? '')

// 条件提示条（三个变体互斥）
const banner = computed<{ tone: 'ok' | 'warn' | 'error'; text: string } | null>(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部 Keyviz 实例（非 Hanxi 托管）。按键可视化正在生效；退出与样式设置都在其托盘图标菜单（左键即弹）中完成。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'Keyviz 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: 'Keyviz 正在运行：全局按键/鼠标可视化即时生效。样式设置请左键点击系统托盘图标 → Settings（上游未提供程序化唤窗入口）。配置存于 %APPDATA%\\org.keyviz。',
    }
  }
  return null
})

// ---------- 数据加载 ----------
async function loadVersions() {
  await loadManagedVersions({
    remote: KeyvizAPI.ListReleases,
    local: KeyvizAPI.ListInstalledVersions,
    active: KeyvizAPI.GetActiveVersion,
    setRemote: value => { releases.value = value },
    setLocal: value => { installed.value = value },
    setActive: value => { activeVersion.value = value },
    setLoading: value => { loading.value = value },
    setError: value => { listError.value = value },
  })
}

async function refreshStatus() {
  try {
    snap.value = await KeyvizAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('keyviz GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: KeyvizRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function startKeyviz() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await KeyvizAPI.StartKeyviz()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function quitKeyviz() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await KeyvizAPI.Quit()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(`退出失败: ${getErrorMessage(e)}`)
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

// ---------- 版本管理操作 ----------
async function download(rel: KeyvizRelease) {
  try {
    const res = await KeyvizAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`安装失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: KeyvizVersionInfo) {
  try {
    const ver = await KeyvizAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await KeyvizAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}


async function openConfigDir() {
  try {
    await KeyvizAPI.OpenConfigDir()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: KeyvizVersionInfo) {
  const ok = await confirm({
    title: `确定卸载 Keyviz ${v.version}？`,
    description: '该版本托管目录将被删除，不可恢复。\n（你的 %APPDATA%\\org.keyviz 样式配置不受影响，后续版本继续共用）',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await KeyvizAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '请输入本机 Keyviz 安装目录完整路径（含 keyviz.exe，如 C:\\Program Files\\keyviz）',
    description: '提示：配置恒在 %APPDATA%\\org.keyviz，与安装位置无关',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await KeyvizAPI.ImportLocal(path.trim())
    showToast(`已导入 Keyviz ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// ---------- 联动开关 / GitHub 仓库 ----------
const followOnExit = ref(false)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([KeyvizAPI.GetFollowOnExit(), KeyvizAPI.RepositoryURL()])
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
    await KeyvizAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，Keyviz 继续独立运行（下次启动生效）')
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
    await KeyvizAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 生命周期 ----------
useWailsEvent<DownloadProgress>('keyviz:version-download', (t) => {
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

useWailsEvent<Snapshot>('keyviz:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

// 状态兜底轮询 + 运行时长 ticker：KeepAlive 生命周期由 usePolling 统一守护（与迁移前等价）
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
  <section class="page keyviz-view">
    <PageHeader title="Keyviz 键显" subtitle="托管开源按键/鼠标可视化工具 Keyviz：官方 MSI 免安装提取、JobObject 启停与运行状态探测；样式设置在其托盘菜单 → Settings 完成。">
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
            <span class="kv-status-light" :class="state"></span>
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
              :title="isRunningOrStarting ? '实例已在运行' : '启动 Keyviz：驻托盘并全局可视化按键/鼠标'"
              @click="startKeyviz"
            >▶ 启动可视化</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部实例请在 Keyviz 托盘菜单退出' : '托管退出：直接终止实例（样式配置即时写盘不受影响）'"
              @click="quitKeyviz"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <!-- 条件提示条 / 引导行 -->
      <UiBanner v-if="banner" :tone="banner.tone" class="slim">{{ banner.text }}</UiBanner>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「启动可视化」拉起 Keyviz，按键/鼠标特效即刻全局生效；样式、快捷键与过滤器在其托盘菜单 → Settings 中调整。配置恒存于 %APPDATA%\org.keyviz，与托管版本切换无关。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Keyviz（约 1~3 秒）…</div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 Keyviz</summary>
        <div class="info-body">
          <p>开源跨平台按键/鼠标实时可视化工具（<a class="inline-link" href="https://github.com/mulaRahul/keyviz" target="_blank" rel="noopener">mulaRahul/keyviz</a>，GPL-3.0，Tauri 2），录教程、演示、结对编程时向观众展示你按下的组合键：<kbd>Alt</kbd> + <kbd>Drag</kbd>、滚轮、鼠标点击皆可显示，样式/位置/动画/历史全可定制。</p>
          <p class="hint-dim">上游只发布安装器没有便携版：托管安装走 msiexec 管理提取（不写注册表、不提权），启停受 JobObject 管控；Keyviz 启动即驻托盘，设置窗口无程序化唤起入口（托盘图标左键即弹菜单），退出无外部优雅通道，托管「退出」为直接终止（配置修改即节流写盘，不丢设置）。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出完全不影响该工具）</span></span>
        </label>
        <button class="btn btn-secondary btn-small" title="打开 Keyviz 用户数据目录（%APPDATA%\org.keyviz，样式配置）" @click="openConfigDir">🗂 数据目录</button>
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
          <span class="hint-dim">安装源为官方 GitHub Releases 的 Windows MSI（官方 digest sha256 四层校验），经 msiexec 管理提取落进隔离目录，不触碰系统</span>
          <span class="hint-dim">「导入本地」可把你机器上已安装的 Keyviz（Program Files\keyviz）收纳进托管；配置在 %APPDATA%，两者互不影响</span>
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
        <p>尚未安装 Keyviz —— 下载官方 MSI 免安装提取，或「导入本地安装」把现有 Keyviz 收纳进来</p>
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
              <UiStatusChip v-else tone="neutral">官方 MSI</UiStatusChip>
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
              :title="state === 'running' && runningVersion === v.version ? '请先退出 Keyviz' : ''"
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
                <span v-if="statusOf(rel) === 'installed'" class="kv-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="kv-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="kv-ver-status error">失败</span>
                <span v-else class="kv-ver-status idle">可安装</span>
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
                  <span v-else-if="downloading[rel.version]!.stage === 'extract'">管理提取安装…</span>
                  <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
                </div>
                <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                  <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
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
/* 原子类由全局 components.css 接管，此处只保留本视图业务样式。 */
.keyviz-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条（control-bar 四件套由全局原子接管；
   本视图原 gap:10px 散差按标准形 gap:8px 定档删除） ---------- */
/* 信号灯类名带 kv- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.kv-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.kv-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.kv-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.kv-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.kv-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
/* status-word/ver-pill/pid-tag/uptime-tag/control-btns 由全局原子接管 */

/* ---------- 提示与说明卡（.banner.slim 由全局原子接管，模板挂 class="slim"；
   hint-line/info-details/info-summary/info-body p 等由全局原子接管） ---------- */
.info-body kbd { font-family: var(--font-mono); font-size: var(--text-xs); background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 0 4px; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* 补差 against 全局原子 .control-panel：本视图面板加宽间距并允许换行 */
.control-panel { gap: 10px; flex-wrap: wrap; }
/* meta-info/btn-group、section-title h3/empty-hint 由全局原子接管 */

/* ---------- 已安装卡片（installed-grid/installed-card(.card-active)/inst-card-top/inst-badges/
   inst-meta/meta-line .k/inst-actions/ver-tag 由全局原子接管；
   本视图原 minmax(340px) 散差按标准形 minmax(320px) 定档删除） ---------- */
/* 补差 against 全局原子 .meta-line：窄列允许收缩 */
.meta-line { min-width: 0; }

/* ---------- 远程表格（table-container/ver-name 由全局原子接管） ---------- */
.ver-name + .chip { margin-left: 4px; }

.kv-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.kv-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.kv-ver-status.installed::before { background: var(--state-positive); }
.kv-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.kv-ver-status.error::before { background: var(--state-danger); }
.kv-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管 */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管） ---------- */
</style>
