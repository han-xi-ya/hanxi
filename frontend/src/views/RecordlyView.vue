<script setup lang="ts">
// Recordly 控制台（Wave 5 · 批 0 契约收敛件）：状态/版本加载/安装进度 map/
// 时长 ticker/busy 闩/启停编排/联动辅助卡全部收进 adapters/recordly +
// components/managed 家族（store 一份，控制条与辅助卡共享注入）。
// 本视图只余业务方言（共享面板装不下、逐字保持）：单目录「托管安装」卡片、
// 远程表核心版本互认（coreCompare）、安装中/校验并静默安装等本模块词面、
// stable/beta 通道行（adapter.channel 槽的真实页面消费位）、升级警告条与
// 引用已装版本号的引导行（纯快照投影承载不了）。
// 与 CCSwitchView 的结构差异：单版本托管目录（NSIS oneClick 语义，无"设为使用"）、
// stable/beta 双通道切换、安装器未签名风险文案。
import { computed, onMounted, ref } from 'vue'
import type { ManagedReleaseRecord, NormalizedProgress } from '../components/managed/adapter'
import { createRecordlyAdapter } from '../adapters/recordly'
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

const adapter = createRecordlyAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron/everything/ccswitch 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🎬 控制台' },
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
const installedInfo = computed(() => store.installed[0] ?? null)

// 核心版本号（去预发布后缀）：NSIS 单目录语义下 beta 的 PE 版本与 tag 互认依据
function coreOf(v: string): string {
  return v.replace(/-.*$/, '')
}

// coreCompare：a>b 返回 1；不可解析退化为字典序比较
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

// 可升级：已装核心 < 当前通道最新核心
const upgradeAvailable = computed(() => {
  if (!installedInfo.value || store.releases.length === 0) return false
  return coreCompare(installedInfo.value.version, store.releases[0].version) < 0
})

// 单目录卡片「运行中」徽标：核心互认（PE 版本与 tag 后缀互认场）
const runningBadge = computed(
  () => store.state === 'running' && !!installedInfo.value && coreCompare(store.runningVersion, installedInfo.value.version) === 0,
)

