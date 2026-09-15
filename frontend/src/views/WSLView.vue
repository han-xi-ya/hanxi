<script setup lang="ts">
// WSL 子系统：就绪体检（流式逐项点亮）+ 官方版本管理（Releases × 本机关系）
// + 白名单提权操作（一键开启/更新/装发行版/正规卸载/组件还原）
// + 发行版实例管理控制台（主操作常驻 + 次要操作收「⋯ 更多」下拉：文件/设默认/导出/迁移/
// 克隆/瘦身/详情/wsl.conf；独立「本机发行版」页签；删除附带商店启动器清理）。
// + 「➕ 添加实例」页：商店官方清单 / 本地 rootfs tar / 现有 VHDX 三源统一新增入口
//（官方发行版清单已并入商店源面板，不再另设页签；镜像站源刻意不做——第三方 rootfs 信任链无法把关）。
// 体检走 wsl:readiness 事件分相推送：先全量 pending 骨架，system/wsl/net 三源
// 并发先到先点亮，done 收口终版报告——骨架 key 与后端 BuildItems 有顺序互锁。
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import * as WSLAPI from '../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { CheckItem, DistroOption, Report } from '../../bindings/hanxi/internal/modules/wsl/readiness/models'
import type { CloneProgress, CompactProgress, DistroForensics, DistroInstance, DistroOpResult, DownloadProgress, ExportRecord, HostConfDoc, PortProxyView, PortRule, PortRuleView, ReadinessUpdate, WslConfDoc } from '../../bindings/hanxi/internal/modules/wsl/models'
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

const activeMainTab = ref<'console' | 'distros' | 'add' | 'versions' | 'proxy'>('console')
// 页签 6→5：原「📦 官方发行版」整页与「添加实例·商店源」是同清单双入口，已并入商店源面板。
// mainTabs 为 computed：distros 页签在克隆/瘦身重任务在飞时 label 追加「·运行中」——跨页签进度可见。
const mainTabs = computed<Array<{ key: string; label: string }>>(() => {
  const heavy = cloneBusy.value || (!!compProg.value && !compTerm.value)
  return [
    { key: 'console', label: '🐧 就绪检测' },
    { key: 'distros', label: heavy ? '💻 本机发行版 ·运行中' : '💻 本机发行版' },
    { key: 'add', label: '➕ 添加实例' },
    { key: 'versions', label: '🧩 本体版本' },
    { key: 'proxy', label: '🔀 端口转发' },
  ]
})

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
// ---------- busy 分级在飞登记（跨页签进度可见 + 不过度锁） ----------
// activeOps：当前全部在飞操作（支持"克隆在飞 + 其他发行版操作"有限并发，后端单飞闸兜底）；
// 命名约定：全局互斥类（整链提权/打停全部/落位链）用裸词（install/update/…/import/wslconfig/
// shutdown/proxy-apply/proxy-cleanup/distro-<id>）；发行版级操作用 `action:发行版名`
// （terminal/folder/restart/stop/setdefault/export/move/clone/compact/confsave/unreg）。
// busyTop 取最近在飞操作驱动常驻进度条与按钮"进行中"文案；globalBusy 为真时全站写操作关门。
const activeOps = ref<Set<string>>(new Set())
const busyOp = computed(() => [...activeOps.value].at(-1) ?? '')
const busyAny = computed(() => activeOps.value.size > 0)
const globalBusy = computed(() => [...activeOps.value].some(op => !op.includes(':')))
const busyWith = (op: string) => activeOps.value.has(op)
// `action:发行版名` 尾缀匹配：该发行版任何在飞写操作都算它"正忙"。
const busyDistro = (name: string) => [...activeOps.value].some(op => op.endsWith(`:${name}`))
// 单发行版写操作可用性闸门：全局互斥在飞，或该发行版正忙，或跨表单互斥（movingName）。
const rowBusy = (name: string) => globalBusy.value || busyDistro(name)
function startOp(op: string) {
  activeOps.value = new Set(activeOps.value).add(op)
}
function finishOp(op: string) {
  if (!activeOps.value.has(op)) return
  const next = new Set(activeOps.value)
  next.delete(op)
  activeOps.value = next
}

// 常驻进度条文案映射：提权类点名「提权窗口」，用户态操作只说「进行中」——不再失实。
const BUSY_LABELS: Record<string, string> = {
  install: '🚀 正在安装 WSL 本体（提权窗口内有详细进度）',
  update: '🔄 正在更新 WSL 本体（提权窗口内有详细进度）',
  'update-web': '🌐 正在 GitHub 直连更新（提权窗口内有详细进度）',
  'set-default': '正在设定默认版本 WSL2（提权窗口内有详细进度）',
  'enable-features': '▶️ 正在开启虚拟机平台组件（提权窗口内有详细进度）',
  'disable-features': '🧨 正在关闭虚拟机平台组件（提权窗口内有详细进度）',
  uninstall: '🗑 正在卸载 WSL（提权窗口内有详细进度）',
  shutdown: '🌑 正在停止全部 WSL（进行中）',
  wslconfig: '💾 正在写回 .wslconfig（进行中）',
  import: '📥 正在导入/挂载新实例（进行中，大镜像可达数分钟）',
  'proxy-apply': '▶ 正在应用端口转发规则（提权窗口内有详细进度）',
  'proxy-cleanup': '🧹 正在清理托管端口转发（提权窗口内有详细进度）',
}
function busyLabelOf(op: string): string {
  if (op.startsWith('distro-')) return `📦 正在安装 ${op.slice('distro-'.length)}（提权窗口内有详细进度）`
  const i = op.indexOf(':')
  if (i < 0) return BUSY_LABELS[op] ?? '⏳ 操作进行中'
  const action = op.slice(0, i)
  const name = op.slice(i + 1)
  switch (action) {
    case 'clone': {
      const target = cloneProg.value?.target ? ` → ${cloneProg.value.target}` : ''
      return `🧬 克隆 ${name}${target}：${cloneProg.value ? CLONE_STAGE_TEXT[cloneProg.value.stage] ?? '进行中' : '进行中'}（切换到「本机发行版」页看进度）`
    }
    case 'compact':
      return `🗜 瘦身 ${name}：${compProg.value ? COMPACT_STAGE_TEXT[compProg.value.stage] ?? '进行中' : '进行中'}（切换到「本机发行版」页看进度）`
    case 'export': return `📤 导出 ${name}：进行中（大发行版可达数分钟）`
    case 'move': return `🧭 迁移 ${name}：进行中（提权窗口内有详细进度）`
    case 'unreg': return `🗑 正在删除发行版 ${name}（进行中）`
    case 'terminal': return `⌨ 正在启动 ${name} 终端（进行中）`
    case 'folder': return `📂 正在打开 ${name} 的文件（进行中）`
    case 'restart': return `🔄 正在重启 ${name}（进行中）`
    case 'stop': return `⏹ 正在关机 ${name}（进行中）`
    case 'setdefault': return `⭐ 正在把 ${name} 设为默认发行版（进行中）`
    case 'confsave': return `⚙ 正在写回 ${name} 的 wsl.conf（进行中）`
    default: return `⏳ ${name} 操作进行中`
  }
}
const busyBanner = computed(() => (busyOp.value ? busyLabelOf(busyOp.value) : ''))

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
    if (u.report.wslVersion) loadInstances() // 体检收口顺带复采实例列表（不竞态：独立通道）
  }
})

async function startCheck() {
  loadError.value = ''
  // Stale 基线：复采不清屏——保留上一份 report/arrived 继续呈现（标题区标注"复采中…"），
  // 新结果到达后逐项覆盖，避免每次复查整页闪回骨架。
  streaming.value = true
  try {
    await WSLAPI.StartReadiness()
  } catch (e) {
    loadError.value = `启动体检失败: ${getErrorMessage(e)}`
    streaming.value = false
  }
}

// ---------- 白名单提权操作（全部为全局互斥类） ----------
async function runOp(opts: {
  name: string
  title: string
  desc: string
  tone?: 'default' | 'warning' | 'danger'
  confirmLabel?: string
  invoke: () => PromiseLike<unknown>
}) {
  if (busyAny.value) return
  const accepted = await confirm({
    title: opts.title,
    description: `${opts.desc}\n\n该操作需管理员权限：随后会弹出系统 UAC 授权窗口（并打开提权命令行窗口显示进度），请在窗口中确认继续。`,
    tone: opts.tone ?? 'warning',
    ...(opts.confirmLabel ? { confirmLabel: opts.confirmLabel } : {}),
  })
  if (!accepted) return
  startOp(opts.name)
  try {
    const out = (await opts.invoke()) as { message?: string }
    showToast(out.message || '操作已完成')
    await startCheck() // 操作后流式复查：若真有变更欠重启，CBS 台账会让引导条自己出现
  } catch (e) {
    showToast(`${opts.title}失败: ${getErrorMessage(e)}`, { duration: 8000 })
    await loadInstances() // 成败都复采：提权脚本可能已部分生效，不让"半成功看不见"
  } finally {
    finishOp(opts.name)
  }
}

