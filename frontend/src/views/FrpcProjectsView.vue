<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, nextTick } from 'vue'
import * as FrpcAPI from '../../bindings/hanxi/internal/modules/frpc/frpcservice'
import type { Project } from '../../bindings/hanxi/internal/domain/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/frpc/instance/models'
import FrpcProjectEditor from '../components/FrpcProjectEditor.vue'
import FrpcVersionsTab from '../components/FrpcVersionsTab.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useConfirm } from '../composables/useConfirm'
import { useClipboard } from '../composables/useClipboard'

const { showToast } = useToast()
const { confirm } = useConfirm()
const { copy } = useClipboard()

// 顶层主选项卡：projects = 项目列表，versions = 版本管理
const activeMainTab = ref('projects')
const mainTabs = [
  { key: 'projects', label: '⧉ 穿透项目' },
  { key: 'versions', label: '📦 版本管理' },
]

const projects = shallowRef<Project[]>([])
const loading = ref(false)
const errorMsg = ref('')

// 编辑器状态：null = 关闭；{project: null} = 新建；{project} = 编辑
const editorOpen = ref(false)
const editingProject = ref<Project | null>(null)
const installedVersions = shallowRef<string[]>([])

// 实例状态：projectId → 最新快照
const instances = shallowRef<Record<string, Snapshot>>({})
const starting = ref<Set<string>>(new Set()) // 启动请求进行中（防重复点击）

// 导入 Modal 状态
const importModalOpen = ref(false)
const importType = ref<'link' | 'toml'>('link')
const importContent = ref('')
const importError = ref('')

// 日志抽屉
const drawerOpen = ref(false)
const drawerProjectId = ref('')
const logLines = ref<string[]>([])
const logLoading = ref(false)
const logError = ref('')
const logAutoScroll = ref(true)
const logBodyRef = ref<HTMLElement | null>(null)

// 运行时长每秒刷新
const nowTick = ref(Date.now())

function projectName(id: string): string {
  return projects.value.find(p => p.id === id)?.name ?? id
}

