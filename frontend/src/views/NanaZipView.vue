<script setup lang="ts">
import { computed, onActivated, onMounted, ref } from 'vue'
import * as NanaZipAPI from '../../bindings/hanxi/internal/modules/nanazip/nanazipservice'
import type { OperationProgress, PackageSnapshot } from '../../bindings/hanxi/internal/modules/nanazip/models'
import type { CachedPackage, Release } from '../../bindings/hanxi/internal/modules/nanazip/version/models'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'

const { showToast } = useToast()
const activeTab = ref<'install' | 'versions'>('install')
const tabs = [
  { key: 'install', label: '安装管理' },
  { key: 'versions', label: '版本资源' },
]
const snapshot = ref<PackageSnapshot | null>(null)
const releases = ref<Release[]>([])
const cached = ref<CachedPackage[]>([])
const localLoading = ref(false)
const remoteLoading = ref(false)
const localError = ref('')
const remoteError = ref('')
const progress = ref<OperationProgress | null>(null)
const rowErrors = ref<Record<string, string>>({})
const dialog = ref<{ kind: 'uninstall' | 'downgrade' | 'cache'; version?: string } | null>(null)
const dialogBusy = ref(false)

const installed = computed(() => snapshot.value?.installed ?? false)
const operationBusy = computed(() => !!progress.value && !progress.value.terminal)
const stale = computed(() => releases.value.some(item => item.stale))
const stateLabel = computed(() => operationBusy.value ? stageLabel(progress.value?.stage ?? '') : installed.value ? '已安装' : '未安装')
const latest = computed(() => releases.value[0] ?? null)
const progressPercent = computed(() => {
  const item = progress.value
  if (!item) return null
  if (item.terminal && item.success) return 100
  return item.total > 0 ? Math.min(100, Math.round(item.done * 100 / item.total)) : null
})

function compareVersion(a: string, b: string): number {
  const aa = a.split('.').map(Number), bb = b.split('.').map(Number)
  for (let i = 0; i < Math.max(aa.length, bb.length); i++) {
    if ((aa[i] ?? 0) !== (bb[i] ?? 0)) return (aa[i] ?? 0) > (bb[i] ?? 0) ? 1 : -1
  }
  return 0
}
function relation(release: Release): 'installed' | 'upgrade' | 'downgrade' | 'install' {
  if (!installed.value || !snapshot.value?.version) return 'install'
  const result = compareVersion(release.version, snapshot.value.version)
  return result === 0 ? 'installed' : result > 0 ? 'upgrade' : 'downgrade'
}
function actionLabel(release: Release): string {
  return ({ installed: '已安装', upgrade: '升级', downgrade: '降级', install: '安装' } as const)[relation(release)]
}
function stageLabel(stage: string): string {
  return ({ preflight:'检查状态', downloading:'下载安装包', 'verify-size':'校验大小', 'verify-sha256':'校验官方摘要', 'verify-bundle':'检查 MSIX 身份', 'cache-commit':'可信缓存已就绪', installing:'Windows 正在安装', uninstalling:'Windows 正在卸载', done:'操作完成', error:'操作失败' } as Record<string,string>)[stage] ?? (stage || '处理中')
}
function shortHash(value: string): string { return value ? `${value.slice(0, 10)}…${value.slice(-8)}` : '—' }
function isCached(version: string): boolean { return cached.value.some(item => item.version === version) }

async function refreshLocal() {
  localLoading.value = true; localError.value = ''
  try {
    const [state, cache] = await Promise.all([NanaZipAPI.GetPackageSnapshot(), NanaZipAPI.ListCachedPackages()])
    if (!snapshot.value || state.revision >= snapshot.value.revision) snapshot.value = state
    cached.value = cache ?? []
  } catch (error) { localError.value = `读取 Windows 包状态失败：${getErrorMessage(error)}` }
  finally { localLoading.value = false }
}
async function refreshRemote() {
  remoteLoading.value = true; remoteError.value = ''
  try { releases.value = (await NanaZipAPI.ListReleases()) ?? [] }
  catch (error) { remoteError.value = `获取 NanaZip stable Releases 失败：${getErrorMessage(error)}` }
  finally { remoteLoading.value = false }
}
async function loadPage() { await Promise.allSettled([refreshLocal(), refreshRemote()]) }

