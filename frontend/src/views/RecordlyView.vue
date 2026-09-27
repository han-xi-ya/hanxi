<script setup lang="ts">
// Recordly 控制台（Wave 5 · 批 0 契约收敛件；共享契约增强批收编版；波 2D 迁壳件）：
// 状态/版本加载/安装进度 map/时长 ticker/busy 闩/启停与安装编排/联动辅助卡
// 全部收进 adapters/recordly + components/managed 家族（store 一份，控制条/
// 通道行/版本面板/辅助卡共享注入）。
// 增强批收编：方言「托管安装」表退役——单目录卡与核心互认远程表由共享
// ManagedVersionPanel 渲染（词面/封锁/阶段词经 copy+versions 钩子声明）；
// stable/beta 通道行换用 ManagedChannelRow；stopped/starting 引导行迁回
// adapter.hint（投影上下文）。视图仅余升级警告条与「什么是 Recordly」说明卡。
// 波 2D 迁壳：本视图结构 100% 壳兼容（控制条/通道行/面板/辅助卡全是壳默认件），
// 页头+页签+error-box+tab-body 的手抄壳骨退役——升级判定仍要 setup 期 store
// 句柄，走「视图自建 useManagedConsole → 传壳 :store」的波 2A 注入通道，
// 编排零改动直吃同一句柄。
// 与 CCSwitchView 的结构差异：单版本托管目录（NSIS oneClick 语义，无"设为使用"）、
// stable/beta 双通道切换、安装器未签名风险文案。
import { computed } from 'vue'
import { createRecordlyAdapter, recordlyUpgradeAvailable } from '../adapters/recordly'
import { useManagedConsole } from '../components/managed/store'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import UiBanner from '../components/ui/UiBanner.vue'

const adapter = createRecordlyAdapter()
const store = useManagedConsole(adapter)

// ---------- 派生状态（升级警告条：唯一留视图的第二横幅） ----------
const installedInfo = computed(() => store.installed[0] ?? null)
const upgradeAvailable = computed(() => {
  if (!installedInfo.value || store.releases.length === 0) return false
  return recordlyUpgradeAvailable(installedInfo.value.version, store.releases[0].version)
})
</script>

<template>
  <ManagedConsoleShell
    class="recordly-view"
    :adapter="adapter"
    :store="store"
    title="Recordly"
    subtitle="托管开源录屏工具 Recordly：版本管理、静默安装、启停与窗口唤起。"
    console-tab-label="🎬 控制台"
  >
    <!-- 控制台 Tab 主体（壳默认具位）：升级警告条（banner 第二行位）——
         共享件无第二位常驻横幅，留视图；说明卡（可折叠） -->
    <UiBanner v-if="upgradeAvailable && installedInfo" tone="warn" class="slim">
      发现可升级版本 {{ store.releases[0].version }}（当前 {{ installedInfo.version }}）——到「版本管理」一键安装，安装期间请先退出运行中的实例。
    </UiBanner>

    <details class="info-details">
      <summary class="info-summary">什么是 Recordly</summary>
      <div class="info-body">
        <p>开源演示录屏与自动剪辑工具（<a class="inline-link" href="https://github.com/webadderallorg/Recordly" target="_blank" rel="noopener">webadderallorg/Recordly</a>，AGPL-3.0）。Hanxi 仅做官方原版安装器的下载托管与启停管理，不内嵌不打包其代码。</p>
        <p class="hint-dim">上游无 Windows 免安装包：在线安装使用官方 NSIS 安装器静默装进 Hanxi 托管目录，安装器自动更新已由官方开关禁用，版本升级统一走此处版本管理。安装器未数字签名，托管直装不触发 SmartScreen；若杀毒软件误拦请把托管目录加入白名单。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/提示条/联动卡/页签与 flex 骨架由托管控制台壳 + components.css
   全局原子接管（波 2D 迁壳后视图手抄壳骨与 .recordly-view flex 副本退役）。 */
</style>
