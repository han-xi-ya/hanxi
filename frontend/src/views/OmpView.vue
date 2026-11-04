<script setup lang="ts">
// 「Oh My Pi (omp)」纯 TUI 编码代理：仅版本管理——列远程版本、下载官方资产
// （sha256+字节数校验）、激活版本切换、导入本地 omp.exe。
// 刻意无控制台 Tab / 状态灯 / 启停 / 唤窗：本模块不托管进程，TUI 由用户在自己的
// 终端启动激活版本 exe。RPC 接线经 adapters/omp 收编（危险确认/路径输入闸门
// 在 adapter 内，回执词面单源 managedCommon），视图保持 DouzyView 同族自绘形制。
import { computed, ref, onMounted } from 'vue'
import type { OmpRelease, OmpVersionInfo } from '../../bindings/hanxi/internal/modules/omp/version/models'
import { createOmpAdapter } from '../adapters/omp'
import type { NormalizedProgress } from '../components/managed/adapter'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import { getErrorMessage } from '../utils/errors'
import { loadManagedVersions } from '../composables/loadManagedVersions'
import { fmtSize, fmtDate } from '../utils/format'
import ManagedVersionDot from '../components/managed/ManagedVersionDot.vue'
import { stepOf, versionRowState } from '../components/managed/managedProgress'
import { useDoneTicketRetire } from '../components/managed/useDoneTicketRetire'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'

const omp = createOmpAdapter()

const releases = ref<OmpRelease[]>([])
const installed = ref<OmpVersionInfo[]>([])
// GetStatus.activeVersion（显式设定值；空=未指定，后端回退"最新已装"——
// 逐条 active 标注由 ListInstalledVersions 给出，卡片高亮吃该布尔）。
const active = ref('')
const dataDir = ref('')
const loading = ref(false)
const listError = ref('')
const busy = ref(false)

// 下载进度 map（按版本索引，与托管模块同构；键归一见 adapters/omp 单键形接线）
const downloading = ref<Record<string, NormalizedProgress>>({})

const { showToast } = useToast()
const { copyWithToast } = useClipboard()

const repoUrl = ref('')

// ---------- 数据加载 ----------
async function loadVersions() {
  await loadManagedVersions({
    remote: omp.listReleases,
    local: omp.listInstalled,
    // 激活通道与页脚目录行同取 GetStatus 一次 RPC（无 douzy 的空串桩接——omp 有真激活语义）
    active: async () => {
      const status = await omp.getStatus()
      dataDir.value = status?.dataDir ?? ''
      return status?.activeVersion ?? ''
    },
    setRemote: value => { releases.value = value },
    setLocal: value => { installed.value = value },
    setActive: value => { active.value = value },
    setLoading: value => { loading.value = value },
    setError: value => { listError.value = value },
  })
}

// stepOf/四态判定单一来源在 managedProgress，本视图只转发。
function statusOf(rel: OmpRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  return versionRowState(downloading.value[rel.version], installed.value, rel.version)
}

// 激活高亮基准：ListInstalledVersions 逐条 active（后端已含"store 空=
// 自动最新已装"的标注口径，前端不复算回退逻辑）。
function isActive(v: OmpVersionInfo): boolean {
  return !!v.active
}

// 页头激活位：显式设定直出；未设定但列表有自动激活标注时如实注记。
const activeLabel = computed(() => {
  if (active.value) return active.value
  const fallback = installed.value.find((v) => v.active)
  return fallback ? `${fallback.version}（自动最新）` : '未激活'
})

// source 词表（bindings 实册 remote|imported；词表外值如实透传，后端扩词不静默吞标）
function sourceWord(source?: string): string {
  if (source === 'imported') return '本地导入'
  if (source === 'remote') return '官方下载'
  return source ?? ''
}

