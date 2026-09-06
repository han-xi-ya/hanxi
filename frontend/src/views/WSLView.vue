<script setup lang="ts">
// WSL 子系统：就绪体检（流式逐项点亮）+ 官方版本管理（Releases × 本机关系）
// + 白名单提权操作（一键开启/更新/装发行版/正规卸载/组件还原）。
// 体检走 wsl:readiness 事件分相推送：先全量 pending 骨架，system/wsl/net 三源
// 并发先到先点亮，done 收口终版报告——骨架 key 与后端 BuildItems 有顺序互锁。
import { ref, computed, watch, onMounted } from 'vue'
import * as WSLAPI from '../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { CheckItem, DistroOption, Report } from '../../bindings/hanxi/internal/modules/wsl/readiness/models'
import type { DownloadProgress, ReadinessUpdate } from '../../bindings/hanxi/internal/modules/wsl/models'
import type { Asset, Overview, Release } from '../../bindings/hanxi/internal/modules/wsl/releases/models'
import { useToast } from '../composables/useToast'
import { useConfirm } from '../composables/useConfirm'
import { useClipboard } from '../composables/useClipboard'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiProgressBar from '../components/ui/UiProgressBar.vue'

const { showToast } = useToast()
const { confirm } = useConfirm()
const { copy } = useClipboard()

const activeMainTab = ref<'console' | 'versions'>('console')
const mainTabs = [
  { key: 'console', label: '🐧 就绪检测' },
  { key: 'versions', label: '📦 版本与发行版' },
]

// ---------- 就绪体检（流式） ----------
// 骨架清单 = 后端 BuildItems 固定顺序镜像（internal/modules/wsl/readiness/evaluate.go），
// 两边改动必须同步，后端 readiness key 集合有单测回归锁。
const CHECK_SKELETON: Array<{ key: string; label: string }> = [
  { key: 'build', label: '系统版本' },
  { key: 'arch', label: 'CPU 架构' },
  { key: 'virt', label: 'CPU 虚拟化 (VT-x / AMD-V)' },
  { key: 'hypervisor', label: '虚拟机监控程序 (Hyper-V/VBS)' },
  { key: 'feature', label: '可选功能 (虚拟机平台)' },
  { key: 'vbs', label: '基于虚拟化的安全性 (VBS)' },
  { key: 'store', label: 'Microsoft Store' },
  { key: 'form', label: '安装形态 (MSIX/MSI/启动器)' },
  { key: 'net', label: 'GitHub 安装通道' },
  { key: 'wsl', label: 'WSL 本体' },
]

const arrived = ref<Record<string, CheckItem>>({})
const report = ref<Report | null>(null)
const streaming = ref(false)
const loadError = ref('')
// busyOp 记录当前提权操作名：按钮全局互斥禁用 + 仅本按钮显示进行中。
const busyOp = ref('')
// 重启引导纯检测驱动：只认系统 CBS 待重启台账（report.rebootPending），
// 与"刚做过什么操作"无关——真欠重启才提示，重启完自动消失。
const rebootNudge = computed(() => !!report.value?.rebootPending)

// 发行版安装被拦的预告（点之前就说清，别等 UAC 弹了再失败）：
// 虚拟机平台未启用，或已启用但 CBS 台账欠重启——WSL2 此刻都起不了虚拟机。
// 与后端 virtualizationGate 同源判据；体检未出结果时不猜测（返回空不显示）。
const distroBlockedReason = computed(() => {
  const r = report.value
  if (!r || !r.wslVersion) return ''
  if (!r.vmPlatformEnabled) return '「虚拟机平台」尚未启用，WSL2 承载不了发行版——现在点安装注定失败。请先回「🐧 就绪检测」页执行「🚀 一键开启」或「▶️ 开启虚拟机平台」。'
  if (r.rebootPending) return '「虚拟机平台」已启用但还没重启生效，WSL2 此刻起不了虚拟机——现在装发行版注定失败。重启一次再回来挑系统即可（注意：Task Manager 显示的"虚拟化已启用"是 BIOS 硬件位，与这个 Windows 功能开关是两回事）。'
  return ''
})

