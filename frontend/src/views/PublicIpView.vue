<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import * as PublicIpAPI from '../../bindings/hubkit/internal/modules/publicip'
import type { NetworkOverview, PingSummary, TracerouteSummary } from '../../bindings/hubkit/internal/modules/publicip/models'
import type { Adapter } from '../../bindings/hubkit/internal/platform/models'

const activeTab = ref<'ip' | 'ping' | 'traceroute'>('ip')

// 1. IP 概览数据
const overview = ref<NetworkOverview | null>(null)
const loading = ref(false)
const errorMsg = ref('')
const toastMsg = ref('')

// 2. Ping 探测状态
const pingTargetInput = ref('1.1.1.1')
const pingCount = ref(4)
const pingLoading = ref(false)
const pingResult = ref<PingSummary | null>(null)
const pingError = ref('')

// 3. 路由追踪状态
const traceTargetInput = ref('1.1.1.1')
const traceMaxHops = ref(20)
const traceLoading = ref(false)
const traceResult = ref<TracerouteSummary | null>(null)
const traceError = ref('')

async function loadNetworkInfo(force = false) {
  loading.value = true
  errorMsg.value = ''
  try {
    overview.value = await PublicIpAPI.PublicIPService.GetNetworkOverview(force)
  } catch (e: any) {
    errorMsg.value = `获取网络配置信息失败: ${e?.message ?? e}`
  } finally {
    loading.value = false
  }
}

async function runPing() {
  if (!pingTargetInput.value.trim()) return
  pingLoading.value = true
  pingError.value = ''
  try {
    pingResult.value = await PublicIpAPI.PublicIPService.PingTarget(pingTargetInput.value.trim(), pingCount.value)
  } catch (e: any) {
    pingError.value = `Ping 执行失败: ${e?.message ?? e}`
  } finally {
    pingLoading.value = false
  }
}

async function runTraceroute() {
  if (!traceTargetInput.value.trim()) return
  traceLoading.value = true
  traceError.value = ''
  try {
    traceResult.value = await PublicIpAPI.PublicIPService.TraceRoute(traceTargetInput.value.trim(), traceMaxHops.value)
  } catch (e: any) {
    traceError.value = `路由追踪执行失败: ${e?.message ?? e}`
  } finally {
    traceLoading.value = false
  }
}

function copyText(text: string, label: string) {
  if (!text) return
  navigator.clipboard.writeText(text)
  toastMsg.value = `已复制 ${label}: ${text}`
  setTimeout(() => {
    toastMsg.value = ''
  }, 2000)
}

function quickPing(ip: string) {
  pingTargetInput.value = ip
  activeTab.value = 'ping'
  runPing()
}

function quickTrace(ip: string) {
  traceTargetInput.value = ip
  activeTab.value = 'traceroute'
  runTraceroute()
}

// 过滤出有意义的活跃网卡
const activeAdapters = computed<Adapter[]>(() => {
  if (!overview.value?.adapters) return []
  return overview.value.adapters.filter(a => {
    if (a.isLoopback) return false
    return (a.ipv4 && a.ipv4.length > 0) || (a.ipv6 && a.ipv6.length > 0)
  })
})

onMounted(() => loadNetworkInfo(false))
</script>

