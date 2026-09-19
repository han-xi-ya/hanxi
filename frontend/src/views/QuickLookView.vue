<script setup lang="ts">
// QuickLook 空格预览托管工作台（Wave 5 · 批 0 共享契约迁入件）：
// adapter（src/adapters/quicklook）承载 RPC/事件/文案；控制台壳位三段共用共享件——
// ManagedControlBar（状态头 + 启停钮 + banner/hint 投影，slim 档现状保持）、
// ManagedExtrasCard（随关 + 仓库行）；store（useManagedConsole）单源接管
// 快照轮询/事件订阅/进度 map/busy 闩/版本区加载。
// 本模块两处契约形状之外，按"共享件零模块知识"纪律留视图：
//  1. 三钮序「启动托管 · 重载配置 · 退出」——中间的重载钮经 ManagedControlBar 的
//     #primary-action 具名槽自绘，动词取 adapter.reset（扩展槽，视图侧接线，busy 共用 store）；
//  2. 版本 Tab 为方言表（安装源=便携 zip 解压，文案位「导入本地便携目录」「安装最新版」
//     「官方 zip」「安装中」「哈希校验…/解压安装…」超出 ManagedVersionPanel 通用形），
//     DOM 逐字保留现状，数据与动作全部走共享 store。
import { ref } from 'vue'
import { createQuickLookAdapter } from '../adapters/quicklook'
import { useManagedConsole } from '../components/managed/store'
import type { ManagedReleaseRecord, ManagedVersionRecord, NormalizedProgress } from '../components/managed/adapter'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { fmtSize, fmtDate } from '../utils/format'

