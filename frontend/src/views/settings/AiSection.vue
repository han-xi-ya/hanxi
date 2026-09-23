<script setup lang="ts">
// 设置分区·AI 接入（F4b MCP 安装向导 + R6 授权开关，PLAN_MCP §2.5/§6/§8 拍板）：
// 2026-09-18 文案重设计（用户反馈"太专业差点没搞明白"）：页面收束为两步心智——
// ① 接入（把 hanxi 登记进 AI 软件配置）② 开放哪些内容（工具授权开关），
// 术语（MCP/stdio/工具名/路径）全部退入「技术细节」折叠区；三态红线流与写链不变：
// 预览（before/after 差异；JSONC/冲突给手动片段）→ 确认写入（备份→原子写→复验→失败回滚）。
// access.json 自 R6 起是本分区的写入口：开关拨动即整档原子回写、保存即生效（读者每次重读盘）；
// 损坏/超纲档拒绝盲写，只经「修复（覆盖重置）」二次确认链（旧档另存 .bak 再重写全关）。卸载走对称逆向链。
import { ref, computed, onMounted } from 'vue'
import * as McpWizardAPI from '../../../bindings/hanxi/internal/mcpwizard'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { WizardStatus, AccessInfo, ClientState, WizardPreview, OpResult, SelfCheckInfo } from '../../../bindings/hanxi/internal/mcpwizard/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const { showToast } = useToast()
const { confirm } = useConfirm()

const status = ref<WizardStatus | null>(null)
const loading = ref(false)

// 向导弹窗态：预览 → 结果两段（result 非空即进入结果态，不再可确认）。
// check/checkBusy 为安装前自检（R2，PLAN §2.5）的本地态：Go 侧短 TTL 缓存，
// 同一次向导会话只真 spawn 一次；失败只警示不阻断（惰性条目 + access 门兜底）。
const modal = ref<{
  client: ClientState
  mode: 'install' | 'uninstall'
  preview: WizardPreview | null
  result: OpResult | null
  busy: boolean
  check: SelfCheckInfo | null
  checkBusy: boolean
} | null>(null)

// 标签主语恒为「hanxi 接入条目」而非客户端软件本身——"该软件装没装"归 envcheck 页管。
// 2026-09-18 口语化：blocked 有两种成因（客户端无踪迹 / 文件不敢自动改），
// 标签取中性"暂不可自动接入"，具体原因由后端 detail 行原样直出。
const stateMeta: Record<string, { label: string; chip: string }> = {
  'not-installed': { label: '尚未接入', chip: 'chip-neutral' },
  installed: { label: '已接入', chip: 'chip-positive' },
  'needs-repair': { label: '需重新接入', chip: 'chip-warning' },
  conflict: { label: '有出入 · 先别动', chip: 'chip-danger' },
  blocked: { label: '暂不可自动接入', chip: 'chip-warning' },
}

const accessMeta = computed(() => {
  const a = status.value?.access
  if (!a) return { label: '读取中…', chip: 'chip-neutral' }
  if (!a.exists) return { label: '尚未生成 · 默认全关', chip: 'chip-neutral' }
  if (!a.readable) return { label: '授权档损坏 · AI 什么都拿不到', chip: 'chip-danger' }
  return { label: '状态正常', chip: 'chip-positive' }
})

// 读者不采信档（存在但 readable=false）= 危险态：开关锁死，出路只剩修复链。
const accessCorrupt = computed(() => {
  const a = status.value?.access
  return !!a && a.exists && !a.readable
})

