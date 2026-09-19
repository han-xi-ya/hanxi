<script setup lang="ts">
// PaperTodo 便签托管工作台（Wave 5 · 批 0 · variant 槽实战）：
// 业务投影（RPC/事件/文案/确认输入/变体偏好/运行时探测）全部收进
// src/adapters/papertodo，本视图仅剩壳骨装配 + variant 槽 UI。
// 未整体套 ManagedConsoleShell：批 0 壳在版本页签无插槽，而「下载变体」
// 选择卡（variant 槽）必须落在版本面板之前——故按壳的槽位契约手工装配
// 同形骨架（PageHeader + MainTabNav + error-box + tab-body），
// ManagedControlBar / ManagedVersionPanel / ManagedExtrasCard 三件共享
// 一份 useManagedConsole store（无重复轮询/订阅）。
// 变体折算（详见 adapter 头注）：listInstalled 退化 0/1、双变体表收敛为
// 「当前变体单列下载 + 变体卡换装入口」；GetRuntimeStatus 经快照扩展字段
// 回流本视图做可用性注记；收拢纸片（adapter.reset，dismiss 族动词）经
// #primary-action 既有控制位与唤回/退出同排。
import { computed, onMounted, ref } from 'vue'
import * as PaperAPI from '../../bindings/hanxi/internal/modules/papertodo/papertodoservice'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiButton from '../components/ui/UiButton.vue'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import { useManagedConsole } from '../components/managed/store'
import { createPaperTodoAdapter, variantName, type PaperSnapshot, type PaperVariant } from '../adapters/papertodo'

const adapter = createPaperTodoAdapter()
const store = useManagedConsole(adapter)
const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/markeron 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '📄 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- variant 槽视图态：adapter.variant 为唯一事实源，本地 ref 只做投影 ----------
const variant = ref<PaperVariant>('self-contained')
onMounted(() => {
  void adapter.variant.get().then((v) => {
    variant.value = v === 'no-runtime' ? 'no-runtime' : 'self-contained'
  })
})

// ⑥：变体切换交 store.runVariant——回执 toast、reloadVersions 重拉版本区
// （远程表 size 列按新变体资产重投影）与失败 '设置失败: ' 前缀全部单源；
// 视图只在成功后翻转本地投影、失败不动（原视图侧手动 load 补丁退役）。
async function chooseVariant(v: PaperVariant) {
  if (v === variant.value) return
  if (await store.runVariant(v)) variant.value = v
}

// no-runtime 变体的运行时可用性提示（GetRuntimeStatus 经快照扩展字段回流）
const runtime = computed(() => (store.snap as PaperSnapshot | null)?.runtime ?? null)
const variantNote = computed(() => {
  if (variant.value !== 'no-runtime') return ''
  if (!runtime.value) return '桌面运行时探测中…'
  if (runtime.value.hasDesktop10) {
    const hit = (runtime.value.desktopRuntimes ?? []).filter((v) => v.startsWith('10.'))
    return `已检测到 .NET 10 桌面运行时${hit.length ? `（${hit.join(' / ')}）` : ''}，精简版可直接运行`
  }
  return '未检测到 .NET 10 桌面运行时：精简版将启动失败，建议改用完整版，或先在「开发环境检测」页了解 .NET 运行时'
})

// 同版本已装但选择了另一变体 → 变体卡承接「换装」入口
// （共享表格的已装版本行无操作钮，换变体语义收在此处；按钮文案逐字沿用现词）
// 先无条件读取 variant/store.releases 再接非响应式的 installedInfo()——
// 若让 !info 短路在前，首评（挂载早期）时 computed 将零依赖永久缓存 null。
const switchTarget = computed(() => {
  const selected = variant.value
  const list = store.releases
  const info = adapter.installedInfo()
  if (!info || info.variant === selected) return null
  return list.find((r) => r.version === info.version) ?? null
})
function switchVariant() {
  if (switchTarget.value) void store.runDownload(switchTarget.value)
}

/** variant options 的 label 全句拆主词与 dim 括注（声明单源于 adapter，DOM 成色对齐现状）。 */
function splitOptLabel(label: string): [string, string] {
  const i = label.indexOf('（')
  return i < 0 ? [label, ''] : [label.slice(0, i), label.slice(i)]
}

