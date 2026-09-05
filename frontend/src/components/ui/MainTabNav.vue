<script setup lang="ts">
// 顶层主选项卡（标注/版本管理这类双区切换）：v-model 收口。
// 类名沿用托管视图迁移前的 main-tab-nav/main-tab-btn——迁移时删视图 scoped 副本、
// 本组件 scoped 样式即唯一标准形；后续如需全局化再上收 components.css。
// idPrefix（可选，§9.5-3）：需要 aria 接线时生成 `${idPrefix}-${key}-tab/-panel`，
// 面板侧由视图自行 id + aria-labelledby 闭环（对应 tablist 的 label 可选命名）。
defineProps<{
  tabs: Array<{ key: string; label: string }>
  modelValue: string
  idPrefix?: string
  label?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [key: string] }>()
</script>

<template>
  <div class="main-tab-nav" role="tablist" :aria-label="label">
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
      @click="emit('update:modelValue', tab.key)"
    >{{ tab.label }}</button>
  </div>
</template>

<style scoped>
.main-tab-nav { display: flex; background: var(--surface-hover); padding: 3px; border-radius: var(--radius-control); gap: 2px; }
.main-tab-btn { background: transparent; border: none; padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; color: var(--color-text-muted); cursor: pointer; transition: color var(--motion-base) ease, background var(--motion-base) ease; }
.main-tab-btn:hover { color: var(--color-text); }
.main-tab-btn.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
</style>
