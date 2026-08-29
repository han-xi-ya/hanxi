<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { Events } from '@wailsio/runtime'
import * as FrpcAPI from '../../bindings/hanxi/internal/modules/frpc/frpcservice'
import type { Project } from '../../bindings/hanxi/internal/domain/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/frpc/instance/models'
import FrpcProjectEditor from '../components/FrpcProjectEditor.vue'
import FrpcVersionsTab from '../components/FrpcVersionsTab.vue'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const { showToast } = useToast()

// 顶层主选项卡：projects = 项目列表，versions = 版本管理
const activeMainTab = ref<'projects' | 'versions'>('projects')

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
const logLines = shallowRef<string[]>([])
const logLoading = ref(false)
const logError = ref('')
const logAutoScroll = ref(true)
const logBodyRef = ref<HTMLElement | null>(null)

// 运行时长每秒刷新
const nowTick = ref(Date.now())
let tickTimer: ReturnType<typeof setInterval> | null = null
let unlistenState: (() => void) | null = null
let unlistenLog: (() => void) | null = null

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
      let dotColor = '#2da44e'
      let statusCls = 'running'

      switch (s.connState) {
        case 'connected':
          connDesc = '已连接服务端'
          dotColor = '#2da44e'
          break
        case 'connecting':
          connDesc = '握手中…'
          dotColor = '#d4a72c'
          statusCls = 'starting'
          break
        case 'auth_failed':
          connDesc = '鉴权失败 (Token错误)'
          dotColor = '#cf222e'
          statusCls = 'failed'
          break
        case 'reconnecting':
          connDesc = '重连服务端中…'
          dotColor = '#d4a72c'
          statusCls = 'starting'
          break
        case 'error':
          connDesc = '连接异常'
          dotColor = '#cf222e'
          statusCls = 'failed'
          break
      }

      const text = connDesc
        ? `${connDesc} · ${runningDuration(s)}`
        : `运行中 · ${runningDuration(s)}`

      return { cls: statusCls, text, dot: dotColor, connInfo: connDesc }
    }
    case 'starting': return { cls: 'starting', text: '启动中…', dot: '#d4a72c' }
    case 'failed': return { cls: 'failed', text: '启动失败', dot: '#cf222e' }
    default: return { cls: 'stopped', text: '未启动', dot: '#c1c7cd' }
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
  navigator.clipboard.writeText(link)
  showToast(`已复制「${p.name}」分享链接 (frp://...)`)
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
  if (!window.confirm(`确定删除项目「${p.name}」？\n配置将被永久移除。`)) return
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
  navigator.clipboard.writeText(text)
  showToast(`已复制: ${text}`)
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

onMounted(async () => {
  await Promise.all([loadProjects(), loadInstances(), loadInstalledVersions()])

  unlistenState = Events.On('frpc:instance-state', (event: { data?: Snapshot }) => {
    const snap = event?.data
    if (!snap?.projectId) return
    instances.value = { ...instances.value, [snap.projectId]: snap }
  })
  unlistenLog = Events.On('frpc:instance-log', (event: { data?: { projectId: string; line: string } }) => {
    const entry = event?.data
    if (!entry?.projectId || entry.projectId !== drawerProjectId.value) return
    logLines.value.push(entry.line)
    if (logLines.value.length > 2000) {
      logLines.value.splice(0, logLines.value.length - 2000)
    }
    void scrollToBottom()
  })

  tickTimer = setInterval(() => { nowTick.value = Date.now() }, 1000)
})

