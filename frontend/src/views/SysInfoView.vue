<script setup lang="ts">
// 系统信息（N10，借鉴 MooTool 机制）：本机软硬件静态档案一屏。
// 数据源为后端 sysinfo 模块单次只读采集（注册表 + WinAPI + 标准库）；
// 内存/磁盘占用是快照值，页头明示采集时刻；段级采集告警如实呈现（banner）。
import { onMounted, ref } from 'vue'
import * as SysInfoAPI from '../../bindings/hanxi/internal/modules/sysinfo/sysinfoservice'
import type { PurgeResult, Report } from '../../bindings/hanxi/internal/modules/sysinfo/models'
import PageHeader from '../components/ui/PageHeader.vue'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useConfirm } from '../composables/useConfirm'
import { fmtSize } from '../utils/format'
import { sortNetAddresses } from '../utils/netaddrs'

const report = ref<Report | null>(null)
const loading = ref(false)
const loadError = ref('')
const collectedAt = ref('')
// purgeBusy 两清理动作共享：UI 层单飞（后端 purgeMu 同锁互斥，同一时刻至多一种在飞）
const purgeBusy = ref(false)
const purgeResult = ref<PurgeLedgerResult | null>(null)
const purgeKind = ref<'standby' | 'workingsets' | ''>('')
const { showToast } = useToast()
const { confirm } = useConfirm()

// Go 侧 PurgeResult 本轮新增的逐项链账字段 + EmptyWorkingSets 新 RPC。bindings
// 目录由主会话统一再生，再生前生成物不含二者，故视图侧以超集类型 + 显式桥接
// 过渡（再生后可原地收敛删除这两处过渡代码）。
type PurgeLedgerResult = PurgeResult &
  Partial<{
    beforeTotalBytes: number
    pagesMeasured: boolean
    beforeStandbyBytes: number
    afterStandbyBytes: number
    beforeModifiedBytes: number
    afterModifiedBytes: number
    processesEmptied: number
    processesSkipped: number
  }>

const SysInfoExtAPI = SysInfoAPI as unknown as {
  EmptyWorkingSets(): Promise<PurgeLedgerResult>
}

