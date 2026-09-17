<script setup lang="ts">
// WSL 端口转发页签（「🔀 端口映射」：NAT netsh portproxy 规则清单账本，Phase 6 式自 WSLView 拆分）。
// 首刷懒加载由视图页签 watch 调 ensureLoaded()；「🌑 停止全部」后的现态复采由视图经
// loadProxy 句柄回拨（doShutdown 收尾序列与拆分前一致）；应用/清理走视图 runOp 白名单编排
//（提权单批脚本 + 操作后流式复查）。行为逐字迁出，调用序列未动。
import { reactive, ref } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { DistroInstance, PortProxyView, PortRule, PortRuleView } from '../../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { getErrorMessage } from '../../utils/errors'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'

const props = defineProps<{
  instances: DistroInstance[]
  busyAny: boolean
  globalBusy: boolean
  runOp: (opts: {
    name: string
    title: string
    desc: string
    tone?: 'default' | 'warning' | 'danger'
    confirmLabel?: string
    invoke: () => PromiseLike<unknown>
  }) => Promise<void>
}>()

const { showToast } = useToast()
const { confirm } = useConfirm()

const proxyView = ref<PortProxyView | null>(null)
const proxyLoading = ref(false)
const proxyError = ref('')
// 默认只开本机回环；0.0.0.0 属"向局域网曝光"的显式选择（UI 有警示条）。
const newRule = reactive({ distro: '', port: '', guest: '', listen: '127.0.0.1', firewall: false, note: '' })

async function resetProxyLedger() {
  const accepted = await confirm({
    title: '清空端口转发规则清单文件？',
    tone: 'warning',
    description: '仅删除 Hanxi 的规则清单（清单文件损坏或想彻底重来时的逃生口）——不摘除系统里已生效的转发，'
      + '它们会转列为「外部转发」由你自行处置。日常删除规则请用「🧹 清理托管」。',
  })
  if (!accepted) return
  try {
    const out = await WSLAPI.ClearPortLedgerFile()
    showToast(out?.message || '规则清单已清空')
    await loadProxy()
  } catch (e) {
    showToast(`清空失败: ${getErrorMessage(e)}`)
  }
}

async function loadProxy() {
  proxyLoading.value = true
  proxyError.value = ''
  try {
    proxyView.value = await WSLAPI.ListPortRules()
  } catch (e) {
    proxyError.value = `读取端口转发失败: ${getErrorMessage(e)}\n需要本机 netsh（PowerShell 通道）可用；若安全软件拦截了 netsh，请如实处理后再刷新。`
  } finally {
    proxyLoading.value = false
  }
}

async function addProxyRule() {
  if (props.globalBusy) return
  const port = Number(newRule.port.trim())
  const guest = Number(newRule.guest.trim() || '0')
  if (!newRule.distro || !Number.isInteger(port)) {
    showToast('请填写发行版与合法的本机监听端口（1-65535）')
    return
  }
  try {
    const r = await WSLAPI.AddPortRule(newRule.distro, port, guest, newRule.listen.trim() || '127.0.0.1', newRule.firewall, newRule.note.trim())
    showToast(`已加入规则清单 ${r?.listen}:${r?.port} → ${r?.distro}（点「▶ 应用规则」才挂进系统）`)
    newRule.port = ''
    newRule.guest = ''
    newRule.note = ''
    await loadProxy()
  } catch (e) {
    showToast(`添加规则失败: ${getErrorMessage(e)}`)
  }
}

const rulePayload = (r: PortRuleView, patch: Partial<PortRule> = {}): PortRule => ({
  id: r.id, distro: r.distro, port: r.port, guest: r.guest, listen: r.listen,
  firewall: r.firewall, note: r.note ?? '', enabled: r.enabled, ...patch,
})

async function updateRule(r: PortRuleView, patch: Partial<PortRule>) {
  try {
    await WSLAPI.UpdatePortRule(rulePayload(r, patch))
  } catch (e) {
    showToast(`更新规则失败: ${getErrorMessage(e)}`)
  }
  await loadProxy()
}