const installWsl = () => runOp({
  name: 'install', title: '一键开启 WSL（不装发行版）？',
  desc: '执行 wsl --install --no-distribution：只安装 WSL 本体并启用「虚拟机平台」，绝不自动捆绑任何 Linux 发行版——系统请重启后到「➕ 添加实例」页手动挑选。',
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
  tone: 'danger', confirmLabel: '继续卸载',
  desc: '正规双路卸载：① 若检出 MSI 系统版，弹出其官方卸载向导（msiexec /X）；② 移除当前用户的 MSIX 包（Remove-AppxPackage）。'
    + '\n只走官方卸载器、不强删注册表；完成后如实报告 Lxss 发行版注册残留。'
    + '\n可选功能（虚拟机平台）不在本操作内——如需彻底还原系统，再执行「关闭虚拟机平台」。',
  invoke: WSLAPI.UninstallWsl,
})
const disableFeatures = () => runOp({
  name: 'disable-features', title: '关闭虚拟机平台组件？',
  tone: 'danger', confirmLabel: '仍要关闭',
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

// ---------- 发行版实例管理 ----------
// 列表与提权通道解耦：独立 loadInstances，操作后复采（wsl 状态有滞后，
// 复采即真相——不在前端乐观编状态）。busy 分级：全局互斥类在飞才全站关门；
// 单发行版写操作只锁本行；只读（终端/文件/详情/刷新）任何状态下可用——
// 后端另有每发行版单飞闸与迁移全局闸兜并发，前端不做过度锁。
const instances = ref<DistroInstance[]>([])
const instLoading = ref(false)
const instError = ref('')
const movingName = ref('') // 迁移内联表单展开中的发行版（同时只开一行）
const moveTarget = ref('')
// 导出：内联格式选择（同时只开一行），本会话导出工件全量登记展示。
const exportingName = ref('')
const exportFormat = ref<'gz' | 'tar'>('gz')
const exportRecords = ref<ExportRecord[]>([])
// 导出/瘦身成功后自动展开「本会话导出记录」抽屉——工件即刻可见，不用手动翻。
const exportLogOpen = ref(false)

async function loadInstances() {
  instLoading.value = true
  instError.value = ''
  try {
    instances.value = (await WSLAPI.ListInstances()) ?? []
  } catch (e) {
    instances.value = []
    instError.value = `获取发行版列表失败: ${getErrorMessage(e)}`
  } finally {
    instLoading.value = false
  }
}

// 发行版 Tab 呈现判据：体检确认装了本体即开管理面；复采通道独立于体检——
// 已复采到实例或复采如实报错时同样呈现，不让"体检未出/失败"遮蔽本机现状。
const distroTabReady = computed(() =>
  !!report.value?.wslVersion || instances.value.length > 0 || !!instError.value)

async function loadExportRecords() {
  try {
    exportRecords.value = (await WSLAPI.ListDistroExports()) ?? []
  } catch {
    // 登记面是辅助信息：失败不打扰主列表，保留已有视图
  }
}

// runDistroOp：发行版操作编排（确认链 + busy 分级闸门 + 复采收口）。
// 与 runOp 的差别：提权与否由 uac 如实声明（terminate/设默认/导出是用户态命令，
// 谎报"会弹 UAC"反而制造困惑）；完成后只复采列表不重跑全量体检。
// kind:'read'（终端/文件）绕过闸门任何状态可用；写操作仅在"全局互斥在飞 / 本操作重入"时拒绝。
async function runDistroOp(opts: {
  name: string
  title: string
  desc: string
  tone?: 'default' | 'warning' | 'danger'
  confirmLabel?: string
  uac?: boolean
  details?: Array<{ label: string; value: string }>
  confirm?: boolean
  kind?: 'read' | 'write'
  invoke: () => PromiseLike<DistroOpResult>
  then?: (out: DistroOpResult) => void
}) {
  if (opts.kind !== 'read' && (globalBusy.value || busyWith(opts.name))) return
  if (opts.confirm !== false) {
    const accepted = await confirm({
      title: opts.title,
      description: opts.desc + (opts.uac
        ? '\n\n该操作需管理员权限：随后会弹出系统 UAC 授权窗口，请在窗口中确认继续。'
        : '\n\n该操作以普通权限执行，不会弹出 UAC。'),
      tone: opts.tone ?? 'warning',
      details: opts.details ?? [],
      ...(opts.confirmLabel ? { confirmLabel: opts.confirmLabel } : {}),
    })
    if (!accepted) return
  }
  startOp(opts.name)
  try {
    const out = await opts.invoke()
    showToast(out?.message || '操作已完成')
    opts.then?.(out)
  } catch (e) {
    showToast(`${opts.title}失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    finishOp(opts.name)
    await loadInstances() // 成败都复采：命令可能已部分生效，状态以复采为准
  }
}

const openTerminal = (d: DistroInstance) => runDistroOp({
  name: `terminal:${d.name}`, kind: 'read', title: `启动 ${d.name}`, confirm: false,
  desc: `唤起系统默认终端进入 ${d.name}。说明：WSL 在发行版内无前台进程时会自动停机，"运行中"是会话驱动的自然形态，本工具不做后台保活。`,
  invoke: () => WSLAPI.OpenTerminal(d.name),
})
const terminateDistro = (d: DistroInstance) => runDistroOp({
  name: `stop:${d.name}`, title: `关机 ${d.name}？`, tone: 'default',
  desc: '执行 wsl --terminate——等同拔掉这一个发行版的虚拟机电源（WSL 不提供单发行版的优雅关机）：未保存的前台进程即刻终止，数据盘无损；要停全部请到就绪检测页「🌑 停止全部」。下次访问（唤终端/\\wsl$ 路径）自动再开机。',
  invoke: () => WSLAPI.TerminateDistro(d.name),
})
// 重启（对齐 wsl-dashboard 语义但刻意不做保活）：后端 terminate→确认已停→拉起探针。
const restartDistro = (d: DistroInstance) => runDistroOp({
  name: `restart:${d.name}`, title: `重启 ${d.name}？`, tone: d.running ? 'warning' : 'default',
  desc: (d.running ? '先停止再启动验证：现有终端/前台会话会被中断（数据无损）。' : '当前已停止——重启即拉起并验证可启动。')
    + '之后不进入终端的话，发行版空闲片刻会自动回落为「已停止」，本工具不做后台保活（平台常态）。',
  invoke: () => WSLAPI.RestartDistro(d.name),
})
// 文件管理器：只读外呼免确认（同唤终端）；停止的发行版会被后端顺手拉起。
const openFolder = (d: DistroInstance) => runDistroOp({
  name: `folder:${d.name}`, kind: 'read', title: `打开 ${d.name} 的文件`, confirm: false,
  desc: `在资源管理器打开 \\\\wsl$\\${d.name} 浏览发行版文件系统${d.running ? '' : '（当前已停止，会先拉起发行版）'}。只读浏览入口，不改动任何数据。`,
  invoke: () => WSLAPI.OpenDistroFolder(d.name),
})
const setDefaultDistro = (d: DistroInstance) => runDistroOp({
  name: `setdefault:${d.name}`, title: `把 ${d.name} 设为默认发行版？`, tone: 'default',
  desc: '执行 wsl --set-default：此后不带 -d 的 wsl 命令与控制台默认进入该发行版。',
  invoke: () => WSLAPI.SetDefaultDistro(d.name),
})
const unregisterDistro = (d: DistroInstance) => runDistroOp({
  name: `unreg:${d.name}`, title: `删除发行版 ${d.name}？`, tone: 'danger', confirmLabel: '🗑 删除数据并移除',
  details: [
    { label: '发行版', value: d.name },
    { label: '磁盘占用', value: d.sizeBytes ? fmtSize(d.sizeBytes) : '未知' },
    { label: '数据位置', value: d.basePath || '未知' },
  ],
  desc: '执行 wsl --unregister：先停止该发行版，随后其数据盘（VHDX）连同全部文件被系统删除——不可恢复。'
    + '如需留档，请先「导出」为 tar 再删。商店安装的发行版其启动器将被一并自动卸载'
    + '（仅当启动器被其它发行版共用、或卸载失败时，回执会点名让你到「设置→应用」手动处理）。',
  invoke: () => WSLAPI.UnregisterDistro(d.name),
})
// 导出两段式：点击展开内联格式选择，「开始导出」才进确认链（对齐迁移的表单先行范式）。
function startExport(d: DistroInstance) {
  exportingName.value = d.name
  exportFormat.value = 'gz'
}
function cancelExport() {
  exportingName.value = ''
}
const confirmExport = (d: DistroInstance) => runDistroOp({
  name: `export:${d.name}`, title: `导出 ${d.name}？`, tone: 'default',
  details: [{ label: '导出格式', value: exportFormat.value === 'gz' ? 'tar.gz（压缩）' : 'tar（未压缩）' }],
  desc: exportFormat.value === 'gz'
    ? '执行 wsl --export --format tar.gz：整个文件系统压缩导出为 tar.gz（需 WSL 2.4.4+），'
      + '落到「下载\\WSL 导出」文件夹（文件名后端拼装、同名不覆盖）。大发行版可达数十 GB 耗时数分钟，期间请勿退出。'
    : '执行 wsl --export：整个文件系统导出为未压缩 tar（速度更快、兼容老版本 WSL，但体积大），'
      + '落到「下载\\WSL 导出」文件夹（文件名后端拼装、同名不覆盖）。大发行版可达数十 GB 耗时数分钟，期间请勿退出。',
  invoke: () => WSLAPI.ExportDistro(d.name, exportFormat.value === 'gz'),
  then: (out) => {
    exportingName.value = ''
    if (out?.success) {
      void loadExportRecords()
      exportLogOpen.value = true // 成功即展开导出记录：工件即刻可见
    }
  },
})

// 迁移：内联表单先行，确认时把目标路径与"会被连带打停的运行中实例"写进确认框。
function startMove(d: DistroInstance) {
  movingName.value = d.name
  // 预填「安装目录\<发行版名>」供修改（同克隆落位规则）；安装目录留空则不猜。
  moveTarget.value = previewSubdir(installDir.value, d.name)
}
function cancelMove() {
  movingName.value = ''
  moveTarget.value = ''
}
const moveRunningOthers = computed(() =>
  instances.value.filter(i => i.running && i.name !== movingName.value))
const confirmMove = (d: DistroInstance) => runDistroOp({
  name: `move:${d.name}`, title: `迁移 ${d.name} 到新位置？`, tone: 'warning', uac: true,
  details: [
    { label: '当前安装位置', value: d.basePath || '未知' },
    { label: '目标目录', value: moveTarget.value.trim() },
  ],
  desc: '执行 wsl --manage --move：先把整个 WSL 子系统 wsl --shutdown'
    + (moveRunningOthers.value.length ? `（将连带打停运行中的：${moveRunningOthers.value.map(i => i.name).join('、')}）` : '')
    + '，再移动数据盘到目标目录（须为空目录或不存在的路径）。迁移期间所有发行版不可用。',
  invoke: () => WSLAPI.MoveDistro(d.name, moveTarget.value.trim()),
  then: () => { movingName.value = ''; moveTarget.value = '' },
})

async function revealExport(id: string) {
  try {
    await WSLAPI.RevealDistroExport(id)
  } catch (e) {
    showToast(`打开位置失败: ${getErrorMessage(e)}`)
  }
}

// ---------- 单发行版详情（只读抽屉；历史名"取证"，内部符号沿用 forensics） ----------
// 后端红线：停止的发行版绝不进 guest（wsl -d 会顺手拉起）——抽屉里以 Notes 如实说明缺项。
interface ForensicsState { loading: boolean; error: string; data: DistroForensics | null }
const forensicsName = ref('')
const forensics = ref<Record<string, ForensicsState>>({})
const forensicsOf = computed(() => (forensicsName.value ? forensics.value[forensicsName.value] : undefined))

function toggleForensics(d: DistroInstance) {
  forensicsName.value = forensicsName.value === d.name ? '' : d.name
  if (forensicsName.value && !forensics.value[d.name]) void loadForensics(d.name)
}

async function loadForensics(name: string) {
  const prev = forensics.value[name]
  forensics.value = { ...forensics.value, [name]: { loading: true, error: '', data: prev?.data ?? null } }
  try {
    const data = await WSLAPI.GetDistroForensics(name)
    forensics.value = { ...forensics.value, [name]: { loading: false, error: '', data } }
  } catch (e) {
    forensics.value = { ...forensics.value, [name]: { loading: false, error: getErrorMessage(e), data: prev?.data ?? null } }
  }
}

// df 值以 MB 计，根盘常见数十 GB——fmtSize 上限只到 MB，这里升一档到 GB 呈现。
function mb(v: number): string {
  return v >= 1024 ? `${(v / 1024).toFixed(1)} GB` : `${v} MB`
}

// ---------- 克隆（异步事件驱动）与导入 ----------
// 克隆受理后转后台：wsl:clone 事件推进度，busy 一直挂到终态。
// 克隆是"单发行版重操作"：源发行版行关门，其他发行版照常可操作
//（数十 GB 拷完前不该让全页灰死；后端另有双名单飞闸兜底）。
const cloneSrc = ref('')
const cloneNewName = ref('')
const cloneTarget = ref('')
const cloneProg = ref<CloneProgress | null>(null)
const cloneBusy = computed(() => !!cloneProg.value && ['copying', 'importing'].includes(cloneProg.value.stage))
const CLONE_STAGE_TEXT: Record<string, string> = {
  copying: '拷贝数据盘中', importing: '挂载新实例中', done: '克隆完成', error: '克隆失败',
}

// 请求后端取消在飞克隆：拷贝段半成品自清，挂载段取消走失败清理路径（保留拷贝盘）。
async function requestCancelClone() {
  if (!cloneProg.value) return
  try {
    const out = await WSLAPI.CancelClone(cloneProg.value.source)
    showToast(out.message || '已请求取消')
  } catch (e) {
    showToast(`取消失败: ${getErrorMessage(e)}`)
  }
}

function startClone(d: DistroInstance) {
  cloneSrc.value = d.name
  cloneNewName.value = `${d.name}-Copy`
  // 目标预填「安装目录\<新实例名>」（与后端 underDir 同规则），可任意改动；
  // 安装目录留空时不猜，留占位符引导手填或 📁 选择。
  cloneTarget.value = previewSubdir(installDir.value, cloneNewName.value)
  cloneProg.value = null
}
function cancelClone() {
  if (cloneBusy.value) return // 在飞不收编：进度可见直到终态
  cloneSrc.value = ''
  cloneProg.value = null
}

async function submitClone(d: DistroInstance) {
  if (globalBusy.value || busyWith(`clone:${d.name}`)) return
  const newName = cloneNewName.value.trim()
  const target = cloneTarget.value.trim()
  const accepted = await confirm({
    title: `克隆 ${d.name} → ${newName}？`,
    tone: 'warning',
    description: `克隆会先关机「${d.name}」，再把数据盘整份复制到目标目录，副本以「${newName}」注册为新发行版（原发行版不受影响）。`
      + '数十 GB 时耗时数分钟，进度直接显示在本行。需要 WSL 2.7.3 以上；版本不足请改走「导出 → 导入」。'
      + '\n\n该操作以普通权限执行，不会弹出 UAC。',
    details: [
      { label: '新发行版名', value: newName },
      { label: '目标目录', value: target },
    ],
  })
  if (!accepted) return
  startOp(`clone:${d.name}`)
  cloneProg.value = { source: d.name, target: newName, stage: 'copying', done: 0, total: 0 }
  try {
    const out = (await WSLAPI.CloneDistro(d.name, newName, target)) as { message?: string }
    showToast(out.message || '已开始克隆')
  } catch (e) {
    finishOp(`clone:${d.name}`)
    // 失败行内保活：错误写进进度态，本行红条驻留可回看——toast 只是补充，不再是唯一线索
    cloneProg.value = { source: d.name, target: newName, stage: 'error', done: 0, total: 0, error: getErrorMessage(e) }
    showToast(`克隆失败: ${getErrorMessage(e)}`, { duration: 8000 })
    await loadInstances()
  }
}

useWailsEvent<CloneProgress>('wsl:clone', (p) => {
  if (!p) return
  cloneProg.value = p
  if (p.stage === 'done') {
    finishOp(`clone:${p.source}`)
    cloneSrc.value = ''
    cloneProg.value = null
    showToast(p.message || '克隆完成')
    void loadInstances()
  } else if (p.stage === 'error') {
    finishOp(`clone:${p.source}`) // 错误留在表单里可见，不吞
    void loadInstances()
  }
})

// ---------- /etc/wsl.conf 编辑器 ----------
// 读时若发行版已停止会被 wsl -d 顺手拉起（点「配置」即默认接受）；
// 写回的语法闸门/引用校验/写前备份全在后端，前端只呈现实情。
const confName = ref('')
const confDoc = ref<WslConfDoc | null>(null)
const confText = ref('')
const confLoading = ref(false)
const confError = ref('')

function openConf(d: DistroInstance) {
  if (confName.value === d.name) {
    closeConf()
    return
  }
  confName.value = d.name
  confDoc.value = null
  confText.value = ''
  void loadConf(d.name)
}
function closeConf() {
  if (confLoading.value || (confName.value && busyDistro(confName.value))) return
  confName.value = ''
  confDoc.value = null
  confText.value = ''
  confError.value = ''
}
async function loadConf(name: string) {
  confLoading.value = true
  confError.value = ''
  try {
    const doc = await WSLAPI.GetWslConf(name)
    confDoc.value = doc
    confText.value = doc?.text ?? ''
  } catch (e) {
    confError.value = getErrorMessage(e)
  } finally {
    confLoading.value = false
  }
}
const saveConf = (d: DistroInstance) => runDistroOp({
  name: `confsave:${d.name}`, title: `写回 ${d.name} 的 /etc/wsl.conf？`, tone: 'warning',
  desc: '后端三步防线：语法校验 → 默认用户存在性求证 → root 备份原文件后整文件写回并复验。'
    + '\nwsl.conf 只在发行版启动时读取——保存后需「⏹ 关机」（或「🔄 重启」）该发行版再进入才生效。',
  details: [{ label: '文件大小', value: `${new Blob([confText.value]).size} B` }],
  invoke: () => WSLAPI.SaveWslConf(d.name, confText.value),
  then: (out) => { if (out?.success) void offerTerminateAfterConf(d) },
})

// 保存成功即给"让配置生效"的下一步：终止后下次进入自然读到新 wsl.conf。
async function offerTerminateAfterConf(d: DistroInstance) {
  const yes = await confirm({
    title: `关机 ${d.name} 使新配置生效？`,
    tone: 'default',
    description: 'wsl.conf 只在发行版启动时读取。现在关机（数据无损，下次进入自动再开机）即可立刻生效；也可以稍后自行点「⏹ 关机」或「🔄 重启」。',
  })
  if (!yes) return
  try {
    const r = await WSLAPI.TerminateDistro(d.name)
    showToast(r?.message || '已关机，配置已生效')
  } catch (e) {
    showToast(`关机失败: ${getErrorMessage(e)}`)
  }
  await loadInstances()
}

// ---------- 磁盘瘦身（三级工作流，异步事件驱动） ----------
const compSrc = ref('')
const compProg = ref<CompactProgress | null>(null)
const compTerm = computed(() => !!compProg.value && ['done', 'error'].includes(compProg.value.stage))
const compBackupDir = ref('') // 留空=默认「下载\WSL 导出」；大盘备份常被 C 盘容量卡住
const compCancelable = computed(() => !!compProg.value && ['backup', 'trim', 'waiting'].includes(compProg.value.stage))

async function cancelCompact() {
  try {
    const out = await WSLAPI.CancelCompact()
    showToast(out.message || '已请求取消')
  } catch (e) {
    showToast(`取消被拒: ${getErrorMessage(e)}`) // 进入动盘段后如实拒绝，不假装停了
  }
}

function openCompact(d: DistroInstance) {
  compSrc.value = d.name
  compProg.value = null
  compBackupDir.value = ''
}
function closeCompact() {
  if (compSrc.value && !compTerm.value && busyAny.value) return // 在飞不收编
  compSrc.value = ''
  compProg.value = null
}

const COMPACT_STAGE_TEXT: Record<string, string> = {
  backup: '全量备份中', trim: 'fstrim/停机收敛中', optimize: '压缩数据盘中',
  reimport: '从备份重建中', done: '瘦身完成', error: '瘦身中止',
}

async function submitCompact(d: DistroInstance) {
  if (globalBusy.value || busyWith(`compact:${d.name}`)) return
  const accepted = await confirm({
    title: `为 ${d.name} 瘦身数据盘？`,
    tone: 'danger',
    description: '这是对数据盘的手术，全程分四步：'
      + '\n① 先强制做一份全量 tar 备份（需要约等于数据盘已用大小的额外空间）；'
      + '\n② 在发行版内做 fstrim 归还已删块，然后关闭发行版；'
      + '\n③ 用 Optimize-VHD 压缩数据盘（需要 Hyper-V 模块，会提权；省得不够就不做这步）；'
      + '\n④ 仍省得不够时，注销旧实例、从刚才的备份重建一个新实例（位置会换到新的旁路目录）。'
      + '\n\n任何一步失败都会如实中止并点名备份文件位置；完成后备份仍保留，确认无误后可自行删除。'
      + '\n数十 GB 盘耗时可达数十分钟，期间请勿退出 Hanxi。',
    details: [
      { label: '发行版', value: d.name },
      { label: '当前占用', value: d.sizeBytes ? fmtSize(d.sizeBytes) : '未知' },
    ],
  })
  if (!accepted) return
  startOp(`compact:${d.name}`)
  compProg.value = { name: d.name, stage: 'backup', beforeMB: 0, afterMB: 0 }
  try {
    const out = (await WSLAPI.CompactDistro(d.name, compBackupDir.value.trim())) as { message?: string }
    showToast(out.message || '已开始瘦身')
  } catch (e) {
    finishOp(`compact:${d.name}`)
    // 失败行内保活：错误进进度态，红条驻留本行（模板有 stage==='error' 分支）
    compProg.value = { name: d.name, stage: 'error', error: getErrorMessage(e), beforeMB: 0, afterMB: 0 }
    showToast(`瘦身未受理: ${getErrorMessage(e)}`, { duration: 8000 })
  }
}

useWailsEvent<CompactProgress>('wsl:compact', (p) => {
  if (!p) return
  compProg.value = p
  if (p.stage === 'done') {
    finishOp(`compact:${p.name}`)
    showToast(p.message || '瘦身完成')
    void loadInstances()
    void loadExportRecords() // 备份 tar 也在导出登记里
    exportLogOpen.value = true // 备份工件即刻可见
  } else if (p.stage === 'error') {
    finishOp(`compact:${p.name}`) // 错误留在表单里可见（含备份位置）
    void loadInstances()
  }
})

// ---------- ➕ 添加实例（三源统一入口，对齐 wsl-dashboard 的 AddInstanceView；
// 镜像站源刻意不做：第三方 rootfs 的信任链无法在本工具内把关） ----------
const addSource = ref<'store' | 'rootfs' | 'vhdx'>('store')
const addName = ref('')
const addFile = ref('')
// VHDX 两形态：false=就地注册（--import-in-place 零拷贝）；true=盘副本落位（--import … --vhd）。
const addVhdCopy = ref(false)
// 三源共用一个 addFile 输入：切源必清空——防止 tar 路径串进 VHDX 框（反之亦然）。
watch(addSource, () => { addFile.value = '' })
// 创建钮可用性：tar 必带落位目录；VHDX 就地挂载可免目录（盘留在原处）。
const canAddRootfs = computed(() =>
  !!addName.value.trim() && !!addFile.value.trim() && !!installDir.value.trim())
const canAddVhd = computed(() =>
  !!addName.value.trim() && !!addFile.value.trim() && (!addVhdCopy.value || !!installDir.value.trim()))
// 落位子目录预览：与后端 underDir 同规则（末级已是实例名则原样）。
function previewSubdir(base: string, id: string): string {
  const clean = base.trim().replace(/[\\/]+$/, '')
  const seg = (clean.split(/[\\/]/).pop() || '').toLowerCase()
  if (!clean) return ''
  return seg === id.toLowerCase() ? clean : `${clean}\\${id}`
}

async function pickAddFile(kind: 'rootfs' | 'vhdx') {
  try {
    const p = await WSLAPI.PickDistroImageDialog(kind)
    if (p) addFile.value = p // 取消返回空串：静默，保留手填通道
  } catch (e) {
    showToast(`打开系统文件选择框失败: ${getErrorMessage(e)}——可直接在输入框手动填写路径`)
  }
}

// 📁 选目录：系统文件夹框选好后回填输入框、仍可继续编辑（选好再改）——
// 克隆/迁移/瘦身备份/安装基目录四处目录输入统一走这扇门。
async function pickFolderInto(fill: (p: string) => void, title: string) {
  try {
    const p = await WSLAPI.PickFolderDialog(title)
    if (p) fill(p)
  } catch (e) {
    showToast(`打开系统文件夹选择框失败: ${getErrorMessage(e)}——可直接在输入框手动填写路径`)
  }
}

// rootfs tar → wsl --import（免 UAC）；目录走全局安装基目录。导入是全局互斥类（整链落盘）。
async function submitImport() {
  if (busyAny.value) return
  const name = addName.value.trim()
  const dir = installDir.value.trim()
  const tar = addFile.value.trim()
  const accepted = await confirm({
    title: `导入新发行版 ${name}？`,
    tone: 'warning',
    description: '执行 wsl --import：解包 tar 建立新发行版实例（数十 GB 时耗时数分钟，请勿退出）。'
      + 'tar 须为本工具导出产物或可信来源 rootfs；落位子目录须为空或不存在；名称与本机名单防撞。'
      + '\n\n该操作以普通权限执行，不会弹出 UAC。',
    details: [
      { label: 'tar 源', value: tar },
      { label: '落位目录', value: previewSubdir(dir, name) },
    ],
  })
  if (!accepted) return
  startOp('import')
  try {
    const out = await WSLAPI.ImportDistro(name, dir, tar)
    showToast(out?.message || '导入完成')
    addName.value = ''
    addFile.value = ''
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    finishOp('import')
    await loadInstances() // 导入可能部分生效：复采为准
  }
}

// 现有 VHDX 盘 → 新实例：就地注册（零拷贝，盘留在原处）或复制落位。
async function submitImportVhd() {
  if (busyAny.value) return
  const name = addName.value.trim()
  const dir = installDir.value.trim()
  const vhdx = addFile.value.trim()
  const copy = addVhdCopy.value
  const accepted = await confirm({
    title: `挂载 VHDX 为新发行版 ${name}？`,
    tone: 'warning',
    description: copy
      ? '执行 wsl --import … --vhd：微软语义即把数据盘【复制】到安装位置下的同名子目录（数十 GB 时耗时较长）。'
        + '适合接管备份盘且想换位置的场合；原盘保持不动。'
      : '执行 wsl --import-in-place：就地注册，零拷贝——该 VHDX 文件从此就是发行版的数据盘，'
        + '移动/删除它会直接伤及实例（想换位置请用挂载后的「🧭 迁移」）。',
    details: [
      { label: 'VHDX 盘', value: vhdx },
      ...(copy ? [{ label: '副本落位', value: previewSubdir(dir, name) }] : []),
    ],
  })
  if (!accepted) return
  startOp('import')
  try {
    const out = await WSLAPI.ImportDistroVhd(name, dir, vhdx, copy)
    showToast(out?.message || '挂载完成')
    addName.value = ''
    addFile.value = ''
  } catch (e) {
    showToast(`挂载失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    finishOp('import')
    await loadInstances()
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

// 首次切到对应页才拉数据（在线清单/netsh 现态均依赖本机命令，冷页避免无谓探测）。
// 官方清单懒加载并入「➕ 添加实例」商店源（原「📦 官方发行版」页签已删）。
watch(activeMainTab, (tab) => {
  if (tab === 'add' && online.value.length === 0 && !onlineLoading.value && !onlineError.value) {
    loadOnline()
  }
  if (tab === 'proxy' && !proxyView.value && !proxyLoading.value) {
    void loadProxy()
  }
})

// ---------- 端口转发（NAT netsh portproxy 账本） ----------
const proxyView = ref<PortProxyView | null>(null)
const proxyLoading = ref(false)
const proxyError = ref('')
// 默认只开本机回环；0.0.0.0 属"向局域网曝光"的显式选择（UI 有警示条）。
const newRule = reactive({ distro: '', port: '', guest: '', listen: '127.0.0.1', firewall: false, note: '' })

async function resetProxyLedger() {
  const accepted = await confirm({
    title: '清空端口转发规则清单文件？',
    tone: 'warning',
    description: '仅删除 Hanxi 的规则清单（清单文件损坏或想彻底重来时的逃生口）——不摘除系统里已生效的转发，'
      + '它们会转列为「外部转发」由你自行处置。日常删除规则请用「🧹 清理托管」。',
  })
  if (!accepted) return
  try {
    const out = await WSLAPI.ClearPortLedgerFile()
    showToast(out?.message || '规则清单已清空')
    await loadProxy()
  } catch (e) {
    showToast(`清空失败: ${getErrorMessage(e)}`)
  }
}

async function loadProxy() {
  proxyLoading.value = true
  proxyError.value = ''
  try {
    proxyView.value = await WSLAPI.ListPortRules()
  } catch (e) {
    proxyError.value = `读取端口转发失败: ${getErrorMessage(e)}\n需要本机 netsh（PowerShell 通道）可用；若安全软件拦截了 netsh，请如实处理后再刷新。`
  } finally {
    proxyLoading.value = false
  }
}

async function addProxyRule() {
  if (globalBusy.value) return
  const port = Number(newRule.port.trim())
  const guest = Number(newRule.guest.trim() || '0')
  if (!newRule.distro || !Number.isInteger(port)) {
    showToast('请填写发行版与合法的本机监听端口（1-65535）')
    return
  }
  try {
    const r = await WSLAPI.AddPortRule(newRule.distro, port, guest, newRule.listen.trim() || '127.0.0.1', newRule.firewall, newRule.note.trim())
    showToast(`已加入规则清单 ${r?.listen}:${r?.port} → ${r?.distro}（点「▶ 应用规则」才挂进系统）`)
    newRule.port = ''
    newRule.guest = ''
    newRule.note = ''
    await loadProxy()
  } catch (e) {
    showToast(`添加规则失败: ${getErrorMessage(e)}`)
  }
}

const rulePayload = (r: PortRuleView, patch: Partial<PortRule> = {}): PortRule => ({
  id: r.id, distro: r.distro, port: r.port, guest: r.guest, listen: r.listen,
  firewall: r.firewall, note: r.note ?? '', enabled: r.enabled, ...patch,
})

async function updateRule(r: PortRuleView, patch: Partial<PortRule>) {
  try {
    await WSLAPI.UpdatePortRule(rulePayload(r, patch))
  } catch (e) {
    showToast(`更新规则失败: ${getErrorMessage(e)}`)
  }
  await loadProxy()
}

async function removeRule(r: PortRuleView) {
  const accepted = await confirm({
    title: `删除规则 ${r.listen}:${r.port} → ${r.distro}？`,
    tone: 'warning',
    description: '只从规则清单删除；若系统里还有对应转发，随后点「▶ 应用规则」会一并摘除（提权执行）。',
  })
  if (!accepted) return
  try {
    const out = await WSLAPI.RemovePortRule(r.id)
    showToast(out?.message || '已删除')
  } catch (e) {
    showToast(`删除失败: ${getErrorMessage(e)}`)
  }
  await loadProxy()
}

const applyProxy = () => runOp({
  name: 'proxy-apply', title: '应用端口转发规则到系统？',
  desc: '单批 netsh portproxy 与防火墙放行命令在同一个提权脚本内执行（先删后加幂等，逐条传播退出码）；'
    + '未运行的发行版会被跳过并在回执点名，不会被顺手拉起。外部程序登记的转发一律不触碰。',
  invoke: async () => {
    const out = await WSLAPI.ApplyPortRules()
    await loadProxy()
    return out
  },
})

const cleanupProxy = () => runOp({
  name: 'proxy-cleanup', title: '清理 Hanxi 登记的全部端口转发？', tone: 'danger', confirmLabel: '摘除并清空',
  desc: '从系统摘除规则清单登记的转发与 "Hanxi WSL *" 防火墙放行（UAC 提权），并清空清单文件。'
    + '\n外部程序的转发不受影响；如你还想恢复，需重新添加规则。',
  invoke: async () => {
    const out = await WSLAPI.CleanupPortRules()
    await loadProxy()
    return out
  },
})

function ruleDrift(r: PortRuleView): boolean {
  return r.applied && !!r.targetIP && !!r.activeIP && r.activeIP !== r.targetIP
}

// ---------- .wslconfig（宿主全局配置：网络模式等） ----------
const hostConfOpen = ref(false)
const hostDoc = ref<HostConfDoc | null>(null)
const hostText = ref('')
const hostLoading = ref(false)
const hostError = ref('')

const MODE_WORD: Record<string, string> = { nat: 'NAT（默认）', bridged: '桥接', mirrored: '镜像网络', unknown: '未知/非法值' }
const modeWord = (m?: string | null) => MODE_WORD[m || 'nat'] || 'NAT（默认）'

async function toggleHostConf() {
  if (hostConfOpen.value) {
    if (globalBusy.value || hostLoading.value) return
    hostConfOpen.value = false
    return
  }
  hostConfOpen.value = true
  if (!hostDoc.value) await loadHostConf()
}
async function loadHostConf() {
  hostLoading.value = true
  hostError.value = ''
  try {
    const doc = await WSLAPI.GetWslHostConf()
    hostDoc.value = doc
    hostText.value = doc?.text ?? ''
  } catch (e) {
    hostError.value = getErrorMessage(e)
  } finally {
    hostLoading.value = false
  }
}
async function saveHostConf() {
  if (busyAny.value) return
  const accepted = await confirm({
    title: '写回 .wslconfig（宿主全局配置）？',
    tone: 'warning',
    description: '该文件影响【所有发行版】（网络模式、资源上限等），保存后需 wsl --shutdown 停止全部才生效。'
      + '\n\n写回防线：INI 语法闸门 + networkingMode 白名单（nat/bridged/mirrored）+ 原文件备份到 .wslconfig.hanxi.bak + 原子写后读回复核。',
  })
  if (!accepted) return
  startOp('wslconfig')
  let saved = false
  try {
    const out = await WSLAPI.SaveWslHostConf(hostText.value)
    showToast(out?.message || '已写回')
    await loadHostConf()
    saved = !!out?.success
  } catch (e) {
    showToast(`保存失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    finishOp('wslconfig')
  }
  // 锁已放再走第二问（三连并两连）：offer→doShutdown 不能被自己刚释放的 wslconfig 锁挡住
  if (saved) await offerShutdownForHostConf()
}
// .wslconfig 三连并两连：这里问"要不要立即生效"，用户点头后 doShutdown 不再重复第三问。
async function offerShutdownForHostConf() {
  const yes = await confirm({
    title: '立即「🌑 停止全部」使新配置生效？',
    tone: 'warning',
    description: 'wsl --shutdown 会终止【所有发行版】正在运行的会话（数据无损，下次访问自动重启）。'
      + '\n里面有跑着的任务就别现在停，稍后自行点「🌑 停止全部」。',
  })
  if (yes) await doShutdown(true)
}
async function doShutdown(alreadyConfirmed = false) {
  if (busyAny.value) return
  if (!alreadyConfirmed) {
    const accepted = await confirm({
      title: '停止全部 WSL（wsl --shutdown）？',
      tone: 'warning',
      description: '终止全部发行版会话：数据无损，下次进入自动重启。这是 .wslconfig 全局改动与网络模式切换的生效前提。',
    })
    if (!accepted) return
  }
  startOp('shutdown')
  try {
    const out = await WSLAPI.ShutdownWsl()
    showToast(out?.message || '已全部停止')
  } catch (e) {
    showToast(`停止全部失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    finishOp('shutdown')
    await loadInstances()
    await loadProxy()
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

// 安装落位基目录（「➕ 添加实例」页全局行，三源共用）：默认 D:\wsl——每个实例落在其下
// 同名子目录（后端 underDir 把关）；留空=系统默认（通常 C 盘，UI 有警示条）。
// 持久化走后端 RPC（不再用 localStorage——旧实现 stored==='' 时默认值 D:\wsl 永远回不来，
// 且分不清"从未设置"与"显式选了系统默认"）：
//   GetDistroInstallDir → { set, dir }：set=false 从未设置过（回退默认 D:\wsl）；
//                          set=true 且 dir='' 为用户显式选择「系统默认位置」（输入框留空）。
//   SetDistroInstallDir(dir)：改动防抖 500ms 写回；失败 console.warn 静默降级为本会话内存值。
// 商店安装由后端在同一条提权链里"装完即迁"（wsl --install 不支持目标目录参数）；rootfs/VHDX 导入直接落位。
const DEFAULT_INSTALL_DIR = 'D:\\wsl'
const installDir = ref('')
// installDirSynced：当前与后端一致的落位值——拉取回显/改回原值都不触发回写，
// 只有真正的用户改动经防抖才 SetDistroInstallDir。
let installDirSynced: string | null = null
let installDirTimer: ReturnType<typeof setTimeout> | null = null
// TODO(接线): 后端绑定 GetDistroInstallDir/SetDistroInstallDir 生成后，去掉此处 (WSLAPI as any)
// 收敛改为直调 WSLAPI；当前以可选调用兜住"绑定尚未生成"（缺失即静默降级，不拦操作）。
const getDistroInstallDir = async (): Promise<{ set: boolean; dir: string }> =>
  (await (WSLAPI as any).GetDistroInstallDir?.()) ?? { set: false, dir: '' }
const setDistroInstallDir = (dir: string): Promise<void> =>
  Promise.resolve((WSLAPI as any).SetDistroInstallDir?.(dir))
async function loadInstallDir() {
  let next = DEFAULT_INSTALL_DIR
  try {
    const pref = await getDistroInstallDir()
    if (pref.set && pref.dir.trim() === '') next = '' // 显式选择系统默认位置
    else if (pref.set) next = pref.dir
  } catch (e) {
    console.warn('[wsl] 拉取安装基目录失败，本会话用默认值:', getErrorMessage(e))
  }
  installDirSynced = next.trim()
  installDir.value = next
}
watch(installDir, (v) => {
  if (installDirTimer) clearTimeout(installDirTimer)
  const dir = v.trim()
  if (dir === installDirSynced) return // 回显或改回原值：不打扰后端
  installDirTimer = setTimeout(() => {
    installDirSynced = dir
    setDistroInstallDir(dir).catch((e) => {
      console.warn('[wsl] 安装基目录写回失败（仅失去跨会话记忆，不拦操作）:', getErrorMessage(e))
    })
  }, 500)
})

async function installDistro(opt: DistroOption) {
  const dir = installDir.value.trim()
  await runOp({
    name: `distro-${opt.id}`,
    title: `安装发行版 ${opt.id}？`,
    tone: 'default',
    desc: `执行 wsl --install -d ${opt.id}：下载安装后首次进入该发行版需创建 Linux 用户名与密码。`
      + (dir
          ? `\n\n装完将自动迁移落位到：${previewSubdir(dir, opt.id)}\n（基目录 + 同名子目录；同一条提权链一次 UAC 完成——wsl --install 本身不支持指定目录，"装完即迁"是唯一正规通道。）`
          : '\n\n当前安装基目录留空——将装到系统默认位置（通常在 C 盘）。想避开 C 盘，请在上方「安装基目录」行填写（默认 D:\\wsl），改动会自动记住。'),
    invoke: () => WSLAPI.InstallDistroTo(opt.id, dir),
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

// ---------- 行级「⋯ 更多」下拉（自研：relative 容器 + v-if 面板；外点/Esc 关闭） ----------
// 同时只开一行；触发内联表单展开后自动收合面板。Esc 走全局 keydown，外点走 document
// pointerdown（命中 .row-menu 内部则忽略），两者仅在面板打开期间挂载、卸载时摘除。
const rowMenuName = ref('')
function toggleRowMenu(name: string) {
  rowMenuName.value = rowMenuName.value === name ? '' : name
}
function closeRowMenu() { rowMenuName.value = '' }
function rowMenuAction(fn: () => void) { closeRowMenu(); fn() }
function onRowMenuDocDown(e: Event) {
  if (rowMenuName.value && !(e.target as HTMLElement | null)?.closest?.('.row-menu')) closeRowMenu()
}
function onRowMenuDocKey(e: KeyboardEvent) {
  if (e.key === 'Escape') closeRowMenu()
}
watch(rowMenuName, (open) => {
  if (open) {
    document.addEventListener('pointerdown', onRowMenuDocDown)
    document.addEventListener('keydown', onRowMenuDocKey)
  } else {
    document.removeEventListener('pointerdown', onRowMenuDocDown)
    document.removeEventListener('keydown', onRowMenuDocKey)
  }
})

onMounted(() => {
  startCheck()
  loadInstances() // 与体检并行的独立通道：未装 WSL 时后端返回空列表，不报错
  loadExportRecords()
  loadReleases()
  loadInstallDir() // 安装基目录持久化以后端为准（W1，替代旧 localStorage）
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onRowMenuDocDown)
  document.removeEventListener('keydown', onRowMenuDocKey)
  if (installDirTimer) clearTimeout(installDirTimer)
})
</script>

<template>
  <section class="page wsl-view">
    <PageHeader title="WSL2" subtitle="Windows Subsystem for Linux：就绪体检、GitHub 通道诊断、官方版本管理、发行版安装/管理与正规卸载。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" id-prefix="wsl-main" label="WSL 功能页签" />
      </template>
    </PageHeader>

    <!-- 控制台 Tab：就绪体检 -->
    <div v-show="activeMainTab === 'console'" id="wsl-main-console-panel" role="tabpanel" aria-labelledby="wsl-main-console-tab" class="tab-body">
      <!-- 常驻进度条：任何在飞操作在全部页签可见（文案如实区分提权/用户态） -->
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
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
          </div>
          <div class="control-btns">
            <!-- 忙时不换字防宽度跳动：保留原文案，追加内联 spinner；进度语义统一由顶部常驻条呈现 -->
            <button class="btn btn-primary btn-small" :disabled="busyAny || streaming"
              title="wsl --install --no-distribution：只装本体+启用虚拟机平台，绝不自动捆绑发行版（UAC 提权）"
              @click="installWsl">🚀 一键开启<span v-if="busyWith('install')" class="btn-spin" aria-hidden="true"></span></button>
            <button class="btn btn-secondary btn-small" :disabled="busyAny || !report?.wslVersion"
              :title="report?.wslVersion ? 'wsl --update：默认通道更新' : '尚未安装 WSL'"
              @click="updateWsl">🔄 更新</button>
            <button class="btn btn-secondary btn-small" :disabled="busyAny || !report?.wslVersion"
              title="wsl --update --web-download：商店通道不通时 GitHub 直连更新"
              @click="updateWeb">🌐 直连更新</button>
            <button class="btn btn-secondary btn-small" :disabled="busyAny"
              title="wsl --set-default-version 2：新装发行版默认用 WSL2"
              @click="setDefaultV2">2️⃣ 默认WSL2</button>
            <button class="btn btn-secondary btn-small" :disabled="busyAny" :class="{ active: hostConfOpen }"
              title="编辑宿主全局配置 .wslconfig（网络模式/资源上限；影响所有发行版，「停止全部」后生效）"
              @click="toggleHostConf">🌐 .wslconfig</button>
            <button class="btn btn-secondary btn-small" :disabled="busyAny"
              title="wsl --shutdown：停止全部发行版与 WSL 虚拟机（数据无损；单个发行版请用发行版页「⏹ 关机」），.wslconfig 改动的生效前提"
              @click="doShutdown()">🌑 停止全部</button>
            <button class="btn btn-secondary btn-small" :disabled="streaming" @click="startCheck">
              {{ streaming ? '体检中…' : '↻ 重新体检' }}
            </button>
            <button class="btn btn-danger-outline btn-small" :disabled="busyAny"
              title="正规双路卸载：MSI 官方卸载向导 + MSIX 用户包移除；不触碰注册表与可选功能"
              @click="uninstallWsl">🗑 卸载 WSL</button>
            <!-- 虚拟机平台状态驱动开关对：体检报告到位后按实际状态呈现其一 -->
            <button v-if="report?.vmPlatformEnabled" class="btn btn-danger-outline btn-small" :disabled="busyAny"
              title="经 DISM 关闭虚拟机平台/WSL 可选功能（影响 Hyper-V、安卓模拟器等共用地基，谨慎）"
              @click="disableFeatures">🧨 关闭虚拟机平台</button>
            <button v-else-if="report" class="btn btn-secondary btn-small" :disabled="busyAny"
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

      <!-- .wslconfig 全局配置面板（宿主文件，网络模式等） -->
      <div v-if="hostConfOpen" class="import-panel">
        <UiBanner tone="warn" class="slim">
          .wslconfig 是<b>宿主全局配置</b>（网络模式、内存/CPU 上限等），作用于所有发行版，
          需「🌑 停止全部」或重启后才被读取。写回防线同 wsl.conf：语法闸门 + networkingMode 白名单 + 写前备份（.wslconfig.hanxi.bak）+ 原子写读回复核。
        </UiBanner>
        <div class="move-input-row">
          <UiStatusChip tone="information">当前网络模式：{{ modeWord(hostDoc?.networkMode) }}</UiStatusChip>
          <span v-if="hostDoc?.missing" class="hint-dim">文件尚不存在——保存即首建（缺省即 NAT）</span>
          <code v-if="hostDoc?.path" class="mono dim">{{ hostDoc.path }}</code>
        </div>
        <div v-if="hostLoading && !hostDoc" class="hint-line">读取 ~/.wslconfig…</div>
        <div v-else-if="hostError && !hostDoc" class="error-box">{{ hostError }}
          <button class="btn btn-secondary btn-small retry-inline" @click="loadHostConf">↻ 重试</button>
        </div>
        <template v-else-if="hostDoc">
          <UiBanner v-for="(w, i) in hostDoc.warnings ?? []" :key="i" tone="warn" class="slim">{{ w }}</UiBanner>
          <textarea v-model="hostText" class="input conf-textarea mono" rows="6" spellcheck="false"
            :disabled="globalBusy || busyWith('wslconfig')" :placeholder="`[wsl2]&#10;memory=8GB&#10;&#10;[networking]&#10;networkingMode=mirrored`"></textarea>
          <div class="move-input-row">
            <button class="btn btn-primary btn-small" :disabled="busyAny || hostLoading"
              @click="saveHostConf">{{ busyWith('wslconfig') ? '保存中…' : '✔ 保存写回' }}</button>
            <button class="btn btn-secondary btn-small" :disabled="globalBusy || busyWith('wslconfig')" @click="loadHostConf">↻ 重读</button>
            <button class="btn btn-secondary btn-small" :disabled="globalBusy || busyWith('wslconfig')" @click="toggleHostConf">收起</button>
          </div>
        </template>
      </div>

      <!-- 逐项结论：骨架先行，事件分相点亮；复采时保留旧值并在此标注（Stale 态基线） -->
      <div class="section-title">
        <h3>逐项体检</h3>
        <span v-if="streaming && report" class="hint-dim">复采中…（以下为上一轮结果，新结果到达即覆盖）</span>
      </div>
      <ul class="check-list">
        <li v-for="sk in CHECK_SKELETON" :key="sk.key" class="check-row" :class="{ pending: !arrived[sk.key] }">
          <div class="check-main">
            <UiStatusChip v-if="arrived[sk.key]" :tone="chipTone(arrived[sk.key].state)">{{ stateWord(arrived[sk.key].state) }}</UiStatusChip>
            <UiStatusChip v-else tone="neutral"><span class="pending-dot"></span>检测中</UiStatusChip>
            <span class="check-label">{{ sk.label }}</span>
            <code v-if="arrived[sk.key]" class="mono check-value">{{ arrived[sk.key]!.value }}</code>
            <!-- 完成落章：✓ 通过（绿）/ ⚠ 注意 / ✕ 阻塞 / ℹ 已检测不判定（灰）——每行到齐都有记号；
                 info 不与 pass 共用灰勾：记号形状本身也要区分语义，不只靠颜色 -->
            <span v-if="arrived[sk.key]" class="check-trail" :class="arrived[sk.key]!.state">
              {{ arrived[sk.key]!.state === 'warn' ? '⚠' : arrived[sk.key]!.state === 'bad' ? '✕' : arrived[sk.key]!.state === 'info' ? 'ℹ' : '✓' }}
            </span>
          </div>
          <div v-if="arrived[sk.key]" class="check-detail">{{ arrived[sk.key]!.detail }}</div>
        </li>
      </ul>

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

    <!-- 本机发行版 Tab：实例管理控制台（状态归一列表 + 行内操作，复采通道独立于体检报告） -->
    <div v-show="activeMainTab === 'distros'" id="wsl-main-distros-panel" role="tabpanel" aria-labelledby="wsl-main-distros-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <template v-if="distroTabReady">
        <div class="section-title distro-head">
          <h3>本机发行版 ({{ instances.length }})</h3>
          <div class="btn-group distro-head-actions">
            <button class="btn btn-secondary btn-small" :disabled="instLoading" @click="loadInstances">
              {{ instLoading ? '刷新中…' : '↻ 刷新列表' }}
            </button>
          </div>
        </div>
        <div v-if="instError" class="error-box">{{ instError }}
          <button class="btn btn-secondary btn-small retry-inline" @click="loadInstances">↻ 重试</button>
        </div>
        <div v-else-if="instLoading && !instances.length" class="hint-line">正在经本机 wsl.exe 读取发行版现状…</div>
        <div v-else-if="!instances.length" class="empty-state">
          <p>WSL 已安装但还没有发行版 —— 到「➕ 添加实例」页挑一个来源（官方商店 / rootfs / VHDX 盘）。</p>
        </div>
        <div v-else class="table-container">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width: 18%;">发行版</th>
                <th style="width: 10%;">状态</th>
                <th style="width: 8%;">WSL 版本</th>
                <th style="width: 10%;">磁盘占用</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="d in instances" :key="d.name">
                <tr>
                  <td>
                    <code class="mono">{{ d.name }}</code>
                    <UiStatusChip v-if="d.default" tone="information">★ 默认</UiStatusChip>
                  </td>
                  <td>
                    <!-- 状态归一呈现：原文是本地化文案（跨语言系统不可作判据），仅收进 title 备查 -->
                    <UiStatusChip :tone="d.running ? 'positive' : 'neutral'" :title="d.stateText">
                      {{ d.running ? '运行中' : '已停止' }}
                    </UiStatusChip>
                  </td>
                  <td class="mono">{{ d.version }}</td>
                  <td class="mono dim" :title="d.vhdxPath || d.basePath || undefined">{{ d.sizeBytes ? fmtSize(d.sizeBytes) : '—' }}</td>
                  <td>
                    <!-- 行内常驻 4 钮（终端/重启/关机/删除），次要操作收「⋯ 更多」；
                         只读钮不受 busy 分级锁，写钮只锁"全局在飞或本行正忙" -->
                    <div class="distro-actions">
                      <button class="btn btn-secondary btn-small"
                        title="唤起系统默认终端进入该发行版（WSL 无前台进程时会自动停机，本工具不做后台保活）"
                        @click="openTerminal(d)">⌨ 终端</button>
                      <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                        title="重启：停止→确认已停→拉起验证（不做后台保活，空闲后自动回落停止；wsl.conf 改动的生效捷径）"
                        @click="restartDistro(d)">🔄 重启</button>
                      <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name) || !d.running"
                        :title="d.running ? 'wsl --terminate：等同关掉本发行版的虚拟机电源（硬停；数据盘无损，下次访问自动再启动）；停全部请到就绪检测页「🌑 停止全部」' : '当前已停止'"
                        @click="terminateDistro(d)">⏹ 关机</button>
                      <button class="btn btn-danger-outline btn-small" :disabled="rowBusy(d.name)"
                        title="wsl --unregister：数据销毁级删除（连带清理独占的商店启动器），不可恢复"
                        @click="unregisterDistro(d)">🗑 删除</button>
                      <div class="row-menu">
                        <button class="btn btn-secondary btn-small" :aria-expanded="rowMenuName === d.name ? 'true' : 'false'" aria-haspopup="true"
                          title="文件 / 设默认 / 导出 / 迁移 / 克隆 / 瘦身 / 详情 / wsl.conf"
                          @click="toggleRowMenu(d.name)">⋯ 更多</button>
                        <div v-if="rowMenuName === d.name" class="row-menu-panel" role="menu" @keydown.esc.stop="closeRowMenu">
                          <button class="row-menu-item" role="menuitem"
                            title="资源管理器打开 \\wsl$\<发行版> 浏览文件系统（停止时会被顺手拉起；只读入口免确认）"
                            @click="rowMenuAction(() => openFolder(d))">📂 文件</button>
                          <button class="row-menu-item" role="menuitem" :disabled="rowBusy(d.name) || d.default"
                            :title="d.default ? '已是默认发行版' : 'wsl --set-default：不带 -d 的 wsl 命令与控制台默认进入它'"
                            @click="rowMenuAction(() => setDefaultDistro(d))">⭐ 设默认</button>
                          <button class="row-menu-item" role="menuitem" :disabled="rowBusy(d.name) || (!!exportingName && exportingName !== d.name)"
                            title="wsl --export：选择格式（tar.gz 压缩 / tar 未压缩）导出到「下载」文件夹，可 long-running"
                            @click="rowMenuAction(() => startExport(d))">📤 导出</button>
                          <button class="row-menu-item" role="menuitem" :disabled="rowBusy(d.name) || (!!movingName && movingName !== d.name)"
                            title="wsl --manage --move：迁移数据盘到其他盘（UAC 提权，会先停机全部 WSL）"
                            @click="rowMenuAction(() => startMove(d))">🧭 迁移</button>
                          <button class="row-menu-item" role="menuitem" :disabled="rowBusy(d.name) || (!!cloneSrc && cloneSrc !== d.name)"
                            title="克隆：关机源发行版 → 整盘复制数据盘 → 副本就地挂为新发行版（原实例不动；需 WSL 2.7.3+，免 UAC）"
                            @click="rowMenuAction(() => startClone(d))">🧬 克隆</button>
                          <button class="row-menu-item" role="menuitem" :disabled="rowBusy(d.name) || (!!compSrc && compSrc !== d.name)"
                            title="数据盘瘦身：备份→fstrim→Optimize-VHD 压缩→不足则从备份注销重建（会换落位目录）"
                            @click="rowMenuAction(() => openCompact(d))">🗜 瘦身</button>
                          <button class="row-menu-item" role="menuitem" :class="{ active: forensicsName === d.name }"
                            title="只读详情：VHDX 逻辑/实占与稀疏、根盘用量、IPv4、网络模式（停止时不进 guest，以免顺手启动它）"
                            @click="rowMenuAction(() => toggleForensics(d))">📋 详情</button>
                          <button class="row-menu-item" role="menuitem" :disabled="rowBusy(d.name) || (!!confName && confName !== d.name)"
                            :class="{ active: confName === d.name }"
                            title="编辑 /etc/wsl.conf（systemd/automount/默认用户等）：读时会启动发行版；写回有语法闸门 + 引用校验 + 写前备份"
                            @click="rowMenuAction(() => openConf(d))">⚙ wsl.conf</button>
                        </div>
                      </div>
                    </div>
                  </td>
                </tr>
                <tr v-if="movingName === d.name" class="move-row-editor">
                  <td colspan="5">
                    <div class="move-editor">
                      <UiBanner tone="warn" class="slim">
                        迁移会先执行 <code class="mono">wsl --shutdown</code> 打停整个 WSL 子系统<template v-if="moveRunningOthers.length">——当前运行中的
                        <b>{{ moveRunningOthers.map(i => i.name).join('、') }}</b> 会被连带终止<template v-if="d.running">（<b>{{ d.name }}</b> 本身也在运行，可先「⏹ 关机」缩小影响面）</template></template>，随后提权移动数据盘（UAC 授权）。目标须为空目录或不存在的路径，路径合法性由后端把关。
                      </UiBanner>
                      <div class="move-input-row">
                        <label class="move-label" for="wsl-move-target">目标目录</label>
                        <input id="wsl-move-target" v-model="moveTarget" class="input mono"
                          :placeholder="`D:\\WSL\\${d.name}`" spellcheck="false" :disabled="rowBusy(d.name)"
                          @keyup.enter="moveTarget.trim() && confirmMove(d)" />
                        <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                          title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => moveTarget = p, `选择 ${d.name} 的迁移目标目录`)">📁 选目录</button>
                        <button class="btn btn-primary btn-small" :disabled="!moveTarget.trim() || rowBusy(d.name)"
                          @click="confirmMove(d)">{{ busyWith(`move:${d.name}`) ? '迁移中…' : '✔ 确认迁移' }}</button>
                        <button class="btn btn-secondary btn-small" :disabled="busyWith(`move:${d.name}`)" @click="cancelMove">取消</button>
                      </div>
                      <div v-if="!moveTarget.trim()" class="hint-line">目标目录为空，「确认迁移」已置灰——填入或 📁 选一个非空路径即可开始。</div>
                    </div>
                  </td>
                </tr>
                <tr v-if="exportingName === d.name" class="export-row-editor">
                  <td colspan="5">
                    <div class="move-editor">
                      <div class="move-input-row">
                        <label class="move-label">导出格式</label>
                        <label class="radio-label"><input v-model="exportFormat" type="radio" value="gz" :disabled="busyWith(`export:${d.name}`)" />
                          tar.gz 压缩（默认，体积小；需 WSL 2.4.4+）</label>
                        <label class="radio-label"><input v-model="exportFormat" type="radio" value="tar" :disabled="busyWith(`export:${d.name}`)" />
                          tar 未压缩（更快、兼容老版本，体积大）</label>
                        <button class="btn btn-primary btn-small" :disabled="rowBusy(d.name)"
                          @click="confirmExport(d)">{{ busyWith(`export:${d.name}`) ? '导出中…' : '✔ 开始导出' }}</button>
                        <button class="btn btn-secondary btn-small" :disabled="busyWith(`export:${d.name}`)" @click="cancelExport">取消</button>
                      </div>
                    </div>
                  </td>
                </tr>
                <tr v-if="cloneSrc === d.name" class="clone-row-editor">
                  <td colspan="5">
                    <div class="move-editor">
                      <template v-if="cloneProg && cloneProg.source === d.name">
                        <div class="move-input-row">
                          <UiStatusChip :tone="cloneProg.stage === 'error' ? 'danger' : 'information'">
                            {{ CLONE_STAGE_TEXT[cloneProg.stage] ?? cloneProg.stage }}
                          </UiStatusChip>
                          <span v-if="(cloneProg.stage === 'copying' || cloneProg.stage === 'importing') && cloneProg.total" class="fx-bar">
                            <UiProgressBar :percent="Math.min(99, Math.round(cloneProg.done / cloneProg.total * 100))" />
                          </span>
                          <span v-if="cloneProg.total" class="mono dim">{{ fmtSize(cloneProg.done) }} / {{ fmtSize(cloneProg.total) }}</span>
                        </div>
                        <div v-if="cloneProg.stage === 'error'" class="error-box">{{ cloneProg.error }}
                          <button class="btn btn-secondary btn-small retry-inline" @click="cancelClone">知道了，收起</button>
                        </div>
                        <div v-if="cloneBusy" class="move-input-row">
                          <button class="btn btn-secondary btn-small" @click="requestCancelClone">✋ 取消克隆</button>
                          <span class="hint-dim">拷贝段取消会清理半成品；挂载段取消则保留已拷盘并点名路径</span>
                        </div>
                      </template>
                      <template v-else>
                        <UiBanner v-if="d.running" tone="warn" class="slim">
                          源发行版 <b>{{ d.name }}</b> 正在运行：克隆开始时会先终止它（数据无损，完成后可再启动）。
                        </UiBanner>
                        <div class="move-input-row">
                          <label class="move-label" for="wsl-clone-name">新发行版名</label>
                          <input id="wsl-clone-name" v-model="cloneNewName" class="input mono" spellcheck="false" :disabled="rowBusy(d.name)" />
                          <label class="move-label" for="wsl-clone-target">目标目录</label>
                          <input id="wsl-clone-target" v-model="cloneTarget" class="input mono" :placeholder="`D:\\WSL\\${d.name}-Copy`"
                            spellcheck="false" :disabled="rowBusy(d.name)"
                            @keyup.enter="cloneNewName.trim() && cloneTarget.trim() && submitClone(d)" />
                          <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                            title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => cloneTarget = p, `选择 ${d.name} 克隆副本的落位目录`)">📁 选目录</button>
                          <button class="btn btn-primary btn-small" :disabled="!cloneNewName.trim() || !cloneTarget.trim() || rowBusy(d.name)"
                            @click="submitClone(d)">✔ 开始克隆</button>
                          <button class="btn btn-secondary btn-small" :disabled="busyWith(`clone:${d.name}`)" @click="cancelClone">取消</button>
                        </div>
                        <div v-if="!cloneNewName.trim() || !cloneTarget.trim()" class="hint-line">
                          名称或目标目录为空，「开始克隆」已置灰——副本必须有一个明确的落位目录（留空没有意义可猜）。
                        </div>
                      </template>
                    </div>
                  </td>
                </tr>
                <tr v-if="compSrc === d.name" class="compact-row-editor">
                  <td colspan="5">
                    <div class="move-editor">
                      <template v-if="compProg">
                        <div class="move-input-row">
                          <UiStatusChip :tone="compProg.stage === 'error' ? 'danger' : compProg.stage === 'done' ? 'positive' : 'information'">
                            {{ COMPACT_STAGE_TEXT[compProg.stage] ?? compProg.stage }}
                          </UiStatusChip>
                          <UiStatusChip v-if="compProg.tier" tone="neutral">{{ compProg.tier === 'tier2' ? '已走备份重建' : compProg.tier === 'tier1' ? '就地压缩完成' : compProg.tier }}</UiStatusChip>
                          <span v-if="compProg.stage === 'done' && compProg.afterMB" class="mono dim">
                            {{ mb(compProg.beforeMB) }} → {{ mb(compProg.afterMB) }}</span>
                        </div>
                        <div v-if="compProg.stage === 'error'" class="error-box">{{ compProg.error }}</div>
                        <div v-else-if="compProg.message" class="hint-line">{{ compProg.message }}</div>
                        <div class="move-input-row">
                          <button v-if="compCancelable" class="btn btn-danger-outline btn-small" @click="cancelCompact">✋ 取消瘦身</button>
                          <button class="btn btn-secondary btn-small" :disabled="!compTerm" @click="closeCompact">
                            {{ compTerm ? '收起' : '在飞（终态后可收起）' }}</button>
                          <span v-if="!compCancelable && !compTerm" class="hint-dim">已进入数据盘处理/重建段，取消会被拒绝（有备份兜底，等它跑完）</span>
                        </div>
                      </template>
                      <template v-else>
                        <UiBanner tone="error" class="slim">
                          瘦身是对数据盘的手术：<b>强制先全量备份</b>，再 fstrim/停机、Optimize-VHD 压缩数据盘（需 Hyper-V 模块，UAC），
                          省量不足自动转「从备份重建」——注销旧实例并从备份重导入（落位会换到新旁路目录）。失败如实中止并点名备份位置；完成后备份保留供你自行清理。
                        </UiBanner>
                        <div class="move-input-row">
                          <label class="move-label" for="wsl-compact-bak">备份目录</label>
                          <input id="wsl-compact-bak" v-model="compBackupDir" class="input mono"
                            placeholder="默认「下载\WSL 导出」；大盘备份可指到空闲卷（须绝对路径）" spellcheck="false" :disabled="rowBusy(d.name)" />
                          <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                            title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => compBackupDir = p, `选择 ${d.name} 瘦身备份的存放目录`)">📁 选目录</button>
                        </div>
                        <div class="move-input-row">
                          <button class="btn btn-danger-outline btn-small" :disabled="rowBusy(d.name)" @click="submitCompact(d)">🗜 确认开始瘦身</button>
                          <button class="btn btn-secondary btn-small" :disabled="busyWith(`compact:${d.name}`)" @click="closeCompact">取消</button>
                        </div>
                      </template>
                    </div>
                  </td>
                </tr>
                <tr v-if="confName === d.name" class="conf-row-editor">
                  <td colspan="5">
                    <div class="move-editor">
                      <div v-if="confLoading && !confDoc" class="hint-line">正在读取 /etc/wsl.conf…（发行版未运行时会先被启动，这是读配置的预期动作）</div>
                      <div v-else-if="confError && !confDoc" class="error-box">{{ confError }}
                        <button class="btn btn-secondary btn-small retry-inline" @click="loadConf(d.name)">↻ 重试</button>
                        <button class="btn btn-secondary btn-small" @click="closeConf">收起</button>
                      </div>
                      <template v-else-if="confDoc">
                        <UiBanner v-if="confDoc.missing" tone="info" class="slim">该发行版尚无 /etc/wsl.conf——保存即首建。</UiBanner>
                        <UiBanner v-for="(w, i) in confDoc.warnings ?? []" :key="i" tone="warn" class="slim">{{ w }}</UiBanner>
                        <textarea v-model="confText" class="input conf-textarea mono" rows="8" spellcheck="false"
                          :disabled="rowBusy(d.name)" :placeholder="`[boot]&#10;systemd=true`"></textarea>
                        <div class="move-input-row">
                          <button class="btn btn-primary btn-small" :disabled="rowBusy(d.name) || confLoading"
                            @click="saveConf(d)">{{ busyWith(`confsave:${d.name}`) ? '保存中…' : '✔ 保存写回' }}</button>
                          <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name) || confLoading"
                            @click="loadConf(d.name)">↻ 重读</button>
                          <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)" @click="closeConf">收起</button>
                          <span class="hint-dim">语法闸门 / 默认用户求证 / 写前备份（{{ '/etc/wsl.conf.hanxi.bak' }}）由后端把关；改完记得「⏹ 关机」或「🔄 重启」再进入</span>
                        </div>
                      </template>
                    </div>
                  </td>
                </tr>
                <tr v-if="forensicsName === d.name" class="forensics-row">
                  <td colspan="5">
                    <div class="forensics-panel">
                      <div v-if="!forensicsOf || (forensicsOf.loading && !forensicsOf.data)" class="hint-line">详情采集中：注册表巡查 + 磁盘双口径 + guest 只读探测…</div>
                      <div v-else-if="forensicsOf.error && !forensicsOf.data" class="error-box">{{ forensicsOf.error }}
                        <button class="btn btn-secondary btn-small retry-inline" @click="loadForensics(d.name)">↻ 重试</button>
                      </div>
                      <template v-else-if="forensicsOf?.data">
                        <dl class="fx-grid">
                          <div><dt>VHDX 逻辑大小</dt><dd class="mono">{{ forensicsOf.data.logicalBytes ? fmtSize(forensicsOf.data.logicalBytes) : '—' }}</dd></div>
                          <div><dt>磁盘实占</dt><dd class="mono">{{ forensicsOf.data.allocBytes ? fmtSize(forensicsOf.data.allocBytes) : '—' }}
                            <UiStatusChip v-if="forensicsOf.data.sparse" tone="information">稀疏盘</UiStatusChip></dd></div>
                          <div v-if="forensicsOf.data.dfOk"><dt>根盘用量（guest df）</dt><dd class="mono">
                            <span class="fx-bar"><UiProgressBar :percent="forensicsOf.data.dfUsePct" /></span>
                            {{ mb(forensicsOf.data.dfUsedMB) }} / {{ mb(forensicsOf.data.dfTotalMB) }} · 可用 {{ mb(forensicsOf.data.dfAvailMB) }} · {{ forensicsOf.data.dfUsePct }}%</dd></div>
                          <div><dt>发行版 IPv4</dt><dd class="mono">
                            <template v-if="forensicsOf.data.ipv4">{{ forensicsOf.data.ipv4 }}
                              <button class="link-button" @click="copy(forensicsOf.data.ipv4)">复制</button>
                            </template>
                            <template v-else>—</template></dd></div>
                          <div v-if="forensicsOf.data.pfn"><dt>包家族名 (PFN)</dt><dd class="mono">{{ forensicsOf.data.pfn }}</dd></div>
                          <div v-if="forensicsOf.data.basePath"><dt>安装路径</dt><dd class="mono">{{ forensicsOf.data.basePath }}</dd></div>
                          <div v-if="forensicsOf.data.vhdxPath"><dt>VHDX 路径</dt><dd class="mono">{{ forensicsOf.data.vhdxPath }}</dd></div>
                        </dl>
                        <ul v-if="forensicsOf.data.notes?.length" class="fx-notes">
                          <li v-for="(n, i) in forensicsOf.data.notes" :key="i">ⓘ {{ n }}</li>
                        </ul>
                        <div class="fx-foot">
                          <UiStatusChip :tone="forensicsOf.data.running ? 'positive' : 'neutral'">{{ forensicsOf.data.running ? '运行中' : '已停止' }}</UiStatusChip>
                          <UiStatusChip tone="neutral" title="来自宿主 ~/.wslconfig 的 [networking] networkingMode">网络模式：{{ modeWord(forensicsOf.data.networkMode) }}</UiStatusChip>
                          <button class="btn btn-secondary btn-small" :disabled="forensicsOf.loading"
                            @click="loadForensics(d.name)">{{ forensicsOf.loading ? '刷新中…' : '↻ 刷新' }}</button>
                        </div>
                      </template>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>

        <!-- 本会话导出工件登记：多份全列，逐份直达位置；导出/瘦身成功即自动展开 -->
        <details v-if="exportRecords.length" class="info-details export-log" :open="exportLogOpen">
          <summary class="info-summary">📦 本会话导出记录（{{ exportRecords.length }}）</summary>
          <ul class="export-log-list">
            <li v-for="r in exportRecords" :key="r.id">
              <code class="mono">{{ r.id }}</code>
              <span class="dim mono">{{ r.name }} · {{ fmtSize(r.size) }} · {{ r.at }}</span>
              <button class="link-button" @click="copy(r.path)">复制路径</button>
              <button class="link-button" @click="revealExport(r.id)">📂 打开位置</button>
            </li>
          </ul>
        </details>
      </template>
      <div v-else-if="report" class="empty-state">
        <p>本机尚未安装 WSL 本体——先到「🐧 就绪检测」页执行「🚀 一键开启」（「虚拟机平台」启用后需重启一次），装好发行版后实例会列在这里。</p>
      </div>
      <div v-else class="hint-line">就绪体检进行中，稍候自动列出本机发行版…</div>
    </div>

    <!-- 版本 Tab：WSL 本体官方发布（Releases × 本机关系 × MSI 应用内下载） -->
    <div v-show="activeMainTab === 'versions'" id="wsl-main-versions-panel" role="tabpanel" aria-labelledby="wsl-main-versions-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
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

    </div>

    <!-- ➕ 添加实例 Tab：三源统一新增入口（官方商店清单 / 本地 rootfs / 现有 VHDX 盘）。
         原「📦 官方发行版」页的清单表格已并入本商店源面板（同清单双入口归一）；
         镜像站下载源刻意不做（第三方 rootfs 信任链无法把关）。 -->
    <div v-show="activeMainTab === 'add'" id="wsl-main-add-panel" role="tabpanel" aria-labelledby="wsl-main-add-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <div class="install-panel">
        <div class="move-input-row">
          <label class="move-label">来源类型</label>
          <div class="btn-group">
            <button class="btn btn-secondary btn-small" :class="{ active: addSource === 'store' }" :disabled="busyAny" @click="addSource = 'store'">🛒 官方商店发行版</button>
            <button class="btn btn-secondary btn-small" :class="{ active: addSource === 'rootfs' }" :disabled="busyAny" @click="addSource = 'rootfs'">📄 本地 rootfs（tar）</button>
            <button class="btn btn-secondary btn-small" :class="{ active: addSource === 'vhdx' }" :disabled="busyAny" @click="addSource = 'vhdx'">💽 现有 VHDX 发行盘</button>
          </div>
        </div>
        <!-- 安装基目录三源共用；VHDX 就地挂载不动盘，故隐藏该行 -->
        <div v-if="addSource !== 'vhdx' || addVhdCopy" class="move-input-row">
          <label class="move-label" for="wsl-install-dir">安装基目录</label>
          <input id="wsl-install-dir" v-model="installDir" class="input mono"
            placeholder="例：D:\wsl" spellcheck="false" :disabled="globalBusy" />
          <button class="btn btn-secondary btn-small" :disabled="globalBusy"
            title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => installDir = p, '选择安装基目录')">📁 选目录</button>
        </div>
        <UiBanner v-if="!installDir.trim() && (addSource !== 'vhdx' || addVhdCopy)" tone="warn" class="slim">
          ⚠ 留空 = 用系统默认位置，新实例通常落在 C 盘
        </UiBanner>
        <div v-if="addSource !== 'vhdx' || addVhdCopy" class="hint-line">安装基目录按「基目录」使用：每个实例自动落在其下<b>同名子目录</b>（如 D:\wsl\Ubuntu；末级已是实例名则不重复追加）。须为本机绝对路径且所在盘存在；改动自动记住。</div>
      </div>

      <!-- 源①：官方商店清单（wsl --install 白名单 + 装完即迁落位，同一条提权链一次 UAC）。
           表格逐行「⬇ 安装」是唯一动线；空态自带「↻ 重新查询」（刷新钮不再只活在成功分支）。 -->
      <div v-if="addSource === 'store'" class="install-panel">
        <UiBanner v-if="distroBlockedReason" tone="warn" class="slim distro-block-banner">{{ distroBlockedReason }}</UiBanner>
        <div class="hint-line">安装落位：<b>{{ installDir.trim() || '系统默认（通常在 C 盘）' }}</b> 下的同名子目录——改基目录见上方「安装基目录」行，改动自动记住。</div>
        <div class="section-title distro-head">
          <h3>可安装的官方发行版 ({{ online.length }})</h3>
          <div class="btn-group distro-head-actions">
            <button class="btn btn-secondary btn-small" :disabled="onlineLoading" @click="loadOnline">
              {{ onlineLoading ? '查询中…' : '↻ 刷新清单' }}
            </button>
          </div>
        </div>
        <div v-if="onlineLoading" class="hint-line">正在向本机 wsl.exe 查询在线清单…</div>
        <div v-else-if="onlineError" class="error-box">{{ onlineError }}
          <button class="btn btn-secondary btn-small retry-inline" @click="loadOnline">↻ 重试</button>
        </div>
        <div v-else-if="!online.length" class="empty-state">
          <p>清单未加载——它由本机 wsl.exe 提供（需 WSL 本体已装好）。
            <button class="btn btn-secondary btn-small retry-inline" @click="loadOnline">↻ 重新查询</button></p>
        </div>
        <div v-else class="table-container">
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
                  <button class="btn btn-secondary btn-small" :disabled="busyAny || !!distroBlockedReason"
                    :title="distroBlockedReason || `wsl --install -d ${opt.id}（UAC 提权）`"
                    @click="installDistro(opt)">⬇ 安装</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="hint-line">下载体量较大多半要几分钟；首次进入发行版需创建 Linux 用户名与密码。</div>
      </div>

      <!-- 源②：本地 rootfs tar（wsl --import，免 UAC） -->
      <div v-else-if="addSource === 'rootfs'" class="install-panel">
        <UiBanner tone="info" class="slim">
          导入 = <code class="mono">wsl --import</code>：把本工具导出产物或可信 rootfs tar 解包落成新增实例（免 UAC）。
          落位子目录须为空或不存在；名称与本机名单防撞；大 tar 解包耗时数分钟，期间请勿退出。
        </UiBanner>
        <div class="move-input-row">
          <label class="move-label" for="wsl-add-name">实例名称</label>
          <input id="wsl-add-name" v-model="addName" class="input mono" placeholder="MyDistro" spellcheck="false" :disabled="busyAny" />
        </div>
        <div class="move-input-row">
          <label class="move-label" for="wsl-add-file">tar 文件路径</label>
          <input id="wsl-add-file" v-model="addFile" class="input mono" placeholder="导出工件或 rootfs tar 的完整路径（「本会话导出记录」处可复制）" spellcheck="false" :disabled="busyAny"
            @keyup.enter="canAddRootfs && submitImport()" />
          <button class="btn btn-secondary btn-small" :disabled="busyAny" title="调系统文件选择框挑选 tar（也可手动填写路径）" @click="pickAddFile('rootfs')">📁 浏览</button>
        </div>
        <div class="move-input-row">
          <button class="btn btn-primary btn-small" :disabled="!canAddRootfs || busyAny" @click="submitImport">
            {{ busyWith('import') ? '导入中…' : '✔ 创建（解包落位）' }}
          </button>
          <span v-if="addName.trim()" class="hint-dim">落位：{{ previewSubdir(installDir.trim(), addName.trim()) || '请先填写安装基目录' }}</span>
        </div>
      </div>

      <!-- 源③：现有 VHDX 发行盘（--import-in-place 零拷贝 / --import … --vhd 复制落位，免 UAC） -->
      <div v-else class="install-panel">
        <UiBanner tone="info" class="slim">
          挂载 = 把现成的 ext4 发行盘（「🗜 瘦身」备份盘、别机带来的 ext4.vhdx 等）落成新增实例（免 UAC；要求 WSL 2.7.3+）。
          <b>就地挂载零拷贝</b>——该文件从此就是实例的数据盘，移动/删除它即伤及实例；换位置请用挂载后的「🧭 迁移」。
        </UiBanner>
        <div class="move-input-row">
          <label class="move-label" for="wsl-add-name">实例名称</label>
          <input id="wsl-add-name" v-model="addName" class="input mono" placeholder="MyDistro" spellcheck="false" :disabled="busyAny" />
        </div>
        <div class="move-input-row">
          <label class="move-label" for="wsl-add-file">VHDX 盘路径</label>
          <input id="wsl-add-file" v-model="addFile" class="input mono" placeholder="ext4.vhdx 等发行盘的完整路径" spellcheck="false" :disabled="busyAny"
            @keyup.enter="canAddVhd && submitImportVhd()" />
          <button class="btn btn-secondary btn-small" :disabled="busyAny" title="调系统文件选择框挑选 VHDX（也可手动填写路径）" @click="pickAddFile('vhdx')">📁 浏览</button>
        </div>
        <div class="move-input-row">
          <label class="move-label">挂载方式</label>
          <label class="radio-label"><input v-model="addVhdCopy" type="radio" :value="false" :disabled="busyAny" /> 就地挂载（零拷贝，推荐）</label>
          <label class="radio-label"><input v-model="addVhdCopy" type="radio" :value="true" :disabled="busyAny" /> 复制盘到安装目录（原盘不动）</label>
        </div>
        <div class="move-input-row">
          <button class="btn btn-primary btn-small" :disabled="!canAddVhd || busyAny" @click="submitImportVhd">
            {{ busyWith('import') ? '挂载中…' : '✔ 创建实例' }}
          </button>
          <span v-if="addVhdCopy && addName.trim()" class="hint-dim">副本落位：{{ previewSubdir(installDir.trim(), addName.trim()) || '请先填写安装基目录' }}</span>
        </div>
      </div>
    </div>

    <!-- 端口转发 Tab：NAT 场景 netsh portproxy 规则清单 -->
    <div v-show="activeMainTab === 'proxy'" id="wsl-main-proxy-panel" role="tabpanel" aria-labelledby="wsl-main-proxy-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <UiBanner v-if="proxyView?.networkMode === 'mirrored'" tone="info" class="slim">
        本机为<b>镜像网络</b>：Windows 的 localhost 直通发行版服务，通常<b>无需</b>这里的转发规则——
        下方只读列出的 portproxy 属 NAT 时代遗产或其它程序建立，自行决定去留。要切回 NAT 可在「🌐 .wslconfig」中修改。
      </UiBanner>
      <UiBanner v-if="proxyView?.pending" tone="warn" class="slim">
        规则清单有改动尚未应用到系统——点「▶ 应用规则」同步（一次 UAC 批量执行）。
      </UiBanner>
      <div class="control-panel">
        <div class="meta-info">
          <span>WSL2 NAT 转发：本机 <b class="mono">监听:端口</b> → <b class="mono">发行版IP:guest端口</b>；WSL 重启后 guest IP 漂移，再点一次应用即重同步。</span>
          <span class="hint-dim">只管理本工具规则清单登记的监听口；系统里其它来源的转发列在「外部转发」，只展示不触碰</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-primary btn-small" :disabled="busyAny || !proxyView?.rules?.length"
            title="netsh portproxy/advfirewall 批量应用（UAC 提权，先删后加幂等）" @click="applyProxy">▶ 应用规则</button>
          <button class="btn btn-secondary btn-small" :disabled="proxyLoading" @click="loadProxy">{{ proxyLoading ? '读取中…' : '↻ 刷新' }}</button>
          <button class="btn btn-danger-outline btn-small" :disabled="busyAny || !proxyView?.rules?.length"
            title="摘除规则清单全部规则的系统转发与 Hanxi WSL 防火墙放行并清空清单" @click="cleanupProxy">🧹 清理托管</button>
        </div>
      </div>

      <div v-if="proxyError" class="error-box">{{ proxyError }}
        <button class="btn btn-secondary btn-small retry-inline" @click="loadProxy">↻ 重试</button>
        <button class="btn btn-secondary btn-small" title="规则清单文件损坏/误拦时的逃生口：只删文件，不碰系统现态" @click="resetProxyLedger">清空规则清单</button>
      </div>
      <div v-else-if="proxyLoading && !proxyView" class="hint-line">正在读取规则清单与 netsh 现态…</div>
      <template v-else-if="proxyView">
        <div class="import-panel">
          <div class="move-input-row">
            <label class="move-label" for="wsl-pp-distro">发行版</label>
            <select id="wsl-pp-distro" v-model="newRule.distro" class="input" :disabled="globalBusy">
              <option value="">（选择）</option>
              <option v-for="i in instances" :key="i.name" :value="i.name">{{ i.name }}</option>
            </select>
            <label class="move-label" for="wsl-pp-port">本机端口</label>
            <input id="wsl-pp-port" v-model="newRule.port" class="input mono pp-num" placeholder="8080" :disabled="globalBusy" />
            <label class="move-label" for="wsl-pp-guest">guest 端口</label>
            <input id="wsl-pp-guest" v-model="newRule.guest" class="input mono pp-num" placeholder="默认同左" :disabled="globalBusy" />
            <label class="move-label" for="wsl-pp-listen">监听地址</label>
            <input id="wsl-pp-listen" v-model="newRule.listen" class="input mono" style="max-width: 130px" :disabled="globalBusy" />
            <label class="radio-label"><input v-model="newRule.firewall" type="checkbox" :disabled="globalBusy" /> 防火墙放行</label>
            <button class="btn btn-primary btn-small" :disabled="!newRule.distro || !newRule.port.trim() || globalBusy"
              @click="addProxyRule">＋ 添加</button>
          </div>
          <div class="move-input-row">
            <label class="move-label" for="wsl-pp-note">备注</label>
            <input id="wsl-pp-note" v-model="newRule.note" class="input" placeholder="可选：这条转发给谁用" :disabled="globalBusy" />
          </div>
          <div v-if="newRule.listen.trim() === '0.0.0.0'" class="hint-line">
            ⚠ 监听 0.0.0.0 + 防火墙放行 = 局域网内<b>其它设备也能访问</b>该端口；只想本机访问请用 127.0.0.1。
          </div>
        </div>

        <div class="section-title"><h3>规则清单 ({{ proxyView.rules?.length ?? 0 }})</h3></div>
        <div v-if="!proxyView.rules?.length" class="empty-state"><p>还没有规则——用上面的表单添加第一条（如 8080 → Ubuntu:80）。</p></div>
        <div v-else class="table-container">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width: 170px;">状态</th>
                <th style="width: 150px;">发行版</th>
                <th style="width: 130px;">本机监听</th>
                <th>目标</th>
                <th style="width: 110px;">防火墙</th>
                <th>备注</th>
                <th style="width: 170px;">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in proxyView.rules" :key="r.id">
                <td>
                  <UiStatusChip v-if="!r.enabled" tone="neutral">已停用</UiStatusChip>
                  <UiStatusChip v-else-if="!r.distroRunning" tone="warning">发行版未运行</UiStatusChip>
                  <UiStatusChip v-else-if="ruleDrift(r)" tone="warning" :title="`netsh 现指 ${r.activeIP}，现为 ${r.targetIP}`">IP 漂移·需重应用</UiStatusChip>
                  <UiStatusChip v-else-if="r.applied" tone="positive">✓ 已生效</UiStatusChip>
                  <UiStatusChip v-else tone="information">待应用</UiStatusChip>
                </td>
                <td><code class="mono">{{ r.distro }}</code></td>
                <td class="mono">{{ r.listen }}:{{ r.port }}</td>
                <td class="mono">{{ r.targetIP || r.activeIP || '—' }}:{{ r.guest }}</td>
                <td>{{ r.firewall ? '✅ 防火墙放行' : '—' }}</td>
                <td class="dim">{{ r.note || '—' }}</td>
                <td>
                  <div class="distro-actions">
                    <button class="btn btn-secondary btn-small" :disabled="globalBusy"
                      :title="r.enabled ? '停用后点「应用规则」将从系统摘除该转发' : '恢复启用'"
                      @click="updateRule(r, { enabled: !r.enabled })">{{ r.enabled ? '⏸ 停用' : '▶ 启用' }}</button>
                    <button class="btn btn-secondary btn-small" :disabled="globalBusy" title="切换是否同步防火墙入站放行"
                      @click="updateRule(r, { firewall: !r.firewall })">{{ r.firewall ? '关放行' : '开放行' }}</button>
                    <button class="btn btn-danger-outline btn-small" :disabled="globalBusy" @click="removeRule(r)">🗑 删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <template v-if="proxyView.foreign?.length">
          <div class="section-title"><h3>外部转发 ({{ proxyView.foreign.length }})</h3></div>
          <div class="table-container">
            <table class="tbl">
              <thead>
                <tr>
                  <th style="width: 130px;">本机监听</th>
                  <th>指向</th>
                  <th style="width: 320px;">说明</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(f, i) in proxyView.foreign" :key="i">
                  <td class="mono">{{ f.listenAddr }}:{{ f.listenPort }}</td>
                  <td class="mono">{{ f.connectAddr }}:{{ f.connectPort }}</td>
                  <td class="dim">非 Hanxi 登记——由其它程序建立，本工具只展示不改动</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
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
/* 信息行的 ℹ：表"已检测、不构成判定"——记号与绿色通过章形状/色彩双重区分 */
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

/* 发行版管理控制台 */
.distro-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.distro-head-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.distro-actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }

/* 行级「⋯ 更多」下拉：自研 relative 容器 + v-if 面板（Esc/外点关闭，见 script） */
.row-menu { position: relative; display: inline-flex; }
.row-menu-panel {
  position: absolute; right: 0; top: calc(100% + 4px); z-index: 30; min-width: 150px;
  display: flex; flex-direction: column; gap: 2px; padding: 4px;
  background: var(--surface-panel); border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control); box-shadow: var(--shadow-panel);
}
.row-menu-item {
  display: flex; align-items: center; gap: 6px; text-align: left; width: 100%;
  background: none; border: none; border-radius: 6px; padding: 6px 10px;
  font-size: 12.5px; color: var(--color-text); cursor: pointer; white-space: nowrap;
}
.row-menu-item:hover:not(:disabled) { background: var(--surface-hover); }
.row-menu-item:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: -2px; }
.row-menu-item:disabled { opacity: 0.5; cursor: not-allowed; }
.row-menu-item.active { color: var(--color-primary); }

