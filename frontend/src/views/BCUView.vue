<script setup lang="ts">
// BCU 控制台（Wave 5 · 批 1 收敛件，波 2E 版本区回迁收口）：共享面全部进托管
// 控制台家族——adapter（src/adapters/bcu）承载业务投影（RPC/事件/文案/确认输入 +
// .NET 环境诊断态），useManagedConsole 单源状态轮询/uptime/下载进度 map/busy 闩编排，
// ManagedControlBar 管状态头与启停钮，ManagedExtrasCard 管联动辅助卡，
// ManagedVersionPanel 管版本区三段默认体（meta 行族/已装卡/首用空态——纯抄段
// 全数换面板，词面经 adapter.copy 与 #meta-extra 槽逐字承接）。
// 方言保留区（实证超纲，不硬套）：双变体下载表经面板 #remote-table 整表替换槽
// 留在本视图——每行「便携版/精简版」两把下载钮、version|variant 复合进度键、
// 按变体独立票行与变体前缀阶段词，超出面板「行→单键」的 progressKey/statusOf
// 判定能力（statusOf 仅在无票据时被调且签名不携 downloading map）。
import { computed, onMounted, ref } from 'vue'
import type { BCURelease } from '../../bindings/hanxi/internal/modules/bcu/version/models'
import { createBCUAdapter, variantLabel, variantRelease, type BCUVariant } from '../adapters/bcu'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import ManagedVersionDot from '../components/managed/ManagedVersionDot.vue'
import { stepOf } from '../components/managed/managedProgress'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import ElevateRestart from '../components/ElevateRestart.vue'
import { fmtSize, fmtDate } from '../utils/format'

const adapter = createBCUAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与各工具模块同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🧹 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- .NET 环境诊断态（波 2E 起收编 adapter 闭包，视图与 copy 词族同源直读） ----------
const { dotnetEnv, envLoading, recommendedVariant } = adapter

// 740 提权直拒（后端 elevateHint 文案统一含"管理员"）→ 追加一键提权重启入口
const needsElevate = computed(() => store.state === 'failed' && (store.snap?.error || '').includes('管理员'))

// ---------- 方言表格投影（复合进度键 version|variant，防同版本双变体互相覆盖） ----------
const releases = computed(() => store.releases as BCURelease[])
const installed = computed(() => store.installed)

// stepOf 已收编 managedProgress 单一来源（波 1a）；statusOf/statusOverall 系本视图
// 方言分叉（version|variant 复合键 + 双变体汇总），不并入公共四态内核，保留本地。
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

// ---------- 初始装载（状态首帧与版本区由 store 的 mounted 即触发；环境探测走 adapter） ----------
onMounted(() => {
  void adapter.loadDotnetEnv()
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

    <!-- 版本管理 Tab：面板接管三段默认体（meta 行族/已装卡/首用空态，波 2E）——
         .NET 环境徽标为动态诊断行走 #meta-extra 槽；双变体远程表超面板单键能力，
         经 #remote-table 整表替换槽留本视图方言原形（词面与判定零改动，
         bcu-ver-status 锚点类随波 2E 退役，spec 选择器走 .ver-status 标准形） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store">
        <template #meta-extra>
          <span v-if="envLoading" class="hint-dim">正在检测本机 .NET 桌面运行时…</span>
          <span v-else-if="dotnetEnv" class="dotnet-banner" :class="dotnetEnv.hasNet8 ? 'ok' : 'warn'">
            <template v-if="dotnetEnv.hasNet8">
              本机已装 .NET 8 桌面运行时（{{ dotnetEnv.desktopVersions?.join(' / ') }}）→ 推荐下载<b>精简版</b>（12MB，省约 60MB）
            </template>
            <template v-else>
              未检测到 .NET 8 桌面运行时 → 推荐下载<b>自包含便携版</b>（76MB，免依赖）。精简版安装后会无法启动
            </template>
          </span>
        </template>
        <template #remote-table>
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
                <!-- 状态圆点直挂 ManagedVersionDot 标准形（波 2E：bcu-ver-status
                     零样式锚点类退役，spec 选择器迁 .ver-status；词面逐字传现词） -->
                <ManagedVersionDot v-if="statusOverall(rel) === 'installed'" status="installed" text="已安装" />
                <ManagedVersionDot v-else-if="statusOverall(rel) === 'downloading'" status="downloading" text="下载中" />
                <ManagedVersionDot v-else-if="statusOverall(rel) === 'error'" status="error" text="失败" />
                <ManagedVersionDot v-else status="idle" text="可安装" />
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
        </template>
      </ManagedVersionPanel>
    </div>
  </section>
</template>

<style scoped>
/* 状态头/启停钮/提示条/引导行/联动卡与面板三段由 managed 共享件 + components.css
   全局原子接管；本页仅余 #remote-table/#meta-extra 槽内方言 DOM 的业务样式
   （槽内容编译于本视图作用域，scoped 照常命中）与两处对标准形的补差。 */
.bcu-view { display: flex; flex-direction: column; gap: 10px; }

/* 补差 against 全局原子（标准形为 radius-element / radius-pill，本视图控制条与
   版本胶囊为小圆角方片形制）：控制条/胶囊渲染进 ManagedControlBar 子件内部，
   用 :deep 穿层维持逐字同形（原 .bcu-status-light 复制体已由 .status-light 标准形替代） */
.bcu-view :deep(.control-bar) { border-radius: var(--radius-control); }
.bcu-view :deep(.ver-pill) { border-radius: 4px; }

/* ---------- 折叠说明（info-details/info-summary::after/info-body p 由全局原子接管） ---------- */
/* 补差 against 全局原子 .info-summary：本视图标题前带图标，加 4px 间距
   （.inline-link 两行 scoped 副本已删净，落回 components.css :where 全局原子） */
.info-summary { gap: 4px; }

/* 面板三段（meta/btn-group、区节标题/首用空态、installed-grid/inst-* /ver-tag）
   随波 2E 迁入 ManagedVersionPanel 子件，其徽标配色家族面板自带——本页删净；
   .badge 基形与 components.css 全局原子逐字同义，以下仅方言表「预发布」配色变体 */
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管 */

/* ---------- 双变体下载与 .NET 环境徽标（方言保留区：#remote-table/#meta-extra 槽） ---------- */
.variant-btns { display: flex; gap: 6px; align-items: center; }
.variant-progress { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.variant-progress .dl-meta-text { font-size: var(--text-xs); }
.dotnet-banner { font-size: var(--text-sm); padding: 4px 10px; border-radius: 6px; border: 1px solid transparent; max-width: 680px; }
.dotnet-banner.ok { background: var(--state-positive-soft); color: var(--state-positive); border-color: var(--state-positive-glow); }
.dotnet-banner.warn { background: var(--state-warning-soft); color: var(--state-warning); border-color: var(--state-warning-glow); }
.dotnet-banner b { font-weight: 700; }
</style>
