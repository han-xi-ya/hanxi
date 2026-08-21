<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import * as PortScanAPI from '../../bindings/hubkit/internal/modules/portscan'
import type { PresetGroup, PortResult } from '../../bindings/hubkit/internal/modules/portscan/models'
import { Events } from '@wailsio/runtime'

const target = ref('127.0.0.1')
const portRange = ref('80,443,3000,5000,5173,8000,8080,8081,8443,8888,9000')
const proxyUrl = ref('')
const currentEgressIP = ref('')
const detectingIP = ref(false)
const scanMode = ref<'safe' | 'normal' | 'fast'>('safe')
const timeoutMs = ref(600)
const concurrency = ref(30)
const rateLimitMs = ref(10)
const deepDetect = ref(true)

const presets = ref<PresetGroup[]>([])
const scanning = ref(false)
const currentTaskId = ref('')
const progressScanned = ref(0)
const progressTotal = ref(0)
const progressPercent = ref(0)
const openPorts = ref<PortResult[]>([])
const durationMs = ref(0)
const errorMessage = ref('')
const toastMsg = ref('')

let unregEvent: (() => void) | null = null
let debounceTimer: any = null

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

function onModeChange() {
  if (scanMode.value === 'safe') {
    concurrency.value = 20
    timeoutMs.value = 800
    rateLimitMs.value = 15
  } else if (scanMode.value === 'normal') {
    concurrency.value = 50
    timeoutMs.value = 600
    rateLimitMs.value = 0
  } else if (scanMode.value === 'fast') {
    concurrency.value = 200
    timeoutMs.value = 400
    rateLimitMs.value = 0
  }
}

async function loadPresets() {
  try {
    const list = await PortScanAPI.PortScanService.GetPresets()
    presets.value = list ?? []
  } catch (e: any) {
    console.error('Failed to load portscan presets:', e)
  }
}

async function checkEgressIP() {
  detectingIP.value = true
  try {
    const ip = await PortScanAPI.PortScanService.CheckEgressIP(proxyUrl.value.trim())
    currentEgressIP.value = ip || '未获取到'
  } catch (e: any) {
    currentEgressIP.value = '检测失败'
    console.warn('Egress IP check failed:', e)
  } finally {
    detectingIP.value = false
  }
}

function applyPreset(p: PresetGroup) {
  portRange.value = p.ports
  showToast(`已应用预设：${p.name}`)
}

async function startScan() {
  if (!target.value.trim()) {
    errorMessage.value = '请输入扫描目标 IP 或域名'
    return
  }
  if (!portRange.value.trim()) {
    errorMessage.value = '请输入要扫描的端口或选择预设'
    return
  }

  errorMessage.value = ''
  openPorts.value = []
  progressScanned.value = 0
  progressTotal.value = 0
  progressPercent.value = 0
  durationMs.value = 0
  scanning.value = true

  try {
    const summary = await PortScanAPI.PortScanService.StartScan({
      target: target.value.trim(),
      portRange: portRange.value.trim(),
      proxyUrl: proxyUrl.value.trim(),
      timeoutMs: Number(timeoutMs.value) || 600,
      concurrency: Number(concurrency.value) || 30,
      rateLimitMs: Number(rateLimitMs.value) || 0,
      deepDetect: Boolean(deepDetect.value),
    })

    if (summary) {
      openPorts.value = summary.openPorts ?? []
      durationMs.value = summary.durationMs
      progressTotal.value = summary.totalPorts
      progressScanned.value = summary.totalPorts
      progressPercent.value = 100
    }
  } catch (e: any) {
    errorMessage.value = e?.message ?? String(e)
  } finally {
    scanning.value = false
  }
}

async function stopScan() {
  try {
    await PortScanAPI.PortScanService.StopScan(currentTaskId.value || 'current')
    scanning.value = false
    showToast('已终止扫描')
  } catch (e: any) {
    showToast(`停止失败: ${e?.message ?? e}`)
  }
}

