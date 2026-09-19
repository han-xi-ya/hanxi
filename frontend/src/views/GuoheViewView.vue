<script setup lang="ts">
// 果核看图控制台（Wave 5 · 批 1 收敛件，模式照抄 CCSwitchView）：共享面全部进
// 托管控制台家族——adapter（src/adapters/guoheview）承载业务投影（RPC/事件/文案），
// useManagedConsole 单源状态轮询/uptime/进度 map/busy 闩，ManagedControlBar 管
// 状态头与启停钮（多实例 OpenWindow 聚焦/唤回/另开三分支在 adapter.control.primary
// run 内部消化，UI 恒为一钮），ManagedExtrasCard 管随关与官网联动卡。
// 方言区：版本 Tab 为官方发布接口表（通道列/安装中文案/无发布时间列），
// 共享 ManagedVersionPanel 形态不符，照 everything 通道表先例留视图，
// 但数据与动作全部消费 store 单源。
import { computed, ref } from 'vue'
import { createGuoheViewAdapter } from '../adapters/guoheview'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import type { ManagedReleaseRecord, NormalizedProgress } from '../components/managed/adapter'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import { fmtSize } from '../utils/format'

const adapter = createGuoheViewAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/piclite 同构）
const activeMainTab = ref('console')
const MAIN_TABS = [
  { key: 'console', label: '🏞️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 方言表格投影 ----------
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

// ⑥收编：本模块下载失败现词「安装失败: 」经 adapter.copy.errorPrefix 回
// store 词源表，视图直调 adapter.versions.download 的绕行保词表自此退役——
// 方言表下载/重试全部走 store.runDownload。
const runningVersion = computed(() => store.runningVersion)
</script>

<template>
  <section class="page guoheview-view">
    <PageHeader title="果核看图" subtitle="托管极速 RAW 看图器 GuoheView：官方发布接口便携版安装（MD5 校验）、JobObject 启停与窗口唤起；浏览操作在 GuoheView 自有窗口完成（多实例：双击图片的窗口不受影响）。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/提示条/引导行由 ManagedControlBar 按 adapter 投影渲染 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" :banner-slim="false" />

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="summary-text">什么是果核看图</summary>
        <div class="info-body">
          <p>果核（ghxi.com）出品的 Windows 极速 RAW 图片查看器（闭源免费软件，Certum 数字签名）：首帧 0.1 秒、自研解码内核覆盖 ARW/CR3/NEF/DNG/HEIC/WebP/TIFF 等格式，分块加载大图、ICC 色彩管理，纯净无广告。</p>
          <p class="hint-dim">上游闭源、发布挂官方自建接口（非 GitHub）：每次仅提供当前版本（stable/beta），无历史归档，旧版本可用「导入本地」收纳；完整性以官方 MD5 + 字节数 + zip CRC + 布局自检四层兜底。应用自带更新检查，升级建议一律回这里走托管安装。</p>
        </div>
      </details>
    </div>

    <!-- 联动与官网设置卡（随关 + 官网行，条目文案在 adapter） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab（方言区：官方发布接口表，数据与动作全走 store） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ store.installed.length }}</strong> 个版本 · 官方当前发布 {{ store.releases.length }} 个</span>
          <span class="hint-dim">安装源为果核官方发布接口的 Windows x64 便携 zip（官方 MD5 + 字节数 + CRC + 布局四层校验），解压进隔离目录，不触碰系统</span>
          <span class="hint-dim">上游只发布当前版本、无历史列表；「导入本地」可把你机器上已有的便携目录（含设置）收纳进托管</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="store.runImport()" :disabled="store.busy">⇥ 导入本地目录</button>
          <button class="btn btn-secondary btn-small" :disabled="store.loading" @click="store.load()">
            {{ store.loading ? '刷新中…' : '↻ 刷新发布接口' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ store.installed.length }})</h3></div>

      <div v-if="store.installed.length === 0" class="empty-state first-use">
        <p>尚未安装果核看图 —— 下载官方当前版本，或「导入本地目录」把现有便携目录收纳进来</p>
        <button v-if="store.releases.length" class="btn btn-primary" @click="store.runDownload(store.releases[0])">
          安装最新版 {{ store.releases[0].version }}
        </button>
        <button v-else-if="!store.loading" class="btn btn-secondary" @click="store.load()">↻ 刷新发布接口</button>
      </div>

      <div class="installed-grid">
        <div v-for="v in store.installed" :key="v.version" class="installed-card" :class="{ 'card-active': store.activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <UiStatusChip v-if="store.activeVersion === v.version" tone="positive">使用中</UiStatusChip>
              <UiStatusChip v-else-if="store.state === 'running' && runningVersion === v.version" tone="information">运行中</UiStatusChip>
              <UiStatusChip v-if="v.isImport" tone="information">本地导入</UiStatusChip>
              <UiStatusChip v-else tone="neutral">官方便携</UiStatusChip>
            </div>
          </div>
          <div class="inst-meta">
            <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
            <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
            <div class="meta-line" v-if="v.isImport && v.source"><span class="k">来源</span><span class="hint-dim">{{ v.source }}</span></div>
          </div>
          <div class="inst-actions">
            <button v-if="store.activeVersion !== v.version" class="btn btn-primary btn-small" @click="store.runSetActive(v)">设为使用</button>
            <button class="btn btn-secondary btn-small" @click="store.runOpenDir(v)">📂 打开位置</button>
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="store.state === 'running' && runningVersion === v.version"
              :title="store.state === 'running' && runningVersion === v.version ? '请先退出托管实例' : ''"
              @click="store.runRemove(v)"
            >卸载</button>
          </div>
        </div>
      </div>

      <!-- 官方当前发布版本 -->
      <div class="section-title"><h3>官方当前发布</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 150px;">版本</th>
              <th style="width: 80px;">通道</th>
              <th style="width: 170px;">状态</th>
              <th style="width: 90px;">大小</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rel in store.releases" :key="rel.version">
              <td><strong class="ver-name">{{ rel.version }}</strong></td>
              <td>
                <UiStatusChip v-if="rel.isPre" tone="warning">beta</UiStatusChip>
                <UiStatusChip v-else tone="neutral">stable</UiStatusChip>
              </td>
              <td>
                <!-- 类名刻意用 gv- 前缀——防与全局原子碰撞压扁表格圆点（markeron 垂直字体事故教训） -->
                <span v-if="statusOf(rel) === 'installed'" class="gv-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="gv-ver-status downloading">安装中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="gv-ver-status error">失败</span>
                <span v-else class="gv-ver-status idle">可安装</span>
              </td>
              <td>{{ fmtSize(rel.size) }}</td>
              <td>
                <div v-if="statusOf(rel) === 'downloading' && store.downloading[rel.version]!.stage === 'downloading'" class="download-cell">
                  <div class="dl-bar-wrap">
                    <div class="dl-bar-inner" :style="{ width: `${stepOf(store.downloading[rel.version]!)}%` }"></div>
                  </div>
                  <span class="dl-percent">{{ stepOf(store.downloading[rel.version]!) }}%</span>
                </div>
                <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
                  <span v-if="store.downloading[rel.version]!.stage === 'verify'">MD5 校验…</span>
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
                <UiStatusChip v-if="statusOf(rel) === 'installed'" tone="positive">已安装</UiStatusChip>
                <button v-if="statusOf(rel) === 'error'" class="link-button" @click="store.runDownload(rel)">重试</button>
              </td>
            </tr>
            <tr v-if="store.releases.length === 0 && !store.loading">
              <td colspan="5" class="empty-hint">官方发布接口暂不可达——可稍后点击「↻ 刷新发布接口」重试，或「导入本地目录」安装你已有的便携版</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 页头/控制条/提示条/联动卡由 managed 组件 + components.css 全局原子接管
   （原 .gv-status-light 复制体已由 .status-light 标准形替代）；
   本页仅余方言版本 Tab 与私有形。 */
