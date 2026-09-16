<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import { usePublicIpOverview } from '../composables/usePublicIpOverview'
import { usePublicIpPing, usePublicIpTraceroute } from '../composables/usePublicIpDiagnostics'
import PageHeader from '../components/ui/PageHeader.vue'
import IpOverviewPanel from '../components/publicip/IpOverviewPanel.vue'
import PingPanel from '../components/publicip/PingPanel.vue'
import TraceroutePanel from '../components/publicip/TraceroutePanel.vue'

// Phase 6 结构拆分后的编排层：只留三 Tab 状态机、跨 Tab 快捷联动与复制反馈；
// 领域状态见 composables/publicIp{Overview,Diagnostics}.ts，区块 DOM 见 components/publicip/*。
const { showToast } = useToast()
const { copy } = useClipboard()

const activeTab = ref<'ip' | 'ping' | 'traceroute'>('ip')

// 1. IP 概览数据
const { overview, loading, errorMsg, activeAdapters, loadNetworkInfo } = usePublicIpOverview()

// 2. Ping 探测状态
const {
  targetInput: pingTargetInput,
  count: pingCount,
  loading: pingLoading,
  result: pingResult,
  error: pingError,
  run: runPing,
} = usePublicIpPing()

// 3. 路由追踪状态
const {
  targetInput: traceTargetInput,
  maxHops: traceMaxHops,
  loading: traceLoading,
  result: traceResult,
  error: traceError,
  run: runTraceroute,
} = usePublicIpTraceroute()

async function copyText(text: string, label: string) {
  if (!text) return
  // 剪贴板两级策略收编进 useClipboard；失败不再谎报成功
  const ok = await copy(text)
  showToast(ok ? `已复制 ${label}: ${text}` : '复制失败')
}

// 快捷 Ping：回填目标并跨 Tab 立即发起；探测经 runPing()（即 composable 的 run()），
// 与面板按钮共用同一在飞单飞守卫，连点 ⚡ 不会并发重入。
// （Phase 6 曾登记的同族死码 quickTrace 已于本次交互整改删除。）
function quickPing(ip: string) {
  pingTargetInput.value = ip
  activeTab.value = 'ping'
  runPing()
}

onMounted(() => loadNetworkInfo(false))
</script>

<template>
  <section class="page network-page">
    <PageHeader title="网络与 IP 诊断" subtitle="综合展示公网/内网 IP、网卡与网关 DNS，提供毫秒级 ICMP Ping 探测与路由跳跃追踪 (Traceroute)。" />

    <!-- 顶部导航 Tab 分页 -->
    <div class="nav-tabs">
      <button
        class="tab-item"
        :class="{ active: activeTab === 'ip' }"
        @click="activeTab = 'ip'"
      >
        <span>≋ IP 与网卡查看</span>
      </button>
      <button
        class="tab-item"
        :class="{ active: activeTab === 'ping' }"
        @click="activeTab = 'ping'"
      >
        <span>⚡ Ping 连通性测试</span>
      </button>
      <button
        class="tab-item"
        :class="{ active: activeTab === 'traceroute' }"
        @click="activeTab = 'traceroute'"
      >
        <span>🧭 路由追踪 (Traceroute)</span>
      </button>
    </div>

    <!-- ==================== Tab 1: IP 查看 ==================== -->
    <template v-if="activeTab === 'ip'">
      <IpOverviewPanel
        :overview="overview"
        :loading="loading"
        :error="errorMsg"
        :adapters="activeAdapters"
        :ping-busy="pingLoading"
        @refresh="loadNetworkInfo(true)"
        @quick-ping="quickPing"
        @copy-text="copyText"
      />
    </template>

    <!-- ==================== Tab 2: Ping 连通性测试 ==================== -->
    <template v-else-if="activeTab === 'ping'">
      <PingPanel
        v-model:target="pingTargetInput"
        v-model:count="pingCount"
        :loading="pingLoading"
        :result="pingResult"
        :error="pingError"
        @run="runPing"
      />
    </template>

    <!-- ==================== Tab 3: 路由追踪 ==================== -->
    <template v-else-if="activeTab === 'traceroute'">
      <TraceroutePanel
        v-model:target="traceTargetInput"
        v-model:max-hops="traceMaxHops"
        :loading="traceLoading"
        :result="traceResult"
        :error="traceError"
        @run="runTraceroute"
      />
    </template>
  </section>
</template>

<style scoped>
/* 页头/错误框/按钮家族/表格基样式/徽标(badge→全局 chip)由 PageHeader 与 components.css 原子接管。
   nav-tabs 为下划线形变体（非 MainTabNav 胶囊形），按手册§9-4 不硬塞、保留视图独有样式并语义 token 化。
   （原 .toast/.header-row/.subtitle 死代码与重复原子已删；status-badge 因承载动态错误文本、
   避免 chip 的 nowrap 截断，保留为变体并随区块迁入 PingPanel/TraceroutePanel；
   Phase 6 拆分后本层只留页面骨架与 Tab 栏，其余 scoped 样式随标记迁往 components/publicip/*） */
.network-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* Tab 切换栏（下划线变体，视图独有） */
.nav-tabs {
  display: flex;
  gap: 8px;
  border-bottom: 1px solid var(--color-border);
  padding-bottom: 8px;
}
.tab-item {
  background: transparent;
  border: 1px solid transparent;
  padding: 6px 14px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text-muted);
  cursor: pointer;
  transition: all var(--motion-base) ease;
}
.tab-item:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}
.tab-item.active {
  background: var(--surface-panel);
  border-color: var(--color-border);
  color: var(--color-primary);
  font-weight: 600;
  box-shadow: var(--shadow-small);
}
</style>