// 六行工具开关（键名=access.json 契约六键，N32/N34 扩充批）：主文案说人话——
// AI 将看到什么、敏感级直书；MCP 工具名退入行内「技术细节」。
const accessTools = computed(() => {
  const t = status.value?.access.tools
  return [
    {
      key: 'envcheck', name: '环境体检', tool: 'hanxi_envcheck_detect', on: !!t?.envcheck,
      desc: 'AI 可查看你装了哪些开发工具、各自什么版本。',
      risk: { text: '低敏感', chip: 'chip-neutral' },
    },
    {
      key: 'everything', name: '全盘文件搜索', tool: 'hanxi_file_search', on: !!t?.everything,
      desc: 'AI 可搜索本机文件；命中的文件名和所在路径会进入 AI 对话。',
      risk: { text: '会暴露文件位置', chip: 'chip-warning' },
    },
    {
      key: 'ocr', name: '图片文字识别', tool: 'hanxi_ocr_recognize', on: !!t?.ocr,
      desc: '你交给 AI 的图片在本机识别成文字——图片不上网，返回的是文字。',
      risk: { text: '只回文字', chip: 'chip-neutral' },
    },
    {
      key: 'memo', name: '便签内容检索', tool: 'hanxi_memo_search', on: !!t?.memo,
      desc: 'AI 可搜索你便签里的文字内容——最私密的一项，建议保持关闭。',
      risk: { text: '含个人笔记 · 建议关', chip: 'chip-danger' },
    },
    {
      key: 'sysinfo', name: '系统信息', tool: 'hanxi_sysinfo_report', on: !!t?.sysinfo,
      desc: 'AI 可查看本机软硬件档案：机型、CPU、内存、显卡、磁盘、系统版本（默认摘要档）。',
      risk: { text: '含计算机名/网络地址', chip: 'chip-warning' },
    },
    {
      key: 'logs', name: '运行日志', tool: 'hanxi_log_read', on: !!t?.logs,
      desc: 'AI 可回看 hanxi 自身运行日志帮你排查问题——每行先自动打码（IP/邮箱/密钥类）再给 AI。',
      risk: { text: '已逐行脱敏', chip: 'chip-warning' },
    },
  ]
})

const accessBusy = ref(false)

function applyAccess(info: AccessInfo) {
  if (status.value) status.value = { ...status.value, access: info }
}

/** setTool 拨开关即写盘：成功以回传呈现就地更新；被拒（损坏档盲写等）toast 中文指引并重读盘。 */
async function setTool(t: { key: string; name: string }, enabled: boolean) {
  if (accessBusy.value || accessCorrupt.value) return
  accessBusy.value = true
  try {
    applyAccess(await McpWizardAPI.McpWizardService.SetToolAccess(t.key, enabled))
    showToast(`${t.name}已${enabled ? '开放' : '收回'}——立即生效，不用重启任何软件`)
  } catch (e: unknown) {
    showToast(`授权改动未生效: ${getErrorMessage(e)}`)
    await refreshAccess()
  } finally {
    accessBusy.value = false
  }
}

/** repairAccess 损坏档唯一出路：二次确认 → ResetAccess（旧档 .bak 后覆盖全关）→ 刷新总览。 */
async function repairAccess() {
  if (accessBusy.value) return
  const accepted = await confirm({
    title: '修复授权设置？',
    description: '授权文件目前格式有问题，AI 侧已按「全部禁止」处理。修复会先备份旧文件，再重置为全部关闭；之后你可以逐个重新打开。',
    confirmLabel: '备份并重置',
    tone: 'danger',
    details: status.value?.access.path ? [{ label: '文件', value: status.value.access.path }] : [],
  })
  if (!accepted) return
  accessBusy.value = true
  try {
    const res = await McpWizardAPI.McpWizardService.ResetAccess()
    showToast(res.message)
    if (!res.success) await refresh() // 备份/写入失败细节在 message，整卡重探一次口径最全
    else applyAccess(await McpWizardAPI.McpWizardService.GetAccessOverview())
  } catch (e: unknown) {
    showToast(`修复失败: ${getErrorMessage(e)}`)
  } finally {
    accessBusy.value = false
  }
}

async function refreshAccess() {
  try {
    applyAccess(await McpWizardAPI.McpWizardService.GetAccessOverview())
  } catch {
    /* GetStatus 重探兜底，静默即可 */
  }
}

const serverCmd = computed(() => {
  const s = status.value?.server
  if (!s?.command) return '（无法解析 hanxi 可执行文件路径）'
  return [`"${s.command}"`, ...(s.args ?? [])].join(' ')
})

async function refresh() {
  loading.value = true
  try {
    status.value = await McpWizardAPI.McpWizardService.GetStatus()
  } catch (e: unknown) {
    showToast(`探测客户端配置失败: ${getErrorMessage(e)}`)
  } finally {
    loading.value = false
  }
}