<template>
  <section class="page network-page">
    <div class="header-row">
      <div>
        <h1>网络与 IP 诊断</h1>
        <p class="subtitle">综合展示公网/内网 IP、网卡与网关 DNS，提供毫秒级 ICMP Ping 探测与路由跳跃追踪 (Traceroute)。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

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
      <!-- 顶部状态栏 -->
      <div class="control-panel">
        <div class="meta-info">
          <span v-if="overview?.fetchedAt">最后探测时间: {{ overview.fetchedAt }}</span>
          <span v-else>等待探测…</span>
        </div>
        <button class="btn btn-primary" :disabled="loading" @click="loadNetworkInfo(true)">
          {{ loading ? '探测中…' : '强制刷新' }}
        </button>
      </div>

      <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

      <!-- 1. 公网出口 IP 卡片区域 -->
      <div class="section-title">
        <h3>出口公网 IP</h3>
      </div>
      <div class="cards-grid">
        <!-- 公网 IPv4 卡片 -->
        <div class="ip-card">
          <div class="card-header">
            <div class="tag-row">
              <span class="card-tag v4">公网 IPv4</span>
              <span class="badge badge-sub" v-if="overview?.publicIpv4">家宽 / 专线</span>
            </div>
            <span class="provider-label" v-if="overview?.sourceV4">来源: {{ overview.sourceV4 }}</span>
          </div>
          <div class="card-body">
            <div v-if="loading && !overview" class="ip-placeholder">正在探测公网 IPv4…</div>
            <div v-else-if="overview?.publicIpv4" class="ip-value">
              <code>{{ overview.publicIpv4 }}</code>
            </div>
            <div v-else class="ip-empty">未获取到公网 IPv4</div>
          </div>
          <div class="card-footer">
            <button
              class="btn-copy"
              :disabled="!overview?.publicIpv4"
              @click="quickPing(overview?.publicIpv4 ?? '')"
            >
              ⚡ Ping 测试
            </button>
            <button
              class="btn-copy"
              :disabled="!overview?.publicIpv4"
              @click="copyText(overview?.publicIpv4 ?? '', '公网 IPv4')"
            >
              复制 IPv4
            </button>
          </div>
        </div>

        <!-- 公网 IPv6 卡片 -->
        <div class="ip-card">
          <div class="card-header">
            <div class="tag-row">
              <span class="card-tag v6">公网 IPv6</span>
              <span class="badge badge-success" v-if="overview?.publicIpv6">已开通 IPv6</span>
            </div>
            <span class="provider-label" v-if="overview?.sourceV6">来源: {{ overview.sourceV6 }}</span>
          </div>
          <div class="card-body">
            <div v-if="loading && !overview" class="ip-placeholder">正在探测公网 IPv6…</div>
            <div v-else-if="overview?.publicIpv6" class="ip-value">
              <code>{{ overview.publicIpv6 }}</code>
            </div>
            <div v-else class="ip-empty">未检测到公网 IPv6（当前网络可能未开通或被防火墙拦截）</div>
          </div>
          <div class="card-footer">
            <button
              class="btn-copy"
              :disabled="!overview?.publicIpv6"
              @click="copyText(overview?.publicIpv6 ?? '', '公网 IPv6')"
            >
              复制 IPv6
            </button>
          </div>
        </div>
      </div>

      <!-- 2. 本机网络适配器详情卡片列表 -->
      <div class="section-title">
        <h3>网卡与局域网配置</h3>
      </div>

      <div class="adapters-list">
        <div v-for="adapter in activeAdapters" :key="adapter.name" class="adapter-card">
          <div class="adapter-header">
            <div class="adapter-title-box">
              <span class="adapter-name">{{ adapter.name }}</span>
              <span v-if="adapter.description && adapter.description !== adapter.name" class="adapter-desc">
                {{ adapter.description }}
              </span>
            </div>
            <div class="adapter-tags">
              <span v-if="adapter.isPhysical" class="badge badge-physical">物理网卡</span>
              <span v-else class="badge badge-virtual">虚拟 / 隧道</span>
              <span class="mac-text" v-if="adapter.mac">MAC: {{ adapter.mac }}</span>
            </div>
          </div>

          <div class="adapter-body">
            <!-- 局域网 IPv4 -->
            <div class="info-group">
              <span class="group-label">局域网 IPv4:</span>
              <div class="items-list">
                <div v-for="ip in adapter.ipv4" :key="ip" class="ip-chip">
                  <code>{{ ip }}</code>
                  <button class="chip-action" title="Ping 测试" @click="quickPing(ip)">⚡</button>
                  <button class="chip-copy" title="复制" @click="copyText(ip, '局域网 IP')">⧉</button>
                </div>
                <span v-if="!adapter.ipv4 || adapter.ipv4.length === 0" class="muted-text">无</span>
              </div>
            </div>

            <!-- 默认网关 -->
            <div class="info-group">
              <span class="group-label">默认网关:</span>
              <div class="items-list">
                <div v-if="adapter.gateway" class="ip-chip">
                  <code>{{ adapter.gateway }}</code>
                  <button class="chip-action" title="Ping 网关" @click="quickPing(adapter.gateway)">⚡</button>
                  <button class="chip-copy" title="复制" @click="copyText(adapter.gateway, '默认网关')">⧉</button>
                </div>
                <div v-if="adapter.ipv6Gateway" class="ip-chip">
                  <span class="v6-tag">v6</span>
                  <code>{{ adapter.ipv6Gateway }}</code>
                  <button class="chip-copy" title="复制" @click="copyText(adapter.ipv6Gateway, 'IPv6 网关')">⧉</button>
                </div>
                <span v-if="!adapter.gateway && !adapter.ipv6Gateway" class="muted-text">—</span>
              </div>
            </div>

            <!-- DNS 服务器 -->
            <div class="info-group">
              <span class="group-label">DNS 服务器:</span>
              <div class="items-list">
                <div v-for="dns in adapter.dnsServers" :key="dns" class="ip-chip dns-chip">
                  <code>{{ dns }}</code>
                  <button class="chip-action" title="Ping DNS" @click="quickPing(dns)">⚡</button>
                  <button class="chip-copy" title="复制" @click="copyText(dns, 'DNS')">⧉</button>
                </div>
                <span v-if="!adapter.dnsServers || adapter.dnsServers.length === 0" class="muted-text">—</span>
              </div>
            </div>

            <!-- IPv6 地址 -->
            <div class="info-group full-width" v-if="adapter.ipv6Details && adapter.ipv6Details.length > 0">
              <span class="group-label">IPv6 地址列表:</span>
              <div class="v6-items-list">
                <div
                  v-for="v6 in adapter.ipv6Details"
                  :key="v6.address"
                  class="v6-chip"
                  :class="{ 'is-temp': v6.isTemporary, 'is-linklocal': v6.type === 'LinkLocal' }"
                >
                  <span class="v6-badge" v-if="v6.isTemporary">临时隐私地址</span>
                  <span class="v6-badge linklocal" v-else-if="v6.type === 'LinkLocal'">链路本地 (Link-Local)</span>
                  <span class="v6-badge main" v-else>全局主地址</span>

                  <code>{{ v6.address }}</code>
                  <button class="chip-copy" title="复制" @click="copyText(v6.address, 'IPv6')">⧉</button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="activeAdapters.length === 0 && !loading" class="empty-hint">
          未检测到活跃网卡网络信息
        </div>
      </div>
    </template>

    <!-- ==================== Tab 2: Ping 连通性测试 ==================== -->
    <template v-else-if="activeTab === 'ping'">
      <div class="tool-panel">
        <div class="tool-form">
          <div class="input-wrap">
            <span class="input-label">目标 IP / 域名:</span>
            <input
              v-model="pingTargetInput"
              class="text-input"
              placeholder="如 1.1.1.1、8.8.8.8 或 www.baidu.com"
              @keyup.enter="runPing"
            />
          </div>
          <div class="input-wrap count-wrap">
            <span class="input-label">次数:</span>
            <select v-model="pingCount" class="select-input">
              <option :value="4">4 次</option>
              <option :value="8">8 次</option>
              <option :value="16">16 次</option>
            </select>
          </div>
          <button class="btn btn-primary" :disabled="pingLoading || !pingTargetInput.trim()" @click="runPing">
            {{ pingLoading ? 'Ping 探测中…' : '发起 Ping' }}
          </button>
        </div>

        <!-- 快捷常用目标 -->
        <div class="quick-targets">
          <span class="quick-label">常用目标:</span>
          <button class="btn-quick" @click="pingTargetInput = '223.5.5.5'; runPing()">阿里 DNS (223.5.5.5)</button>
          <button class="btn-quick" @click="pingTargetInput = '119.29.29.29'; runPing()">腾讯 DNS (119.29.29.29)</button>
          <button class="btn-quick" @click="pingTargetInput = '1.1.1.1'; runPing()">Cloudflare (1.1.1.1)</button>
          <button class="btn-quick" @click="pingTargetInput = '8.8.8.8'; runPing()">Google (8.8.8.8)</button>
        </div>
      </div>

      <div v-if="pingError" class="error-box">{{ pingError }}</div>

      <!-- Ping 结果面板 -->
      <div v-if="pingResult" class="diag-card">
        <div class="diag-summary-bar">
          <div class="summary-col">
            <span class="s-label">目标主机</span>
            <span class="s-val">{{ pingResult.target }} <code v-if="pingResult.ip !== pingResult.target">({{ pingResult.ip }})</code></span>
          </div>
          <div class="summary-col">
            <span class="s-label">发包 / 收包</span>
            <span class="s-val">{{ pingResult.sent }} / {{ pingResult.received }}</span>
          </div>
          <div class="summary-col">
            <span class="s-label">丢包率</span>
            <span class="s-val" :class="{ 'val-warn': pingResult.lossRate > 0 }">{{ pingResult.lossRate.toFixed(1) }}%</span>
          </div>
          <div class="summary-col" v-if="pingResult.received > 0">
            <span class="s-label">延迟 (最小/平均/最大)</span>
            <span class="s-val text-success">{{ pingResult.minRtt.toFixed(1) }} / {{ pingResult.avgRtt.toFixed(1) }} / {{ pingResult.maxRtt.toFixed(1) }} ms</span>
          </div>
        </div>

        <div class="table-container">
          <table class="diag-tbl">
            <thead>
              <tr>
                <th style="width: 80px;">序号</th>
                <th>目标 IP</th>
                <th style="width: 120px;">往返耗时 (RTT)</th>
                <th style="width: 100px;">TTL</th>
                <th>结果状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in pingResult.results" :key="r.seq">
                <td>#{{ r.seq }}</td>
                <td><code>{{ r.ip }}</code></td>
                <td>
                  <span v-if="r.success" class="rtt-tag" :class="r.rttMs < 50 ? 'fast' : r.rttMs < 150 ? 'medium' : 'slow'">
                    {{ r.rttMs.toFixed(1) }} ms
                  </span>
                  <span v-else class="text-danger">—</span>
                </td>
                <td>{{ r.success ? r.ttl : '—' }}</td>
                <td>
                  <span v-if="r.success" class="status-badge ok">成功回复</span>
                  <span v-else class="status-badge fail">{{ r.errorMsg || '请求超时' }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- ==================== Tab 3: 路由追踪 ==================== -->
    <template v-else-if="activeTab === 'traceroute'">
      <div class="tool-panel">
        <div class="tool-form">
          <div class="input-wrap">
            <span class="input-label">目标 IP / 域名:</span>
            <input
              v-model="traceTargetInput"
              class="text-input"
              placeholder="如 1.1.1.1、8.8.8.8 或 www.taobao.com"
              @keyup.enter="runTraceroute"
            />
          </div>
          <div class="input-wrap count-wrap">
            <span class="input-label">最大跳数:</span>
            <select v-model="traceMaxHops" class="select-input">
              <option :value="15">15 跳</option>
              <option :value="20">20 跳</option>
              <option :value="30">30 跳</option>
            </select>
          </div>
          <button class="btn btn-primary" :disabled="traceLoading || !traceTargetInput.trim()" @click="runTraceroute">
            {{ traceLoading ? '正在追踪路由节点…' : '开始追踪' }}
          </button>
        </div>

        <!-- 快捷常用目标 -->
        <div class="quick-targets">
          <span class="quick-label">常用目标:</span>
          <button class="btn-quick" @click="traceTargetInput = '223.5.5.5'; runTraceroute()">阿里 DNS (223.5.5.5)</button>
          <button class="btn-quick" @click="traceTargetInput = '1.1.1.1'; runTraceroute()">Cloudflare (1.1.1.1)</button>
          <button class="btn-quick" @click="traceTargetInput = '8.8.8.8'; runTraceroute()">Google (8.8.8.8)</button>
        </div>
      </div>

      <div v-if="traceError" class="error-box">{{ traceError }}</div>

      <!-- 路由追踪结果 -->
      <div v-if="traceResult" class="diag-card">
        <div class="diag-summary-bar">
          <div class="summary-col">
            <span class="s-label">追踪目标</span>
            <span class="s-val">{{ traceResult.target }} <code>({{ traceResult.ip }})</code></span>
          </div>
          <div class="summary-col">
            <span class="s-label">总跳数</span>
            <span class="s-val">{{ traceResult.hops ? traceResult.hops.length : 0 }} 跳</span>
          </div>
          <div class="summary-col">
            <span class="s-label">追踪状态</span>
            <span class="s-val" :class="traceResult.complete ? 'text-success' : 'text-warn'">
              {{ traceResult.complete ? '已到达目标主机' : '追踪结束 (未完全响应)' }}
            </span>
          </div>
        </div>

        <div class="table-container">
          <table class="diag-tbl">
            <thead>
              <tr>
                <th style="width: 80px;">跳数</th>
                <th>跳跃节点 IP</th>
                <th style="width: 140px;">往返延迟 (RTT)</th>
                <th>节点状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="h in traceResult.hops" :key="h.hop">
                <td><strong>#{{ h.hop }}</strong></td>
                <td>
                  <code v-if="h.ip !== '*'" class="node-ip">{{ h.ip }}</code>
                  <span v-else class="text-subtle">* * * (节点不响应 ICMP)</span>
                </td>
                <td>
                  <span v-if="h.success" class="rtt-tag" :class="h.rttMs < 50 ? 'fast' : h.rttMs < 150 ? 'medium' : 'slow'">
                    {{ h.rttMs.toFixed(1) }} ms
                  </span>
                  <span v-else class="text-subtle">—</span>
                </td>
                <td>
                  <span v-if="h.ip === traceResult.ip" class="status-badge target">🎯 最终目标</span>
                  <span v-else-if="h.success" class="status-badge ok">路由正常</span>
                  <span v-else class="status-badge timeout">请求超时</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.network-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.subtitle {
  color: var(--text-muted);
  font-size: 13px;
  margin: 4px 0 0;
}

.toast {
  background: var(--text-main);
  color: #fff;
  padding: 6px 14px;
  border-radius: 6px;
  font-size: 12px;
  animation: fadeIn 0.2s ease;
}

/* Tab 切换栏 */
.nav-tabs {
  display: flex;
  gap: 8px;
  border-bottom: 1px solid var(--border-color);
  padding-bottom: 8px;
}
.tab-item {
  background: transparent;
  border: 1px solid transparent;
  padding: 6px 14px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}
.tab-item:hover {
  background: var(--bg-hover);
  color: var(--text-main);
}
.tab-item.active {
  background: #fff;
  border-color: var(--border-color);
  color: var(--accent);
  font-weight: 600;
  box-shadow: 0 1px 2px rgba(0,0,0,0.05);
}

.control-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  padding: 12px 16px;
  border-radius: 8px;
}

