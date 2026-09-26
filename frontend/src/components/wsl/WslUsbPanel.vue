<script setup lang="ts">
// WSL「🔌 USB 直通」页签（F9：usbipd-win 集成；N31 方案 A：零门槛一键直通，
// N31 方案 B：未装引导卡加「📦 一键安装（winget）」代装主操作——机主 2026-09-26 拍板）。
// 列集对齐 wsl-dashboard 实测（总线号/VID:PID/描述/序列号/状态/客户端 IP）。
// 主路径（A 方案）：设备行「⚡ 一键直通」一次点击自动完成 目标发行版解析 →
// 未共享时 bind（提权 UAC，事前明示）→ attach；默认/唯一发行版直接附加，
// 多发行版且无默认才弹选择框；每步失败中文如实归因，刷新后按现态可单独重试。
// 细粒度操作（仅共享/下拉选发行版附加/取消共享等）收进「高级操作」折叠区
// （面板级开关 + localStorage 记忆），高级用户仍全量可用，一个不删。
// 开机自动共享 = Hanxi 账本 + 重放（不自建计划任务、不碰 usbipd 原生 auto-attach）：
// 设备行「⭐ 登记」写账本，hanxi 启动/发行版启动自动重放，总开关默认关。
// 本面板随页签 v-if 挂载：usePolling 生命周期即开关（离开页签零轮询，
// 对齐 usePolling 的 KeepAlive 契约与"冷页避免无谓探测"的 wsl 模块惯例）。
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { USBShareEntry, UsbView } from '../../../bindings/hanxi/internal/modules/wsl/models'
import type { Device } from '../../../bindings/hanxi/internal/modules/wsl/usbipd/models'
import type { DistroInstance } from '../../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePolling } from '../../composables/usePolling'
import { getErrorMessage } from '../../utils/errors'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'

const props = defineProps<{
  instances: DistroInstance[]
}>()

const { showToast } = useToast()
const { confirm } = useConfirm()

const view = ref<UsbView | null>(null)
const loading = ref(false)
const error = ref('')
// 面板内串行操作闸：usbipd 命令本身串行，撞车时后来者快速失败即可。
const opBusy = ref(false)
// 行级目标发行版选择（busid → distro；缺省回落到默认 WSL2 实例）。
const rowDistro = ref<Record<string, string>>({})

const defaultDistro = computed(() => {
  const def = props.instances.find(i => i.default && i.version === '2')
    || props.instances.find(i => i.running && i.version === '2')
    || props.instances.find(i => i.version === '2')
  return def?.name ?? ''
})
const distroFor = (d: Device) => rowDistro.value[d.busId] || defaultDistro.value

// ---- N31 方案 A：零门槛一键直通 ----
// 目标解析顺位：行级手选（高级模式遗留）> 默认 WSL2 发行版 > 唯一 WSL2 发行版；
// 多发行版且无默认才弹选择框（用户没点名时绝不代猜）。发行版未运行不用管——
// AttachUsbDevice 后端顺手拉起。
const wsl2Instances = computed(() => props.instances.filter(i => i.version === '2'))

function autoTarget(): string {
  const cands = wsl2Instances.value
  const def = cands.find(i => i.default)
  if (def) return def.name
  return cands.length === 1 ? cands[0].name : ''
}
// 一行设备此刻的直通目标（按钮标题与折叠态目标列共用）。
const passthroughTarget = (d: Device) => rowDistro.value[d.busId] || autoTarget()

// 「高级操作」折叠：默认收起（零门槛用户只见一键直通/卸下），选择跨会话记忆。
const ADV_KEY = 'hanxi.wsl.usb.advanced'
const advancedOpen = ref(readAdvancedPref())
function readAdvancedPref(): boolean {
  try { return localStorage.getItem(ADV_KEY) === '1' } catch { return false }
}
function toggleAdvanced() {
  advancedOpen.value = !advancedOpen.value
  try { localStorage.setItem(ADV_KEY, advancedOpen.value ? '1' : '0') } catch { /* 隐私模式写失败：不记忆即可 */ }
}

// 多发行版选择框（面板内微型模态）：resolve 选中名，取消/Esc/遮罩 resolve null。
const pickOpen = ref(false)
const pickChoice = ref('')
const pickDialog = ref<HTMLElement | null>(null)
const pickDevice = ref<Device | null>(null)
let pickResolve: ((name: string | null) => void) | null = null

