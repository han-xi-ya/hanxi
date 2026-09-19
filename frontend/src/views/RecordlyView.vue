<script setup lang="ts">
// Recordly 控制台（Wave 5 · 批 0 契约收敛件；共享契约增强批收编版）：
// 状态/版本加载/安装进度 map/时长 ticker/busy 闩/启停与安装编排/联动辅助卡
// 全部收进 adapters/recordly + components/managed 家族（store 一份，控制条/
// 通道行/版本面板/辅助卡共享注入）。
// 增强批收编：方言「托管安装」表退役——单目录卡与核心互认远程表由共享
// ManagedVersionPanel 渲染（词面/封锁/阶段词经 copy+versions 钩子声明）；
// stable/beta 通道行换用 ManagedChannelRow；stopped/starting 引导行迁回
// adapter.hint（投影上下文）。视图仅余升级警告条与「什么是 Recordly」说明卡。
// 与 CCSwitchView 的结构差异：单版本托管目录（NSIS oneClick 语义，无"设为使用"）、
// stable/beta 双通道切换、安装器未签名风险文案。
import { computed, ref } from 'vue'
import { createRecordlyAdapter, recordlyUpgradeAvailable } from '../adapters/recordly'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedChannelRow from '../components/managed/ManagedChannelRow.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiBanner from '../components/ui/UiBanner.vue'

const adapter = createRecordlyAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 frpc/markeron/everything/ccswitch 同构）
const activeMainTab = ref<'console' | 'versions'>('console')

const MAIN_TABS = [
  { key: 'console', label: '🎬 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 派生状态（升级警告条：唯一留视图的第二横幅） ----------
const installedInfo = computed(() => store.installed[0] ?? null)
const upgradeAvailable = computed(() => {
  if (!installedInfo.value || store.releases.length === 0) return false
  return recordlyUpgradeAvailable(installedInfo.value.version, store.releases[0].version)
})
</script>

<template>
  <section class="page recordly-view">
    <PageHeader title="Recordly" subtitle="托管开源录屏工具 Recordly：版本管理、静默安装、启停与窗口唤起。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/启停钮/条件提示条/引导行全部由 ManagedControlBar（adapter 投影）承载 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" />

      <!-- 升级警告条（banner 第二行位）：共享件无第二位常驻横幅，留视图 -->
      <UiBanner v-if="upgradeAvailable && installedInfo" tone="warn" class="slim">
        发现可升级版本 {{ store.releases[0].version }}（当前 {{ installedInfo.version }}）——到「版本管理」一键安装，安装期间请先退出运行中的实例。
      </UiBanner>

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="info-summary">什么是 Recordly</summary>
        <div class="info-body">
          <p>开源演示录屏与自动剪辑工具（<a class="inline-link" href="https://github.com/webadderallorg/Recordly" target="_blank" rel="noopener">webadderallorg/Recordly</a>，AGPL-3.0）。Hanxi 仅做官方原版安装器的下载托管与启停管理，不内嵌不打包其代码。</p>
          <p class="hint-dim">上游无 Windows 免安装包：在线安装使用官方 NSIS 安装器静默装进 Hanxi 托管目录，安装器自动更新已由官方开关禁用，版本升级统一走此处版本管理。安装器未数字签名，托管直装不触发 SmartScreen；若杀毒软件误拦请把托管目录加入白名单。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关/快捷方式/数据目录/仓库，全部经 adapter.extras 投影） -->
    <ManagedExtrasCard :adapter="adapter" :store="store" />

    <!-- 版本管理 Tab：channel 共享块 + 共享面板（词面/互认/封锁全部 adapter 声明） -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedChannelRow :adapter="adapter" :store="store" />
      <ManagedVersionPanel :adapter="adapter" :store="store" />
    </div>
  </section>
</template>

<style scoped>
/* 页头/控制条/提示条/联动卡/页签与 flex 骨架由 managed 组件 + components.css
   全局原子接管；增强批收编后方言表与通道行副本（rd-ver-status/channel-row）退役。 */
.recordly-view { display: flex; flex-direction: column; gap: 10px; }

/* status-word/ver-pill/pid-tag/uptime-tag/control-btns 与状态信号灯、hint-line/
   info-* 折叠卡、control-panel/meta-info/btn-group、channel-row 家族、
   section-title/empty-hint、徽标档位色、inst-*、ver-status/dl-* 与 retry-link
   全部由共享件承载（本视图零私有形，仅余内联链接一条） */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
</style>
