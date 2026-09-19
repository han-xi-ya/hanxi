<script setup lang="ts">
// Paseo 控制台（Wave 5 · 批 0 契约收敛件）：状态/版本加载/安装进度 map/
// 时长 ticker/busy 闩/启停与设版编排/联动辅助卡全部收进 adapters/paseo +
// components/managed 家族（store 一份，控制条与辅助卡共享注入）。
// 本视图只余业务方言（共享面板装不下、逐字保持）：多版本卡片（使用版本徽标
// + 自动最新回退高亮 + 未验证哈希列）、通道行（adapter.channel 槽的真实页面
// 消费位）、升级警告条、常驻自动更新提示条与引用将启版本的引导行。
// 与 CCSwitchView 的结构差异：单设定版本可空（空=自动最新已装）、stable/beta
// 双通道切换、双数据目录入口（Electron 走 extras.dataDir，daemon 走 #extras-action 槽）。
import { computed, onMounted, ref } from 'vue'
import type { PaseoVersionInfo } from '../../bindings/hanxi/internal/modules/paseo/version/models'
import type { ManagedReleaseRecord, ManagedVersionRecord, NormalizedProgress } from '../components/managed/adapter'
import { createPaseoAdapter, openPaseoDaemonHome } from '../adapters/paseo'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'

const adapter = createPaseoAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 recordly/vscode 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🐾 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 更新通道（adapter.channel 槽批 0 无共享 UI，视图驱动） ----------
const channel = ref<'stable' | 'beta'>('stable')

async function switchChannel(target: 'stable' | 'beta') {
  if (target === channel.value) return
  try {
    const res = await adapter.channel!.set(target)
    channel.value = target
    if (res?.reloadVersions) await store.load()
  } catch (e) {
    showToast(`切换通道失败: ${getErrorMessage(e)}`)
  }
}

onMounted(() => {
  // 通道初值（迁移前随 loadVersions 的 localTask 并带取回；store 版本区无通道位，
  // 改视图侧独立拉取，读取失败保留现词 '读取本地版本失败: '）
  void Promise.resolve(adapter.channel!.get())
    .then((ch) => {
      channel.value = ch === 'beta' ? 'beta' : 'stable'
    })
    .catch((e: unknown) => {
      store.listError = `读取本地版本失败: ${getErrorMessage(e)}`
    })
})

// ---------- 派生状态 ----------
const state = computed(() => store.state)

// 核心版本号（去预发布后缀）：升级判定用数值核心对比，不可解析退化字典序
function coreOf(v: string): string {
  return v.replace(/-.*$/, '')
}

function coreCompare(a: string, b: string): number {
  const pa = coreOf(a).split('.').map(Number)
  const pb = coreOf(b).split('.').map(Number)
  for (let i = 0; i < 3; i++) {
    const na = pa[i], nb = pb[i]
    if (Number.isNaN(na) || Number.isNaN(nb)) return coreOf(a) < coreOf(b) ? -1 : coreOf(a) > coreOf(b) ? 1 : 0
    if (na !== nb) return na > nb ? 1 : -1
  }
  return 0
}

// 最新已装规范版本（后端已新→旧排序，imported- 目录不参与升级判定）
const latestInstalled = computed(() =>
  store.installed.find((v) => /^\d+\.\d+\.\d+/.test(v.version)) ?? null)

// 可升级：最新已装核心 < 当前通道最新核心
const upgradeAvailable = computed(() => {
  if (!latestInstalled.value || store.releases.length === 0) return false
  return coreCompare(latestInstalled.value.version, store.releases[0].version) < 0
})

// 冷启动将使用的版本（activeVersion 未设定时后端回退最新已装）
const launchVersion = computed(() => store.activeVersion || latestInstalled.value?.version || '')

// 常驻风险提示：应用内"安装更新"会装出托管外的平行副本（上游无禁用开关）
const updaterNote = '上游无自动更新禁用开关：在 Paseo 界面内点「安装更新」会把新版装进 %LOCALAPPDATA%（托管目录外的平行副本），版本升级请统一走这里。'