function copyOpenPorts() {
  if (openPorts.value.length === 0) return
  const str = openPorts.value.map(p => p.port).join(', ')
  navigator.clipboard.writeText(str)
  showToast(`已复制 ${openPorts.value.length} 个开放端口`)
}

function openInBrowser(port: number) {
  const host = target.value.trim() === '0.0.0.0' ? '127.0.0.1' : target.value.trim()
  const proto = (port === 443 || port === 8443) ? 'https' : 'http'
  window.open(`${proto}://${host}:${port}`, '_blank')
}

// 监听代理输入变化，防抖自动重新探测出网 IP
watch(proxyUrl, () => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    checkEgressIP()
  }, 600)
})

onMounted(() => {
  loadPresets()
  onModeChange()
  checkEgressIP()

  // 监听后端流式进度推送
  unregEvent = Events.On('portscan:progress', (event: any) => {
    const p = event?.data
    if (!p) return
    currentTaskId.value = p.taskId
    progressScanned.value = p.scanned
    progressTotal.value = p.total
    progressPercent.value = Math.round(p.percent)

    if (p.latestPort && p.latestPort.status === 'open') {
      if (!openPorts.value.some(item => item.port === p.latestPort.port)) {
        openPorts.value.push(p.latestPort)
        openPorts.value.sort((a, b) => a.port - b.port)
      }
    }
  })
})

onUnmounted(() => {
  if (unregEvent) {
    unregEvent()
  }
})
</script>