// 对外话术统一「接入 / 断开」；Go 面与令牌语义仍是 install/uninstall。
function actionWord(mode: 'install' | 'uninstall'): string {
  return mode === 'install' ? '接入' : '断开'
}

async function openWizard(client: ClientState, mode: 'install' | 'uninstall') {
  modal.value = { client, mode, preview: null, result: null, busy: true, check: null, checkBusy: false }
  try {
    const pv = mode === 'install'
      ? await McpWizardAPI.McpWizardService.PreviewInstall(client.id)
      : await McpWizardAPI.McpWizardService.PreviewUninstall(client.id)
    if (modal.value) modal.value.preview = pv
    // 自检只在「可写入的安装预览」里做：拒动路径本就不落盘，无须再 spawn
    if (mode === 'install' && pv.allowed) void runCheck(false)
  } catch (e: unknown) {
    showToast(`${actionWord(mode)}预览失败: ${getErrorMessage(e)}`)
    modal.value = null
  } finally {
    if (modal.value) modal.value.busy = false
  }
}

/** runCheck 调 Go 侧自检（refresh=true 强制重 spawn）；弹窗切换后丢弃迟到结果。 */
async function runCheck(refresh: boolean) {
  const m = modal.value
  if (!m || m.mode !== 'install') return
  m.checkBusy = true
  try {
    const info = await McpWizardAPI.McpWizardService.SelfCheck(refresh)
    if (modal.value === m) m.check = info
  } catch (e: unknown) {
    // 绑定层异常（进程崩桥/超时）按自检失败呈现，原因取错误文本
    if (modal.value === m) {
      m.check = { state: 'failed', toolCount: 0, tools: null, message: `自检调用失败: ${getErrorMessage(e)}`, checkedAt: '', fresh: false }
    }
  } finally {
    if (modal.value === m) m.checkBusy = false
  }
}

async function confirmWrite() {
  const m = modal.value
  if (!m?.preview?.allowed || !m.preview.token) return
  m.busy = true
  try {
    const res = m.mode === 'install'
      ? await McpWizardAPI.McpWizardService.ConfirmInstall(m.client.id, m.preview.token)
      : await McpWizardAPI.McpWizardService.ConfirmUninstall(m.client.id, m.preview.token)
    if (modal.value) modal.value.result = res
    if (!res.success) showToast(`${actionWord(m.mode)}未完成：${res.message}`)
    await refresh()
  } catch (e: unknown) {
    showToast(`${actionWord(m.mode)}被拒绝: ${getErrorMessage(e)}`)
    if (modal.value) modal.value.result = { success: false, rolledBack: false, backupPath: '', message: getErrorMessage(e) }
  } finally {
    if (modal.value) modal.value.busy = false
  }
}

function closeWizard() {
  modal.value = null
}

const resultChip = computed(() => {
  const r = modal.value?.result
  if (!r) return 'chip-neutral'
  if (r.success) return 'chip-positive'
  return r.rolledBack ? 'chip-warning' : 'chip-danger'
})

/** access.json 所在目录（对文件本身 OpenPath 语义是"打开文件"，这里只要目录）。 */
const accessDir = computed(() => {
  const p = status.value?.access.path ?? ''
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return i > 0 ? p.slice(0, i) : p
})