const verdictTone = computed<'ok' | 'warn' | 'error' | 'info'>(() => {
  switch (report.value?.verdict) {
    case 'ready': return 'ok'
    case 'attention': return 'warn'
    case 'blocked': return 'error'
    default: return 'info'
  }
})

const chipTone = (state: string): 'positive' | 'warning' | 'danger' | 'information' => {
  switch (state) {
    case 'ok': return 'positive'
    case 'warn': return 'warning'
    case 'bad': return 'danger'
    default: return 'information'
  }
}

const stateWord = (state: string): string => {
  switch (state) {
    case 'ok': return '通过'
    case 'warn': return '注意'
    case 'bad': return '阻塞'
    default: return '信息'
  }
}

function distroRunningChip(state: string): 'positive' | 'neutral' | 'information' {
  const s = state.toLowerCase()
  if (s.includes('running') || state.includes('正在运行')) return 'positive'
  if (s.includes('stopped') || state.includes('已停止')) return 'neutral'
  return 'information'
}

useWailsEvent<ReadinessUpdate>('wsl:readiness', (u) => {
  if (!u) return
  if (u.stage === 'error') {
    loadError.value = u.error || '就绪体检执行失败'
    streaming.value = false
    return
  }
  for (const it of u.items ?? []) {
    arrived.value = { ...arrived.value, [it.key]: it }
  }
  if (u.stage === 'done' && u.report) {
    report.value = u.report
    streaming.value = false
  }
})

async function startCheck() {
  loadError.value = ''
  report.value = null
  arrived.value = {}
  streaming.value = true
  try {
    await WSLAPI.StartReadiness()
  } catch (e) {
    loadError.value = `启动体检失败: ${getErrorMessage(e)}`
    streaming.value = false
  }
}

// ---------- 白名单提权操作 ----------
async function runOp(opts: {
  name: string
  title: string
  desc: string
  tone?: 'default' | 'warning' | 'danger'
  invoke: () => PromiseLike<unknown>
}) {
  if (busyOp.value) return
  const accepted = await confirm({
    title: opts.title,
    description: `${opts.desc}\n\n该操作需管理员权限：随后会弹出系统 UAC 授权窗口（并打开提权命令行窗口显示进度），请在窗口中确认继续。`,
    tone: opts.tone ?? 'warning',
  })
  if (!accepted) return
  busyOp.value = opts.name
  try {
    const out = (await opts.invoke()) as { message?: string }
    showToast(out.message || '操作已完成')
    await startCheck() // 操作后流式复查：若真有变更欠重启，CBS 台账会让引导条自己出现
  } catch (e) {
    showToast(`${opts.title}失败: ${getErrorMessage(e)}`)
  } finally {
    busyOp.value = ''
  }
}

