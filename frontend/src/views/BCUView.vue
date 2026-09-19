<script setup lang="ts">
// BCU 控制台（Wave 5 · 批 1 收敛件，模式照抄 CCSwitchView）：共享面全部进托管
// 控制台家族——adapter（src/adapters/bcu）承载业务投影（RPC/事件/文案/确认输入），
// useManagedConsole 单源状态轮询/uptime/下载进度 map/busy 闩编排，
// ManagedControlBar 管状态头与启停钮，ManagedExtrasCard 管联动辅助卡。
// 方言区（依 everything 通道表先例，DOM 与文案零改动）：双变体下载表——
// 共享面板单钮形态表达不下「便携版/精简版」双入口与 version|variant 复合进度键；
// .NET 环境诊断为版本 Tab 推荐变体的输入，留在原 meta 位（验证 DOM 等价最稳：
// 原位零迁移，adapter.hint 只读快照无法承载环境态）。方言区全部消费 store 现值。
import { computed, onMounted, ref } from 'vue'
import type { BCURelease } from '../../bindings/hanxi/internal/modules/bcu/version/models'
import type { DotnetEnv } from '../../bindings/hanxi/internal/modules/bcu/models'
import { createBCUAdapter, variantRelease, type BCUVariant } from '../adapters/bcu'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import type { NormalizedProgress } from '../components/managed/adapter'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'
import ElevateRestart from '../components/ElevateRestart.vue'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate } from '../utils/format'

const adapter = createBCUAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与各工具模块同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🧹 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- .NET 环境诊断（方言版本 Tab：推荐变体输入） ----------
const dotnetEnv = ref<DotnetEnv | null>(null)
const envLoading = ref(false)

// 推荐变体：有 .NET 8 桌面运行时 → 推荐精简版（省 ~60MB）；
// 无 → 推荐自包含便携版（免依赖永远能跑）。null 环境未加载完毕。
const recommendedVariant = computed<'portable' | 'fdd' | null>(() => {
  if (!dotnetEnv.value) return null
  return dotnetEnv.value.hasNet8 ? 'fdd' : 'portable'
})

async function loadDotnetEnv() {
  envLoading.value = true
  try {
    dotnetEnv.value = (await adapter.getDotnetEnv()) ?? null
  } catch (e) {
    console.warn('bcu GetDotnetEnvironment failed:', getErrorMessage(e))
  } finally {
    envLoading.value = false
  }
}

// 740 提权直拒（后端 elevateHint 文案统一含"管理员"）→ 追加一键提权重启入口
const needsElevate = computed(() => store.state === 'failed' && (store.snap?.error || '').includes('管理员'))

// ---------- 方言表格投影（复合进度键 version|variant，防同版本双变体互相覆盖） ----------
const releases = computed(() => store.releases as BCURelease[])
const installed = computed(() => store.installed)

// 列内推荐变体的下载按钮样式与提示
function variantLabel(variant: string): string {
  return variant === 'fdd' ? '精简版' : '便携版'
}