const adapter = createQuickLookAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理
const activeMainTab = ref<string>('console')
const MAIN_TABS = [
  { key: 'console', label: '👁️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 方言版本表投影（数据全部来自共享 store） ----------
function stepOf(p: NormalizedProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: ManagedReleaseRecord): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = store.downloading[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = store.installed.find((v) => v.version === rel.version)
  return hit ? 'installed' : 'idle'
}

const isActive = (v: ManagedVersionRecord) => !!store.activeVersion && store.activeVersion === v.version
const isRunning = (v: ManagedVersionRecord) => store.state === 'running' && store.runningVersion === v.version
const removeTitle = (v: ManagedVersionRecord) => (isRunning(v) ? '请先退出 QuickLook' : '')

// ---------- 重载配置（adapter.reset 扩展槽的视图侧接线：共享件批 0 不消费该槽） ----------
async function runReload(): Promise<void> {
  const reset = adapter.reset
  if (!reset) return
  await store.runExclusive(async () => {
    try {
      const res = await reset.run()
      if (res?.message !== undefined) showToast(res.message)
    } catch (e) {
      showToast(`重载失败: ${getErrorMessage(e)}`)
    }
  })
}

</script>

<template>
  <section class="page quicklook-view">
    <PageHeader
      title="QuickLook 预览"
      subtitle="托管开源空格秒预览工具 QuickLook：官方便携 zip 解压安装、JobObject 启停、命名管道优雅退出与运行状态探测；样式设置在其托盘菜单完成。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头与提示条走共享控制条（banner slim 档现状保持） -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store">
        <!-- 三钮序不变：主钮（声明位）→ 重载（本槽自绘）→ 退出（声明位恒居末） -->
        <template #primary-action="{ state, busy }">
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || state !== 'running'"
            :title="state === 'running' ? '请求运行中的实例重载配置（命名管道 Reload）' : '仅运行中可重载'"
            @click="runReload"
          >↻ 重载配置</button>
        </template>
      </ManagedControlBar>

      <!-- 说明卡（可折叠，文案逐字保留） -->
      <details class="info-details">
        <summary class="info-summary">什么是 QuickLook</summary>
        <div class="info-body">
          <p>开源的 Windows 版 macOS「快速查看」工具（<a class="inline-link" href="https://github.com/QL-Win/QuickLook" target="_blank" rel="noopener">QL-Win/QuickLook</a>，GPL-3.0，.NET WPF）：在资源管理器、桌面、通用文件对话框乃至 Directory Opus / Everything 等第三方文件管理器中选中文件按 <kbd>空格</kbd>，即时预览图片、文档、压缩包、代码、音视频等，无需真正打开应用。</p>
          <p class="hint-dim">「按空格」能力由其主进程内的全局低级键盘钩子实现（非注入资源管理器），故进程终止即钩子自动摘除、系统零残渣；托管安装走官方便携 zip 解压（免安装、不写注册表、不提权），启停受 JobObject 管控。设置窗口无程序化唤起入口（托盘图标左键即弹菜单），因此本控制台不设「打开窗口」按钮；退出优先经命名管道投递 Quit 优雅收尾，宽限后 JobObject 强杀兜底。默认随 Hanxi 退出，可在下方改为独立常驻。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关 + 仓库行；本模块无快捷方式/数据目录，自动缺席） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab：方言表（zip 安装词形超出 ManagedVersionPanel），数据/动作走共享 store -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ store.installed.length }}</strong> 个版本 · 远程版本 {{ store.releases.length }} 个</span>
          <span class="hint-dim">安装源为官方 GitHub Releases 的便携 zip（官方 digest sha256 四层校验），保布局解压落进隔离目录，不触碰系统</span>
          <span class="hint-dim">「导入本地」可把你机器上手动解压的 QuickLook 便携目录整套收纳进托管</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="store.runImport()" :disabled="store.busy">⇥ 导入本地便携目录</button>
          <button class="btn btn-secondary btn-small" :disabled="store.loading" @click="store.load()">
            {{ store.loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ store.installed.length }})</h3></div>

      <div v-if="store.installed.length === 0" class="empty-state first-use">
        <p>尚未安装 QuickLook —— 下载官方便携 zip 解压安装，或「导入本地便携目录」把现有解压目录收纳进来</p>
        <button v-if="store.releases.length" class="btn btn-primary" @click="store.runDownload(store.releases[0])">
          安装最新版 {{ store.releases[0].version }}
        </button>
        <button v-else-if="!store.loading" class="btn btn-secondary" @click="store.load()">↻ 刷新远程列表</button>
      </div>

      <div class="installed-grid">
        <div v-for="v in store.installed" :key="v.version" class="installed-card" :class="{ 'card-active': isActive(v) }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <UiStatusChip v-if="isActive(v)" tone="positive">使用中</UiStatusChip>
              <UiStatusChip v-else-if="isRunning(v)" tone="information">运行中</UiStatusChip>
              <UiStatusChip v-if="v.isImport" tone="information">本地导入</UiStatusChip>
              <UiStatusChip v-else tone="neutral">官方 zip</UiStatusChip>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
            <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
            <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
          </div>
          <div class="inst-actions">
            <button v-if="!isActive(v)" class="btn btn-primary btn-small" @click="store.runSetActive(v)">设为使用</button>
            <button class="btn btn-secondary btn-small" @click="store.runOpenDir(v)">📂 打开位置</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="isRunning(v)"
              :title="removeTitle(v)"
              @click="store.runRemove(v)"
            >卸载</button>
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
            <tr v-for="rel in store.releases" :key="rel.version">
              <td>
                <strong class="ver-name">{{ rel.version }}</strong>
                <UiStatusChip v-if="rel.isPre" tone="warning">预发布</UiStatusChip>
              </td>
              <td>
                <span v-if="statusOf(rel) === 'installed'" class="ql-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="ql-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="ql-ver-status error">失败</span>
                <span v-else class="ql-ver-status idle">可安装</span>
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
                  <span v-if="store.downloading[rel.version]!.stage === 'verify'">哈希校验…</span>
                  <span v-else-if="store.downloading[rel.version]!.stage === 'extract'">解压安装…</span>
                  <span v-else class="dl-error" :title="store.downloading[rel.version]!.message">{{ store.downloading[rel.version]!.message }}</span>
                </div>
                <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                  <span class="dl-error" :title="store.downloading[rel.version]!.message">{{ store.downloading[rel.version]!.message }}</span>
                </div>
                <button
                  v-if="statusOf(rel) === 'idle'"
                  class="btn btn-primary btn-small"
                  @click="store.runDownload(rel)"
                >下载安装</button>
                <UiStatusChip v-if="statusOf(rel) === 'installed'" tone="information">已安装</UiStatusChip>
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
/* 控制条四件套/联动卡/banner/hint-line 由 managed 共享件 + components.css 全局原子接管；
   本页保留：页级 flex 骨架、方言版本表私有形、说明卡内联形。 */
.quicklook-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 提示与说明卡（hint-line/info-details/info-summary/info-body p 等由全局原子接管） ---------- */
.info-body kbd { font-family: var(--font-mono); font-size: var(--text-xs); background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 0 4px; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* 补差 against 全局原子 .control-panel：本视图面板加宽间距并允许换行 */
.control-panel { gap: 10px; flex-wrap: wrap; }
/* meta-info/btn-group、section-title h3/empty-hint 由全局原子接管 */

/* ---------- 已安装卡片（installed-grid/installed-card(.card-active)/inst-card-top/inst-badges/
   inst-meta/meta-line .k/inst-actions/ver-tag 由全局原子接管） ---------- */
/* 补差 against 全局原子 .meta-line：窄列允许收缩 */
.meta-line { min-width: 0; }

/* ---------- 远程表格（table-container/ver-name 由全局原子接管） ---------- */
.ver-name + .chip { margin-left: 4px; }

.ql-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.ql-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ql-ver-status.installed::before { background: var(--state-positive); }
.ql-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ql-ver-status.error::before { background: var(--state-danger); }
.ql-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管 */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管） ---------- */
</style>