function stepOf(p: NormalizedProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

// 安装状态判定：精确 tag 命中，或数值核心一致（PE 版本抹掉 -beta 后缀的互认场）
function statusOf(rel: ManagedReleaseRecord): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = store.downloading[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = store.installed.find((v) => v.version === rel.version || coreCompare(v.version, rel.version) === 0)
  return hit ? 'installed' : 'idle'
}
</script>

<template>
  <section class="page recordly-view">
    <PageHeader title="Recordly" subtitle="托管开源录屏工具 Recordly：版本管理、静默安装、启停与窗口唤起。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/启停钮/条件提示条由 ManagedControlBar（adapter 投影）承载 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" />

      <!-- 引导行（banner 缺席时）：文案引用已装版本，共享 hint(snap) 承载不了，留视图 -->
      <template v-if="!store.banner">
        <div v-if="store.state === 'stopped' && installedInfo" class="hint-line">
          尚未运行：点击「打开窗口」启动 Recordly {{ installedInfo.version }}，录制与剪辑在其窗口内完成。配置恒存于 %APPDATA%\Recordly，与托管版本无关。
        </div>
        <div v-else-if="store.state === 'stopped'" class="hint-line">
          尚未安装：请到「版本管理」在线安装或导入本地副本。
        </div>
        <div v-else-if="store.state === 'starting'" class="hint-line">正在拉起 Recordly（Electron 冷启动约 2~10 秒）…</div>
      </template>

      <UiBanner v-if="upgradeAvailable && installedInfo" tone="warn" class="slim">
        发现可升级版本 {{ store.releases[0].version }}（当前 {{ installedInfo.version }}）——到「版本管理」一键安装，安装期间请先退出运行中的实例。
      </UiBanner>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 Recordly</summary>
        <div class="info-body">
          <p>开源演示录屏与自动剪辑工具（<a class="inline-link" href="https://github.com/webadderallorg/Recordly" target="_blank" rel="noopener">webadderallorg/Recordly</a>，AGPL-3.0）。Hanxi 仅做官方原版安装器的下载托管与启停管理，不内嵌不打包其代码。</p>
          <p class="hint-dim">上游无 Windows 免安装包：在线安装使用官方 NSIS 安装器静默装进 Hanxi 托管目录，安装器自动更新已由官方开关禁用，版本升级统一走此处版本管理。安装器未数字签名，托管直装不触发 SmartScreen；若杀毒软件误拦请把托管目录加入白名单。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关/快捷方式/数据目录/仓库，全部经 adapter.extras 投影） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab（方言表：单目录托管安装卡 + 核心互认远程表，留视图） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>
            当前托管 <strong>{{ installedInfo ? installedInfo.version : '未安装' }}</strong> · {{ channel === 'beta' ? 'beta 通道（含预发布）' : 'stable 通道' }} · 远程版本 {{ store.releases.length }} 个
          </span>
          <span class="hint-dim">Windows 仅提供 NSIS 在线安装器（GitHub digest + SHA256SUMS 双源校验），安装/升级统一静默落进托管目录，多版本不共存为上游安装器语义所限</span>
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
        <span v-if="channel === 'beta'" class="beta-warn">beta 版上游标注"可能不稳定"，仅供尝鲜</span>
      </div>

      <!-- 已安装 -->
      <div class="section-title"><h3>托管安装</h3></div>

      <UiEmptyState v-if="!installedInfo" class="first-use">
        <p>尚未安装 Recordly —— 在线安装官方版本，或「导入本地安装」把机器上已有的安装目录收编进来</p>
        <UiButton v-if="store.releases.length" variant="primary" @click="store.runDownload(store.releases[0])">
          安装最新版 {{ store.releases[0].version }}（约 {{ fmtSize(store.releases[0].size) }}）
        </UiButton>
        <UiButton v-else-if="!store.loading" variant="secondary" @click="store.load()">↻ 刷新远程列表</UiButton>
      </UiEmptyState>

      <div v-else class="installed-grid">
        <div class="installed-card card-active">
          <div class="inst-card-top">
            <span class="ver-tag">{{ installedInfo.version }}</span>
            <div class="inst-badges">
              <span v-if="runningBadge" class="badge badge-running">运行中</span>
              <span v-if="installedInfo.isImport" class="badge badge-import">本地导入</span>
              <span v-else class="badge badge-official">官方下载</span>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">路径</span><code class="mono">{{ installedInfo.exePath }}</code></div>
            <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(installedInfo.size) }} · 安装于 {{ installedInfo.installedAt }}</span></div>
            <div class="meta-line" v-if="installedInfo.isImport && installedInfo.source"><span class="k">来源</span><span class="hint-dim">{{ installedInfo.source }}</span></div>
          </div>
          <div class="inst-actions">
            <UiButton variant="secondary" small @click="store.runOpenDir(installedInfo)">📂 打开位置</UiButton>
            <UiButton
              variant="danger"
              small
              :disabled="store.state === 'running'"
              :title="store.state === 'running' ? '请先退出 Recordly' : ''"
              @click="store.runRemove(installedInfo)"
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
                <!-- 类名刻意用 rd-ver-status（含 rd- 前缀）与全局原子族隔离 -->
                <span v-if="statusOf(rel) === 'installed'" class="rd-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="rd-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="rd-ver-status error">失败</span>
                <span v-else class="rd-ver-status idle">可安装</span>
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
                  <span v-if="['verify', 'install'].includes(store.downloading[rel.version]!.stage)">校验并静默安装…</span>
                  <span v-else class="dl-error" :title="store.downloading[rel.version]!.message">{{ store.downloading[rel.version]!.message }}</span>
                </div>
                <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                  <span class="dl-error" :title="store.downloading[rel.version]!.message">{{ store.downloading[rel.version]!.message }}</span>
                </div>
                <UiButton
                  v-if="statusOf(rel) === 'idle'"
                  variant="primary"
                  small
                  :disabled="store.isRunningOrStarting"
                  :title="store.isRunningOrStarting ? '请先退出运行中的 Recordly' : ''"
                  @click="store.runDownload(rel)"
                >{{ installedInfo ? '覆盖安装' : '安装' }}</UiButton>
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
/* 页头/控制条/提示条/联动卡/页签与 flex 骨架由 managed 组件 + components.css
   全局原子接管；本视图仅余方言表（rd-ver-status）与通道行等私有形。 */
.recordly-view { display: flex; flex-direction: column; gap: 10px; }

/* status-word/ver-pill/pid-tag/uptime-tag/control-btns 与状态信号灯由
   ManagedControlBar 承载（前缀复制体 .rd-status-light 自此退役） */

/* hint-line/info-details/info-summary/info-body p 等由全局原子接管 */
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

/* ---------- 托管安装卡片（installed-grid/installed-card/inst-card-top/inst-badges/ver-tag 由全局原子接管；
   本视图原 minmax(340px) 散差按标准形 minmax(320px) 定档删除） ---------- */
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

/* inst-meta/meta-line(.k)/inst-actions、table-container/ver-name 由全局原子接管 */

/* ---------- 远程表格 ---------- */
.rd-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.rd-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.rd-ver-status.installed::before { background: var(--state-positive); }
.rd-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.rd-ver-status.error::before { background: var(--state-danger); }
.rd-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管（本视图原 dl-bar-inner
   "fast linear" 散差按标准形 "base ease" 定档删除） */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管，
   卡体由 ManagedExtrasCard 承载） ---------- */
</style>
