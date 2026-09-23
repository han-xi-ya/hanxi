<script setup lang="ts">
// 顶层主选项卡（标注/版本管理这类双区切换）：v-model 收口。
// 类名沿用托管视图迁移前的 main-tab-nav/main-tab-btn——迁移时删视图 scoped 副本、
// 本组件 scoped 样式即唯一标准形；后续如需全局化再上收 components.css。
// idPrefix（可选，§9.5-3）：需要 aria 接线时生成 `${idPrefix}-${key}-tab/-panel`，
// 面板侧由视图自行 id + aria-labelledby 闭环（对应 tablist 的 label 可选命名）。
// 键盘导航（P0 批 3·4.6）：roving tabindex（仅选中项可聚焦）+ ←/→ 循环、
// Home/End 跳首尾，change-selects 模式——焦点移即换选，与点击语义一致。
import { nextTick, ref } from 'vue'

const props = defineProps<{
  tabs: Array<{ key: string; label: string }>
  modelValue: string
  idPrefix?: string
  label?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [key: string] }>()

const rootEl = ref<HTMLElement | null>(null)

function onKeydown(e: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return
  const n = props.tabs.length
  if (n === 0) return
  e.preventDefault()
  const idx = props.tabs.findIndex((t) => t.key === props.modelValue)
  if (idx < 0) return
  const next =
    e.key === 'ArrowRight' ? (idx + 1) % n
    : e.key === 'ArrowLeft' ? (idx - 1 + n) % n
    : e.key === 'Home' ? 0
    : n - 1
  emit('update:modelValue', props.tabs[next].key)
  // 焦点随选中迁移（roving：非选中项 tabindex=-1 不可 Tab 停留）。
  void nextTick(() => {
    const btns = rootEl.value?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    btns?.[next]?.focus()
  })
}
</script>

<template>
  <div ref="rootEl" class="main-tab-nav" role="tablist" :aria-label="label" @keydown="onKeydown">
    <button
      v-for="tab in tabs"
      :key="tab.key"
      type="button"
      role="tab"
      class="main-tab-btn"
      :class="{ active: modelValue === tab.key }"
      :id="idPrefix ? `${idPrefix}-${tab.key}-tab` : undefined"
      :aria-controls="idPrefix ? `${idPrefix}-${tab.key}-panel` : undefined"
      :aria-selected="modelValue === tab.key"
      :tabindex="modelValue === tab.key ? 0 : -1"
      @click="emit('update:modelValue', tab.key)"
    >{{ tab.label }}</button>
  </div>
</template>

<style scoped>
.main-tab-nav { display: flex; background: var(--surface-hover); padding: 3px; border-radius: var(--radius-control); gap: 2px; }
.main-tab-btn { background: transparent; border: none; padding: 6px 16px; border-radius: 6px; font-size: var(--text-base); font-weight: 500; color: var(--color-text-muted); cursor: pointer; transition: color var(--motion-base) ease, background var(--motion-base) ease; }
.main-tab-btn:hover { color: var(--color-text); }
.main-tab-btn.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
</style>
