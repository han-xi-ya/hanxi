<script setup lang="ts">
// GoNavi 控制台：ManagedConsoleShell 标准壳装配（ccswitch 同款形制，零 bespoke 页）——
// 业务 RPC/事件/文案投影全部收在 src/adapters/gonavi，本页仅剩装配、页头漂移徽标
// （#header-badge 扩展点）与说明卡（默认槽）。
//
// 漂移展示接法（共享壳无现成 drifted 槽位，按既有扩展点接入，两路同源）：
//  ① adapter.statusTone → 状态灯琥珀（warn 档，⑧ 色档覆写通道）；
//  ② adapter.banner → 状态区漂移警示横幅压过运行态横幅；
//  ③ 本页 #header-badge 槽 → 页头常驻「账外漂移」chip（failed 态横幅让位故障
//     文案时，徽标不受影响，警示不丢）。
import type { GoNaviStatus } from '../adapters/gonavi'
import { createGoNaviAdapter } from '../adapters/gonavi'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createGoNaviAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="gonavi-view"
    :adapter="adapter"
    title="GoNavi"
    subtitle="托管 GoNavi 数据库工具：版本管理、启停与窗口唤起。"
    tab-id-prefix="gonavi"
    tab-label="GoNavi 主选项卡"
  >
    <template #header-badge="{ snap }">
      <span
        v-if="(snap as GoNaviStatus | null)?.drifted"
        class="chip chip-warning drift-badge"
        title="该版本二进制已被 GoNavi 应用内更新器原地替换，与 Hanxi 下载账目不一致——只警示，不自动处置"
      >账外漂移</span>
    </template>

    <!-- 控制台 Tab 主体：说明卡（可折叠，与 adapter.metaHints 同口径） -->
    <details class="info-details">
      <summary class="info-summary">什么是 GoNavi 托管</summary>
      <div class="info-body">
        <p>GoNavi 是便携分发的数据库管理工具，由 Hanxi 统一接管版本（官方 GitHub Releases 下载或导入本地既有安装）、JobObject 管控启停与窗口唤起。</p>
        <p class="hint-dim">注意：GoNavi <b>自带应用内更新器</b>，会原地替换托管目录内的 exe——届时 Hanxi 账外漂移检测会把该版本标为「账外漂移」琥珀警示（只警示、不自动处置，如需对齐账目可删除该版本重新下载）；你的数据与配置在 <code class="mono">%USERPROFILE%\.gonavi</code>，跨版本共享、不随卸载版本删除；托管退出若被其「未保存 SQL」确认框挂住，宽限期后强杀兜底。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头漂移徽标：chip 皮肤走全局原子（.chip.chip-warning），此处仅补徽标在
   header-row flex 轴上的紧凑对齐（不新增全局样式） */
.drift-badge { align-self: center; white-space: nowrap; }
</style>