async function install(release: Release, allowDowngrade = false) {
  if (operationBusy.value || relation(release) === 'installed') return
  if (relation(release) === 'downgrade' && !allowDowngrade) { dialog.value = { kind:'downgrade', version:release.version }; return }
  rowErrors.value[release.version] = ''
  try {
    const accepted = await NanaZipAPI.InstallVersion(release.version, allowDowngrade)
    progress.value = { operationId: accepted.operationId, kind: accepted.kind, targetVersion: release.version, stage:'preflight', done:0, total:0, message:accepted.message, terminal:false, success:false, errorCode:'', errorDetail:'' }
  } catch (error) { rowErrors.value[release.version] = getErrorMessage(error) }
}
async function launch() { try { await NanaZipAPI.Launch(); showToast('已提交 NanaZip 启动请求') } catch (error) { showToast(`打开失败：${getErrorMessage(error)}`) } }
async function uninstall() {
  dialogBusy.value = true
  try { const accepted = await NanaZipAPI.Uninstall(); progress.value = { operationId:accepted.operationId, kind:accepted.kind, targetVersion:snapshot.value?.version ?? '', stage:'uninstalling', done:0, total:0, message:accepted.message, terminal:false, success:false, errorCode:'', errorDetail:'' }; dialog.value = null }
  catch (error) { showToast(`卸载失败：${getErrorMessage(error)}`) }
  finally { dialogBusy.value = false }
}
async function removeCache(version: string) {
  dialogBusy.value = true
  try { await NanaZipAPI.RemoveCachedPackage(version); dialog.value = null; await refreshLocal(); showToast(`已移除 NanaZip ${version} 安装包缓存`) }
  catch (error) { rowErrors.value[version] = getErrorMessage(error) }
  finally { dialogBusy.value = false }
}
async function confirmDialog() {
  if (dialog.value?.kind === 'uninstall') await uninstall()
  else if (dialog.value?.kind === 'downgrade' && dialog.value.version) { const target = releases.value.find(item => item.version === dialog.value?.version); dialog.value = null; if (target) await install(target, true) }
  else if (dialog.value?.kind === 'cache' && dialog.value.version) await removeCache(dialog.value.version)
}
function handleProgress(item: OperationProgress) {
  if (progress.value?.operationId && item.operationId !== progress.value.operationId) return
  progress.value = item
  if (item.stage === 'error' && item.targetVersion) rowErrors.value[item.targetVersion] = item.message || item.errorDetail
  if (item.terminal) void refreshLocal()
}
function handleSnapshot(item: PackageSnapshot) { if (!snapshot.value || item.revision >= snapshot.value.revision) snapshot.value = item }

onMounted(loadPage)
onActivated(() => { void refreshLocal() })
// MSIX 管理型无轮询：进度与包状态全部由后端事件推送。
useWailsEvent<OperationProgress>('nanazip:operation-progress', (data) => data && handleProgress(data))
useWailsEvent<PackageSnapshot>('nanazip:package-snapshot', (data) => data && handleSnapshot(data))
</script>

