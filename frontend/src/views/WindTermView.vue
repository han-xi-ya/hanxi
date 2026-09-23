<script setup lang="ts">
// WindTerm 控制台（W4 托管批 · N8，共享契约形态照抄 FlClashView）：
// 业务投影全部收进 src/adapters/windterm（RPC/事件/文案/confirm-force 往返），
// ManagedConsoleShell 管页头页签骨架，ManagedControlBar 管状态头与启停钮区，
// ManagedVersionPanel 管版本区（标准远程表：版本/状态/大小/发布时间）。
// 本视图仅剩装配与"关于"说明卡。
import { createWindTermAdapter } from '../adapters/windterm'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createWindTermAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="windterm-view"
    :adapter="adapter"
    title="WindTerm"
    subtitle="托管 WindTerm 终端：SSH/Sftp/Shell/Telnet/Serial 会话在其自有窗口进行。"
    console-tab-label="🖥 控制台"
    :banner-slim="false"
  >
    <details class="info-details">
      <summary class="info-summary">关于 WindTerm 托管</summary>
      <div class="info-body">
        <p>WindTerm 是跨平台专业终端（Qt 构建，官方 Windows 包即便携免安装）（上游 <a class="inline-link" href="https://github.com/kingToolbox/WindTerm" target="_blank" rel="noopener">kingToolbox/WindTerm</a>：README 自述完全免费使用，开源部分 Apache-2.0、仓库根无 LICENSE 文件）。本模块仅转链官方 GitHub Releases 托管其运行：启停受 JobObject 管控。</p>
        <p class="hint-dim">上游未发布官方哈希（实测），下载完整性走降级三层：字节数 + zip CRC 闸门 + 布局自检，自算摘要入账本防内容漂移；你在 Hanxi 之外自行启动的 WindTerm 按会话类风险档治理——优雅优先，强杀必经你确认。</p>
        <p class="hint-dim">portable 会话与密钥索引数据保存在版本目录内：卸载版本前请先「打开位置」迁出。</p>
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