const installWsl = () => runOp({
  name: 'install', title: '一键开启 WSL（不装发行版）？',
  desc: '执行 wsl --install --no-distribution：只安装 WSL 本体并启用「虚拟机平台」，绝不自动捆绑任何 Linux 发行版——系统请重启后到「版本与发行版」页手动挑选。',
  invoke: WSLAPI.InstallWsl,
})
const updateWsl = () => runOp({
  name: 'update', title: '更新 WSL 本体？',
  desc: '执行 wsl --update：经默认（商店）通道升级到最新正式版。',
  invoke: WSLAPI.UpdateWsl,
})
const updateWeb = () => runOp({
  name: 'update-web', title: 'GitHub 直连更新？',
  desc: '执行 wsl --update --web-download：商店通道不通时强制从 GitHub 下载更新。',
  invoke: WSLAPI.UpdateWslWebDownload,
})
const setDefaultV2 = () => runOp({
  name: 'set-default', title: '将新装发行版默认设为 WSL2？',
  desc: '执行 wsl --set-default-version 2：此后安装的所有发行版默认使用 WSL2（完整 Linux 内核）。',
  invoke: WSLAPI.SetDefaultVersion2,
})
const enableFeatures = () => runOp({
  name: 'enable-features', title: '开启虚拟机平台组件？',
  desc: '经 DISM 启用「虚拟机平台」与「Windows Subsystem for Linux」可选功能（/norestart，重启后生效）。这是 WSL2 的硬前提；已装 WSL 的话重启后即可创建/启动发行版。',
  invoke: WSLAPI.EnableWslFeatures,
})
const uninstallWsl = () => runOp({
  name: 'uninstall', title: '卸载 WSL？',
  tone: 'danger',
  desc: '正规双路卸载：① 若检出 MSI 系统版，弹出其官方卸载向导（msiexec /X）；② 移除当前用户的 MSIX 包（Remove-AppxPackage）。'
    + '\n只走官方卸载器、不强删注册表；完成后如实报告 Lxss 发行版注册残留。'
    + '\n可选功能（虚拟机平台）不在本操作内——如需彻底还原系统，再执行「关闭虚拟机平台」。',
  invoke: WSLAPI.UninstallWsl,
})
const disableFeatures = () => runOp({
  name: 'disable-features', title: '关闭虚拟机平台组件？',
  tone: 'danger',
  desc: '经 DISM 关闭「虚拟机平台」与「Windows Subsystem for Linux」两个 Windows 可选功能（/norestart，重启后生效）。'
    + '\n注意：虚拟机平台同时是 Hyper-V 轻量栈、Android 模拟器（如 WSA/部分厂商模拟器）、沙盒等功能的地基——'
    + '如果你还在用这些东西，请不要执行本操作。',
  invoke: WSLAPI.DisableWslFeatures,
})

async function openPowerSettings() {
  try {
    await WSLAPI.OpenPowerSettings()
  } catch (e) {
    showToast(`打开系统设置失败: ${getErrorMessage(e)}`)
  }
}

// ---------- 版本与发行版 ----------
const overview = ref<Overview | null>(null)
const relLoading = ref(false)
const relError = ref('')
const online = ref<DistroOption[]>([])
const onlineLoading = ref(false)
const onlineError = ref('')

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

async function loadOnline() {
  onlineLoading.value = true
  onlineError.value = ''
  try {
    online.value = (await WSLAPI.ListOnlineDistros()) ?? []
  } catch (e) {
    online.value = []
    onlineError.value = `获取在线发行版清单失败: ${getErrorMessage(e)}\n该清单由本机 wsl.exe 提供：未装 WSL 或版本过旧时，请先在「就绪检测」页完成一键开启并重启。`
  } finally {
    onlineLoading.value = false
  }
}

// 首次切到版本页时才拉清单（在线清单依赖 wsl.exe，冷页避免双重探测）。
watch(activeMainTab, (tab) => {
  if (tab === 'versions' && online.value.length === 0 && !onlineLoading.value && !onlineError.value) {
    loadOnline()
  }
})

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
  const m = report.value?.machineArch
  return !!m && platform.toLowerCase() === m.toLowerCase()
}
function assetsShown(rel: Release): Asset[] {
  const list = rel.assets ?? []
  const m = report.value?.machineArch
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

async function installDistro(opt: DistroOption) {
  await runOp({
    name: `distro-${opt.id}`,
    title: `安装发行版 ${opt.id}？`,
    tone: 'default',
    desc: `执行 wsl --install -d ${opt.id}：下载安装后首次进入该发行版需创建 Linux 用户名与密码。`,
    invoke: () => WSLAPI.InstallDistro(opt.id),
  })
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
  startCheck()
  loadReleases()
})
</script>

