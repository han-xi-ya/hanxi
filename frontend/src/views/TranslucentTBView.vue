<script setup lang="ts">
// TranslucentTB 控制台（Wave 5 · 批 1 收敛件，模式照抄 CCSwitchView）：共享面全部
// 进托管控制台家族——adapter（src/adapters/translucenttb）承载业务投影（RPC/事件/
// 文案/确认输入），useManagedConsole 单源状态轮询/uptime/进度 map/busy 闩，
// ManagedControlBar 管状态头与启停钮，ManagedExtrasCard 管随关与仓库联动卡，
// 版本 Tab 与共享 ManagedVersionPanel 逐字同形、全量接管（本模块为四件中最完整
// 的共享面板消费样本，原方言表格徽标样式随之下线）。
// 方言位（见 adapter 头注记）：「🪄 重设任务栏状态」动词走契约 reset 槽、钮体经
// #primary-action 位留在控制条钮区（原 DOM 位逐字等价；其现状语义成功/失败均
// 不刷快照，与 store.runControl 的恒刷不同，故执行器在视图自持并共用 store.busy
// 闩）；「🗂 安装目录」按 running > active > 任一已装解析目标，点击走
// store.runOpenDir；「🌫️ 启动」为声明式 control.primary（含无已装版本的禁用与
// title 分支，adapter 以 hasInstalled ref 表达）。
// 双形态 Wave（打包版/MSIX）：控制台 Tab 挂「打包版（系统管理）」区块（状态行 +
// 安装包缓存列表，adapter.msix 单源，GetMsixState 失败整块降级单行）；版本 Tab
// 远程行经共享面板 #release-actions 槽挂「可装打包版」chip 与「装打包版」次级
// 钮（词表走 releaseFormWord 共享件，无 assets 数据静默缺席）；崩溃鉴别直钮双
// 语义取位（pickAVProbe）：打包对照优先、便携降级回退，二者永不并排。
import { computed, onMounted, ref } from 'vue'
import { createTBAdapter, hasMsixBundleAsset, isAVCrash, pickAVProbe } from '../adapters/translucenttb'
import type { TBMsixSurface } from '../adapters/translucenttb'
import type { ManagedActionResult } from '../components/managed/adapter'
import { releaseFormWord } from '../components/managed/adapter'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'

const adapter = createTBAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

// 打包线 refs 解构供模板直读（script setup 顶层自动 unwrap）
const msix: TBMsixSurface = adapter.msix
const msixState = msix.state
const msixUnavailable = msix.unavailable
const msixBusy = msix.busy

onMounted(() => {
  void msix.refresh()
})

