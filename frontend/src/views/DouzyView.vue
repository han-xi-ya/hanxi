<script setup lang="ts">
// 「Douzy 全能下载器」（多平台：抖音/TikTok/YouTube/Telegram/X）：仅版本管理与安装包下载——列远程版本、下载官方
// Windows 安装包（sha256+字节数+PE 魔数校验）、拉起上游安装向导、管理已下载包。
// 刻意无控制台 Tab / 状态灯 / 启停：本模块不托管进程（上游内测 + 壳闭源）。
import { ref, onMounted } from 'vue'
import * as DouzyAPI from '../../bindings/hanxi/internal/modules/douzy/douzyservice'
import type { DouzyRelease, DouzyVersionInfo } from '../../bindings/hanxi/internal/modules/douzy/version/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/douzy/version/models'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { useWailsEvent } from '../composables/useWailsEvent'
import { loadManagedVersions } from '../composables/loadManagedVersions'
import { useConfirm } from '../composables/useConfirm'
import { useClipboard } from '../composables/useClipboard'
import { fmtSize, fmtDate } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'

const releases = ref<DouzyRelease[]>([])
const installed = ref<DouzyVersionInfo[]>([])
const loading = ref(false)
const listError = ref('')
const busy = ref(false)

// 下载进度 map（按版本索引，与托管模块同构）
const downloading = ref<Record<string, DownloadProgress>>({})

const { showToast } = useToast()
const { confirm } = useConfirm()
const { copy } = useClipboard()

const repoUrl = ref('')

// ---------- 数据加载 ----------
async function loadVersions() {
  await loadManagedVersions({
    remote: DouzyAPI.ListReleases,
    local: DouzyAPI.ListInstalledVersions,
    // 本模块无「使用版本」概念（不托管运行），active 通道按空串桩接
    active: async () => '',
    setRemote: value => { releases.value = value },
    setLocal: value => { installed.value = value },
    setActive: () => {},
    setLoading: value => { loading.value = value },
    setError: value => { listError.value = value },
  })
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: DouzyRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  return installed.value.some(v => v.version === rel.version) ? 'installed' : 'idle'
}