// 「未验证哈希」方言列：ManagedVersionRecord 无 verifiedHash 扩展位（契约只对
// 快照开放 S 泛型），运行时对象即 PaseoVersionInfo，cast 读取。
function unverifiedHash(v: ManagedVersionRecord): boolean {
  return !(v as Partial<PaseoVersionInfo>).verifiedHash && !v.isImport
}

function stepOf(p: NormalizedProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

// 安装状态判定：版本目录名与远程 tag（去 v）精确互等
function statusOf(rel: ManagedReleaseRecord): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = store.downloading[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  return store.installed.some((v) => v.version === rel.version) ? 'installed' : 'idle'
}

// daemon 数据目录钮（#extras-action 槽位，见 adapters/paseo 差异④）
async function openDaemonHome() {
  try {
    await openPaseoDaemonHome()
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}
</script>

<template>
  <section class="page paseo-view">
    <PageHeader title="Paseo" subtitle="托管开源 coding agent 编排器 Paseo：版本管理、启停与窗口唤起，agent 会话在其窗口与手机端继续运行。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/启停钮/条件提示条由 ManagedControlBar（adapter 投影）承载 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" />

      <!-- 引导行（banner 缺席时）：文案引用将启版本，共享 hint(snap) 承载不了，留视图 -->
      <template v-if="!store.banner">
        <div v-if="state === 'stopped' && launchVersion" class="hint-line">
          尚未运行：点击「打开窗口」启动 Paseo {{ launchVersion }}。agent 编排与手机配对在其窗口内操作；数据在 %APPDATA%\Paseo 与 ~/.paseo，与托管版本目录无关。
        </div>
        <div v-else-if="state === 'stopped'" class="hint-line">
          尚未安装：请到「版本管理」在线安装或导入本地副本。
        </div>
        <div v-else-if="state === 'starting'" class="hint-line">正在拉起 Paseo（Electron 冷启动 + 内置 daemon 拉起，约 2~15 秒）…</div>
      </template>

      <UiBanner v-if="upgradeAvailable && latestInstalled" tone="warn" class="slim">
        发现可升级版本 {{ store.releases[0].version }}（当前最新已装 {{ latestInstalled.version }}）——到「版本管理」一键安装。
      </UiBanner>

      <UiBanner tone="info" class="slim">{{ updaterNote }}</UiBanner>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 Paseo</summary>
        <div class="info-body">
          <p>开源 coding agent 编排器（<a class="inline-link" href="https://github.com/getpaseo/paseo" target="_blank" rel="noopener">getpaseo/paseo</a>，Apache-2.0）：本机跑 daemon，桌面/手机/Web 统一调度 Claude Code、Codex 等 agent CLI。Hanxi 仅做官方便携 zip 的下载托管与启停管理，不内嵌不打包其代码。</p>
          <p class="hint-dim">共享数据模式（与 cc-switch 同构）：托管实例与自装实例同数据同锁组，全局至多一个桌面主实例；「外部」状态即你的自装实例在场。唤窗优先直接唤起已有窗口（上游二次拉起语义是"再开新窗"而非聚焦）。无窗运行 ≠ 空闲——其 agent 会话可能正在进行，本模块不设空闲自动退出。</p>
          <p class="hint-dim">开启「随 Hanxi 一起关闭」后，Hanxi 退出会连带终止 Paseo 及其 daemon 上正在运行的全部 agent 会话，请谨慎。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关/快捷方式/Electron 数据/仓库经 adapter.extras 投影；
         daemon 数据为第二位数据目录，经 #extras-action 槽注入） -->
    <ManagedExtrasCard :adapter="adapter">
      <template #extras-action>
        <button class="btn btn-secondary btn-small" title="打开 daemon 数据主目录（~/.paseo：持久配置、会话与手机配对）" @click="openDaemonHome">🐾 daemon 数据</button>
      </template>
    </ManagedExtrasCard>

    <!-- 版本管理 Tab（方言表：多版本卡片 + 通道行 + 精确匹配远程表，留视图） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>
            使用版本 <strong>{{ store.activeVersion || (latestInstalled ? `自动最新（${latestInstalled.version}）` : '未安装') }}</strong> · {{ channel === 'beta' ? 'beta 通道（含预发布）' : 'stable 通道' }} · 已装 {{ store.installed.length }} 版 · 远程版本 {{ store.releases.length }} 个
          </span>
          <span class="hint-dim">官方 Windows 便携 zip 解压进独立版本目录（GitHub digest sha256 + 字节数 + zip CRC + 布局四层校验），多版本共存，数据共享不受版本增删影响</span>
        </div>
        <div class="btn-group">
          <UiButton variant="secondary" small :disabled="store.busy" @click="store.runImport()">⇥ 导入本地安装</UiButton>
          <UiButton variant="secondary" small :disabled="store.loading" @click="store.load()">
            {{ store.loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </UiButton>
        </div>
      </div>

      <!-- 更新通道切换（adapter.channel：Get/SetReleaseChannel 的真实页面消费位） -->
      <div class="channel-row">
        <span class="k">更新通道</span>
        <div class="channel-seg">
          <button :class="{ active: channel === 'stable' }" @click="switchChannel('stable')">Stable 稳定</button>
          <button :class="{ active: channel === 'beta' }" @click="switchChannel('beta')">Beta 预发布</button>
        </div>
        <span v-if="channel === 'beta'" class="beta-warn">beta 为上游预发布版，仅供尝鲜</span>
      </div>

      <!-- 已安装 -->
      <div class="section-title"><h3>托管版本</h3></div>

      <UiEmptyState v-if="store.installed.length === 0" class="first-use">
        <p>尚未安装 Paseo —— 在线安装官方便携 zip 解压版，或「导入本地安装」把机器上已有的程序目录收编进来</p>
        <UiButton v-if="store.releases.length" variant="primary" @click="store.runDownload(store.releases[0])">
          安装最新版 {{ store.releases[0].version }}（约 {{ fmtSize(store.releases[0].size) }}）
        </UiButton>
        <UiButton v-else-if="!store.loading" variant="secondary" @click="store.load()">↻ 刷新远程列表</UiButton>
      </UiEmptyState>

      <div v-else class="installed-grid">
        <div
          v-for="v in store.installed" :key="v.version"
          class="installed-card" :class="{ 'card-active': store.activeVersion === v.version || (!store.activeVersion && latestInstalled?.version === v.version) }"
        >
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <span v-if="state === 'running' && store.runningVersion === v.version" class="badge badge-running">运行中</span>
              <span v-else-if="(store.activeVersion || latestInstalled?.version) === v.version" class="badge badge-active">使用版本</span>
              <span v-if="unverifiedHash(v)" class="badge badge-import">未验证哈希</span>
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
            <UiButton
              v-if="(store.activeVersion || latestInstalled?.version) !== v.version"
              variant="primary" small
              :disabled="store.busy"
              title="下次启动使用该版本（运行中的实例不受影响）"
              @click="store.runSetActive(v)"
            >设为使用</UiButton>
            <UiButton variant="secondary" small @click="store.runOpenDir(v)">📂 打开位置</UiButton>
            <UiButton
              variant="danger"
              small
              :disabled="state === 'running' && store.runningVersion === v.version"
              :title="state === 'running' && store.runningVersion === v.version ? '请先退出该版本' : '仅删本版本目录，共享数据不受影响'"
              @click="store.runRemove(v)"
            >卸载</UiButton>
          </div>
        </div>
      </div>

      <!-- 远程可用版本 -->
      <div class="section-title"><h3>远程可用版本</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 160px;">版本</th>
              <th style="width: 170px;">状态</th>
              <th style="width: 90px;">大小</th>
              <th style="width: 110px;">发布时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rel in store.releases" :key="rel.version">
              <td>
                <strong class="ver-name">{{ rel.version }}</strong>
                <span v-if="rel.isPre" class="badge badge-pre">预发布</span>
              </td>
              <td>
                <!-- 类名刻意用 ps- 前缀与全局原子族隔离（markeron 垂直字体事故教训） -->
                <span v-if="statusOf(rel) === 'installed'" class="ps-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="ps-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="ps-ver-status error">失败</span>
                <span v-else class="ps-ver-status idle">可安装</span>
              </td>
              <td>{{ fmtSize(rel.size) }}</td>
              <td>{{ fmtDate(rel.published) }}</td>
              <td>
                <div v-if="statusOf(rel) === 'downloading' && store.downloading[rel.version]!.stage === 'downloading'" class="download-cell">
                  <div class="dl-bar-wrap">
                    <div class="dl-bar-inner" :style="{ width: `${stepOf(store.downloading[rel.version]!)}%` }"></div>
                  </div>
                  <span class="dl-percent">{{ stepOf(store.downloading[rel.version]!) }}%</span>
                </div>
                <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
                  <span v-if="['verify', 'extract'].includes(store.downloading[rel.version]!.stage)">校验并解压…</span>
                  <span v-else class="dl-error" :title="store.downloading[rel.version]!.message">{{ store.downloading[rel.version]!.message }}</span>
                </div>
                <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                  <span class="dl-error" :title="store.downloading[rel.version]!.message">{{ store.downloading[rel.version]!.message }}</span>
                </div>
                <UiButton
                  v-if="statusOf(rel) === 'idle'"
                  variant="primary"
                  small
                  @click="store.runDownload(rel)"
                >安装</UiButton>
                <span v-if="statusOf(rel) === 'installed'" class="chip chip-positive">已安装</span>
                <a v-if="statusOf(rel) === 'error'" class="retry-link" @click="store.runDownload(rel)">重试</a>
              </td>
            </tr>
            <tr v-if="store.releases.length === 0 && !store.loading">
              <td colspan="5" class="empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 页头/控制条/提示条/页签与 flex 骨架由 managed 组件 + components.css
   全局原子接管；本视图仅余方言表（ps-ver-status）与通道行等私有形。 */
.paseo-view { display: flex; flex-direction: column; gap: 10px; }

/* status-word/pid-tag/uptime-tag/control-btns 与状态信号灯由 ManagedControlBar
   承载（前缀复制体 .ps-status-light 自此退役；原 .control-bar/.ver-pill 方片形制
   覆写对子组件内部节点本就不生效，随收编一并退役，观感落回全局标准形——见迁移报告） */

/* hint-line/info-details/info-summary(.info-summary::after 等)/info-body p 由全局原子接管 */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* control-panel/meta-info/btn-group 由全局原子接管 */

/* ---------- 更新通道切换 ---------- */
.channel-row { display: flex; align-items: center; gap: 10px; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 8px 14px; font-size: var(--text-base); flex-wrap: wrap; }
.channel-row .k { color: var(--color-text-subtle); }
.channel-seg { display: flex; background: var(--surface-hover); border-radius: 6px; padding: 2px; gap: 2px; }
.channel-seg button { border: none; background: transparent; padding: 4px 12px; font-size: var(--text-sm); border-radius: 5px; color: var(--color-text-muted); cursor: pointer; transition: color var(--motion-base) ease, background var(--motion-base) ease; }
.channel-seg button.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
.beta-warn { font-size: var(--text-sm); color: var(--state-warning); }

/* section-title h3/empty-hint 由全局原子接管 */

/* ---------- 托管版本卡片（多版本并存；installed-grid/installed-card(.card-active)/inst-card-top/
   inst-badges/ver-tag 由全局原子接管；本视图原 minmax(340px) 散差按标准形 minmax(320px) 定档删除） ---------- */
/* .badge 基形与 components.css 全局原子逐字同义，scoped 副本已删除；以下仅本视图配色变体 */
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

/* inst-meta/meta-line(.k)/inst-actions、table-container/ver-name 由全局原子接管 */

.ps-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.ps-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ps-ver-status.installed::before { background: var(--state-positive); }
.ps-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ps-ver-status.error::before { background: var(--state-danger); }
.ps-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管（本视图原 dl-bar-inner
   "fast linear" 散差按标准形 "base ease" 定档删除） */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管，
   卡体由 ManagedExtrasCard 承载） ---------- */
</style>