<template>
  <section class="page portscan-page">
    <div class="header-row">
      <div>
        <h1>端口扫描与服务识别</h1>
        <p class="subtitle">高并发 TCP 探测，集成轻量服务指纹识别，支持中继代理与出网源 IP 实时核验。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

    <!-- 控制面板卡片 -->
    <div class="section-card control-card">
      <div class="input-grid">
        <div class="form-group">
          <label>目标主机 / IP</label>
          <div class="target-input-row">
            <input
              v-model="target"
              type="text"
              class="input target-input"
              placeholder="如 127.0.0.1, 192.168.1.1, baidu.com"
              :disabled="scanning"
            />
            <button class="btn btn-secondary btn-small" :disabled="scanning" @click="target = '127.0.0.1'">本机</button>
          </div>
        </div>

        <div class="form-group span-2">
          <label>端口范围 (支持逗号隔开与连字符区间)</label>
          <input
            v-model="portRange"
            type="text"
            class="input"
            placeholder="如 80,443,3000-3010,8080"
            :disabled="scanning"
          />
        </div>
      </div>

      <!-- 常用预设胶囊标签 -->
      <div class="presets-row">
        <span class="preset-label">快捷预设:</span>
        <div class="preset-badges">
          <button
            v-for="p in presets"
            :key="p.key"
            class="preset-chip"
            :title="p.description"
            :disabled="scanning"
            @click="applyPreset(p)"
          >
            {{ p.name }}
          </button>
        </div>
      </div>

      <!-- 代理与出网 IP 状态条 -->
      <div class="egress-bar">
        <div class="egress-left">
          <div class="param-item proxy-item" title="通过 SOCKS5 代理中继扫描流量，隐藏本机真实 IP（例如 socks5://127.0.0.1:7890）">
            <span class="param-title">🛡️ 代理中继:</span>
            <input
              v-model="proxyUrl"
              type="text"
              class="input proxy-input"
              placeholder="留空直连 / socks5://127.0.0.1:7890"
              :disabled="scanning"
            />
          </div>

          <!-- 实时出网 IP 提示卡片 -->
          <div class="egress-indicator" :class="{ 'is-proxy': !!proxyUrl.trim() }">
            <span class="indicator-label">本次出网探测 IP:</span>
            <span class="indicator-ip" v-if="!detectingIP">
              <strong>{{ currentEgressIP || '探测中...' }}</strong>
              <span v-if="proxyUrl.trim()" class="proxy-badge">已代理</span>
              <span v-else class="direct-badge">直连</span>
            </span>
            <span class="indicator-loading" v-else>🔄 正在核验出网 IP...</span>
            <button class="btn-refresh" title="重新测试出网 IP" :disabled="detectingIP || scanning" @click="checkEgressIP">↻</button>
          </div>
        </div>
      </div>

      <!-- 高级设置与操作条 -->
      <div class="action-bar">
        <div class="options-group">
          <!-- 扫描模式切换 -->
          <div class="mode-selector">
            <span class="param-title">模式:</span>
            <select v-model="scanMode" class="select-input" :disabled="scanning" @change="onModeChange">
              <option value="safe">🍃 温和防封 (公网/云服务器推荐)</option>
              <option value="normal">⚡ 平衡模式 (局域网/开发联调)</option>
              <option value="fast">🚀 极速并发 (内网/全端口)</option>
            </select>
          </div>

          <label class="checkbox-label" title="自动探测 Web 标题、SSH、Redis、MySQL 等服务特征">
            <input type="checkbox" v-model="deepDetect" :disabled="scanning" />
            <span>✨ 指纹识别</span>
          </label>

          <div class="param-item">
            <span class="param-title">超时:</span>
            <input v-model.number="timeoutMs" type="number" step="100" min="100" max="5000" class="input num-input" :disabled="scanning" />
            <span class="unit">ms</span>
          </div>

          <div class="param-item">
            <span class="param-title">并发:</span>
            <input v-model.number="concurrency" type="number" step="10" min="5" max="2000" class="input num-input" :disabled="scanning" />
          </div>

          <div class="param-item" v-if="rateLimitMs > 0">
            <span class="param-title">微延:</span>
            <input v-model.number="rateLimitMs" type="number" step="5" min="0" max="500" class="input num-input" :disabled="scanning" />
            <span class="unit">ms</span>
          </div>
        </div>

        <div class="btn-group">
          <button v-if="!scanning" class="btn btn-primary" @click="startScan">
            🚀 开始扫描
          </button>
          <button v-else class="btn btn-danger" @click="stopScan">
            ⏹ 终止扫描
          </button>
        </div>
      </div>
    </div>

    <!-- 进度与错误提示 -->
    <div v-if="errorMessage" class="error-banner">
      ⚠️ {{ errorMessage }}
    </div>

    <div v-if="scanning || progressTotal > 0" class="progress-card">
      <div class="progress-info">
        <span>扫描进度：<strong>{{ progressScanned }}</strong> / {{ progressTotal }} ({{ progressPercent }}%)</span>
        <span>已发现开放端口：<strong class="highlight-open">{{ openPorts.length }}</strong></span>
        <span v-if="durationMs > 0" class="duration-tag">耗时: {{ durationMs }}ms</span>
      </div>
      <div class="progress-bar-bg">
        <div class="progress-bar-fill" :style="{ width: `${progressPercent}%` }"></div>
      </div>
    </div>

    <!-- 扫描结果列表卡片 -->
    <div class="section-card result-card">
      <div class="card-header">
        <div class="header-left">
          <h2>开放端口列表 ({{ openPorts.length }})</h2>
          <span v-if="openPorts.length > 0" class="badge-tag">Target: {{ target }}</span>
          <span v-if="openPorts.length > 0 && currentEgressIP" class="badge-tag source-tag">Source IP: {{ currentEgressIP }}</span>
        </div>
        <div class="header-actions" v-if="openPorts.length > 0">
          <button class="btn btn-secondary btn-small" @click="copyOpenPorts">📋 复制全部端口</button>
        </div>
      </div>

      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 100px;">端口</th>
              <th style="width: 110px;">状态</th>
              <th style="width: 140px;">推测服务</th>
              <th>Banner / 版本详情 / 指纹</th>
              <th style="width: 100px;">响应延迟</th>
              <th style="width: 110px; text-align: right;">快捷操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in openPorts" :key="item.port">
              <td>
                <strong class="port-num">{{ item.port }}</strong>
              </td>
              <td>
                <span class="status-tag ok">● OPEN</span>
              </td>
              <td>
                <span class="service-tag">{{ item.service || 'unknown' }}</span>
              </td>
              <td>
                <div class="banner-cell">
                  <span v-if="item.banner" class="banner-text" :title="item.banner">{{ item.banner }}</span>
                  <span v-else class="text-muted">—</span>
                  <span v-if="item.fingerprint" class="fp-badge" :title="item.fingerprint">{{ item.fingerprint }}</span>
                </div>
              </td>
              <td>
                <span class="latency-tag">{{ item.latencyMs }}ms</span>
              </td>
              <td style="text-align: right;">
                <button
                  v-if="item.service?.includes('http') || item.port === 80 || item.port === 443 || item.port === 3000 || item.port === 5173 || item.port === 8080"
                  class="btn btn-secondary btn-xs"
                  title="在浏览器中打开"
                  @click="openInBrowser(item.port)"
                >
                  🌐 打开
                </button>
              </td>
            </tr>
            <tr v-if="openPorts.length === 0">
              <td colspan="6" class="empty-cell">
                <div class="empty-state">
                  <span class="empty-icon">{{ scanning ? '⏳' : '🔍' }}</span>
                  <p>{{ scanning ? '正在高速扫描中，请稍候...' : '暂无扫描结果，在上方输入目标并点击「开始扫描」' }}</p>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.portscan-page { display: flex; flex-direction: column; gap: 14px; height: 100%; }