function stepOf(p: NormalizedProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: BCURelease, variant: BCUVariant): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = store.downloading[`${rel.version}|${variant}`]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  // 同版本任一形态已装则该版本整体视为已装（目录共享）
  const hit = installed.value.find(v => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

// 状态列的整体判定：任一形态 downloading/error 即反映（双变体并存场景）
const VARIANTS: readonly BCUVariant[] = ['portable', 'fdd']

function statusOverall(rel: BCURelease): 'installed' | 'downloading' | 'error' | 'idle' {
  for (const v of VARIANTS) {
    const s = statusOf(rel, v)
    if (s === 'downloading' || s === 'error') return s
  }
  return statusOf(rel, 'portable')
}

function isRecommended(rel: BCURelease, variant: BCUVariant): boolean {
  return recommendedVariant.value === variant && !!rel.fddName
}

function downloadVariant(rel: BCURelease, variant: BCUVariant) {
  void store.runDownload(variantRelease(rel, variant))
}

// ---------- 初始装载（状态首帧与版本区由 store 的 mounted 即触发） ----------
onMounted(() => {
  void loadDotnetEnv()
})
</script>

<template>
  <section class="page bcu-view">
    <PageHeader title="BC 卸载工具" subtitle="托管 Bulk Crap Uninstaller：版本管理、启停与窗口唤起，批量卸载干净又彻底。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/提示条/引导行由 ManagedControlBar 按 adapter 投影渲染 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" />
      <ElevateRestart v-if="needsElevate" route="/ext/bcu" />

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 BCU</summary>
        <div class="info-body">
          <p>开源批量卸载工具（<a class="inline-link" href="https://github.com/BCUninstaller/Bulk-Crap-Uninstaller" target="_blank" rel="noopener">BCUninstaller/Bulk-Crap-Uninstaller</a>，Apache-2.0）：静默卸载、残留清理、孤儿检测、开机项管理一站式完成。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
          <p class="hint-dim">版本目录携带各自独立的 BCUninstaller.settings，「导入本地」整套搬入；「设为使用」在多版本间切换。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关/桌面快捷方式/GitHub 仓库，条目文案在 adapter） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab（方言区：双变体表 + .NET 环境诊断，数据与动作全走 store） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程版本 {{ releases.length }} 个</span>
          <span class="hint-dim">两种形态：自包含便携版（内嵌 .NET 运行时，76MB）+ 精简版（框架依赖，12MB，需本机 .NET 8 桌面运行时）——均经官方 digest 四层校验</span>
          <span class="hint-dim">2024 年末前的旧版本无官方哈希不入列表（完整性第一优先）；6.1 起资产版本号与 tag 对齐校验防串版</span>
          <!-- .NET 环境徽标：决定推荐变体 -->
          <span v-if="envLoading" class="hint-dim">正在检测本机 .NET 桌面运行时…</span>
          <span v-else-if="dotnetEnv" class="dotnet-banner" :class="dotnetEnv.hasNet8 ? 'ok' : 'warn'">
            <template v-if="dotnetEnv.hasNet8">
              本机已装 .NET 8 桌面运行时（{{ dotnetEnv.desktopVersions?.join(' / ') }}）→ 推荐下载<b>精简版</b>（12MB，省约 60MB）
            </template>
            <template v-else>
              未检测到 .NET 8 桌面运行时 → 推荐下载<b>自包含便携版</b>（76MB，免依赖）。精简版安装后会无法启动
            </template>
          </span>
        </div>
        <div class="btn-group">
          <UiButton variant="secondary" small :disabled="store.busy" @click="store.runImport()">⇥ 导入本地安装</UiButton>
          <UiButton variant="secondary" small :disabled="store.loading" @click="store.load()">
            {{ store.loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </UiButton>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ installed.length }})</h3></div>

      <UiEmptyState v-if="installed.length === 0" class="first-use">
        <p>尚未安装 BCU —— 下载官方便携版，或「导入本地安装」把现有 BCU 收纳进来</p>
        <UiButton v-if="releases.length && recommendedVariant" variant="primary" @click="downloadVariant(releases[0], recommendedVariant)">
          下载最新版 {{ releases[0].version }}（{{ variantLabel(recommendedVariant) }}，推荐）
        </UiButton>
        <UiButton v-else-if="releases.length" variant="primary" @click="downloadVariant(releases[0], 'portable')">
          下载最新版 {{ releases[0].version }}
        </UiButton>
        <UiButton v-else-if="!store.loading" variant="secondary" @click="store.load()">↻ 刷新远程列表</UiButton>
      </UiEmptyState>

      <div class="installed-grid">
        <div v-for="v in installed" :key="v.version" class="installed-card" :class="{ 'card-active': store.activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <span v-if="store.activeVersion === v.version" class="badge badge-active">使用中</span>
              <span v-else-if="store.state === 'running' && store.runningVersion === v.version" class="badge badge-running">运行中</span>
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
            <UiButton v-if="store.activeVersion !== v.version" variant="primary" small @click="store.runSetActive(v)">设为使用</UiButton>
            <UiButton variant="secondary" small @click="store.runOpenDir(v)">📂 打开位置</UiButton>
            <UiButton
              variant="danger"
              small
              :disabled="store.state === 'running' && store.runningVersion === v.version"
              :title="store.state === 'running' && store.runningVersion === v.version ? '请先退出 BCU' : ''"
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
                <!-- 类名刻意用 bcu-status（含 bcu- 前缀）——全局原子有 .chip 族，防语义混淆 -->
                <span v-if="statusOverall(rel) === 'installed'" class="bcu-ver-status installed">已安装</span>
                <span v-else-if="statusOverall(rel) === 'downloading'" class="bcu-ver-status downloading">下载中</span>
                <span v-else-if="statusOverall(rel) === 'error'" class="bcu-ver-status error">失败</span>
                <span v-else class="bcu-ver-status idle">可安装</span>
              </td>
              <td>{{ fmtSize(rel.size) }}</td>
              <td>{{ fmtDate(rel.published) }}</td>
              <td>
                <template v-if="statusOf(rel, 'portable') === 'installed'">
                  <span class="chip chip-positive">已安装</span>
                </template>
                <template v-else>
                  <!-- 双变体下载：推荐项主按钮高亮 -->
                  <div class="variant-btns">
                    <button
                      :class="['btn btn-small', isRecommended(rel, 'portable') || recommendedVariant === null ? 'btn-primary' : 'btn-secondary']"
                      :disabled="statusOf(rel, 'portable') === 'downloading' || (!rel.fddName && statusOf(rel, 'fdd') === 'downloading')"
                      @click="downloadVariant(rel, 'portable')"
                    >便携版 {{ fmtSize(rel.size) }}</button>
                    <button
                      v-if="rel.fddName"
                      :class="['btn btn-small', isRecommended(rel, 'fdd') ? 'btn-primary' : 'btn-secondary']"
                      :disabled="statusOf(rel, 'fdd') === 'downloading' || statusOf(rel, 'portable') === 'downloading'"
                      @click="downloadVariant(rel, 'fdd')"
                    >精简版 {{ rel.fddSize ? fmtSize(rel.fddSize) : '' }}</button>
                  </div>
                  <div v-if="['downloading', 'error'].includes(statusOf(rel, 'portable')) || ['downloading', 'error'].includes(statusOf(rel, 'fdd'))" class="variant-progress">
                    <template v-for="v in VARIANTS" :key="v">
                      <div v-if="statusOf(rel, v) === 'downloading' || statusOf(rel, v) === 'error'" class="dl-meta-text">
                        <span v-if="['verify', 'extract'].includes(store.downloading[`${rel.version}|${v}`]!.stage)">
                          {{ variantLabel(v) }}校验解压安装…
                        </span>
                        <span v-else-if="store.downloading[`${rel.version}|${v}`]!.stage === 'downloading'" class="download-cell">
                          <div class="dl-bar-wrap">
                            <div class="dl-bar-inner" :style="{ width: `${stepOf(store.downloading[`${rel.version}|${v}`]!)}%` }"></div>
                          </div>
                          <span class="dl-percent">{{ stepOf(store.downloading[`${rel.version}|${v}`]!) }}%</span>
                        </span>
                        <span v-else class="dl-error" :title="store.downloading[`${rel.version}|${v}`]!.message">
                          {{ store.downloading[`${rel.version}|${v}`]!.message }}
                        </span>
                        <a class="retry-link" @click="downloadVariant(rel, v)">重试</a>
                      </div>
                    </template>
                  </div>
                </template>
              </td>
            </tr>
            <tr v-if="releases.length === 0 && !store.loading">
              <td colspan="5" class="empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 状态头/启停钮/提示条/引导行/联动卡由 managed 组件 + components.css 全局原子接管；
   本页仅余方言版本 Tab 的业务样式与两处对标准形的补差。 */
