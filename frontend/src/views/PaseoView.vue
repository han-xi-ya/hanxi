<script setup lang="ts">
// Paseo 控制台（Wave 5 · 批 0 契约收敛件；共享契约增强批收编版；波 2D 迁壳件）：
// 状态/版本加载/进度 map/uptime/busy 闩/启停与设版编排/联动辅助卡全部收进
// adapters/paseo + components/managed 家族（store 一份，子件共享注入）。
// 增强批收编后视图方言表退役：多版本卡（隐式「使用版本」徽标 + 自动最新回退
// 高亮 + 未验证哈希徽标）与精确互认远程表改由共享 ManagedVersionPanel 渲染
// （词面经 copy 覆写、隐式使用经 versions.implicitActive、徽标经
// #version-row-extra 槽免 cast）；通道行换用共享 ManagedChannelRow；
// 引用将启版本的引导行迁回 adapter.hint 投影上下文。
// 视图仅余：升级警告条 + 常驻自动更新提示条（谓词/文案在 adapter 或本文件常量）、
// 「什么是 Paseo」说明卡与 daemon 数据钮（#extras-action 槽）。
// 波 2D 迁壳：页头+页签+error-box+tab-body 手抄壳骨退役——升级判定要 setup 期
// store 句柄，走「视图自建 useManagedConsole → 传壳 :store」注入通道；版本 Tab
// 因面板 #version-row-extra 槽位需求走壳的 #versions-body 整体替换位（契约内
// 逃生门：默认体的 channel 块 + 面板照抄重挂，仅多带方言徽标槽）。
// 与 CCSwitchView 的结构差异：单设定版本可空（空=自动最新已装）、stable/beta
// 双通道切换、双数据目录入口（Electron 走 extras.dataDir，daemon 走 #extras-action 槽）。
import { computed } from 'vue'
import type { ManagedVersionRecord } from '../components/managed/adapter'
import { createPaseoAdapter, openPaseoDaemonHome, paseoUpgradeAvailable, type PaseoVersionDialect } from '../adapters/paseo'
import { useManagedConsole } from '../components/managed/store'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import ManagedChannelRow from '../components/managed/ManagedChannelRow.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import UiBanner from '../components/ui/UiBanner.vue'

const adapter = createPaseoAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

// ---------- 派生状态（升级警告条） ----------
// 最新已装规范版本：与 adapter.implicitActive 同判据（后端已新→旧排序）
const latestInstalled = computed(() => store.installed.find((v) => /^\d+\.\d+\.\d+/.test(v.version)) ?? null)

const upgradeAvailable = computed(() => {
  if (!latestInstalled.value || store.releases.length === 0) return false
  return paseoUpgradeAvailable(latestInstalled.value.version, store.releases[0].version)
})

// 常驻风险提示：应用内"安装更新"会装出托管外的平行副本（上游无禁用开关）
const updaterNote = '上游无自动更新禁用开关：在 Paseo 界面内点「安装更新」会把新版装进 %LOCALAPPDATA%（托管目录外的平行副本），版本升级请统一走这里。'

// daemon 数据目录钮（#extras-action 槽位，见 adapters/paseo 差异④）
async function openDaemonHome() {
  try {
    await openPaseoDaemonHome()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

// 「未验证哈希」徽标谓词（③收编）：面板 #version-row-extra 槽作用域的 record
// 随泛型 V 携带 verifiedHash——本函数形参即方言型，全链路零 cast。
// 导入安装本身无官方 digest，不打此徽标（与批 0 视图现词逐字一致）。
function unverified(v: ManagedVersionRecord<PaseoVersionDialect>): boolean {
  return !v.verifiedHash && !v.isImport
}
</script>

<template>
  <ManagedConsoleShell
    class="paseo-view"
    :adapter="adapter"
    :store="store"
    title="Paseo"
    subtitle="托管开源 coding agent 编排器 Paseo：版本管理、启停与窗口唤起，agent 会话在其窗口与手机端继续运行。"
    console-tab-label="🐾 控制台"
  >
    <!-- 控制台 Tab 主体（壳默认具位）：升级警告条 + 常驻自动更新提示条 + 说明卡 -->
    <UiBanner v-if="upgradeAvailable && latestInstalled" tone="warn" class="slim">
      发现可升级版本 {{ store.releases[0].version }}（当前最新已装 {{ latestInstalled.version }}）——到「版本管理」一键安装。
    </UiBanner>

    <UiBanner tone="info" class="slim">{{ updaterNote }}</UiBanner>

    <details class="info-details">
      <summary class="info-summary">什么是 Paseo</summary>
      <div class="info-body">
        <p>开源 coding agent 编排器（<a class="inline-link" href="https://github.com/getpaseo/paseo" target="_blank" rel="noopener">getpaseo/paseo</a>，Apache-2.0）：本机跑 daemon，桌面/手机/Web 统一调度 Claude Code、Codex 等 agent CLI。Hanxi 仅做官方便携 zip 的下载托管与启停管理，不内嵌不打包其代码。</p>
        <p class="hint-dim">共享数据模式（与 cc-switch 同构）：托管实例与自装实例同数据同锁组，全局至多一个桌面主实例；「外部」状态即你的自装实例在场。唤窗优先直接唤起已有窗口（上游二次拉起语义是"再开新窗"而非聚焦）。无窗运行 ≠ 空闲——其 agent 会话可能正在进行，本模块不设空闲自动退出。</p>
        <p class="hint-dim">开启「随 Hanxi 一起关闭」后，Hanxi 退出会连带终止 Paseo 及其 daemon 上正在运行的全部 agent 会话，请谨慎。</p>
      </div>
    </details>

    <!-- 联动与辅助设置卡经壳默认 ManagedExtrasCard；daemon 数据为第二位数据
         目录，经壳的 #extras-action 槽转发注入 -->
    <template #extras-action>
      <button class="btn btn-secondary btn-small" title="打开 daemon 数据主目录（~/.paseo：持久配置、会话与手机配对）" @click="openDaemonHome">🐾 daemon 数据</button>
    </template>

    <!-- 版本 Tab 整体替换位：默认体的 channel 共享块 + 共享面板照抄重挂，
         仅多带「未验证哈希」方言徽标的 #version-row-extra 槽 -->
    <template #versions-body>
      <ManagedChannelRow :adapter="adapter" :store="store" />
      <ManagedVersionPanel :adapter="adapter" :store="store">
        <template #version-row-extra="{ record }">
          <span v-if="unverified(record)" class="badge badge-import">未验证哈希</span>
        </template>
      </ManagedVersionPanel>
    </template>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/提示条/页签与 flex 骨架由托管控制台壳 + components.css 全局原子
   接管（波 2D 迁壳后视图手抄壳骨与 .paseo-view flex 副本退役；
   hint-line/info-details/info-summary/info-body p 与内联链接由全局原子接管；
   control-panel/meta-info/btn-group、channel-row 家族、section-title、
   徽标档位色（含 badge-import——面板 scoped 提供）、inst-* 与表格 dl-* 家族
   全部由共享面板/块承载）。 */
</style>
