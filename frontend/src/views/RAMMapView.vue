<script setup lang="ts">
// RAMMap 控制台（W4 · N4，共享壳形态照抄 WindTermView）：业务投影全部收进
// src/adapters/rammap（RPC/事件/文案/提权预告），本视图仅剩装配与说明卡。
import { createRAMMapAdapter } from '../adapters/rammap'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import ElevateRestart from '../components/ElevateRestart.vue'

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
    <!-- A/B 双路提权选择条（机主反馈：两钮不得脱离状态卡孤悬）：紧贴状态头
         区渲染（#console-extra 首位），左缘归属条视觉从属于上方卡簇，不再是页面
         底部的独立大卡。三重契约之③预告文案与 A/B 语义逐字保留。 -->
    <template #console-extra="{ state, busy }">
      <div v-if="adapter.needsElevationChoice.value && (state === 'stopped' || state === 'failed')" class="elev-choice">
        <p class="elev-choice-note">RAMMap 本体要求管理员权限：一次性看数据选「仅提权启动」（外部实例，Hanxi 不负责自动关闭）；当常用工具则重启 Hanxi 继续托管。</p>
        <div class="elev-choice-actions">
          <button class="btn btn-primary btn-small" :disabled="busy" @click="adapter.runElevationChoice()">🔑 仅提权启动 RAMMap</button>
          <ElevateRestart route="/ext/rammap" />
        </div>
      </div>
    </template>

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
/* A/B 双路选择条（N22；机主钮位反馈收编）：软底嵌套面 + 左缘 2px primary 混色
   归属条——与存储目录展开子行同一视觉语言（--attribution-line token），
   紧贴状态头下方呈现"附属于状态卡"的从属感，不再是被读作页面底部的孤悬独立卡。 */
.elev-choice {
  --attribution-line: color-mix(in srgb, var(--color-primary) 30%, transparent);
  display: flex; flex-direction: column; gap: 8px;
  margin-top: -4px; padding: 10px 12px;
  border: 1px solid var(--color-border); border-left: 2px solid var(--attribution-line);
  border-radius: var(--radius-control);
  background: var(--surface-soft);
}
.elev-choice-note { margin: 0; font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.55; }
.elev-choice-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
</style>