async function openAccessDir() {
  if (!accessDir.value) return
  try {
    await AppAPI.AppService.OpenPath(accessDir.value)
  } catch (e: unknown) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <PageHeader title="AI 接入" subtitle="让 Claude Code / Codex / Cursor 里的 AI 助手直接使用 hanxi 的本机能力。只需两步：先在下方点「接入」，再打开愿意让 AI 查询的内容开关。每一步都先预览、你确认了才动文件；随时可关、可断开。">
      <template #actions>
        <span class="chip chip-information">只读能力 · hanxi 不替 AI 改任何东西</span>
      </template>
    </PageHeader>

    <!-- ① 接入：客户端列表（主文案只讲"接没接"，配置文件路径退入行内技术细节） -->
    <div class="card">
      <div class="card-head">
        <span class="card-title"><span class="step-no">①</span> 接入你的 AI 软件</span>
        <span class="card-meta">
          <button class="btn btn-ghost btn-small" :disabled="loading" @click="refresh">
            <AppIcon name="search" :size="13" /> {{ loading ? '探测中…' : '重新探测' }}
          </button>
        </span>
      </div>
      <div class="step-note">「接入」= 在 AI 软件自己的配置里登记一行 hanxi 启动命令。动手前先给你看改什么，写完自动校验，不对就自动还原。</div>
      <div v-if="loading && !status" class="ai-empty">正在探测客户端配置文件…</div>
      <div v-else class="client-list">
        <div v-for="c in status?.clients ?? []" :key="c.id" class="client-row">
          <div class="client-main">
            <span class="client-name">
              {{ c.name }}
              <span class="chip" :class="stateMeta[c.state]?.chip ?? 'chip-neutral'">{{ stateMeta[c.state]?.label ?? c.state }}</span>
            </span>
            <span v-if="c.detail" class="client-detail" :class="{ 'detail-warn': c.state === 'conflict' || c.state === 'blocked' }">{{ c.detail }}</span>
            <span v-if="c.installedAt" class="client-time">接入于 {{ c.installedAt }}</span>
            <details v-if="c.configPath" class="row-tech">
              <summary>技术细节</summary>
              <code class="client-path" :title="c.configPath">{{ c.configPath }}</code>
            </details>
          </div>
          <div class="client-actions">
            <button class="btn btn-secondary btn-small" @click="openWizard(c, 'install')" :disabled="!c.canInstall">
              {{ c.state === 'needs-repair' ? '重新接入' : '接入' }}
            </button>
            <button class="btn btn-ghost btn-small" @click="openWizard(c, 'uninstall')" :disabled="!c.canUninstall">断开</button>
            <button v-if="!c.canInstall && !c.canUninstall" class="btn btn-ghost btn-small" @click="openWizard(c, 'install')">查看指引</button>
          </div>
        </div>
      </div>
    </div>

    <!-- ② 开放内容（access.json · 本分区即写入口，R6）：六开关保存即生效；损坏档锁死并给修复链 -->
    <div class="card">
      <div class="card-head">
        <span class="card-title"><span class="step-no">②</span> 允许 AI 查询哪些内容</span>
        <span class="chip" :class="accessMeta.chip">{{ accessMeta.label }}</span>
      </div>
      <div class="step-note">没打开的项，AI 问不到任何内容；拨动开关立即生效，不用重启任何软件。</div>
      <div class="access-tools">
        <div v-for="t in accessTools" :key="t.key" class="tool-row">
          <div class="tool-main">
            <span class="tool-name">
              {{ t.name }}
              <span class="chip" :class="t.risk.chip">{{ t.risk.text }}</span>
              <span class="chip" :class="t.on ? 'chip-positive' : 'chip-neutral'">{{ t.on ? '已开放' : '未开放' }}</span>
            </span>
            <span class="tool-desc">{{ t.desc }}</span>
            <details class="row-tech">
              <summary>技术细节</summary>
              <code class="tool-id">{{ t.tool }}</code>
            </details>
          </div>
          <input
            type="checkbox"
            class="switch"
            role="switch"
            :aria-checked="t.on"
            :aria-label="`授权工具 ${t.name}`"
            :checked="t.on"
            :disabled="accessBusy || accessCorrupt"
            @change="setTool(t, ($event.target as HTMLInputElement).checked)"
          />
        </div>
      </div>
      <div v-if="status?.access.note" class="access-note" :class="{ 'note-danger': accessCorrupt }">{{ status.access.note }}</div>
      <div v-if="accessCorrupt" class="access-repair">
        <button class="btn btn-danger-outline btn-small" :disabled="accessBusy" @click="repairAccess">修复（备份并重置）</button>
        <span class="access-hint">旧文件会先备份，再重置为全部关闭——不会无退路覆盖</span>
      </div>
    </div>

    <!-- 技术细节汇总（术语全量收纳：启动命令、stdio、授权档路径与开关名对照） -->
    <details class="card tech-details">
      <summary class="tech-summary">技术细节（给开发者）</summary>
      <div class="tech-body">
        <div class="tech-line">
          <span class="tech-label">MCP 传输</span>
          <code class="server-cmd" :title="serverCmd">{{ serverCmd }}</code>
          <span v-if="status && !status.server.ready" class="chip chip-danger">路径不可解析 · 接入已禁用</span>
        </div>
        <table class="tech-table">
          <thead><tr><th>开关</th><th>MCP 工具名</th><th>access.json 键</th></tr></thead>
          <tbody>
            <tr v-for="t in accessTools" :key="t.key"><td>{{ t.name }}</td><td><code>{{ t.tool }}</code></td><td><code>{{ t.key }}</code></td></tr>
          </tbody>
        </table>
        <div class="access-foot">
          <code class="client-path" :title="status?.access.path">授权文件：{{ status?.access.path || '—' }}</code>
          <button class="btn btn-secondary btn-small" :disabled="!status?.access.path" @click="openAccessDir">
            <AppIcon name="folder" :size="13" /> 打开所在目录
          </button>
        </div>
        <div class="access-hint">协议形态：stdio（客户端拉起 <code>hanxi mcp</code> 子进程对话）；读者每次工具调用重读授权文件，撤销即时生效。</div>
      </div>
    </details>

    <!-- 向导弹窗：预览(diff/手动片段) → 确认 → 结果三态 -->
    <div v-if="modal" class="modal-backdrop" @click.self="closeWizard">
      <div class="modal-card">
        <div class="modal-head">
          <h3>{{ actionWord(modal.mode) }} · {{ modal.client.name }}</h3>
          <button class="btn-close" aria-label="关闭" @click="closeWizard">✕</button>
        </div>
        <div class="modal-body">
          <div v-if="modal.busy && !modal.preview" class="ai-empty">正在生成预览…</div>
          <template v-else-if="modal.preview">
            <template v-if="!modal.result">
              <div class="pv-path">
                目标文件：<code :title="modal.preview.configPath">{{ modal.preview.configPath }}</code>
                <span v-if="modal.preview.willCreate" class="chip chip-information">文件不存在，将新建</span>
                <span v-if="modal.preview.zeroDiff" class="chip chip-neutral">零改动（幂等）</span>
              </div>
              <template v-if="modal.preview.allowed">
                <!-- 接入前自检（R2）：spawn 自家 hanxi mcp 走 initialize→tools/list -->
                <div v-if="modal.mode === 'install'" class="check-row">
                  <span class="check-label">接入前自检</span>
                  <span v-if="modal.checkBusy && !modal.check" class="chip chip-neutral">握手中…</span>
                  <template v-else-if="modal.check">
                    <span class="chip" :class="modal.check.state === 'ok' ? 'chip-positive' : 'chip-danger'"
                      :title="`检查于 ${modal.check.checkedAt}${modal.check.fresh ? '' : '（会话内缓存）'}`">
                      {{ modal.check.state === 'ok' ? `通过（${modal.check.toolCount} 工具）` : '未通过' }}
                    </span>
                    <span class="check-msg" :class="{ 'detail-warn': modal.check.state !== 'ok' }">{{ modal.check.message }}</span>
                    <button class="btn btn-ghost btn-small" :disabled="modal.checkBusy" @click="runCheck(true)">
                      {{ modal.checkBusy ? '握手中…' : '重新自检' }}
                    </button>
                    <div v-if="modal.check.state !== 'ok'" class="check-hint">
                      自检失败不阻断本次写入——条目在客户端真正拉起 hanxi mcp 之前不会生效，工具面另有授权开关兜底。
                      若客户端日后连不上 hanxi：检查杀毒软件/企业策略是否拦截其子进程，或在终端运行 hanxi mcp 观察报错后「重新自检」。
                    </div>
                  </template>
                </div>
                <div class="diff-pane" role="log" aria-label="配置差异预览">
                  <div v-for="(l, i) in modal.preview.diff" :key="i" class="diff-line" :class="`d-${l.kind}`">
                    <span class="diff-g">{{ l.kind === 'add' ? '+' : l.kind === 'del' ? '-' : ' ' }}</span>{{ l.text }}
                  </div>
                </div>
                <div v-if="modal.preview.reason" class="pv-reason">{{ modal.preview.reason }}</div>
              </template>
              <template v-else>
                <div class="refuse-block">
                  <div class="refuse-title">为安全起见，不会自动改你的文件</div>
                  <div class="refuse-reason">{{ modal.preview.reason || '该文件无法安全合并' }}</div>
                  <pre v-if="modal.preview.manualSnippet" class="snippet">{{ modal.preview.manualSnippet }}</pre>
                </div>
              </template>
            </template>
            <template v-else>
              <div class="result-block">
                <span class="chip" :class="resultChip">
                  {{ modal.result.success ? '完成' : modal.result.rolledBack ? '已回滚' : '未完成' }}
                </span>
                <div class="result-message">{{ modal.result.message }}</div>
                <div v-if="modal.result.backupPath" class="result-bak">备份文件：<code>{{ modal.result.backupPath }}</code></div>
              </div>
            </template>
          </template>
        </div>
        <div class="modal-actions">
          <button class="btn btn-secondary" @click="closeWizard">{{ modal.result ? '关闭' : '取消' }}</button>
          <button
            v-if="!modal.result && modal.preview?.allowed"
            class="btn btn-primary"
            :disabled="modal.busy"
            @click="confirmWrite"
          >
            {{ modal.busy ? '写入中…' : `确认${actionWord(modal.mode)}${modal.preview.zeroDiff ? '' : '（自动备份）'}` }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 行骨架复用全局 .card / .chip / .btn / .setting 原子，此处仅本分区专属皮 */
.card { margin-bottom: 16px; }

.step-no { color: var(--color-primary); font-weight: 700; margin-right: 2px; }
.step-note { font-size: var(--text-sm); color: var(--color-text-muted); margin-bottom: 8px; }

.server-cmd {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text);
  background: var(--surface-chrome); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 3px 8px; max-width: 560px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.card-head { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 8px; gap: 8px; }
.card-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text); }
.card-meta { font-size: var(--text-xs); color: var(--color-text-subtle); }
.ai-empty { padding: 18px 4px; font-size: var(--text-sm); color: var(--color-text-muted); }

