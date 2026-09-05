<script setup lang="ts">
import type { Adapter } from '../../../bindings/hanxi/internal/platform/models'

// 单张网卡详情卡：局域网 IPv4 / 默认网关 / DNS / IPv6 地址列表（纯展示）。
// v-for 与 :key 留在宿主 IpOverviewPanel 的用法处；快捷动作一律上抛，
// 「切 Tab 并立即探测」与复制 toast 口径归视图编排层所有。
defineProps<{
  adapter: Adapter
}>()

const emit = defineEmits<{
  'quick-ping': [ip: string]
  'copy-text': [text: string, label: string]
}>()
</script>

<template>
  <div class="adapter-card">
    <div class="adapter-header">
      <div class="adapter-title-box">
        <span class="adapter-name">{{ adapter.name }}</span>
        <span v-if="adapter.description && adapter.description !== adapter.name" class="adapter-desc">
          {{ adapter.description }}
        </span>
      </div>
      <div class="adapter-tags">
        <span v-if="adapter.isPhysical" class="chip chip-positive">物理网卡</span>
        <span v-else class="chip chip-neutral">虚拟 / 隧道</span>
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
            <button class="chip-action" title="Ping 测试" @click="emit('quick-ping', ip)">⚡</button>
            <button class="chip-copy" title="复制" @click="emit('copy-text', ip, '局域网 IP')">⧉</button>
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
            <button class="chip-action" title="Ping 网关" @click="emit('quick-ping', adapter.gateway)">⚡</button>
            <button class="chip-copy" title="复制" @click="emit('copy-text', adapter.gateway, '默认网关')">⧉</button>
          </div>
          <div v-if="adapter.ipv6Gateway" class="ip-chip">
            <span class="v6-tag">v6</span>
            <code>{{ adapter.ipv6Gateway }}</code>
            <button class="chip-copy" title="复制" @click="emit('copy-text', adapter.ipv6Gateway, 'IPv6 网关')">⧉</button>
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
            <button class="chip-action" title="Ping DNS" @click="emit('quick-ping', dns)">⚡</button>
            <button class="chip-copy" title="复制" @click="emit('copy-text', dns, 'DNS')">⧉</button>
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
            <span class="chip chip-information" v-if="v6.isTemporary">临时隐私地址</span>
            <span class="chip chip-neutral" v-else-if="v6.type === 'LinkLocal'">链路本地 (Link-Local)</span>
            <span class="chip chip-positive" v-else>全局主地址</span>

            <code>{{ v6.address }}</code>
            <button class="chip-copy" title="复制" @click="emit('copy-text', v6.address, 'IPv6')">⧉</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 网卡卡家族为本视图独有形态（.chip/.code 等原子仍由 components.css 承载），
   拆分时随标记整体迁入本组件；颜色/圆角/动效一律语义 token，无硬编码值。 */
.adapter-card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
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
  border-bottom: 1px solid var(--color-border);
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
  color: var(--color-text);
}

.adapter-desc {
  font-size: 12px;
  color: var(--color-text-subtle);
}

.adapter-tags {
  display: flex;
  align-items: center;
  gap: 10px;
}

.mac-text {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-text-muted);
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
  color: var(--color-text-muted);
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
  background: var(--surface-page);
  border: 1px solid var(--color-border);
  padding: 3px 8px;
  border-radius: 5px;
  font-size: 12px;
}

.ip-chip code {
  font-family: var(--font-mono);
  font-weight: 600;
}

.chip-action {
  background: transparent;
  border: none;
  cursor: pointer;
  font-size: 11px;
  color: var(--color-primary);
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
  color: var(--color-text-subtle);
  padding: 0 2px;
}
.chip-copy:hover {
  color: var(--color-primary);
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
  background: var(--surface-page);
  border: 1px solid var(--color-border);
  padding: 4px 10px;
  border-radius: 5px;
  font-size: 12px;
}

.v6-chip.is-temp {
  border-color: var(--state-information-glow);
  background: var(--state-information-soft);
}

.v6-chip.is-linklocal {
  border-color: var(--color-border);
  opacity: 0.85;
}

.v6-chip code {
  font-family: var(--font-mono);
  font-weight: 600;
  word-break: break-all;
}

.v6-tag {
  font-size: 10px;
  font-weight: 600;
  color: var(--state-warning);
  background: var(--state-warning-soft);
  padding: 1px 4px;
  border-radius: 3px;
}

.muted-text {
  font-size: 12px;
  color: var(--color-text-subtle);
}
</style>
