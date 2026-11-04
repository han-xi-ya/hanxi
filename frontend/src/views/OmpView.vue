<script setup lang="ts">
// Oh My Pi (omp) 托管页——官方安装通道形态（2026-11-04 机主拍板拆除自建版本树）：
// 一张「官方安装」卡（装/更/卸三键 + 实探状态 + 脚本进度行）+ 页脚两条目录/仓库行。
// 不显示版本树/激活/导入等概念——后端已无这些 RPC，前端不留死界面。
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { createOmpAdapter } from '../adapters/omp'
import type { OfficialOpProgress } from '../../bindings/hanxi/internal/modules/omp/models'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'

const omp = createOmpAdapter()
const { showToast } = useToast()
const { copyWithToast } = useClipboard()

// 官方装件四事实（GetStatus 现场探测，本模块零持久化）
const officialDir = ref('')
const officialPresent = ref(false)
const officialVersion = ref('')
const onPath = ref(false)
const dataDir = ref('')
const officialOp = ref<OfficialOpProgress | null>(null)
const busy = ref(false)
const repoUrl = ref('')

async function loadStatus() {
  try {
    const st = await omp.getStatus()
    officialDir.value = st?.officialDir ?? ''
    officialPresent.value = !!st?.officialPresent
    officialVersion.value = st?.officialVersion ?? ''
    onPath.value = !!st?.onPath
    dataDir.value = st?.dataDir ?? ''
  } catch (e) {
    showToast(`状态刷新失败: ${getErrorMessage(e)}`)
  }
}

async function runOfficial(kind: 'install' | 'update' | 'uninstall') {
  if (busy.value) return
  busy.value = true
  try {
    const out = kind === 'install'
      ? await omp.installOfficial()
      : kind === 'update'
        ? await omp.updateOfficial()
        : await omp.uninstallOfficial()
    if (out.message) showToast(out.message)
    // 卸载同步跑完才返回，立即复刷；装/更为后台起跑，终态由事件流复刷。
    if (out.reloadVersions) await loadStatus()
  } catch (e) {
    showToast({ install: '安装失败', update: '更新失败', uninstall: '卸载失败' }[kind] + `: ${getErrorMessage(e)}`)
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
  if (repoUrl.value) await copyWithToast(repoUrl.value, '仓库地址已复制')
}

async function openRepo() {
  try {
    await omp.openRepository()
  } catch (e) {
    showToast(`打开失败: ${getErrorMessage(e)}`)
  }
}

// 官方脚本进度：running 禁钮、done/error 后复刷事实
const unlisten = omp.subscribeOfficialOp((p) => {
  officialOp.value = p
  if (p.stage === 'done' || p.stage === 'error') loadStatus()
})

onMounted(() => {
  loadStatus()
  omp.repositoryURL().then(url => { repoUrl.value = url }).catch(e => console.warn('omp repo url:', e))
})
onBeforeUnmount(() => unlisten())
</script>

<template>
  <section class="page omp-view">
    <PageHeader title="Oh My Pi (omp)" subtitle="终端 TUI 编码代理 omp 的官方安装通道：一键安装 / 更新 / 卸载（hanxi 代跑官方脚本）。" />

    <UiBanner tone="warn" class="omp-slim">
      omp 是<strong>纯 TUI</strong> 编码代理，Hanxi 不托管其运行——「安装/更新」由 hanxi 替你执行官方脚本（下载、校验、落位、PATH 打理全程官方行为，无需你配环境变量）；装完<strong>新开终端</strong>直接敲 <code>omp</code>。
    </UiBanner>

    <!-- 官方安装卡：三键 + 实探状态 + 脚本进度行 -->
    <div class="omp-panel">
      <div class="omp-meta">
        <span>
          官方安装：
          <span v-if="officialPresent && onPath" class="omp-badge active">已装 · {{ officialVersion || '版本未知' }} · 命令行可用</span>
          <span v-else-if="officialPresent" class="omp-badge installed">已装 · {{ officialVersion || '版本未知' }}（PATH 未含）</span>
          <span v-else class="omp-badge installed">未安装</span>
          <span class="omp-dim">{{ officialDir }}</span>
        </span>
        <span class="omp-dim">「卸载」删官方目录并剔除其 PATH 条目；你的配置 / 会话 / 记忆在 <code>~/.omp</code>，卸载不动它们。版本追更提醒在「环境检测」页的 Oh My Pi 卡。</span>
        <span v-if="officialOp" :class="['omp-progress', officialOp.stage === 'error' ? 'omp-error' : '']">
          {{ officialOp.stage === 'error' ? '✗ ' : officialOp.stage === 'done' ? '✓ ' : '⏳ ' }}{{ officialOp.message }}
        </span>
      </div>
      <div class="omp-btns">
        <button v-if="!officialPresent" class="btn btn-primary btn-small" :disabled="busy || officialOp?.stage === 'running'" @click="runOfficial('install')">⚡ 安装（官方脚本）</button>
        <button v-else class="btn btn-secondary btn-small" :disabled="busy || officialOp?.stage === 'running'" @click="runOfficial('update')">↑ 更新到最新版</button>
        <button v-if="officialPresent || onPath" class="link-button" :disabled="busy" @click="runOfficial('uninstall')">卸载</button>
        <button class="link-button" :disabled="busy || !officialPresent" title="重跑官方脚本原地修复（幂等）" @click="runOfficial('update')">修复/重装</button>
      </div>
    </div>

    <!-- 页脚：上游仓库 + omp 数据根（目录直达，仅目录语义） -->
    <div class="omp-footer">
      <span>
        上游仓库：<code>{{ repoUrl || '—' }}</code>
        <button class="link-button" :disabled="!repoUrl" @click="copyRepo">复制</button>
        <button class="link-button" :disabled="!repoUrl" @click="openRepo">浏览器打开</button>
      </span>
      <span>
        数据根：<code>{{ dataDir || '—' }}</code>
        <button class="link-button" :disabled="!dataDir" title="打开 ~/.omp（配置、凭据、会话与记忆库所在）" @click="openDir(dataDir)">打开</button>
      </span>
      <span>
        官方安装目录：<code>{{ officialDir || '—' }}</code>
        <button class="link-button" :disabled="!officialPresent" @click="openDir(officialDir)">打开</button>
      </span>
    </div>
  </section>
</template>

<style scoped>
.omp-slim { padding: 8px 12px; font-size: var(--text-sm); }
.omp-panel { background: var(--surface-raised); border: 1px solid var(--border-default); border-radius: var(--radius-lg); padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
.omp-meta { font-size: var(--text-base); color: var(--color-text-muted); display: flex; flex-direction: column; gap: 2px; }
.omp-meta strong { color: var(--color-text); }
.omp-dim { color: var(--color-text-subtle); font-size: var(--text-sm); }
.omp-badge { font-size: var(--text-xs); padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; }
.omp-badge.active { background: var(--state-positive-soft); color: var(--state-positive); font-weight: 600; }
.omp-badge.installed { background: var(--surface-hover); color: var(--color-text-muted); }
.omp-btns { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.omp-progress { font-size: var(--text-sm); color: var(--color-text-muted); }
.omp-error { color: var(--state-danger); }
.omp-footer { display: flex; flex-direction: column; gap: 4px; font-size: var(--text-sm); color: var(--color-text-muted); }
.omp-footer code { font-size: var(--text-xs); }
</style>