/* 忙时不换字防宽度跳动：按钮尾部内联 spinner（仅真在飞才转，符合动效纪律） */
.btn-spin {
  width: 9px; height: 9px; margin-left: 6px; display: inline-block; vertical-align: 0;
  border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%;
  animation: hx-spin 750ms linear infinite;
}
@keyframes hx-spin { to { transform: rotate(360deg); } }
/* 常驻进度条高度锚定：换文案不抖页 */
.busy-banner { min-height: 34px; }
.radio-label { display: inline-flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--color-text); cursor: pointer; }
.radio-label input[type='radio'] { accent-color: var(--color-primary); margin: 0; }
.export-row-editor td { background: var(--surface-page); }
.clone-row-editor td { background: var(--surface-page); }
.btn.active { border-color: var(--color-primary); color: var(--color-primary); }

/* 导入面板 / 安装落位目录面板 */
.import-panel,
.install-panel {
  display: flex; flex-direction: column; gap: 8px; padding: 10px 12px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}

/* 端口转发 Tab */
.pp-num { width: 96px; flex: 0 0 auto; }

/* wsl.conf 编辑器与瘦身 */
.compact-row-editor td { background: var(--surface-page); }
.conf-row-editor td { background: var(--surface-page); }
.conf-textarea { resize: vertical; min-height: 120px; line-height: 1.6; font-size: 12px; }