<template>
  <section class="page wsl-view">
    <PageHeader title="WSL2" subtitle="Windows Subsystem for Linux：就绪体检、GitHub 通道诊断、官方版本管理、发行版安装与正规卸载。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <!-- 控制台 Tab：就绪体检 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <div v-if="loadError" class="error-box">{{ loadError }}</div>

      <!-- 总体结论条（done 前显示进度语义） -->
      <UiBanner v-if="report" :tone="verdictTone" class="verdict-banner">
        <div class="verdict-line">
          <strong>{{ report.verdictTitle }}</strong>
          <span v-if="report.collectedAt" class="hint-dim">采集于 {{ report.collectedAt }}</span>
        </div>
        <div class="verdict-detail">{{ report.verdictDetail }}</div>
      </UiBanner>
      <div v-else-if="streaming && !loadError" class="hint-line">正在逐项体检：系统探针、本机运行时与 GitHub 通道三路并发，先到先点亮…</div>

      <!-- 操作条：白名单提权操作 -->
      <div class="control-bar">
        <div class="control-top">
          <div class="control-status">
            <UiStatusChip v-if="report && report.wslVersion" tone="positive">WSL {{ report.wslVersion }}</UiStatusChip>
            <UiStatusChip v-else-if="report" tone="neutral">WSL 未安装</UiStatusChip>
            <span v-if="busyOp" class="hint-dim">⏳ 提权窗口正在执行，请在弹窗中查看进度…</span>
          </div>
          <div class="control-btns">
            <button class="btn btn-primary btn-small" :disabled="!!busyOp || streaming"
              title="wsl --install --no-distribution：只装本体+启用虚拟机平台，绝不自动捆绑发行版（UAC 提权）"
              @click="installWsl">{{ busyOp === 'install' ? '执行中…' : '🚀 一键开启' }}</button>
            <button class="btn btn-secondary btn-small" :disabled="!!busyOp || !report?.wslVersion"
              :title="report?.wslVersion ? 'wsl --update：默认通道更新' : '尚未安装 WSL'"
              @click="updateWsl">🔄 更新</button>
            <button class="btn btn-secondary btn-small" :disabled="!!busyOp || !report?.wslVersion"
              title="wsl --update --web-download：商店通道不通时 GitHub 直连更新"
              @click="updateWeb">🌐 直连更新</button>
            <button class="btn btn-secondary btn-small" :disabled="!!busyOp"
              title="wsl --set-default-version 2：新装发行版默认用 WSL2"
              @click="setDefaultV2">2️⃣ 默认WSL2</button>
            <button class="btn btn-secondary btn-small" :disabled="streaming" @click="startCheck">
              {{ streaming ? '体检中…' : '↻ 重新体检' }}
            </button>
            <button class="btn btn-danger-outline btn-small" :disabled="!!busyOp"
              title="正规双路卸载：MSI 官方卸载向导 + MSIX 用户包移除；不触碰注册表与可选功能"
              @click="uninstallWsl">🗑 卸载 WSL</button>
            <!-- 虚拟机平台状态驱动开关对：体检报告到位后按实际状态呈现其一 -->
            <button v-if="report?.vmPlatformEnabled" class="btn btn-danger-outline btn-small" :disabled="!!busyOp"
              title="经 DISM 关闭虚拟机平台/WSL 可选功能（影响 Hyper-V、安卓模拟器等共用地基，谨慎）"
              @click="disableFeatures">🧨 关闭虚拟机平台</button>
            <button v-else-if="report" class="btn btn-secondary btn-small" :disabled="!!busyOp"
              title="经 DISM 启用虚拟机平台/WSL 可选功能（UAC 提权，重启生效）——WSL2 硬前提"
              @click="enableFeatures">▶️ 开启虚拟机平台</button>
          </div>
        </div>
      </div>

      <!-- 重启引导：检测驱动——只认系统 CBS 待重启台账，与刚做过什么操作无关 -->
      <UiBanner v-if="rebootNudge" tone="info" class="slim">
        系统记有组件变更待重启生效（虚拟机平台/功能开关类改动，重启前发行版无法拉起）。就绪后自行重启即可，本工具不代点；重启后本提示自动消失。
        <button class="link-button reboot-link" @click="openPowerSettings">打开系统设置·电源 ↗</button>
      </UiBanner>

      <!-- 逐项结论：骨架先行，事件分相点亮 -->
      <div class="section-title"><h3>逐项体检</h3></div>
      <ul class="check-list">
        <li v-for="sk in CHECK_SKELETON" :key="sk.key" class="check-row" :class="{ pending: !arrived[sk.key] }">
          <div class="check-main">
            <UiStatusChip v-if="arrived[sk.key]" :tone="chipTone(arrived[sk.key].state)">{{ stateWord(arrived[sk.key].state) }}</UiStatusChip>
            <UiStatusChip v-else tone="neutral"><span class="pending-dot"></span>检测中</UiStatusChip>
            <span class="check-label">{{ sk.label }}</span>
            <code v-if="arrived[sk.key]" class="mono check-value">{{ arrived[sk.key]!.value }}</code>
            <!-- 完成落章：✓ 通过（绿）/ ⚠ 注意 / ✕ 阻塞 / ✓ 已检测不判定（灰）——每行到齐都有记号 -->
            <span v-if="arrived[sk.key]" class="check-trail" :class="arrived[sk.key]!.state">
              {{ arrived[sk.key]!.state === 'warn' ? '⚠' : arrived[sk.key]!.state === 'bad' ? '✕' : '✓' }}
            </span>
          </div>
          <div v-if="arrived[sk.key]" class="check-detail">{{ arrived[sk.key]!.detail }}</div>
        </li>
      </ul>

      <!-- 已安装发行版 -->
      <template v-if="report && report.wslVersion">
        <div class="section-title"><h3>本机发行版 ({{ report.distros?.length ?? 0 }})</h3></div>
        <div v-if="!report.distros?.length" class="empty-state">
          <p>WSL 已安装但还没有发行版 —— 到「📦 版本与发行版」页从官方清单挑一个一键安装。</p>
        </div>
        <div v-else class="table-container">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width: 40%;">发行版</th>
                <th style="width: 25%;">状态</th>
                <th style="width: 15%;">WSL 版本</th>
                <th>默认</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="d in report.distros" :key="d.name">
                <td><code class="mono">{{ d.name }}</code></td>
                <td><UiStatusChip :tone="distroRunningChip(d.state)">{{ d.state }}</UiStatusChip></td>
                <td class="mono">{{ d.version }}</td>
                <td><span v-if="d.default" class="hint-dim">★ 默认</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>

      <!-- 知识沉淀（踩坑记录入口） -->
      <details class="info-details">
        <summary class="info-summary">关于「已禁止(403)」「卸不干净」与 WSL</summary>
        <div class="info-body">
          <p><a class="inline-link" href="https://learn.microsoft.com/windows/wsl/install" target="_blank" rel="noopener">WSL 2</a> 需要 Windows 10 2004（Build 19041）以上、CPU 虚拟化（VT-x/AMD-V）与「虚拟机平台」功能。<b>wsl --install</b> 会先查询 GitHub API 获取安装信息——若你的网络出口被 GitHub API 拦截，就会报「已禁止(403)」。体检报告的「GitHub 安装通道」项即复现该判据：API 403 时改用「🌐 直连更新」、挂代理，或直接下载 MSI 离线包。</p>
          <p>WSL 在系统中可能<b>同时存在 MSIX 用户包与 MSI 系统版两种形态</b>，「设置→应用」通常只显示其一——只卸一边时另一边依旧存活，「安装形态」项会如实展示两路信号。卸载请点「🗑 卸载 WSL」，双形态各自走官方卸载器。</p>
          <p class="hint-dim">另一已知现象：虚拟机监控程序运行时 WMI 读固件虚拟化位会报 False——这不是故障，报告以「监控程序在运行」为最强证据。启用虚拟机平台后必须重启一次，发行版才能拉起。</p>
        </div>
      </details>
    </div>

    <!-- 版本 Tab：官方版本管理与发行版 -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
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
        <button class="btn btn-primary btn-small update-inline" :disabled="!!busyOp" @click="updateWsl">🔄 立即更新</button>
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

      <!-- 在线发行版清单 -->
      <UiBanner v-if="distroBlockedReason" tone="warn" class="slim distro-block-banner">{{ distroBlockedReason }}</UiBanner>
      <div class="section-title"><h3>可安装的官方发行版 ({{ online.length }})</h3></div>
      <div v-if="onlineLoading" class="hint-line">正在向本机 wsl.exe 查询在线清单…</div>
      <div v-else-if="onlineError" class="error-box">{{ onlineError }}
        <button class="btn btn-secondary btn-small retry-inline" @click="loadOnline">↻ 重试</button>
      </div>
      <div v-else-if="!online.length" class="empty-state"><p>清单未加载。点右侧按钮向本机 wsl.exe 查询。</p></div>
      <template v-else>
        <div class="btn-group online-refresh"><button class="btn btn-secondary btn-small" @click="loadOnline">↻ 刷新清单</button></div>
        <div class="table-container">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width: 220px;">ID</th>
                <th>名称</th>
                <th style="width: 110px;">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="opt in online" :key="opt.id">
                <td><code class="mono">{{ opt.id }}</code></td>
                <td>{{ opt.label }}</td>
                <td>
                  <button class="btn btn-secondary btn-small" :disabled="!!busyOp || !!distroBlockedReason"
                    :title="distroBlockedReason || `wsl --install -d ${opt.id}（UAC 提权）`"
                    @click="installDistro(opt)">⬇ 安装</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.wsl-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }
