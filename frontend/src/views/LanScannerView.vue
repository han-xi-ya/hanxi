<script setup lang="ts">
import { ref, shallowRef, onMounted, onUnmounted, nextTick } from 'vue'
import { Events } from '@wailsio/runtime'
import * as LanAPI from '../../bindings/hanxi/internal/modules/lan'
import type { SubnetInfo, DeviceInfo, LanProgress } from '../../bindings/hanxi/internal/modules/lan/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const { showToast } = useToast()

const subnets = shallowRef<SubnetInfo[]>([])
const selectedCidr = ref('')
const scanning = ref(false)
const progress = ref<LanProgress>({ scanned: 0, total: 254, found: 0 })
const devices = shallowRef<DeviceInfo[]>([])
const errorMsg = ref('')

// 行内编辑备注状态
const editingKey = ref<string | null>(null)
const editingValue = ref('')
const remarkInputRef = ref<HTMLInputElement | null>(null)

let unlistenProgress: (() => void) | null = null

async function loadSubnets() {
  try {
    const list = await LanAPI.LanService.GetSubnets()
    subnets.value = list ?? []
    if (subnets.value.length > 0 && !selectedCidr.value) {
      selectedCidr.value = subnets.value[0].cidr
    }
  } catch (err: unknown) {
    errorMsg.value = `获取网卡子网失败: ${getErrorMessage(err)}`
  }
}

function onSelectSubnet(e: Event) {
  const target = e.target as HTMLSelectElement
  if (target.value) {
    selectedCidr.value = target.value
  }
}

async function startScan() {
  const range = selectedCidr.value.trim()
  if (!range || scanning.value) return

  scanning.value = true
  errorMsg.value = ''
  devices.value = []
  progress.value = { scanned: 0, total: 254, found: 0 }

  try {
    const res = await LanAPI.LanService.Scan(range)
    devices.value = res ?? []
    if (devices.value.length === 0) {
      showToast('扫描完成，未探测到在线设备')
    }
  } catch (err: unknown) {
    errorMsg.value = `扫描异常: ${getErrorMessage(err)}`
  } finally {
    scanning.value = false
  }
}

async function stopScan() {
  try {
    await LanAPI.LanService.Cancel()
  } catch (err: unknown) {
    console.error('Cancel failed', err)
  } finally {
    scanning.value = false
  }
}

function copyIP(ip: string) {
  navigator.clipboard.writeText(ip)
  showToast(`已复制 IP: ${ip}`)
}

function startEditRemark(dev: DeviceInfo) {
  editingKey.value = dev.ip
  editingValue.value = dev.remark || ''
  nextTick(() => {
    if (remarkInputRef.value) {
      remarkInputRef.value.focus()
      remarkInputRef.value.select()
    }
  })
}

async function saveRemark(dev: DeviceInfo) {
  if (editingKey.value !== dev.ip) return

  const newRemark = editingValue.value.trim()
  const key = dev.mac ? dev.mac : dev.ip
  editingKey.value = null

  try {
    await LanAPI.LanService.SetRemark(key, newRemark)
    // 更新浅响应式设备列表
    devices.value = devices.value.map(d => (d.ip === dev.ip ? { ...d, remark: newRemark } : d))
    showToast(newRemark ? '备注已保存' : '备注已清除')
  } catch (err: unknown) {
    showToast(`保存失败: ${getErrorMessage(err)}`)
  }
}

function cancelEditRemark() {
  editingKey.value = null
  editingValue.value = ''
}

onMounted(async () => {
  await loadSubnets()

  // 监听扫描实时进度推流
  unlistenProgress = Events.On('lan:progress', (event: { data?: LanProgress }) => {
    if (event?.data) {
      progress.value = event.data
    }
  })
})

onUnmounted(() => {
  if (unlistenProgress) {
    unlistenProgress()
    unlistenProgress = null
  }
})
</script>

