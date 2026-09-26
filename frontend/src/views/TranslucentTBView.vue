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
import { computed, ref } from 'vue'
import { createTBAdapter, isAVCrash, pickDowngradeTarget } from '../adapters/translucenttb'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'

const adapter = createTBAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

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

// C-迷你降级直钮（failed+AV 语境）：候选 = 比崩溃版本更近的更旧稳定版且未装
// （pickDowngradeTarget 判据，本地已有旧版在场时不出钮——后端 AV 话术已点名
// 「你已装 X，设为使用即可试」，直钮只补"无旧版需一键下载"缺口，不硬造）。
// 点击恒走共享 runDownload 既有链（下载完成自动记使用仅在尚无使用版本时生效），
// 装完由用户自己点「设为使用」+「启动」观察——这就是全部自动化，无编排状态机。
const avDowngrade = computed(() => {
  const s = store.snap
  if (!s || !isAVCrash(s)) return null
  return pickDowngradeTarget(s.version, store.installed, store.releases)
})

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
          <!-- C-迷你：AV 崩溃降级鉴别直钮（仅 failed+AV 且无旧版在场、远程有更早
               稳定版时出现；钮序：启动 → 重设 → 安装目录 → 降级直钮 → 退出） -->
          <button
            v-if="avDowngrade"
            class="btn btn-secondary btn-small"
            :disabled="store.busy"
            :title="`降级鉴别：安装比当前崩溃版本更旧的最近稳定版 ${avDowngrade.version}，完成后到「版本管理」设为使用，再点启动观察`"
            @click="avDowngrade && store.runDownload(avDowngrade)"
          >⬇ 装 {{ avDowngrade.version }} 试</button>
        </template>
      </ManagedControlBar>

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

    <!-- 版本管理 Tab：与共享 ManagedVersionPanel 逐字同形，全量接管 -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store" />
    </div>
  </section>
</template>

<style scoped>
/* 页头/状态头/提示条/版本区/联动卡由 managed 组件 + components.css 全局原子接管；
   原 .tb-status-light/.tb-ver-status/.badge-* 复制体已由 .status-light 标准形与
   ManagedVersionPanel 的 scoped 徽标替代，本页仅余页级骨架与说明卡内联链接私有形。 */
.ttb-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
