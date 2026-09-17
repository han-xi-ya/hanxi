<script setup lang="ts">
// WSL 子系统视图编排层（原 2209 行巨型孤本，Phase 6 式按五页签拆分后的骨架）。
// 界面标记与 scoped 样式已随 DOM 逐字迁出至 components/wsl/*：
//   WslReadinessPanel（🐧 就绪体检：流式骨架逐项点亮 + .wslconfig 编辑器 + 停止全部）
//   WslDistroTable（💻 本机发行版控制台：行内操作/「⋯ 更多」/迁移/导出/克隆/瘦身/wsl.conf/详情/导出记录）
//   WslAddInstancePanel（➕ 添加实例：商店/rootfs/VHDX 三源统一入口）
//   WslVersionsPanel（🧩 本体版本：官方 Releases × 本机关系 × MSI 应用内下载）
//   WslPortProxyPanel（🔀 端口转发：NAT netsh portproxy 规则清单）
//   WslUsbPanel（🔌 USB 直通：usbipd-win 设备表 + bind/attach/detach + 自动共享账本重放，
//     唯一随页签 v-if 挂载的面板——5s 轮询的生命周期即开关，冷页零探测）
// 本体只留跨页签共享状态与编排：wsl:readiness 事件流与体检报告（多页签消费）、
// busy 分级在飞登记（activeOps → 常驻进度横幅 + 页签 label「·运行中」互锁）、
// 白名单提权操作 runOp 链、发行版列表复采通道、安装基目录后端持久化、
// 页签懒加载 watch（经子组件 defineExpose 回拨）与克隆/瘦身为跨页签可见的进度态（v-model 上抛）。
// 行为逐字不变：bindings 调用、事件订阅、confirm/toast 调用序列一律未动。
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import * as WSLAPI from '../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { CheckItem, Report } from '../../bindings/hanxi/internal/modules/wsl/readiness/models'
import type { CloneProgress, CompactProgress, DistroInstance, ReadinessUpdate } from '../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../composables/useToast'
import { useConfirm } from '../composables/useConfirm'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import WslReadinessPanel from '../components/wsl/WslReadinessPanel.vue'
import WslDistroTable from '../components/wsl/WslDistroTable.vue'
import WslAddInstancePanel from '../components/wsl/WslAddInstancePanel.vue'
import WslVersionsPanel from '../components/wsl/WslVersionsPanel.vue'
import WslPortProxyPanel from '../components/wsl/WslPortProxyPanel.vue'
import WslUsbPanel from '../components/wsl/WslUsbPanel.vue'

const { showToast } = useToast()
const { confirm } = useConfirm()

const activeMainTab = ref<'console' | 'distros' | 'add' | 'versions' | 'proxy' | 'usb'>('console')
// 页签 6→5：原「📦 官方发行版」整页与「添加实例·商店源」是同清单双入口，已并入商店源面板。
// F9 起为六页：追加「🔌 USB 直通」（usbipd-win 集成）。
// mainTabs 为 computed：distros 页签在克隆/瘦身重任务在飞时 label 追加「·运行中」——跨页签进度可见。
const mainTabs = computed<Array<{ key: string; label: string }>>(() => {
  const heavy = cloneBusy.value || (!!compProg.value && !compTerm.value)
  return [
    { key: 'console', label: '🐧 就绪检测' },
    { key: 'distros', label: heavy ? '💻 本机发行版 ·运行中' : '💻 本机发行版' },
    { key: 'add', label: '➕ 添加实例' },
    { key: 'versions', label: '🧩 本体版本' },
    { key: 'proxy', label: '🔀 端口转发' },
    { key: 'usb', label: '🔌 USB 直通' },
  ]
})

