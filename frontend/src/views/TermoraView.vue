<script setup lang="ts">
// Termora 控制台（N9，与 WindTermView 同形的托管控制台标准件）：
// 业务投影全部收进 src/adapters/termora（RPC/事件/文案/confirm-force 往返），
// ManagedConsoleShell 管页头页签骨架，ManagedControlBar 管状态头与启停钮区，
// ManagedVersionPanel 管版本区（标准远程表：版本/状态/大小/发布时间）。
// 本视图仅剩装配与"关于"说明卡。
import { createTermoraAdapter } from '../adapters/termora'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createTermoraAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="termora-view"
    :adapter="adapter"
    title="Termora"
    subtitle="托管 Termora 终端与 SSH 客户端：会话在其自有窗口进行。"
    console-tab-label="🖥 控制台"
    :banner-slim="false"
  >
    <details class="info-details">
      <summary class="info-summary">关于 Termora 托管</summary>
      <div class="info-body">
        <p>Termora 是跨平台终端与 SSH 客户端（Java/Swing 构建，官方 Windows 包为 jpackage 便携版，<a class="inline-link" href="https://github.com/TermoraDev/termora" target="_blank" rel="noopener">TermoraDev/termora</a>，双许可 AGPL-3.0/商业）。本模块仅转链官方 GitHub Releases 托管其运行——便携包自带 JRE，不依赖系统 Java；启停受 JobObject 管控。</p>
        <p class="hint-dim">Termora 是单实例应用：Hanxi 启动的实例与你自行打开的实例互斥（后者在场时前者会激活它后让位）。数据位置由联动区「数据模式」决定——默认<b>共享</b> <code class="mono">~/.termora</code>（与你自己打开的 Termora 同一份主机、会话与凭据索引，升级换版本或卸载托管版本都不动它）；切到<b>独立</b>后才落在本版本目录 <code class="mono">data\</code> 内，卸载该版本会连带删除，请先迁出。</p>
        <p class="hint-dim">上游 2.x 线全部以预发布（beta）发布，beta 即事实主干——列表如实全列、预发布徽标不藏行。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>