.header-row { display: flex; justify-content: space-between; align-items: center; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }

.section-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  border-radius: 8px; padding: 16px 18px; display: flex; flex-direction: column; gap: 12px;
}

.input-grid { display: grid; grid-template-columns: 1fr 2fr; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-group label { font-size: 12px; font-weight: 600; color: var(--text-muted); }
.target-input-row { display: flex; gap: 8px; }

.input, .select-input {
  background: var(--bg-app); border: 1px solid var(--border-color); border-radius: 6px;
  padding: 8px 12px; font-size: 13px; color: var(--text-main); font-family: inherit; width: 100%;
}
.select-input { width: auto; font-size: 12px; padding: 6px 10px; cursor: pointer; }
.input:focus, .select-input:focus { outline: none; border-color: var(--accent); }
.num-input { width: 70px; text-align: center; }
.proxy-input { width: 230px; font-size: 12px; padding: 5px 8px; }

.presets-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.preset-label { font-size: 12px; color: var(--text-muted); font-weight: 600; white-space: nowrap; }
.preset-badges { display: flex; gap: 6px; flex-wrap: wrap; }
.preset-chip {
  background: var(--bg-app); border: 1px solid var(--border-color); border-radius: 14px;
  padding: 3px 10px; font-size: 11px; color: var(--text-main); cursor: pointer; transition: all 0.15s ease;
}
.preset-chip:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); background: var(--bg-hover); }

