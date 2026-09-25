<script setup lang="ts">
import { onMounted, ref, shallowRef } from 'vue'
import * as PortKillAPI from '../../bindings/hanxi/internal/modules/portkill'
import type { PortOccupant, KillResult } from '../../bindings/hanxi/internal/modules/portkill/models'
import type { Record as HistoryRecord } from '../../bindings/hanxi/internal/history/models'
import { getErrorMessage } from '../utils/errors'
import { useConfirm } from '../composables/useConfirm'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import HistoryPanel from '../components/tool/HistoryPanel.vue'
import UiHistoryDialog from '../components/ui/UiHistoryDialog.vue'

const { showToast } = useToast()
const { confirm } = useConfirm()
const { copyWithToast } = useClipboard()

const inputPort = ref<number | ''>('')
const loading = ref(false)
const occupants = shallowRef<PortOccupant[]>([])
const listeningList = shallowRef<PortOccupant[]>([])
const searching = ref(false)
const errorMsg = ref('')

// 快捷端口预设
const QUICK_PORTS = [80, 443, 3000, 5173, 8080, 8000, 3306, 6379, 27017]

// 查杀串行门禁：一次查杀（含 UAC 提权等待）未完成前不再受理新的释放
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

// 释放端口：危险确认经全局 useConfirm 单例（原视图自挂 ConfirmDialog 已收编）；
// 确认后弹层落定，查杀期由 killing 门禁全页释放按钮（含 UAC 提权等待窗口）。
async function requestKill(occ: PortOccupant) {
  if (killing.value) return
  const accepted = await confirm({
    title: '确认终止进程并释放端口？',
    description: '即将终止以下进程，端口将被立即释放：',
    confirmLabel: '确认终止',
    tone: 'danger',
    details: killDetails(occ),
  })
  if (!accepted) return
  await doKill(occ)
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

// 确认框明细（原手搓 modal 的信息行逐字迁移到标准 ConfirmDialog details）
function killDetails(occ: PortOccupant) {
  const rows = [
    { label: '目标端口', value: `:${occ.port} (${occ.protocol})` },
    { label: '进程名称', value: occ.processName || '未知进程' },
    { label: '进程 PID', value: String(occ.pid) },
  ]
  if (occ.exePath) rows.push({ label: '程序路径', value: occ.exePath })
  return rows
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

// ---------- 历史记录（弹窗；应用回填端口并触发查询，Q6 行内数据直用） ----------
const showHistory = ref(false)

function applyHistoryPort(rec: HistoryRecord) {
  const raw = String(rec.input ?? '').trim()
  if (!/^\d+$/.test(raw)) {
    showToast('仅「端口查询」类历史可回填端口号（查杀记录含 PID 标识）')
    return
  }
  showHistory.value = false
  void searchPort(Number(raw))
}



// ---------- 输出区复制（PLAN_CLIPBOARD §3.2C：PortKill 整页零复制的缺口补齐） ----------
// 整表导出按"一行一条、Tab 分隔"，粘进表格/文档即可对齐。
function occupantLines(list: PortOccupant[]): string {
  return list
    .map((o) => [o.protocol, `${o.localIp}:${o.port}`, `PID ${o.pid}`, o.processName || '—', o.exePath || '—'].join('\t'))
    .join('\n')
}

function copyRows(list: PortOccupant[], tip: string) {
  if (!list.length) {
    showToast('暂无可复制的记录')
    return
  }
  void copyWithToast(occupantLines(list), tip)
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
      <button class="btn btn-secondary btn-small" :aria-expanded="showHistory" @click="showHistory = true" title="最近的端口查询与查杀留档，可回填重查">
        🕘 历史
      </button>
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
        <span class="pk-head-actions">
          <UiStatusChip tone="danger">占用中 ({{ occupants.length }})</UiStatusChip>
          <button class="btn btn-secondary btn-small" @click="copyRows(occupants, `已复制 ${occupants.length} 条占用详情`)">复制详情</button>
        </span>
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
              <td class="col-path">
                <template v-if="occ.exePath">
                  <span class="path-text" :title="occ.exePath">{{ occ.exePath }}</span>
                  <button class="link-button" :aria-label="`复制 PID ${occ.pid} 的程序路径`" @click="copyWithToast(occ.exePath, '已复制程序路径')">复制</button>
                </template>
                <template v-else>—</template>
              </td>
              <td>{{ formatTime(occ.startedAt as any) }}</td>
              <td>
                <button
                  v-if="!occ.isProtected"
                  class="btn btn-danger-outline btn-micro"
                  :disabled="killing"
                  @click="requestKill(occ)"
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
        <span class="pk-head-actions">
          <button class="btn btn-secondary btn-small" @click="copyRows(listeningList, `已复制 ${listeningList.length} 条监听端口`)">复制列表</button>
          <button class="btn btn-secondary btn-small" :disabled="loading" @click="loadListeningPorts">
            {{ loading ? '刷新中…' : '刷新列表' }}
          </button>
        </span>
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
              <td class="col-path">
                <template v-if="occ.exePath">
                  <span class="path-text" :title="occ.exePath">{{ occ.exePath }}</span>
                  <button class="link-button" :aria-label="`复制 PID ${occ.pid} 的程序路径`" @click="copyWithToast(occ.exePath, '已复制程序路径')">复制</button>
                </template>
                <template v-else>—</template>
              </td>
              <td>
                <button
                  v-if="!occ.isProtected"
                  class="btn btn-danger-outline btn-micro"
                  :disabled="killing"
                  @click="requestKill(occ)"
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

    <!-- 历史记录弹窗（共享壳承载 Esc 分层/焦点契约，面板自取数） -->
    <UiHistoryDialog :open="showHistory" title="查杀历史" @close="showHistory = false">
      <HistoryPanel func-type="portkill" @apply="applyHistoryPort" />
    </UiHistoryDialog>
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
  font-size: var(--text-md);
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
  font-size: var(--text-sm);
  color: var(--color-text-subtle);
}

.tag-btn {
  background: var(--surface-page);
  border: 1px solid var(--color-border);
  border-radius: 4px;
  padding: 3px 8px;
  font-size: var(--text-sm);
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
  font-size: var(--text-md);
  font-weight: 600;
  margin: 0;
}

.table-wrap {
  overflow-x: auto;
}

.col-path {
  max-width: 320px;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  font-family: var(--font-mono);
  white-space: nowrap;
}

/* 路径文本省略号与悬停浮现的"复制"钮共存：截断收进 span，钮恒定可见域 */
.col-path .path-text {
  display: inline-block;
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: bottom;
}
.col-path .link-button {
  opacity: 0;
  transition: opacity var(--motion-base) ease;
}
tr:hover .col-path .link-button,
.col-path:focus-within .link-button {
  opacity: 1;
}

.pk-head-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.port-num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  color: var(--color-primary);
}

/* 行内"释放端口"危险微钮收编全局 .btn .btn-danger-outline .btn-micro 标准形，
   原私有软底 .btn-kill（含 hover 实底反白变体）删净落回（目视项）。 */

/* 裸文字 32px 档副本删净，落回全局 .empty-hint 标准虚线卡形（§9.6-10 定档裁决——目视项；
   挂点为 <td colspan>，虚线卡染进单元格为预期收编效果） */

</style>