// ---------- 就绪体检（流式）：报告跨页签消费（发行版呈现判据/安装拦截预告/架构过滤） ----------
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
// 子面板经函数 props 消费同一份闸门（busyWith/rowBusy/globalBusy…），单一来源不复制。
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
// 克隆/瘦身阶段白话文案：发行版控制台行内呈现与本横幅文案共用一份。
const CLONE_STAGE_TEXT: Record<string, string> = {
  copying: '拷贝数据盘中', importing: '挂载新实例中', done: '克隆完成', error: '克隆失败',
}
const COMPACT_STAGE_TEXT: Record<string, string> = {
  backup: '全量备份中', trim: 'fstrim/停机收敛中', optimize: '压缩数据盘中',
  reimport: '从备份重建中', done: '瘦身完成', error: '瘦身中止',
}
// 克隆/瘦身进度住在视图（跨页签进度可见性互锁：busy 文案与页签 label），
// 表单推进由发行版控制台经 v-model 回写。
const cloneProg = ref<CloneProgress | null>(null)
const compProg = ref<CompactProgress | null>(null)
const cloneBusy = computed(() => !!cloneProg.value && ['copying', 'importing'].includes(cloneProg.value.stage))
const compTerm = computed(() => !!compProg.value && ['done', 'error'].includes(compProg.value.stage))

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

// ---------- 白名单提权操作（全部为全局互斥类；就绪页操作条与版本页/端口页共用本编排） ----------
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
    + '只走官方卸载器、不强删注册表；完成后如实报告 Lxss 发行版注册残留。'
    + '可选功能（虚拟机平台）不在本操作内——如需彻底还原系统，再执行「关闭虚拟机平台」。',
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

// ---------- 发行版实例列表（独立复采通道，跨页签消费：控制台表格 + 端口页下拉） ----------
const instances = ref<DistroInstance[]>([])
const instLoading = ref(false)
const instError = ref('')

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