.slim { padding: 8px 12px; font-size: 12px; }
.dim { color: var(--color-text-muted); font-size: 12px; }
/* 多行诊断文案（换行符保留） */
.error-box { white-space: pre-line; }

/* 结论条 */
.verdict-banner { padding: 10px 14px; }
.verdict-line { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.verdict-detail { font-size: 12px; color: var(--color-text-muted); margin-top: 2px; }
.hint-line { font-size: 12px; color: var(--color-text-subtle); padding-left: 2px; line-height: 1.7; white-space: pre-line; }
.hint-dim { color: var(--color-text-subtle); font-size: 12px; }

/* 操作条 */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
  padding: 10px 12px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; min-width: 0; }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }

/* 体检清单 */
.check-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
.check-row { padding: 8px 10px; border-bottom: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 2px; }
.check-row:last-child { border-bottom: none; }
.check-row.pending .check-label { color: var(--color-text-subtle); }
.check-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.check-label { font-weight: 600; font-size: 13px; }
.check-value { font-size: 11px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 0 7px; color: var(--color-text-muted); }
.check-detail { font-size: 12px; color: var(--color-text-muted); line-height: 1.6; }
.check-trail { margin-left: auto; font-size: 15px; font-weight: 800; line-height: 1; }
.check-trail.ok { color: var(--state-positive); }
/* 信息行的灰勾：表"已检测、不构成判定"，与绿色通过章以色彩分层 */
.check-trail.info { color: var(--color-text-subtle); font-weight: 600; }
.check-trail.warn { color: var(--state-warning); }
.check-trail.bad { color: var(--state-danger); }
.reboot-link { margin-left: 10px; }
/* 检测中脉冲点：真实活状态才允许持续动画 */
.pending-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; display: inline-block; animation: hx-pulse 1.8s ease-in-out infinite; }

