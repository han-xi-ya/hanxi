<script setup lang="ts">
// 系统信息（N10，借鉴 MooTool 机制）：本机软硬件静态档案一屏。
// 数据源为后端 sysinfo 模块单次只读采集（注册表 + WinAPI + 标准库）；
// 内存/磁盘占用是快照值，页头明示采集时刻；段级采集告警如实呈现（banner）。
import { onMounted, ref } from 'vue'
import * as SysInfoAPI from '../../bindings/hanxi/internal/modules/sysinfo/sysinfoservice'
import type { Report } from '../../bindings/hanxi/internal/modules/sysinfo/models'
import PageHeader from '../components/ui/PageHeader.vue'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'
import { sortNetAddresses } from '../utils/netaddrs'

const report = ref<Report | null>(null)
const loading = ref(false)
const loadError = ref('')
const collectedAt = ref('')

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
        <h3 class="sys-card-title">内存</h3>
        <div class="sys-kv"><span class="k">物理总量</span><span class="v">{{ fmtSize(report.memory.totalBytes) }}</span></div>
        <div class="sys-kv"><span class="k">可用</span><span class="v">{{ fmtSize(report.memory.availableBytes) }}</span></div>
        <div class="sys-bar-wrap" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="report.memory.loadPercent" aria-label="内存占用">
          <div class="sys-bar-inner" :class="{ warn: report.memory.loadPercent > 80 }" :style="{ width: `${report.memory.loadPercent}%` }"></div>
        </div>
        <div class="sys-kv"><span class="k">占用</span><span class="v">{{ report.memory.loadPercent }}%</span></div>
        <div class="sys-kv"><span class="k">提交</span><span class="v mono">{{ fmtSize(report.memory.commitTotal) }} / {{ fmtSize(report.memory.commitLimit) }}</span></div>
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