.bcu-view { display: flex; flex-direction: column; gap: 10px; }

/* 补差 against 全局原子（标准形为 radius-element / radius-pill，本视图控制条与
   版本胶囊为小圆角方片形制）：控制条/胶囊渲染进 ManagedControlBar 子件内部，
   用 :deep 穿层维持逐字同形（原 .bcu-status-light 复制体已由 .status-light 标准形替代） */
.bcu-view :deep(.control-bar) { border-radius: var(--radius-control); }
.bcu-view :deep(.ver-pill) { border-radius: 4px; }

/* ---------- 折叠说明（info-details/info-summary::after/info-body p 由全局原子接管） ---------- */
/* 补差 against 全局原子 .info-summary：本视图标题前带图标，加 4px 间距 */
.info-summary { gap: 4px; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* control-panel/meta-info/btn-group、section-title h3/empty-hint、installed-grid/
   installed-card(.card-active)/inst-* /ver-tag、table-container/ver-name 由全局原子接管；
   .badge 基形与 components.css 全局原子逐字同义，以下仅本视图配色变体 */
.badge-active { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-running { background: var(--state-information-soft); color: var(--state-information); }
.badge-import { background: var(--state-information-soft); color: var(--state-information); }
.badge-official { background: var(--surface-hover); color: var(--color-text-muted); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.bcu-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.bcu-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.bcu-ver-status.installed::before { background: var(--state-positive); }
.bcu-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.bcu-ver-status.error::before { background: var(--state-danger); }
.bcu-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管 */

/* ---------- 双变体下载与 .NET 环境徽标（方言区核心） ---------- */
.variant-btns { display: flex; gap: 6px; align-items: center; }
.variant-progress { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.variant-progress .dl-meta-text { font-size: var(--text-xs); }
.dotnet-banner { font-size: var(--text-sm); padding: 4px 10px; border-radius: 6px; border: 1px solid transparent; max-width: 680px; }
.dotnet-banner.ok { background: var(--state-positive-soft); color: var(--state-positive); border-color: var(--state-positive-glow); }
.dotnet-banner.warn { background: var(--state-warning-soft); color: var(--state-warning); border-color: var(--state-warning-glow); }
.dotnet-banner b { font-weight: 700; }
</style>
