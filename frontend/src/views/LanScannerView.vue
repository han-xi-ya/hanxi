<script setup lang="ts">
import { ref, shallowRef, onMounted, nextTick } from 'vue'
import * as LanAPI from '../../bindings/hanxi/internal/modules/lan'
import type { SubnetInfo, DeviceInfo, LanProgress } from '../../bindings/hanxi/internal/modules/lan/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useClipboard } from '../composables/useClipboard'
import PageHeader from '../components/ui/PageHeader.vue'

const { showToast } = useToast()
const { copy } = useClipboard()

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

async function copyIP(ip: string) {
  // 剪贴板两级策略收编进 useClipboard；失败不再"谎报成功 toast"（原实现不 await，reject 时 toast 仍弹已复制）
  const ok = await copy(ip)
  showToast(ok ? `已复制 IP: ${ip}` : '复制失败')
}

function startEditRemark(dev: DeviceInfo) {
  editingKey.value = dev.ip
  editingValue.value = dev.remark || ''
  nextTick(() => {
    // 修复注记（组 G2）：ref 位于 v-for 子树内，Vue 语义下收集为数组——
    // 原实现 .focus() 直接调用必抛 TypeError（自动聚焦从未生效、线上静默报错）。
    // 此处按作者本意恢复"进入编辑即聚焦并全选"；若主线判定该行为属变更，回退本段即可。
    const el = Array.isArray(remarkInputRef.value) ? remarkInputRef.value[0] : remarkInputRef.value
    if (el) {
      el.focus()
      el.select()
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

// 扫描实时进度推流（setup 期订阅防丢早期事件，卸载自动注销）
useWailsEvent<LanProgress>('lan:progress', (data) => {
  if (data) {
    progress.value = data
  }
})

onMounted(() => {
  loadSubnets()
})
</script>

<template>
  <section class="page lan-page">
    <PageHeader title="局域网扫描" subtitle="支持标准 CIDR 子网 (如 /24、/22) 与自定义 IP 范围扫描，毫秒级探测活跃设备与持久化备注。" />

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
              <span v-if="dev.isSelf" class="chip chip-information">本机</span>
              <span v-else class="chip chip-neutral">在线设备</span>
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
/* 页头/错误框/.btn 家族/.tbl 基样式/badge→chip/空态等由 PageHeader 与 components.css 全局原子接管；
   下方仅保留本视图独有布局与"与全局原子的差异覆盖"（行内备注编辑、进度卡、RTT 分级等）。
   （原 .toast 死代码 + fadeIn keyframes 已删——模板从未引用） */
.lan-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.control-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
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
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-size: 13px;
  background: var(--surface-panel);
  flex: 1;
  font-family: var(--font-mono);
}
.input-box:focus {
  border-color: var(--color-primary);
  outline: none;
}

.select-addon {
  padding: 6px 10px;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-size: 12px;
  background: var(--surface-panel);
  max-width: 220px;
  color: var(--color-text-muted);
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
  color: var(--color-text-subtle);
}

.helper-tag {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--color-text-muted);
  cursor: pointer;
  font-family: var(--font-mono);
  transition: all var(--motion-base) ease;
}
.helper-tag:hover {
  background: var(--surface-hover);
  color: var(--color-primary);
  border-color: var(--color-primary);
}

.btn-group {
  display: flex;
  gap: 8px;
}

/* 实心红"停止扫描"为本视图专属变体（全局只有 btn-danger-outline 描边形） */
.btn-danger {
  background: var(--state-danger);
  color: var(--color-on-primary);
}
.btn-danger:hover:not(:disabled) {
  filter: brightness(0.92);
}

/* 进度条 */
.progress-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  padding: 10px 16px;
  border-radius: 8px;
}

.progress-meta {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--color-text-muted);
}

.found-badge {
  font-weight: 600;
  color: var(--color-primary);
}

.progress-bar-wrap {
  height: 6px;
  background: var(--surface-hover);
  border-radius: 3px;
  overflow: hidden;
}

.progress-bar-inner {
  height: 100%;
  background: var(--color-primary);
  transition: width var(--motion-base) ease;
}

/* 表格展示（.tbl 基样式全局接管，此处仅本视图差异：更宽内距与 page 色表头） */
.table-container {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  overflow: hidden;
}

.tbl th {
  background: var(--surface-page);
  padding: 10px 14px;
}

.tbl td {
  padding: 10px 14px;
  color: var(--color-text);
}

.row-self {
  background: var(--state-information-soft);
}

.col-ip strong {
  font-family: var(--font-mono);
}

.col-mac code {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-text-muted);
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
  transition: background var(--motion-base) ease;
  max-width: 100%;
}

.remark-display:hover {
  background: var(--surface-hover);
}

.remark-text {
  font-weight: 500;
  color: var(--color-text);
}

.remark-placeholder {
  color: var(--color-text-subtle);
  font-size: 12px;
}

.edit-icon {
  font-size: 11px;
  color: var(--color-text-subtle);
  opacity: 0;
  transition: opacity var(--motion-base) ease;
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
  border: 1px solid var(--color-primary);
  border-radius: 4px;
  outline: none;
  background: var(--surface-panel);
  box-shadow: 0 0 0 2px var(--color-primary-glow);
}

.rtt-tag {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 600;
}
.rtt-tag.fast {
  background: var(--state-positive-soft);
  color: var(--state-positive);
}
.rtt-tag.normal {
  background: var(--state-warning-soft);
  color: var(--state-warning);
}
.rtt-tag.slow {
  background: var(--surface-hover);
  color: var(--color-text-muted);
}

.btn-action {
  padding: 3px 10px;
  border: 1px solid var(--color-border);
  border-radius: 5px;
  background: var(--surface-panel);
  cursor: pointer;
  font-size: 12px;
  transition: all var(--motion-base) ease;
}
.btn-action:hover {
  border-color: var(--color-primary);
  color: var(--color-primary);
}

.empty-hint {
  text-align: center;
  padding: 32px 0;
  color: var(--color-text-subtle);
}
</style>