onUnmounted(() => {
  if (unlistenState) {
    unlistenState()
    unlistenState = null
  }
  if (unlistenLog) {
    unlistenLog()
    unlistenLog = null
  }
  if (tickTimer) {
    clearInterval(tickTimer)
    tickTimer = null
  }
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
      <div class="header-row">
        <div>
          <h1>frpc 穿透</h1>
          <p class="subtitle">支持多实例隔离运行与版本管理。退出 Hanxi 时内核连带清理，杜绝孤儿进程。</p>
        </div>
        <div class="main-tab-nav">
          <button
            class="main-tab-btn"
            :class="{ active: activeMainTab === 'projects' }"
            @click="activeMainTab = 'projects'"
          >
            ⧉ 穿透项目
          </button>
          <button
            class="main-tab-btn"
            :class="{ active: activeMainTab === 'versions' }"
            @click="activeMainTab = 'versions'"
          >
            📦 版本管理
          </button>
        </div>
      </div>

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
.header-row { display: flex; justify-content: space-between; align-items: center; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }

.main-tab-nav {
  display: flex;
  background: var(--bg-hover);
  padding: 3px;
  border-radius: 8px;
  gap: 2px;
}
.main-tab-btn {
  background: transparent;
  border: none;
  padding: 6px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}
.main-tab-btn:hover {
  color: var(--text-main);
}
.main-tab-btn.active {
  background: var(--bg-app);
  color: var(--accent);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}
.tab-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  padding: 12px 16px; border-radius: 8px;
}
.control-actions { display: flex; gap: 10px; }
.meta-info { font-size: 13px; color: var(--text-muted); }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

.empty-state { text-align: center; padding: 56px 0; color: var(--text-subtle); display: flex; flex-direction: column; align-items: center; gap: 10px; }
.empty-icon { font-size: 40px; }
.empty-actions { display: flex; gap: 12px; margin-top: 8px; }

.project-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 14px; }
.project-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px;
  padding: 16px; display: flex; flex-direction: column; gap: 12px; transition: box-shadow 0.15s ease;
}
.project-card:hover { box-shadow: 0 2px 12px rgba(0, 0, 0, 0.06); }
.project-card.active { border-color: rgba(45, 164, 78, 0.55); box-shadow: 0 0 0 1px rgba(45, 164, 78, 0.25); }

