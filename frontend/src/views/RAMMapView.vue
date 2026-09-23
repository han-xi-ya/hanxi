<script setup lang="ts">
// RAMMap 控制台（W4 · N4，共享壳形态照抄 WindTermView）：业务投影全部收进
// src/adapters/rammap（RPC/事件/文案/提权预告），本视图仅剩装配与说明卡。
import { createRAMMapAdapter } from '../adapters/rammap'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createRAMMapAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="rammap-view"
    :adapter="adapter"
    title="RAMMap 内存"
    subtitle="托管微软 Sysinternals RAMMap：物理内存占用明细与待机列表清空。"
    console-tab-label="🧮 控制台"
    :banner-slim="false"
  >
    <details class="info-details">
      <summary class="info-summary">关于 RAMMap 托管</summary>
      <div class="info-body">
        <p>RAMMap 是微软 Sysinternals 内存观察工具（<a class="inline-link" href="https://learn.microsoft.com/sysinternals/downloads/rammap" target="_blank" rel="noopener">官方工具页</a>，免费使用）。本模块代下载官方直链包并治理启停：版本模型为"最新版日期"（微软同址覆盖发布、无历史版本），完整性走降级三层（无官方摘要如实标注）。</p>
        <p class="hint-dim">RAMMap 本体要求管理员权限：若你在非提权 Hanxi 下点启动被拒，按页面指引"以管理员身份重启 Hanxi"即可。释放内存动作（Empty → Empty Standby List）在 RAMMap 自己的窗口里完成。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
