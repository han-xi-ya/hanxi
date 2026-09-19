<script setup lang="ts">
// Keyviz 键显托管工作台（Wave 5 · 批 1 · MSI 提取族收敛件）：
// 业务投影全部收进 src/adapters/keyviz（RPC/事件/文案），本页仅剩装配与
// 业务说明卡（默认槽内容）；状态轮询/uptime/下载进度/启停/版本区/联动卡
// 由 components/managed 托管控制台家族单源承载（照抄 ccswitch 黄金样本形）。
// MSI 特殊安装文案走 adapter copy 钩子；表内标准词按批 0 契约定档。
// 键显特有：主钮为「▶ 启动可视化」（StartKeyviz 非唤窗），上游无桌面快捷方式。
import { createKeyvizAdapter } from '../adapters/keyviz'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createKeyvizAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="keyviz-view"
    :adapter="adapter"
    title="Keyviz 键显"
    subtitle="托管开源按键/鼠标可视化工具 Keyviz：官方 MSI 免安装提取、JobObject 启停与运行状态探测；样式设置在其托盘菜单 → Settings 完成。"
    console-tab-label="⌨️ 控制台"
  >
    <!-- 控制台 Tab 主体：说明卡（可折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">什么是 Keyviz</summary>
      <div class="info-body">
        <p>开源跨平台按键/鼠标实时可视化工具（<a class="inline-link" href="https://github.com/mulaRahul/keyviz" target="_blank" rel="noopener">mulaRahul/keyviz</a>，GPL-3.0，Tauri 2），录教程、演示、结对编程时向观众展示你按下的组合键：<kbd>Alt</kbd> + <kbd>Drag</kbd>、滚轮、鼠标点击皆可显示，样式/位置/动画/历史全可定制。</p>
        <p class="hint-dim">上游只发布安装器没有便携版：托管安装走 msiexec 管理提取（不写注册表、不提权），启停受 JobObject 管控；Keyviz 启动即驻托盘，设置窗口无程序化唤起入口（托盘图标左键即弹菜单），退出无外部优雅通道，托管「退出」为直接终止（配置修改即节流写盘，不丢设置）。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/版本区/联动卡/页签与 flex 骨架全部由 managed 组件 + components.css 全局原子接管；
   本页仅余说明卡内联链接与 kbd 键帽这两处私有形 */
.info-body kbd { font-family: var(--font-mono); font-size: var(--text-xs); background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 0 4px; }
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