// stripAnsi 剥离 frpc 输出的 ANSI 色码（\x1b[1;34m 等）
function stripAnsi(s: string): string {
  return s.replace(/\x1b\[[\d;]*m/g, '')
}

// cleanLines 剥离色码后的日志（保留原行做样式判断）
const displayLines = computed(() => logLines.value.map(l => stripAnsi(l)))

function stateOf(p: Project): Snapshot | undefined {
  return instances.value[p.id]
}

function isActive(p: Project): boolean {
  const s = stateOf(p)
  return s?.state === 'running' || s?.state === 'starting'
}

function runningDuration(s: Snapshot | undefined): string {
  if (!s || s.state !== 'running' || !s.startedAt) return ''
  const start = new Date(s.startedAt).getTime()
  const secs = Math.max(0, Math.floor((nowTick.value - start) / 1000))
  if (secs < 60) return `${secs}s`
  const m = Math.floor(secs / 60)
  if (m < 60) return `${m}m ${secs % 60}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

const stateBadge = computed(() => (p: Project): { cls: string; text: string; dot: string; connInfo?: string } => {
  const s = stateOf(p)
  switch (s?.state) {
    case 'running': {
      let connDesc = ''
      let dotColor = 'var(--state-positive)'
      let statusCls = 'running'

      switch (s.connState) {
        case 'connected':
          connDesc = '已连接服务端'
          dotColor = 'var(--state-positive)'
          break
        case 'connecting':
          connDesc = '握手中…'
          dotColor = 'var(--state-warning)'
          statusCls = 'starting'
          break
        case 'auth_failed':
          connDesc = '鉴权失败 (Token错误)'
          dotColor = 'var(--state-danger)'
          statusCls = 'failed'
          break
        case 'reconnecting':
          connDesc = '重连服务端中…'
          dotColor = 'var(--state-warning)'
          statusCls = 'starting'
          break
        case 'error':
          connDesc = '连接异常'
          dotColor = 'var(--state-danger)'
          statusCls = 'failed'
          break
      }

      const text = connDesc
        ? `${connDesc} · ${runningDuration(s)}`
        : `运行中 · ${runningDuration(s)}`

      return { cls: statusCls, text, dot: dotColor, connInfo: connDesc }
    }
    case 'starting': return { cls: 'starting', text: '启动中…', dot: 'var(--state-warning)' }
    case 'failed': return { cls: 'failed', text: '启动失败', dot: 'var(--state-danger)' }
    default: return { cls: 'stopped', text: '未启动', dot: 'var(--color-border-strong)' }
  }
})

async function loadProjects() {
  loading.value = true
  errorMsg.value = ''
  try {
    projects.value = (await FrpcAPI.ListProjects()) ?? []
  } catch (err: unknown) {
    errorMsg.value = `加载项目失败: ${getErrorMessage(err)}`
  } finally {
    loading.value = false
  }
}

async function loadInstances() {
  try {
    const snaps = (await FrpcAPI.ListInstanceStates()) ?? []
    const map: Record<string, Snapshot> = {}
    for (const s of snaps) map[s.projectId] = s
    instances.value = map
  } catch {
    // 静默：状态为空也可接受
  }
}

async function loadInstalledVersions() {
  try {
    const list = (await FrpcAPI.ListInstalledVersions()) ?? []
    installedVersions.value = list.map(v => v.version)
  } catch {
    installedVersions.value = []
  }
}

function openCreate() {
  editingProject.value = null
  editorOpen.value = true
}

function openEdit(p: Project) {
  editingProject.value = p
  editorOpen.value = true
}

function copyProject(p: Project) {
  // 深度复制一份项目配置，清空 id，名称添加后缀，直接打开编辑页供用户修改保存
  const cloned: Project = {
    ...JSON.parse(JSON.stringify(p)),
    id: '', // 空 ID 表示新建，保存时将生成新 ID
    name: `${p.name} (副本)`,
    createdAt: '',
    updatedAt: '',
  }
  editingProject.value = cloned
  editorOpen.value = true
}

function exportProjectShareLink(p: Project) {
  const sharePayload = {
    name: p.name,
    server: {
      serverAddr: p.server.serverAddr,
      serverPort: p.server.serverPort,
      token: p.server.token,
      tlsEnable: p.server.tlsEnable,
      useEncryption: p.server.useEncryption,
      useCompression: p.server.useCompression,
      logLevel: p.server.logLevel,
    },
    proxies: p.proxies,
  }
  const jsonStr = JSON.stringify(sharePayload)
  const b64 = btoa(encodeURIComponent(jsonStr).replace(/%([0-9A-F]{2})/g, (_, p1) => String.fromCharCode(parseInt(p1, 16))))
  const link = `frp://${b64}`
  // 剪贴板收编 useClipboard（两级回退）；分享链接编码逻辑不变
  void copy(link).then((ok) => showToast(ok ? `已复制「${p.name}」分享链接 (frp://...)` : '复制失败'))
}

function openImportModal() {
  importContent.value = ''
  importError.value = ''
  importType.value = 'link'
  importModalOpen.value = true
}

async function parseAndImport() {
  importError.value = ''
  const raw = importContent.value.trim()
  if (!raw) {
    importError.value = '请粘贴导入内容'
    return
  }

  try {
    let targetProject: Project | null = null

    if (importType.value === 'link' || raw.startsWith('frp://')) {
      const b64 = raw.replace(/^frp:\/\//, '').trim()
      const jsonStr = decodeURIComponent(atob(b64).split('').map(c => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2)).join(''))
      const parsed = JSON.parse(jsonStr)
      targetProject = {
        id: '',
        name: parsed.name ? `${parsed.name} (导入)` : '导入项目',
        version: '',
        createdAt: '',
        updatedAt: '',
        server: parsed.server || {
          serverAddr: parsed.serverAddr || '',
          serverPort: parsed.serverPort || 7000,
          token: parsed.token || parsed.authToken || '',
          tlsEnable: parsed.tlsEnable ?? false,
          useEncryption: parsed.useEncryption ?? true,
          useCompression: parsed.useCompression ?? false,
          logLevel: parsed.logLevel || 'info',
        },
        proxies: parsed.proxies || [],
      }
    } else {
      // TOML 格式导入
      const p = await FrpcAPI.ParseToml(raw)
      targetProject = {
        ...p,
        id: '',
        name: '导入项目 (TOML)',
        version: '',
        createdAt: '',
        updatedAt: '',
      }
    }

    if (targetProject) {
      importModalOpen.value = false
      editingProject.value = targetProject
      editorOpen.value = true
      showToast('配置已解析，请核对并保存')
    }
  } catch (err: unknown) {
    importError.value = `解析失败: ${getErrorMessage(err)}`
  }
}

async function onSaved() {
  editorOpen.value = false
  await loadProjects()
}

async function toggleStart(p: Project) {
  if (starting.value.has(p.id) || stateOf(p)?.state === 'starting' || stateOf(p)?.state === 'running') {
    // 运行中：停止
    if (stateOf(p)?.state !== 'running') return
    try {
      await FrpcAPI.StopProject(p.id)
      showToast(`已停止「${p.name}」`)
    } catch (err: unknown) {
      showToast(`停止失败: ${getErrorMessage(err)}`)
    }
    return
  }
  starting.value.add(p.id)
  try {
    await FrpcAPI.StartProject(p.id)
    showToast(`正在启动「${p.name}」…`)
  } catch (err: unknown) {
    showToast(`启动失败: ${getErrorMessage(err)}`)
    await loadInstances() // 同步失败态
  } finally {
    starting.value.delete(p.id)
  }
}

async function deleteProject(p: Project) {
  if (isActive(p)) {
    showToast('请先停止实例再删除项目')
    return
  }
  // 删除确认收编全局 useConfirm（文案逐字拆 title/description）
  const accepted = await confirm({
    title: `确定删除项目「${p.name}」？`,
    description: '配置将被永久移除。',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await FrpcAPI.DeleteProject(p.id)
    showToast(`已删除「${p.name}」`)
    await Promise.all([loadProjects(), loadInstances()])
  } catch (err: unknown) {
    showToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

function typeCount(p: Project): string {
  const map = new Map<string, number>()
  for (const r of p.proxies ?? []) map.set(r.type, (map.get(r.type) ?? 0) + 1)
  return [...map.entries()].map(([t, n]) => `${t}×${n}`).join(' · ')
}

// 格式化展示规则穿透目标地址及链接
interface ProxyEndpoint {
  name: string
  type: string
  local: string
  remoteDisplay: string
  url?: string
}

function resolveEndpoints(p: Project): ProxyEndpoint[] {
  const serverHost = p.server?.serverAddr || '127.0.0.1'
  return (p.proxies ?? []).map(r => {
    let local = `${r.localIp || '127.0.0.1'}:${r.localPort}`
    let remoteDisplay = ''
    let url: string | undefined
    const isVisitor = r.role === 'visitor'

    if (isVisitor) {
      local = `${r.bindAddr || '127.0.0.1'}:${r.bindPort}`
      remoteDisplay = `service:${r.serverName}`
    } else if (r.type === 'http' || r.type === 'https') {
      const proto = r.type
      if (r.customDomains && r.customDomains.length > 0) {
        const domain = r.customDomains[0]
        remoteDisplay = domain + (r.customDomains.length > 1 ? ` (+${r.customDomains.length - 1})` : '')
        url = `${proto}://${domain}`
      } else if (r.subdomain) {
        remoteDisplay = `${r.subdomain}.${serverHost}`
        url = `${proto}://${r.subdomain}.${serverHost}`
      } else {
        remoteDisplay = `${proto}://${serverHost}`
        url = `${proto}://${serverHost}`
      }
    } else if (r.type === 'tcp' || r.type === 'udp') {
      remoteDisplay = `${serverHost}:${r.remotePort}`
    } else if (r.type === 'stcp' || r.type === 'xtcp') {
      remoteDisplay = `p2p:${r.name}`
    } else {
      remoteDisplay = r.type
    }

    return {
      name: r.name,
      type: isVisitor ? `${r.type}-visitor` : r.type,
      local,
      remoteDisplay,
      url,
    }
  })
}

function copyEndpoint(ep: ProxyEndpoint) {
  const text = ep.url || ep.remoteDisplay
  if (!text) return
  void copy(text).then((ok) => showToast(ok ? `已复制: ${text}` : '复制失败'))
}

// ---------- 日志抽屉 ----------

// 事件行缓冲基线：拉取返回前可能已收到事件行，用基线合并避免丢行
let logPullBaseline = 0

async function openLogs(p: Project) {
  drawerProjectId.value = p.id
  logPullBaseline = logLines.value.length // 记录拉取前已缓冲的事件行
  logError.value = ''
  logLoading.value = true
  drawerOpen.value = true
  try {
    const initial = (await FrpcAPI.GetProjectLogs(p.id, 500)) ?? []
    // 先到的事件行（baseline 之后）追加在初始快照之后
    logLines.value = [...initial, ...logLines.value.slice(logPullBaseline)]
  } catch (err: unknown) {
    logError.value = `拉取日志失败: ${getErrorMessage(err)}`
  } finally {
    logLoading.value = false
    await scrollToBottom()
  }
}

function closeLogs() {
  drawerOpen.value = false
  drawerProjectId.value = ''
}

async function scrollToBottom() {
  await nextTick()
  const el = logBodyRef.value
  if (el && logAutoScroll.value) el.scrollTop = el.scrollHeight
}

function clearLogs() {
  logLines.value = []
}

function nowTickRefresh() {
  nowTick.value = Date.now()
}

// ---------- 订阅与生命周期 ----------
useWailsEvent<Snapshot>('frpc:instance-state', (snap) => {
  if (!snap?.projectId) return
  instances.value = { ...instances.value, [snap.projectId]: snap }
})

useWailsEvent<{ projectId: string; line: string }>('frpc:instance-log', (entry) => {
  if (!entry?.projectId || entry.projectId !== drawerProjectId.value) return
  logLines.value.push(entry.line)
  if (logLines.value.length > 2000) {
    logLines.value.splice(0, logLines.value.length - 2000)
  }
  void scrollToBottom()
})

// 运行时长秒表（usePolling 内置 KeepAlive 激活/停用/卸载契约）
usePolling(nowTickRefresh, 1000)

onMounted(async () => {
  await Promise.all([loadProjects(), loadInstances(), loadInstalledVersions()])
})
</script>

<template>
  <section class="page projects-page">
    <!-- 编辑模式 -->
    <FrpcProjectEditor
      v-if="editorOpen"
      :project="editingProject"
      :installed-versions="installedVersions"
      @saved="onSaved"
      @cancel="editorOpen = false"
    />

    <!-- 列表模式 -->
    <template v-else>
      <PageHeader title="frpc 穿透" subtitle="支持多实例隔离运行与版本管理。退出 Hanxi 时内核连带清理，杜绝孤儿进程。">
        <template #actions>
          <MainTabNav v-model="activeMainTab" :tabs="mainTabs" />
        </template>
      </PageHeader>

      <!-- 版本管理 Tab -->
      <div v-show="activeMainTab === 'versions'" class="tab-body">
        <FrpcVersionsTab @version-changed="loadInstalledVersions" />
      </div>

      <!-- 项目管理 Tab -->
      <div v-show="activeMainTab === 'projects'" class="tab-body">
        <div class="control-panel">
          <div class="meta-info">
            <span>共 {{ projects.length }} 个项目 · {{ Object.keys(instances).filter(id => instances[id]?.state === 'running').length }} 个运行中</span>
          </div>
          <div class="control-actions">
            <button class="btn btn-secondary" @click="openImportModal">⬇ 导入配置 / 链接</button>
            <button class="btn btn-primary" @click="openCreate">+ 新建项目</button>
          </div>
        </div>

        <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

        <div v-if="projects.length === 0 && !loading" class="empty-state">
          <div class="empty-icon">⧉</div>
          <p>还没有 frp 项目</p>
          <div class="empty-actions">
            <button class="btn btn-secondary" @click="openImportModal">导入已有配置</button>
            <button class="btn btn-primary" @click="openCreate">创建第一个项目</button>
          </div>
        </div>

        <div class="project-grid">
          <div v-for="p in projects" :key="p.id" class="project-card" :class="{ active: isActive(p) }">
            <div class="proj-top">
              <div class="proj-title-box">
                <span class="proj-name">{{ p.name }}</span>
                <span v-if="p.version" class="badge badge-version">{{ p.version }}</span>
                <span v-else class="badge badge-unbound">未绑定版本</span>
              </div>
              <span class="proj-status" :class="stateBadge(p).cls" :title="stateOf(p)?.error || ''">
                <span class="dot" :style="{ background: stateBadge(p).dot }"></span>{{ stateBadge(p).text }}
              </span>
            </div>

            <div class="proj-server">
              <span class="server-addr">{{ p.server.serverAddr }}:{{ p.server.serverPort }}</span>
              <span class="server-flags">
                <span v-if="p.server.tlsEnable" class="flag">TLS</span>
                <span v-if="p.server.useEncryption" class="flag">加密</span>
                <span v-if="p.server.useCompression" class="flag">压缩</span>
              </span>
            </div>

            <div class="proj-proxies">
              <span class="proxies-label">{{ (p.proxies ?? []).length }} 条规则</span>
              <span v-if="(p.proxies ?? []).length" class="proxies-types">{{ typeCount(p) }}</span>
              <span v-if="stateOf(p)?.pid" class="proxy-pid">PID {{ stateOf(p)?.pid }}</span>
            </div>

            <!-- 运行态/常态规则目标与快捷直达列表 -->
            <div v-if="(p.proxies ?? []).length" class="endpoints-box">
              <div v-for="ep in resolveEndpoints(p)" :key="ep.name" class="endpoint-row">
                <div class="ep-left">
                  <span class="ep-badge" :class="ep.type">{{ ep.type.toUpperCase() }}</span>
                  <span class="ep-name" :title="ep.name">{{ ep.name }}</span>
                </div>
                <div class="ep-flow">
                  <span class="ep-local" :title="ep.local">{{ ep.local }}</span>
                  <span class="ep-arrow">➜</span>
                  <span class="ep-remote" :title="ep.url || ep.remoteDisplay">
                    <a v-if="ep.url && isActive(p)" :href="ep.url" target="_blank" class="ep-link">{{ ep.remoteDisplay }}</a>
                    <span v-else>{{ ep.remoteDisplay }}</span>
                  </span>
                </div>
                <button class="btn-copy-mini" title="复制远程访问地址" @click="copyEndpoint(ep)">⧉</button>
              </div>
            </div>

            <div v-if="stateOf(p)?.error" class="proj-error">{{ stateOf(p)?.error }}</div>

            <div class="proj-actions">
              <template v-if="stateOf(p)?.state === 'running'">
                <button class="btn btn-stop btn-small" @click="toggleStart(p)">■ 停止</button>
              </template>
              <template v-else>
                <button
                  class="btn btn-primary btn-small"
                  :disabled="starting.has(p.id) || stateOf(p)?.state === 'starting'"
                  @click="toggleStart(p)"
                >{{ starting.has(p.id) || stateOf(p)?.state === 'starting' ? '启动中…' : '▶ 启动' }}</button>
              </template>
              <button class="btn btn-secondary btn-small" @click="openLogs(p)">日志</button>
              <button class="btn btn-secondary btn-small" :disabled="isActive(p)" @click="openEdit(p)">编辑</button>
              <button class="btn btn-secondary btn-small" title="生成并复制 frp:// 分享链接" @click="exportProjectShareLink(p)">⚡ 分享</button>
              <button class="btn btn-secondary btn-small" @click="copyProject(p)">复制</button>
              <button class="btn btn-danger-outline btn-small" :disabled="isActive(p)" @click="deleteProject(p)">删除</button>
            </div>
          </div>
        </div>
      </div>
    </template>

    <!-- 导入弹窗 -->
    <div v-if="importModalOpen" class="modal-backdrop" @click.self="importModalOpen = false">
      <div class="modal-card">
        <div class="modal-head">
          <h3>导入 frpc 配置</h3>
          <button class="btn-close" @click="importModalOpen = false">✕</button>
        </div>
        <div v-if="importError" class="error-box" style="margin: 12px 20px 0;">{{ importError }}</div>
        <div class="modal-body">
          <div class="import-tabs">
            <button class="tab-btn" :class="{ active: importType === 'link' }" @click="importType = 'link'">分享链接 (frp://...)</button>
            <button class="tab-btn" :class="{ active: importType === 'toml' }" @click="importType = 'toml'">TOML 配置文本</button>
          </div>
          <label class="form-item">
            <span>{{ importType === 'link' ? '粘贴 frp:// 分享链接' : '粘贴 frpc.toml 文件内容' }}</span>
            <textarea
              v-model="importContent"
              class="input textarea"
              :placeholder="importType === 'link' ? '例如: frp://eyJzZXJ2ZXIiOnsic2VydmVyQWRkciI...' : 'serverAddr = &quot;x.x.x.x&quot;\nserverPort = 7000\n...'"
              rows="6"
            ></textarea>
          </label>
        </div>
        <div class="modal-actions">
          <button class="btn btn-secondary" @click="importModalOpen = false">取消</button>
          <button class="btn btn-primary" @click="parseAndImport">解析并进入编辑</button>
        </div>
      </div>
    </div>

    <!-- 日志抽屉 -->
    <div v-if="drawerOpen" class="log-drawer">
      <div class="log-drawer-head">
        <span class="log-drawer-title">日志 · {{ projectName(drawerProjectId) }}</span>
        <div class="log-drawer-tools">
          <label class="auto-scroll"><input v-model="logAutoScroll" type="checkbox" />自动滚动</label>
          <button class="btn btn-secondary btn-small" @click="clearLogs">清屏</button>
          <button class="btn btn-secondary btn-small" @click="openLogs(projects.find(x => x.id === drawerProjectId) as any)">刷新</button>
          <button class="btn btn-secondary btn-small" @click="closeLogs">✕ 收起</button>
        </div>
      </div>
      <div v-if="logError" class="log-error">{{ logError }}</div>
      <div ref="logBodyRef" class="log-body" @scroll.passive="logAutoScroll = ($event.target as HTMLElement).scrollTop + ($event.target as HTMLElement).clientHeight >= ($event.target as HTMLElement).scrollHeight - 40">
        <template v-if="displayLines.length">
          <div v-for="(line, i) in displayLines" :key="i" class="log-line" :class="{ 'log-warn': /\[W\]|WARN|error|fail/i.test(line) }">{{ line }}</div>
        </template>
        <div v-else class="log-empty">{{ logLoading ? '加载中…' : '暂无日志输出' }}</div>
      </div>
    </div>
    <div v-if="drawerOpen" class="drawer-backdrop" @click="closeLogs"></div>
  </section>
</template>

<style scoped>
.projects-page { display: flex; flex-direction: column; gap: 16px; height: 100%; }
/* 页头/副标题/选项卡/按钮家族/表格/mono/错误框/空态 由 PageHeader、MainTabNav、components.css 全局原子接管 */
.tab-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--surface-panel); border: 1px solid var(--color-border);
  padding: 12px 16px; border-radius: var(--radius-control);
}
.control-actions { display: flex; gap: 10px; }
.meta-info { font-size: 13px; color: var(--color-text-muted); }

.empty-state { padding: 56px 24px; gap: 10px; }
.empty-icon { font-size: 40px; }
.empty-actions { display: flex; gap: 12px; margin-top: 8px; }

.project-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 14px; }
.project-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-element);
  padding: 16px; display: flex; flex-direction: column; gap: 12px; transition: box-shadow var(--motion-base) ease;
}
.project-card:hover { box-shadow: var(--shadow-small); }
.project-card.active { border-color: var(--state-positive-glow); box-shadow: 0 0 0 1px var(--state-positive-glow); }

