<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { Events } from '@wailsio/runtime'
import * as FrpcAPI from '../../bindings/hanxi/internal/modules/frpc/frpcservice'
import type { FrpRelease, FrpVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/frpc/version/models'

const releases = ref<FrpRelease[]>([])
const installed = ref<FrpVersionInfo[]>([])
const loading = ref(false)
const errorMsg = ref('')
const toastMsg = ref('')

// 下载进度 map: version -> progress
const downloading = ref<Record<string, DownloadProgress>>({})
const importing = ref(false)

let unlisten: (() => void) | null = null

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

async function loadAll() {
  loading.value = true
  errorMsg.value = ''
  try {
    const [remote, local] = await Promise.all([
      FrpcAPI.ListReleases(),
      FrpcAPI.ListInstalledVersions(),
    ])
    releases.value = remote ?? []
    installed.value = local ?? []
  } catch (e: any) {
    errorMsg.value = `获取版本列表失败: ${e?.message ?? e}`
  } finally {
    loading.value = false
  }
}

async function refreshRemote() {
  loading.value = true
  try {
    releases.value = (await FrpcAPI.ListReleases()) ?? []
  } catch (e: any) {
    errorMsg.value = `刷新远程列表失败: ${e?.message ?? e}`
  } finally {
    loading.value = false
  }
}

function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024).toFixed(0)} KB`
}

function fmtDate(s?: string): string {
  if (!s) return '—'
  return s.slice(0, 10)
}

function stepOf(p: DownloadProgress): number {
  if (p.stage === 'done') return 100
  if (p.stage !== 'downloading') return 0
  if (!p.total) return 0
  return Math.min(99, Math.round((p.done / p.total) * 100))
}

function statusOf(rel: FrpRelease): 'installed' | 'downloading' | 'error' | 'idle' {
  const p = downloading.value[rel.version]
  if (p) return p.stage === 'error' ? 'error' : 'downloading'
  const hit = installed.value.find(v => v.version.replace(/^v/, '') === rel.version.replace(/^v/, ''))
  return hit ? 'installed' : 'idle'
}

async function download(rel: FrpRelease) {
  try {
    const res = await FrpcAPI.DownloadVersion(rel.version)
    if (res === 'already-installed') {
      showToast(`版本 ${rel.version} 已安装`)
      await loadAll()
    }
  } catch (e: any) {
    showToast(`下载失败: ${e?.message ?? e}`)
  }
}

async function importLocal() {
  importing.value = true
  try {
    const info = await FrpcAPI.ImportLocalFrpc()
    if (info && info.exePath) {
      showToast(`已导入 frpc ${info.version}`)
      await loadAll()
    }
  } catch (e: any) {
    showToast(`导入失败: ${e?.message ?? e}`)
  } finally {
    importing.value = false
  }
}

async function openExeFolder(path: string) {
  try {
    await FrpcAPI.OpenDir(path)
  } catch (e: any) {
    showToast(`打开所在目录失败: ${e?.message ?? e}`)
  }
}

async function removeVersion(v: FrpVersionInfo) {
  const versionShort = v.version.replace(/^v/, '')
  if (!window.confirm(`确定卸载 frpc ${versionShort}？\n该版本不可恢复（删除隔离目录）。`)) return
  try {
    await FrpcAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${versionShort}`)
    await loadAll()
  } catch (e: any) {
    showToast(`卸载失败: ${e?.message ?? e}`)
  }
}

const installedVersionSet = computed(() => new Set(installed.value.map(v => v.version.replace(/^v/, ''))))

onMounted(async () => {
  unlisten = Events.On('frpc:version-download', (event) => {
    const p = event.data as DownloadProgress
    if (!p || !p.version) return
    downloading.value = { ...downloading.value, [p.version]: p }
    if (p.stage === 'done') {
      setTimeout(() => {
        const next = { ...downloading.value }
        delete next[p.version]
        downloading.value = next
      }, 800)
      loadAll()
    }
  })
  await loadAll()
})

onUnmounted(() => {
  if (unlisten) unlisten()
})
</script>

