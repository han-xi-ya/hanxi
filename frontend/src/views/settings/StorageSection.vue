<script setup lang="ts">
// 设置分区·存储目录：当前数据根（同级默认 / 显式绑定）+ 日志/版本仓/运行时目录直达。
// F6 数据根策略收口后"便携/标准模式"概念退役：本页只回答"家在哪、能不能改"，
// 绑定指针是 exe 同级 hanxi.bind，"绑定哪个用哪个"，换绑/解绑重启后生效。
import { ref, computed, onMounted } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { AppInfo } from '../../../bindings/hanxi/internal/app/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import { usePrompt } from '../../composables/usePrompt'
import { useConfirm } from '../../composables/useConfirm'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const { showToast } = useToast()
const { prompt } = usePrompt()
const { confirm } = useConfirm()

const appInfo = ref<AppInfo | null>(null)
const busy = ref(false)

// mode 为 F6 内部来源标记（sibling/bound），仅用于呈现"家从哪来"，不再是运行模式
const bound = computed(() => appInfo.value?.mode === 'bound')

// 技术子目录行（数据根之外的派生目录；配置随根走，根行单列于顶部）
const dirs = computed(() => [
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

async function rebind() {
  const target = await prompt({
    title: '更改数据目录位置',
    label: '请输入新数据目录的完整绝对路径（如 E:\\HanxiData）',
    description: '绑定只是声明"哪个是家"，不会自动搬运现有数据：重启 Hanxi 前，请把当前数据根内容手工移入新目录（全新安家则无需移动）。',
    placeholder: appInfo.value?.baseDir ?? '',
    confirmLabel: '绑定',
  })
  // 取消为 null；空串视同取消（空路径绑定必失败，不留无意义报错）
  if (!target) return
  busy.value = true
  try {
    await AppAPI.AppService.BindDataDir(target)
    showToast('绑定声明已写入，重启 Hanxi 后生效')
  } catch (e: unknown) {
    showToast(`绑定失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function unbind() {
  const accepted = await confirm({
    title: '回到应用同级',
    description: '将清除绑定声明，重启 Hanxi 后数据根回到应用同级 hanxidata/。绑定目录中的现有数据不会被移动或删除。',
    confirmLabel: '解绑',
    tone: 'warning',
  })
  if (!accepted) return
  busy.value = true
  try {
    await AppAPI.AppService.UnbindDataDir()
    showToast('已解除绑定，重启 Hanxi 后回到应用同级')
  } catch (e: unknown) {
    showToast(`解绑失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <PageHeader title="存储目录" subtitle="配置、日志与托管工具的落盘位置；数据根默认在应用同级 hanxidata/，可显式绑定到其他位置。">
      <template #actions>
        <span class="chip" :class="bound ? 'chip-warning' : 'chip-information'">
          {{ appInfo ? (bound ? '已绑定数据之家 · 重启后生效切换' : '应用同级 · ./hanxidata') : '正在读取数据根…' }}
        </span>
      </template>
    </PageHeader>

    <div class="card dir-list">
      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">当前数据根 <span class="chip chip-neutral dir-badge">配置 & 状态 & 托管</span></span>
          <code class="setting-desc dir-path" :title="appInfo?.baseDir">{{ appInfo?.baseDir || '—' }}</code>
        </span>
        <span class="root-actions">
          <button class="btn btn-secondary btn-small" :disabled="!appInfo" @click="openFolder(appInfo?.baseDir)">
            <AppIcon name="folder" :size="14" /> 打开目录
          </button>
          <button class="btn btn-secondary btn-small" :disabled="!appInfo || busy" @click="rebind">更改位置…</button>
          <button v-if="bound" class="btn btn-secondary btn-small" :disabled="busy" @click="unbind">回到同级</button>
        </span>
      </div>
    </div>

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
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.root-actions { display: inline-flex; align-items: center; gap: 8px; flex-shrink: 0; }
</style>