async function refresh() {
  loading.value = true
  loadError.value = ''
  try {
    const rep = await SysInfoAPI.GetReport()
    if (rep) {
      report.value = rep
      collectedAt.value = new Date().toLocaleTimeString()
    }
  } catch (e) {
    loadError.value = getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

async function runPurge(kind: 'standby' | 'workingsets') {
  if (purgeBusy.value) return
  purgeBusy.value = true
  purgeResult.value = null
  purgeKind.value = kind
  try {
    const result = kind === 'standby' ? ((await SysInfoAPI.PurgeStandby()) as PurgeLedgerResult) : await SysInfoExtAPI.EmptyWorkingSets()
    purgeResult.value = result
    if (result.success) {
      showToast(purgeReceiptText(result))
      await refresh()
    } else {
      showToast(result.message || '清理未完成')
    }
  } catch (e) {
    showToast(`清理失败: ${getErrorMessage(e)}`)
  } finally {
    purgeBusy.value = false
  }
}

async function purgeStandby() {
  const accepted = await confirm({
    title: '清理可回收内存？',
    description: '只清理 Windows 的待机/文件缓存，不关闭程序、不删除文件，也不碰进程私有内存与压缩存储。清理后热数据可能需要重新从磁盘加载；完成后给出逐项链账。',
    confirmLabel: '清理可回收内存',
    tone: 'warning',
  })
  if (accepted) await runPurge('standby')
}

async function emptyWorkingSets() {
  const accepted = await confirm({
    title: '清空全部进程工作集？',
    description: '会把所有程序占用的内存强行压回最小，接下来几秒普遍变卡（程序要用时重新取页）。这些页是被"挤回待机列表"而非释放；Windows 本身会自动管理——只想立刻看数字大跌时按。',
    confirmLabel: '清空工作集（会变卡）',
    tone: 'danger',
  })
  if (accepted) await runPurge('workingsets')
}

// fmtBytes：账目专用。fmtSize 把 0 显示为 '—'（档案缺项语义），但实测 0 是
// 结论不是缺项（如清理后待机归零），必须如实显示 "0 B"。
function fmtBytes(v?: number): string {
  return v && v > 0 ? fmtSize(v) : '0 B'
}

function purgeReceiptText(r: PurgeLedgerResult): string {
  if (purgeKind.value === 'workingsets') {
    return `已收 ${r.processesEmptied ?? 0} 个进程工作集（跳过 ${r.processesSkipped ?? 0} 个打不开/受保护），可用内存 +${fmtBytes(r.availableDeltaBytes)}`
  }
  return `清理完成：${purgeLedgerText(r)}`
}

// purgeLedgerText 逐项链账：把"这按钮管哪一格"讲透——待机前后、可用增量；
// 页列表拿不到实测时如实降级（后端禁编数，前端禁装懂）。
function purgeLedgerText(r: PurgeLedgerResult): string {
  const parts: string[] = []
  if (r.pagesMeasured) {
    parts.push(`清理前待机 ${fmtBytes(r.beforeStandbyBytes)} → 后 ${fmtBytes(r.afterStandbyBytes)}`)
    parts.push(`修改页 ${fmtBytes(r.beforeModifiedBytes)} → ${fmtBytes(r.afterModifiedBytes)}`)
  } else {
    parts.push('待机/修改页大小本按钮拿不到实测（需提权页列表读数）')
  }
  parts.push(`可用 +${fmtBytes(r.availableDeltaBytes)}`)
  return parts.join('，')
}

function purgeScopeNote(): string {
  return purgeKind.value === 'workingsets'
    ? '本按钮把各进程在用页强行压回待机列表（不是释放）；压缩存储与文件缓存不在其内，Windows 会自动回补。'
    : '本按钮只管 Windows 待机页列表——压缩存储（内存压缩）本按钮不触碰、未实测；各进程工作集请用旁边"清空工作集"。'
}
onMounted(refresh)

function pct(part?: number, total?: number): number {
  if (!part || !total) return 0
  return Math.min(100, Math.round((part / total) * 100))
}

const VOL_TYPES: Record<string, string> = {
  fixed: '本地磁盘',
  removable: '可移动',
  network: '网络驱动器',
  cdrom: '光驱',
  ramdisk: '内存盘',
  unknown: '未知',
}
</script>

<template>
  <section class="page sysinfo-view">
    <PageHeader
      title="系统信息"
      subtitle="本机软硬件静态档案（注册表与系统 API 只读采集，不写任何系统状态）。"
    >
      <template #actions>
        <button class="btn btn-secondary btn-small" :disabled="loading" @click="refresh">
          {{ loading ? '采集中…' : '↻ 重新采集' }}
        </button>
      </template>
    </PageHeader>

    <div v-if="loadError" class="banner banner-error" role="alert">{{ loadError }}</div>
    <div
      v-else-if="report && report.errors?.length"
      class="banner banner-warn"
      role="status"
    >部分信息采集受限：{{ report.errors.join('；') }}（其余照常呈现）</div>

    <div class="status-toolbar">
      <div class="status-summary">
        <strong>{{ report ? `${report.os?.productName || 'Windows'} · ${report.machine?.hostname || ''}` : '尚未完成采集' }}</strong>
        <span>{{ collectedAt ? `采集于 ${collectedAt}（内存/磁盘占用为快照值）` : '' }}</span>
      </div>
    </div>

    <div v-if="report" class="sys-grid">
      <!-- 处理器 -->
      <div class="sys-card">
        <h3 class="sys-card-title">处理器</h3>
        <div class="sys-kv"><span class="k">型号</span><span class="mono v">{{ report.cpu.name || '—' }}</span></div>
        <div class="sys-kv"><span class="k">厂商</span><span class="v">{{ report.cpu.vendor || '—' }}</span></div>
        <div class="sys-kv"><span class="k">核数 / 线程</span><span class="v">{{ report.cpu.cores || '?' }} C / {{ report.cpu.logical }} T</span></div>
        <div class="sys-kv"><span class="k">标称频率</span><span class="v">{{ report.cpu.speedMHz ? `${report.cpu.speedMHz} MHz（当前值）` : '—' }}</span></div>
        <div v-if="report.cpu.sockets > 1" class="sys-kv"><span class="k">物理处理器</span><span class="v">{{ report.cpu.sockets }} 颗</span></div>
      </div>

      <!-- 内存 -->
      <div class="sys-card">
        <div class="sys-card-head">
          <h3 class="sys-card-title">内存</h3>
          <div class="sys-card-actions">
            <button class="btn btn-secondary btn-small" :disabled="purgeBusy" title="只清 Windows 待机页列表，不关程序、不碰工作集与压缩存储" @click="purgeStandby">
              {{ purgeBusy && purgeKind === 'standby' ? '清理中…' : '清理可回收内存' }}
            </button>
            <button class="btn btn-secondary btn-small" :disabled="purgeBusy" title="RAMMap 同款谨慎动作：强行压回各进程工作集，随后几秒普遍变卡" @click="emptyWorkingSets">
              {{ purgeBusy && purgeKind === 'workingsets' ? '收集中…' : '清空工作集' }}
            </button>
          </div>
        </div>
        <div class="sys-kv"><span class="k">物理总量</span><span class="v">{{ fmtSize(report.memory.totalBytes) }}</span></div>
        <div class="sys-kv"><span class="k">可用</span><span class="v">{{ fmtSize(report.memory.availableBytes) }}</span></div>
        <template v-if="purgeResult && purgeResult.success">
          <div class="sys-kv"><span class="k">清理账目</span><span class="v">{{ purgeLedgerText(purgeResult) }}</span></div>
          <div v-if="purgeKind === 'workingsets'" class="sys-kv"><span class="k">回执</span><span class="v">已收 {{ purgeResult.processesEmptied ?? 0 }} 个进程工作集，跳过 {{ purgeResult.processesSkipped ?? 0 }} 个（打不开/受保护）</span></div>
        </template>
        <div class="sys-bar-wrap" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="report.memory.loadPercent" aria-label="内存占用">
          <div class="sys-bar-inner" :class="{ warn: report.memory.loadPercent > 80 }" :style="{ width: `${report.memory.loadPercent}%` }"></div>
        </div>
        <div class="sys-kv"><span class="k">占用</span><span class="v">{{ report.memory.loadPercent }}%</span></div>
        <div class="sys-kv"><span class="k">提交</span><span class="v mono">{{ fmtSize(report.memory.commitTotal) }} / {{ fmtSize(report.memory.commitLimit) }}</span></div>
        <p class="sys-action-note">{{ purgeResult && purgeResult.success ? purgeScopeNote() : '两按钮各管一格：待机页列表可回收清理 / 全进程工作集强压。均不关程序、不删文件；清理后热数据可能重新从磁盘预热。' }}</p>
        <p v-if="purgeResult && !purgeResult.success" class="sys-action-error" role="alert">{{ purgeResult.message }}</p>
      </div>

      <!-- 操作系统 -->
      <div class="sys-card">
        <h3 class="sys-card-title">操作系统</h3>
        <div class="sys-kv"><span class="k">版本</span><span class="v">{{ report.os.productName }}{{ report.os.edition ? `（${report.os.edition}）` : '' }}</span></div>
        <div class="sys-kv"><span class="k">特性更新</span><span class="v">{{ report.os.version || '—' }}</span></div>
        <div class="sys-kv"><span class="k">构建号</span><span class="v mono">{{ report.os.build }}</span></div>
        <div class="sys-kv"><span class="k">位数</span><span class="v">{{ report.os.is64Bit ? '64 位' : '32 位' }}</span></div>
        <div class="sys-kv"><span class="k">装机日期</span><span class="v">{{ report.os.installDate || '—' }}</span></div>
        <div class="sys-kv"><span class="k">已开机</span><span class="v">{{ report.os.uptime }}</span></div>
      </div>

      <!-- 整机与固件 -->
      <div class="sys-card">
        <h3 class="sys-card-title">整机与固件</h3>
        <div class="sys-kv"><span class="k">计算机名</span><span class="v mono">{{ report.machine.hostname }}</span></div>
        <div class="sys-kv"><span class="k">制造商</span><span class="v">{{ report.machine.manufacturer || '—' }}</span></div>
        <div class="sys-kv"><span class="k">型号</span><span class="v">{{ report.machine.model || '—（组装机常见）' }}</span></div>
        <div class="sys-kv"><span class="k">BIOS</span><span class="v">{{ [report.machine.biosVendor, report.machine.biosVersion].filter(Boolean).join(' ') || '—' }}</span></div>
        <div v-if="report.machine.systemSku" class="sys-kv"><span class="k">SKU</span><span class="v mono">{{ report.machine.systemSku }}</span></div>
      </div>

      <!-- 显卡与显示 -->
      <div class="sys-card">
        <h3 class="sys-card-title">显卡与显示</h3>
        <div v-for="(g, i) in report.gpus" :key="'gpu' + i" class="sys-sub">
          <div class="sys-kv"><span class="k">显卡</span><span class="v">{{ g.desc }}</span></div>
          <div class="sys-kv"><span class="k">驱动</span><span class="v mono">{{ [g.provider, g.driverVersion, g.driverDate].filter(Boolean).join(' · ') || '—' }}</span></div>
        </div>
        <div v-if="!report.gpus?.length" class="hint-dim">显卡信息未采集到（驱动注册表项缺失）。</div>
        <div v-for="(d, i) in report.displays" :key="'disp' + i" class="sys-sub">
          <div class="sys-kv">
            <span class="k">显示器{{ d.primary ? '（主）' : '' }}</span>
            <span class="v">{{ d.width }}×{{ d.height }}{{ d.refreshHz ? ` @ ${d.refreshHz}Hz` : '' }}{{ d.colorBits ? ` · ${d.colorBits}bit` : '' }}</span>
          </div>
        </div>
        <div class="hint-dim">EDID 品牌型号未采集（需 WMI，本模块边界内如实缺席）。</div>
      </div>

      <!-- 磁盘卷 -->
      <div class="sys-card">
        <h3 class="sys-card-title">磁盘卷</h3>
        <div v-for="v in report.volumes" :key="v.letter" class="sys-sub">
          <div class="sys-kv">
            <span class="k">{{ v.letter }} {{ v.label ? `(${v.label})` : '' }}</span>
            <span class="v"><span class="chip chip-neutral">{{ VOL_TYPES[v.type] || v.type }}</span> {{ v.fileSystem }}</span>
          </div>
          <div class="sys-bar-wrap" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="pct(v.totalBytes - v.freeBytes, v.totalBytes)" :aria-label="`${v.letter} 卷用量`">
            <div class="sys-bar-inner" :class="{ warn: pct(v.totalBytes - v.freeBytes, v.totalBytes) > 88 }" :style="{ width: `${pct(v.totalBytes - v.freeBytes, v.totalBytes)}%` }"></div>
          </div>
          <div class="sys-kv"><span class="k">用量</span><span class="v mono">{{ fmtSize(v.totalBytes - v.freeBytes) }} / {{ fmtSize(v.totalBytes) }}（余 {{ fmtSize(v.freeBytes) }}）</span></div>
        </div>
        <div class="hint-dim">物理磁盘型号/序列号未采集（需卷句柄查询，本模块边界内如实缺席）。</div>
      </div>

      <!-- 网络接口 -->
      <div class="sys-card sys-card-wide">
        <h3 class="sys-card-title">网络接口</h3>
        <table class="tbl">
          <thead>
            <tr><th style="width: 200px;">接口</th><th style="width: 150px;">MAC</th><th style="width: 60px;">MTU</th><th style="width: 70px;">状态</th><th>地址</th></tr>
          </thead>
          <tbody>
            <tr v-for="n in report.network" :key="n.name">
              <td>{{ n.name }}<span v-if="n.loopback" class="chip chip-neutral" style="margin-left: 6px;">环回</span></td>
              <td class="mono">{{ n.mac || '—' }}</td>
              <td>{{ n.mtu }}</td>
              <td><span :class="['ver-status', n.up ? 'installed' : 'idle']">{{ n.up ? '启用' : '停用' }}</span></td>
              <td class="mono">{{ sortNetAddresses(n.addresses).join('、') || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 家族原子（page/btn/chip/banner/tbl/mono/hint-dim/status-toolbar）由全局样式层
   承担；本视图私有形仅 sys-grid/sys-card/kv/bar 档案家族（sys- 前缀纪律） */
.sys-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 14px; }
.sys-card-wide { grid-column: 1 / -1; }
.sys-card { background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-card, 10px); padding: 14px 16px; }
.sys-card-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text); margin: 0 0 10px; }
.sys-card-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.sys-card-actions { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
.sys-card-head .sys-card-title { margin-bottom: 10px; }
.sys-action-note { margin: 8px 0 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.5; }
.sys-action-error { margin: 6px 0 0; font-size: var(--text-xs); color: var(--state-danger); }
.sys-kv { display: flex; gap: 10px; align-items: baseline; padding: 2px 0; font-size: var(--text-sm); }
.sys-kv .k { flex: 0 0 92px; color: var(--color-text-subtle); }
.sys-kv .v { color: var(--color-text); word-break: break-all; }
.sys-sub + .sys-sub { border-top: 1px dashed var(--color-border); margin-top: 8px; padding-top: 8px; }
.sys-bar-wrap { height: 6px; background: var(--surface-hover); border-radius: 3px; overflow: hidden; margin: 6px 0 4px; }
.sys-bar-inner { height: 100%; background: var(--color-primary); transition: width var(--motion-base) ease; }
.sys-bar-inner.warn { background: var(--state-warning); }
/* 状态点复用托管家族 ver-status 语义在本页缺失（scoped 隔离），本地最小实现 */
.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; }
.ver-status.installed::before { background: var(--state-positive); }
.ver-status.idle::before { background: var(--color-text-subtle); }
</style>