.meta-info {
  font-size: 13px;
  color: var(--text-muted);
}

.section-title {
  margin-top: 6px;
}

.section-title h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 0;
}

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
  gap: 16px;
}

.ip-card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.tag-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.card-tag {
  font-size: 12px;
  font-weight: 600;
  padding: 3px 8px;
  border-radius: 4px;
}
.card-tag.v4 {
  background: #ddf4ff;
  color: #0969da;
}
.card-tag.v6 {
  background: #fff8c5;
  color: #9a6700;
}

.provider-label {
  font-size: 11px;
  color: var(--text-subtle);
}

.card-body {
  padding: 8px 0;
  min-height: 48px;
  display: flex;
  align-items: center;
}

.ip-value code {
  font-family: Consolas, monospace;
  font-size: 18px;
  font-weight: 700;
  color: var(--text-main);
  background: var(--bg-hover);
  padding: 4px 10px;
  border-radius: 6px;
  word-break: break-all;
}

.ip-placeholder, .ip-empty {
  font-size: 13px;
  color: var(--text-subtle);
}

.card-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  border-top: 1px solid var(--border-color);
  padding-top: 8px;
}

.btn-copy {
  padding: 4px 12px;
  border: 1px solid var(--border-color);
  border-radius: 5px;
  background: #fff;
  cursor: pointer;
  font-size: 12px;
  transition: all 0.15s ease;
}
.btn-copy:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}
.btn-copy:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* 适配器卡片列表 */
.adapters-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.adapter-card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.adapter-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  border-bottom: 1px solid var(--border-color);
  padding-bottom: 10px;
}