// ---------- 跨页签共享的落位与目录工具（子面板经函数 props 消费） ----------
// 落位子目录预览：与后端 underDir 同规则（末级已是实例名则原样）。
function previewSubdir(base: string, id: string): string {
  const clean = base.trim().replace(/[\\/]+$/, '')
  const seg = (clean.split(/[\\/]/).pop() || '').toLowerCase()
  if (!clean) return ''
  return seg === id.toLowerCase() ? clean : `${clean}\\${id}`
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

// .wslconfig 网络模式白话词表（就绪页配置面板与发行版详情抽屉共用）。
const MODE_WORD: Record<string, string> = { nat: 'NAT（默认）', bridged: '桥接', mirrored: '镜像网络', unknown: '未知/非法值' }
const modeWord = (m?: string | null) => MODE_WORD[m || 'nat'] || 'NAT（默认）'

// 安装落位基目录（「➕ 添加实例」页全局行，三源共用；控制台迁移/克隆预填同源）：默认 D:\wsl——
// 每个实例落在其下同名子目录（后端 underDir 把关）；留空=系统默认（通常 C 盘，UI 有警示条）。
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

// ---------- 页签懒加载与跨页签复采接线（子组件 defineExpose 回拨） ----------
const addPanelRef = ref<InstanceType<typeof WslAddInstancePanel> | null>(null)
const proxyPanelRef = ref<InstanceType<typeof WslPortProxyPanel> | null>(null)

// 首次切到对应页才拉数据（在线清单/netsh 现态均依赖本机命令，冷页避免无谓探测）。
// 官方清单懒加载并入「➕ 添加实例」商店源（原「📦 官方发行版」页签已删）。
watch(activeMainTab, (tab) => {
  if (tab === 'add') addPanelRef.value?.ensureOnlineLoaded()
  if (tab === 'proxy') proxyPanelRef.value?.ensureLoaded()
})

// 「🌑 停止全部」收尾复采端口转发现态（就绪页 doShutdown 经 props 回拨，序列同拆分前）。
const reloadProxy = async () => { await proxyPanelRef.value?.loadProxy() }

onMounted(() => {
  startCheck()
  loadInstances() // 与体检并行的独立通道：未装 WSL 时后端返回空列表，不报错
  loadInstallDir() // 安装基目录持久化以后端为准（W1，替代旧 localStorage）
})

onBeforeUnmount(() => {
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
      <WslReadinessPanel
        :report="report" :arrived="arrived" :streaming="streaming" :load-error="loadError"
        :busy-any="busyAny" :global-busy="globalBusy" :busy-with="busyWith"
        :start-op="startOp" :finish-op="finishOp" :start-check="startCheck"
        :load-instances="loadInstances" :reload-proxy="reloadProxy" :mode-word="modeWord"
        :install-wsl="installWsl" :update-wsl="updateWsl" :update-web="updateWeb"
        :set-default-v2="setDefaultV2" :enable-features="enableFeatures"
        :disable-features="disableFeatures" :uninstall-wsl="uninstallWsl"
        :open-power-settings="openPowerSettings"
      />
    </div>

    <!-- 本机发行版 Tab：实例管理控制台（状态归一列表 + 行内操作，复采通道独立于体检报告） -->
    <div v-show="activeMainTab === 'distros'" id="wsl-main-distros-panel" role="tabpanel" aria-labelledby="wsl-main-distros-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <WslDistroTable
        v-model:clone-prog="cloneProg" v-model:comp-prog="compProg"
        :report="report" :ready="distroTabReady"
        :instances="instances" :inst-loading="instLoading" :inst-error="instError"
        :load-instances="loadInstances"
        :busy-any="busyAny" :global-busy="globalBusy" :busy-with="busyWith"
        :busy-distro="busyDistro" :row-busy="rowBusy" :start-op="startOp" :finish-op="finishOp"
        :clone-busy="cloneBusy" :comp-term="compTerm"
        :clone-stage-text="CLONE_STAGE_TEXT" :compact-stage-text="COMPACT_STAGE_TEXT"
        :install-dir="installDir" :preview-subdir="previewSubdir"
        :pick-folder-into="pickFolderInto" :mode-word="modeWord"
      />
    </div>

    <!-- ➕ 添加实例 Tab：三源统一新增入口（官方商店清单 / 本地 rootfs / 现有 VHDX 盘）。
         原「📦 官方发行版」页的清单表格已并入本商店源面板（同清单双入口归一）；
         镜像站下载源刻意不做（第三方 rootfs 信任链无法把关）。 -->
    <div v-show="activeMainTab === 'add'" id="wsl-main-add-panel" role="tabpanel" aria-labelledby="wsl-main-add-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <WslAddInstancePanel
        ref="addPanelRef" v-model:install-dir="installDir"
        :report="report" :busy-any="busyAny" :global-busy="globalBusy" :busy-with="busyWith"
        :start-op="startOp" :finish-op="finishOp" :run-op="runOp" :load-instances="loadInstances"
        :preview-subdir="previewSubdir" :pick-folder-into="pickFolderInto"
      />
    </div>

    <!-- 版本 Tab：WSL 本体官方发布（Releases × 本机关系 × MSI 应用内下载） -->
    <div v-show="activeMainTab === 'versions'" id="wsl-main-versions-panel" role="tabpanel" aria-labelledby="wsl-main-versions-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <WslVersionsPanel :report="report" :busy-any="busyAny" :update-wsl="updateWsl" />
    </div>

    <!-- 端口转发 Tab：NAT 场景 netsh portproxy 规则清单 -->
    <div v-show="activeMainTab === 'proxy'" id="wsl-main-proxy-panel" role="tabpanel" aria-labelledby="wsl-main-proxy-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <WslPortProxyPanel
        ref="proxyPanelRef"
        :instances="instances" :busy-any="busyAny" :global-busy="globalBusy" :run-op="runOp"
      />
    </div>

    <!-- USB 直通 Tab：usbipd-win 设备表与自动共享账本（F9）。
         刻意 v-if 而非 v-show：面板内 usePolling 的 mounted/unmounted 即轮询开关，
         离开页签 5s 探测链归零；切回即重拉最新现态，无需 ensureLoaded 回拨。 -->
    <div v-if="activeMainTab === 'usb'" id="wsl-main-usb-panel" role="tabpanel" aria-labelledby="wsl-main-usb-tab" class="tab-body">
      <UiBanner v-if="busyBanner" tone="info" class="slim busy-banner">{{ busyBanner }}</UiBanner>
      <WslUsbPanel :instances="instances" />
    </div>
  </section>
</template>

<style scoped>
.wsl-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }
/* 常驻进度条高度锚定：换文案不抖页 */
.busy-banner { min-height: 34px; }
/* .slim 等值副本已删净：UiBanner 根元素挂 .banner + .slim，落回全局 :where(.banner.slim) */
</style>
