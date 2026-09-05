<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as FrpcAPI from '../../bindings/hanxi/internal/modules/frpc/frpcservice'
import type { FrpRelease, FrpVersionInfo, DownloadProgress } from '../../bindings/hanxi/internal/modules/frpc/version/models'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { fmtSize, fmtDate } from '../utils/format'
import { getErrorMessage } from '../utils/errors'

const emit = defineEmits<{ (e: 'version-changed'): void }>()

const releases = ref<FrpRelease[]>([])
const installed = ref<FrpVersionInfo[]>([])
const loading = ref(false)
const errorMsg = ref('')

// 下载进度 map: version -> progress
const downloading = ref<Record<string, DownloadProgress>>({})
const importing = ref(false)

const { showToast } = useToast()
const { confirm } = useConfirm()

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
    emit('version-changed')
  } catch (e: unknown) {
    errorMsg.value = `获取版本列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function refreshRemote() {
  loading.value = true
  try {
    releases.value = (await FrpcAPI.ListReleases()) ?? []
  } catch (e: unknown) {
    errorMsg.value = `刷新远程列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
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
  } catch (e: unknown) {
    showToast(`下载失败: ${getErrorMessage(e)}`)
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
  } catch (e: unknown) {
    showToast(`导入失败: ${getErrorMessage(e)}`)
  } finally {
    importing.value = false
  }
}

async function openExeFolder(path: string) {
  try {
    await FrpcAPI.OpenDir(path)
  } catch (e: unknown) {
    showToast(`打开所在目录失败: ${getErrorMessage(e)}`)
  }
}

async function removeVersion(v: FrpVersionInfo) {
  const versionShort = v.version.replace(/^v/, '')
  // 卸载确认收编至全局 useConfirm（文案逐字拆 title/description）
  const accepted = await confirm({
    title: `确定卸载 frpc ${versionShort}？`,
    description: '该版本不可恢复（删除隔离目录）。',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await FrpcAPI.RemoveVersion(v.version)
    showToast(`已卸载 ${versionShort}`)
    await loadAll()
  } catch (e: unknown) {
    showToast(`卸载失败: ${getErrorMessage(e)}`)
  }
}

// 事件订阅收编 useWailsEvent（setup 期注册、卸载自动注销）；toast 由局部实现升级为全局 useToast
useWailsEvent<DownloadProgress>('frpc:version-download', (p) => {
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

onMounted(() => {
  loadAll()
})
</script>

<template>
  <div class="versions-tab-content">
    <div class="header-row">
      <p class="subtitle">从 GitHub 官方源下载 frp Windows 二进制，SHA256 硬校验后隔离安装；也支持导入本地已有的 frpc.exe。</p>
    </div>

    <!-- 顶部操作栏（toast 提示升级为全局 useToast，不再局部渲染） -->
    <div class="control-panel">
      <div class="meta-info">
        <span>已安装 <strong>{{ installed.length }}</strong> 个版本 · 远程可用 {{ releases.length }} 个版本</span>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary btn-small" :disabled="loading" @click="refreshRemote">
          {{ loading ? '刷新中…' : '↻ 刷新远程列表' }}
        </button>
        <button class="btn btn-primary btn-small" :disabled="importing" @click="importLocal">
          {{ importing ? '导入中…' : '📁 导入本地 frpc.exe' }}
        </button>
      </div>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 已安装版本 -->
    <div class="section-title"><h3>已安装二进制版本 ({{ installed.length }})</h3></div>
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
    <div class="section-title" style="margin-top: 12px;"><h3>远程可用版本列表</h3></div>
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
              <div v-else-if="statusOf(rel) === 'error'" class="dl-meta-text">
                <span class="dl-error" :title="downloading[rel.version]!.message">{{ downloading[rel.version]!.message }}</span>
              </div>
              <button
                v-if="statusOf(rel) === 'idle'"
                class="btn btn-primary btn-small"
                @click="download(rel)"
              >下载安装</button>
              <span v-if="statusOf(rel) === 'installed'" class="btn btn-ghost btn-small" disabled>已安装</span>
              <button v-if="statusOf(rel) === 'error'" class="link-button" @click="download(rel)">重试</button>
            </td>
          </tr>
          <tr v-if="releases.length === 0 && !loading">
            <td colspan="6" class="empty-hint">无法加载远程版本列表（GitHub 或被墙/限流），可尝试右上角"刷新远程列表"，或直接导入本地 frpc.exe</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.versions-tab-content { display: flex; flex-direction: column; gap: 14px; }
/* .header-row/.subtitle/.btn 家族/.tbl/.mono/.error-box 由 components.css 全局原子接管 */

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border);
  padding: 10px 14px; border-radius: var(--radius-control);
}
.meta-info { font-size: 13px; color: var(--color-text-muted); }
.meta-info strong { color: var(--color-text); }
.btn-group { display: flex; gap: 8px; }

.section-title h3 { font-size: 13px; font-weight: 600; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin: 0 0 6px; }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: var(--radius-control); border: 1px dashed var(--color-border); }

/* 已安装卡片 */
.installed-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(360px, 1fr)); gap: 12px; }
.installed-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px;
}
.inst-card-top { display: flex; justify-content: space-between; align-items: center; }
.ver-tag { font-family: var(--font-mono); font-size: 14px; font-weight: 700; color: var(--color-text); }
.inst-badges { display: flex; gap: 6px; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.badge-import { background: var(--state-positive-soft); color: var(--state-positive); }
.badge-official { background: var(--state-information-soft); color: var(--state-information); }
.badge-pre { background: var(--state-warning-soft); color: var(--state-warning); margin-left: 4px; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--color-text-muted); align-items: baseline; }
.meta-line .k { color: var(--color-text-subtle); width: 44px; flex-shrink: 0; }
.mono { font-size: 11px; }
.mono.short { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.inst-actions { display: flex; gap: 8px; margin-top: 4px; justify-content: flex-end; }

/* 表格（.tbl 全局原子接管） */
.table-container { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow: hidden; }
.ver-name { font-family: var(--font-mono); }

.frpc-version-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; white-space: nowrap; }
.frpc-version-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex: 0 0 7px; }
.frpc-version-status.installed::before { background: var(--state-positive); }
.frpc-version-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.frpc-version-status.error::before { background: var(--state-danger); }
.frpc-version-status.idle::before { background: var(--color-text-subtle); }

.download-cell { display: flex; align-items: center; gap: 8px; width: 140px; }
.dl-bar-wrap { flex: 1; height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; }
.dl-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
.dl-percent { font-size: 11px; color: var(--color-text-muted); width: 32px; text-align: right; }
.dl-meta-text { font-size: 12px; color: var(--color-primary); }
.dl-error { color: var(--state-danger); font-size: 11px; }
.muted-text { color: var(--color-text-subtle); }
</style>