.proj-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.proj-title-box { display: flex; align-items: center; gap: 8px; min-width: 0; }
.proj-name { font-size: 15px; font-weight: 600; color: var(--color-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); font-weight: 500; white-space: nowrap; }
.badge-version { background: var(--state-information-soft); color: var(--state-information); }
.badge-unbound { background: var(--surface-hover); color: var(--color-text-muted); }

.proj-status { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; flex-shrink: 0; }
.proj-status .dot { width: 8px; height: 8px; border-radius: 50%; }
.proj-status.stopped { color: var(--color-text-subtle); }
.proj-status.running { color: var(--state-positive); font-weight: 600; }
.proj-status.starting { color: var(--state-warning); }
.proj-status.failed { color: var(--state-danger); }

.proj-server { display: flex; align-items: center; justify-content: space-between; background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 8px 10px; }
.server-addr { font-family: var(--font-mono); font-size: 12px; color: var(--color-text); }
.server-flags { display: flex; gap: 4px; }
.flag { font-size: 10px; padding: 1px 6px; border-radius: 3px; background: var(--state-information-soft); color: var(--state-information); }

.proj-proxies { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--color-text-muted); }
.proxies-label { font-weight: 600; }
.proxies-types { color: var(--color-text-subtle); }
.proxy-pid { margin-left: auto; font-family: var(--font-mono); font-size: 11px; color: var(--color-text-subtle); }

