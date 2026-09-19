<script setup lang="ts">
// CC Switch 控制台（Wave 5 · 批 0 黄金样本迁移件）：
// 共享面全部收敛进 components/managed 托管控制台家族——adapter（src/adapters/ccswitch）
// 承载业务投影（RPC/事件/文案），ManagedConsoleShell 管页头与页签骨架，
// ManagedControlBar 管状态头与启停钮，ManagedVersionPanel 管版本区，
// ManagedExtrasCard 管联动辅助卡。本视图仅剩装配与业务说明卡（插槽内容）。
// 状态轮询/uptime/下载进度 map/busy 闩等编排此前逐字同构于 19+ 视图，现由 store 单源。
import { createCCSwitchAdapter } from '../adapters/ccswitch'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createCCSwitchAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="ccswitch-view"
    :adapter="adapter"
    title="CC Switch"
    subtitle="托管 Claude Code / Codex 多供应商切换工具：版本管理、启停与窗口唤起。"
  >
    <!-- 控制台 Tab 主体：说明卡（可折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">什么是 CC Switch</summary>
      <div class="info-body">
        <p>跨平台 Claude Code / Codex / OpenCode 等多工具供应商切换管理器（<a class="inline-link" href="https://github.com/farion1231/cc-switch" target="_blank" rel="noopener">farion1231/cc-switch</a>，MIT）。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
        <p class="hint-dim">多版本时在「版本管理」设为使用即可切换；供应商配置（~/.cc-switch）各版本共享，不影响现有密钥。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/版本区/联动卡/页签与 flex 骨架全部由 managed 组件 + components.css 全局原子接管；
   本页仅余说明卡内联链接这一私有形 */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
