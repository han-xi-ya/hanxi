<script setup lang="ts">
import { computed, onActivated, onMounted, ref } from 'vue'
import * as EarTrumpetAPI from '../../bindings/hanxi/internal/modules/eartrumpet/eartrumpetservice'
import type { PackageSnapshot } from '../../bindings/hanxi/internal/modules/eartrumpet/models'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const { showToast } = useToast()
const snapshot = ref<PackageSnapshot | null>(null)
const remoteVersion = ref('')
const loading = ref(false)
const error = ref('')
const busy = ref<'' | 'launch' | 'exit' | 'install' | 'repo'>('')
const confirmUninstall = ref(false)
const uninstallBusy = ref(false)

const installed = computed(() => snapshot.value?.installed ?? false)
const running = computed(() => snapshot.value?.running ?? false)
const hasUpdate = computed(() => !!(installed.value && remoteVersion.value && remoteVersion.value !== snapshot.value?.version))
const canInstall = computed(() => !!snapshot.value && (!installed.value || hasUpdate.value))
const stateLabel = computed(() => {
  if (loading.value && !snapshot.value) return '读取中…'
  if (installed.value && running.value) return '运行中'
  return installed.value ? '已安装' : '未安装'
})

async function refresh() {
  loading.value = true
  error.value = ''
  const remote = refreshRemote() // 版本源与包状态并行，不互相阻塞
  try {
    snapshot.value = await EarTrumpetAPI.GetStatus()
  } catch (err) {
    error.value = `读取 Windows 包状态失败：${getErrorMessage(err)}`
  } finally {
    loading.value = false
  }
  await remote
}

async function refreshRemote() {
  try {
    remoteVersion.value = await EarTrumpetAPI.GetRemoteVersion()
  } catch {
    remoteVersion.value = ''
  }
}

async function launch() {
  busy.value = 'launch'
  try {
    await EarTrumpetAPI.Launch()
    showToast('已提交 EarTrumpet 启动请求')
    window.setTimeout(() => { void refresh() }, 1500)
  } catch (err) {
    showToast(`启动失败：${getErrorMessage(err)}`)
  } finally {
    busy.value = ''
  }
}

async function exit() {
  busy.value = 'exit'
  try {
    await EarTrumpetAPI.Exit()
    showToast('已终止 EarTrumpet（下次登录仍会自启）')
    await refresh()
  } catch (err) {
    showToast(`退出失败：${getErrorMessage(err)}`)
  } finally {
    busy.value = ''
  }
}

async function install() {
  busy.value = 'install'
  try {
    const version = await EarTrumpetAPI.Install()
    showToast(`官方直装版 ${version} 已就绪`)
    await refresh()
  } catch (err) {
    showToast(`安装失败：${getErrorMessage(err)}`)
  } finally {
    busy.value = ''
  }
}

async function confirmUninstallAction() {
  uninstallBusy.value = true
  try {
    await EarTrumpetAPI.Uninstall()
    confirmUninstall.value = false
    showToast('已卸载（当前用户）')
    await refresh()
  } catch (err) {
    showToast(`卸载失败：${getErrorMessage(err)}`)
  } finally {
    uninstallBusy.value = false
  }
}

async function openRepo() {
  busy.value = 'repo'
  try {
    await EarTrumpetAPI.OpenRepo()
  } catch (err) {
    showToast(`打开失败：${getErrorMessage(err)}`)
  } finally {
    busy.value = ''
  }
}

// KeepAlive 首次进入时 onActivated 会紧跟 onMounted 触发一次，跳过该次
// 避免双份 PowerShell 子进程开销；此后每次复回页面才刷新。
let firstActivatedSkipped = false
onMounted(() => { void refresh() })
onActivated(() => {
  if (!firstActivatedSkipped) { firstActivatedSkipped = true; return }
  void refresh()
})
</script>