.guoheview-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* 补差 against 全局原子 .ver-pill：版本胶囊数字等宽（渲染在 ManagedControlBar
   子件内部，父级 scoped 穿不过去，:deep 维持逐字同形） */
.guoheview-view :deep(.ver-pill) { font-variant-numeric: tabular-nums; }

/* ---------- 提示与说明卡（hint-line 原 line-height:1.6 散差按标准形定档删除；
   info-details/info-body p 由全局原子接管；本视图折叠标题为自名 .summary-text，非原子选择器，留局部） ---------- */
.summary-text { padding: 7px 12px; font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-details summary::-webkit-details-marker { display: none; }
.summary-text::after { content: '▸'; font-size: var(--text-micro); margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .summary-text { border-bottom: 1px solid var(--color-border); }
.info-details[open] .summary-text::after { transform: rotate(90deg); }

/* 补差 against 全局原子 .control-panel：本视图面板加宽间距并允许换行 */
.control-panel { gap: 10px; flex-wrap: wrap; }
/* meta-info/btn-group、section-title h3/empty-hint 由全局原子接管 */

/* ---------- 已安装卡片（installed-grid/installed-card(.card-active) 由全局原子接管；
   本视图原 minmax(340px) 散差按标准形 minmax(320px) 定档删除） ---------- */
/* 补差 against 全局原子 .inst-card-top / .inst-badges / .inst-actions：窄卡允许换行 */
.inst-card-top { gap: 8px; flex-wrap: wrap; }
.inst-badges { flex-wrap: wrap; }
.inst-actions { flex-wrap: wrap; }
/* 补差 against 全局原子 .meta-line：窄列允许收缩 */
.meta-line { min-width: 0; }

/* ---------- 远程表格（table-container 由全局原子接管） ---------- */
/* 补差 against 全局原子 .ver-name：版本名字符串（等宽数字） */
.ver-name { font-variant-numeric: tabular-nums; }

.gv-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.gv-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.gv-ver-status.installed::before { background: var(--state-positive); }
.gv-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.gv-ver-status.error::before { background: var(--state-danger); }
.gv-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族由全局原子接管 */

/* ---------- 联动与官网设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管） ---------- */
</style>
