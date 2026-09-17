<script setup lang="ts">
// WSL 就绪体检面板（「🐧 就绪检测」页签主体，Phase 6 式自 WSLView 拆分）。
// 流式骨架清单、结论条、白名单提权操作条、重启引导与 .wslconfig 宿主配置编辑器归本面板；
// 体检事件流（wsl:readiness）、busy 分级登记与提权命令编排（runOp 白名单）留视图——
// 跨页签进度可见性（busyBanner / 页签 label）依赖视图状态，本层只做呈现与意图转发。
// 行为逐字迁出：调用序列、确认文案与事件语义未做任何改动。
import { computed, ref } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { CheckItem, Report } from '../../../bindings/hanxi/internal/modules/wsl/readiness/models'
import type { HostConfDoc } from '../../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { getErrorMessage } from '../../utils/errors'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'

const props = defineProps<{
  report: Report | null
  arrived: Record<string, CheckItem>
  streaming: boolean
  loadError: string
  busyAny: boolean
  globalBusy: boolean
  busyWith: (op: string) => boolean
  startOp: (op: string) => void
  finishOp: (op: string) => void
  startCheck: () => Promise<void>
  loadInstances: () => Promise<void>
  // 停止全部后复采端口转发现态（视图转调 proxy 页签，保持拆分前 doShutdown 收尾序列）
  reloadProxy: () => Promise<void>
  modeWord: (m?: string | null) => string
  // 白名单提权操作（runOp 编排留视图，本层仅接线）
  installWsl: () => Promise<void>
  updateWsl: () => Promise<void>
  updateWeb: () => Promise<void>
  setDefaultV2: () => Promise<void>
  enableFeatures: () => Promise<void>
  disableFeatures: () => Promise<void>
  uninstallWsl: () => Promise<void>
  openPowerSettings: () => Promise<void>
}>()

const { showToast } = useToast()
const { confirm } = useConfirm()

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