// ---------- 下载 / 安装 / 目录操作 ----------
async function download(rel: DouzyRelease) {
  try {
    const res = await DouzyAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 的安装包已下载`)
      await loadVersions()
    }
  } catch (e) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
  }
}

async function launchInstaller(v: DouzyVersionInfo) {
  if (busy.value) return
  busy.value = true
  try {
    await DouzyAPI.LaunchInstaller(v.version)
    showToast('安装向导已弹出：后续 UAC 确认与安装步骤由你完成（Hanxi 不接管安装过程）')
  } catch (e) {
    showToast(`启动安装程序失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function openDir(dir: string) {
  try {
    await DouzyAPI.OpenDir(dir)
  } catch (e) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeInstaller(v: DouzyVersionInfo) {
  // 危险操作经全局确认框；文案诚实区分"删安装包"与"卸载程序"
  const accepted = await confirm({
    title: `确定删除 Douzy ${v.version} 安装包？`,
    description: '仅删除已下载的安装包文件，不会卸载已安装到系统的 Douzy 程序（卸载请到系统设置）。',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await DouzyAPI.RemoveVersion(v.version)
    showToast(`已删除安装包 ${v.version}`)
    await loadVersions()
  } catch (e) {
    showToast(`删除失败: ${getErrorMessage(e)}`)
  }
}

async function copyRepo() {
  const ok = await copy(repoUrl.value)
  showToast(ok ? '仓库地址已复制' : '复制失败')
}

async function openRepo() {
  try {
    await DouzyAPI.OpenRepository()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}

// ---------- 事件与生命周期 ----------
useWailsEvent<DownloadProgress>('douzy:version-download', (t) => {
  if (!t || !t.version) return
  downloading.value = { ...downloading.value, [t.version]: t }
  if (t.stage === 'done') {
    setTimeout(() => {
      const next = { ...downloading.value }
      delete next[t.version]
      downloading.value = next
    }, 800)
    loadVersions()
  }
})

onMounted(async () => {
  loadVersions()
  try {
    repoUrl.value = await DouzyAPI.RepositoryURL()
  } catch (e) {
    console.warn('douzy RepositoryURL failed:', getErrorMessage(e))
  }
})
</script>

<template>
  <section class="page douzy-view">
    <PageHeader title="Douzy 全能下载器" subtitle="多平台视频下载器 Douzy（抖音 / TikTok / YouTube / Telegram / X）的安装包版本管理：下载、校验与安装向导拉起（仅版本管理+下载，不接管程序运行）。" />

    <!-- 常驻诚实横幅：上游内测 + 边界声明 -->
    <UiBanner tone="warn" class="dz-slim">
      桌面版 Douzy 尚处<strong>上游内测期</strong>，功能与稳定性由上游项目负责；Hanxi 只负责安装包下载与哈希校验，不接管其运行，安装后使用风险自行承担。
    </UiBanner>

    <div v-if="listError" class="error-box">{{ listError }}</div>

    <div class="dz-panel">
      <div class="dz-meta">
        <span>已下载 <strong>{{ installed.length }}</strong> 个安装包 · 远程版本 {{ releases.length }} 个</span>
        <span class="dz-dim">安装包下载自 GitHub Releases 官方资产（sha256 + 字节数 + PE 魔数三重校验）；<strong>下载后需自行双击安装</strong>，或使用卡片上的「运行安装程序」。</span>
      </div>
      <div class="dz-btns">
        <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadVersions">
          {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
        </button>
      </div>
    </div>

    <!-- 已下载安装包 -->
    <div class="dz-section"><h3>已下载安装包 ({{ installed.length }})</h3></div>

    <div v-if="installed.length === 0" class="empty-state first-use">
      <p>尚未下载任何安装包 —— 下载最新版后双击安装即可使用</p>
      <button v-if="releases.length" class="btn btn-primary" @click="download(releases[0])">
        下载最新版 {{ releases[0].version }}
      </button>
      <button v-else-if="!loading" class="btn btn-secondary" @click="loadVersions">↻ 刷新远程列表</button>
    </div>

    <div class="dz-grid">
      <div v-for="v in installed" :key="v.version" class="dz-card">
        <div class="dz-card-top">
          <span class="dz-ver">{{ v.version }}</span>
          <span class="dz-badge downloaded">已下载</span>
        </div>
        <div class="dz-card-meta">
          <div class="dz-line"><span class="k">安装包</span><code class="mono">{{ v.exePath }}</code></div>
          <div class="dz-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 下载于 {{ v.installedAt }}</span></div>
        </div>
        <div class="dz-card-actions">
          <button class="btn btn-primary btn-small" :disabled="busy" title="拉起上游 NSIS 安装向导；UAC 与向导步骤由你完成" @click="launchInstaller(v)">▶ 运行安装程序</button>
          <button class="btn btn-secondary btn-small" @click="openDir(v.dir)">📂 打开位置</button>
          <button class="btn btn-danger-outline btn-small" @click="removeInstaller(v)">删除安装包</button>
        </div>
      </div>
    </div>

    <!-- 远程可用版本 -->
    <div class="dz-section"><h3>远程可用版本</h3></div>
    <div class="dz-table-wrap">
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
              <strong class="dz-ver-name">{{ rel.version }}</strong>
              <span v-if="rel.isPre" class="dz-badge pre">预发布</span>
            </td>
            <td>
              <!-- 类名带 dz- 前缀：全局原子有 .status-dot（7px），防碰撞压扁（markeron 事故教训） -->
              <span v-if="statusOf(rel) === 'installed'" class="dz-status installed">已下载</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="dz-status downloading">下载中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="dz-status error">失败</span>
              <span v-else class="dz-status idle">可下载</span>
            </td>
            <td>{{ fmtSize(rel.size) }}</td>
            <td>{{ fmtDate(rel.published) }}</td>
            <td>
              <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="dz-dl-cell">
                <div class="dz-bar-wrap">
                  <div class="dz-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
                </div>
                <span class="dz-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
              </div>
              <div v-else-if="statusOf(rel) === 'downloading'" class="dz-dl-meta">
                <span v-if="['verify', 'install'].includes(downloading[rel.version]!.stage)">校验落位中…</span>
              </div>
              <div v-else-if="statusOf(rel) === 'error'" class="dz-dl-meta">
                <span class="dz-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <button v-if="statusOf(rel) === 'idle'" class="btn btn-primary btn-small" @click="download(rel)">下载安装包</button>
              <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small">已下载</span>
              <a v-if="statusOf(rel) === 'error'" class="dz-retry" @click="download(rel)">重试</a>
            </td>
          </tr>
          <tr v-if="releases.length === 0 && !loading">
            <td colspan="5" class="dz-empty-hint">无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 上游信息 -->
    <div class="dz-repo">
      <span class="k">GitHub 仓库</span>
      <code class="mono dz-repo-addr">{{ repoUrl }}</code>
      <button class="link-button" @click="copyRepo">复制</button>
      <button class="link-button" @click="openRepo">浏览器打开</button>
    </div>
  </section>
</template>

<style scoped>
.douzy-view { display: flex; flex-direction: column; gap: 10px; }
/* UiBanner 紧凑变体 */
.dz-slim { padding: 8px 12px; font-size: var(--text-sm); }

.dz-panel {
  display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap;
  background: var(--surface-panel); border: 1px solid var(--color-border); padding: 10px 14px; border-radius: var(--radius-control);
}
.dz-meta { font-size: var(--text-base); color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.dz-meta strong { color: var(--color-text); }
.dz-dim { color: var(--color-text-subtle); }
.dz-btns { display: flex; gap: 8px; }

.dz-section h3 { font-size: var(--text-base); font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.dz-empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: var(--text-base); background: var(--surface-panel); border-radius: var(--radius-control); border: 1px dashed var(--color-border); }

/* ---------- 已下载卡片 ---------- */
.dz-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 12px; }
.dz-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px;
}
.dz-card-top { display: flex; justify-content: space-between; align-items: center; }
.dz-ver { font-family: var(--font-mono); font-size: var(--text-md); font-weight: 700; color: var(--color-text); }
.dz-badge { font-size: var(--text-xs); padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.dz-badge.downloaded { background: var(--state-positive-soft); color: var(--state-positive); }
.dz-badge.pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.dz-card-meta { display: flex; flex-direction: column; gap: 4px; font-size: var(--text-sm); }
.dz-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.dz-line .k { color: var(--color-text-subtle); width: 48px; flex-shrink: 0; }
/* .dz-mono 紧凑字号副本删除,落回 components.css 全局 .mono(sm) */
.dz-card-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; flex-wrap: wrap; }

/* ---------- 远程表格（.tbl 全局原子接管） ---------- */
.dz-table-wrap { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow-x: auto; }
.dz-ver-name { font-family: var(--font-mono); }

.dz-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.dz-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.dz-status.installed::before { background: var(--state-positive); }
.dz-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.dz-status.error::before { background: var(--state-danger); }
.dz-status.idle::before { background: var(--color-text-subtle); }

.dz-dl-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dz-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: var(--radius-pill); overflow: hidden; }
.dz-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
.dz-percent { font-size: var(--text-xs); color: var(--color-text-muted); width: 32px; text-align: right; }
.dz-dl-meta { font-size: var(--text-sm); color: var(--color-primary); }
.dz-error { color: var(--state-danger); font-size: var(--text-xs); }
.dz-retry { color: var(--color-primary); font-size: var(--text-sm); cursor: pointer; margin-left: 8px; }
.dz-retry:hover { text-decoration: underline; }

/* ---------- 上游信息 ---------- */
.dz-repo { display: flex; align-items: center; gap: 8px; font-size: var(--text-sm); color: var(--color-text-muted); flex-wrap: wrap; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 10px 14px; }
.dz-repo .k { color: var(--color-text-subtle); flex-shrink: 0; }
.dz-repo-addr { flex: 1; min-width: 220px; }
</style>
