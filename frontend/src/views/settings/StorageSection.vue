<script setup lang="ts">
// 设置分区·存储目录：运行模式 + 配置/日志/版本仓/运行时四类目录直达。
// 拆分自原 SettingsView 单页：数据源 GetAppInfo，打开目录走后端 OpenPath。
import { ref, computed, onMounted } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { AppInfo } from '../../../bindings/hanxi/internal/app/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const { showToast } = useToast()

const appInfo = ref<AppInfo | null>(null)

const portable = computed(() => appInfo.value?.mode === 'portable')

// 四类目录行清单：路径待 GetAppInfo 回填，未回填时按钮禁用占位
const dirs = computed(() => [
  { name: '配置数据目录', badge: '配置 & 项目', path: appInfo.value?.configDir },
  { name: '日志存储目录', badge: '脱敏运行日志', path: appInfo.value?.logsDir },
  { name: '免安装包版本目录', badge: '托管工具可执行文件隔离仓', path: appInfo.value?.versionsDir },
  { name: '运行时临时目录', badge: '动态 TOML & PID', path: appInfo.value?.runtimeDir },
])

async function refresh() {
  try {
    appInfo.value = await AppAPI.AppService.GetAppInfo()
  } catch (e: unknown) {
    showToast(`获取系统信息失败: ${getErrorMessage(e)}`)
  }
}

async function openFolder(path?: string) {
  if (!path) return
  try {
    await AppAPI.AppService.OpenPath(path)
  } catch (e: unknown) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <PageHeader title="存储目录" subtitle="配置、日志与托管工具的落盘位置；便携模式全部随应用目录迁移。">
      <template #actions>
        <span class="chip" :class="portable ? 'chip-information' : 'chip-neutral'">
          {{ appInfo ? (portable ? '便携免安装模式 · ./data' : '系统标准模式 · %APPDATA%') : '正在读取运行模式…' }}
        </span>
      </template>
    </PageHeader>

    <div class="card dir-list">
      <div v-for="dir in dirs" :key="dir.name" class="setting-row">
        <span class="setting-main">
          <span class="setting-name">{{ dir.name }} <span class="chip chip-neutral dir-badge">{{ dir.badge }}</span></span>
          <code class="setting-desc dir-path" :title="dir.path">{{ dir.path || '—' }}</code>
        </span>
        <button class="btn btn-secondary btn-small" :disabled="!dir.path" @click="openFolder(dir.path)">
          <AppIcon name="folder" :size="14" /> 打开目录
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 目录行复用全局 .setting-row/.card/.chip 原子，此处仅列表节奏与路径机器值皮 */
.dir-list { display: flex; flex-direction: column; gap: 8px; }
.dir-badge { margin-left: 6px; vertical-align: 1px; }
.dir-path {
  font-family: var(--font-mono); font-size: 11px; color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
</style>