<template>
  <section class="nanazip-page">
    <header class="nanazip-header">
      <div class="nanazip-identity"><div class="nanazip-logo">NZ</div><div><h1>NanaZip</h1><p>管理官方 stable MSIX 完整版，由 Windows 提供右键菜单、文件关联与包生命周期。</p></div></div>
      <span class="nanazip-state" :class="{ active: operationBusy, installed }"><i></i>{{ stateLabel }}</span>
    </header>

    <MainTabNav
      :tabs="tabs"
      :model-value="activeTab"
      aria-label="NanaZip 页面"
      @update:model-value="activeTab = $event as 'install' | 'versions'"
    />

    <div v-if="localError" class="nanazip-state-box error"><strong>系统状态读取失败</strong><span>{{ localError }}</span><UiButton small @click="refreshLocal">重试</UiButton></div>

    <template v-if="activeTab === 'install'">
      <section class="nanazip-panel nanazip-overview">
        <div class="nanazip-overview-main">
          <span class="nanazip-eyebrow">CURRENT USER PACKAGE</span>
          <h2>{{ installed ? `NanaZip ${snapshot?.version}` : '尚未安装 NanaZip' }}</h2>
          <p>{{ installed ? '当前状态来自 Windows 包数据库，不依赖 Hanxi 安装包缓存。' : '从版本资源选择 stable 版本，Hanxi 校验官方摘要和包身份后交给 Windows 安装。' }}</p>
          <div class="nanazip-actions">
            <UiButton v-if="installed" variant="primary" :disabled="operationBusy" @click="launch">打开 NanaZip</UiButton>
            <UiButton v-else variant="primary" @click="activeTab = 'versions'">选择版本安装</UiButton>
            <UiButton :disabled="localLoading" @click="refreshLocal">{{ localLoading ? '读取中…' : '刷新状态' }}</UiButton>
            <UiButton v-if="installed" variant="danger" :disabled="operationBusy" @click="dialog = { kind:'uninstall' }">卸载</UiButton>
          </div>
        </div>
        <dl class="nanazip-facts">
          <div><dt>安装范围</dt><dd>当前用户</dd></div><div><dt>架构</dt><dd>{{ snapshot?.architecture || '—' }}</dd></div>
          <div><dt>包状态</dt><dd>{{ snapshot?.packageStatus || (installed ? '已注册' : '未注册') }}</dd></div><div><dt>Package Family</dt><dd>{{ snapshot?.packageFamily || '40174MouriNaruto.NanaZip_gnj4mf6z9tkrc' }}</dd></div>
        </dl>
      </section>

      <section v-if="progress" class="nanazip-panel nanazip-progress" aria-live="polite">
        <div><strong>{{ stageLabel(progress.stage) }}</strong><span>{{ progress.message || `目标版本 ${progress.targetVersion}` }}</span></div>
        <span v-if="progressPercent !== null" class="nanazip-progress-value">{{ progressPercent }}%</span>
        <div class="nanazip-progress-track" role="progressbar" :aria-valuenow="progressPercent ?? undefined"><i :class="{ indeterminate: progressPercent === null && !progress.terminal, failed: progress.stage === 'error' }" :style="progressPercent !== null ? { width: `${progressPercent}%` } : progress.terminal ? { width: '100%' } : undefined"></i></div>
        <p v-if="progress.stage === 'error'" class="nanazip-inline-error">{{ progress.errorDetail || progress.message }}</p>
      </section>

      <section class="nanazip-integrations">
        <article><span>01</span><div><h3>Explorer Shell 集成</h3><p>右键菜单由 MSIX manifest 注册。安装或卸载后若菜单未立即刷新，可重新登录或手动重启 Explorer；Hanxi 不会自动中断桌面会话。</p></div></article>
        <article><span>02</span><div><h3>默认文件关联</h3><p>Windows 控制默认应用选择，Hanxi 不静默接管 ZIP、7z、RAR 等关联。</p></div></article>
        <article><span>03</span><div><h3>运行边界</h3><p>NanaZip 由 Windows 激活，Hanxi 不追踪 PID、不绑定 JobObject，关闭 Hanxi 不影响 NanaZip。</p></div></article>
      </section>
    </template>

    <template v-else>
      <section class="nanazip-panel nanazip-resource-panel">
        <div class="nanazip-section-head"><div><h2>Stable Releases</h2><p>只展示带 GitHub 官方 SHA-256 digest 的正式 MSIXBundle。</p></div><UiButton small :disabled="remoteLoading" @click="refreshRemote">{{ remoteLoading ? '刷新中…' : '刷新远程' }}</UiButton></div>
        <UiBanner v-if="stale" tone="warn" class="nanazip-stale-banner">网络暂不可用，当前展示上次成功缓存的版本列表。</UiBanner>
        <div v-if="remoteError" class="nanazip-state-box error"><strong>远程列表不可用</strong><span>{{ remoteError }}</span></div>
        <div v-else-if="remoteLoading && !releases.length" class="nanazip-state-box"><strong>正在读取 NanaZip Releases</strong><span>请求 GitHub 官方版本与摘要信息。</span></div>
        <div v-else-if="!releases.length" class="nanazip-state-box"><strong>没有可安装的 stable 版本</strong><span>未找到同时满足版本、资产与官方摘要规则的 Release。</span></div>
        <div v-else class="nanazip-resource-list">
          <article v-for="release in releases" :key="release.version" class="nanazip-resource-row">
            <div class="nanazip-resource-main"><div class="nanazip-version-icon">{{ release.version.split('.')[0] }}</div><div><h3>NanaZip {{ release.version }} <span v-if="isCached(release.version)">已缓存</span></h3><p>{{ new Date(release.published).toLocaleDateString() }} · {{ fmtSize(release.size) }} · SHA-256 {{ shortHash(release.sha256) }}</p><p v-if="rowErrors[release.version]" class="nanazip-inline-error">{{ rowErrors[release.version] }}</p></div></div>
            <div class="nanazip-row-actions"><UiButton variant="primary" :disabled="operationBusy || relation(release) === 'installed'" @click="install(release)">{{ actionLabel(release) }}</UiButton></div>
          </article>
        </div>
      </section>

      <section class="nanazip-panel nanazip-resource-panel">
        <div class="nanazip-section-head"><div><h2>可信安装包缓存</h2><p>缓存可用于后续安装，但不代表 Windows 已安装该版本。</p></div></div>
        <div v-if="!cached.length" class="nanazip-state-box"><strong>尚无缓存</strong><span>首次安装时会在完整校验后保存官方 MSIXBundle。</span></div>
        <div v-else class="nanazip-resource-list">
          <article v-for="item in cached" :key="item.version" class="nanazip-resource-row">
            <div class="nanazip-resource-main"><div class="nanazip-version-icon cached">✓</div><div><h3>NanaZip {{ item.version }}</h3><p>{{ fmtSize(item.size) }} · {{ item.architectures?.join(' / ') || '架构已验证' }} · {{ item.verificationMode }}</p></div></div>
            <UiButton variant="danger" :disabled="operationBusy" @click="dialog = { kind:'cache', version:item.version }">移除缓存</UiButton>
          </article>
        </div>
      </section>
    </template>

    <ConfirmDialog :open="!!dialog" :title="dialog?.kind === 'uninstall' ? '卸载 NanaZip' : dialog?.kind === 'downgrade' ? '确认降级' : '移除安装包缓存'" :description="dialog?.kind === 'uninstall' ? '仅卸载当前用户 NanaZip。Explorer 右键菜单可能需要重新登录后完全消失，Hanxi 不会自动重启 Explorer。' : dialog?.kind === 'downgrade' ? 'Windows 将使用 ForceUpdateFromAnyVersion 部署旧版本，请先关闭 NanaZip。' : '仅删除 Hanxi 保存的可信 MSIXBundle，不会卸载系统中的 NanaZip。'" :confirm-label="dialog?.kind === 'uninstall' ? '卸载 NanaZip' : dialog?.kind === 'downgrade' ? '确认降级' : '移除缓存'" :tone="dialog?.kind === 'uninstall' || dialog?.kind === 'cache' ? 'danger' : 'warning'" :busy="dialogBusy" :details="dialog?.kind === 'downgrade' ? [{label:'当前版本',value:snapshot?.version || '—'},{label:'目标版本',value:dialog?.version || '—'}] : []" @confirm="confirmDialog" @cancel="dialog = null" />
  </section>
