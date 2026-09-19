<script setup lang="ts">
// Rufus 控制台（Wave 5 · 批 1 收敛件，模式照抄 CCSwitchView）：共享面全部进托管
// 控制台家族——adapter（src/adapters/rufus）承载业务投影（RPC/事件/文案/确认输入），
// useManagedConsole 单源状态轮询/uptime/进度 map/busy 闩，ManagedControlBar 管
// 状态头与启停钮，ManagedExtrasCard 管随关与仓库联动卡。Rufus 是一次性工具
// （写完盘即关窗）：不声明桌面快捷方式等自启类条目，缺项由联动卡自动缺席。
// 方言区：单文件版阶段词表含 install 不含 extract（「校验落位…」非共享面板的
// 「校验解压安装…」），表格徽标用 UiStatusChip 系——版本 Tab 表体留视图，
// 数据与动作全部消费 store 单源；extras 卡的「打开位置」钮经 #extras-action
// 具名位注入（title/disabled 依赖已装清单动态解析，静态 dataDir 条目表达不下）。
import { computed, ref } from 'vue'
import { createRufusAdapter } from '../adapters/rufus'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import type { ManagedReleaseRecord, NormalizedProgress } from '../components/managed/adapter'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import ElevateRestart from '../components/ElevateRestart.vue'
import { fmtSize, fmtDate } from '../utils/format'

const adapter = createRufusAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/litemonitor 同构）
const activeMainTab = ref<string>('console')
const MAIN_TABS = [
  { key: 'console', label: '⚡ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = store.state === 'running' && store.runningVersion ? store.runningVersion : store.activeVersion
  return store.installed.find((v) => v.version === prefer) ?? store.installed[0] ?? null
})

// 740 提权直拒（后端 elevateHint 文案统一含"管理员"）→ 追加一键提权重启入口
const needsElevate = computed(() => store.state === 'failed' && (store.snap?.error || '').includes('管理员'))

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
</script>