.adapter-title-box {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.adapter-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-main);
}

.adapter-desc {
  font-size: 12px;
  color: var(--text-subtle);
}

.adapter-tags {
  display: flex;
  align-items: center;
  gap: 10px;
}

.mac-text {
  font-family: Consolas, monospace;
  font-size: 12px;
  color: var(--text-muted);
}

.adapter-body {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 14px;
}

.info-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.info-group.full-width {
  grid-column: 1 / -1;
}

.group-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
}

.items-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.ip-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--bg-app);
  border: 1px solid var(--border-color);
  padding: 3px 8px;
  border-radius: 5px;
  font-size: 12px;
}

.ip-chip code {
  font-family: Consolas, monospace;
  font-weight: 600;
}

.chip-action {
  background: transparent;
  border: none;
  cursor: pointer;
  font-size: 11px;
  color: var(--accent);
  padding: 0 2px;
}
.chip-action:hover {
  transform: scale(1.15);
}

.chip-copy {
  background: transparent;
  border: none;
  cursor: pointer;
  font-size: 12px;
  color: var(--text-subtle);
  padding: 0 2px;
}
.chip-copy:hover {
  color: var(--accent);
}

.v6-items-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.v6-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  background: var(--bg-app);
  border: 1px solid var(--border-color);
  padding: 4px 10px;
  border-radius: 5px;
  font-size: 12px;
}

