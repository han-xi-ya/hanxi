<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Status } from '../../../bindings/hanxi/internal/modules/vscode/models'
import type { Snapshot } from '../../../bindings/hanxi/internal/modules/vscode/instance/models'
import type { Release, VersionInfo } from '../../../bindings/hanxi/internal/modules/vscode/version/models'
import type { NormalizedProgress } from '../managed/adapter'
import type { VSCodeForm } from '../../adapters/vscode'
import VSCodeInstalledAppCard from './VSCodeInstalledAppCard.vue'
import VSCodePortableVersionCard from './VSCodePortableVersionCard.vue'
import VSCodeReleaseTable from './VSCodeReleaseTable.vue'

const props = defineProps<{
  installedApp: Status['installed'] | null
  installerSnap: Snapshot | null
  portableSnap: Snapshot | null
  installed: VersionInfo[]
  activeVersion: string
  releasesPortable: Release[]
  releasesInstaller: Release[]
  downloading: Record<string, NormalizedProgress>
  loading: boolean
  busy: boolean
}>()

const emit = defineEmits<{
  refresh: []
  import: []
  download: [payload: { form: VSCodeForm; release: Release }]
  'set-active': [info: VersionInfo]
  'open-dir': [path: string]
  remove: [info: VersionInfo]
}>()

const remoteForm = ref<VSCodeForm>('portable')
const remoteList = computed(() => remoteForm.value === 'installer' ? props.releasesInstaller : props.releasesPortable)
</script>

<template>
  <div class="control-panel">
    <div class="meta-info">
      <span>便携版已装 <strong>{{ installed.length }}</strong> 个 · 远程便携 {{ releasesPortable.length }} 个 · 远程安装器 {{ releasesInstaller.length }} 个</span>
      <span class="hint-dim">全部资产来自微软官方 CDN（update.code.visualstudio.com）；下载后自动补建 data\ 便携数据目录</span>
    </div>
    <div class="btn-group">
      <button class="btn btn-secondary btn-small" :disabled="busy" @click="emit('import')">⇥ 导入本地便携版</button>
      <button class="btn btn-secondary btn-small" :disabled="loading" @click="emit('refresh')">{{ loading ? '刷新中…' : '↻ 刷新远程列表' }}</button>
    </div>
  </div>

  <div class="section-title"><h3>本机安装版（注册表感知）</h3></div>
  <VSCodeInstalledAppCard :info="installedApp" :snap="installerSnap" :busy="busy" @open-dir="emit('open-dir', $event)" />

  <div class="section-title"><h3>已安装便携版 ({{ installed.length }})</h3></div>
  <div v-if="installed.length === 0" class="empty-state first-use">
    <p>尚未安装 VS Code 便携版 —— 下载官方归档（zip），或「导入本地便携版」把已有目录收纳进来</p>
    <button v-if="releasesPortable.length" class="btn btn-primary" @click="emit('download', { form: 'portable', release: releasesPortable[0] })">下载最新版 {{ releasesPortable[0].version }}</button>
    <button v-else-if="!loading" class="btn btn-secondary" @click="emit('refresh')">↻ 刷新远程列表</button>
  </div>
  <div class="installed-grid">
    <VSCodePortableVersionCard
      v-for="info in installed"
      :key="info.version"
      :info="info"
      :active-version="activeVersion"
      :running-version="portableSnap?.state === 'running' ? portableSnap.version : ''"
      :busy="busy"
      @set-active="emit('set-active', $event)"
      @open-dir="emit('open-dir', $event)"
      @remove="emit('remove', $event)"
    />
  </div>

  <div class="section-title vc-remote-head">
    <h3>远程可用版本</h3>
    <div class="vc-form-switch" role="group" aria-label="远程版本形态">
      <button class="btn btn-small" :class="remoteForm === 'portable' ? 'btn-primary' : 'btn-ghost'" @click="remoteForm = 'portable'">便携 ZIP</button>
      <button class="btn btn-small" :class="remoteForm === 'installer' ? 'btn-primary' : 'btn-ghost'" @click="remoteForm = 'installer'">安装器 EXE</button>
    </div>
  </div>
  <VSCodeReleaseTable
    :form="remoteForm"
    :releases="remoteList"
    :installed-versions="installed.map((info) => info.version)"
    :installed-app-version="installedApp?.installed ? installedApp.version : ''"
    :downloading="downloading"
    :loading="loading"
    @download="emit('download', { form: remoteForm, release: $event })"
  />
  <div class="hint-line">
    {{ remoteForm === 'portable'
      ? '便携 ZIP：解压到 versions/vscode_X.Y.Z/ 隔离目录并补建 data\；多版本并存互不干扰。'
      : '安装器 EXE：官方 User Installer 静默安装（免 UAC），安装位置跟随本机既有安装；运行中的 VS Code（含您日常窗口）会被强制关闭后再安装——历史版本无官方哈希时按三层校验放行。' }}
  </div>
</template>

<style scoped>
.vc-remote-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 10px; flex-wrap: wrap; }
.vc-form-switch { display: flex; gap: 6px; }
</style>
