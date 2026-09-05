<script setup lang="ts">
// MSIX 管理型孪生壳·头部（components/tool 层首落点，§9.5-5 自 NanaZip/EarTrumpet 抽取）：
// 字标徽 + 标题/副题 + 状态胶囊。状态点语义：installed=绿点，active=主色呼吸灯
// （操作进行中/进程运行中；动效复用全局 hx-pulse，不造局部 keyframes）。
defineProps<{
  logoText: string
  title: string
  subtitle: string
  stateLabel: string
  installed?: boolean
  active?: boolean
}>()
</script>

<template>
  <header class="msix-header">
    <div class="msix-identity">
      <div class="msix-logo">{{ logoText }}</div>
      <div>
        <h1>{{ title }}</h1>
        <p>{{ subtitle }}</p>
      </div>
    </div>
    <span class="msix-state" :class="{ active, installed }"><i></i>{{ stateLabel }}</span>
  </header>
</template>

<style scoped>
.msix-header { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.msix-identity { display: flex; align-items: center; gap: 13px; min-width: 0; }
.msix-logo { display: grid; place-items: center; width: 44px; height: 44px; flex: none; border: 1px solid color-mix(in srgb, var(--color-primary) 28%, var(--color-border)); border-radius: var(--radius-element); background: color-mix(in srgb, var(--color-primary) 10%, var(--surface-panel)); color: var(--color-primary); font-weight: 800; letter-spacing: -0.05em; }
.msix-header h1 { margin: 0; font-size: 20px; }
.msix-header p { margin: 4px 0 0; color: var(--color-text-muted); font-size: 12px; line-height: 1.5; }
.msix-state { display: inline-flex; align-items: center; gap: 7px; padding: 7px 11px; border: 1px solid var(--color-border); border-radius: var(--radius-pill); background: var(--surface-panel); font-size: 12px; font-weight: 700; white-space: nowrap; }
.msix-state i { width: 7px; height: 7px; border-radius: 50%; background: var(--color-text-subtle); }
.msix-state.installed i { background: var(--state-positive); }
.msix-state.active i { background: var(--color-primary); animation: hx-pulse 1.8s infinite; }
@media (max-width: 760px) { .msix-header { align-items: flex-start; } .msix-header p { display: none; } }
@media (max-width: 460px) { .msix-header { flex-direction: column; } }
</style>