.v6-chip.is-temp {
  border-color: rgba(9, 105, 218, 0.3);
  background: #f0f8ff;
}

.v6-chip.is-linklocal {
  border-color: var(--border-color);
  opacity: 0.85;
}

.v6-chip code {
  font-family: Consolas, monospace;
  font-weight: 600;
  word-break: break-all;
}

.v6-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 600;
  background: #ddf4ff;
  color: #0969da;
}
.v6-badge.linklocal {
  background: var(--bg-hover);
  color: var(--text-muted);
}
.v6-badge.main {
  background: #dafbe1;
  color: var(--success);
}

.v6-tag {
  font-size: 10px;
  font-weight: 600;
  color: #9a6700;
  background: #fff8c5;
  padding: 1px 4px;
  border-radius: 3px;
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 12px;
  font-weight: 500;
}
.badge-physical {
  background: #dafbe1;
  color: var(--success);
}
.badge-virtual {
  background: var(--bg-hover);
  color: var(--text-muted);
}
.badge-sub {
  background: var(--bg-hover);
  color: var(--text-muted);
}
.badge-success {
  background: #dafbe1;
  color: var(--success);
}

.muted-text {
  font-size: 12px;
  color: var(--text-subtle);
}

.empty-hint {
  text-align: center;
  padding: 32px;
  color: var(--text-subtle);
  font-size: 13px;
}

