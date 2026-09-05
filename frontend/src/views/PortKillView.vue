<script setup lang="ts">
import { ref, shallowRef, computed, onMounted } from 'vue'
import * as PortKillAPI from '../../bindings/hanxi/internal/modules/portkill'
import type { PortOccupant, KillResult } from '../../bindings/hanxi/internal/modules/portkill/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'

const { showToast } = useToast()

const inputPort = ref<number | ''>('')
const loading = ref(false)
const occupants = shallowRef<PortOccupant[]>([])
const listeningList = shallowRef<PortOccupant[]>([])
const searching = ref(false)
const errorMsg = ref('')

// 快捷端口预设
const QUICK_PORTS = [80, 443, 3000, 5173, 8080, 8000, 3306, 6379, 27017]

// 待终止的确认弹窗状态
const targetToKill = ref<PortOccupant | null>(null)
const killing = ref(false)

function sortReleasableFirst(list: PortOccupant[]) {
  return [...list].sort((a, b) => Number(a.isProtected) - Number(b.isProtected))
}

async function loadListeningPorts() {
  loading.value = true
  errorMsg.value = ''
  try {
    const list = await PortKillAPI.PortKillService.ListListeningPorts()
    listeningList.value = sortReleasableFirst(list ?? [])
  } catch (e: unknown) {
    errorMsg.value = `获取端口占用列表失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

async function searchPort(portVal?: number) {
  const p = portVal !== undefined ? portVal : inputPort.value
  if (!p || typeof p !== 'number' || p <= 0 || p > 65535) {
    showToast('请输入有效的端口号 (1 - 65535)')
    return
  }

  inputPort.value = p
  searching.value = true
  errorMsg.value = ''
  try {
    const res = await PortKillAPI.PortKillService.QueryPort(p)
    occupants.value = sortReleasableFirst(res ?? [])
    if (occupants.value.length === 0) {
      showToast(`端口 ${p} 当前未被占用`)
    }
  } catch (e: unknown) {
    errorMsg.value = `查询端口失败: ${getErrorMessage(e)}`
  } finally {
    searching.value = false
  }
}

function selectQuickPort(port: number) {
  inputPort.value = port
  searchPort(port)
}

function confirmKill(occ: PortOccupant) {
  targetToKill.value = occ
}

function cancelKill() {
  targetToKill.value = null
}

async function doKill(occ: PortOccupant) {
  killing.value = true
  try {
    let startedUnix = 0
    if (occ.startedAt) {
      startedUnix = Math.floor(new Date(occ.startedAt).getTime() / 1000)
    }

    // 1. 先尝试普通权限查杀
    let res: KillResult = await PortKillAPI.PortKillService.KillProcess(occ.pid, occ.exePath, startedUnix)

    // 2. 如果返回需要提权
    if (!res.success && res.needElevate) {
      showToast('普通权限不足，正在调起 Windows UAC 提权终止…')
      res = await PortKillAPI.PortKillService.KillProcessElevated(occ.pid)
    }

    if (res.success) {
      showToast(`已成功终止进程 PID ${occ.pid} (${occ.processName || '未知'})`)
      targetToKill.value = null
      // 重新刷新列表或查询
      if (inputPort.value) {
        searchPort()
      }
      loadListeningPorts()
    } else {
      showToast(`查杀失败: ${res.errorMessage}`)
    }
  } catch (e: unknown) {
    showToast(`操作异常: ${getErrorMessage(e)}`)
  } finally {
    killing.value = false
  }
}

// 确认对话框明细（原手搓 modal 的信息行逐字迁移到标准 ConfirmDialog details）
const killDetails = computed(() => {
  const occ = targetToKill.value
  if (!occ) return []
  const rows = [
    { label: '目标端口', value: `:${occ.port} (${occ.protocol})` },
    { label: '进程名称', value: occ.processName || '未知进程' },
    { label: '进程 PID', value: String(occ.pid) },
  ]
  if (occ.exePath) rows.push({ label: '程序路径', value: occ.exePath })
  return rows
})

function formatTime(timeStr: string) {
  if (!timeStr || timeStr.startsWith('0001-01-01')) return '—'
  try {
    const d = new Date(timeStr)
    return d.toLocaleTimeString('zh-CN', { hour12: false })
  } catch {
    return timeStr
  }
}

onMounted(() => {
  loadListeningPorts()
})
</script>

<template>
  <section class="page portkill-page">
    <div class="header-row">
      <div>
        <h1>释放端口</h1>
        <p class="subtitle">精准定位端口占用进程，防 PID 复用令牌复核，支持 UAC 提权快速释放。</p>
      </div>
    </div>

    <!-- 端口精准搜索与快捷标签 -->
    <div class="search-panel">
      <div class="search-input-wrap">
        <input
          v-model.number="inputPort"
          type="number"
          min="1"
          max="65535"
          placeholder="输入端口号 (如 8080)"
          class="input-port"
          @keydown.enter="searchPort()"
        />
        <button class="btn btn-primary" :disabled="searching" @click="searchPort()">
          {{ searching ? '查询中…' : '查询占用' }}
        </button>
      </div>

      <div class="quick-tags">
        <span class="tag-label">常用开发端口:</span>
        <button
          v-for="qp in QUICK_PORTS"
          :key="qp"
          class="tag-btn"
          :class="{ active: inputPort === qp }"
          @click="selectQuickPort(qp)"
        >
          :{{ qp }}
        </button>
      </div>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <!-- 单个端口查询结果卡片 -->
    <div v-if="occupants.length > 0" class="card result-card">
      <div class="card-header">
        <h3>端口 :{{ inputPort }} 占用详情</h3>
        <UiStatusChip tone="danger">占用中 ({{ occupants.length }})</UiStatusChip>
      </div>
      <div class="table-wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 80px;">协议</th>
              <th style="width: 140px;">绑定地址</th>
              <th style="width: 90px;">PID</th>
              <th style="width: 160px;">进程名</th>
              <th>可执行路径</th>
              <th style="width: 100px;">启动时间</th>
              <th style="width: 100px;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="occ in occupants" :key="`${occ.protocol}-${occ.pid}`">
              <td><strong>{{ occ.protocol }}</strong></td>
              <td><code>{{ occ.localIp }}:{{ occ.port }}</code></td>
              <td><code>{{ occ.pid }}</code></td>
              <td class="col-name"><strong>{{ occ.processName || '—' }}</strong></td>
              <td class="col-path" :title="occ.exePath">{{ occ.exePath || '—' }}</td>
              <td>{{ formatTime(occ.startedAt as any) }}</td>
              <td>
                <button
                  v-if="!occ.isProtected"
                  class="btn-kill"
                  @click="confirmKill(occ)"
                >
                  释放端口
                </button>
                <UiStatusChip v-else tone="neutral">系统保护</UiStatusChip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 系统监听中端口大表 -->
    <div class="card list-card">
      <div class="card-header">
        <h3>当前活跃监听端口 (LISTEN)</h3>
        <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadListeningPorts">
          {{ loading ? '刷新中…' : '刷新列表' }}
        </button>
      </div>

      <div class="table-wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 90px;">端口</th>
              <th style="width: 80px;">协议</th>
              <th style="width: 140px;">绑定地址</th>
              <th style="width: 90px;">PID</th>
              <th style="width: 170px;">进程名称</th>
              <th>程序路径</th>
              <th style="width: 100px;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="occ in listeningList" :key="`listen-${occ.port}-${occ.pid}`">
              <td><span class="port-num">:{{ occ.port }}</span></td>
              <td><strong>{{ occ.protocol }}</strong></td>
              <td><code>{{ occ.localIp }}</code></td>
              <td><code>{{ occ.pid }}</code></td>
              <td class="col-name"><strong>{{ occ.processName || '—' }}</strong></td>
              <td class="col-path" :title="occ.exePath">{{ occ.exePath || '—' }}</td>
              <td>
                <button
                  v-if="!occ.isProtected"
                  class="btn-kill"
                  @click="confirmKill(occ)"
                >
                  释放端口
                </button>
                <UiStatusChip v-else tone="neutral">系统保护</UiStatusChip>
              </td>
            </tr>
            <tr v-if="listeningList.length === 0 && !loading">
              <td colspan="7" class="empty-hint">暂未读取到监听端口或列表为空</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 查杀安全确认：手搓 modal 收编为标准 ConfirmDialog（焦点陷阱/Esc/danger 语义内建；
         busy 由组件统一呈现为「处理中…」，替代原「终止中…」文案——机制归一属迁移预期差异） -->
    <ConfirmDialog
      :open="!!targetToKill"
      title="确认终止进程并释放端口？"
      description="即将终止以下进程，端口将被立即释放："
      tone="danger"
      confirm-label="确认终止"
      :busy="killing"
      :details="killDetails"
      @confirm="targetToKill && doKill(targetToKill)"
      @cancel="cancelKill"
    />
  </section>
</template>

<style scoped>
/* 页头/表格/按钮族/错误框/徽章已由全局原子与 UiStatusChip 接管；
   死代码 .toast/@keyframes fadeIn 与查杀 modal 族（收编进 ConfirmDialog）一并清除。 */
.portkill-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.search-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  padding: 14px 18px;
  border-radius: var(--radius-control);
}

.search-input-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
  max-width: 420px;
}

.input-port {
  flex: 1;
  padding: 8px 12px;
  font-size: 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  outline: none;
  background: var(--surface-soft);
  color: var(--color-text);
}
.input-port:focus {
  border-color: var(--color-primary);
}

.quick-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.tag-label {
  font-size: 12px;
  color: var(--color-text-subtle);
}

.tag-btn {
  background: var(--surface-page);
  border: 1px solid var(--color-border);
  border-radius: 4px;
  padding: 3px 8px;
  font-size: 12px;
  cursor: pointer;
  color: var(--color-text-muted);
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.tag-btn:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}
.tag-btn.active {
  background: var(--surface-selected);
  border-color: var(--color-primary);
  color: var(--color-primary);
  font-weight: 600;
}

/* 表格外壳：全局 .card 原子的内距/投影不适合零内距表格容器，保留独有形状 */
.card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  overflow: hidden;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--color-border);
}

.card-header h3 {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}

.table-wrap {
  overflow-x: auto;
}

.col-path {
  max-width: 280px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--color-text-muted);
  font-family: var(--font-mono);
}

.port-num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  color: var(--color-primary);
}

.btn-kill {
  padding: 3px 8px;
  background: var(--state-danger-soft);
  border: 1px solid var(--state-danger-glow);
  color: var(--state-danger);
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.btn-kill:hover {
  background: var(--state-danger);
  color: var(--color-on-primary);
}

.empty-hint {
  text-align: center;
  padding: 32px 0;
  color: var(--color-text-subtle);
}
</style>