/** 打包动词统一执行器：成功回执弹 message，失败裸后端错误串如实透传（缓存拦截文案口径）。 */
async function runMsix(run: () => Promise<ManagedActionResult>): Promise<void> {
  try {
    const res = await run()
    if (res.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/everything 同构）
const activeMainTab = ref<'console' | 'versions'>('console')
const mainTabs = [
  { key: 'console', label: '🌫️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

const canReset = computed(() => store.state === 'running' || store.state === 'external')

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirTarget = computed(() => {
  const prefer = store.state === 'running' && store.runningVersion ? store.runningVersion : store.activeVersion
  return store.installed.find((v) => v.version === prefer) ?? store.installed[0] ?? null
})

// 崩溃鉴别直钮（failed+AV 语境，双语义一位取位 pickAVProbe）：
//  - 打包对照（优先）：GetMsixState 预读可用且打包版未装、崩溃版本远程行有
//    .msixbundle 资产 → 装同版本打包版才是真鉴别（InstallMsix 走通即对照步骤
//    完成，成功回执自带指引）；
//  - 便携降级（回退）：原 C-迷你语义逐字保留——比崩溃版本更旧的最近稳定版且
//    本机无旧版在场才出钮，点击走共享 runDownload 既有链，无编排状态机。
//    打包线不可用（预读失败/已装/无资产）时视图与本钮行为和双形态前完全一致。
const avProbe = computed(() => {
  const s = store.snap
  if (!s || !isAVCrash(s)) return null
  return pickAVProbe({
    crashVersion: s.version,
    msixReady: msix.probeReady.value,
    installed: store.installed,
    releases: store.releases,
  })
})

async function runAVProbe(): Promise<void> {
  const p = avProbe.value
  if (!p) return
  if (p.mode === 'msix') {
    await runMsix(() => msix.probeInstall(p.version))
  } else if (p.target) {
    await store.runDownload(p.target)
  }
}

// reset 槽动词的走槽执行器：与原视图 resetState 逐字同构——busy 闩共用 store、
// 成功弹后端 message、失败裸错误串、两分支均不刷快照（runControl 恒刷不适用）
async function runReset() {
  const reset = adapter.reset
  if (!reset) return
  await store.runExclusive(async () => {
    try {
      const res = await reset.run()
      if (res.message !== undefined) showToast(res.message)
    } catch (e) {
      showToast(getErrorMessage(e))
    }
  })
}
</script>

<template>
  <section class="page ttb-view">
    <PageHeader title="TranslucentTB" subtitle="托管任务栏透明工具：版本管理、JobObject 启停与任务栏状态重设。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/提示条/引导行/启停钮区由 ManagedControlBar 按 adapter
         投影渲染；重设与安装目录两钮经 #primary-action 位注入主钮与退出钮之间
         （钮序：启动 → 重设 → 安装目录 → [AV 降级直钮] → 退出；前三位与现状
         逐字同位，降级直钮仅 failed+AV 有候选时条件出现） -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store">
        <template #primary-action>
          <button
            class="btn btn-secondary btn-small"
            :disabled="store.busy || !canReset"
            :title="canReset ? '任务栏外观异常时重放配置（等价托盘菜单 Reset dynamic state）' : '实例未在运行'"
            @click="runReset"
          >🪄 重设任务栏状态</button>
          <button
            class="btn btn-secondary btn-small"
            :disabled="store.busy || !openDirTarget"
            title="打开版本安装目录（透明样式配置 settings.json 就在这里，可用编辑器直接修改）"
            @click="openDirTarget && store.runOpenDir(openDirTarget)"
          >🗂 安装目录</button>
          <!-- 崩溃鉴别直钮（双语义一位，永不并排）：打包线可用且崩溃版本有
               msixbundle 资产 → 对照钮；否则原降级钮词与行为逐字不变 -->
          <button
            v-if="avProbe && avProbe.mode === 'msix'"
            class="btn btn-secondary btn-small"
            :disabled="store.busy || msixBusy"
            title="对照鉴别（踩坑 #85）：安装同版本打包版，观察崩溃是否便携形态专属——不动便携文件、不关闭运行中便携实例"
            @click="runAVProbe"
          >⬇ 装打包版对照（#85 鉴别）</button>
          <button
            v-else-if="avProbe && avProbe.target"
            class="btn btn-secondary btn-small"
            :disabled="store.busy"
            :title="`降级鉴别：安装比当前崩溃版本更旧的最近稳定版 ${avProbe.version}，完成后到「版本管理」设为使用，再点启动观察`"
            @click="runAVProbe"
          >⬇ 装 {{ avProbe.version }} 试</button>
        </template>
      </ManagedControlBar>

      <!-- 打包版（Windows 包/MSIX）系统管理区块：状态行 + 可信安装包缓存列表；
           GetMsixState 失败（含绑定再生缺席）整块降级为单行提示，不空白不报错 -->
      <section class="extras-card tb-msix">
        <div class="tb-msix-head">
          <span class="tb-msix-title">📦 打包版（系统管理）</span>
          <button class="btn btn-ghost btn-small" :disabled="msixBusy" title="重读 Windows 包状态" @click="msix.refresh()">↻ 刷新</button>
        </div>
        <p v-if="msixUnavailable" class="tb-msix-degraded hint-dim">打包版状态暂不可读取（系统包状态查询失败）——便携版管理不受影响，可稍后点「↻ 刷新」重试。</p>
        <template v-else-if="msixState">
          <div v-if="msixState.installed" class="tb-msix-status">
            <span class="tb-msix-state installed">已装 v{{ msixState.version }}</span>
            <code class="mono tb-msix-family" :title="msixState.packageFamily">{{ msixState.packageFamily }}</code>
            <div class="btn-group tb-msix-actions">
              <button class="btn btn-secondary btn-small" :disabled="store.busy || msixBusy" title="启动 Windows 打包版（独立于便携托管实例，JobObject 不管控）" @click="runMsix(msix.launch)">▶ 启动</button>
              <button class="btn btn-danger-outline btn-small" :disabled="store.busy || msixBusy" @click="runMsix(msix.uninstall)">卸载</button>
            </div>
          </div>
          <p v-else class="tb-msix-status">未安装 <span class="hint-dim">——「版本管理」中带「可装打包版」标注的行可装 Windows 包形态，与便携版互不干扰</span></p>
          <div class="tb-msix-cache">
            <span class="tb-msix-sub">安装包缓存（{{ msixState.cache?.length ?? 0 }}）</span>
            <template v-if="msixState.cache?.length">
              <div v-for="c in msixState.cache" :key="c.version" class="tb-msix-cache-row">
                <span class="ver-tag tb-msix-cache-ver">{{ c.version }}</span>
                <span class="tb-msix-cache-size">{{ fmtSize(c.size) }}</span>
                <code class="mono tb-msix-cache-path" :title="c.path">{{ c.path }}</code>
                <button class="btn btn-danger-outline btn-small" :disabled="store.busy || msixBusy" title="删除该版本留存的安装包（不影响已装打包版）；运行中包版本会被系统拦截" @click="runMsix(() => msix.removeCache(c.version))">移除</button>
              </div>
            </template>
            <p v-else class="hint-dim">暂无留存的 msixbundle 安装包缓存</p>
          </div>
        </template>
        <p v-else class="tb-msix-degraded hint-dim">正在读取 Windows 包状态…</p>
      </section>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 TranslucentTB</summary>
        <div class="info-body">
          <p>Windows 任务栏透明/模糊/亚克力效果工具（<a class="inline-link" href="https://github.com/TranslucentTB/TranslucentTB" target="_blank" rel="noopener">TranslucentTB/TranslucentTB</a>，GPL-3.0）。它通过向资源管理器注入组件实时改写任务栏外观，全部样式设置都在系统托盘图标菜单（XAML 飞控）中完成——上游没有独立设置窗口。</p>
          <p class="hint-dim">版本下载自官方 GitHub Releases（portable-x64，sha256 四层校验），启停受 JobObject 管控。「🪄 重设任务栏状态」等价上游托盘菜单的 Reset dynamic state：任务栏被 explorer 重启、换肤工具改动弄花时点一下即可重放配置。退出进程后任务栏自动还原默认外观。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关 + GitHub 仓库行，条目文案在 adapter） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab：与共享 ManagedVersionPanel 逐字同形，全量接管；
         #release-actions 槽按 N13 资产矩阵标注打包形态可用性（词表走共享件，
         无 form/assets 数据静默缺席 = 既有行零变化） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store">
        <template #release-actions="{ release }">
          <template v-if="hasMsixBundleAsset(release)">
            <span
              class="chip chip-information tb-msix-chip"
              :title="`上游发布矩阵含${releaseFormWord('package')}形态（.msixbundle）资产，可加装 Windows 打包版（与便携版互不干扰）`"
            >可装打包版</span>
            <button
              class="btn btn-secondary btn-small"
              :disabled="store.busy || msixBusy"
              title="以 Windows 包（MSIX）形态安装该版本——不触碰便携托管线"
              @click="runMsix(() => msix.installFromRelease(release.version))"
            >装打包版</button>
          </template>
        </template>
      </ManagedVersionPanel>
    </div>
  </section>
</template>

<style scoped>
/* 页头/状态头/提示条/版本区/联动卡由 managed 组件 + components.css 全局原子接管；
   原 .tb-status-light/.tb-ver-status/.badge-* 复制体已由 .status-light 标准形与
   ManagedVersionPanel 的 scoped 徽标替代，本页仅余页级骨架、说明卡内联链接与
   打包版区块（tb- 前缀）私有形——面板壳走全局 .extras-card，此处只补行排布与截断。 */
.ttb-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---- 打包版（系统管理）区块（双形态 Wave 私有形） ---- */
.tb-msix { gap: 8px; }
.tb-msix-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.tb-msix-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text-muted); letter-spacing: 0.5px; }
.tb-msix-degraded { margin: 0; }
.tb-msix-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: 0; font-size: var(--text-sm); }
.tb-msix-state { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.tb-msix-state.installed { color: var(--state-positive); font-weight: 700; }
.tb-msix-family { max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: bottom; color: var(--color-text-muted); font-size: var(--text-xs); }
.tb-msix-actions { margin-left: auto; }
.tb-msix-cache { display: flex; flex-direction: column; gap: 5px; border-top: 1px dashed var(--color-border); padding-top: 8px; }
.tb-msix-sub { font-size: var(--text-xs); color: var(--color-text-subtle); }
.tb-msix-cache-row { display: flex; align-items: center; gap: 10px; min-width: 0; }
.tb-msix-cache-ver { font-size: var(--text-sm); }
.tb-msix-cache-size { color: var(--color-text-muted); white-space: nowrap; font-size: var(--text-sm); }
.tb-msix-cache-path { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-subtle); font-size: var(--text-xs); }
.tb-msix-chip { font-size: var(--text-xs); padding: 1px 7px; white-space: nowrap; margin-right: 6px; vertical-align: middle; }
</style>