async function removeRule(r: PortRuleView) {
  const accepted = await confirm({
    title: `删除规则 ${r.listen}:${r.port} → ${r.distro}？`,
    tone: 'warning',
    description: '只从规则清单删除；若系统里还有对应转发，随后点「▶ 应用规则」会一并摘除（提权执行）。',
  })
  if (!accepted) return
  try {
    const out = await WSLAPI.RemovePortRule(r.id)
    showToast(out?.message || '已删除')
  } catch (e) {
    showToast(`删除失败: ${getErrorMessage(e)}`)
  }
  await loadProxy()
}

const applyProxy = () => props.runOp({
  name: 'proxy-apply', title: '应用端口转发规则到系统？',
  desc: '单批 netsh portproxy 与防火墙放行命令在同一个提权脚本内执行（先删后加幂等，逐条传播退出码）；'
    + '未运行的发行版会被跳过并在回执点名，不会被顺手拉起。外部程序登记的转发一律不触碰。',
  invoke: async () => {
    const out = await WSLAPI.ApplyPortRules()
    await loadProxy()
    return out
  },
})

const cleanupProxy = () => props.runOp({
  name: 'proxy-cleanup', title: '清理 Hanxi 登记的全部端口转发？', tone: 'danger', confirmLabel: '摘除并清空',
  desc: '从系统摘除规则清单登记的转发与 "Hanxi WSL *" 防火墙放行（UAC 提权），并清空清单文件。'
    + '\n外部程序的转发不受影响；如你还想恢复，需重新添加规则。',
  invoke: async () => {
    const out = await WSLAPI.CleanupPortRules()
    await loadProxy()
    return out
  },
})

function ruleDrift(r: PortRuleView): boolean {
  return r.applied && !!r.targetIP && !!r.activeIP && r.activeIP !== r.targetIP
}

defineExpose({
  // 视图接线：首次切到本页才拉数据（netsh 现态依赖本机命令，冷页避免无谓探测）；
  // loadProxy 供「🌑 停止全部」后的现态复采回拨。
  loadProxy,
  ensureLoaded() {
    if (!proxyView.value && !proxyLoading.value) void loadProxy()
  },
})
</script>

