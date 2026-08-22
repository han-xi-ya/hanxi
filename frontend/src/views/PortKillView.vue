<script setup lang="ts">
import { ref, shallowRef, onMounted } from 'vue'
import * as PortKillAPI from '../../bindings/hubkit/internal/modules/portkill'
import type { PortOccupant, KillResult } from '../../bindings/hubkit/internal/modules/portkill/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

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

async function loadListeningPorts() {
  loading.value = true
  errorMsg.value = ''
  try {
    const list = await PortKillAPI.PortKillService.ListListeningPorts()
    listeningList.value = list ?? []
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
    occupants.value = res ?? []
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
        <span class="badge badge-danger">占用中 ({{ occupants.length }})</span>
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
                <span v-else class="badge badge-prot">系统保护</span>
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
        <button class="btn btn-secondary btn-sm" :disabled="loading" @click="loadListeningPorts">
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
                <span v-else class="badge badge-prot">系统保护</span>
              </td>
            </tr>
            <tr v-if="listeningList.length === 0 && !loading">
              <td colspan="7" class="empty-hint">暂未读取到监听端口或列表为空</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 查杀安全确认对话框 Modal -->
    <div v-if="targetToKill" class="modal-overlay">
      <div class="modal-card">
        <h3>⚠️ 确认终止进程并释放端口？</h3>
        <p class="modal-desc">即将终止以下进程，端口将被立即释放：</p>

        <div class="modal-info-box">
          <div class="info-row">
            <span class="lbl">目标端口:</span>
            <strong>:{{ targetToKill.port }} ({{ targetToKill.protocol }})</strong>
          </div>
          <div class="info-row">
            <span class="lbl">进程名称:</span>
            <strong>{{ targetToKill.processName || '未知进程' }}</strong>
          </div>
          <div class="info-row">
            <span class="lbl">进程 PID:</span>
            <code>{{ targetToKill.pid }}</code>
          </div>
          <div class="info-row" v-if="targetToKill.exePath">
            <span class="lbl">程序路径:</span>
            <span class="path-text">{{ targetToKill.exePath }}</span>
          </div>
        </div>

        <div class="modal-actions">
          <button class="btn btn-secondary" :disabled="killing" @click="cancelKill">取消</button>
          <button class="btn btn-danger" :disabled="killing" @click="doKill(targetToKill)">
            {{ killing ? '终止中…' : '确认终止' }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.portkill-page {
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

.search-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  padding: 14px 18px;
  border-radius: 8px;
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
  border: 1px solid var(--border-color);
  border-radius: 6px;
  outline: none;
}
.input-port:focus {
  border-color: var(--accent);
}

.quick-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.tag-label {
  font-size: 12px;
  color: var(--text-subtle);
}

.tag-btn {
  background: var(--bg-app);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  padding: 3px 8px;
  font-size: 12px;
  cursor: pointer;
  color: var(--text-muted);
  font-family: Consolas, monospace;
}
.tag-btn:hover {
  background: var(--bg-hover);
  color: var(--text-main);
}
.tag-btn.active {
  background: var(--bg-active);
  border-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}

.card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  overflow: hidden;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-color);
}

.card-header h3 {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}

.table-wrap {
  overflow-x: auto;
}

.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  text-align: left;
}

.tbl th {
  background: var(--bg-app);
  padding: 9px 14px;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border-color);
}

.tbl td {
  padding: 9px 14px;
  border-bottom: 1px solid var(--border-color);
  color: var(--text-main);
}

.tbl tr:last-child td {
  border-bottom: none;
}

.col-path {
  max-width: 280px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--text-muted);
  font-family: Consolas, monospace;
}

.port-num {
  font-family: Consolas, monospace;
  font-weight: 700;
  color: var(--accent);
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

.btn-sm {
  padding: 4px 10px;
  font-size: 12px;
}

.btn-primary {
  background: var(--accent);
  color: #fff;
}
.btn-primary:hover {
  background: var(--accent-hover);
}

.btn-secondary {
  background: #fff;
  border-color: var(--border-color);
  color: var(--text-main);
}
.btn-secondary:hover {
  background: var(--bg-hover);
}

.btn-danger {
  background: var(--danger);
  color: #fff;
}

.btn-kill {
  padding: 3px 8px;
  background: #ffebe9;
  border: 1px solid rgba(207, 34, 46, 0.2);
  color: var(--danger);
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.btn-kill:hover {
  background: var(--danger);
  color: #fff;
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 12px;
  font-weight: 500;
}
.badge-danger {
  background: #ffebe9;
  color: var(--danger);
}
.badge-prot {
  background: var(--bg-hover);
  color: var(--text-subtle);
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

/* Modal 弹窗 */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-card {
  background: #fff;
  border-radius: 10px;
  width: 440px;
  padding: 20px 24px;
  box-shadow: 0 10px 25px rgba(0, 0, 0, 0.15);
}

.modal-card h3 {
  margin: 0 0 8px;
  font-size: 16px;
  color: #1f2328;
}

.modal-desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0 0 16px;
}

.modal-info-box {
  background: var(--bg-app);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 13px;
  margin-bottom: 20px;
}

.info-row {
  display: flex;
  gap: 8px;
}

.info-row .lbl {
  color: var(--text-muted);
  width: 70px;
  flex-shrink: 0;
}

.path-text {
  font-family: Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
