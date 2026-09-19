<script setup lang="ts">
// 托管控制台状态头（Wave 5 · 批 0）：control-bar 四件套（状态灯 + 状态词 +
// ver-pill/PID/uptime）+ 声明式启停钮区 + 条件 UiBanner/引导行。
// 自 ccswitch/everything 等视图的逐字同构段收编；样式类挂 components.css
// 全局原子（control-bar 家族），状态灯/表格徽标的前缀复制体自此标准形为
// .status-light（组件 scoped，无全局撞名风险——components.css §托管家族皮注记）。
//
// 输入契约：store 缺省时本组件在 setup 期自建控制台 store（独立挂载/测试用）；
// 组合进 ManagedConsoleShell 时由壳注入共享 store，杜绝双份轮询。
// 钮区规则：主钮位先渲染 adapter.control.primary 声明的标准钮（省略 control 即无），
// 其后插入 #primary-action 槽（markeron 六态 toggle、vscode 双快照等自绘钮注入位）；
// 退出钮恒由本组件按 adapter.control.quit 声明渲染并居末位。
import { computed } from 'vue'
import type { ManagedModuleAdapter } from './adapter'
import type { ManagedConsoleStore } from './store'
import { useManagedConsole } from './store'
import { fmtDuration } from '../../utils/format'
import UiBanner from '../ui/UiBanner.vue'

const props = withDefaults(
  defineProps<{
    adapter: ManagedModuleAdapter
    store?: ManagedConsoleStore
    /** 提示条紧凑档（现状 ccswitch/everything 挂 slim；ddnsgo 类非 slim 迁入时传 false）。 */
    bannerSlim?: boolean
  }>(),
  { store: undefined, bannerSlim: true },
)

// setup 期分支固定（有外部 store 绝不再建），条件式创建安全
const store = props.store ?? useManagedConsole(props.adapter)

const primary = computed(() => props.adapter.control?.primary)
const quit = computed(() => props.adapter.control?.quit)

const primaryClass = computed(() => `btn ${primary.value?.cssClass ?? 'btn-secondary'} btn-small`)
const primaryDisabled = computed(
  () => store.busy || (primary.value?.disabledFor ? primary.value.disabledFor(store.state) : false),
)
const primaryTitle = computed(() => primary.value?.titleFor?.(store.state) ?? '')
const quitDisabled = computed(
  () => store.busy || (quit.value?.disabledFor ? quit.value.disabledFor(store.state) : false),
)
const quitTitle = computed(() => quit.value?.titleFor?.(store.state) ?? '')
</script>

<template>
  <div class="control-bar">
    <div class="control-top">
      <div class="control-status">
        <span class="status-light" :class="store.state"></span>
        <span class="status-word">{{ store.stateText }}</span>
        <template v-if="store.isRunningOrStarting && store.runningVersion">
          <span class="ver-pill">{{ store.runningVersion }}</span>
          <span v-if="store.snap?.pid" class="mono pid-tag">PID {{ store.snap?.pid }}</span>
        </template>
        <span v-if="store.state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(store.uptimeSec) }}</span>
      </div>
      <div class="control-btns">
        <!-- 钮序：声明式主钮 → 模块注入钮（#primary-action 槽）→ 退出钮恒居末位 -->
        <button
          v-if="primary"
          :class="primaryClass"
          :disabled="primaryDisabled"
          :title="primaryTitle"
          @click="store.runControl('primary')"
        >{{ primary.label }}</button>
        <slot
          name="primary-action"
          :snap="store.snap"
          :state="store.state"
          :busy="store.busy"
          :installed-count="store.installed.length"
        />
        <button
          v-if="quit"
          class="btn btn-danger-outline btn-small"
          :disabled="quitDisabled"
          :title="quitTitle"
          @click="store.runControl('quit')"
        >{{ quit.label }}</button>
      </div>
    </div>
  </div>

  <!-- 条件提示条 / 引导行（互斥；文案全部来自 adapter 投影） -->
  <UiBanner v-if="store.banner" :tone="store.banner.tone" :class="{ slim: bannerSlim }">{{ store.banner.text }}</UiBanner>
  <div v-else-if="store.hint" class="hint-line">{{ store.hint }}</div>
</template>

<style scoped>
/* 信号灯标准形（收编各视图 {前缀}-status-light 复制体；markeron 垂直字体事故
   教训的隔离性由组件 scoped 属性天然保证） */
.status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
</style>
