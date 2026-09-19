<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { createVSCodeAdapter, type VSCodeForm } from '../adapters/vscode'
import type { Release, VersionInfo } from '../../bindings/hanxi/internal/modules/vscode/version/models'
import type { ManagedConsoleStore } from '../components/managed/store'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import VSCodeControlBar from '../components/vscode/VSCodeControlBar.vue'
import VSCodeVersionsPanel from '../components/vscode/VSCodeVersionsPanel.vue'

const { adapter, runtime } = createVSCodeAdapter()
const { showToast } = useToast()

const openDirVersion = computed(() => {
  const preferred = runtime.portable?.state === 'running' && runtime.portable.version
    ? runtime.portable.version
    : runtime.activeVersion
  return runtime.installed.find((info) => info.version === preferred) ?? runtime.installed[0] ?? null
})

async function openWindow(store: ManagedConsoleStore, form: VSCodeForm) {
  await store.runExclusive(async () => {
    try {
      const outcome = await runtime.openWindow(form)
      showToast(outcome.message)
      await store.refresh()
    } catch (error) {
      showToast(getErrorMessage(error))
      await store.refresh()
    }
  })
}

async function quitForm(store: ManagedConsoleStore, form: VSCodeForm) {
  await store.runExclusive(async () => {
    try {
      const outcome = await runtime.quit(form)
      showToast(outcome.message)
      await store.refresh()
    } catch (error) {
      showToast(`退出失败: ${getErrorMessage(error)}`)
      await store.refresh()
    }
  })
}

async function download(store: ManagedConsoleStore, form: VSCodeForm, release: Release) {
  try {
    const result = await store.runExclusive(() => runtime.download(form, release))
    if (result?.message !== undefined) showToast(result.message)
    if (result?.reloadVersions) await runtime.loadVersions()
  } catch (error) {
    showToast(`操作失败: ${getErrorMessage(error)}`)
  }
}

async function setActive(store: ManagedConsoleStore, info: VersionInfo) {
  try {
    const result = await store.runExclusive(() => runtime.setActive(info))
    if (result?.message !== undefined) showToast(result.message)
  } catch (error) {
    showToast(`设置失败: ${getErrorMessage(error)}`)
  }
}

async function openDir(store: ManagedConsoleStore, path: string) {
  try {
    await store.runExclusive(() => runtime.openDir(path))
  } catch (error) {
    showToast(`打开目录失败: ${getErrorMessage(error)}`)
  }
}

async function removeVersion(store: ManagedConsoleStore, info: VersionInfo) {
  try {
    const result = await store.runExclusive(() => runtime.remove(info))
    if (result?.message !== undefined) showToast(result.message)
    if (result?.reloadVersions) await runtime.loadVersions()
  } catch (error) {
    showToast(`卸载失败: ${getErrorMessage(error)}`)
  }
}

async function importLocal(store: ManagedConsoleStore) {
  try {
    const result = await store.runExclusive(() => runtime.importLocal())
    if (result?.message !== undefined) showToast(result.message)
    if (result?.reloadVersions) await runtime.loadVersions()
  } catch (error) {
    showToast(`导入失败: ${getErrorMessage(error)}`)
  }
}

onMounted(() => {
  void runtime.loadVersions()
})
</script>

<template>
  <ManagedConsoleShell
    class="vscode-view"
    :adapter="adapter"
    title="VS Code"
    subtitle="托管 Visual Studio Code：便携版隔离安装 + 安装版静默升级，双通道启停与窗口唤起。"
    console-tab-label="💻 控制台"
  >
    <template #control-bar="{ busy, store, selectTab }">
      <div class="vscode-controls">
        <VSCodeControlBar
          form="portable"
          :snap="runtime.portable"
          :installed-app="runtime.installedApp"
          :uptime="runtime.uptimePortable"
          :busy="busy"
          :can-open-dir="!!openDirVersion"
          @open="openWindow(store, $event)"
          @quit="quitForm(store, $event)"
          @open-dir="openDir(store, openDirVersion!.dir)"
          @select-versions="selectTab('versions')"
        />
        <VSCodeControlBar
          form="installer"
          :snap="runtime.installer"
          :installed-app="runtime.installedApp"
          :uptime="runtime.uptimeInstaller"
          :busy="busy"
          :can-open-dir="false"
          @open="openWindow(store, $event)"
          @quit="quitForm(store, $event)"
          @select-versions="selectTab('versions')"
        />
      </div>
    </template>

    <template #default>
      <details class="info-details">
        <summary class="info-summary">什么是 VS Code 双形态托管</summary>
        <div class="info-body">
          <p>VS Code 是微软的开源代码编辑器（<a class="inline-link" href="https://code.visualstudio.com" target="_blank" rel="noopener">code.visualstudio.com</a>，MIT）。上游二进制仅微软官方 CDN 分发：便携版（ZIP 归档）解压即用，配置/扩展全部落在版本目录 data\ 内；安装版（User Installer）为免 UAC 用户级安装，位置跟随本机既有安装（以注册表为准）。</p>
          <p class="hint-dim">两形态实例组天然隔离可并行运行；便携版无应用内自动更新，版本由 Hanxi 统一管理。官方 sha256 仅对最新版可得，历史版本自动降级为字节数 + CRC32 + 布局三层校验（卡片会如实标注）。</p>
          <p class="hint-dim">不设空闲自动退出：VS Code 关窗即退，窗口开着说明你正在编辑，强退反需求。</p>
        </div>
      </details>
    </template>

    <template #versions-body="{ busy, store }">
      <div v-if="runtime.listError" class="error-box">{{ runtime.listError }}</div>
      <VSCodeVersionsPanel
        :installed-app="runtime.installedApp"
        :installer-snap="runtime.installer"
        :portable-snap="runtime.portable"
        :installed="runtime.installed"
        :active-version="runtime.activeVersion"
        :releases-portable="runtime.releasesPortable"
        :releases-installer="runtime.releasesInstaller"
        :downloading="runtime.downloading"
        :loading="runtime.loading"
        :busy="busy"
        @refresh="runtime.loadVersions()"
        @import="importLocal(store)"
        @download="download(store, $event.form, $event.release)"
        @set-active="setActive(store, $event)"
        @open-dir="openDir(store, $event)"
        @remove="removeVersion(store, $event)"
      />
    </template>
  </ManagedConsoleShell>
</template>

<style scoped>
.vscode-controls { display: flex; flex-direction: column; gap: 10px; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