/* 规则端点简略展示 */
.endpoints-box {
  display: flex; flex-direction: column; gap: 6px;
  background: var(--surface-soft); border: 1px solid var(--color-border);
  border-radius: var(--radius-control); padding: 8px 10px; max-height: 140px; overflow-y: auto;
}
.endpoint-row {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  font-size: 12px; font-family: var(--font-mono);
}
.ep-left { display: flex; align-items: center; gap: 6px; min-width: 80px; }
.ep-badge {
  font-size: 10px; font-weight: 600; padding: 1px 5px; border-radius: 3px;
  background: var(--surface-hover); color: var(--color-text-muted);
}
.ep-badge.http, .ep-badge.https { background: var(--state-information-soft); color: var(--state-information); }
.ep-badge.tcp { background: var(--state-positive-soft); color: var(--state-positive); }
.ep-badge.udp { background: var(--state-warning-soft); color: var(--state-warning); }
.ep-name {
  font-family: inherit; font-size: 11px; color: var(--color-text-muted);
  max-width: 70px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.ep-flow {
  display: flex; align-items: center; gap: 6px; flex: 1; min-width: 0;
  justify-content: flex-start;
}
.ep-local { color: var(--color-text-subtle); font-size: 11px; }
.ep-arrow { font-size: 10px; color: var(--color-text-subtle); }
.ep-remote {
  color: var(--color-text); font-weight: 600; font-size: 11px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.ep-link { color: var(--color-primary); text-decoration: none; }
.ep-link:hover { text-decoration: underline; }
.btn-copy-mini {
  background: transparent; border: none; cursor: pointer; font-size: 12px;
  color: var(--color-text-subtle); padding: 0 2px; flex-shrink: 0;
}
.btn-copy-mini:hover { color: var(--color-primary); }

.proj-error { font-size: 12px; color: var(--state-danger); background: var(--state-danger-soft); border: 1px solid var(--state-danger-glow); border-radius: var(--radius-control); padding: 6px 10px; }

.proj-actions { display: flex; gap: 6px; border-top: 1px solid var(--color-border); padding-top: 12px; flex-wrap: wrap; }

/* 导入 Modal 弹窗（富内容手搓件，二次收编 UiModal 在跟进清单；颜色已 token 化） */
.modal-backdrop {
  position: fixed; inset: 0; z-index: 100;
  background: var(--overlay-mask);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: var(--surface-panel); border-radius: var(--radius-element); width: 480px; max-width: 90vw;
  box-shadow: var(--shadow-panel); overflow: hidden;
}
.modal-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 16px 20px; border-bottom: 1px solid var(--color-border);
}
.modal-head h3 { margin: 0; font-size: 16px; color: var(--color-text); }
.btn-close { background: transparent; border: none; font-size: 16px; cursor: pointer; color: var(--color-text-muted); }
.modal-body { padding: 16px 20px; display: flex; flex-direction: column; gap: 12px; }
.modal-actions {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 12px 20px; border-top: 1px solid var(--color-border); background: var(--surface-soft);
}

.import-tabs { display: flex; border-bottom: 1px solid var(--color-border); margin-bottom: 8px; }
.tab-btn {
  padding: 8px 16px; background: transparent; border: none; font-size: 13px;
  color: var(--color-text-muted); cursor: pointer; border-bottom: 2px solid transparent;
}
.tab-btn.active { color: var(--color-primary); border-bottom-color: var(--color-primary); font-weight: 600; }

.textarea {
  padding: 8px 10px; border: 1px solid var(--color-border-strong); border-radius: var(--radius-control);
  font-family: var(--font-mono); font-size: 12px; width: 100%; box-sizing: border-box;
  background: var(--surface-soft); color: var(--color-text); resize: vertical;
}
.textarea:focus { border-color: var(--color-primary); outline: none; background: var(--surface-panel); }

.form-item { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--color-text-muted); }