/* 取证抽屉 */
.forensics-row td { background: var(--surface-page); }
.forensics-panel { display: flex; flex-direction: column; gap: 6px; padding: 4px 0; }
.fx-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 8px 18px; margin: 0; }
.fx-grid dt { font-size: 11px; color: var(--color-text-subtle); }
.fx-grid dd { margin: 0; font-size: 12.5px; word-break: break-all; }
.fx-bar { width: 90px; display: inline-flex; vertical-align: middle; margin-right: 6px; }
.fx-notes { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; font-size: 12px; color: var(--color-text-muted); }
.fx-foot { display: flex; align-items: center; gap: 10px; }
.export-log { margin-top: 2px; }
.export-log-list { list-style: none; margin: 0; padding: 4px 0; display: flex; flex-direction: column; }
.export-log-list li { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 4px 12px; font-size: 12px; border-bottom: 1px solid var(--color-border); }
.export-log-list li:last-child { border-bottom: none; }
.tbl td .mono.dim { font-size: 12px; }
.move-row-editor td { background: var(--surface-page); }
.move-editor { display: flex; flex-direction: column; gap: 8px; padding: 2px 0; }
.move-input-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.move-label { font-size: 12px; font-weight: 600; color: var(--color-text-muted); white-space: nowrap; }
.input {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 6px 10px; font-size: 12.5px; color: var(--color-text); font-family: inherit; flex: 1 1 240px; min-width: 0;
}
.input:focus { border-color: var(--color-primary); }
/* 焦点环不再被 outline:none 掐灭：键盘聚焦（:focus-visible）恢复清晰焦点环 */
.input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }
.input:disabled { opacity: 0.6; }

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