const verdictTone = computed<'ok' | 'warn' | 'error' | 'info'>(() => {
  switch (props.report?.verdict) {
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

// 重启引导纯检测驱动：只认系统 CBS 待重启台账（report.rebootPending），
// 与"刚做过什么操作"无关——真欠重启才提示，重启完自动消失。
const rebootNudge = computed(() => !!props.report?.rebootPending)

// ---------- .wslconfig（宿主全局配置：网络模式等） ----------
const hostConfOpen = ref(false)
const hostDoc = ref<HostConfDoc | null>(null)
const hostText = ref('')
const hostLoading = ref(false)
const hostError = ref('')

async function toggleHostConf() {
  if (hostConfOpen.value) {
    if (props.globalBusy || hostLoading.value) return
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
  if (props.busyAny) return
  const accepted = await confirm({
    title: '写回 .wslconfig（宿主全局配置）？',
    tone: 'warning',
    description: '该文件影响【所有发行版】（网络模式、资源上限等），保存后需 wsl --shutdown 停止全部才生效。'
      + '\n\n写回防线：INI 语法闸门 + networkingMode 白名单（nat/bridged/mirrored）+ 原文件备份到 .wslconfig.hanxi.bak + 原子写后读回复核。',
  })
  if (!accepted) return
  props.startOp('wslconfig')
  let saved = false
  try {
    const out = await WSLAPI.SaveWslHostConf(hostText.value)
    showToast(out?.message || '已写回')
    await loadHostConf()
    saved = !!out?.success
  } catch (e) {
    showToast(`保存失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    props.finishOp('wslconfig')
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
  if (props.busyAny) return
  if (!alreadyConfirmed) {
    const accepted = await confirm({
      title: '停止全部 WSL（wsl --shutdown）？',
      tone: 'warning',
      description: '终止全部发行版会话：数据无损，下次进入自动重启。这是 .wslconfig 全局改动与网络模式切换的生效前提。',
    })
    if (!accepted) return
  }
  props.startOp('shutdown')
  try {
    const out = await WSLAPI.ShutdownWsl()
    showToast(out?.message || '已全部停止')
  } catch (e) {
    showToast(`停止全部失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    props.finishOp('shutdown')
    await props.loadInstances()
    await props.reloadProxy()
  }
}
</script>

<template>
  <!-- 错误框（多行诊断文案：white-space 补差，底色/边框落全局 .error-box） -->
  <div v-if="loadError" class="error-box">{{ loadError }}</div>

  <!-- 总体结论条（done 前显示进度语义） -->
  <UiBanner v-if="report" :tone="verdictTone">
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
</template>

<style scoped>
/* 全局原子（.btn 家族/.chip/.banner/.error-box/.mono/.hint-dim/.link-button/.empty-state）
   落 components.css；以下仅本面板独有或有意补差（注释注明）。 */

/* 多行诊断文案（换行符保留）——对全局 .error-box 的补差，非同名副本 */
.error-box { white-space: pre-line; }
/* 全局 .banner 内距已是 10px 14px，原 .verdict-banner 重复声明已删；结论条恢复默认内距 */
.verdict-line { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.verdict-detail { font-size: var(--text-sm); color: var(--color-text-muted); margin-top: 2px; }
/* 基形（字号/颜色/左内距）落回全局 .hint-line；此处仅留 WSL 档补差两行 */
.hint-line { line-height: 1.7; white-space: pre-line; }
/* 全局 .hint-dim 只定义颜色，WSL 注记统一配 --text-sm 小字——补差，非副本 */
.hint-dim { font-size: var(--text-sm); }
/* .banner.slim 等值副本已删净：UiBanner 根元素挂 .banner + .slim，落回全局 :where(.banner.slim) */
/* .dim 非全局原子名，本控制台独有 */
.dim { color: var(--color-text-muted); font-size: var(--text-sm); }

/* 操作条：.control-bar 与 .control-top 等值副本删净落回全局
   （全局形多出 flex 纵向列与 gap 8，单子节点下渲染逐点等值——目视项） */
.control-status { min-width: 0; } /* 收缩补差；display/gap/换行落回全局 .control-status */
.control-btns { flex-wrap: wrap; justify-content: flex-end; } /* 补差；display/gap 落回全局 .control-btns */

/* 体检清单 */
.check-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
.check-row { padding: 8px 10px; border-bottom: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 2px; }
.check-row:last-child { border-bottom: none; }
.check-row.pending .check-label { color: var(--color-text-subtle); }
.check-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.check-label { font-weight: 600; font-size: var(--text-base); }
.check-value { font-size: var(--text-xs); background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-pill); padding: 0 7px; color: var(--color-text-muted); }
.check-detail { font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.6; }
.check-trail { margin-left: auto; font-size: var(--text-md); font-weight: 800; line-height: 1; }
.check-trail.ok { color: var(--state-positive); }
/* 信息行的 ℹ：表"已检测、不构成判定"——记号与绿色通过章形状/色彩双重区分 */
.check-trail.info { color: var(--color-text-subtle); font-weight: 600; }
.check-trail.warn { color: var(--state-warning); }
.check-trail.bad { color: var(--state-danger); }
.reboot-link { margin-left: 10px; }
/* 检测中脉冲点：真实活状态才允许持续动画（hx-pulse 全局 keyframes） */
.pending-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; display: inline-block; animation: hx-pulse 1.8s ease-in-out infinite; }

/* 忙时不换字防宽度跳动：按钮尾部内联 spinner（仅真在飞才转，符合动效纪律） */
.btn-spin {
  width: 9px; height: 9px; margin-left: 6px; display: inline-block; vertical-align: 0;
  border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%;
  animation: hx-spin 750ms linear infinite;
}
@keyframes hx-spin { to { transform: rotate(360deg); } }

/* 表单行 / 输入 / 面板壳：与其余 WSL 页签同形（§③ 上收候选），本面板先自持 */
.import-panel {
  display: flex; flex-direction: column; gap: 8px; padding: 10px 12px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
.move-input-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.input {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 6px 10px; font-size: var(--text-sm); color: var(--color-text); font-family: inherit; flex: 1 1 240px; min-width: 0;
}
.input:focus { border-color: var(--color-primary); }
/* 焦点环不再被 outline:none 掐灭：键盘聚焦（:focus-visible）恢复清晰焦点环 */
.input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }
.input:disabled { opacity: 0.6; }
.conf-textarea { resize: vertical; min-height: 120px; line-height: 1.6; font-size: var(--text-sm); }
.retry-inline { margin-left: 10px; }
/* .btn.active 为"开关钮选中态"补差（全局 .btn 家族无此态）——非副本 */
.btn.active { border-color: var(--color-primary); color: var(--color-primary); }

/* 知识卡：info-details/info-summary 全家族（含 ::after/marker/[open] 两条）与 .info-body p
   均与全局原子逐字等值，副本删净落回；.info-body 同规则散差（gap 4 vs 全局 6）按裁决定档
   标准形一并落回（段间距 +2px，登记目视项）。 */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
