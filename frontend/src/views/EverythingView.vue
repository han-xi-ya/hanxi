<script setup lang="ts">
// 编排层：托管状态 / 版本数据与操作 / 事件订阅与轮询生命周期（骨架基于重构共享层）。
// 内嵌搜索与 ES 组件就绪收编 useEverythingSearch、结果表列宽记忆收编 useEverythingColumns，
// 控制台整合条 / 结果区拆至 components/everything/* 子组件；
// DOM 结构、文案与 bindings 调用契约逐字保持，业务动作仍由本视图编排接线。
//
// 波 2F 供体回迁：版本管理 Tab（概览 + 已装卡 + 远程表）换装 components/managed/
// ManagedVersionPanel——本视图正是共享面板当年「以 components/everything/
// {VersionCard,ReleaseTable} 的拆分语义为供体」的原主，方言件退役回共享件：
//   - 通道徽标 + 快照降级标记 → #release-extra-col 方言列槽（波 2A 通道列承接位）；
//   - 随关联动开关 → #meta-extra 槽（波 2E meta 行族尾部承接位）；
//   - meta 概览/双空态/导入刷新钮 → copy 词面钩子逐字覆写（remoteSummary/metaHints/
//     firstUseEmpty/firstUseDownloadLabel/remoteUnavailable/uninstallRunningHint）；
//   - 版本区数据面与写动作编排归共享 store（useManagedConsole），状态轮询/双事件
//     订阅/请求代次/下载票据随之单源化；
//   - 控制台整合条（EverythingConsoleBar）与内嵌搜索、结果区方言一律不动，
//     其控制动词仍走视图 useAsyncAction——与搜索防抖共享单飞闩（P0 批 3·4.5
//     契约，store.busy 不横跨控制台钮面）。
// 适配器暂驻视图文件：托管共享契约要求 adapter 注入，而 adapters/everything.ts
// 的建档不在本轮可改面（波 2F 编辑白名单）；待主线平移进 adapters/ 即成标准形。
import { ref, computed, onMounted } from 'vue'
import * as EverythingAPI from '../../bindings/hanxi/internal/modules/everything/everythingservice'
import type { EverythingRelease } from '../../bindings/hanxi/internal/modules/everything/version/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/everything/instance/models'
import type { DownloadTicket } from '../../bindings/hanxi/internal/modules/everything/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useEverythingSearch } from '../composables/useEverythingSearch'
import type { ManagedModuleAdapter, ManagedReleaseRecord } from '../components/managed/adapter'
import { useManagedConsole } from '../components/managed/store'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import EverythingConsoleBar from '../components/everything/EverythingConsoleBar.vue'
import EverythingResultsPanel from '../components/everything/EverythingResultsPanel.vue'

const { busy, run } = useAsyncAction()
const { showToast } = useToast()
const { confirm } = useConfirm()
const { prompt } = usePrompt()

// 内嵌搜索 + ES 组件就绪（逻辑收编 useEverythingSearch，视图只做 props/emits 接线）
const {
  keyword, searched, results, searching, searchError, truncated,
  esReady, esBusy, esProgress, composing,
  ensureTool, onKeywordInput, onKeywordEnter, onCompositionEnd, doSearch,
  openResult, revealResult, copyResult, handleEsTicket,
} = useEverythingSearch(busy)

// ---------- 托管共享契约（波 2F） ----------
/**
 * 状态快照代次（P0 批 3·4.4 原视图口径保真）：store.refresh 不自带请求代次，
 * 闸口前移到 adapter.getStatus——旧响应晚到交回上一份新鲜快照，store 的
 * sameSnapshot 内容判等随即弃写，「慢回的旧状态不倒灌新状态」逐字等价。
 */
let statusSeq = 0
let lastSnap: Snapshot | null = null

/** 面板 #release-extra-col 行回投方言型：listReleases 原样直交（无字段映射），
 *  store.releases 的成员运行时即 EverythingRelease，断言只补类型视角。 */
const releaseRow = (rel: ManagedReleaseRecord) => rel as EverythingRelease

/** 通道徽标词（逐字沿自原方言表）。 */
function channelLabel(channel: string): string {
  if (channel === 'stable') return '稳定'
  if (channel === 'beta') return '1.5 测试'
  return channel || '其他'
}