// ---------- 收拢纸片（dismiss 族动词收在 adapter.reset，此处置钮+回执） ----------
// busy 闩为视图本地（共享 store.busy 只包 primary/quit/import 等声明动词）
const hideBusy = ref(false)
async function hidePapers() {
  if (hideBusy.value) return
  hideBusy.value = true
  try {
    const res = await adapter.reset.run()
    if (res.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(getErrorMessage(e)) // 裸错误串（无前缀，迁移前口径）
  } finally {
    hideBusy.value = false
  }
}

// ---------- Releases 页直达：契约 repo 槽只有 open，上游页直连为视图胶水 ----------
async function openReleases() {
  try {
    await PaperAPI.OpenReleasesPage()
  } catch (e) {
    showToast('打开失败: ' + getErrorMessage(e))
  }
}
</script>

<template>
  <section class="page papertodo-view">
    <PageHeader
      title="PaperTodo 便签"
      subtitle="托管极简桌面便签 PaperTodo：官方绿色单文件双变体下载、单目录覆盖升级（便签数据永不迁移）、JobObject 启停；唤窗/收拢/退出走上游官方命令通道。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态灯/五态词/banner/hint/启停钮全部由共享控制条按 adapter 投影渲染 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" banner-slim>
        <!-- #primary-action 既有控制位：唤回(primary)与退出(quit)之间插「收拢纸片」 -->
        <template #primary-action>
          <UiButton
            variant="secondary"
            small
            :disabled="hideBusy || (store.state !== 'running' && !store.isExternal)"
            title="收拢全部纸片（hide 命令，托盘与双击召回不受影响）"
            @click="hidePapers"
          >🗜 收拢纸片</UiButton>
        </template>
      </ManagedControlBar>

      <details class="info-details">
        <summary class="info-summary">什么是 PaperTodo</summary>
        <div class="info-body">
          <p>极简 Windows 桌面便签（<a class="inline-link" href="https://github.com/snownico0722/PaperTodo" target="_blank" rel="noopener">snownico0722/PaperTodo</a>，PolyForm Noncommercial 个人可用）：待办纸 + 笔记纸，每张纸独立悬浮窗口，内容自动保存，边缘胶囊收纳。WPF/.NET 原生，无账号无联网。</p>
          <p class="hint-dim">许可证禁止组织统一分发——Hanxi 仅托管"你这台机器直接从官方 Releases 下载原版"的流程，不内嵌、不再分发任何二进制。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（共享件；Releases 页直连经 #extras-action 槽注入） -->
    <ManagedExtrasCard :adapter="adapter">
      <template #extras-action>
        <button class="link-button" @click="openReleases">Releases 页</button>
      </template>
    </ManagedExtrasCard>

    <!-- 版本管理 Tab：variant 槽选择卡 + 共享版本面板（listInstalled 退化 0/1，
         成色经 adapter.copy 投影对齐现状） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <div class="variant-card">
        <span class="k">下载变体</span>
        <label v-for="opt in adapter.variant.options" :key="opt.value" class="variant-opt">
          <input
            type="radio"
            name="pt-variant"
            :checked="variant === opt.value"
            @change="chooseVariant(opt.value as PaperVariant)"
          />
          <span>{{ splitOptLabel(opt.label)[0] }} <span class="hint-dim">{{ splitOptLabel(opt.label)[1] }}</span></span>
        </label>
        <UiButton
          v-if="switchTarget"
          variant="secondary"
          small
          :title="`覆盖安装为${variantName(variant)}变体（便签数据不动）`"
          @click="switchVariant"
        >换装</UiButton>
        <span v-if="variantNote" class="variant-note" :class="{ warn: variant === 'no-runtime' && runtime && !runtime.hasDesktop10 }">{{ variantNote }}</span>
      </div>
      <ManagedVersionPanel :adapter="adapter" :store="store" />
    </div>
  </section>
</template>

<style scoped>
/* 仅保留本视图独有样式；页头/控制条/版本区/联动卡/空态/进度格与徽标家族
   全部由 managed 组件 + components.css 全局原子接管。 */
.papertodo-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* hint-line/info-details/extras-card/repo-row 等由全局原子与共享件皮承担 */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 变体选择卡（variant 槽的模块私有 UI，共享契约无此位） ---------- */
.variant-card { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 8px 14px; font-size: var(--text-base); }
.variant-card .k { color: var(--color-text-subtle); flex-shrink: 0; }
.variant-opt { display: flex; align-items: center; gap: 6px; cursor: pointer; color: var(--color-text); font-size: var(--text-sm); }
.variant-opt input { width: 14px; height: 14px; cursor: pointer; }
.variant-note { font-size: var(--text-sm); color: var(--state-positive); }
.variant-note.warn { color: var(--state-warning); }
</style>