.error-box {
  padding: 10px 14px;
  background: #ffebe9;
  color: var(--danger);
  border: 1px solid rgba(207, 34, 46, 0.2);
  border-radius: 6px;
  font-size: 13px;
}

.btn {
  padding: 6px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border: 1px solid transparent;
  transition: all 0.15s ease;
}
.btn-primary {
  background: var(--accent);
  color: #fff;
}
.btn-primary:hover:not(:disabled) {
  background: var(--accent-hover);
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 诊断工具控制条 */
.tool-panel {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.tool-form {
  display: flex;
  gap: 10px;
  align-items: center;
}
.input-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
}
.input-wrap.count-wrap {
  flex: 0 0 140px;
}
.input-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  white-space: nowrap;
}
.text-input {
  width: 100%;
  padding: 6px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 13px;
}
.select-input {
  width: 100%;
  padding: 6px 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 13px;
  background: #fff;
}

.quick-targets {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
.quick-label {
  color: var(--text-subtle);
}
.btn-quick {
  background: #fff;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
}
.btn-quick:hover {
  border-color: var(--accent);
  color: var(--accent);
}

/* 诊断结果卡片 */
.diag-card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px 16px;
}
.diag-summary-bar {
  display: flex;
  gap: 24px;
  background: var(--bg-app);
  padding: 10px 14px;
  border-radius: 6px;
  border: 1px solid var(--border-color);
}
.summary-col {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.s-label {
  font-size: 11px;
  color: var(--text-subtle);
}
.s-val {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-main);
}
.val-warn {
  color: var(--danger);
}
.text-success {
  color: var(--success);
}
.text-warn {
  color: #9a6700;
}
.text-danger {
  color: var(--danger);
}
.text-subtle {
  color: var(--text-subtle);
}

.table-container {
  border: 1px solid var(--border-color);
  border-radius: 6px;
  overflow: hidden;
}
.diag-tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  text-align: left;
}
.diag-tbl th {
  background: var(--bg-app);
  padding: 8px 12px;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border-color);
}
.diag-tbl td {
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-color);
  color: var(--text-main);
}
.diag-tbl tr:last-child td {
  border-bottom: none;
}

.rtt-tag {
  font-family: Consolas, monospace;
  font-weight: 600;
  font-size: 12px;
}
.rtt-tag.fast {
  color: var(--success);
}
.rtt-tag.medium {
  color: #9a6700;
}
.rtt-tag.slow {
  color: var(--danger);
}

.node-ip {
  font-family: Consolas, monospace;
  font-weight: 600;
}

.status-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  font-weight: 500;
}
.status-badge.ok {
  background: #dafbe1;
  color: var(--success);
}
.status-badge.fail {
  background: #ffebe9;
  color: var(--danger);
}
.status-badge.timeout {
  background: var(--bg-hover);
  color: var(--text-subtle);
}
.status-badge.target {
  background: #ddf4ff;
  color: #0969da;
  font-weight: 600;
}
</style>