<template>
  <UiBanner v-if="proxyView?.networkMode === 'mirrored'" tone="info" class="slim">
    本机为<b>镜像网络</b>：Windows 的 localhost 直通发行版服务，通常<b>无需</b>这里的转发规则——
    下方只读列出的 portproxy 属 NAT 时代遗产或其它程序建立，自行决定去留。要切回 NAT 可在「🌐 .wslconfig」中修改。
  </UiBanner>
  <UiBanner v-if="proxyView?.pending" tone="warn" class="slim">
    规则清单有改动尚未应用到系统——点「▶ 应用规则」同步（一次 UAC 批量执行）。
  </UiBanner>
  <div class="control-panel">
    <div class="meta-info">
      <span>WSL2 NAT 转发：本机 <b class="mono">监听:端口</b> → <b class="mono">发行版IP:guest端口</b>；WSL 重启后 guest IP 漂移，再点一次应用即重同步。</span>
      <span class="hint-dim">只管理本工具规则清单登记的监听口；系统里其它来源的转发列在「外部转发」，只展示不触碰</span>
    </div>
    <div class="btn-group">
      <button class="btn btn-primary btn-small" :disabled="busyAny || !proxyView?.rules?.length"
        title="netsh portproxy/advfirewall 批量应用（UAC 提权，先删后加幂等）" @click="applyProxy">▶ 应用规则</button>
      <button class="btn btn-secondary btn-small" :disabled="proxyLoading" @click="loadProxy">{{ proxyLoading ? '读取中…' : '↻ 刷新' }}</button>
      <button class="btn btn-danger-outline btn-small" :disabled="busyAny || !proxyView?.rules?.length"
        title="摘除规则清单全部规则的系统转发与 Hanxi WSL 防火墙放行并清空清单" @click="cleanupProxy">🧹 清理托管</button>
    </div>
  </div>

  <div v-if="proxyError" class="error-box">{{ proxyError }}
    <button class="btn btn-secondary btn-small retry-inline" @click="loadProxy">↻ 重试</button>
    <button class="btn btn-secondary btn-small" title="规则清单文件损坏/误拦时的逃生口：只删文件，不碰系统现态" @click="resetProxyLedger">清空规则清单</button>
  </div>
  <div v-else-if="proxyLoading && !proxyView" class="hint-line">正在读取规则清单与 netsh 现态…</div>
  <template v-else-if="proxyView">
    <div class="import-panel">
      <div class="move-input-row">
        <label class="move-label" for="wsl-pp-distro">发行版</label>
        <select id="wsl-pp-distro" v-model="newRule.distro" class="input" :disabled="globalBusy">
          <option value="">（选择）</option>
          <option v-for="i in instances" :key="i.name" :value="i.name">{{ i.name }}</option>
        </select>
        <label class="move-label" for="wsl-pp-port">本机端口</label>
        <input id="wsl-pp-port" v-model="newRule.port" class="input mono pp-num" placeholder="8080" :disabled="globalBusy" />
        <label class="move-label" for="wsl-pp-guest">guest 端口</label>
        <input id="wsl-pp-guest" v-model="newRule.guest" class="input mono pp-num" placeholder="默认同左" :disabled="globalBusy" />
        <label class="move-label" for="wsl-pp-listen">监听地址</label>
        <input id="wsl-pp-listen" v-model="newRule.listen" class="input mono" style="max-width: 130px" :disabled="globalBusy" />
        <label class="radio-label"><input v-model="newRule.firewall" type="checkbox" :disabled="globalBusy" /> 防火墙放行</label>
        <button class="btn btn-primary btn-small" :disabled="!newRule.distro || !newRule.port.trim() || globalBusy"
          @click="addProxyRule">＋ 添加</button>
      </div>
      <div class="move-input-row">
        <label class="move-label" for="wsl-pp-note">备注</label>
        <input id="wsl-pp-note" v-model="newRule.note" class="input" placeholder="可选：这条转发给谁用" :disabled="globalBusy" />
      </div>
      <div v-if="newRule.listen.trim() === '0.0.0.0'" class="hint-line">
        ⚠ 监听 0.0.0.0 + 防火墙放行 = 局域网内<b>其它设备也能访问</b>该端口；只想本机访问请用 127.0.0.1。
      </div>
    </div>

    <div class="section-title"><h3>规则清单 ({{ proxyView.rules?.length ?? 0 }})</h3></div>
    <div v-if="!proxyView.rules?.length" class="empty-state"><p>还没有规则——用上面的表单添加第一条（如 8080 → Ubuntu:80）。</p></div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 170px;">状态</th>
            <th style="width: 150px;">发行版</th>
            <th style="width: 130px;">本机监听</th>
            <th>目标</th>
            <th style="width: 110px;">防火墙</th>
            <th>备注</th>
            <th style="width: 170px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in proxyView.rules" :key="r.id">
            <td>
              <UiStatusChip v-if="!r.enabled" tone="neutral">已停用</UiStatusChip>
              <UiStatusChip v-else-if="!r.distroRunning" tone="warning">发行版未运行</UiStatusChip>
              <UiStatusChip v-else-if="ruleDrift(r)" tone="warning" :title="`netsh 现指 ${r.activeIP}，现为 ${r.targetIP}`">IP 漂移·需重应用</UiStatusChip>
              <UiStatusChip v-else-if="r.applied" tone="positive">✓ 已生效</UiStatusChip>
              <UiStatusChip v-else tone="information">待应用</UiStatusChip>
            </td>
            <td><code class="mono">{{ r.distro }}</code></td>
            <td class="mono">{{ r.listen }}:{{ r.port }}</td>
            <td class="mono">{{ r.targetIP || r.activeIP || '—' }}:{{ r.guest }}</td>
            <td>{{ r.firewall ? '✅ 防火墙放行' : '—' }}</td>
            <td class="dim">{{ r.note || '—' }}</td>
            <td>
              <div class="distro-actions">
                <button class="btn btn-secondary btn-small" :disabled="globalBusy"
                  :title="r.enabled ? '停用后点「应用规则」将从系统摘除该转发' : '恢复启用'"
                  @click="updateRule(r, { enabled: !r.enabled })">{{ r.enabled ? '⏸ 停用' : '▶ 启用' }}</button>
                <button class="btn btn-secondary btn-small" :disabled="globalBusy" title="切换是否同步防火墙入站放行"
                  @click="updateRule(r, { firewall: !r.firewall })">{{ r.firewall ? '关放行' : '开放行' }}</button>
                <button class="btn btn-danger-outline btn-small" :disabled="globalBusy" @click="removeRule(r)">🗑 删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <template v-if="proxyView.foreign?.length">
      <div class="section-title"><h3>外部转发 ({{ proxyView.foreign.length }})</h3></div>
      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 130px;">本机监听</th>
              <th>指向</th>
              <th style="width: 320px;">说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(f, i) in proxyView.foreign" :key="i">
              <td class="mono">{{ f.listenAddr }}:{{ f.listenPort }}</td>
              <td class="mono">{{ f.connectAddr }}:{{ f.connectPort }}</td>
              <td class="dim">非 Hanxi 登记——由其它程序建立，本工具只展示不改动</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </template>