<template>
  <section class="et-page">
    <header class="et-header">
      <div class="et-identity">
        <div class="et-logo">ET</div>
        <div>
          <h1>EarTrumpet</h1>
          <p>Windows 每应用音量控制托盘工具；Hanxi 纳管官方直装渠道（install.eartrumpet.app）。</p>
        </div>
      </div>
      <span class="et-state" :class="{ installed, active: running }"><i></i>{{ stateLabel }}</span>
    </header>

    <UiBanner v-if="error" tone="error" class="et-banner">
      <strong>系统状态读取失败</strong><span>{{ error }}</span><UiButton small class="et-retry" @click="refresh">重试</UiButton>
    </UiBanner>
    <UiBanner v-if="snapshot?.storeCoexist" tone="warn" class="et-banner">
      <strong>检测到商店版并存</strong><span>商店版 v{{ snapshot.storeVersion }} 与直装版共享同一单实例互斥体，同一时刻只有一个能运行且配置互相独立。建议经 Windows 设置或商店卸载商店版——Hanxi 不再代管商店渠道。</span>
    </UiBanner>

    <section class="et-panel et-overview">
      <div class="et-overview-main">
        <span class="et-eyebrow">OFFICIAL SIDELOAD PACKAGE · {{ (remoteVersion || '版本源未知').toUpperCase() }}</span>
        <h2>{{ installed ? `EarTrumpet 直装版 ${snapshot?.version}` : '尚未安装 EarTrumpet' }}</h2>
        <p>{{ installed ? '常驻托盘应用，随系统登录自启；"退出"为直接终止进程（上游无优雅退出通道），下次登录它仍会回来。' : '从上游自托管的官方直装渠道安装最新版本：清单钉死包名/发布者/主机，SHA-256 交叉比对，安装期由 Windows 校验包签名。' }}</p>
        <div class="et-actions">
          <UiButton v-if="canInstall" variant="primary" :disabled="busy !== ''" @click="install">{{ busy === 'install' ? '安装中…' : installed ? `更新到 ${remoteVersion}` : '安装官方直装版' }}</UiButton>
          <UiButton :disabled="!installed || busy !== ''" @click="launch">启动</UiButton>
          <UiButton :disabled="!running || busy !== ''" @click="exit">退出</UiButton>
          <UiButton :disabled="loading" @click="refresh">{{ loading ? '读取中…' : '刷新状态' }}</UiButton>
          <UiButton :disabled="busy !== ''" @click="openRepo">{{ busy === 'repo' ? '正在打开…' : '项目主页' }}</UiButton>
          <UiButton v-if="installed" variant="danger" :disabled="busy !== ''" @click="confirmUninstall = true">卸载</UiButton>
        </div>
      </div>
      <dl class="et-facts">
        <div><dt>版本</dt><dd>{{ snapshot?.version || '—' }}</dd></div>
        <div><dt>官方最新</dt><dd>{{ remoteVersion || '—' }}</dd></div>
        <div><dt>架构</dt><dd>{{ snapshot?.architecture || '—' }}</dd></div>
        <div><dt>进程</dt><dd>{{ installed ? (running ? '运行中' : '未运行') : '—' }}</dd></div>
        <div><dt>Package Family</dt><dd class="et-mono-break">40459File-New-Project.EarTrumpet_725pr5jq8wr8a</dd></div>
      </dl>
    </section>

    <ConfirmDialog
      :open="confirmUninstall"
      title="卸载 EarTrumpet 直装版"
      description="为当前用户卸载 Windows 包。热键、音量覆盖与 Actions 规则等设置保存在包的 LocalSettings 容器中，将随卸载一并删除。"
      confirm-label="卸载"
      tone="danger"
      :busy="uninstallBusy"
      :details="[{ label: '当前版本', value: snapshot?.version || '—' }]"
      @confirm="confirmUninstallAction"
      @cancel="confirmUninstall = false"
    />
  </section>
</template>

<style scoped>
/* 迁移说明：原视图本地定义 --et-primary:#2f6fed（蓝色）压过全局主色——Phase 1 全局转青绿时
   该视图会"漏色"，现删除本地调色板、统一引用语义 token（这是蓝图 §6-4 预判的目视校正点，
   观感变化：强调色蓝→青绿，与全应用一致）。 */
.et-page{max-width:1120px;margin:0 auto;padding-bottom:28px;color:var(--color-text)}
.et-header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:16px}.et-identity{display:flex;align-items:center;gap:13px;min-width:0}.et-logo{display:grid;place-items:center;width:44px;height:44px;flex:none;border:1px solid color-mix(in srgb,var(--color-primary) 28%,var(--color-border));border-radius:13px;background:color-mix(in srgb,var(--color-primary) 10%,var(--surface-panel));color:var(--color-primary);font-weight:800;letter-spacing:-.05em}.et-header h1{margin:0;font-size:20px}.et-header p{margin:4px 0 0;color:var(--color-text-muted);font-size:12px;line-height:1.5}.et-state{display:inline-flex;align-items:center;gap:7px;padding:7px 11px;border:1px solid var(--color-border);border-radius:var(--radius-pill);background:var(--surface-panel);font-size:12px;font-weight:700;white-space:nowrap}.et-state i{width:7px;height:7px;border-radius:50%;background:var(--color-text-subtle)}.et-state.installed i{background:var(--state-positive)}.et-state.active i{animation:hx-pulse 1.8s infinite}
.et-banner{margin-bottom:14px;display:flex;align-items:center;gap:10px}.et-banner span{color:inherit;opacity:.9}.et-retry{margin-left:auto}
.et-panel{margin-bottom:14px;padding:18px;border:1px solid var(--color-border);border-radius:15px;background:var(--surface-panel);box-shadow:var(--shadow-small)}.et-overview{display:grid;grid-template-columns:minmax(0,1fr) minmax(310px,.7fr);gap:24px}.et-eyebrow{color:var(--color-primary);font:700 10px/1.2 var(--font-mono);letter-spacing:.1em}.et-overview h2{margin:8px 0 6px;font-size:19px}.et-overview p{margin:0;color:var(--color-text-muted);font-size:13px;line-height:1.65}.et-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:17px}.et-facts{margin:0;padding:13px;border:1px solid var(--color-border);border-radius:var(--radius-element);background:var(--surface-soft)}.et-facts div{display:grid;grid-template-columns:94px minmax(0,1fr);gap:10px;padding:7px 0;border-bottom:1px solid var(--color-border)}.et-facts div:last-child{border:0}.et-facts dt{color:var(--color-text-muted);font-size:11px}.et-facts dd{margin:0;font:12px/1.45 var(--font-mono)}.et-mono-break{overflow-wrap:anywhere}
@media(max-width:760px){.et-header{align-items:flex-start}.et-header p{display:none}.et-overview{grid-template-columns:1fr}}
@media(max-width:460px){.et-page{padding-bottom:16px}.et-header{flex-direction:column}.et-actions{flex-direction:column}.et-actions :deep(.btn){min-height:44px;width:100%}.et-facts div{grid-template-columns:1fr}.et-banner{align-items:flex-start;flex-direction:column}}
</style>