.client-list { display: flex; flex-direction: column; gap: 8px; }
.client-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 8px 10px; border: 1px solid var(--color-border); border-radius: var(--radius-element);
}
.client-row:hover { background: var(--surface-hover); }
.client-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.client-name { display: inline-flex; align-items: center; gap: 8px; font-size: var(--text-base); font-weight: 600; color: var(--color-text); }
.client-path {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 520px;
}
.client-detail { font-size: var(--text-sm); color: var(--color-text-muted); }
.client-detail.detail-warn { color: var(--state-warning, var(--color-text)); }
.client-time { font-size: var(--text-xs); color: var(--color-text-subtle); }
.client-actions { display: flex; gap: 6px; flex: none; }

.row-tech { font-size: var(--text-xs); color: var(--color-text-subtle); }
.row-tech summary { cursor: pointer; list-style: none; user-select: none; width: fit-content; }
.row-tech summary::-webkit-details-marker { display: none; }
.row-tech summary::before { content: '▸ '; }
.row-tech[open] summary::before { content: '▾ '; }
.row-tech > *:not(summary) { margin-top: 2px; }

.access-tools { display: flex; flex-direction: column; gap: 6px; margin: 4px 0 8px; }
.tool-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 8px 10px; border: 1px solid var(--color-border); border-radius: var(--radius-element);
}
.tool-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.tool-name { display: inline-flex; align-items: center; gap: 8px; font-size: var(--text-sm); font-weight: 600; color: var(--color-text); flex-wrap: wrap; }
.tool-desc { font-size: var(--text-sm); color: var(--color-text-muted); }
.tool-id { font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle); }
.switch { width: 18px; height: 18px; cursor: pointer; accent-color: var(--color-primary); flex: none; }
.switch:disabled { cursor: not-allowed; }
.access-note { font-size: var(--text-sm); color: var(--color-text-muted); margin-bottom: 8px; }
.access-note.note-danger { color: var(--state-danger, var(--color-text)); }
.access-repair { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.access-hint { font-size: var(--text-xs); color: var(--color-text-subtle); margin-top: 6px; }
.access-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 4px; }

