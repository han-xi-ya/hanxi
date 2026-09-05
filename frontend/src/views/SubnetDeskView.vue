<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期（骨架基于重构共享层：
// usePolling/useWailsEvent/useAsyncAction/useConfirm/usePrompt/useClipboard + utils/format）
// 与 RustDeskView 同源孪生（LAN fork），刻意保持独立：文案/端口/配置目录不同，不合并。
import { ref, computed, onMounted, onDeactivated } from 'vue'
import * as SubnetDeskAPI from '../../bindings/hanxi/internal/modules/subnetdesk/subnetdeskservice'
import type { SDRelease, SDVersionInfo } from '../../bindings/hanxi/internal/modules/subnetdesk/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/subnetdesk/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/subnetdesk/version/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<SDRelease[]>([])
const installed = ref<SDVersionInfo[]>([])
const activeVersion = ref('')
const loading = ref(false)
const listError = ref('')
const uptimeSec = ref(0)

const { busy, run } = useAsyncAction()
const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()
const { copy } = useClipboard()

// 下载进度 map（按版本索引）
const downloading = ref<Record<string, DownloadProgress>>({})

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/ccswitch 同构）
const activeMainTab = ref('console')
const mainTabs = [
  { key: 'console', label: '🖥 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')

// 五态通用文案接 constants/status 单一来源（§9.5-5）；业务扩展话术视图自行覆写。
const stateText = computed(() => toolStateMeta(state.value).text)

const runningVersion = computed(() => snap.value?.version ?? '')

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = state.value === 'running' && runningVersion.value ? runningVersion.value : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

// 条件提示条（三个变体互斥）
const banner = computed<{ tone: 'ok' | 'warn' | 'error'; text: string } | null>(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部便携 SubnetDesk 实例（非 Hanxi 托管）。可尝试唤起其窗口；驻留托盘无窗可唤时点击其托盘图标，或在其中自行退出。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'SubnetDesk 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: 'SubnetDesk 正在运行：被控端在其窗口「LAN 设置」启用账号密码并放行防火墙 TCP 21118；关闭主窗仍驻留托盘继续托管，「退出」会终止整个进程树（断开进行中的会话）。',
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
      SubnetDeskAPI.ListReleases(),
      SubnetDeskAPI.ListInstalledVersions(),
      SubnetDeskAPI.GetActiveVersion(),
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
    snap.value = await SubnetDeskAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('subnetdesk GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: SDRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function openWindow() {
  if (busy.value) return
  const r = await run(() => SubnetDeskAPI.OpenWindow())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await refreshStatus()
}

async function quitSubnetDesk() {
  if (busy.value) return
  const r = await run(() => SubnetDeskAPI.Quit())
  showToast(r.ok ? r.data.message : `退出失败: ${getErrorMessage(r.error)}`)
  await refreshStatus()
}

// ---------- 版本管理操作 ----------
async function download(rel: SDRelease) {
  try {
    const res = await SubnetDeskAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: SDVersionInfo) {
  try {
    const ver = await SubnetDeskAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await SubnetDeskAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: SDVersionInfo) {
  const ok = await confirm({
    title: `卸载 SubnetDesk ${v.version}`,
    description: '该版本隔离目录将被删除，不可恢复。',
    details: [{ label: '配置数据', value: '连接记录与 LAN 设置恒存于 %APPDATA%\\SubnetDesk，后续版本继续共用' }],
    tone: 'danger',
    confirmLabel: '卸载',
  })
  if (!ok) return
  try {
    await SubnetDeskAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  const path = await prompt({
    title: '导入本地便携版',
    label: 'exe 完整路径或所在目录',
    description: '官方形态文件名 subnetdesk-版本-x86_64.exe；安装版/MSI 不支持导入。配置恒在 %APPDATA%\\SubnetDesk，与安装位置无关',
  })
  if (!path) return
  const r = await run(() => SubnetDeskAPI.ImportLocal(path.trim()))
  if (r.ok) {
    showToast(`已导入 SubnetDesk ${r.data.version}`)
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

// ---------- 联动开关 / 桌面快捷方式 / GitHub 仓库 ----------
const followOnExit = ref(true)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([SubnetDeskAPI.GetFollowOnExit(), SubnetDeskAPI.RepositoryURL()])
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
    await SubnetDeskAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function createShortcut() {
  try {
    await SubnetDeskAPI.CreateDesktopShortcut()
    showToast('桌面快捷方式已创建（指向当前使用版本）')
  } catch (e) {
    showToast('创建快捷方式失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  if (await copy(repoUrl.value)) {
    showToast('仓库地址已复制')
  } else {
    showToast('复制失败')
  }
}

async function openRepo() {
  try {
    await SubnetDeskAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件订阅（自动注销）与装载 ----------
useWailsEvent<DownloadProgress>('subnetdesk:version-download', (t) => {
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

useWailsEvent<Snapshot>('subnetdesk:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

onMounted(async () => {
  await Promise.all([refreshStatus(), loadVersions(), loadExtras()])
})
</script>

<template>
  <section class="page subnetdesk-view">
    <PageHeader
      title="SubnetDesk 局域网远程桌面"
      subtitle="托管 RustDesk 的 LAN 直连 fork：版本管理、JobObject 托管启停与窗口唤起；与「RustDesk 公网」互补共存。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <!-- 顶部整合条：状态 + 启停按钮，一行内解决问题 -->
      <div class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <span class="sd-status-light" :class="state"></span>
            <span class="status-word">{{ stateText }}</span>
            <template v-if="isRunningOrStarting && runningVersion">
              <span class="ver-pill">{{ runningVersion }}</span>
              <span v-if="snap?.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
            </template>
            <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
          </div>
          <div class="control-btns">
            <button
              class="btn btn-secondary btn-small"
              :disabled="busy || state === 'starting'"
              :title="state === 'running' ? '唤起窗口（驻留托盘无窗时派生新窗口）' : state === 'starting' ? '启动中…' : '启动 SubnetDesk 并打开窗口'"
              @click="openWindow"
            >🗔 打开窗口</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
              :title="isExternal ? '外部实例请在其托盘退出' : '终止托管进程树（便携版无优雅退出，进行中的会话会断开）'"
              @click="quitSubnetDesk"
            >⏻ 退出</button>
          </div>
        </div>
      </div>

      <!-- 条件提示条 / 引导行 -->
      <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：点击「打开窗口」启动。被控端在其窗口「LAN 设置」中配置用户名/密码并开启局域网发现，控制端经 IP:21118 或主机名直连，无需任何服务器。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 SubnetDesk（首次启动需解压内置负载，可能较慢）…</div>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 SubnetDesk · 与 RustDesk 如何组合</summary>
        <div class="info-body">
          <p>面向局域网/路由内网/VPN 的远程桌面（<a class="inline-link" href="https://github.com/zibo-chen/SubnetDesk" target="_blank" rel="noopener">zibo-chen/SubnetDesk</a>，AGPL-3.0，RustDesk 的独立 LAN fork）：去掉公网设备 ID 与中继服务器，改为 mDNS 自动发现 + IP 直连（默认 TCP 21118）、用户名/密码（Argon2id）与设备指纹、CIDR 白名单。版本下载自官方 GitHub Releases（官方 sha256 校验），启停受 JobObject 管控。</p>
          <p>组合分工：同局域网/VPN 内用 <strong>SubnetDesk</strong>（直连、无服务器依赖）；跨公网场景用 <strong>RustDesk</strong>（设备 ID + 信令/中继，可自建服务器）。两者<b>协议互不兼容</b>——SubnetDesk 不能接入 RustDesk 主机，反之亦然，两端需安装同一软件。</p>
          <p class="hint-dim">便携版边界：不装 Windows 服务，被控仅在进程/托盘存活期间可被接入，锁屏与安全桌面场景受限；连接记录与 LAN 设置恒存于 %APPDATA%\SubnetDesk，多版本共享。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（关闭后 Hanxi 退出完全不影响该工具；被控常驻场景建议关闭本项或改用系统安装版）</span></span>
        </label>
        <button class="btn btn-secondary btn-small" @click="createShortcut">🖥 创建桌面快捷方式</button>
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
          <span class="hint-dim">便携包为官方单文件 packer exe（subnetdesk-版本-x86_64.exe，GitHub digest 校验，下载即安装）；或「导入本地」收纳你机器上已有的便携版</span>
          <span class="hint-dim">便携 exe 首次运行自动解压到 %LOCALAPPDATA%\SubnetDesk；配置恒在 %APPDATA%\SubnetDesk，与安装位置无关</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="importLocal" :disabled="busy">⇥ 导入本地便携版</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
            {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

      <UiEmptyState v-if="installed.length === 0">
        <p>尚未安装 SubnetDesk —— 下载官方便携版，或「导入本地便携版」把现有下载收纳进来</p>
        <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
          下载最新版 {{ releases[0].version }}
        </button>
        <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
      </UiEmptyState>

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
              :title="state === 'running' && runningVersion === v.version ? '请先退出 SubnetDesk' : ''"
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
                <span v-if="rel.isPre" class="badge badge-pre">预发布</span>
              </td>
              <td>
                <!-- 类名刻意用 sd-ver-status（含 sd- 前缀）——防与全局/其他视图状态点样式碰撞（markeron 垂直字体事故教训） -->
                <span v-if="statusOf(rel) === 'installed'" class="sd-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="sd-ver-status downloading">下载中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="sd-ver-status error">失败</span>
                <span v-else class="sd-ver-status idle">可安装</span>
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
                  <span v-if="['verify', 'install'].includes(downloading[rel.version]!.stage)">校验安装中…</span>
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
                <span v-if="statusOf(rel) === 'installed'" class="installed-tag">已安装</span>
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
/* 共享层已接管：.page/.header-row/.subtitle/.error-box/.btn 家族/.tbl/.mono/.link-button/
   .empty-state/.banner(UiBanner)/.main-tab-nav(MainTabNav)——此处只留本视图业务样式。 */
.subnetdesk-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 顶部整合控制条 ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 信号灯类名带 sd- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.sd-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.sd-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.sd-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.sd-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.sd-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 提示行与说明卡 ---------- */
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; }
.info-details { border: 1px solid var(--color-border); border-radius: 8px; background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 版本区（业务专属壳） ---------- */
.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }
.hint-dim { color: var(--color-text-subtle); }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: 6px; border: 1px dashed var(--color-border); }

/* ---------- 已安装卡片 ---------- */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; transition: border-color var(--motion-base) ease;
}
.installed-card.card-active { border-color: var(--color-primary); }
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* ---------- 远程表格 ---------- */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; overflow-x: auto; }
.ver-name { font-family: var(--font-mono); }

.sd-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.sd-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.sd-ver-status.installed::before { background: var(--state-positive); }
.sd-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.sd-ver-status.error::before { background: var(--state-danger); }
.sd-ver-status.idle::before { background: var(--color-text-subtle); }

/* 「已安装」表内标记：区别于全局 .btn-ghost（悬停幽灵按钮）的静态标签态 */
.installed-tag {
  display: inline-flex; align-items: center; padding: 4px 12px; font-size: 12px;
  border-radius: var(--radius-control); border: 1px solid var(--color-border);
  background: var(--surface-hover); color: var(--color-text-muted);
}

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-fast) linear; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.retry-link { color: var(--color-primary); font-size: 12px; cursor: pointer; margin-left: 8px; }
.retry-link:hover { text-decoration: underline; }

/* ---------- 联动与辅助设置卡 ---------- */
.extras-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 8px; padding: 10px 14px; display: flex; flex-direction: column; gap: 8px; }
.extras-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--color-text); cursor: pointer; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }
.repo-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); flex-wrap: wrap; }
.repo-row .k { color: var(--color-text-subtle); flex-shrink: 0; }
.repo-addr { flex: 1; min-width: 220px; }
</style>
