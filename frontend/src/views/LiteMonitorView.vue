<script setup lang="ts">
// LiteMonitor 托管工作台（Wave 5 · 批 1 收敛件）：
// 共享面全部收敛进 components/managed 托管控制台家族——adapter（src/adapters/litemonitor）
// 承载业务投影（RPC/事件/文案），ManagedConsoleShell 管页头与页签骨架。
// 本模块特化：GetRuntimeStatus 第三路数据经 adapter 以快照扩展字段 hasDesktop8
// 并入（视图生命周期内单发探测），缺 .NET 8 桌面运行时的常驻警示与 740 提权
// 直拒后的一键提权重启入口走 #console-extra 槽（状态头之后、说明卡之前）；
// 辅助卡「📂 打开位置」为便携目录直达钮（adapter 镜像三源解析目标版本）。
import { createLiteMonitorAdapter } from '../adapters/litemonitor'
import type { LmConsoleSnapshot } from '../adapters/litemonitor'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import ElevateRestart from '../components/ElevateRestart.vue'
import type { ManagedSnapshot } from '../components/managed/adapter'

const adapter = createLiteMonitorAdapter()

// 环境提示现态（迁移前 runtimeMissing computed 的投影形）：仅在探测明确报缺时警示
function isRuntimeMissing(snap: ManagedSnapshot | null): boolean {
  return (snap as LmConsoleSnapshot | null)?.hasDesktop8 === false
}

// 740 提权直拒（后端 elevateHint 文案统一含"管理员"）→ 追加一键提权重启入口
function needsElevate(snap: ManagedSnapshot | null, state: string): boolean {
  return state === 'failed' && !!snap && (snap.error || '').includes('管理员')
}
</script>

<template>
  <ManagedConsoleShell
    class="litemonitor-view"
    :adapter="adapter"
    title="LiteMonitor"
    subtitle="托管桌面硬件监控 LiteMonitor：版本管理、JobObject 启停与监控条唤起。"
    console-tab-label="⚡ 控制台"
  >
    <!-- 控制台内联大件：缺运行库常驻警示（独立于状态横幅）+ 提权重启入口 -->
    <template #console-extra="{ snap, state }">
      <UiBanner v-if="isRuntimeMissing(snap)" tone="warn" class="slim">
        未检测到 .NET 8 桌面运行时：LiteMonitor 为框架依赖发布，缺少运行库将无法启动。可在「开发环境检测」页查看详情或前往微软官网安装。
      </UiBanner>
      <ElevateRestart v-if="needsElevate(snap, state)" route="/ext/litemonitor" />
    </template>

    <!-- 控制台 Tab 主体：说明卡（折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">关于 LiteMonitor</summary>
      <div class="info-body">
        <p>
          LiteMonitor 是轻量可定制的 Windows 桌面/任务栏硬件监控（CPU/GPU/内存/磁盘/网速/FPS/插件），
          上游 <a class="inline-link" href="https://github.com/Diorser/LiteMonitor" target="_blank" rel="noopener">Diorser/LiteMonitor</a>（C# WinForms，.NET 8）。
          本模块仅托管其运行：版本下载自官方 GitHub Releases（官方 sha256 四层校验），启停受 JobObject 管控。
        </p>
        <p class="hint-dim">
          settings.json/主题/插件随各版本隔离目录保存；首次托管启动时 Hanxi 会自动关闭其内置更新检查（版本升级走本页「版本管理」）。
          已装多个版本时，在「版本管理」设为使用即可切换。
        </p>
        <p class="hint-dim">
          注意：上游仓库暂未提供开源 LICENSE，本模块仅代为下载托管官方发布，不镜像分发其代码与二进制。
        </p>
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