.tech-details summary { cursor: pointer; font-size: var(--text-sm); color: var(--color-text-muted); user-select: none; }
.tech-body { display: flex; flex-direction: column; gap: 10px; margin-top: 10px; }
.tech-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.tech-label { font-size: var(--text-sm); color: var(--color-text-muted); flex: none; }
.tech-table { border-collapse: collapse; font-size: var(--text-xs); width: fit-content; }
.tech-table th, .tech-table td { text-align: left; padding: 3px 12px 3px 0; border-bottom: 1px solid var(--color-border); color: var(--color-text-muted); }
.tech-table code { font-family: var(--font-mono); color: var(--color-text); }

/* 向导弹窗（皮对齐 SnapshotSection 的 .modal-* 家族） */
.modal-backdrop {
  position: fixed; inset: 0; z-index: 100;
  background: var(--overlay-mask);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: var(--surface-panel); border-radius: var(--radius-element); width: 640px; max-width: 92vw;
  box-shadow: var(--shadow-panel); overflow: hidden; display: flex; flex-direction: column;
}
.modal-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 20px; border-bottom: 1px solid var(--color-border);
}
.modal-head h3 { margin: 0; font-size: var(--text-base); color: var(--color-text); font-weight: 600; }
.btn-close { background: transparent; border: none; font-size: var(--text-lg); cursor: pointer; color: var(--color-text-muted); }
.modal-body { padding: 14px 20px; display: flex; flex-direction: column; gap: 10px; max-height: 62vh; overflow: auto; }
.modal-actions {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 12px 20px; border-top: 1px solid var(--color-border);
}

