<script setup lang="ts">
// WSL「🔌 USB 直通」页签（F9：usbipd-win 集成）。
// 列集对齐 wsl-dashboard 实测（总线号/VID:PID/描述/序列号/状态/客户端 IP），
// 操作全部点点点：共享 bind（提权 UAC）、附加 attach（用户态，未共享时先补 bind）、
// 卸下 detach、取消共享 unbind（在场按 busid、不在场按 GUID）。
// 开机自动共享 = Hanxi 账本 + 重放（不自建计划任务、不碰 usbipd 原生 auto-attach）：
// 设备行「⭐ 登记」写账本，hanxi 启动/发行版启动自动重放，总开关默认关。
// 本面板随页签 v-if 挂载：usePolling 生命周期即开关（离开页签零轮询，
// 对齐 usePolling 的 KeepAlive 契约与"冷页避免无谓探测"的 wsl 模块惯例）。
import { computed, ref } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { USBShareEntry, UsbView } from '../../../bindings/hanxi/internal/modules/wsl/models'
import type { Device } from '../../../bindings/hanxi/internal/modules/wsl/usbipd/models'
import type { DistroInstance } from '../../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { usePolling } from '../../composables/usePolling'
import { getErrorMessage } from '../../utils/errors'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'

const props = defineProps<{
  instances: DistroInstance[]
}>()

const { showToast } = useToast()
const { confirm } = useConfirm()

const view = ref<UsbView | null>(null)
const loading = ref(false)
const error = ref('')
// 面板内串行操作闸：usbipd 命令本身串行，撞车时后来者快速失败即可。
const opBusy = ref(false)
// 行级目标发行版选择（busid → distro；缺省回落到默认 WSL2 实例）。
const rowDistro = ref<Record<string, string>>({})

const defaultDistro = computed(() => {
  const def = props.instances.find(i => i.default && i.version === '2')
    || props.instances.find(i => i.running && i.version === '2')
    || props.instances.find(i => i.version === '2')
  return def?.name ?? ''
})
const distroFor = (d: Device) => rowDistro.value[d.busId] || defaultDistro.value

