<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import * as PublicIpAPI from '../../bindings/hubkit/internal/modules/publicip'
import type { NetworkOverview } from '../../bindings/hubkit/internal/modules/publicip/models'
import type { Adapter } from '../../bindings/hubkit/internal/platform/models'

const overview = ref<NetworkOverview | null>(null)
const loading = ref(false)
const errorMsg = ref('')
const toastMsg = ref('')

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

function copyText(text: string, label: string) {
  if (!text) return
  navigator.clipboard.writeText(text)
  toastMsg.value = `已复制 ${label}: ${text}`
  setTimeout(() => {
    toastMsg.value = ''
  }, 2000)
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
        <h1>IP 查看</h1>
        <p class="subtitle">综合展示公网出口 IP (IPv4/IPv6)、局域网内网 IP、临时 IPv6、默认网关与 DNS 服务器（2分钟内自动缓存，秒级呈现）。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

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

    <!-- 2. 本机网络适配器 (局域网 IP / 临时 IPv6 / 网关 / DNS) 详情卡片列表 -->
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
                <button class="chip-copy" title="复制" @click="copyText(dns, 'DNS')">⧉</button>
              </div>
              <span v-if="!adapter.dnsServers || adapter.dnsServers.length === 0" class="muted-text">—</span>
            </div>
          </div>

          <!-- IPv6 地址（包含临时隐私地址与主地址） -->
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
.btn-copy:hover {
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

.dns-chip {
  background: #f0f7ff;
  border-color: #c8e1ff;
}

.v6-tag {
  font-size: 10px;
  background: #fff8c5;
  color: #9a6700;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 600;
}

.chip-copy {
  background: transparent;
  border: none;
  cursor: pointer;
  font-size: 11px;
  color: var(--text-subtle);
  padding: 0 2px;
  line-height: 1;
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
  display: flex;
  align-items: center;
  gap: 10px;
  background: var(--bg-app);
  border: 1px solid var(--border-color);
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
}

.v6-chip.is-temp {
  border-left: 3px solid #0969da;
}

.v6-chip.is-linklocal {
  opacity: 0.75;
}

.v6-chip code {
  font-family: Consolas, monospace;
  font-weight: 500;
  flex: 1;
  word-break: break-all;
}

.v6-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 500;
}
.v6-badge.main {
  background: #dafbe1;
  color: var(--success);
}
.v6-badge.linklocal {
  background: var(--bg-hover);
  color: var(--text-subtle);
}
.v6-chip.is-temp .v6-badge {
  background: #ddf4ff;
  color: #0969da;
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 12px;
  font-weight: 500;
}
.badge-physical {
  background: #ddf4ff;
  color: #0969da;
}
.badge-virtual {
  background: var(--bg-hover);
  color: var(--text-muted);
}
.badge-success {
  background: #dafbe1;
  color: var(--success);
}
.badge-sub {
  background: #f0f7ff;
  color: #0969da;
}

.muted-text {
  font-size: 12px;
  color: var(--text-subtle);
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
.btn-primary:hover {
  background: var(--accent-hover);
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.error-box {
  padding: 10px 14px;
  background: #ffebe9;
  color: var(--danger);
  border: 1px solid rgba(207, 34, 46, 0.2);
  border-radius: 6px;
  font-size: 13px;
}

.empty-hint {
  text-align: center;
  padding: 32px 0;
  color: var(--text-subtle);
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
