<script setup lang="ts">
// 代理连通性测试面板（PortScanView 的第二个标签页）。
// 状态与动作在 useProxyCheck.ts，本文件只管呈现；卡片壳与字段行样式来自家族共享件
// portscanPanels.css（与视图各 scoped 引入同一份，不复制第二份定义）。
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import { PROXY_PORT_PRESETS, sourceHost, useProxyCheck } from './useProxyCheck'

const { form, credOpen, optsOpen, timeoutMs, skipTls, result, error, checking, test, copyResult } = useProxyCheck()
</script>

<template>
  <div class="section-card">
    <div class="card-header">
      <div class="header-left">
        <h2>代理连通性测试</h2>
        <span v-if="result" class="badge-tag">{{ (result.probes?.length ?? 0) }} 个出口源</span>
      </div>
      <button v-if="result" class="btn btn-secondary btn-small" @click="copyResult">📋 复制结果</button>
    </div>

    <!-- 主路径就一行：协议 + 地址:端口 + 测试。打开面板默认已填好本机常见口，点即可 -->
    <div class="check-row">
      <select v-model="form.kind" class="select-input proxy-kind" :disabled="checking" aria-label="代理协议">
        <option value="socks5">SOCKS5</option>
        <option value="http">HTTP</option>
        <option value="https">HTTPS</option>
      </select>
      <div class="proxy-addr-group">
        <input v-model="form.host" type="text" class="input check-host" placeholder="127.0.0.1" aria-label="代理地址" :disabled="checking" />
        <span class="check-colon" aria-hidden="true">:</span>
        <input v-model.number="form.port" type="number" min="1" max="65535" class="input check-port" aria-label="代理端口" :disabled="checking" />
      </div>
      <button class="btn btn-secondary" :disabled="checking" @click="test">
        {{ checking ? '⏳ 测试中…' : '🧪 测试' }}
      </button>
    </div>

    <div class="quick-targets">
      <span class="quick-label">常用端口:</span>
      <button
        v-for="preset in PROXY_PORT_PRESETS"
        :key="preset.port"
        type="button"
        class="btn btn-quick"
        :title="preset.title"
        :disabled="checking"
        @click="form.port = preset.port"
      >{{ preset.port }}</button>
    </div>

    <!-- 低频项收进次级行：本地代理大多不需要认证，也不该改超时 -->
    <div class="check-advanced">
      <button
        type="button"
        class="btn btn-ghost btn-small"
        :disabled="checking"
        :aria-expanded="credOpen"
        @click="credOpen = !credOpen"
      >{{ form.user ? `已填账号：${form.user}` : '需要账号密码' }}</button>
      <button type="button" class="btn btn-ghost btn-small" :disabled="checking" :aria-expanded="optsOpen" @click="optsOpen = !optsOpen">
        超时 / 证书校验
      </button>
    </div>

    <div v-if="credOpen" class="check-subrow">
      <label class="param-item">账号
        <input v-model="form.user" type="text" class="input check-cred-input" placeholder="可选" autocomplete="off" :disabled="checking" />
      </label>
      <label class="param-item">密码
        <input v-model="form.pass" type="text" class="input check-cred-input" placeholder="可选" autocomplete="off" :disabled="checking" />
      </label>
      <span class="hint-line">只用于这一次请求：不进存储、不进结果、不回显。输入框刻意明文显示，只为方便核对粘贴来的密码有没有多带字符，不代表它会被保存。</span>
    </div>

    <div v-if="optsOpen" class="check-subrow">
      <label class="param-item">单条超时
        <input v-model.number="timeoutMs" type="number" step="500" min="0" max="30000" class="input check-num" :disabled="checking" />
        <span class="unit">ms</span>
      </label>
      <label class="checkbox-label" title="仅内网自签证书代理需要勾选；勾上后 TLS 校验被跳过，出口 IP 结论不可信">
        <input v-model="skipTls" type="checkbox" :disabled="checking" />
        <span>跳过证书校验</span>
      </label>
    </div>

    <UiBanner v-if="error" tone="error">⚠️ {{ error }}</UiBanner>
    <div v-if="checking" class="state-box">
      <span class="live-pulse">⏳ 正在经该代理逐个出口源取 IP（同步返回，最长约一个超时预算）…</span>
    </div>

    <template v-if="result">
      <div class="check-verdict">
        <UiStatusChip :tone="result.ok ? 'positive' : 'danger'">{{ result.ok ? '通' : '不通' }}</UiStatusChip>
        <div class="check-verdict-body">
          <div class="check-meta">{{ result.addr }} · {{ result.latencyMs }}ms</div>
          <div v-if="result.ok" class="proxy-egress">
            <span class="text-subtle">出口</span>
            <span class="proxy-egress-ip">{{ result.egressIp }}</span>
            <span class="text-subtle">{{ result.egressAgree }}/{{ result.egressSources }} 源一致（代表源 {{ sourceHost(result.egressSource) }}）</span>
          </div>
          <div v-else class="check-reason">{{ result.error || '未知原因' }}</div>
        </div>
      </div>

      <!-- 逐源明细：每个源各查各的，IP 分别显示；与结论不同必须看得出来 -->
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th>出口源</th>
              <th style="width: 150px;">拿到的 IP</th>
              <th style="width: 74px;">耗时</th>
              <th>与结论</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in result.probes ?? []" :key="p.source">
              <td><span class="mono">{{ sourceHost(p.source) }}</span></td>
              <td><span class="mono">{{ p.ip || '—' }}</span></td>
              <td><span class="mono">{{ p.latencyMs }}ms</span></td>
              <td>
                <span v-if="!p.ip" class="check-reason">{{ p.error || '失败' }}</span>
                <span v-else-if="p.ip === result.egressIp" class="text-success">一致</span>
                <UiStatusChip v-else tone="warning">与结论不同</UiStatusChip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="hint-line">各源各查各的：给出不同 IP、或有的源失败都属正常（源侧多机房、代理端 NAT 池轮转）。判通只看「有没有源拿到 IP」，不拿 IP 变化当判据。</div>
    </template>

    <div v-else-if="!checking && !error" class="empty-hint">填好地址与端口（或直接点「测试」用默认的本机常见口），逐源结果会显示在这里</div>
  </div>
</template>

<style scoped src="./portscanPanels.css"></style>
