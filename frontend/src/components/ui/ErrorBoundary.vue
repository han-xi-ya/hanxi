<script setup lang="ts">
// 视图渲染崩溃兜底：某个子树抛错时只塌这一块，应用外壳存活（消灭"一崩全白屏"）。
// onErrorCaptured 捕获后阻断向上扩散；resetKey 变化（如路由切换）自动复位；
// 用户也可点"重试"就地重挂载子树。
import { onErrorCaptured, ref, watch } from 'vue'
import { getErrorMessage } from '@/utils/errors'

const props = defineProps<{
  /** 变化即自动清除错误态（通常绑路由等"上下文已切换"的信号） */
  resetKey?: unknown
}>()

const error = ref<unknown | null>(null)

onErrorCaptured((err) => {
  error.value = err
  return false // 阻断继续向上传播，外壳与其他区域不受影响
})

watch(() => props.resetKey, () => { error.value = null })

function reset() {
  error.value = null
}
</script>

<template>
  <slot v-if="error === null" />
  <div v-else class="error-boundary" role="alert">
    <p class="eb-title">该模块渲染出错</p>
    <p class="eb-desc">{{ getErrorMessage(error) }}</p>
    <div class="eb-actions">
      <button type="button" class="btn btn-secondary btn-small" @click="reset">重试</button>
    </div>
  </div>
</template>

<style scoped>
.error-boundary {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 32px 24px;
  background: var(--surface-panel);
  border: 1px solid var(--state-danger-glow);
  border-radius: var(--radius-element);
  text-align: center;
}
.eb-title { margin: 0; font-size: 15px; font-weight: 700; color: var(--state-danger); }
.eb-desc { margin: 0; font-size: 13px; color: var(--color-text-muted); max-width: 520px; overflow-wrap: anywhere; }
.eb-actions { display: flex; gap: 8px; margin-top: 4px; }
</style>
