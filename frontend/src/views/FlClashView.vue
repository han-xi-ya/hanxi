<script setup lang="ts">
// FlClash 控制台（Wave 5 · 批 0 共享契约迁入件）：
// 共享面全部收敛进 components/managed 托管控制台家族——adapter（src/adapters/flclash）
// 承载业务投影（RPC/事件/文案），ManagedConsoleShell 管页头与页签骨架，
// ManagedControlBar 管状态头与启停钮，ManagedVersionPanel 管版本区，
// ManagedExtrasCard 管联动辅助卡。本视图仅剩装配与业务说明卡（插槽内容）。
// 现状差异位：提示条非 slim 档（页内横幅恒为全高形），经 banner-slim=false 声明。
import { createFlClashAdapter } from '../adapters/flclash'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createFlClashAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="flclash-view"
    :adapter="adapter"
    title="FlClash"
    subtitle="托管 Clash 系代理客户端 FlClash：版本管理、启停与窗口唤起。"
    console-tab-label="⚡ 控制台"
    :banner-slim="false"
  >
    <!-- 控制台 Tab 主体：说明卡（可折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">关于 FlClash</summary>
      <div class="info-body">
        <p>FlClash 是跨平台 Clash 系代理客户端（Flutter 构建，Windows 便携免安装）（上游 <a class="inline-link" href="https://github.com/chen08209/FlClash" target="_blank" rel="noopener">chen08209/FlClash</a>，MIT）。本模块仅托管其运行：版本下载自官方 GitHub Releases（官方 sha256 四层校验），启停受 JobObject 管控。</p>
        <p class="hint-dim">已装多个版本时，在「版本管理」设为使用即可切换；代理配置数据（%APPDATA%）各版本共享，不影响你现有的订阅与节点。</p>
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
