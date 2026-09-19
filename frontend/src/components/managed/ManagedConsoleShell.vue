<script setup lang="ts">
// 托管控制台壳（Wave 5 · 批 0 定型件）：PageHeader + MainTabNav（控制台/版本）
// + 版本区错误框 + 状态头 + 联动辅助卡 + 版本面板的固定骨架，组合 store 一份、
// 子件共享注入（杜绝双轮询/双订阅）。
//
// 插槽契约（批 1-5 迁入各模块时按下表填充）：
//   默认具位=控制台 Tab 主体（说明卡等业务内联内容，渲染于日志位之后）；
//   #primary-action（作用域 {snap,state,busy,installedCount}）=主操作钮区注入位
//     ——markeron 六态 toggle、vscode 双形态钮组经此自绘；
//   #console-extra（作用域 {snap,state,busy}）=控制台内联大件（进程日志面板、
//     ddns 端口行等），渲染于状态头之后、主体之前；
//   #danger-extra=强杀/复位等危险动作位（渲染于辅助卡之后，恒居控制台尾部）；
//   MainTabNav 的钮位经 PageHeader #actions 已由本壳固定，无需视图干预。
import { computed, ref } from 'vue'
import type { ManagedModuleAdapter } from './adapter'
import { useManagedConsole } from './store'
import PageHeader from '../ui/PageHeader.vue'
import MainTabNav from '../ui/MainTabNav.vue'
import ManagedControlBar from './ManagedControlBar.vue'
import ManagedVersionPanel from './ManagedVersionPanel.vue'
import ManagedExtrasCard from './ManagedExtrasCard.vue'

const props = withDefaults(
  defineProps<{
    adapter: ManagedModuleAdapter
    title: string
    subtitle?: string
    /** 控制台页签 key/label（markeron 覆写为 annotate/✎ 标注开关）。 */
    consoleTabKey?: string
    consoleTabLabel?: string
    versionsTabLabel?: string
    /** 状态头提示条紧凑档透传。 */
    bannerSlim?: boolean
  }>(),
  {
    subtitle: undefined,
    consoleTabKey: 'console',
    consoleTabLabel: '🔀 控制台',
    versionsTabLabel: '📦 版本管理',
    bannerSlim: true,
  },
)

const store = useManagedConsole(props.adapter)

const activeMainTab = ref(props.consoleTabKey)
const tabs = computed(() => [
  { key: props.consoleTabKey, label: props.consoleTabLabel },
  { key: 'versions', label: props.versionsTabLabel },
])
</script>

<template>
  <section class="page managed-shell">
    <PageHeader :title="title" :subtitle="subtitle">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="tabs" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab -->
    <div v-show="activeMainTab === consoleTabKey" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" :banner-slim="bannerSlim">
        <template v-if="$slots['primary-action']" #primary-action="scope">
          <slot name="primary-action" v-bind="scope" />
        </template>
      </ManagedControlBar>

      <slot name="console-extra" :snap="store.snap" :state="store.state" :busy="store.busy" />
      <slot :snap="store.snap" :state="store.state" :busy="store.busy" />
    </div>

    <!-- 联动与辅助设置卡（无 extras 条目的模块自动缺席） -->
    <ManagedExtrasCard v-if="adapter.extras" :adapter="adapter" />

    <slot name="danger-extra" />

    <!-- 版本管理 Tab -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store" />
    </div>
  </section>
</template>

<style scoped>
/* 控制台壳骨架：页级 flex 纵向 10px 距与 tab-body 家族（视图 scoped 副本的逐字同形） */
.managed-shell { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }
</style>
