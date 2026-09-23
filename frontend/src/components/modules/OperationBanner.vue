<script setup lang="ts">
// 模块中心顶部操作条（Wave 4）：恢复条（resumable）与在途条（active）两用。
// 消费纪律：只读 useOperations 投影，文案全取 constants/status 词表，零本地状态推断；
// 观察面刷新失败时保留旧投影并如实标注（stale），不清空、不假装新鲜。
// 「忽略残留」会把该事务以 failed 收口进账本（审计单收口、不可翻案）且清理托管现场，
// 属难以撤销动作，故走确认框；单写约束（同模块存在未收口事务时开不了新事务）如实告知。
import { ref } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { Operation } from '../../../bindings/hanxi/internal/extapi/models'
import { resumableTxnID, useOperations } from '../../composables/useOperations'
import { useModuleCatalog } from '../../composables/useModuleCatalog'
import { useConfirm } from '../../composables/useConfirm'
import { useToast } from '../../composables/useToast'
import { getErrorMessage } from '../../utils/errors'
import { MODULE_PRESENTATION } from '../../constants/navigation'
import { operationKindMeta, operationPhaseText, operationStatusMeta } from '../../constants/status'
import AppIcon from '../ui/AppIcon.vue'
import UiBanner from '../ui/UiBanner.vue'
import UiButton from '../ui/UiButton.vue'
import UiProgressBar from '../ui/UiProgressBar.vue'

const emit = defineEmits<{
  /** 前往模块页重试（路由解析与模块中心 open 同一口径：建档优先，未建档回落 /ext/<id>）。 */
  (e: 'navigate', route: string): void
}>()

const { resumable, active, error, loading, refresh } = useOperations()
// 模块名唯一来源仍是目录投影（本组件只读，不另立名册）
const { entries } = useModuleCatalog()
const { confirm } = useConfirm()
const { showToast, showErrorToast } = useToast()

/** 正在发起忽略的事务 ID：RPC 在途期间禁用该行按钮（维稳定、防重复翻案）。 */
const dismissing = ref<Record<string, boolean>>({})

/** N26 取消信号在途的事务 ID：按钮防连点（取消是信号不是结果——worker
 *  收口后经 operation:changed 广播回流，本组件只负责把信号发出去）。 */
const cancelling = ref<Record<string, boolean>>({})

async function cancel(op: Operation) {
  const txnID = String(op.txnId ?? '')
  if (!txnID || cancelling.value[txnID]) return
  cancelling.value = { ...cancelling.value, [txnID]: true }
  try {
    await AppAPI.AppService.CancelOperation(String(op.moduleId), txnID)
    showToast('已发送取消请求，该操作正在收口')
    await refresh()
  } catch (err: unknown) {
    showErrorToast(`取消失败: ${getErrorMessage(err)}`)
  } finally {
    const next = { ...cancelling.value }
    delete next[txnID]
    cancelling.value = next
  }
}

function moduleName(op: Operation): string {
  return entries.value.find((e) => e.catalog.id === op.moduleId)?.catalog.name || op.moduleId
}

function routeOf(moduleId: string): string {
  return MODULE_PRESENTATION[moduleId]?.route || `/ext/${moduleId}`
}

/** 阶段短语：phase 为空（尚未步进）时不编造阶段名。 */
function phaseText(op: Operation): string {
  const phase = String(op.phase ?? '')
  return phase ? operationPhaseText(phase) : ''
}

async function dismiss(op: Operation) {
  const name = moduleName(op)
  const accepted = await confirm({
    title: `忽略「${name}」的残留事务？`,
    description: '忽略会按背书清理该事务的托管现场，并在账本以失败收口；收口后的审计记录不可翻案。',
    confirmLabel: '忽略残留',
    tone: 'danger',
    details: [
      { label: '清理内容', value: '该事务遗留的暂存目录与安装现场' },
      { label: '账本影响', value: '以失败收口登记（保留审计记录，不可翻案）' },
      { label: '收口前限制', value: '该模块在此之前无法开始新的安装事务' },
    ],
  })
  if (!accepted) return
  const txnID = resumableTxnID(op)
  if (dismissing.value[txnID]) return
  dismissing.value = { ...dismissing.value, [txnID]: true }
  try {
    await AppAPI.AppService.DismissResumable(txnID)
    showToast(`已忽略「${name}」的残留事务`)
    await refresh()
  } catch (err: unknown) {
    showErrorToast(`忽略残留事务失败: ${getErrorMessage(err)}`)
  } finally {
    const next = { ...dismissing.value }
    delete next[txnID]
    dismissing.value = next
  }
}
</script>