function askDistroChoice(d: Device): Promise<string | null> {
  pickDevice.value = d
  pickChoice.value = ''
  pickOpen.value = true
  return new Promise((resolve) => { pickResolve = resolve })
}
function settlePick(name: string | null) {
  if (!pickOpen.value) return
  pickOpen.value = false
  pickResolve?.(name)
  pickResolve = null
}
// Enter 提交（Esc 绑在弹窗上）；未选时不给落定，保持等待。
function submitPick() {
  if (pickChoice.value) settlePick(pickChoice.value)
}
watch(pickOpen, async (open) => {
  if (open) {
    await nextTick()
    pickDialog.value?.focus()
  }
})
// 面板卸载时未决选择框按取消落定，防 Promise 悬挂（共享弹窗泄漏兜底同款纪律）。
onBeforeUnmount(() => settlePick(null))

async function load(silent = false) {
  if (loading.value) return
  loading.value = true
  if (!silent) error.value = ''
  try {
    const v = await WSLAPI.GetUsbOverview()
    if (v) {
      view.value = v
      error.value = v.error || ''
    }
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

// 5s 轮询：插拔设备实时反映（usePolling 自带在飞去重；操作在飞时跳过整拍）。
usePolling(() => {
  if (opBusy.value) return
  return load(true)
}, 5000)

const devices = computed(() => view.value?.devices ?? [])
const ledger = computed(() => view.value?.ledger ?? [])
const installed = computed(() => !!view.value?.installed)

const WINGET_CMD = 'winget install --id dorssel.usbipd-win -e'

// 在场判据：后端 Device.Connected() 不进 JSON，busid 空即"已共享但拔出"。
const isConn = (d: Device) => !!d.busId

function stateInfo(d: Device): { tone: 'positive' | 'information' | 'warning' | 'danger' | 'neutral'; text: string; title: string } {
  if (!isConn(d)) return { tone: 'warning', text: '已共享·不在场', title: '设备当前未插入（bind 记录仍在，插回自动可用）' }
  switch (d.state) {
    case 'attached':
      return { tone: 'positive', text: '已附加', title: d.clientIp ? `客户端 ${d.clientIp}` : '' }
    case 'shared':
      return d.forced
        ? { tone: 'warning', text: '已共享(强制)', title: '--force 绑定：Windows 侧独占直通，拔出前注意' }
        : { tone: 'information', text: '已共享', title: '已 bind，可附加' }
    case 'incompatible':
      return { tone: 'warning', text: '不兼容集线器', title: '该 Hub 不报告端口号，usbipd 拒绝共享' }
    default:
      return { tone: 'neutral', text: '未共享', title: '尚未 bind' }
  }
}

function entryFor(d: Device): USBShareEntry | undefined {
  return ledger.value.find(e => e.busId.toLowerCase() === d.busId.toLowerCase())
}

function registerTitle(d: Device): string {
  const e = entryFor(d)
  return e ? `已登记自动共享 → ${e.distro}（再点可改目标）` : '登记到开机自动共享账本'
}

async function withOp<T>(fn: () => Promise<T>): Promise<T | undefined> {
  if (opBusy.value) return undefined
  opBusy.value = true
  try {
    return await fn()
  } catch (e) {
    showToast(`操作失败: ${getErrorMessage(e)}`, { duration: 8000 })
    return undefined
  } finally {
    opBusy.value = false
    await load(true)
  }
}

async function askUac(title: string, desc: string): Promise<boolean> {
  return confirm({
    title,
    description: `${desc}\n\n该操作需要管理员权限：随后弹出系统 UAC 授权窗口（提权命令行窗口内可见进度）。`,
    tone: 'warning',
  })
}

const bindDevice = (d: Device) => withOp(async () => {
  if (!(await askUac(`共享设备 ${d.busId}（${d.description}）？`, 'bind 把设备交给 usbipd 共享池（持久绑定，重插仍有效）。'))) return
  const out = await WSLAPI.BindUsbDevice(d.busId, false)
  showToast(out?.message || '已提交共享')
})

const unbindDevice = (d: Device) => withOp(async () => {
  if (!(await askUac(`取消共享 ${d.busId}（${d.description}）？`, 'unbind 收回 usbipd 的共享绑定；设备若已附加请先「卸下」。'))) return
  const out = await WSLAPI.UnbindUsbDevice(d.busId)
  showToast(out?.message || '已提交取消共享')
})

const unbindAbsent = (d: Device) => withOp(async () => {
  if (!d.guid) return
  if (!(await askUac('取消共享（设备不在场）？', '该设备已共享但未插入，按绑定 GUID 收回；Windows 将在下次插入时正常接管。'))) return
  const out = await WSLAPI.UnbindAbsentUsbDevice(d.guid)
  showToast(out?.message || '已提交取消共享')
})

// 主按钮 title：把"会发生什么"（UAC 与否、目标谁）说在人点之前。
function oneClickTitle(d: Device): string {
  const t = passthroughTarget(d)
  const target = t ? `附加到 ${t}` : wsl2Instances.value.length > 1 ? '先弹选目标发行版再附加' : '自动选目标发行版附加'
  return d.state === 'notshared'
    ? `一次点击直通：先 bind 共享（需管理员，会弹 UAC），再${target}`
    : `一次点击直通：${target}（用户态，不弹 UAC）`
}

// 一键直通（A 方案主按钮）：目标解析 →（未共享时 UAC 明示 + bind）→ attach。
// 每步失败中文如实归因后中止；withOp 收尾必刷新，行现态即真相——
// bind 成功、attach 失败时设备已变「已共享」，再点直通自动跳过 bind 只补附加（天然分步重试）。
const oneClickAttach = (d: Device) => withOp(async () => {
  let distro = passthroughTarget(d)
  if (!distro) {
    if (!wsl2Instances.value.length) {
      showToast('本机还没有 WSL2 发行版——先到「本机发行版」页装一个', { duration: 8000 })
      return
    }
    const chosen = await askDistroChoice(d)
    if (!chosen) return // 用户放弃选择：整链不动
    // 按行记忆本次点选，同排设备下次直通不再弹选（行内下拉与标题同步跟随）。
    rowDistro.value[d.busId] = chosen
    distro = chosen
  }
  if (d.state === 'notshared') {
    if (!(await askUac(`一键直通 ${d.busId}（${d.description}）到 ${distro}？`, `一次点击完成两步：先 bind 共享（需管理员），再附加到 ${distro}。`))) return
    const b = await WSLAPI.BindUsbDevice(d.busId, false)
    if (!b?.success) {
      showToast(`共享（bind）未完成：${b?.message || '原因未知'}。附加已中止——可重试一键直通，或在「高级操作」里单独共享`, { duration: 8000 })
      return
    }
  }
  const out = await WSLAPI.AttachUsbDevice(d.busId, distro)
  showToast(out?.message || `已附加到 ${distro}`)
})

// 高级操作·附加：纯 attach（用户态、不弹 UAC），目标取行内下拉；
// 与一键直通的分工=这里绝不自动补 bind——「先共享后附加」的编排交给主按钮。
const attachDevice = (d: Device) => withOp(async () => {
  const distro = distroFor(d)
  if (!distro) {
    showToast('本机还没有 WSL2 发行版——先到「本机发行版」页装一个')
    return
  }
  const out = await WSLAPI.AttachUsbDevice(d.busId, distro)
  showToast(out?.message || '已提交附加')
})

const detachDevice = (d: Device) => withOp(async () => {
  const out = await WSLAPI.DetachUsbDevice(d.busId)
  showToast(out?.message || '已提交卸下')
})

const registerShare = (d: Device) => withOp(async () => {
  const distro = distroFor(d)
  if (!distro) {
    showToast('先选择目标发行版（下拉里挑一个 WSL2 实例）')
    return
  }
  const out = await WSLAPI.SetUsbShare(d.busId, distro)
  showToast(out?.message || '已登记')
  if (out?.success && !view.value?.autoEnabled) {
    showToast('提示：开机自动共享总开关还没开——打开后立即按账本补挂')
  }
})

// 确认被拒时原生 checkbox 已被用户翻位而绑定值未变（Vue 不重patch同值 :checked），
// 用 key 自增强制重挂载回真实账本态。
const switchKey = ref(0)

async function toggleAuto() {
  const next = !view.value?.autoEnabled
  if (next) {
    const ok = await confirm({
      title: '开启开机自动共享？',
      description: 'hanxi 启动与本机拉起发行版后，会自动按账本补做「bind(如需)+attach」：'
        + 'bind 欠账时弹一次 UAC；未运行发行版的条目跳过、待其启动后自动补挂。'
        + '仅 hanxi 在跑时生效（刻意不自建 Windows 计划任务，无系统残留）。',
      tone: 'warning',
    })
    if (!ok) {
      switchKey.value++
      return
    }
  }
  await withOp(async () => {
    const out = await WSLAPI.SetUsbAutoAttach(next)
    showToast(out?.message || '已更新总开关')
  })
}

const toggleEntry = (e: USBShareEntry) => withOp(async () => {
  const out = await WSLAPI.SetUsbShareEnabled(e.id, !e.enabled)
  showToast(out?.message || '账本已更新')
})

const removeEntry = async (e: USBShareEntry) => {
  if (!(await confirm({
    title: `移除 ${e.busId}（${e.description || '设备'}）的自动共享登记？`,
    description: '只删 Hanxi 账本条目；usbipd 的系统绑定不动——想退绑到设备行点「取消共享」。',
  }))) return
  await withOp(async () => {
    const out = await WSLAPI.RemoveUsbShare(e.id)
    showToast(out?.message || '已移除')
  })
}

const replayNow = () => withOp(async () => {
  const out = await WSLAPI.ReplayUsbNow()
  showToast(out?.message || '重放已执行')
})

async function openReleases() {
  try {
    await WSLAPI.OpenUsbipdReleases()
  } catch (e) {
    showToast(`打开发布页失败: ${getErrorMessage(e)}`)
  }
}

async function copyWinget() {
  try {
    await navigator.clipboard.writeText(WINGET_CMD)
    showToast('安装命令已复制——粘贴到终端执行（装完点「↻ 重新探测」）')
  } catch {
    showToast('复制失败，请手动输入命令')
  }
}

// ---- N31 方案 B：真·一键 winget 代装 ----
// 命令面全出自后端固定字面量（包 ID 钉死 dorssel.usbipd-win），本侧零入参零拼接；
// winget 机器级安装自带 UAC（后端不套 PowerShell RunAs），10 分钟预算封顶在后端。
// 回执分流：成功/已装（success=true）、取消/失败（success=false）、
// winget 缺席等前提问题（reject，withOp 兜底进失败 toast）。
const WINGET_BTN_TITLE = '调用系统 winget 从微软官方源下载安装 usbipd-win：需要联网、可能耗时数分钟，'
  + '期间可能弹系统 UAC 授权窗；取消授权则什么都不发生'
const wingetBusy = ref(false)
const installViaWinget = () => withOp(async () => {
  const ok = await confirm({
    title: '用系统 winget 安装 usbipd-win？',
    description: '将调用系统 winget 从微软官方源安装 usbipd-win（开源，dorssel 维护）：需要联网、'
      + '耗时可能数分钟，期间可能弹出系统 UAC 授权窗口——落位的 usbipd 服务与 ViPciBus 内核驱动'
      + '都由上游官方安装器完成，hanxi 只负责发起与复核。取消授权则什么都不发生；'
      + '不想代装可走卡片下方的手动路径。',
    tone: 'warning',
  })
  if (!ok) return
  wingetBusy.value = true
  try {
    const out = await WSLAPI.InstallUsbipdViaWinget()
    if (out?.success) {
      showToast(`✅ ${out.message || 'usbipd-win 安装完成，本页已自动刷新'}`, { duration: 6000 })
    } else {
      showToast(out?.message || '安装未完成（原因未知）——可重试或走下方手动路径', { duration: 10000 })
    }
  } finally {
    wingetBusy.value = false
  }
})

type USBShareIdentity = USBShareEntry & Partial<Pick<Device, 'instanceId' | 'serial' | 'guid'>>

function sameNonEmpty(a?: string, b?: string): boolean {
  return !!a && !!b && a.toLowerCase() === b.toLowerCase()
}

function stableIdentityMatches(e: USBShareIdentity, d: Device): boolean {
  if (sameNonEmpty(e.serial, d.serial)) return true
  if (sameNonEmpty(e.instanceId, d.instanceId)) return true
  return sameNonEmpty(e.guid, d.guid)
}

// 账本行 × 设备表现态 → 呈现徽标。优先后端现态：同 busid 仅在双方非空时
// EqualFold 命中；换口再按稳定身份，最后才兼容旧账本的唯一 VID:PID。
function entryLive(entry: USBShareEntry): { tone: 'positive' | 'information' | 'warning' | 'danger' | 'neutral'; text: string } {
  const d = entry as USBShareIdentity
  const sameBus = devices.value.find(x => !!d.busId && !!x.busId && x.busId.toLowerCase() === d.busId.toLowerCase())
  let dev = sameBus
  if (!dev) {
    const stable = devices.value.filter(x => !!x.busId && stableIdentityMatches(d, x))
    if (stable.length === 1) dev = stable[0]
  }
  if (!dev && !d.instanceId && !d.serial && !d.guid && d.vid && d.pid) {
    const legacy = devices.value.filter(x => !!x.busId
      && !!x.vid && x.vid.toLowerCase() === d.vid!.toLowerCase()
      && !!x.pid && x.pid.toLowerCase() === d.pid!.toLowerCase())
    if (legacy.length === 1) dev = legacy[0]
  }
  if (!dev) return { tone: 'warning', text: '不在场' }
  if (dev.state === 'attached') return { tone: 'positive', text: '已附加' }
  if (dev.state === 'shared') return { tone: 'information', text: '待附加' }
  return { tone: 'neutral', text: '需先共享' }
}
</script>

<template>
  <!-- usbipd-win 未装：引导卡（N31 方案 B：winget 一键代装为主操作，手动路径兜底） -->
  <div v-if="view && !installed" class="guide-card">
    <h3 class="guide-title">需要 usbipd-win：本机 USB 直通到 WSL2 的桥梁</h3>
    <p class="guide-text">
      「USB 直通」把 Windows 本机 USB 设备（串口、加密狗、存储类等）共享并附加给 WSL2 发行版，
      底层由开源工具 <b>usbipd-win</b> 承担。安装一次即可长期使用；绑定（bind）需管理员，
      附加/卸下无需。装好后本页每台设备一行一个「⚡ 一键直通」，一次点击完成共享+附加。
    </p>
    <UiBanner v-if="wingetBusy" tone="info" class="slim">
      winget 正在下载/安装 usbipd-win（可能要几分钟）——若弹出 UAC 授权窗请点「是」，
      完成后本卡自动刷新为设备表。
    </UiBanner>
    <div class="btn-group">
      <button class="btn btn-primary btn-small" :disabled="opBusy" :title="WINGET_BTN_TITLE" @click="installViaWinget">
        {{ wingetBusy ? '⏳ 正在安装（留意 UAC 窗）…' : '📦 一键安装（winget）' }}
      </button>
      <button class="btn btn-secondary btn-small" :disabled="loading || opBusy" @click="load()">{{ loading ? '探测中…' : '↻ 重新探测' }}</button>
    </div>
    <p class="guide-text hint-dim">
      代装=让系统 winget 从微软官方源跑上游 MSI（包 ID 后端钉死 dorssel.usbipd-win，
      服务与内核驱动由官方安装器落位）；不想代装就走下面的手动路径，殊途同归。
    </p>
    <ol class="guide-steps">
      <li>手动路径：官方发布页下载 MSI（或复制下方命令到自己的终端执行）：
        <div class="cmd-row">
          <code class="mono cmd">{{ WINGET_CMD }}</code>
          <button class="btn btn-secondary btn-small" @click="copyWinget">📋 复制命令</button>
        </div>
      </li>
      <li>装完回到本页点「↻ 重新探测」——hanxi 按固定落位现场识别，通常无需重启 hanxi。</li>
      <li>装的是别的机器？本表只管理本机直通。</li>
    </ol>
    <div class="btn-group">
      <button class="btn btn-secondary btn-small" @click="openReleases">🌐 打开官方发布页</button>
    </div>
  </div>

  <UiBanner v-if="view?.replayBusy" tone="info" class="slim">正在按账本重放 USB 共享（若需补绑定会弹一次 UAC）…</UiBanner>

  <div v-if="error" class="error-box">{{ error }}
    <button class="btn btn-secondary btn-small retry-inline" :disabled="loading" @click="load()">↻ 重试</button>
  </div>
  <div v-if="!view && loading" class="hint-line">正在探测 usbipd-win 与设备表…</div>

  <template v-if="view && installed">
    <div class="control-panel">
      <div class="meta-info">
        <span>usbipd <b class="mono">v{{ view.version || '?' }}</b> · 列：总线号 / VID:PID / 设备 / 序列号 / 状态 / 客户端；
          日常只需「⚡ 一键直通」（自动共享+附加，未共享时弹 UAC），细粒度拆分在「🔧 高级操作」。</span>
        <span class="hint-dim">只登记与管理 Hanxi 账本内的自动共享；usbipd 自身与他机远程 usbip 不触碰</span>
      </div>
      <label class="auto-switch" :key="switchKey" title="开机自动共享总开关：hanxi 启动/拉起发行版时按账本补挂（默认关）">
        <input type="checkbox" :checked="view.autoEnabled" :disabled="opBusy" @change="toggleAuto" />
        开机自动共享<span v-if="!view.autoEnabled" class="hint-dim">（关）</span>
      </label>
      <div class="btn-group">
        <button class="btn btn-secondary btn-small" :disabled="loading || opBusy" @click="load()">{{ loading ? '读取中…' : '↻ 刷新' }}</button>
        <button class="btn btn-secondary btn-small" :disabled="opBusy || !ledger.length"
          title="立即按账本重放 bind(欠账补)+attach（与开机自动共享同一通道）" @click="replayNow">⟳ 重放共享</button>
        <button class="btn btn-secondary btn-small" :aria-expanded="advancedOpen"
          :title="advancedOpen ? '收起细粒度操作（仅共享/指定发行版附加/取消共享等）' : '展开细粒度操作：单独共享、下拉指定发行版、取消共享等（选择会记住）'"
          @click="toggleAdvanced">🔧 高级操作 {{ advancedOpen ? '▴' : '▾' }}</button>
      </div>
    </div>

    <!-- 设备表 -->
    <div class="section-title"><h3>USB 设备 ({{ devices.length }})</h3></div>
    <div v-if="!devices.length && !loading" class="empty-state">
      <p>本机此刻没有可列出的 USB 设备——插上目标设备后点「↻ 刷新」（页开着时也会自动复采）。</p>
    </div>
    <div v-else-if="loading && !view" class="hint-line">正在读取 usbipd 设备表…</div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 64px;">总线号</th>
            <th style="width: 104px;">VID:PID</th>
            <th>设备</th>
            <th style="width: 130px;">序列号</th>
            <th style="width: 128px;">状态</th>
            <th style="width: 120px;">目标发行版</th>
            <th :style="{ width: advancedOpen ? '320px' : '150px' }">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in devices" :key="d.busId || d.guid || d.instanceId" :class="{ 'row-dim': !isConn(d) }">
            <td class="mono">{{ d.busId || '—' }}</td>
            <td class="mono">{{ d.vid && d.pid ? `${d.vid}:${d.pid}` : '—' }}</td>
            <td :title="d.instanceId">{{ d.description || '（未知设备）' }}</td>
            <td class="mono serial">{{ d.serial || '—' }}</td>
            <td>
              <UiStatusChip :tone="stateInfo(d).tone" :title="stateInfo(d).title">{{ stateInfo(d).text }}</UiStatusChip>
              <span v-if="d.clientIp" class="mono client-ip">{{ d.clientIp }}</span>
            </td>
            <td>
              <template v-if="isConn(d)">
                <select v-if="advancedOpen" class="input distro-select" :value="distroFor(d)"
                  :aria-label="`设备 ${d.busId} 的目标发行版`" @change="rowDistro[d.busId] = ($event.target as HTMLSelectElement).value">
                  <option v-for="i in instances" :key="i.name" :value="i.name" :disabled="i.version !== '2'">
                    {{ i.name }}{{ i.version !== '2' ? '（WSL1 不可用）' : i.running ? '' : '（未运行）' }}
                  </option>
                </select>
                <span v-else-if="passthroughTarget(d)" class="hint-dim" title="一键直通的自动目标；要改点上方「🔧 高级操作」">{{ passthroughTarget(d) }}</span>
                <span v-else class="hint-dim" title="多个发行版且无默认——点「一键直通」时会让你选">自动选择</span>
              </template>
              <span v-else class="hint-dim">不在场</span>
            </td>
            <td>
              <div class="distro-actions">
                <template v-if="isConn(d)">
                  <button v-if="d.state === 'attached'" class="btn btn-secondary btn-small" :disabled="opBusy"
                    title="从 WSL 卸下，设备回到 Windows" @click="detachDevice(d)">⏏ 卸下</button>
                  <button v-else-if="d.state === 'shared' || d.state === 'notshared'" class="btn btn-primary btn-small"
                    :disabled="opBusy" :title="oneClickTitle(d)" @click="oneClickAttach(d)">⚡ 一键直通</button>
                  <button v-else class="btn btn-secondary btn-small" disabled>不可共享</button>
                  <template v-if="advancedOpen">
                    <button v-if="d.state === 'shared'" class="btn btn-secondary btn-small" :disabled="opBusy"
                      title="只附加到行内下拉选中的发行版（用户态，不弹 UAC；未共享请先「仅共享」）" @click="attachDevice(d)">▶ 附加</button>
                    <button v-if="d.state === 'notshared'" class="btn btn-secondary btn-small" :disabled="opBusy"
                      title="仅共享进 usbipd 池（需管理员），不立即附加；想用时再点「附加」" @click="bindDevice(d)">⇗ 仅共享</button>
                    <button v-if="d.state === 'shared' || d.state === 'notshared'" class="btn btn-secondary btn-small"
                      :disabled="opBusy" :title="registerTitle(d)"
                      @click="registerShare(d)">⭐ 自动共享</button>
                    <button v-if="d.state === 'shared'" class="btn btn-secondary btn-small" :disabled="opBusy"
                      title="收回共享绑定（需管理员）；已附加请先卸下" @click="unbindDevice(d)">✂ 取消共享</button>
                  </template>
                </template>
                <template v-else>
                  <button class="btn btn-danger-outline btn-small" :disabled="opBusy"
                    title="设备不在场也能退绑：按绑定 GUID 收回（需管理员）" @click="unbindAbsent(d)">✂ 退绑(不在场)</button>
                </template>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 自动共享账本 -->
    <div class="section-title">
      <h3>自动共享账本 ({{ ledger.length }})</h3>
      <span v-if="view.lastReplay" class="hint-dim">最近重放：{{ view.lastReplay }}</span>
    </div>
    <div v-if="!ledger.length" class="empty-state">
      <p>账本为空——在上方设备行点「⭐ 自动共享」登记第一条（开机/插回后自动附加到指定发行版）。</p>
    </div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 64px;">总线号</th>
            <th>设备</th>
            <th style="width: 150px;">目标发行版</th>
            <th style="width: 110px;">现态</th>
            <th>上次结果</th>
            <th style="width: 150px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in ledger" :key="e.id" :class="{ 'row-dim': !e.enabled }">
            <td class="mono">{{ e.busId }}</td>
            <td>{{ e.description || '—' }}<span v-if="e.vid && e.pid" class="mono vidpid">（{{ e.vid }}:{{ e.pid }}）</span></td>
            <td><code class="mono">{{ e.distro }}</code></td>
            <td>
              <UiStatusChip v-if="!e.enabled" tone="neutral">已停用</UiStatusChip>
              <UiStatusChip v-else :tone="entryLive(e).tone">{{ entryLive(e).text }}</UiStatusChip>
            </td>
            <td class="dim wrap">
              <template v-if="e.lastStatus">{{ e.lastStatus }}<span class="hint-dim"> · {{ e.lastAt }}</span></template>
              <template v-else>未重放过</template>
            </td>
            <td>
              <div class="distro-actions">
                <button class="btn btn-secondary btn-small" :disabled="opBusy" @click="toggleEntry(e)">
                  {{ e.enabled ? '⏸ 停用' : '▶ 启用' }}
                </button>
                <button class="btn btn-danger-outline btn-small" :disabled="opBusy" @click="removeEntry(e)">🗑 移除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="hint-line">
      换插口场景：账本优先按总线号匹配，认不出时按登记快照的 VID:PID 唯一回落（状态列会点名"换插口"）。
      同 VID:PID 多义时不猜、跳过并记因。
    </p>
  </template>

  <!-- 一键直通·多发行版选择框（仅"多个 WSL2 且无默认"时出现；Esc/遮罩=放弃本次直通） -->
  <Teleport to="body">
    <div v-if="pickOpen" class="pick-backdrop" @mousedown.self="settlePick(null)">
      <section ref="pickDialog" class="pick" role="dialog" aria-modal="true" aria-labelledby="usb-pick-title" tabindex="-1"
        @keydown.esc="settlePick(null)" @keydown.enter="submitPick">
        <header>
          <h2 id="usb-pick-title">直通给哪个发行版？</h2>
          <p>本机有多个 WSL2 发行版且未设默认——设备 {{ pickDevice?.busId }}（{{ pickDevice?.description }}）附加给谁？</p>
        </header>
        <label v-for="i in wsl2Instances" :key="i.name" class="pick-option">
          <input v-model="pickChoice" type="radio" name="usb-pick-distro" :value="i.name" />
          <span class="mono">{{ i.name }}</span>
          <span v-if="i.running" class="pick-tag">运行中</span>
          <span v-else class="pick-tag dim">未运行（直通时顺手拉起）</span>
        </label>
        <footer>
          <button type="button" class="btn btn-secondary btn-small" @click="settlePick(null)">取消</button>
          <button type="button" class="btn btn-primary btn-small" :disabled="!pickChoice" @click="settlePick(pickChoice)">⚡ 直通</button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
/* 全局原子落 components.css（.tbl/.control-panel/.empty-state/.error-box/.btn 等）；
   此处只留本 tab 独有或有意补差（对齐端口转发页的注释纪律）。 */
.error-box { white-space: pre-line; }
.hint-line { line-height: 1.7; white-space: pre-line; font-size: var(--text-sm); color: var(--color-text-muted); }
.hint-dim { font-size: var(--text-sm); color: var(--color-text-muted); }
.retry-inline { margin-left: 10px; }
.control-panel { gap: 10px; flex-wrap: wrap; }
.meta-info { min-width: 0; }
.btn-group { flex-wrap: wrap; }
.distro-actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
.section-title { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; }
.row-dim .mono, .row-dim td { opacity: 0.65; }
.serial, .client-ip { color: var(--color-text-muted); font-size: var(--text-sm); }
.client-ip { margin-left: 6px; }
.vidpid { color: var(--color-text-muted); }
.wrap { word-break: break-word; }
.distro-select { flex: 0 0 auto; width: 150px; }
.auto-switch { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); cursor: pointer; white-space: nowrap; }
.auto-switch input { accent-color: var(--color-primary); margin: 0; }

/* 引导卡（未安装态）：面板级底色 + 步骤条，与 .control-panel 同族表面语言 */
.guide-card {
  display: flex; flex-direction: column; gap: 10px; padding: 14px 16px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
.guide-title { margin: 0; font-size: var(--text-base); font-weight: 700; }
.guide-text { margin: 0; font-size: var(--text-sm); line-height: 1.7; }
.guide-steps { margin: 0; padding-left: 20px; font-size: var(--text-sm); line-height: 1.8; }
.cmd-row { display: flex; align-items: center; gap: 8px; margin-top: 4px; flex-wrap: wrap; }
.cmd {
  padding: 3px 8px; background: var(--surface-page); border: 1px solid var(--color-border);
  border-radius: 6px; user-select: all;
}
.input {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 4px 8px; font-size: var(--text-sm); color: var(--color-text); font-family: inherit;
}
.input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }

/* 直通目标选择框：与 UiPromptDialog 同族表面语言（遮罩/面板/页脚），选项为纵向单选 */
.pick-backdrop { position: fixed; inset: 0; z-index: 1000; display: grid; place-items: center; padding: 24px; background: var(--overlay-mask); }
.pick {
  width: min(440px, 100%); padding: 20px; border: 1px solid var(--color-border); border-radius: var(--radius-panel);
  background: var(--surface-panel); box-shadow: var(--shadow-panel); color: var(--color-text);
}
.pick h2 { margin: 0 0 6px; font-size: var(--text-lg); }
.pick header p { margin: 0; color: var(--color-text-muted); font-size: var(--text-base); line-height: 1.65; }
.pick-option {
  display: flex; align-items: center; gap: 8px; margin-top: 10px; padding: 9px 12px; cursor: pointer;
  border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-soft);
}
.pick-option:hover { background: var(--surface-hover); }
.pick-option:has(input:checked) { border-color: var(--color-primary); outline: 1px solid var(--color-primary); }
.pick-option input { accent-color: var(--color-primary); margin: 0; }
.pick-tag { font-size: var(--text-sm); color: var(--color-text-muted); }
.pick-tag.dim { opacity: 0.8; }
.pick footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
@media (max-width: 460px) { .pick-backdrop { align-items: end; padding: 12px } .pick { padding: 16px } .pick footer { flex-direction: column-reverse } }
</style>