.proj-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.proj-title-box { display: flex; align-items: center; gap: 8px; min-width: 0; }
.proj-name { font-size: 15px; font-weight: 600; color: var(--text-main); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; white-space: nowrap; }
.badge-version { background: #ddf4ff; color: #0969da; }
.badge-unbound { background: var(--bg-hover); color: var(--text-muted); }

.proj-status { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; flex-shrink: 0; }
.proj-status .dot { width: 8px; height: 8px; border-radius: 50%; }
.proj-status.stopped { color: var(--text-subtle); }
.proj-status.running { color: #1a7f37; font-weight: 600; }
.proj-status.starting { color: #9a6700; }
.proj-status.failed { color: var(--danger); }

.proj-server { display: flex; align-items: center; justify-content: space-between; background: var(--bg-app); border: 1px solid var(--border-color); border-radius: 6px; padding: 8px 10px; }
.server-addr { font-family: Consolas, monospace; font-size: 12px; color: var(--text-main); }
.server-flags { display: flex; gap: 4px; }
.flag { font-size: 10px; padding: 1px 6px; border-radius: 3px; background: #f0f7ff; color: #0969da; }

.proj-proxies { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); }
.proxies-label { font-weight: 600; }
.proxies-types { color: var(--text-subtle); }
.proxy-pid { margin-left: auto; font-family: Consolas, monospace; font-size: 11px; color: var(--text-subtle); }

/* 规则端点简略展示 */
.endpoints-box {
  display: flex; flex-direction: column; gap: 6px;
  background: var(--bg-app); border: 1px solid var(--border-color);
  border-radius: 6px; padding: 8px 10px; max-height: 140px; overflow-y: auto;
}
.endpoint-row {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  font-size: 12px; font-family: Consolas, monospace;
}
.ep-left { display: flex; align-items: center; gap: 6px; min-width: 80px; }
.ep-badge {
  font-size: 10px; font-weight: 600; padding: 1px 5px; border-radius: 3px;
  background: var(--bg-hover); color: var(--text-muted);
}
.ep-badge.http, .ep-badge.https { background: #ddf4ff; color: #0969da; }
.ep-badge.tcp { background: #dafbe1; color: var(--success); }
.ep-badge.udp { background: #fff8c5; color: #9a6700; }
.ep-name {
  font-family: inherit; font-size: 11px; color: var(--text-muted);
  max-width: 70px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.ep-flow {
  display: flex; align-items: center; gap: 6px; flex: 1; min-width: 0;
  justify-content: flex-start;
}
.ep-local { color: var(--text-subtle); font-size: 11px; }
.ep-arrow { font-size: 10px; color: var(--text-subtle); }
.ep-remote {
  color: var(--text-main); font-weight: 600; font-size: 11px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.ep-link { color: var(--accent); text-decoration: none; }
.ep-link:hover { text-decoration: underline; }
.btn-copy-mini {
  background: transparent; border: none; cursor: pointer; font-size: 12px;
  color: var(--text-subtle); padding: 0 2px; flex-shrink: 0;
}
.btn-copy-mini:hover { color: var(--accent); }

.proj-error { font-size: 12px; color: var(--danger); background: #ffebe9; border: 1px solid rgba(207,34,46,.15); border-radius: 6px; padding: 6px 10px; }

.proj-actions { display: flex; gap: 6px; border-top: 1px solid var(--border-color); padding-top: 12px; flex-wrap: wrap; }

/* Modal 弹窗 */
.modal-backdrop {
  position: fixed; inset: 0; z-index: 100;
  background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: #fff; border-radius: 10px; width: 480px; max-width: 90vw;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2); overflow: hidden;
}
.modal-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 16px 20px; border-bottom: 1px solid var(--border-color);
}
.modal-head h3 { margin: 0; font-size: 16px; color: var(--text-main); }
.btn-close { background: transparent; border: none; font-size: 16px; cursor: pointer; color: var(--text-muted); }
.modal-body { padding: 16px 20px; display: flex; flex-direction: column; gap: 12px; }
.modal-actions {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 12px 20px; border-top: 1px solid var(--border-color); background: var(--bg-sidebar);
}

.import-tabs { display: flex; border-bottom: 1px solid var(--border-color); margin-bottom: 8px; }
.tab-btn {
  padding: 8px 16px; background: transparent; border: none; font-size: 13px;
  color: var(--text-muted); cursor: pointer; border-bottom: 2px solid transparent;
}
.tab-btn.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }

.textarea {
  padding: 8px 10px; border: 1px solid var(--border-color); border-radius: 6px;
  font-family: Consolas, monospace; font-size: 12px; width: 100%; box-sizing: border-box;
  background: var(--bg-app); color: var(--text-main); resize: vertical;
}
.textarea:focus { border-color: var(--accent); outline: none; background: #fff; }

.form-item { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-muted); }

.btn { padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.45; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-stop { background: #fff; border-color: #ff8170; color: var(--danger); }
.btn-stop:hover { background: #ffebe9; }
.btn-small { padding: 4px 10px; font-size: 12px; }
.btn-danger-outline { background: #fff; border-color: #ff8170; color: var(--danger); margin-left: auto; }
.btn-danger-outline:hover:not(:disabled) { background: #ffebe9; }

/* 日志抽屉 */
.log-drawer {
  position: fixed; left: 0; right: 0; bottom: 0; height: 42%;
  background: #0f172a; color: #e2e8f0; border-radius: 10px 10px 0 0;
  display: flex; flex-direction: column; z-index: 50; overflow: hidden;
  box-shadow: 0 -6px 24px rgba(0, 0, 0, 0.28);
}
.drawer-backdrop { position: fixed; inset: 0; z-index: 40; background: rgba(0, 0, 0, 0.25); }
.log-drawer-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 10px 16px; background: #1e293b; border-bottom: 1px solid #334155;
}
.log-drawer-title { font-size: 13px; font-weight: 600; color: #f1f5f9; }
.log-drawer-tools { display: flex; align-items: center; gap: 8px; }
.auto-scroll { display: flex; align-items: center; gap: 4px; font-size: 12px; color: #94a3b8; cursor: pointer; }
.auto-scroll input { accent-color: var(--accent); }
.log-error { padding: 6px 16px; background: #450a0a; color: #fca5a5; font-size: 12px; }
.log-body {
  flex: 1; overflow-y: auto; padding: 12px 16px;
  font-family: Consolas, monospace; font-size: 12px; line-height: 1.55;
  user-select: text;
}
.log-line { white-space: pre-wrap; word-break: break-all; color: #cbd5e1; }
.log-warn { color: #fde047; }
.log-empty { color: #64748b; text-align: center; padding: 32px 0; }
</style>