/* 停止按钮变体（全局原子之外的业务语义色） */
.btn-stop { background: var(--surface-panel); border-color: var(--state-danger); color: var(--state-danger); }
.btn-stop:hover { background: var(--state-danger-soft); }
.btn-danger-outline { margin-left: auto; }

/* 日志抽屉：终端风格固定深底，不随主题反相（tokens.css --terminal-* 约定） */
.log-drawer {
  position: fixed; left: 0; right: 0; bottom: 0; height: 42%;
  background: var(--terminal-bg); color: var(--terminal-fg); border-radius: var(--radius-element) var(--radius-element) 0 0;
  display: flex; flex-direction: column; z-index: 50; overflow: hidden;
  box-shadow: 0 -6px 24px rgba(0, 0, 0, 0.28);
}
.drawer-backdrop { position: fixed; inset: 0; z-index: 40; background: rgba(0, 0, 0, 0.25); }
.log-drawer-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 10px 16px; background: color-mix(in srgb, var(--terminal-fg) 8%, var(--terminal-bg));
  border-bottom: 1px solid color-mix(in srgb, var(--terminal-fg) 18%, var(--terminal-bg));
}
.log-drawer-title { font-size: 13px; font-weight: 600; color: color-mix(in srgb, var(--terminal-fg) 92%, white); }
.log-drawer-tools { display: flex; align-items: center; gap: 8px; }
.auto-scroll { display: flex; align-items: center; gap: 4px; font-size: 12px; color: color-mix(in srgb, var(--terminal-fg) 60%, var(--terminal-bg)); cursor: pointer; }
.auto-scroll input { accent-color: var(--color-primary); }
.log-error { padding: 6px 16px; background: color-mix(in srgb, var(--state-danger) 18%, var(--terminal-bg)); color: var(--state-danger); font-size: 12px; }
.log-body {
  flex: 1; overflow-y: auto; padding: 12px 16px;
  font-family: var(--font-mono); font-size: 12px; line-height: 1.55;
  user-select: text;
}
.log-line { white-space: pre-wrap; word-break: break-all; color: var(--terminal-fg); }
.log-warn { color: var(--ansi-3); }
.log-empty { color: color-mix(in srgb, var(--terminal-fg) 45%, var(--terminal-bg)); text-align: center; padding: 32px 0; }
</style>
