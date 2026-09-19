<script setup lang="ts">
// PicLite 压图托管工作台（Wave 5 · 批 1 · MSI 提取族收敛件）：
// 业务投影全部收进 src/adapters/piclite（RPC/事件/文案），本页仅剩装配与
// 业务说明卡（默认槽内容）；状态轮询/uptime/下载进度/启停/版本区/联动卡
// 由 components/managed 托管控制台家族单源承载（照抄 ccswitch 黄金样本形）。
// MSI 特殊安装文案走 adapter copy 钩子；表内标准词按批 0 契约定档。
import { createPicLiteAdapter } from '../adapters/piclite'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createPicLiteAdapter()
</script>

<template>
  <ManagedConsoleShell
    class="piclite-view"
    :adapter="adapter"
    title="PicLite 压图"
    subtitle="托管本地优先图片/GIF 压缩工具 PicLite 图轻：官方 MSI 免安装提取、JobObject 启停与窗口唤起；压缩/悬浮/图床操作在 PicLite 自有界面完成。"
    console-tab-label="🖼️ 控制台"
  >
    <!-- 控制台 Tab 主体：说明卡（可折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">什么是 PicLite</summary>
      <div class="info-body">
        <p>开源跨平台本地图片/GIF 压缩工具（<a class="inline-link" href="https://github.com/amiaoapp/PicLite" target="_blank" rel="noopener">amiaoapp/PicLite</a>，GPL-3.0，Tauri 2）：智能择优压缩、悬浮结果窗、剪贴板/文件夹监测、图床上传，图片默认只在本机处理。</p>
        <p class="hint-dim">上游只发布安装器没有便携版：托管安装走 msiexec 管理提取（不写注册表、不提权），启停受 JobObject 管控；PicLite 关窗即驻托盘，退出无外部优雅通道，托管「退出」为直接终止。</p>
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