<template>
  <section class="page versions-page">
    <div class="header-row">
      <div>
        <h1>frp 版本管理</h1>
        <p class="subtitle">从 GitHub 官方源（镜像回退）下载 frp Windows 版本，SHA256 硬校验后隔离安装；也已支持手动导入本地 frpc.exe。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

    <!-- 顶部操作栏 -->
    <div class="control-panel">
      <div class="meta-info">
        <span>已安装 {{ installed.length }} 个版本 · 远程可用 {{ releases.length }} 个版本</span>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary" :disabled="loading" @click="refreshRemote">
          {{ loading ? '刷新中…' : '刷新远程列表' }}
        </button>
        <button class="btn btn-primary" :disabled="importing" @click="importLocal">
          {{ importing ? '导入中…' : '导入本地 frpc.exe' }}
        </button>
      </div>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 已安装版本 -->
    <div class="section-title"><h3>已安装版本</h3></div>
    <div v-if="installed.length === 0" class="empty-hint">尚未安装任何 frp 版本 — 从下方远程列表下载，或导入本地 frpc.exe</div>
    <div class="installed-grid">
      <div v-for="v in installed" :key="v.version" class="installed-card">
        <div class="inst-card-top">
          <span class="ver-tag">{{ v.version }}</span>
          <div class="inst-badges">
            <span v-if="v.isImport" class="badge badge-import">本地导入</span>
            <span v-else class="badge badge-official">官方下载</span>
          </div>
        </div>
        <div class="inst-meta">
          <div class="meta-line"><span class="k">路径</span><code class="mono">{{ v.exePath }}</code></div>
          <div class="meta-line"><span class="k">大小</span><span>{{ fmtSize(v.size) }} · 安装于 {{ v.installedAt }}</span></div>
          <div class="meta-line" v-if="v.sha256"><span class="k">SHA256</span><code class="mono short">{{ v.sha256 }}</code></div>
        </div>
        <div class="inst-actions">
          <button class="btn btn-secondary btn-small" @click="openExeFolder(v.exePath)">📂 打开位置</button>
          <button class="btn btn-danger-outline btn-small" @click="removeVersion(v)">卸载</button>
        </div>
      </div>
    </div>

    <!-- 远程可用版本列表 -->
    <div class="section-title"><h3>远程可用版本</h3></div>
    <div class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 110px;">版本</th>
            <th style="width: 90px;">状态</th>
            <th style="width: 100px;">大小</th>
            <th style="width: 110px;">发布时间</th>
            <th style="width: 150px;">SHA256</th>
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
              <span v-if="statusOf(rel) === 'installed'" class="frpc-version-status installed">已安装</span>
              <span v-else-if="statusOf(rel) === 'downloading'" class="frpc-version-status downloading">下载中</span>
              <span v-else-if="statusOf(rel) === 'error'" class="frpc-version-status error">失败</span>
              <span v-else class="frpc-version-status idle">可安装</span>
            </td>
            <td>{{ fmtSize(rel.size) }}</td>
            <td>{{ fmtDate(rel.published) }}</td>
            <td>
              <code v-if="rel.sha256" class="mono short" :title="rel.sha256">{{ rel.sha256.slice(0, 12) }}…</code>
              <span v-else class="muted-text">—</span>
            </td>
            <td>
              <div v-if="statusOf(rel) === 'downloading' && downloading[rel.version]!.stage === 'downloading'" class="download-cell">
                <div class="dl-bar-wrap">
                  <div class="dl-bar-inner" :style="{ width: `${stepOf(downloading[rel.version]!)}%` }"></div>
                </div>
                <span class="dl-percent">{{ stepOf(downloading[rel.version]!) }}%</span>
              </div>
              <div v-else-if="statusOf(rel) === 'downloading'" class="dl-meta-text">
                <span v-if="downloading[rel.version]!.stage === 'hash'">校验 SHA256…</span>
                <span v-else-if="downloading[rel.version]!.stage === 'extract'">解压写入…</span>
                <span v-else class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <button
                v-if="statusOf(rel) === 'idle'"
                class="btn btn-primary btn-small"
                @click="download(rel)"
              >下载安装</button>
              <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small" disabled>已安装</span>
            </td>
          </tr>
          <tr v-if="releases.length === 0 && !loading">
            <td colspan="6" class="empty-hint">无法加载远程版本列表（GitHub 或被墙/限流），可尝试右上角"刷新远程列表"，或直接导入本地 frpc.exe</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.versions-page { display: flex; flex-direction: column; gap: 16px; }

.header-row { display: flex; justify-content: space-between; align-items: center; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  padding: 12px 16px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--text-muted); }
.btn-group { display: flex; gap: 8px; }

.btn { padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn-ghost { background: #f0f7ff; color: #0969da; border-color: #c8e1ff; cursor: default; }
.btn-danger-outline { background: #fff; border-color: #ff8170; color: var(--danger); }
.btn-danger-outline:hover { background: #ffebe9; }

.section-title h3 { font-size: 14px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 8px; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }
.empty-hint { text-align: center; padding: 24px; color: var(--text-subtle); font-size: 13px; }

/* 已安装卡片 */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(380px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px;
  padding: 14px 16px; display: flex; flex-direction: column; gap: 10px;
}
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: Consolas, monospace; font-size: 15px; font-weight: 700; color: var(--text-main); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
.badge-official { background: #dafbe1; color: var(--success); }
.badge-import { background: #ddf4ff; color: #0969da; }
.badge-pre { background: #fff8c5; color: #9a6700; margin-left: 6px; }
.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-muted); }
.meta-line { display: flex; align-items: center; gap: 8px; }
.meta-line .k { color: var(--text-subtle); width: 58px; flex-shrink: 0; }
.mono { font-family: Consolas, monospace; font-size: 12px; color: var(--text-muted); word-break: break-all; }
.mono.short { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; vertical-align: middle; }
.inst-actions { display: flex; justify-content: flex-end; gap: 8px; }

/* 远程版本表格 */
.table-container { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; overflow: hidden; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
.tbl th { background: var(--bg-app); padding: 10px 14px; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border-color); }
.tbl td { padding: 10px 14px; border-bottom: 1px solid var(--border-color); color: var(--text-main); }
.tbl tr:last-child td { border-bottom: none; }
.ver-name { font-family: Consolas, monospace; }

.frpc-version-status { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 500; white-space: nowrap; }
.frpc-version-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex: 0 0 7px; }
.frpc-version-status.installed::before { background: var(--success); }
.frpc-version-status.downloading::before { background: #0969da; }
.frpc-version-status.error::before { background: var(--danger); }
.frpc-version-status.idle::before { background: var(--text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; min-width: 150px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--bg-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--accent); transition: width 0.15s ease; }
.dl-percent { font-size: 11px; color: var(--text-muted); width: 34px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--text-muted); }
.dl-error { color: var(--danger); }

.muted-text { font-size: 12px; color: var(--text-subtle); }

@keyframes fadeIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }
</style>