/* 出网 IP 指示条 */
.egress-bar {
  background: var(--bg-app); border: 1px solid var(--border-color);
  border-radius: 6px; padding: 8px 12px;
}
.egress-left { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.egress-indicator {
  display: flex; align-items: center; gap: 8px; font-size: 12px;
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  padding: 4px 10px; border-radius: 6px;
}
.indicator-label { color: var(--text-muted); }
.indicator-ip { font-family: Consolas, monospace; color: var(--accent); display: flex; align-items: center; gap: 6px; }
.indicator-loading { color: var(--text-subtle); font-size: 11px; }
.proxy-badge { background: #dafbe1; color: #1a7f37; font-size: 10px; padding: 1px 5px; border-radius: 3px; font-weight: 600; }
.direct-badge { background: var(--bg-hover); color: var(--text-muted); font-size: 10px; padding: 1px 5px; border-radius: 3px; }
.btn-refresh {
  background: transparent; border: none; cursor: pointer; color: var(--text-muted);
  font-size: 14px; padding: 0 2px; line-height: 1; transition: color 0.15s;
}
.btn-refresh:hover:not(:disabled) { color: var(--accent); }

.action-bar {
  display: flex; justify-content: space-between; align-items: center; padding-top: 6px;
  border-top: 1px solid var(--border-color); flex-wrap: wrap; gap: 12px;
}
.options-group { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
.mode-selector { display: flex; align-items: center; gap: 6px; }
.proxy-item { display: flex; align-items: center; gap: 6px; }
.checkbox-label { display: flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 500; cursor: pointer; color: var(--text-main); }
.param-item { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-muted); }
.unit { font-size: 11px; }

.btn-group { display: flex; gap: 10px; }

/* 进度卡片 */
.progress-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px;
  padding: 12px 16px; display: flex; flex-direction: column; gap: 8px;
}
.progress-info { display: flex; justify-content: space-between; font-size: 12px; color: var(--text-muted); }
.highlight-open { color: var(--success); font-size: 13px; }
.duration-tag { color: var(--text-subtle); }
.progress-bar-bg { width: 100%; height: 6px; background: var(--bg-hover); border-radius: 3px; overflow: hidden; }
.progress-bar-fill { height: 100%; background: var(--accent); transition: width 0.15s ease; }

/* 结果表格 */
.result-card { flex: 1; min-height: 280px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-header h2 { font-size: 14px; font-weight: 600; margin: 0; }
.header-left { display: flex; align-items: center; gap: 8px; }
.badge-tag { font-size: 11px; background: var(--bg-hover); padding: 2px 8px; border-radius: 4px; color: var(--text-subtle); }
.source-tag { color: var(--accent); border: 1px solid var(--accent); font-family: Consolas, monospace; }

.table-container { border: 1px solid var(--border-color); border-radius: 6px; overflow: auto; max-height: 480px; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
.tbl th { background: var(--bg-app); padding: 10px 12px; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border-color); position: sticky; top: 0; }
.tbl td { padding: 8px 12px; border-bottom: 1px solid var(--border-color); color: var(--text-main); }
.tbl tr:last-child td { border-bottom: none; }

.port-num { font-family: Consolas, monospace; font-size: 14px; color: var(--accent); }
.status-tag { font-size: 11px; padding: 2px 8px; border-radius: 10px; font-weight: 600; }
.status-tag.ok { background: #dafbe1; color: #1a7f37; }
.service-tag { font-family: Consolas, monospace; font-size: 12px; font-weight: 600; color: #0969da; }
.banner-cell { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.banner-text { font-size: 12px; color: var(--text-main); font-family: Consolas, monospace; }
.fp-badge { font-size: 10px; background: var(--bg-hover); color: var(--text-subtle); padding: 1px 6px; border-radius: 4px; }
.latency-tag { font-family: Consolas, monospace; font-size: 11px; color: var(--text-muted); }

.empty-cell { text-align: center; padding: 40px 10px !important; }
.empty-state { display: flex; flex-direction: column; align-items: center; gap: 8px; color: var(--text-muted); font-size: 13px; }
.empty-icon { font-size: 24px; }

.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn-primary { background: #0969da; color: #fff; border-color: #0969da; }
.btn-primary:hover:not(:disabled) { background: #0854ad; }
.btn-danger { background: #cf222e; color: #fff; border-color: #cf222e; }
.btn-danger:hover:not(:disabled) { background: #a40e26; }
.btn-secondary { background: var(--bg-app); border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 10px; font-size: 12px; }
.btn-xs { padding: 2px 8px; font-size: 11px; }
.error-banner { background: #ffebe9; color: #cf222e; padding: 8px 12px; border-radius: 6px; font-size: 12px; border: 1px solid #ff8182; }
</style>