const adapter: ManagedModuleAdapter<Snapshot> = {
  async getStatus() {
    const generation = ++statusSeq
    const s = await EverythingAPI.GetStatus()
    if (generation !== statusSeq) return lastSnap
    lastSnap = s
    return s
  },

  subscribeInstanceState(cb) {
    useWailsEvent<Snapshot>('everything:instance-state', (s) => {
      if (s) cb(s)
    })
  },

  // 单订阅双分发（铁律③）：es 组件票据交 useEverythingSearch 独立成态、
  // 不进版本 map；app 版本票据归一后交共享 downloading；缺 component/空票据守卫逐字保留。
  subscribeProgress(cb) {
    useWailsEvent<DownloadTicket>('everything:download', (t) => {
      if (!t || !t.component) return
      if (t.component === 'es') {
        handleEsTicket(t)
        return
      }
      cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
    })
  },

  versions: {
    listInstalled: EverythingAPI.ListInstalledVersions,
    listReleases: EverythingAPI.ListReleases,
    getActive: EverythingAPI.GetActiveVersion,
    async setActive(version) {
      const active = await EverythingAPI.SetActiveVersion(version)
      return { activeVersion: active, message: `已将 ${active} 设为使用版本` }
    },
    async download(rel) {
      const result = await EverythingAPI.DownloadVersion(rel.version)
      if (result === 'already-installed') {
        return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
      }
      return {}
    },
    async remove(info) {
      const ok = await confirm({
        title: `卸载 Everything ${info.version}`,
        description: '该版本隔离目录及其配置与索引库将被删除，不可恢复。',
        tone: 'danger',
        confirmLabel: '卸载',
      })
      if (!ok) return {}
      await EverythingAPI.RemoveVersion(info.version)
      return { message: `已卸载 ${info.version}`, reloadVersions: true }
    },
    async importLocal() {
      const path = await prompt({
        title: '导入本地安装',
        label: 'Everything 安装目录完整路径',
        description: '将整套迁移 exe/配置/语言包/索引库',
      })
      if (!path) return {}
      const info = await EverythingAPI.ImportLocal(path.trim())
      return { message: `已导入 Everything ${info.version}（含配置与索引库，无需重建索引）`, reloadVersions: true }
    },
    async openDir(info) {
      // 打开安装目录必须传「目录」路径——explorer.exe 收文件参数会执行文件
      // （markeron 教训），本模块目录语义统一走 OpenTarget（= 资源管理器打开目录）。
      await EverythingAPI.OpenTarget(info.dir)
      return {}
    },
  },

  copy: {
    remoteSummary: (releaseCount) => `远程槽位 ${releaseCount} 个（稳定 + 1.5 测试）`,
    metaHints: [
      '便携包下载自官网 voidtools（官方 sha256 校验）；或导入本机已有安装连配置与索引库一起收纳',
      '托管启动会自动隐藏 Everything 托盘图标；注意：手动直接运行版本目录里的 exe 将没有托盘，退出需用任务管理器',
    ],
    firstUseEmpty: '尚未安装 Everything —— 下载官方便携版，或「导入本地安装」把现有配置与索引库整套搬进来（免重建索引）',
    firstUseDownloadLabel: (rel) => `下载 ${releaseRow(rel).channel === 'stable' ? '稳定版' : '最新版'} ${rel.version}`,
    remoteUnavailable: '无法加载远程版本列表（官网不可达），已尝试内置快照——可稍后点击「↻ 刷新远程列表」重试',
    uninstallRunningHint: '请先退出 Everything',
  },
}