</template>

<style scoped>
/* 全局原子落 components.css；以下为本页签独有或有意补差（注释注明）。 */

/* 多行诊断文案（换行符保留）——对全局 .error-box 的补差白名单行，非同名副本 */
.error-box { white-space: pre-line; }
/* 基形（字号/颜色/左内距）落回全局 .hint-line；此处仅留 WSL 档补差两行 */
.hint-line { line-height: 1.7; white-space: pre-line; }
/* 全局 .hint-dim 只定义颜色，WSL 注记统一配 --text-sm 小字——补差，非副本 */
.hint-dim { font-size: var(--text-sm); }
/* .banner.slim 等值副本已删净：UiBanner 根元素挂 .banner + .slim，落回全局 :where(.banner.slim) */
/* .dim 非全局原子名，本控制台独有 */
.dim { color: var(--color-text-muted); font-size: var(--text-sm); }
.retry-inline { margin-left: 10px; }

/* 控制条：底色/边框/内距/圆角/弹性布局落回全局 .control-panel；
   此处仅留 WSL 档补差（gap 与换行，全局标准形无——定档候选，见收编报告） */
.control-panel { gap: 10px; flex-wrap: wrap; }
/* 字号/颜色/列布局落回全局 .meta-info；此处仅留收缩补差 */
.meta-info { min-width: 0; }
/* display/gap 落回全局 .btn-group；此处仅留换行补差 */
.btn-group { flex-wrap: wrap; }

/* 规则表单面板 */
.import-panel {
  display: flex; flex-direction: column; gap: 8px; padding: 10px 12px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
.move-input-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.move-label { font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted); white-space: nowrap; }
.input {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 6px 10px; font-size: var(--text-sm); color: var(--color-text); font-family: inherit; flex: 1 1 240px; min-width: 0;
}
.input:focus { border-color: var(--color-primary); }
/* 焦点环不再被 outline:none 掐灭：键盘聚焦（:focus-visible）恢复清晰焦点环 */
.input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }
.input:disabled { opacity: 0.6; }
.radio-label { display: inline-flex; align-items: center; gap: 5px; font-size: var(--text-sm); color: var(--color-text); cursor: pointer; }
.radio-label input[type='radio'] { accent-color: var(--color-primary); margin: 0; }
/* 端口输入定宽不参与 flex 伸展 */
.pp-num { width: 96px; flex: 0 0 auto; }
.distro-actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
</style>