<template>
  <div
    v-if="resumable.length || active.length || error"
    class="operation-banner"
    role="region"
    aria-label="模块操作动态"
  >
    <!-- 恢复条：崩溃/强杀遗留的未收口事务，逐条给出"中断于何处 + 为什么 + 两条出口" -->
    <UiBanner v-if="resumable.length" tone="warn" class="op-resume">
      <p class="op-head">
        <AppIcon name="alert-triangle" :size="15" />
        <strong>有 {{ resumable.length }} 笔未收口的模块事务待处理</strong>
      </p>
      <ul class="op-list">
        <li v-for="op in resumable" :key="op.id" class="op-item">
          <div class="op-copy">
            <span class="op-title">
              「{{ moduleName(op) }}」的{{ operationKindMeta(String(op.kind)).text }}上次操作失败于{{ phaseText(op) ? `「${phaseText(op)}」阶段` : '尚未步进的阶段' }}
            </span>
            <span class="op-reason">{{ op.error?.message || '后端未提供原因说明' }}</span>
          </div>
          <div class="op-actions">
            <UiButton small variant="secondary" @click="emit('navigate', routeOf(op.moduleId))">前往重试</UiButton>
            <UiButton
              small
              variant="ghost"
              :disabled="!!dismissing[resumableTxnID(op)]"
              title="清理托管现场并以失败收口登记账本"
              @click="dismiss(op)"
            >{{ dismissing[resumableTxnID(op)] ? '忽略中…' : '忽略残留' }}</UiButton>
          </div>
        </li>
      </ul>
      <p class="op-note">
        单写约束：忽略残留前，该模块无法开始新的安装事务（同一模块同时只允许一笔未收口事务）。
      </p>
    </UiBanner>

    <!-- 在途条：queued/running 真进度；progress=nil 不编造百分比，cancellable=false 不伪装可取消 -->
    <UiBanner v-if="active.length" tone="info" class="op-running">
      <p class="op-head">
        <AppIcon name="activity" :size="15" />
        <strong>{{ active.length }} 笔操作进行中</strong>
      </p>
      <ul class="op-list">
        <li v-for="op in active" :key="op.id" class="op-item op-item-block">
          <div class="op-copy">
            <span class="op-title">
              「{{ moduleName(op) }}」{{ operationKindMeta(String(op.kind)).text }} · {{ operationStatusMeta(String(op.status)).text }}<template v-if="phaseText(op)"> · {{ phaseText(op) }}</template>
            </span>
            <span class="op-progress-value mono">
              {{ op.progress == null ? '进度不可量化' : `${Math.round(op.progress)}%` }}
            </span>
          </div>
          <UiProgressBar v-if="op.progress != null" :percent="op.progress" />
          <div v-if="op.cancellable && op.txnId" class="op-actions">
            <UiButton
              small
              variant="ghost"
              :disabled="!!cancelling[String(op.txnId)]"
              title="中断该操作：下载/解包即时停止，半截现场自动清理；已拉起的外部安装器无法中断（如实等其收口）"
              @click="cancel(op)"
            >{{ cancelling[String(op.txnId)] ? '取消中…' : '取消' }}</UiButton>
          </div>
          <p v-else-if="!op.cancellable" class="op-note">该操作不支持取消，请等待其自然收口。</p>
        </li>
      </ul>
    </UiBanner>

    <!-- 观察面自身故障：保留旧投影时如实标注（不静默回退成"没有操作"） -->
    <UiBanner v-if="error" tone="error" class="op-stale">
      <span>
        操作动态{{ loading ? '重试中' : '刷新失败' }}，当前展示为上一次投影: {{ error }}
      </span>
      <button type="button" class="state-action" :disabled="loading" @click="refresh()">重试</button>
    </UiBanner>
  </div>
</template>

<style scoped>
.operation-banner { display: flex; flex-direction: column; gap: 10px; }
.op-head {
  margin: 0 0 6px;
  display: flex; align-items: center; gap: 7px;
  font-size: var(--text-sm);
}
.op-list { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 8px; }
.op-item {
  display: flex; align-items: center; justify-content: space-between;
  gap: 10px; flex-wrap: wrap;
}
.op-item-block { flex-direction: column; align-items: stretch; gap: 6px; }
.op-copy { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; flex-wrap: wrap; min-width: 0; }
.op-title { font-size: var(--text-sm); font-weight: 650; }
.op-reason { font-size: var(--text-xs); opacity: 0.85; min-width: 0; flex: 1; }
.op-progress-value { font-size: var(--text-xs); font-variant-numeric: tabular-nums; }
.op-actions { display: flex; align-items: center; gap: 6px; flex: none; }
.op-note {
  margin: 0; font-size: var(--text-xs); line-height: 1.5; opacity: 0.9;
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
}
.op-stale { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }

/* 窄屏：动作钮与进度行改纵向堆叠，不挤压文字（本组件无动效，无需 reduced-motion 降级） */
@media (max-width: 520px) {
  .op-item { flex-direction: column; align-items: stretch; }
  .op-actions { justify-content: flex-end; }
}
</style>
