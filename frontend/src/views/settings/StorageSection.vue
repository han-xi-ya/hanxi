<script setup lang="ts">
// 设置分区·存储目录：当前数据根（同级默认 / 显式绑定）+ 日志/版本仓/运行时目录直达。
// F6 数据根策略收口后"便携/标准模式"概念退役：本页只回答"家在哪、能不能改"，
// 绑定指针是 exe 同级 hanxi.bind，"绑定哪个用哪个"，换绑/解绑重启后生效。
import { ref, computed, onMounted } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { AppInfo, StorageUsageItem, StorageSubUsageItem } from '../../../bindings/hanxi/internal/app/models'
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

// ---------- 数据根占用一览（N11）----------
// 每个一级子目录 = 一块家底（便携软件/数据类目）。后端 15s 预算截断 + 3 分钟
// 缓存：进页即测走缓存/快速测量；巨目录超时不谎报——Partial 行标"≥"下限。
const usage = ref<StorageUsageItem[]>([])
const usageLoading = ref(false)
const usageAt = ref('')

function fmtSize(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${bytes} B`
}

async function loadUsage(force = false) {
  if (usageLoading.value) return
  usageLoading.value = true
  try {
    usage.value = (await AppAPI.AppService.DataRootUsage(force)) ?? []
    usageAt.value = new Date().toLocaleTimeString()
    // 重测穿透时，展开着的 versions 明细同步刷新（同一 force 语义）
    if (force && versionsOpen.value) void loadSub(true)
  } catch (e: unknown) {
    showToast(`占用测量失败: ${getErrorMessage(e)}`)
  } finally {
    usageLoading.value = false
  }
}

const usageAnyPartial = computed(() => usage.value.some((u) => u.partial))

// ---------- versions 按软件展开（W3-b）----------
// 后端把 `<模块>_<版本>` 子目录聚合成每软件一行（Entries 携版本目录清单）。
const versionsOpen = ref(false)
const subUsage = ref<StorageSubUsageItem[]>([])
const subLoading = ref(false)
const subError = ref('')

async function loadSub(force = false) {
  if (subLoading.value) return
  subLoading.value = true
  subError.value = ''
  try {
    subUsage.value = (await AppAPI.AppService.DataRootSubUsage('versions', force)) ?? []
  } catch (e: unknown) {
    subUsage.value = []
    subError.value = getErrorMessage(e)
  } finally {
    subLoading.value = false
  }
}

function toggleVersions() {
  versionsOpen.value = !versionsOpen.value
  if (versionsOpen.value && !subUsage.value.length) void loadSub()
}

// 数据根级删留徽章（W3-c）：仅静态建议不提供删除钮；未列名的目录不戴章——宁缺毋滥。
const ROOT_VERDICTS: Record<string, { word: string; tone: string; tip: string }> = {
  versions: { word: '保留', tone: 'chip-neutral', tip: '托管软件本体所在；腾容量请到各托管页按版本卸载' },
  memo: { word: '保留', tone: 'chip-warning', tip: '随手记数据，删了=丢笔记' },
  '.snapshots': { word: '保留', tone: 'chip-neutral', tip: '数据自动快照的保命符，确认不再需要回滚前别删' },
  logs: { word: '可删', tone: 'chip-information', tip: '历史运行日志而已，删了只影响翻旧账' },
}
const NO_BADGE = { word: '', tone: 'chip-neutral', tip: '' }
function rootVerdict(name?: string) {
  return (name ? ROOT_VERDICTS[name] : undefined) ?? NO_BADGE
}

onMounted(() => {
  void refresh()
  void loadUsage()
})
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

    <!-- 数据根占用一览：每个一级子目录（= 每个便携软件/数据类目）一块家底 -->
    <div class="card dir-list">
      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">数据根占用一览 <span class="chip chip-neutral dir-badge">一级子目录 · 软件与数据类目</span></span>
          <code class="setting-desc dir-path">
            <template v-if="usageLoading">正在测量（巨目录限时 15 秒，超时按"≥"下限呈现）…</template>
            <template v-else-if="usageAt">测量于 {{ usageAt }}{{ usageAnyPartial ? ' · 含下限估算项' : '' }}</template>
            <template v-else>尚未测量</template>
          </code>
        </span>
        <button class="btn btn-secondary btn-small" :disabled="usageLoading" @click="loadUsage(true)">
          <AppIcon name="refresh-cw" :size="14" /> 重新测量
        </button>
      </div>
      <template v-for="item in usage" :key="item.name">
        <div class="setting-row usage-row">
          <span class="setting-main">
            <span class="setting-name">
              {{ item.name }}
              <span v-if="item.partial" class="chip chip-warning dir-badge" title="测量超时被截断，此值为下限估算">≥ 下限</span>
              <span v-else-if="item.errorCount" class="chip chip-neutral dir-badge" :title="`${item.errorCount} 个条目读取失败已跳过`">{{ item.errorCount }} 项跳过</span>
              <span v-if="rootVerdict(item.name).word" class="chip dir-badge" :class="rootVerdict(item.name).tone" :title="rootVerdict(item.name).tip">{{ rootVerdict(item.name).word }}</span>
            </span>
            <code v-if="item.isDir && item.files" class="setting-desc dir-path">{{ item.files.toLocaleString() }} 个文件</code>
          </span>
          <button v-if="item.name === 'versions'" class="btn btn-secondary btn-small" :disabled="subLoading" @click="toggleVersions">
            {{ versionsOpen ? '收起软件明细' : '展开到每软件' }}
          </button>
          <span class="usage-size" :class="{ 'usage-partial': item.partial }" :title="`${item.bytes.toLocaleString()} 字节${item.partial ? '（下限）' : ''}`">
            {{ item.partial ? '≥ ' : '' }}{{ fmtSize(item.bytes) }}
          </span>
        </div>
        <!-- versions 展开：每个软件一行（悬停看版本清单） -->
        <template v-if="item.name === 'versions' && versionsOpen">
          <div v-if="subLoading" class="setting-row usage-subrow">
            <span class="setting-main"><code class="setting-desc dir-path">正在按软件测量（限时 15 秒，超时按"≥"下限）…</code></span>
          </div>
          <div v-else-if="subError" class="setting-row usage-subrow">
            <span class="setting-main"><code class="setting-desc dir-path usage-sub-err">展开测量失败: {{ subError }}</code></span>
          </div>
          <div v-for="g in subUsage" :key="g.name" class="setting-row usage-subrow">
            <span class="setting-main">
              <span class="setting-name">
                {{ g.name }}
                <span v-if="g.entries && g.entries.length > 1" class="chip chip-neutral dir-badge" :title="`版本目录: ${g.entries.join('、')}`">{{ g.entries.length }} 个版本</span>
                <span v-if="g.partial" class="chip chip-warning dir-badge" title="测量超时被截断，此值为下限估算">≥ 下限</span>
              </span>
              <code v-if="g.files" class="setting-desc dir-path">{{ g.files.toLocaleString() }} 个文件</code>
            </span>
            <span class="usage-size" :class="{ 'usage-partial': g.partial }" :title="`${g.bytes.toLocaleString()} 字节${g.partial ? '（下限）' : ''} · 删了=卸掉该软件，用时要重新下载安装`">
              {{ g.partial ? '≥ ' : '' }}{{ fmtSize(g.bytes) }}
            </span>
          </div>
        </template>
      </template>
      <p v-if="!usageLoading && !usage.length" class="setting-desc">数据根当前没有可统计的子目录。</p>
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
.usage-size {
  font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--text-sm);
  color: var(--color-text-muted); flex-shrink: 0; align-self: center;
}
.usage-partial { color: var(--state-warning); }
.usage-subrow { padding-left: 22px; }
.usage-sub-err { color: var(--state-warning); }
</style>
