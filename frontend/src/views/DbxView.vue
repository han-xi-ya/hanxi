<script setup lang="ts">
// DBX 控制台：ManagedConsoleShell 标准壳装配（ccswitch/gonavi 同款形制，零 bespoke
// 页）——业务 RPC/事件/文案投影全部收在 src/adapters/dbx，本页仅剩装配、页头漂移
// 徽标（#header-badge 扩展点）与说明卡（默认槽）。
//
// 漂移展示接法（共享壳无现成 drifted 槽位，按既有扩展点接入，三处同源）：
//  ① adapter.statusTone → 状态灯琥珀（warn 档，⑧ 色档覆写通道）；
//  ② adapter.banner → 状态区漂移警示横幅压过运行态横幅（failed 恒先）；
//  ③ 本页 #header-badge 槽 → 页头常驻「账本漂移」chip（failed 态横幅让位故障
//     文案时，徽标不受影响，警示不丢）。
import type { DbxStatus } from '../adapters/dbx'
import { createDbxAdapter } from '../adapters/dbx'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createDbxAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="dbx-view"
    :adapter="adapter"
    title="DBX"
    subtitle="托管 DBX 数据库管理工具：版本管理、启停与窗口唤起。"
    tab-id-prefix="dbx"
    tab-label="DBX 主选项卡"
  >
    <template #header-badge="{ snap }">
      <span
        v-if="(snap as DbxStatus | null)?.drifted"
        class="chip chip-warning drift-badge"
        title="该版本二进制已被 DBX 应用自更新原地替换，与 Hanxi 下载账目不一致——只警示，不自动处置"
      >账本漂移</span>
    </template>

    <!-- 控制台 Tab 主体：说明卡（可折叠，与 adapter.metaHints 同口径） -->
    <details class="info-details">
      <summary class="info-summary">什么是 DBX 托管</summary>
      <div class="info-body">
        <p>DBX 是便携分发的数据库管理工具，由 Hanxi 统一接管版本（官方 GitHub Releases 便携包下载——官方 digest 四层完整校验，附带 <code class="mono">.sig</code> 为 minisign 签名、本托管不验证——或导入本地既有便携版）、JobObject 管控启停与窗口唤起。</p>
        <p class="hint-dim">注意：上游<b>日更节奏</b>（月均 20+ 版本），远程列表天然很长，按需停留稳定版即可；DBX <b>自带应用内自更新</b>，会原地替换托管目录内的 exe——届时 Hanxi 账本漂移检测会把该版本标为「账本漂移」琥珀警示（只警示、不自动处置，如需对齐账目可删除该版本重新下载）；数据统一留存 <b>Hanxi 数据根</b>（启动注入 <code class="mono">DBX_DATA_DIR</code>），卸载版本不删数据；关窗驻托盘不退出，托管退出若被托盘弹问挂住，宽限期后强杀兜底，且若开启了应用自带「托管备份」，计划任务可能重新拉起进程——Hanxi 不追杀外部同名进程。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头漂移徽标：chip 皮肤走全局原子（.chip.chip-warning），此处仅补徽标在
   header-row flex 轴上的紧凑对齐（不新增全局样式） */
.drift-badge { align-self: center; white-space: nowrap; }
</style>