async function load(silent = false) {
  if (loading.value) return
  loading.value = true
  if (!silent) error.value = ''
  try {
    const v = await WSLAPI.GetUsbOverview()
    if (v) {
      view.value = v
      error.value = v.error || ''
    }
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

// 5s 轮询：插拔设备实时反映（usePolling 自带在飞去重；操作在飞时跳过整拍）。
usePolling(() => {
  if (opBusy.value) return
  return load(true)
}, 5000)

const devices = computed(() => view.value?.devices ?? [])
const ledger = computed(() => view.value?.ledger ?? [])
const installed = computed(() => !!view.value?.installed)

const WINGET_CMD = 'winget install --id dorssel.usbipd-win -e'

// 在场判据：后端 Device.Connected() 不进 JSON，busid 空即"已共享但拔出"。
const isConn = (d: Device) => !!d.busId

function stateInfo(d: Device): { tone: 'positive' | 'information' | 'warning' | 'danger' | 'neutral'; text: string; title: string } {
  if (!isConn(d)) return { tone: 'warning', text: '已共享·不在场', title: '设备当前未插入（bind 记录仍在，插回自动可用）' }
  switch (d.state) {
    case 'attached':
      return { tone: 'positive', text: '已附加', title: d.clientIp ? `客户端 ${d.clientIp}` : '' }
    case 'shared':
      return d.forced
        ? { tone: 'warning', text: '已共享(强制)', title: '--force 绑定：Windows 侧独占直通，拔出前注意' }
        : { tone: 'information', text: '已共享', title: '已 bind，可附加' }
    case 'incompatible':
      return { tone: 'warning', text: '不兼容集线器', title: '该 Hub 不报告端口号，usbipd 拒绝共享' }
    default:
      return { tone: 'neutral', text: '未共享', title: '尚未 bind' }
  }
}

function entryFor(d: Device): USBShareEntry | undefined {
  return ledger.value.find(e => e.busId.toLowerCase() === d.busId.toLowerCase())
}

function registerTitle(d: Device): string {
  const e = entryFor(d)
  return e ? `已登记自动共享 → ${e.distro}（再点可改目标）` : '登记到开机自动共享账本'
}

async function withOp<T>(fn: () => Promise<T>): Promise<T | undefined> {
  if (opBusy.value) return undefined
  opBusy.value = true
  try {
    return await fn()
  } catch (e) {
    showToast(`操作失败: ${getErrorMessage(e)}`, { duration: 8000 })
    return undefined
  } finally {
    opBusy.value = false
    await load(true)
  }
}

async function askUac(title: string, desc: string): Promise<boolean> {
  return confirm({
    title,
    description: `${desc}\n\n该操作需要管理员权限：随后弹出系统 UAC 授权窗口（提权命令行窗口内可见进度）。`,
    tone: 'warning',
  })
}

const bindDevice = (d: Device) => withOp(async () => {
  if (!(await askUac(`共享设备 ${d.busId}（${d.description}）？`, 'bind 把设备交给 usbipd 共享池（持久绑定，重插仍有效）。'))) return
  const out = await WSLAPI.BindUsbDevice(d.busId, false)
  showToast(out?.message || '已提交共享')
})

const unbindDevice = (d: Device) => withOp(async () => {
  if (!(await askUac(`取消共享 ${d.busId}（${d.description}）？`, 'unbind 收回 usbipd 的共享绑定；设备若已附加请先「卸下」。'))) return
  const out = await WSLAPI.UnbindUsbDevice(d.busId)
  showToast(out?.message || '已提交取消共享')
})

const unbindAbsent = (d: Device) => withOp(async () => {
  if (!d.guid) return
  if (!(await askUac('取消共享（设备不在场）？', '该设备已共享但未插入，按绑定 GUID 收回；Windows 将在下次插入时正常接管。'))) return
  const out = await WSLAPI.UnbindAbsentUsbDevice(d.guid)
  showToast(out?.message || '已提交取消共享')
})

const attachDevice = (d: Device) => withOp(async () => {
  const distro = distroFor(d)
  if (!distro) {
    showToast('本机还没有 WSL2 发行版——先到「本机发行版」页装一个')
    return
  }
  // 一键直通（蓝本同款体验）：未共享时在同一次点击里先补 bind 再 attach。
  if (d.state === 'notshared') {
    if (!(await askUac(`直通设备 ${d.busId} 到 ${distro}？`, `设备尚未共享，本次将先执行 bind（管理员）再附加到 ${distro}。`))) return
    const b = await WSLAPI.BindUsbDevice(d.busId, false)
    if (!b?.success) {
      showToast(b?.message || 'bind 未完成，附加已中止')
      return
    }
  }
  const out = await WSLAPI.AttachUsbDevice(d.busId, distro)
  showToast(out?.message || '已提交附加')
})

const detachDevice = (d: Device) => withOp(async () => {
  const out = await WSLAPI.DetachUsbDevice(d.busId)
  showToast(out?.message || '已提交卸下')
})

const registerShare = (d: Device) => withOp(async () => {
  const distro = distroFor(d)
  if (!distro) {
    showToast('先选择目标发行版（下拉里挑一个 WSL2 实例）')
    return
  }
  const out = await WSLAPI.SetUsbShare(d.busId, distro)
  showToast(out?.message || '已登记')
  if (out?.success && !view.value?.autoEnabled) {
    showToast('提示：开机自动共享总开关还没开——打开后立即按账本补挂')
  }
})

// 确认被拒时原生 checkbox 已被用户翻位而绑定值未变（Vue 不重patch同值 :checked），
// 用 key 自增强制重挂载回真实账本态。
const switchKey = ref(0)

async function toggleAuto() {
  const next = !view.value?.autoEnabled
  if (next) {
    const ok = await confirm({
      title: '开启开机自动共享？',
      description: 'hanxi 启动与本机拉起发行版后，会自动按账本补做「bind(如需)+attach」：'
        + 'bind 欠账时弹一次 UAC；未运行发行版的条目跳过、待其启动后自动补挂。'
        + '仅 hanxi 在跑时生效（刻意不自建 Windows 计划任务，无系统残留）。',
      tone: 'warning',
    })
    if (!ok) {
      switchKey.value++
      return
    }
  }
  await withOp(async () => {
    const out = await WSLAPI.SetUsbAutoAttach(next)
    showToast(out?.message || '已更新总开关')
  })
}

const toggleEntry = (e: USBShareEntry) => withOp(async () => {
  const out = await WSLAPI.SetUsbShareEnabled(e.id, !e.enabled)
  showToast(out?.message || '账本已更新')
})

const removeEntry = async (e: USBShareEntry) => {
  if (!(await confirm({
    title: `移除 ${e.busId}（${e.description || '设备'}）的自动共享登记？`,
    description: '只删 Hanxi 账本条目；usbipd 的系统绑定不动——想退绑到设备行点「取消共享」。',
  }))) return
  await withOp(async () => {
    const out = await WSLAPI.RemoveUsbShare(e.id)
    showToast(out?.message || '已移除')
  })
}

const replayNow = () => withOp(async () => {
  const out = await WSLAPI.ReplayUsbNow()
  showToast(out?.message || '重放已执行')
})

async function openReleases() {
  try {
    await WSLAPI.OpenUsbipdReleases()
  } catch (e) {
    showToast(`打开发布页失败: ${getErrorMessage(e)}`)
  }
}

async function copyWinget() {
  try {
    await navigator.clipboard.writeText(WINGET_CMD)
    showToast('安装命令已复制——粘贴到终端执行（装完点「↻ 刷新」重新探测）')
  } catch {
    showToast('复制失败，请手动输入命令')
  }
}

type USBShareIdentity = USBShareEntry & Partial<Pick<Device, 'instanceId' | 'serial' | 'guid'>>

function sameNonEmpty(a?: string, b?: string): boolean {
  return !!a && !!b && a.toLowerCase() === b.toLowerCase()
}

function stableIdentityMatches(e: USBShareIdentity, d: Device): boolean {
  if (sameNonEmpty(e.serial, d.serial)) return true
  if (sameNonEmpty(e.instanceId, d.instanceId)) return true
  return sameNonEmpty(e.guid, d.guid)
}

// 账本行 × 设备表现态 → 呈现徽标。优先后端现态：同 busid 仅在双方非空时
// EqualFold 命中；换口再按稳定身份，最后才兼容旧账本的唯一 VID:PID。
function entryLive(entry: USBShareEntry): { tone: 'positive' | 'information' | 'warning' | 'danger' | 'neutral'; text: string } {
  const d = entry as USBShareIdentity
  const sameBus = devices.value.find(x => !!d.busId && !!x.busId && x.busId.toLowerCase() === d.busId.toLowerCase())
  let dev = sameBus
  if (!dev) {
    const stable = devices.value.filter(x => !!x.busId && stableIdentityMatches(d, x))
    if (stable.length === 1) dev = stable[0]
  }
  if (!dev && !d.instanceId && !d.serial && !d.guid && d.vid && d.pid) {
    const legacy = devices.value.filter(x => !!x.busId
      && !!x.vid && x.vid.toLowerCase() === d.vid!.toLowerCase()
      && !!x.pid && x.pid.toLowerCase() === d.pid!.toLowerCase())
    if (legacy.length === 1) dev = legacy[0]
  }
  if (!dev) return { tone: 'warning', text: '不在场' }
  if (dev.state === 'attached') return { tone: 'positive', text: '已附加' }
  if (dev.state === 'shared') return { tone: 'information', text: '待附加' }
  return { tone: 'neutral', text: '需先共享' }
}
</script>

<template>
  <!-- usbipd-win 未装：引导卡（打开官方发布页 + winget 命令复制；不代装） -->
  <div v-if="view && !installed" class="guide-card">
    <h3 class="guide-title">需要 usbipd-win：本机 USB 直通到 WSL2 的桥梁</h3>
    <p class="guide-text">
      「USB 直通」把 Windows 本机 USB 设备（串口、加密狗、存储类等）共享并附加给 WSL2 发行版，
      底层由开源工具 <b>usbipd-win</b> 承担。安装一次即可长期使用；绑定（bind）需管理员，
      附加/卸下无需。
    </p>
    <ol class="guide-steps">
      <li>官方发布页下载 MSI 安装（或复制下方 winget 命令）：
        <div class="cmd-row">
          <code class="mono cmd">{{ WINGET_CMD }}</code>
          <button class="btn btn-secondary btn-small" @click="copyWinget">📋 复制命令</button>
        </div>
      </li>
      <li>安装后新开终端执行一次 <code class="mono">usbipd</code> 确认在 PATH 中；</li>
      <li>回到这里点「↻ 重新探测」。装的是别的机器？本表只管理本机直通。</li>
    </ol>
    <div class="btn-group">
      <button class="btn btn-primary btn-small" @click="openReleases">🌐 打开官方发布页</button>
      <button class="btn btn-secondary btn-small" :disabled="loading" @click="load()">{{ loading ? '探测中…' : '↻ 重新探测' }}</button>
    </div>
  </div>

  <UiBanner v-if="view?.replayBusy" tone="info" class="slim">正在按账本重放 USB 共享（若需补绑定会弹一次 UAC）…</UiBanner>

  <div v-if="error" class="error-box">{{ error }}
    <button class="btn btn-secondary btn-small retry-inline" :disabled="loading" @click="load()">↻ 重试</button>
  </div>
  <div v-if="!view && loading" class="hint-line">正在探测 usbipd-win 与设备表…</div>

  <template v-if="view && installed">
    <div class="control-panel">
      <div class="meta-info">
        <span>usbipd <b class="mono">v{{ view.version || '?' }}</b> · 列：总线号 / VID:PID / 设备 / 序列号 / 状态 / 客户端；
          共享(bind)需管理员，附加(attach)/卸下(detach)用户态即行。</span>
        <span class="hint-dim">只登记与管理 Hanxi 账本内的自动共享；usbipd 自身与他机远程 usbip 不触碰</span>
      </div>
      <label class="auto-switch" :key="switchKey" title="开机自动共享总开关：hanxi 启动/拉起发行版时按账本补挂（默认关）">
        <input type="checkbox" :checked="view.autoEnabled" :disabled="opBusy" @change="toggleAuto" />
        开机自动共享<span v-if="!view.autoEnabled" class="hint-dim">（关）</span>
      </label>
      <div class="btn-group">
        <button class="btn btn-secondary btn-small" :disabled="loading || opBusy" @click="load()">{{ loading ? '读取中…' : '↻ 刷新' }}</button>
        <button class="btn btn-secondary btn-small" :disabled="opBusy || !ledger.length"
          title="立即按账本重放 bind(欠账补)+attach（与开机自动共享同一通道）" @click="replayNow">⟳ 重放共享</button>
      </div>
    </div>

    <!-- 设备表 -->
    <div class="section-title"><h3>USB 设备 ({{ devices.length }})</h3></div>
    <div v-if="!devices.length && !loading" class="empty-state">
      <p>本机此刻没有可列出的 USB 设备——插上目标设备后点「↻ 刷新」（页开着时也会自动复采）。</p>
    </div>
    <div v-else-if="loading && !view" class="hint-line">正在读取 usbipd 设备表…</div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 64px;">总线号</th>
            <th style="width: 104px;">VID:PID</th>
            <th>设备</th>
            <th style="width: 130px;">序列号</th>
            <th style="width: 128px;">状态</th>
            <th style="width: 120px;">目标发行版</th>
            <th style="width: 260px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in devices" :key="d.busId || d.guid || d.instanceId" :class="{ 'row-dim': !isConn(d) }">
            <td class="mono">{{ d.busId || '—' }}</td>
            <td class="mono">{{ d.vid && d.pid ? `${d.vid}:${d.pid}` : '—' }}</td>
            <td :title="d.instanceId">{{ d.description || '（未知设备）' }}</td>
            <td class="mono serial">{{ d.serial || '—' }}</td>
            <td>
              <UiStatusChip :tone="stateInfo(d).tone" :title="stateInfo(d).title">{{ stateInfo(d).text }}</UiStatusChip>
              <span v-if="d.clientIp" class="mono client-ip">{{ d.clientIp }}</span>
            </td>
            <td>
              <select v-if="isConn(d)" class="input distro-select" :value="distroFor(d)"
                :aria-label="`设备 ${d.busId} 的目标发行版`" @change="rowDistro[d.busId] = ($event.target as HTMLSelectElement).value">
                <option v-for="i in instances" :key="i.name" :value="i.name" :disabled="i.version !== '2'">
                  {{ i.name }}{{ i.version !== '2' ? '（WSL1 不可用）' : i.running ? '' : '（未运行）' }}
                </option>
              </select>
              <span v-else class="hint-dim">不在场</span>
            </td>
            <td>
              <div class="distro-actions">
                <template v-if="isConn(d)">
                  <button v-if="d.state === 'attached'" class="btn btn-secondary btn-small" :disabled="opBusy"
                    title="从 WSL 卸下，设备回到 Windows" @click="detachDevice(d)">⏏ 卸下</button>
                  <button v-else-if="d.state === 'shared' || d.state === 'notshared'" class="btn btn-primary btn-small"
                    :disabled="opBusy" :title="d.state === 'notshared' ? '未共享：本次点击先 bind 再附加（弹 UAC）' : '附加到右侧发行版（用户态，不弹 UAC）'"
                    @click="attachDevice(d)">▶ 附加</button>
                  <button v-else class="btn btn-secondary btn-small" disabled>不可共享</button>
                  <button v-if="d.state === 'shared' || d.state === 'notshared'" class="btn btn-secondary btn-small"
                    :disabled="opBusy" :title="registerTitle(d)"
                    @click="registerShare(d)">⭐ 自动共享</button>
                  <button v-if="d.state === 'shared'" class="btn btn-secondary btn-small" :disabled="opBusy"
                    title="收回共享绑定（需管理员）；已附加请先卸下" @click="unbindDevice(d)">✂ 取消共享</button>
                </template>
                <template v-else>
                  <button class="btn btn-danger-outline btn-small" :disabled="opBusy"
                    title="设备不在场也能退绑：按绑定 GUID 收回（需管理员）" @click="unbindAbsent(d)">✂ 退绑(不在场)</button>
                </template>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 自动共享账本 -->
    <div class="section-title">
      <h3>自动共享账本 ({{ ledger.length }})</h3>
      <span v-if="view.lastReplay" class="hint-dim">最近重放：{{ view.lastReplay }}</span>
    </div>
    <div v-if="!ledger.length" class="empty-state">
      <p>账本为空——在上方设备行点「⭐ 自动共享」登记第一条（开机/插回后自动附加到指定发行版）。</p>
    </div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 64px;">总线号</th>
            <th>设备</th>
            <th style="width: 150px;">目标发行版</th>
            <th style="width: 110px;">现态</th>
            <th>上次结果</th>
            <th style="width: 150px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in ledger" :key="e.id" :class="{ 'row-dim': !e.enabled }">
            <td class="mono">{{ e.busId }}</td>
            <td>{{ e.description || '—' }}<span v-if="e.vid && e.pid" class="mono vidpid">（{{ e.vid }}:{{ e.pid }}）</span></td>
            <td><code class="mono">{{ e.distro }}</code></td>
            <td>
              <UiStatusChip v-if="!e.enabled" tone="neutral">已停用</UiStatusChip>
              <UiStatusChip v-else :tone="entryLive(e).tone">{{ entryLive(e).text }}</UiStatusChip>
            </td>
            <td class="dim wrap">
              <template v-if="e.lastStatus">{{ e.lastStatus }}<span class="hint-dim"> · {{ e.lastAt }}</span></template>
              <template v-else>未重放过</template>
            </td>
            <td>
              <div class="distro-actions">
                <button class="btn btn-secondary btn-small" :disabled="opBusy" @click="toggleEntry(e)">
                  {{ e.enabled ? '⏸ 停用' : '▶ 启用' }}
                </button>
                <button class="btn btn-danger-outline btn-small" :disabled="opBusy" @click="removeEntry(e)">🗑 移除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="hint-line">
      换插口场景：账本优先按总线号匹配，认不出时按登记快照的 VID:PID 唯一回落（状态列会点名"换插口"）。
      同 VID:PID 多义时不猜、跳过并记因。
    </p>
  </template>
</template>

<style scoped>
/* 全局原子落 components.css（.tbl/.control-panel/.empty-state/.error-box/.btn 等）；
   此处只留本 tab 独有或有意补差（对齐端口转发页的注释纪律）。 */
.error-box { white-space: pre-line; }
.hint-line { line-height: 1.7; white-space: pre-line; font-size: var(--text-sm); color: var(--color-text-muted); }
.hint-dim { font-size: var(--text-sm); color: var(--color-text-muted); }
.retry-inline { margin-left: 10px; }
.control-panel { gap: 10px; flex-wrap: wrap; }
.meta-info { min-width: 0; }
.btn-group { flex-wrap: wrap; }
.distro-actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
.section-title { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; }
.row-dim .mono, .row-dim td { opacity: 0.65; }
.serial, .client-ip { color: var(--color-text-muted); font-size: var(--text-sm); }
.client-ip { margin-left: 6px; }
.vidpid { color: var(--color-text-muted); }
.wrap { word-break: break-word; }
.distro-select { flex: 0 0 auto; width: 150px; }
.auto-switch { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); cursor: pointer; white-space: nowrap; }
.auto-switch input { accent-color: var(--color-primary); margin: 0; }

/* 引导卡（未安装态）：面板级底色 + 步骤条，与 .control-panel 同族表面语言 */
.guide-card {
  display: flex; flex-direction: column; gap: 10px; padding: 14px 16px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
.guide-title { margin: 0; font-size: var(--text-base); font-weight: 700; }
.guide-text { margin: 0; font-size: var(--text-sm); line-height: 1.7; }
.guide-steps { margin: 0; padding-left: 20px; font-size: var(--text-sm); line-height: 1.8; }
.cmd-row { display: flex; align-items: center; gap: 8px; margin-top: 4px; flex-wrap: wrap; }
.cmd {
  padding: 3px 8px; background: var(--surface-page); border: 1px solid var(--color-border);
  border-radius: 6px; user-select: all;
}
.input {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 4px 8px; font-size: var(--text-sm); color: var(--color-text); font-family: inherit;
}
.input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }
</style>
