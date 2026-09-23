<script setup lang="ts">
// 托管控制台壳（Wave 5 · 批 0 定型件；增强批⑦扩装）：PageHeader + MainTabNav
// （控制台/版本）+ 版本区错误框 + 状态头 + 联动辅助卡 + 版本面板的固定骨架，
// 组合 store 一份、子件共享注入（杜绝双轮询/双订阅）。
//
// 插槽契约（批 1-5 迁入各模块时按下表填充）：
//   默认具位=控制台 Tab 主体（说明卡等业务内联内容，渲染于日志位之后）；
//   #control-bar（作用域 {snap,state,busy,store,selectTab}）=完整替换默认状态头，
//     用于 VS Code 双形态、Snipaste 本会话所有权等无法压成单状态头的控制面；
//   #primary-action（作用域 {snap,state,busy,installedCount}）=默认 ManagedControlBar
//     内的主操作钮区注入位——markeron 六态 toggle 等经此自绘；
//   #console-extra（作用域 {snap,state,busy}）=控制台内联大件（进程日志面板、
//     ddns 端口行等），渲染于状态头之后、主体之前；
//   #danger-extra（作用域 {snap,state,busy,store}，⑦）=强杀/复位等危险动作位
//     （渲染于辅助卡之后，恒居控制台尾部）；
//   #extras-action（作用域 {snap,state,busy,store}，⑦）=联动卡钮位转发；
//   #header-badge（作用域 {snap,state,busy}，⑦）=页头状态徽标透传位
//     （bili23/mangodisk 型，渲染于页签钮之前）；
//   #versions-body（作用域 {snap,state,busy,store}，⑦）=版本 Tab 整体替换位
//     （vscode 双表 / everything 方言表等面板装不下的形态；缺省=共享 channel
//     块 + ManagedVersionPanel）；
//   versionsTabCount（⑦）=版本页签计数后缀（mangodisk「版本管理 N」形制）；
//   MainTabNav 的钮位经 PageHeader #actions 已由本壳固定，无需视图干预。
import { computed, ref } from 'vue'
import type { ManagedModuleAdapter } from './adapter'
import { useManagedConsole } from './store'
import PageHeader from '../ui/PageHeader.vue'
import MainTabNav from '../ui/MainTabNav.vue'
import ManagedControlBar from './ManagedControlBar.vue'
import ManagedChannelRow from './ManagedChannelRow.vue'
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
    /** ⑦：版本页签计数（提供时页签词追加「 N」，mangodisk 形制）。 */
    versionsTabCount?: number
    /** 状态头提示条紧凑档透传。 */
    bannerSlim?: boolean
    /** 可选 tab/panel ARIA 前缀（如 snipaste）。 */
    tabIdPrefix?: string
    /** tablist 可访问名称。 */
    tabLabel?: string
  }>(),
  {
    subtitle: undefined,
    consoleTabKey: 'console',
    consoleTabLabel: '🔀 控制台',
    versionsTabLabel: '📦 版本管理',
    versionsTabCount: undefined,
    bannerSlim: true,
    tabIdPrefix: undefined,
    tabLabel: undefined,
  },
)

const store = useManagedConsole(props.adapter)

const activeMainTab = ref(props.consoleTabKey)
function selectTab(key: string) {
  activeMainTab.value = key
}
// N43②：未安装态「启动」直落版本管理页——store 单源判定，此处只供接线。
store.goVersions = () => selectTab('versions')

const tabs = computed(() => [
  { key: props.consoleTabKey, label: props.consoleTabLabel },
  {
    key: 'versions',
    label: props.versionsTabCount === undefined ? props.versionsTabLabel : `${props.versionsTabLabel} ${props.versionsTabCount}`,
  },
])
</script>

<template>
  <section class="page managed-shell">
    <PageHeader :title="title" :subtitle="subtitle">
      <template v-if="$slots.icon" #icon>
        <slot name="icon" />
      </template>
      <template #actions>
        <!-- ⑦：页头徽标透传位（display:contents 不破坏 header-row 的 flex 排布） -->
        <div class="shell-header-actions">
          <slot name="header-badge" :snap="store.snap" :state="store.state" :busy="store.busy" />
          <MainTabNav
            v-model="activeMainTab"
            :tabs="tabs"
            :id-prefix="tabIdPrefix"
            :label="tabLabel"
          />
        </div>
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab -->
    <div
      v-show="activeMainTab === consoleTabKey"
      class="tab-body"
      :id="tabIdPrefix ? `${tabIdPrefix}-${consoleTabKey}-panel` : undefined"
      role="tabpanel"
      :aria-labelledby="tabIdPrefix ? `${tabIdPrefix}-${consoleTabKey}-tab` : undefined"
    >
      <slot
        name="control-bar"
        :snap="store.snap"
        :state="store.state"
        :busy="store.busy"
        :store="store"
        :select-tab="selectTab"
      >
        <ManagedControlBar :adapter="adapter" :store="store" :banner-slim="bannerSlim">
          <template v-if="$slots['primary-action']" #primary-action="scope">
            <slot name="primary-action" v-bind="scope" />
          </template>
        </ManagedControlBar>
      </slot>

      <slot name="console-extra" :snap="store.snap" :state="store.state" :busy="store.busy" />
      <slot :snap="store.snap" :state="store.state" :busy="store.busy" :store="store" :select-tab="selectTab" />
    </div>

    <!-- 联动与辅助设置卡（无条目且无注入钮/无托管位置行的模块自动缺席） -->
    <ManagedExtrasCard
      v-if="adapter.extras || adapter.copy?.dataDirRow || $slots['extras-action']"
      :adapter="adapter"
      :store="store"
    >
      <template v-if="$slots['extras-action']" #extras-action="scope">
        <slot name="extras-action" v-bind="scope" />
      </template>
    </ManagedExtrasCard>

    <!-- ⑦：危险动作位补作用域（bili23 强杀钮现态/busy 闩回归共享件） -->
    <slot name="danger-extra" :snap="store.snap" :state="store.state" :busy="store.busy" :store="store" />

    <!-- 版本管理 Tab -->
    <div
      v-show="activeMainTab === 'versions'"
      class="tab-body"
      :id="tabIdPrefix ? `${tabIdPrefix}-versions-panel` : undefined"
      role="tabpanel"
      :aria-labelledby="tabIdPrefix ? `${tabIdPrefix}-versions-tab` : undefined"
    >
      <slot name="versions-body" :snap="store.snap" :state="store.state" :busy="store.busy" :store="store">
        <!-- ⑧：channel 声明即自动渲染面板上方共享块 -->
        <ManagedChannelRow v-if="adapter.channel" :adapter="adapter" :store="store" />
        <ManagedVersionPanel :adapter="adapter" :store="store" />
      </slot>
    </div>
  </section>
</template>

<style scoped>
/* 控制台壳骨架：页级 flex 纵向 10px 距与 tab-body 家族（视图 scoped 副本的逐字同形） */
.managed-shell { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }
/* 页头徽标与页签钮并排：contents 让子节点直挂 PageHeader .header-row 的 flex 轴 */
.shell-header-actions { display: contents; }
</style>
