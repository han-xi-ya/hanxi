<script setup lang="ts">
// 状态 / 版本管理 / 下载进度 / 时长 ticker / 生命周期
import { ref, computed, onMounted } from 'vue'
import * as TBAPI from '../../bindings/hanxi/internal/modules/translucenttb/translucenttbservice'
import type { TBRelease, TBVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/translucenttb/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/translucenttb/instance/models'
import type { ControlOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/translucenttb/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { loadManagedVersions } from '../composables/loadManagedVersions'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDate, fmtDuration } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'

// ---------- 状态 ----------
const snap = ref<Snapshot | null>(null)
const releases = ref<TBRelease[]>([])
const installed = ref<TBVersionInfo[]>([])
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

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/everything 同构）
const activeMainTab = ref<'console' | 'versions'>('console')
const mainTabs = [
  { key: 'console', label: '🌫️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态 ----------
const state = computed(() => snap.value?.state ?? '')
const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
const isExternal = computed(() => state.value === 'external')
const canReset = computed(() => state.value === 'running' || isExternal.value)

// 五态通用文案接 constants/status 单一来源（§9.5-5）；业务扩展话术视图自行覆写。
const stateText = computed(() => toolStateMeta(state.value).text)

const runningVersion = computed(() => snap.value?.version ?? '')

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirTarget = computed(() => {
  const prefer = state.value === 'running' && runningVersion.value ? runningVersion.value : activeVersion.value
  return installed.value.find(v => v.version === prefer) ?? installed.value[0] ?? null
})

// 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义
const banner = computed(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部 TranslucentTB 实例（非 Hanxi 托管）。可重设任务栏状态；如需彻底退出请在 TranslucentTB 托盘菜单操作。',
    } as const
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'TranslucentTB 异常退出' } as const
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: 'TranslucentTB 正在运行：任务栏透明样式在系统托盘图标菜单中设置（首次启动需在欢迎窗口确认许可）。退出进程后任务栏自动还原。',
    } as const
  }
  return null
})

// ---------- 数据加载 ----------
async function loadVersions() {
  await loadManagedVersions({
    remote: TBAPI.ListReleases,
    local: TBAPI.ListInstalledVersions,
    active: TBAPI.GetActiveVersion,
    setRemote: value => { releases.value = value },
    setLocal: value => { installed.value = value },
    setActive: value => { activeVersion.value = value },
    setLoading: value => { loading.value = value },
    setError: value => { listError.value = value },
  })
}

async function refreshStatus() {
  try {
    snap.value = await TBAPI.GetStatus()
  } catch (e) {
    // 轮询静默失败：保留上次快照即可
    console.warn('translucenttb GetStatus failed:', getErrorMessage(e))
  }
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: TBRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// ---------- 控制操作 ----------
async function startTool() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await TBAPI.Start()
    showToast(out.message)
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
    await refreshStatus()
  } finally {
    busy.value = false
  }
}

async function resetState() {
  if (busy.value) return
  busy.value = true
  try {
    const out: ControlOutcome = await TBAPI.ResetState()
    showToast(out.message)
  } catch (e) {
    showToast(getErrorMessage(e))
  } finally {
    busy.value = false
  }
}