/* 版本面板 */
.control-panel {
  display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
}
.meta-info { font-size: 13px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; flex-wrap: wrap; }
.update-inline, .retry-inline { margin-left: 10px; }
.asset-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 2px 0; }
.plat-chip.current { color: var(--color-on-primary); background: var(--color-primary); border-color: var(--color-primary); }
.badge-current { font-size: 10.5px; font-weight: 700; color: var(--color-primary); }
.dl-bar { width: 110px; display: inline-flex; }
.dl-error { font-size: 11.5px; color: var(--state-danger); font-weight: 600; }
.plat-chip { font-size: 11px; font-weight: 700; color: var(--color-primary); background: var(--color-primary-soft, var(--surface-hover)); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 0 8px; }
.online-refresh { justify-content: flex-end; }

/* 知识卡 */
.info-details { border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); overflow: hidden; }
.info-summary { padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-summary::-webkit-details-marker { display: none; }
.info-summary::after { content: '▸'; font-size: 10px; margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .info-summary { border-bottom: 1px solid var(--color-border); }
.info-details[open] .info-summary::after { transform: rotate(90deg); }
.info-body { padding: 8px 12px; font-size: 12px; color: var(--color-text-muted); display: flex; flex-direction: column; gap: 4px; }
.info-body p { margin: 0; line-height: 1.6; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