.pv-path { font-size: var(--text-sm); color: var(--color-text-muted); display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.pv-path code { font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text); }
.pv-reason { font-size: var(--text-sm); color: var(--color-text-muted); }

/* 接入前自检行（R2）：一行结论徽章 + 原因 + 重检按钮；失败时追加指引段 */
.check-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.check-label { font-size: var(--text-sm); color: var(--color-text-muted); flex: none; }
.check-msg { font-size: var(--text-sm); color: var(--color-text-muted); min-width: 0; }
.check-msg.detail-warn { color: var(--state-danger, var(--color-text)); }
.check-hint {
  flex-basis: 100%; font-size: var(--text-xs); line-height: 1.6;
  color: var(--color-text-muted);
  background: var(--surface-chrome); border-left: 2px solid var(--state-warning, var(--color-border));
  padding: 6px 10px; border-radius: 0 var(--radius-element) var(--radius-element) 0;
}

.diff-pane {
  border: 1px solid var(--color-border); border-radius: var(--radius-element);
  padding: 8px 10px; overflow: auto; max-height: 40vh; background: var(--surface-chrome);
}
.diff-line {
  font-family: var(--font-mono); font-size: var(--text-xs); line-height: 1.6; white-space: pre-wrap; word-break: break-all;
  color: var(--color-text-muted);
}
.diff-g { display: inline-block; width: 12px; user-select: none; }
.diff-line.d-add { color: var(--state-positive, var(--color-text)); background: var(--state-positive-soft, transparent); }
.diff-line.d-del { color: var(--state-danger, var(--color-text)); background: var(--state-danger-soft, transparent); }

.refuse-block { display: flex; flex-direction: column; gap: 8px; }
.refuse-title { font-size: var(--text-base); font-weight: 600; color: var(--state-warning, var(--color-text)); }
.refuse-reason { font-size: var(--text-sm); color: var(--color-text); }
.snippet {
  margin: 0; padding: 10px 12px; overflow: auto;
  font-family: var(--font-mono); font-size: var(--text-xs); line-height: 1.5; color: var(--color-text);
  white-space: pre-wrap; word-break: break-all;
  background: var(--surface-chrome); border: 1px solid var(--color-border); border-radius: var(--radius-element);
}

.result-block { display: flex; flex-direction: column; gap: 8px; }
.result-message { font-size: var(--text-sm); color: var(--color-text); }
.result-bak { font-size: var(--text-xs); color: var(--color-text-subtle); font-family: var(--font-mono); word-break: break-all; }
</style>
