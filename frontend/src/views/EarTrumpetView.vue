<script setup lang="ts">
import { computed, onActivated, onMounted, ref } from 'vue'
import * as EarTrumpetAPI from '../../bindings/hanxi/internal/modules/eartrumpet/eartrumpetservice'
import type { PackageSnapshot } from '../../bindings/hanxi/internal/modules/eartrumpet/models'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import MsixToolHeader from '../components/tool/MsixToolHeader.vue'
import MsixOverview from '../components/tool/MsixOverview.vue'
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

// 概览面板描述（含半角引号，走 script 字符串避免模板属性转义歧义）
const overviewDescription = computed(() =>
  installed.value
    ? '常驻托盘应用，随系统登录自启；"退出"为直接终止进程（上游无优雅退出通道），下次登录它仍会回来。'
    : '从上游自托管的官方直装渠道安装最新版本：清单钉死包名/发布者/主机，SHA-256 交叉比对，安装期由 Windows 校验包签名。')

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
    <MsixToolHeader logo-text="ET" title="EarTrumpet" subtitle="Windows 每应用音量控制托盘工具；Hanxi 纳管官方直装渠道（install.eartrumpet.app）。" :state-label="stateLabel" :installed="installed" :active="running" />

    <UiBanner v-if="error" tone="error" class="et-banner">
      <strong>系统状态读取失败</strong><span>{{ error }}</span><UiButton small class="et-retry" @click="refresh">重试</UiButton>
    </UiBanner>
    <UiBanner v-if="snapshot?.storeCoexist" tone="warn" class="et-banner">
      <strong>检测到商店版并存</strong><span>商店版 v{{ snapshot.storeVersion }} 与直装版共享同一单实例互斥体，同一时刻只有一个能运行且配置互相独立。建议经 Windows 设置或商店卸载商店版——Hanxi 不再代管商店渠道。</span>
    </UiBanner>

    <MsixOverview
      :eyebrow="`OFFICIAL SIDELOAD PACKAGE · ${(remoteVersion || '版本源未知').toUpperCase()}`"
      :headline="installed ? `EarTrumpet 直装版 ${snapshot?.version}` : '尚未安装 EarTrumpet'"
      :description="overviewDescription"
      :facts="[
        { label: '版本', value: snapshot?.version || '—' },
        { label: '官方最新', value: remoteVersion || '—' },
        { label: '架构', value: snapshot?.architecture || '—' },
        { label: '进程', value: installed ? (running ? '运行中' : '未运行') : '—' },
        { label: 'Package Family', value: '40459File-New-Project.EarTrumpet_725pr5jq8wr8a' },
      ]"
    >
      <template #actions>
        <UiButton v-if="canInstall" variant="primary" :disabled="busy !== ''" @click="install">{{ busy === 'install' ? '安装中…' : installed ? `更新到 ${remoteVersion}` : '安装官方直装版' }}</UiButton>
        <UiButton :disabled="!installed || busy !== ''" @click="launch">启动</UiButton>
        <UiButton :disabled="!running || busy !== ''" @click="exit">退出</UiButton>
        <UiButton :disabled="loading" @click="refresh">{{ loading ? '读取中…' : '刷新状态' }}</UiButton>
        <UiButton :disabled="busy !== ''" @click="openRepo">{{ busy === 'repo' ? '正在打开…' : '项目主页' }}</UiButton>
        <UiButton v-if="installed" variant="danger" :disabled="busy !== ''" @click="confirmUninstall = true">卸载</UiButton>
      </template>
    </MsixOverview>

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
/* 迁移说明：原视图本地 --et-primary:#2f6fed（蓝）已删，统一语义 token；
   头部/状态胶囊/概览面板孪生样式上收 components/tool/Msix*（§9.5-5）。 */
.et-page{max-width:1120px;margin:0 auto;padding-bottom:28px;color:var(--color-text)}
.et-banner{margin-bottom:14px;display:flex;align-items:center;gap:10px}.et-banner span{color:inherit;opacity:.9}.et-retry{margin-left:auto}
@media(max-width:460px){.et-page{padding-bottom:16px}.et-banner{align-items:flex-start;flex-direction:column}}
</style>