// ---------- 版本动作（危险确认/输入闸在 adapter，本层只落 toast 与复刷） ----------
async function download(rel: OmpRelease) {
  try {
    const out = await omp.download(rel.version)
    if (out.message) showToast(out.message)
    if (out.reloadVersions) await loadVersions()
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function setActiveVersion(v: OmpVersionInfo) {
  if (busy.value) return
  busy.value = true
  try {
    const out = await omp.setActive(v.version)
    if (out.message) showToast(out.message)
    await loadVersions()
  } catch (e) {
    showToast(`激活失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function importLocal() {
  if (busy.value) return
  busy.value = true
  try {
    const out = await omp.importLocal() // 取消/空白串回 {}：静默零播报
    if (out.message) showToast(out.message)
    if (out.reloadVersions) await loadVersions()
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function removeVersion(v: OmpVersionInfo) {
  if (busy.value) return
  busy.value = true
  try {
    const out = await omp.remove(v.version) // 确认框在 adapter；否决回 {}
    if (out.message) showToast(out.message)
    if (out.reloadVersions) await loadVersions()
  } catch (e) {
    showToast(`删除失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function openDir(dir: string) {
  try {
    await omp.openDir(dir)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function copyRepo() {
  await copyWithToast(repoUrl.value, '仓库地址已复制')
}

async function openRepo() {
  try {
    await omp.openRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件与生命周期 ----------
// done 票据驻留摘除编排照 douzy 走 managed/useDoneTicketRetire（track 形制：
// 卸载统一清尽迟到清票定时器）；形制取 DouzyView 现值，摘除不复核。
const scheduleDoneRetire = useDoneTicketRetire({ track: true })

omp.subscribeProgress((t) => {
  downloading.value = { ...downloading.value, [t.key]: t }
  if (t.stage === 'done') {
    scheduleDoneRetire(() => {
      const next = { ...downloading.value }
      delete next[t.key]
      downloading.value = next
    })
    loadVersions()
  }
})

onMounted(async () => {
  loadVersions()
  try {
    repoUrl.value = await omp.repositoryURL()
  } catch (e) {
    console.warn('omp RepositoryURL failed:', getErrorMessage(e))
  }
})
</script>

<template>
  <section class="page omp-view">
    <PageHeader title="Oh My Pi (omp)" subtitle="纯 TUI 编码代理 omp 的版本管理：远程列表、下载校验、激活切换与本地 exe 导入（仅版本管理，不托管进程运行）。" />

    <!-- 常驻诚实横幅：TUI 语义与运行边界声明 -->
    <UiBanner tone="warn" class="omp-slim">
      omp 是<strong>纯 TUI</strong> 编码代理，Hanxi 只做版本管理（下载校验 / 激活切换 / 导入本地），不托管其运行——请在终端中启动「激活中」版本的 omp.exe（exe 路径见对应版本卡片）。
    </UiBanner>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <div class="omp-panel">
      <div class="omp-meta">
        <span>已装 <strong>{{ installed.length }}</strong> 个版本 · 远程版本 {{ releases.length }} 个 · 当前激活 <strong>{{ activeLabel }}</strong></span>
        <span class="omp-dim">版本包下载自 GitHub Releases 官方资产（sha256 + 字节数校验）；「设为激活」只切换托管指向，<strong>不影响正在运行的终端会话</strong>；已有 omp.exe 可用「导入本地 exe」收纳进版本区。omp 的配置 / 会话 / 记忆库在其数据根（见页脚目录行），与版本目录分离。</span>
      </div>
      <div class="omp-btns">
        <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
          {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
        </button>
        <button class="btn btn-secondary btn-small" :disabled="busy" title="把本机已有的 omp.exe（路径或所在目录）校验后登记进版本区" @click="importLocal">⇥ 导入本地 exe</button>
      </div>
    </div>

    <!-- 已安装版本 -->
    <div class="omp-section"><h3>已安装版本 ({{ installed.length }})</h3></div>

    <div v-if="installed.length === 0" class="empty-state first-use">
      <p>尚未安装任何版本 —— 下载最新版，或「导入本地 exe」把已有 omp.exe 收纳进来</p>
      <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
        下载最新版 {{ releases[0].version }}
      </button>
      <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
    </div>

    <div class="omp-grid">
      <div v-for="v in installed" :key="v.version" :class="['omp-card', { 'omp-card-active': isActive(v) }]">
        <div class="omp-card-top">
          <span class="omp-ver">{{ v.version }}</span>
          <span v-if="isActive(v)" class="omp-badge active">激活中</span>
          <span v-else class="omp-badge installed">已安装</span>
        </div>
        <div class="omp-card-meta">
          <div class="omp-line"><span class="k">exe</span><code class="mono">{{ v.exePath }}</code></div>
          <div class="omp-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
          <div v-if="v.source" class="omp-line"><span class="k">来源</span><span>{{ sourceWord(v.source) }}</span></div>
        </div>
        <div class="omp-card-actions">
          <button v-if="!isActive(v)" class="btn btn-primary btn-small" :disabled="busy" title="切换该版本的激活指向（目标须在版本树且 exe 可用）；已在终端运行的进程不受影响" @click="setActiveVersion(v)">设为激活</button>
          <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 打开位置</button>
          <button class="btn btn-danger-outline btn-small" @click="removeVersion(v)">删除</button>
        </div>
      </div>
    </div>

    <!-- 远程可用版本 -->
    <div class="omp-section"><h3>远程可用版本</h3></div>
    <div class="omp-table-wrap">
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
              <strong class="omp-ver-name">{{ rel.version }}</strong>
              <span v-if="rel.isPre" class="omp-badge pre">预发布</span>
            </td>
            <td>
              <!-- omp- 前缀锚点类保留位；圆点形制由 ManagedVersionDot 自带 scoped 标准形承担，
                   本模块词面「已安装/可下载」逐字直传 -->
              <ManagedVersionDot v-if="statusOf(rel) === 'installed'" class="omp-status" status="installed" text="已安装" />
              <ManagedVersionDot v-else-if="statusOf(rel) === 'downloading'" class="omp-status" status="downloading" text="下载中" />
              <ManagedVersionDot v-else-if="statusOf(rel) === 'error'" class="omp-status" status="error" text="失败" />
              <ManagedVersionDot v-else class="omp-status" status="idle" text="可下载" />
            </td>
            <td>{{ fmtSize(rel.size) }}</td>
            <td>{{ fmtDate(rel.published) }}</td>
            <td>
              <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="omp-dl-cell">
                <div class="omp-bar-wrap">
                  <div class="omp-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
                </div>
                <span class="omp-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
              </div>
              <div v-else-if="statusOf(rel) === 'downloading'" class="omp-dl-meta">
                <!-- 阶段词表实册 downloading/verify/extract/done/error（bindings 生成物），无 install 档 -->
                <span v-if="['verify', 'extract'].includes(downloading[rel.version]!.stage)">校验落位中…</span>
              </div>
              <div v-else-if="statusOf(rel) === 'error'" class="omp-dl-meta">
                <span class="omp-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <button v-if="statusOf(rel) === 'idle'" class="btn btn-primary btn-small" @click="download(rel)">下载</button>
              <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small">已安装</span>
              <a v-if="statusOf(rel) === 'error'" class="omp-retry" @click="download(rel)">重试</a>
            </td>
          </tr>
          <tr v-if="releases.length === 0 && !loading">
            <td colspan="5" class="omp-empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 上游信息（DouzyView 同位行） -->
    <div class="omp-repo">
      <span class="k">GitHub 仓库</span>
      <code class="mono omp-repo-addr">{{ repoUrl }}</code>
      <button class="link-button" @click="copyRepo">复制</button>
      <button class="link-button" @click="openRepo">浏览器打开</button>
    </div>

    <!-- omp 数据根（GetStatus.dataDir=~/.omp 直读，OpenDir 仅目录语义；配置/凭据/会话/记忆库所在） -->
    <div class="omp-repo">
      <span class="k">omp 数据目录</span>
      <code class="mono omp-repo-addr">{{ dataDir || '数据目录未知（状态读取失败，请点上方刷新）' }}</code>
      <button class="link-button" :disabled="!dataDir" title="打开 ~/.omp（配置、凭据、会话与记忆库所在，跨版本共用）" @click="openDir(dataDir)">打开</button>
    </div>
  </section>
</template>

<style scoped>
.omp-view { display: flex; flex-direction: column; gap: 10px; }
/* UiBanner 紧凑变体（词面同族纪律：自定义类一律 omp- 前缀，防全局压扁事故） */
.omp-slim { padding: 8px 12px; font-size: var(--text-sm); }

.omp-panel {
  display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
}
.omp-meta { font-size: var(--text-base); color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.omp-meta strong { color: var(--color-text); }
.omp-dim { color: var(--color-text-subtle); }
.omp-btns { display: flex; gap: 8px; }

.omp-section h3 { font-size: var(--text-base); font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.omp-empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: var(--text-base); background: var(--surface-panel); border-radius: var(--radius-control); border: 1px dashed var(--color-border); }

/* ---------- 已装版本卡片 ---------- */
.omp-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.omp-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px;
}
/* 激活卡高亮：徽标走家族正色档（与共享面板 badge-active 同语义），边框随色 */
.omp-card-active { border-color: var(--state-positive); }
.omp-card-top { display: flex; justify-content: space-between; align-items: center; }
.omp-ver { font-family: var(--font-mono); font-size: var(--text-md); font-weight: 700; color: var(--color-text); }
.omp-badge { font-size: var(--text-xs); padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.omp-badge.active { background: var(--state-positive-soft); color: var(--state-positive); font-weight: 600; }
.omp-badge.installed { background: var(--surface-hover); color: var(--color-text-muted); }
.omp-badge.pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.omp-card-meta { display: flex; flex-direction: column; gap: 4px; font-size: var(--text-sm); }
.omp-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.omp-line .k { color: var(--color-text-subtle); width: 48px; flex-shrink: 0; }
.omp-card-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; flex-wrap: wrap; }

/* ---------- 远程表格（.tbl 全局原子接管） ---------- */
.omp-table-wrap { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.omp-ver-name { font-family: var(--font-mono); }

/* 状态圆点形制由 ManagedVersionDot 自带 scoped 标准形承担，.omp-status 类仅存模板锚点位 */

.omp-dl-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.omp-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: var(--radius-pill); overflow: hidden; }
.omp-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
.omp-percent { font-size: var(--text-xs); color: var(--color-text-muted); width: 32px; text-align: right; }
.omp-dl-meta { font-size: var(--text-sm); color: var(--color-primary); }
.omp-error { color: var(--state-danger); font-size: var(--text-xs); }
.omp-retry { color: var(--color-primary); font-size: var(--text-sm); cursor: pointer; margin-left: 8px; }
.omp-retry:hover { text-decoration: underline; }

/* ---------- 页脚信息行（上游仓库 / omp 数据根共用形制） ---------- */
.omp-repo { display: flex; align-items: center; gap: 8px; font-size: var(--text-sm); color: var(--color-text-muted); flex-wrap: wrap; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 10px 14px; }
.omp-repo .k { color: var(--color-text-subtle); flex-shrink: 0; }
.omp-repo-addr { flex: 1; min-width: 220px; }
</style>