<template>
  <section class="page lan-page">
    <div class="header-row">
      <div>
        <h1>局域网扫描</h1>
        <p class="subtitle">支持标准 CIDR 子网 (如 /24、/22) 与自定义 IP 范围扫描，毫秒级探测活跃设备与持久化备注。</p>
      </div>
    </div>

    <!-- 顶部操作栏 -->
    <div class="control-panel">
      <div class="form-item">
        <label>目标网段 / IP 范围:</label>
        <div class="input-combo-wrap">
          <input
            v-model="selectedCidr"
            type="text"
            :disabled="scanning"
            class="input-box"
            placeholder="如 192.168.1.0/24 或 100.94.178.1-100"
            @keydown.enter="startScan"
          />
          <select :disabled="scanning" class="select-addon" @change="onSelectSubnet">
            <option value="" disabled selected>选择网卡快捷网段…</option>
            <option v-for="s in subnets" :key="s.cidr" :value="s.cidr">
              {{ s.name }} ({{ s.ip }}) - {{ s.cidr }}
            </option>
          </select>
        </div>
      </div>

      <div class="btn-group">
        <button v-if="!scanning" class="btn btn-primary" :disabled="!selectedCidr" @click="startScan">
          开始扫描
        </button>
        <button v-else class="btn btn-danger" @click="stopScan">
          停止扫描
        </button>
        <button class="btn btn-secondary" :disabled="scanning" @click="loadSubnets">
          刷新网卡
        </button>
      </div>
    </div>

    <!-- 快捷网段范围辅助提示 -->
    <div class="quick-helpers">
      <span class="helper-label">快捷示例:</span>
      <button class="helper-tag" @click="selectedCidr = '192.168.1.0/24'">192.168.1.0/24</button>
      <button class="helper-tag" @click="selectedCidr = '100.94.178.1-100.94.178.100'">100.94.178.1-100</button>
      <button class="helper-tag" @click="selectedCidr = '100.111.18.0/24'">100.111.18.0/24</button>
      <button class="helper-tag" @click="selectedCidr = '172.16.0.0/22'">172.16.0.0/22 (/22大网段)</button>
    </div>

    <!-- 进度条 -->
    <div v-if="scanning || progress.scanned > 0" class="progress-section">
      <div class="progress-meta">
        <span>扫描进度: {{ progress.scanned }} / {{ progress.total }}</span>
        <span class="found-badge">发现 {{ devices.length || progress.found }} 台设备</span>
      </div>
      <div class="progress-bar-wrap">
        <div
          class="progress-bar-inner"
          :style="{ width: `${(progress.scanned / (progress.total || 254)) * 100}%` }"
        ></div>
      </div>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 设备列表表格 -->
    <div class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 170px;">IP 地址</th>
            <th style="width: 170px;">MAC 物理地址</th>
            <th style="width: 220px;">设备备注</th>
            <th style="width: 110px;">延迟 (RTT)</th>
            <th style="width: 110px;">角色 / 标识</th>
            <th>快捷操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="dev in devices" :key="dev.ip" :class="{ 'row-self': dev.isSelf }">
            <td class="col-ip">
              <strong>{{ dev.ip }}</strong>
            </td>
            <td class="col-mac">
              <code>{{ dev.mac || '—' }}</code>
            </td>
            <td class="col-remark">
              <div v-if="editingKey === dev.ip" class="remark-edit-box">
                <input
                  ref="remarkInputRef"
                  v-model="editingValue"
                  class="input-inline"
                  type="text"
                  placeholder="如: 开发服务器、打印机"
                  maxlength="30"
                  @keydown.enter="saveRemark(dev)"
                  @keydown.esc="cancelEditRemark"
                  @blur="saveRemark(dev)"
                />
              </div>
              <div
                v-else
                class="remark-display"
                :class="{ 'has-remark': Boolean(dev.remark) }"
                title="点击修改备注"
                @click="startEditRemark(dev)"
              >
                <span v-if="dev.remark" class="remark-text">{{ dev.remark }}</span>
                <span v-else class="remark-placeholder">+ 添加备注</span>
                <span class="edit-icon">✎</span>
              </div>
            </td>
            <td>
              <span class="rtt-tag" :class="dev.rttMs < 20 ? 'fast' : (dev.rttMs < 500 ? 'normal' : 'slow')">
                {{ dev.rttMs }} ms
              </span>
            </td>
            <td>
              <span v-if="dev.isSelf" class="badge badge-self">本机</span>
              <span v-else class="badge badge-client">在线设备</span>
            </td>
            <td>
              <button class="btn-action" title="复制 IP 到剪贴板" @click="copyIP(dev.ip)">
                复制 IP
              </button>
            </td>
          </tr>
          <tr v-if="devices.length === 0 && !scanning">
            <td colspan="6" class="empty-hint">暂无扫描数据，请输入或选择网段后点击“开始扫描”</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.lan-page {
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

.form-item {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
}

.form-item label {
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}

.input-combo-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 520px;
  width: 100%;
}

