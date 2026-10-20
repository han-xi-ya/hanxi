<script setup lang="ts">
// SSH 检查面板（PortScanView 的第三个标签页）。
// 两个动作分得很开：「连接测试」只读版本横幅；「试登录」真的发起一次密码认证，
// 会在对端 sshd 日志里留记录——所以凭据默认收起、按钮单独一个，不藏在主按钮后面。
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import { SSH_AUTH_LABEL, SSH_AUTH_TONE, SSH_PORT_PRESETS, useSSHCheck } from './useSSHCheck'

const {
  form, cred, credOpen, optsOpen, timeoutMs,
  result, auth, error, checking,
  test, testAuth, copyResult,
} = useSSHCheck()
</script>

<template>
  <div class="section-card">
    <div class="card-header">
      <div class="header-left">
        <h2>SSH 连接测试</h2>
        <span v-if="result?.ok" class="badge-tag mono">{{ result.protocol === '2.0' ? 'SSH 2.0' : `SSH ${result.protocol}` }}</span>
      </div>
      <button v-if="result || auth" class="btn btn-secondary btn-small" @click="copyResult">📋 复制结果</button>
    </div>

    <div class="check-row">
      <input v-model="form.host" type="text" class="input check-host" placeholder="127.0.0.1" aria-label="SSH 主机地址" :disabled="checking" />
      <span class="check-colon" aria-hidden="true">:</span>
      <input v-model.number="form.port" type="number" min="1" max="65535" class="input check-port" aria-label="SSH 端口" :disabled="checking" />
      <button class="btn btn-secondary" :disabled="checking" @click="test">
        {{ checking ? '⏳ 进行中…' : '🔌 连接测试' }}
      </button>
    </div>

    <div class="quick-targets">
      <span class="quick-label">常用端口:</span>
      <button
        v-for="preset in SSH_PORT_PRESETS"
        :key="preset.port"
        type="button"
        class="btn btn-quick"
        :title="preset.title"
        :disabled="checking"
        @click="form.port = preset.port"
      >{{ preset.port }}</button>
    </div>

    <div class="check-advanced">
      <button type="button" class="btn btn-ghost btn-small" :disabled="checking" :aria-expanded="credOpen" @click="credOpen = !credOpen">
        {{ cred.user ? `已填用户名：${cred.user}` : '测试账号密码' }}
      </button>
      <button type="button" class="btn btn-ghost btn-small" :disabled="checking" :aria-expanded="optsOpen" @click="optsOpen = !optsOpen">超时</button>
    </div>

    <div v-if="optsOpen" class="check-subrow">
      <label class="param-item">连接预算
        <input v-model.number="timeoutMs" type="number" step="500" min="0" max="30000" class="input check-num" :disabled="checking" />
        <span class="unit">ms</span>
      </label>
    </div>

    <div v-if="credOpen" class="check-cred">
      <div class="check-subrow">
        <label class="param-item">用户名
          <input v-model="cred.user" type="text" class="input check-cred-input" placeholder="登录用户名" autocomplete="off" :disabled="checking" />
        </label>
        <label class="param-item">密码
          <input v-model="cred.pass" type="text" class="input check-cred-input" placeholder="登录密码" autocomplete="off" :disabled="checking" />
        </label>
        <button class="btn btn-secondary" :disabled="checking || !cred.user.trim() || !cred.pass" @click="testAuth">
          🔑 试登录
        </button>
      </div>
      <div class="hint-line">
        ⚠️ 这一步会<strong>真的向服务端发起一次密码认证</strong>：对端 sshd 日志会留下记录，装了 fail2ban 的主机可能被临时封禁。
        密码只存在这一次请求里，不保存、不写进结果、不回显。
      </div>
    </div>

    <UiBanner v-if="error" tone="error">⚠️ {{ error }}</UiBanner>
    <div v-if="checking" class="state-box">
      <span class="live-pulse">⏳ 正在与对端交换 SSH 会话…</span>
    </div>

    <div v-if="result" class="check-verdict">
      <UiStatusChip :tone="result.ok ? 'positive' : 'danger'">{{ result.ok ? '可达' : '不可达' }}</UiStatusChip>
      <div class="check-verdict-body">
        <div class="check-meta">{{ result.addr }} · {{ result.latencyMs }}ms</div>
        <div v-if="result.ok" class="check-server mono">{{ result.serverVersion }}</div>
        <div v-else class="check-reason">{{ result.error || '未知原因' }}</div>
      </div>
    </div>

    <div v-if="auth" class="check-verdict">
      <UiStatusChip :tone="SSH_AUTH_TONE[auth.verdict] ?? 'neutral'">{{ SSH_AUTH_LABEL[auth.verdict] ?? auth.verdict }}</UiStatusChip>
      <div class="check-verdict-body">
        <div class="check-meta">凭据测试 · {{ auth.addr }} · {{ auth.latencyMs }}ms</div>
        <div class="check-reason">{{ auth.reason || '（无说明）' }}</div>
        <div v-if="auth.hostKeyFingerprint" class="check-meta">
          主机密钥 <span class="mono">{{ auth.hostKeyType }}</span> 指纹
          <span class="mono">{{ auth.hostKeyFingerprint }}</span>
        </div>
      </div>
    </div>

    <div v-if="result?.ok && !auth" class="hint-line">
      「可达」只说明端口通、对方确实是 SSH 服务；能不能登录点上面「测试账号密码」。
    </div>
    <div v-else-if="!checking && !error && !result && !auth" class="empty-hint">填好主机与端口点「连接测试」；要测账号密码就展开「测试账号密码」</div>
  </div>
</template>

<style scoped src="./portscanPanels.css"></style>