async function quitTool() {
  if (busy.value) return
  busy.value = true
  try {
    const out: QuitOutcome = await TBAPI.Quit()
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
async function download(rel: TBRelease) {
  try {
    const res = await TBAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActive(v: TBVersionInfo) {
  try {
    const ver = await TBAPI.SetActiveVersion(v.version)
    activeVersion.value = ver
    showToast(`已将 ${ver} 设为使用版本`)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
  }
}

async function openDir(path: string) {
  try {
    await TBAPI.OpenDir(path)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: TBVersionInfo) {
  // 危险操作经全局可访问确认框（useConfirm 单例）。
  // TranslucentTB 的配置 settings.json 就在版本目录内，卸载连配置一起删——如实预告。
  const accepted = await confirm({
    title: `确定卸载 TranslucentTB ${v.version}？`,
    description: '该版本隔离目录将被删除，不可恢复。\n注意：你的透明样式配置（settings.json）就在该目录内，会一并删除。如需保留请先备份。',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await TBAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

async function importLocal() {
  // 路径输入经全局输入框（usePrompt 单例），提示文案如实说明整套迁移语义
  const path = await prompt({
    title: '导入本地 TranslucentTB',
    description: '提示：配置（settings.json）跟着安装目录走，导入时整套迁入托管目录',
    label: '便携版目录完整路径（含 TranslucentTB.exe 与伴生 DLL）',
  })
  if (!path) return
  try {
    busy.value = true
    const info = await TBAPI.ImportLocal(path.trim())
    showToast(`已导入 TranslucentTB ${info.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

// 运行时长秒表（每秒从快照 startedAt 重算；KeepAlive 停用期间暂停）
function uptimeTick() {
  if (snap.value?.state === 'running' && snap.value.startedAt) {
    const started = new Date(snap.value.startedAt).getTime()
    if (!Number.isNaN(started)) {
      uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
  }
}

// ---------- 联动开关 / GitHub 仓库 ----------
const followOnExit = ref(false)
const repoUrl = ref('')

async function loadExtras() {
  try {
    const [f, u] = await Promise.all([TBAPI.GetFollowOnExit(), TBAPI.RepositoryURL()])
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
    await TBAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭该工具（任务栏透明随之消失）' : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

async function copyRepo() {
  // 剪贴板两级策略已收编进 useClipboard
  const ok = await copy(repoUrl.value)
  showToast(ok ? '仓库地址已复制' : '复制失败')
}

async function openRepo() {
  try {
    await TBAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 订阅与生命周期 ----------
useWailsEvent<DownloadProgress>('translucenttb:version-download', (t) => {
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

useWailsEvent<Snapshot>('translucenttb:instance-state', (s) => {
  if (!s) return
  snap.value = s
  if (s.state !== 'running') uptimeSec.value = 0
})

// 轮询（usePolling 内置 KeepAlive 激活/停用/卸载契约；首跑即完成进页状态刷新）
usePolling(refreshStatus, 2500)
usePolling(uptimeTick, 1000)

onMounted(() => {
  loadVersions()
  loadExtras()
})
</script>

<template>
  <section class="page ttb-view">
    <PageHeader title="TranslucentTB" subtitle="托管任务栏透明工具：版本管理、JobObject 启停与任务栏状态重设。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
    <!-- 顶部整合条：状态 + 操作按钮，一行内解决问题 -->
    <div class="control-bar">
      <div class="control-top">
        <div class="control-status">
          <span class="tb-status-light" :class="state"></span>
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
            :disabled="busy || isRunningOrStarting || isExternal || !openDirTarget"
            :title="isRunningOrStarting ? '已在运行' : isExternal ? '外部实例已在运行' : openDirTarget ? '启动 TranslucentTB（驻系统托盘）' : '尚未安装，请先在「版本管理」下载'"
            @click="startTool"
          >🌫️ 启动</button>
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || !canReset"
            :title="canReset ? '任务栏外观异常时重放配置（等价托盘菜单 Reset dynamic state）' : '实例未在运行'"
            @click="resetState"
          >🪄 重设任务栏状态</button>
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || !openDirTarget"
            title="打开版本安装目录（透明样式配置 settings.json 就在这里，可用编辑器直接修改）"
            @click="openDir(openDirTarget!.dir)"
          >🗂 安装目录</button>
          <button
            class="btn btn-danger-outline btn-small"
            :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
            :title="isExternal ? '外部实例请在 TranslucentTB 托盘菜单退出' : '优雅退出（保存设置后进程退出，任务栏还原）'"
            @click="quitTool"
          >⏻ 退出</button>
        </div>
      </div>
    </div>

    <!-- 条件提示条 / 引导行 -->
    <UiBanner v-if="banner" :tone="banner.tone" class="slim">{{ banner.text }}</UiBanner>
    <div v-else-if="state === 'stopped'" class="hint-line">
      尚未运行：点击「🌫️ 启动」。样式设置在该程序的系统托盘图标菜单里完成（首次启动会弹欢迎窗口需先确认许可）。便携版仅支持 Windows 11，且依赖系统已装的 WinUI / VCLibs 框架包。
    </div>
    <div v-else-if="state === 'starting'" class="hint-line">正在拉起 TranslucentTB（约 1~3 秒）…</div>

    <!-- 说明卡（可折叠） -->
    <details class="info-details">
      <summary class="info-summary">什么是 TranslucentTB</summary>
      <div class="info-body">
        <p>Windows 任务栏透明/模糊/亚克力效果工具（<a class="inline-link" href="https://github.com/TranslucentTB/TranslucentTB" target="_blank" rel="noopener">TranslucentTB/TranslucentTB</a>，GPL-3.0）。它通过向资源管理器注入组件实时改写任务栏外观，全部样式设置都在系统托盘图标菜单（XAML 飞控）中完成——上游没有独立设置窗口。</p>
        <p class="hint-dim">版本下载自官方 GitHub Releases（portable-x64，sha256 四层校验），启停受 JobObject 管控。「🪄 重设任务栏状态」等价上游托盘菜单的 Reset dynamic state：任务栏被 explorer 重启、换肤工具改动弄花时点一下即可重放配置。退出进程后任务栏自动还原默认外观。</p>
      </div>
    </details>
    </div>

    <!-- 联动与辅助设置卡 -->
    <div class="extras-card">
      <div class="extras-row">
        <label class="toggle-label">
          <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
          <span>随 Hanxi 一起关闭 <span class="hint-dim">（开启后 Hanxi 退出连带退出该工具，任务栏透明消失）</span></span>
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
        <span class="hint-dim">便携包下载自 GitHub Releases（TranslucentTB-portable-x64.zip，官方 digest 校验）；或「导入本地」把你机器上已有的便携版整套收纳进来</span>
        <span class="hint-dim">注意：透明样式配置 settings.json 随版本目录走——多版本并存时各版本配置互相独立</span>
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
      <p>尚未安装 TranslucentTB —— 下载官方便携版，或「导入本地安装」把现有便携版收纳进来</p>
      <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
        下载最新版 {{ releases[0].version }}
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
            :title="state === 'running' && runningVersion === v.version ? '请先退出 TranslucentTB' : ''"
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
              <!-- 类名刻意用 tb- 前缀——App.vue 全局样式有 .status-dot（7px），防碰撞压扁徽标 -->
              <span v-if="statusOf(rel) === 'installed'" class="tb-ver-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="tb-ver-status downloading">下载中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="tb-ver-status error">失败</span>
              <span v-else class="tb-ver-status idle">可安装</span>
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
              <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
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
            <td colspan="5" class="empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
          </tr>
        </tbody>
      </table>
    </div>
    </div>
  </section>
</template>

<style scoped>
.ttb-view { display: flex; flex-direction: column; gap: 10px; }
/* 页头/副标题/错误框/主选项卡：由 PageHeader、MainTabNav 与 components.css 全局原子接管 */
.tab-body { display: flex; flex-direction: column; gap: 10px; }
/* UiBanner 紧凑变体：.banner.slim 由 components.css 全局原子接管（模板挂 class="slim"） */

/* ---------- 顶部整合控制条（control-bar 四件套由全局原子接管；
   本视图原 gap:10px 散差按标准形 gap:8px 定档删除） ---------- */
/* 信号灯类名带 tb- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.tb-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.tb-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.tb-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.tb-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.tb-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
/* status-word/ver-pill/pid-tag/uptime-tag/control-btns 由全局原子接管 */

/* ---------- 提示与说明卡（banner 家族、hint-line、info-details、info-summary、info-body p 等由全局原子接管） ---------- */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* 通用按钮 .btn 家族已由 components.css 全局原子提供 */

/* control-panel/meta-info/btn-group、section-title h3/empty-hint 由全局原子接管 */
/* .empty-state 空态全局原子接管（components.css） */

/* ---------- 已安装卡片（installed-grid/installed-card(.card-active)/inst-card-top/inst-badges/ver-tag 由全局原子接管；
   本视图原 minmax(340px) 散差按标准形 minmax(320px) 定档删除） ---------- */
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

/* inst-meta/meta-line(.k)/inst-actions、table-container/ver-name 由全局原子接管 */
/* .mono 基样式与字号由 components.css 全局原子接管 */

/* ---------- 远程表格（.tbl 全局原子接管） ---------- */

.tb-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.tb-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.tb-ver-status.installed::before { background: var(--state-positive); }
.tb-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.tb-ver-status.error::before { background: var(--state-danger); }
.tb-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管 */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管） ---------- */
/* .link-button 全局原子接管 */
</style>
