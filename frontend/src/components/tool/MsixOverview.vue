<script setup lang="ts">
// MSIX 管理型孪生壳·概览面板（§9.5-5 自 NanaZip/EarTrumpet 抽取）：
// 左列眉标/标题/描述 + #actions 动作钮，右列 facts 事实表。
// 文案由视图计算传入；本壳不含业务判断。
defineProps<{
  eyebrow: string
  headline: string
  description: string
  facts: Array<{ label: string; value: string }>
}>()
</script>

<template>
  <section class="msix-panel msix-overview">
    <div class="msix-overview-main">
      <span class="msix-eyebrow">{{ eyebrow }}</span>
      <h2>{{ headline }}</h2>
      <p>{{ description }}</p>
      <div class="msix-actions"><slot name="actions" /></div>
    </div>
    <dl class="msix-facts">
      <div v-for="fact in facts" :key="fact.label">
        <dt>{{ fact.label }}</dt>
        <dd>{{ fact.value }}</dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.msix-panel { margin-bottom: 14px; padding: 18px; border: 1px solid var(--color-border); border-radius: var(--radius-element); background: var(--surface-panel); box-shadow: var(--shadow-small); }
.msix-overview { display: grid; grid-template-columns: minmax(0, 1fr) minmax(310px, 0.7fr); gap: 24px; }
.msix-eyebrow { color: var(--color-primary); font: 700 10px/1.2 var(--font-mono); letter-spacing: 0.1em; }
.msix-overview h2 { margin: 8px 0 6px; font-size: 19px; }
.msix-overview p { margin: 0; color: var(--color-text-muted); font-size: 13px; line-height: 1.65; }
.msix-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 17px; }
.msix-facts { margin: 0; padding: 13px; border: 1px solid var(--color-border); border-radius: var(--radius-element); background: var(--surface-soft); }
.msix-facts div { display: grid; grid-template-columns: 94px minmax(0, 1fr); gap: 10px; padding: 7px 0; border-bottom: 1px solid var(--color-border); }
.msix-facts div:last-child { border: 0; }
.msix-facts dt { color: var(--color-text-muted); font-size: 11px; }
.msix-facts dd { margin: 0; overflow-wrap: anywhere; font: 12px/1.45 var(--font-mono); }
@media (max-width: 760px) { .msix-overview { grid-template-columns: 1fr; } }
@media (max-width: 460px) { .msix-actions { flex-direction: column; } .msix-actions :deep(.btn) { width: 100%; min-height: 44px; } .msix-facts div { grid-template-columns: 1fr; } }
</style>