// 版本区写动作全部走 store 编排（runDownload/runSetActive/runRemove/runImport/runOpenDir）：
// 面板动作彼此单飞互斥（store.busy 闩）；控制台三钮与搜索仍共享视图 busy 闩（见文件头）。
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron 同构）
const activeMainTab = ref('console')
const mainTabs = [
  { key: 'console', label: '🔎 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 控制台方言投影（真相取共享 store，词面与判定逐字沿自波 2F 前现状） ----------
const snap = computed(() => store.snap as Snapshot | null)
const state = computed(() => store.state)
const runningVersion = computed(() => store.runningVersion)

// §9.5-5 文案评审结论：通用五态接单一来源（running=「运行中」）；
// Everything 的 running 按托管模式细分「后台/窗口运行中」，属业务扩展话术，保留局部覆写。
// 不取 store.stateText：N43 未安装降档词（「未安装」）不入本视图控制台方言现状口径。
const stateText = computed(() => {
  switch (state.value) {
    case 'running':
      return snap.value?.mode === 'background' ? '后台运行中' : '窗口运行中'
    case 'starting': return '启动中…'
    case 'failed': return '异常退出'
    case 'external': return '外部运行'
    default: return '未运行'
  }
})

// 条件提示条（三个变体互斥）
const banner = computed<{ tone: 'ok' | 'warn' | 'error'; text: string } | null>(() => {
  if (state.value === 'external') {
    return {
      tone: 'warn',
      text: '检测到外部 Everything 实例（非 Hanxi 托管）。可唤起其搜索窗口，也可按低损档退出：先经 -quit 优雅请求（落盘索引库），无响应时强制结束——索引将在下次启动增量重建。以管理员权限运行的实例无法代杀，需在托盘自行退出。内嵌搜索对默认实例有效。',
    }
  }
  if (state.value === 'failed') {
    return { tone: 'error', text: snap.value?.error || 'Everything 异常退出' }
  }
  if (state.value === 'running') {
    return {
      tone: 'ok',
      text: snap.value?.mode === 'background'
        ? 'Everything 在后台驻留建索引：搜索窗口秒开、内嵌搜索可用。空闲 3 分钟将自动退出，再次搜索自动重启。'
        : 'Everything 正在运行。关闭搜索窗口不会退出实例（继续后台驻留）；空闲 3 分钟自动退出。',
    }
  }
  return null
})

// ---------- 控制操作（视图 busy 闩：与搜索防抖单飞互斥，P0 批 3·4.5） ----------
async function startBackground() {
  if (busy.value) return
  const r = await run(() => EverythingAPI.StartBackground())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await store.refresh()
}

async function openWindow() {
  if (busy.value) return
  const r = await run(() => EverythingAPI.OpenWindow())
  showToast(r.ok ? r.data.message : getErrorMessage(r.error))
  await store.refresh()
}

async function quitEverything() {
  if (busy.value) return
  const r = await run(() => EverythingAPI.Quit())
  showToast(r.ok ? r.data.message : `退出失败: ${getErrorMessage(r.error)}`)
  await store.refresh()
}

// ---------- 联动开关 ----------
const followOnExit = ref(false)

async function loadExtras() {
  try {
    followOnExit.value = await EverythingAPI.GetFollowOnExit()
  } catch (e) {
    console.warn('loadExtras failed:', getErrorMessage(e))
  }
}

async function onFollowToggle() {
  const next = !followOnExit.value
  followOnExit.value = next // 用户点击已将勾选框翻转，ref 同步跟进，保持绑定状态一致
  try {
    await EverythingAPI.SetFollowOnExit(next)
    showToast(next ? '已开启：Hanxi 退出时一并关闭 Everything' : '已关闭：Hanxi 退出不影响该工具，Everything 继续独立运行（下次启动生效）')
  } catch (e) {
    followOnExit.value = !next // 失败回滚：ref 变化驱动勾选框复位到后端真实值
    showToast('设置失败: ' + getErrorMessage(e))
  }
}

onMounted(async () => {
  // 版本三源加载已归 store（onMounted 自动 load）；状态首拉沿用迁移前双路形制
  // （轮询 immediate 首跑之外，挂载显式再拉一次——P0 批 3·4.4 请求代次口径的
  // 前提就是挂载期两发并发）。联动开关读取与 ES 搜索组件预就绪留在视图。
  void store.refresh()
  await Promise.all([loadExtras(), ensureTool()])
})
</script>

<template>
  <section class="page everything-view">
    <PageHeader
      title="Everything 搜索"
      subtitle="托管 Everything 后台索引与搜索窗口；Hanxi 内直接秒搜文件。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <!-- 顶部整合条：状态 + 启停按钮 + 搜索框，一行内解决问题 -->
      <EverythingConsoleBar
        :state="state"
        :state-text="stateText"
        :busy="busy"
        :running-version="runningVersion"
        :pid="snap?.pid"
        :uptime-sec="store.uptimeSec"
        :searching="searching"
        :es-ready="esReady"
        :es-busy="esBusy"
        :es-progress="esProgress"
        :keyword="keyword"
        @update:keyword="keyword = $event"
        @keyword-input="onKeywordInput"
        @keyword-enter="onKeywordEnter"
        @composition-start="composing = true"
        @composition-end="onCompositionEnd"
        @search="doSearch"
        @install-es="ensureTool()"
        @start-background="startBackground"
        @open-window="openWindow"
        @quit="quitEverything"
      />

      <!-- 条件提示条 / 引导行 -->
      <UiBanner v-if="banner" :tone="banner.tone">{{ banner.text }}</UiBanner>
      <div v-else-if="state === 'stopped'" class="hint-line">
        尚未运行：上方直接输入关键词即会搜索（首次自动拉起后台实例，约 1~3 秒）；「启动后台」常驻索引、「打开窗口」直接现搜。空闲 3 分钟自动退出。
      </div>
      <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Everything 实例（约 1~3 秒）…</div>

      <div v-if="searchError" class="error-box slim">{{ searchError }}</div>

      <!-- 结果区 -->
      <EverythingResultsPanel
        :results="results"
        :searched="searched"
        :truncated="truncated"
        :searching="searching"
        @open="openResult"
        @reveal="revealResult"
        @copy="copyResult"
      />
    </div>

    <!-- 版本管理 Tab（波 2F：共享面板直挂，方言经具名槽承接） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store">
        <!-- 随关联动开关（原 meta-info 位形制不变，波 2E #meta-extra 承接位） -->
        <template #meta-extra>
          <label class="toggle-label">
            <input type="checkbox" :checked="followOnExit" @change="onFollowToggle" />
            <span>随 Hanxi 一起关闭 <span class="hint-dim">（默认关闭：Hanxi 退出不影响该工具；开启后退出时经 -quit 优雅收尾并落盘索引库）</span></span>
          </label>
        </template>
        <!-- 通道列（波 2A 方言列槽）：通道徽标 + 快照降级标记，画法逐字沿自原方言表 -->
        <template #release-extra-col-head>通道</template>
        <template #release-extra-col="{ release }">
          <div class="channel-cell">
            <span class="channel-badge" :class="{ 'ch-stable': releaseRow(release).channel === 'stable', 'ch-beta': releaseRow(release).channel !== 'stable' }">
              {{ channelLabel(releaseRow(release).channel) }}
            </span>
            <span v-if="releaseRow(release).stale" class="badge badge-pre">快照</span>
          </div>
        </template>
      </ManagedVersionPanel>
    </div>
  </section>
</template>

<style scoped>
/* 共享层已接管：.page/.header-row/.subtitle/.error-box/.btn 家族/.tbl/.mono/.link-button/
   .empty-state/.banner(UiBanner)/.main-tab-nav(MainTabNav)；搜索控制台/结果表的业务样式
   随 DOM 迁入 components/everything/* 子组件 scoped；版本区样式随波 2F 面板化一并出清——
   .control-panel/.meta-info/.btn-group/.section-title/.installed-grid 留回全局原子家族，
   .toggle-label 本视图方言行仍消费。 */
.everything-view { display: flex; flex-direction: column; gap: 14px; }
.tab-body { display: flex; flex-direction: column; gap: 16px; }
.hint-line { font-size: var(--text-sm); color: var(--color-text-subtle); padding-left: 2px; }
.error-box.slim { padding: 8px 12px; font-size: var(--text-sm); }

.toggle-label { display: flex; align-items: center; gap: 8px; font-size: var(--text-base); color: var(--color-text); cursor: pointer; margin-top: 4px; }
.toggle-label input { width: 15px; height: 15px; cursor: pointer; }

/* ---------- #release-extra-col 通道列（逐字沿自 EverythingReleaseTable，随槽位移居本视图） ---------- */
.channel-cell { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.channel-badge { font-size: var(--text-xs); font-weight: 600; padding: 2px 8px; border-radius: var(--radius-pill); }
.ch-stable { background: var(--state-positive-soft); color: var(--state-positive); }
.ch-beta { background: var(--state-warning-soft); color: var(--state-warning); }
/* .badge 基形由 components.css 全局原子接管，此处仅快照档位色（原方言表同词同色） */
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); }
</style>