.input-box {
  padding: 6px 12px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 13px;
  background: #fff;
  flex: 1;
  font-family: Consolas, monospace;
}
.input-box:focus {
  border-color: var(--accent);
  outline: none;
}

.select-addon {
  padding: 6px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 12px;
  background: #fff;
  max-width: 220px;
  color: var(--text-muted);
}

.quick-helpers {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 0 4px;
}

.helper-label {
  font-size: 12px;
  color: var(--text-subtle);
}

.helper-tag {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  font-family: Consolas, monospace;
  transition: all 0.15s ease;
}
.helper-tag:hover {
  background: var(--bg-hover);
  color: var(--accent);
  border-color: var(--accent);
}

.btn-group {
  display: flex;
  gap: 8px;
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

.btn-danger {
  background: var(--danger);
  color: #fff;
}

.btn-secondary {
  background: #fff;
  border-color: var(--border-color);
  color: var(--text-main);
}
.btn-secondary:hover {
  background: var(--bg-hover);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 进度条 */
.progress-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  padding: 10px 16px;
  border-radius: 8px;
}

.progress-meta {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-muted);
}

.found-badge {
  font-weight: 600;
  color: var(--accent);
}

.progress-bar-wrap {
  height: 6px;
  background: var(--bg-hover);
  border-radius: 3px;
  overflow: hidden;
}

.progress-bar-inner {
  height: 100%;
  background: var(--accent);
  transition: width 0.15s ease;
}

.error-box {
  padding: 10px 14px;
  background: #ffebe9;
  color: var(--danger);
  border: 1px solid rgba(207, 34, 46, 0.2);
  border-radius: 6px;
  font-size: 13px;
}

/* 表格展示 */
.table-container {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  overflow: hidden;
}

.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  text-align: left;
}

.tbl th {
  background: var(--bg-app);
  padding: 10px 14px;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border-color);
}

.tbl td {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color);
  color: var(--text-main);
}

.tbl tr:last-child td {
  border-bottom: none;
}

.row-self {
  background: #f0f7ff;
}

.col-ip strong {
  font-family: Consolas, monospace;
}

.col-mac code {
  font-family: Consolas, monospace;
  font-size: 12px;
  color: var(--text-muted);
}

/* 备注单元格与内联编辑 */
.col-remark {
  position: relative;
}

.remark-display {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 3px 6px;
  border-radius: 4px;
  transition: background 0.15s ease;
  max-width: 100%;
}

.remark-display:hover {
  background: var(--bg-hover);
}

.remark-text {
  font-weight: 500;
  color: #1e293b;
}

.remark-placeholder {
  color: var(--text-subtle);
  font-size: 12px;
}

.edit-icon {
  font-size: 11px;
  color: var(--text-subtle);
  opacity: 0;
  transition: opacity 0.15s ease;
}

.remark-display:hover .edit-icon {
  opacity: 1;
}

.remark-edit-box {
  display: flex;
  align-items: center;
}

.input-inline {
  width: 100%;
  padding: 4px 8px;
  font-size: 13px;
  border: 1px solid var(--accent);
  border-radius: 4px;
  outline: none;
  background: #fff;
  box-shadow: 0 0 0 2px rgba(47, 111, 237, 0.15);
}

.rtt-tag {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 600;
}
.rtt-tag.fast {
  background: #dafbe1;
  color: var(--success);
}
.rtt-tag.normal {
  background: #fff8c5;
  color: #9a6700;
}
.rtt-tag.slow {
  background: #f0f2f5;
  color: var(--text-muted);
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 12px;
  font-weight: 500;
}
.badge-self {
  background: #ddf4ff;
  color: #0969da;
}
.badge-client {
  background: var(--bg-hover);
  color: var(--text-muted);
}

.btn-action {
  padding: 3px 10px;
  border: 1px solid var(--border-color);
  border-radius: 5px;
  background: #fff;
  cursor: pointer;
  font-size: 12px;
  transition: all 0.15s ease;
}
.btn-action:hover {
  border-color: var(--accent);
  color: var(--accent);
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
