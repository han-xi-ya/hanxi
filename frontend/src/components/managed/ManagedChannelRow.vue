<!--
  更新通道共享块（增强批⑧）：adapter.channel（options/get/set 批 0 已定形）的
  统一渲染位——recordly/paseo 视图互抄的 channel-row 段自此收编。渲染于版本面板
  上方；选中项为带 warn 注记的通道时行尾追加琥珀警示（beta 尝鲜话术逐字沿用）。
  编排全部走 store.runChannel（settle/reloadVersions/失败前缀单源）；组件只持
  当前通道值一份本地 ref：首屏 get() 失败保留现词 '读取本地版本失败: ' 写进
  store.listError（recordly 原 localTask 错误面逐字），切换成功才翻转选中态。
-->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { ManagedModuleAdapter } from './adapter'
import type { ManagedConsoleStore } from './store'
import { getErrorMessage } from '../../utils/errors'

const props = defineProps<{
  adapter: ManagedModuleAdapter
  store: ManagedConsoleStore
}>()

const channel = ref('')

onMounted(() => {
  const spec = props.adapter.channel
  if (!spec) return
  void Promise.resolve(spec.get())
    .then((ch) => {
      channel.value = ch
    })
    .catch((e: unknown) => {
      // store 是 reactive 对象 prop，但 listError 属于共享 store 的动作面；
      // 赋值经显式局部别名完成，避免组件语义被误判为改写父级普通 prop。
      const store = props.store
      store.listError = `读取本地版本失败: ${getErrorMessage(e)}`
    })
})

async function choose(value: string) {
  if (value === channel.value) return
  if (await props.store.runChannel(value)) channel.value = value
}

/** 当前选中通道的警示注记（无=不渲染）。 */
function warnOf(): string {
  return props.adapter.channel?.options.find((o) => o.value === channel.value)?.warn ?? ''
}
</script>

<template>
  <div v-if="adapter.channel" class="channel-row">
    <span class="k">更新通道</span>
    <div class="channel-seg">
      <button
        v-for="opt in adapter.channel.options"
        :key="opt.value"
        :class="{ active: channel === opt.value }"
        :disabled="store.busy"
        @click="choose(opt.value)"
      >{{ opt.label }}</button>
    </div>
    <span v-if="warnOf()" class="beta-warn">{{ warnOf() }}</span>
  </div>
</template>

<style scoped>
/* channel 家族标准形（自 recordly/paseo 视图逐字同构段收编） */
.channel-row { display: flex; align-items: center; gap: 10px; background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 8px 14px; font-size: var(--text-base); flex-wrap: wrap; }
.channel-row .k { color: var(--color-text-subtle); }
.channel-seg { display: flex; background: var(--surface-hover); border-radius: 6px; padding: 2px; gap: 2px; }
.channel-seg button { border: none; background: transparent; padding: 4px 12px; font-size: var(--text-sm); border-radius: 5px; color: var(--color-text-muted); cursor: pointer; transition: color var(--motion-base) ease, background var(--motion-base) ease; }
.channel-seg button.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
.beta-warn { font-size: var(--text-sm); color: var(--state-warning); }
</style>