<template>
  <section class="page rufus-view">
    <PageHeader title="Rufus" subtitle="托管 USB 启动盘制作工具 Rufus：版本管理、JobObject 启停与窗口唤起。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/提示条/引导行由 ManagedControlBar 按 adapter 投影渲染 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" />
      <ElevateRestart v-if="needsElevate" route="/ext/rufus" />

      <!-- 说明卡（折叠） -->
      <details class="info-details">
        <summary class="info-summary">关于 Rufus</summary>
        <div class="info-body">
        <p>
          Rufus 是老牌 USB 启动盘制作工具（格式化 U 盘、写入 Windows/Linux/PE 镜像，支持大量奇技淫巧），
          上游 <a class="inline-link" href="https://github.com/pbatard/rufus" target="_blank" rel="noopener">pbatard/rufus</a>（C/Win32 原生，GPL-3.0）。
          本模块仅托管其运行：版本下载自官方 GitHub Releases（官方 digest sha256 + 字节数 + MZ 魔数三重校验），启停受 JobObject 管控。
        </p>
        <p class="hint-dim">
          便携版与安装版是同一个二进制（上游实证同哈希），Hanxi 收纳为单 rufus.exe 隔离目录；首次托管启动时自动预置 rufus.ini
          ——全部设置保存在版本目录内（零注册表污染），并顺带关闭其内置更新检查（版本升级走本页「版本管理」）。
          卸载版本会把该目录连同 rufus.ini 设置一并删除。
        </p>
        <p class="hint-dim">
          磁盘级写入不可逆：Rufus 界面内的所有确认对话框（分区方案、覆盖警告）请逐条阅读；写入过程中不要点「退出」或关闭 Hanxi。
        </p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（控制台与版本 Tab 均可见；随关与仓库行在 adapter，
         「打开位置」为清单驱动的动态钮，经具名位注入） -->
    <ManagedExtrasCard :adapter="adapter">
      <template #extras-action>
        <div class="extras-btns">
          <button
            class="btn btn-secondary btn-small"
            :disabled="!openDirVersion"
            :title="openDirVersion ? `打开 ${openDirVersion.version} 的便携目录（rufus.exe 与 rufus.ini 所在处）` : '尚未安装任何版本'"
            @click="openDirVersion && store.runOpenDir(openDirVersion)"
          >📂 打开位置</button>
        </div>
      </template>
    </ManagedExtrasCard>

    <!-- 版本管理 Tab（方言区：单文件版「校验落位…」表，数据与动作全走 store） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="control-panel">
        <div class="meta-info">
          <span>已安装 <strong>{{ store.installed.length }}</strong> 个版本 · 远程版本 {{ store.releases.length }} 个</span>
          <span class="hint-dim">便携单文件下载自 GitHub Releases（rufus-X.Yp.exe，官方 digest 校验）；或「导入本地」把你机器上已有的便携 exe 收纳进来（随行 rufus.ini 一并迁入）</span>
        </div>
        <div class="btn-group">
          <button class="btn btn-secondary btn-small" @click="store.runImport()" :disabled="store.busy">⇥ 导入本地 exe</button>
          <button class="btn btn-secondary btn-small" :disabled="store.loading" @click="store.load()">
            {{ store.loading ? '刷新中…' : '↻ 刷新远程列表' }}
          </button>
        </div>
      </div>

      <!-- 已安装版本 -->
      <div class="section-title"><h3>已安装版本 ({{ store.installed.length }})</h3></div>

      <div v-if="store.installed.length === 0" class="empty-state first-use">
        <p>尚未安装 Rufus —— 下载官方便携单文件，或「导入本地 exe」把现有便携文件收纳进来</p>
        <button v-if="store.releases.length" class="btn btn-primary" @click="store.runDownload(store.releases[0])">
          下载最新版 {{ store.releases[0].version }}
        </button>
        <button v-else-if="!store.loading" class="btn btn-secondary" @click="store.load()">↻ 刷新远程列表</button>
      </div>

      <div class="installed-grid">
        <div v-for="v in store.installed" :key="v.version" class="installed-card" :class="{ 'card-active': store.activeVersion === v.version }">
          <div class="inst-card-top">
            <span class="ver-tag">{{ v.version }}</span>
            <div class="inst-badges">
              <UiStatusChip v-if="store.activeVersion === v.version" tone="positive">使用中</UiStatusChip>
              <UiStatusChip v-else-if="store.state === 'running' && store.runningVersion === v.version" tone="information">运行中</UiStatusChip>
              <UiStatusChip v-if="v.isImport" tone="information">本地导入</UiStatusChip>
              <UiStatusChip v-else tone="neutral">官方下载</UiStatusChip>
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
              :disabled="store.state === 'running' && store.runningVersion === v.version"
              :title="store.state === 'running' && store.runningVersion === v.version ? '请先退出 Rufus' : ''"
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
                <span v-if="statusOf(rel) === 'installed'" class="rf-ver-status installed">已安装</span>
                <span v-else-if="statusOf(rel) === 'downloading'" class="rf-ver-status downloading">下载中</span>
                <span v-else-if="statusOf(rel) === 'error'" class="rf-ver-status error">失败</span>
                <span v-else class="rf-ver-status idle">可安装</span>
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
                  <span v-if="['resolve', 'verify', 'install'].includes(store.downloading[rel.version]!.stage)">校验落位…</span>
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
/* 页头/状态头/提示条/联动卡由 managed 组件 + components.css 全局原子接管
   （原 .rf-status-light 复制体已由 .status-light 标准形替代），此处只保留本视图业务样式。 */
.rufus-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 提示与说明卡（.banner.slim 由全局原子接管；hint-line 标准形定档；
   info-details/info-summary/info-body p 等由全局原子接管） ---------- */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* control-panel/meta-info/btn-group、section-title h3/empty-hint、installed-grid/
   installed-card(.card-active)/inst-*、table-container 由全局原子接管 */

/* ---------- 远程表格 ---------- */
.ver-name + .chip { margin-left: 4px; }

.rf-ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.rf-ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.rf-ver-status.installed::before { background: var(--state-positive); }
.rf-ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.rf-ver-status.error::before { background: var(--state-danger); }
.rf-ver-status.idle::before { background: var(--color-text-subtle); }

/* download-cell/dl-* 家族与 retry-link(:hover) 由全局原子接管 */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管） ---------- */
.extras-btns { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