</template>

<style scoped>
/* 设计 token 引用规范见 docs/FRONTEND.md §7：本视图不再本地定义 --nz-* 私有调色板，
   一律使用语义 token（原 --nz-primary 恰为青绿 #0f8b8d，与主色一致，无观感变化）。 */
.nanazip-page{max-width:1120px;margin:0 auto;padding-bottom:28px;color:var(--color-text)}
.nanazip-header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:16px}.nanazip-identity{display:flex;align-items:center;gap:13px;min-width:0}.nanazip-logo{display:grid;place-items:center;width:44px;height:44px;flex:none;border:1px solid color-mix(in srgb,var(--color-primary) 28%,var(--color-border));border-radius:13px;background:color-mix(in srgb,var(--color-primary) 10%,var(--surface-panel));color:var(--color-primary);font-weight:800;letter-spacing:-.05em}.nanazip-header h1{margin:0;font-size:20px}.nanazip-header p{margin:4px 0 0;color:var(--color-text-muted);font-size:12px;line-height:1.5}.nanazip-state{display:inline-flex;align-items:center;gap:7px;padding:7px 11px;border:1px solid var(--color-border);border-radius:var(--radius-pill);background:var(--surface-panel);font-size:12px;font-weight:700;white-space:nowrap}.nanazip-state i{width:7px;height:7px;border-radius:50%;background:var(--color-text-subtle)}.nanazip-state.installed i{background:var(--state-positive)}.nanazip-state.active i{background:var(--color-primary);animation:hx-pulse 1.8s infinite}
.nanazip-stale-banner{margin-bottom:10px}
.nanazip-panel{margin-bottom:14px;padding:18px;border:1px solid var(--color-border);border-radius:15px;background:var(--surface-panel);box-shadow:var(--shadow-small)}.nanazip-overview{display:grid;grid-template-columns:minmax(0,1fr) minmax(310px,.7fr);gap:24px}.nanazip-eyebrow{color:var(--color-primary);font:700 10px/1.2 var(--font-mono);letter-spacing:.1em}.nanazip-overview h2{margin:8px 0 6px;font-size:19px}.nanazip-overview p{margin:0;color:var(--color-text-muted);font-size:13px;line-height:1.65}.nanazip-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:17px}.nanazip-facts{margin:0;padding:13px;border:1px solid var(--color-border);border-radius:var(--radius-element);background:var(--surface-soft)}.nanazip-facts div{display:grid;grid-template-columns:94px minmax(0,1fr);gap:10px;padding:7px 0;border-bottom:1px solid var(--color-border)}.nanazip-facts div:last-child{border:0}.nanazip-facts dt{color:var(--color-text-muted);font-size:11px}.nanazip-facts dd{margin:0;overflow-wrap:anywhere;font:12px/1.45 var(--font-mono)}
.nanazip-progress{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px}.nanazip-progress strong,.nanazip-progress span{display:block}.nanazip-progress>div>span{margin-top:3px;color:var(--color-text-muted);font-size:12px}.nanazip-progress-value{font:700 13px var(--font-mono)}.nanazip-progress-track{grid-column:1/-1;height:7px;overflow:hidden;border-radius:var(--radius-pill);background:var(--surface-soft)}.nanazip-progress-track i{display:block;height:100%;border-radius:inherit;background:var(--color-primary);transition:width var(--motion-fast) linear}.nanazip-progress-track i.indeterminate{width:34%;animation:nz-indeterminate 1.25s infinite ease-in-out}.nanazip-progress-track i.failed{background:var(--state-danger)}.nanazip-inline-error{margin:7px 0 0!important;color:var(--state-danger)!important;font-size:12px!important}
.nanazip-integrations{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.nanazip-integrations article{display:flex;gap:11px;padding:15px;border:1px solid var(--color-border);border-radius:13px;background:var(--surface-panel)}.nanazip-integrations article>span{color:var(--color-primary);font:700 11px var(--font-mono)}.nanazip-integrations h3{margin:0 0 6px;font-size:13px}.nanazip-integrations p{margin:0;color:var(--color-text-muted);font-size:12px;line-height:1.6}
.nanazip-section-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:13px}.nanazip-section-head h2{margin:0;font-size:15px}.nanazip-section-head p{margin:4px 0 0;color:var(--color-text-muted);font-size:12px}.nanazip-resource-panel{padding:16px}.nanazip-resource-list{display:grid;gap:8px}.nanazip-resource-row{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:14px;padding:12px;border:1px solid var(--color-border);border-radius:11px;background:var(--surface-soft)}.nanazip-resource-main{display:flex;align-items:center;gap:11px;min-width:0}.nanazip-version-icon{display:grid;place-items:center;width:36px;height:36px;flex:none;border-radius:10px;background:color-mix(in srgb,var(--color-primary) 10%,var(--surface-panel));color:var(--color-primary);font-weight:800}.nanazip-version-icon.cached{color:var(--state-positive)}.nanazip-resource-main h3{margin:0;font-size:13px}.nanazip-resource-main h3 span{margin-left:6px;color:var(--state-positive);font-size:10px}.nanazip-resource-main p{margin:4px 0 0;overflow-wrap:anywhere;color:var(--color-text-muted);font:11px/1.5 var(--font-mono)}.nanazip-row-actions{display:flex;align-items:center;gap:8px}.nanazip-state-box{display:flex;align-items:center;gap:10px;padding:14px;border:1px dashed var(--color-border);border-radius:11px;background:var(--surface-soft);font-size:12px}.nanazip-state-box span{color:var(--color-text-muted)}.nanazip-state-box button{margin-left:auto}.nanazip-state-box.error{border-style:solid;color:var(--state-danger)}
@keyframes nz-indeterminate{0%{transform:translateX(-110%)}100%{transform:translateX(300%)}}
@media(max-width:760px){.nanazip-header{align-items:flex-start}.nanazip-header p{display:none}.nanazip-overview{grid-template-columns:1fr}.nanazip-integrations{grid-template-columns:1fr}.nanazip-resource-row{grid-template-columns:1fr}.nanazip-row-actions{justify-content:flex-end}}
@media(max-width:460px){.nanazip-page{padding-bottom:16px}.nanazip-header{flex-direction:column}.nanazip-actions{flex-direction:column}.nanazip-actions :deep(.btn){width:100%;min-height:44px}.nanazip-facts div{grid-template-columns:1fr}.nanazip-section-head{flex-direction:column}.nanazip-section-head :deep(.btn){width:100%}.nanazip-row-actions{align-items:stretch;flex-direction:column}.nanazip-row-actions :deep(.btn){width:100%}.nanazip-state-box{align-items:flex-start;flex-direction:column}}
</style>
