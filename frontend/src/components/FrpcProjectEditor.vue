<script setup lang="ts">
import { ref, reactive, watch, nextTick } from 'vue'
import * as FrpcAPI from '../../bindings/hubkit/internal/modules/frpc/frpcservice'
import type { Project, ProxyRule } from '../../bindings/hubkit/internal/domain/models'

const props = defineProps<{
  project: Project | null
  installedVersions: string[]
}>()
const emit = defineEmits<{ (e: 'saved', p: Project): void; (e: 'cancel'): void }>()

const toastMsg = ref('')
const saving = ref(false)
const errorMsg = ref('')

// 编辑模式：form (表单) | toml (源码)
const editorMode = ref<'form' | 'toml'>('form')
const rawTomlContent = ref('')
const tomlParseError = ref('')

// 扩展类型：编辑态包含完整的高级属性与交互控制
interface EditableProxy {
  name: string
  type: string
  role: string // "" | "server" | "visitor"
  localIp: string
  localPort: number
  remotePort: number
  customDomains: string[]
  subdomain: string
  secretKey: string
  serverName: string
  bindAddr: string
  bindPort: number
  hostHeaderRewrite: string
  proxyProtocolVersion: string
  bandwidthLimit: string
  encryptTransport: boolean
  customDomainsText: string
  showAdvanced?: boolean
}

function toEditable(r: ProxyRule): EditableProxy {
  return {
    name: r.name ?? '',
    type: r.type ?? 'tcp',
    role: r.role ?? 'server',
    localIp: r.localIp ?? '127.0.0.1',
    localPort: r.localPort ?? 0,
    remotePort: r.remotePort ?? 0,
    customDomains: [...(r.customDomains ?? [])],
    subdomain: r.subdomain ?? '',
    secretKey: r.secretKey ?? '',
    serverName: r.serverName ?? '',
    bindAddr: r.bindAddr ?? '127.0.0.1',
    bindPort: r.bindPort ?? 0,
    hostHeaderRewrite: r.hostHeaderRewrite ?? '',
    proxyProtocolVersion: r.proxyProtocolVersion ?? '',
    bandwidthLimit: r.bandwidthLimit ?? '',
    encryptTransport: r.encryptTransport ?? false,
    customDomainsText: (r.customDomains ?? []).join(', '),
    showAdvanced: Boolean(r.hostHeaderRewrite || r.proxyProtocolVersion || r.bandwidthLimit),
  }
}

// 本地编辑副本
const draft = reactive<{
  id: string
  name: string
  version: string
  createdAt: string
  updatedAt: string
  server: Project['server']
  proxies: EditableProxy[]
}>({
  id: props.project?.id ?? '',
  name: props.project?.name ?? '',
  version: props.project?.version ?? '',
  createdAt: props.project?.createdAt ?? '',
  updatedAt: props.project?.updatedAt ?? '',
  server: {
    serverAddr: props.project?.server?.serverAddr ?? '',
    serverPort: props.project?.server?.serverPort ?? 7000,
    token: props.project?.server?.token ?? '',
    protocol: props.project?.server?.protocol ?? 'tcp',
    proxyUrl: props.project?.server?.proxyUrl ?? '',
    tlsEnable: props.project?.server?.tlsEnable ?? false,
    useEncryption: props.project?.server?.useEncryption ?? true,
    useCompression: props.project?.server?.useCompression ?? false,
    logLevel: props.project?.server?.logLevel ?? 'info',
  },
  proxies: props.project?.proxies?.length
    ? props.project.proxies.map(toEditable)
    : [toEditable({
        name: '',
        type: 'tcp',
        role: 'server',
        localIp: '127.0.0.1',
        localPort: 8080,
        remotePort: 0,
        customDomains: [],
        subdomain: '',
        secretKey: '',
        serverName: '',
        bindAddr: '127.0.0.1',
        bindPort: 0,
        hostHeaderRewrite: '',
        proxyProtocolVersion: '',
        bandwidthLimit: '',
        encryptTransport: false,
      } as ProxyRule)],
})

const showServerAdvanced = ref(Boolean(draft.server.protocol && draft.server.protocol !== 'tcp' || draft.server.proxyUrl))
const tomlPreview = ref('')
const LOG_LEVELS = ['trace', 'debug', 'info', 'warn', 'error']
const PROTOCOLS = [
  { label: 'TCP (标准默认)', value: 'tcp' },
  { label: 'KCP (UDP 抗丢包)', value: 'kcp' },
  { label: 'QUIC (极速 UDP)', value: 'quic' },
  { label: 'WebSocket (穿透反代)', value: 'websocket' },
  { label: 'WSS (TLS WebSocket)', value: 'wss' },
]

// 批量端口添加 Modal 状态
const batchModalOpen = ref(false)
const batchForm = reactive({
  prefix: 'batch',
  type: 'tcp',
  localIp: '127.0.0.1',
  localPorts: '',  // 支持 8080-8085 或 8080,8081,8082
  remotePorts: '', // 支持 9080-9085 或 9080,9081,9082
})
const batchError = ref('')

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

function newProxy(): EditableProxy {
  return toEditable({
    name: '',
    type: 'tcp',
    role: 'server',
    localIp: '127.0.0.1',
    localPort: 8080,
    remotePort: 0,
    customDomains: [],
    subdomain: '',
    secretKey: '',
    serverName: '',
    bindAddr: '127.0.0.1',
    bindPort: 0,
    hostHeaderRewrite: '',
    proxyProtocolVersion: '',
    bandwidthLimit: '',
    encryptTransport: false,
  } as ProxyRule)
}

function addProxy() {
  draft.proxies.push(newProxy())
  refreshPreview()
}

function removeProxy(i: number) {
  draft.proxies.splice(i, 1)
  refreshPreview()
}

function openBatchModal() {
  batchError.value = ''
  batchForm.prefix = draft.name ? `${draft.name}-port` : 'proxy'
  batchForm.localPorts = ''
  batchForm.remotePorts = ''
  batchModalOpen.value = true
}

// 解析端口列表格式：支持逗号分隔与区间，如 8000-8003,8008 -> [8000, 8001, 8002, 8003, 8008]
function parsePortRange(str: string): number[] {
  const ports: number[] = []
  const segments = str.split(',').map(s => s.trim()).filter(Boolean)
  for (const seg of segments) {
    if (seg.includes('-')) {
      const parts = seg.split('-').map(s => parseInt(s.trim(), 10))
      if (parts.length === 2 && !isNaN(parts[0]) && !isNaN(parts[1])) {
        const start = Math.min(parts[0], parts[1])
        const end = Math.max(parts[0], parts[1])
        if (end - start > 100) {
          throw new Error(`端口区间 ${seg} 跨度超过 100，防止意外生成过多规则`)
        }
        for (let p = start; p <= end; p++) {
          if (p >= 1 && p <= 65535) ports.push(p)
        }
      }
    } else {
      const p = parseInt(seg, 10)
      if (!isNaN(p) && p >= 1 && p <= 65535) {
        ports.push(p)
      }
    }
  }
  return ports
}

function applyBatchPorts() {
  batchError.value = ''
  let lPorts: number[] = []
  let rPorts: number[] = []
  try {
    lPorts = parsePortRange(batchForm.localPorts)
    if (!lPorts.length) throw new Error('请填写有效的本地端口或端口范围')

    if (batchForm.type === 'tcp' || batchForm.type === 'udp') {
      rPorts = parsePortRange(batchForm.remotePorts)
      if (rPorts.length !== lPorts.length) {
        throw new Error(`本地端口数量 (${lPorts.length}) 与 远程端口数量 (${rPorts.length}) 不一致`)
      }
    }
  } catch (err: any) {
    batchError.value = err.message || String(err)
    return
  }

  // 批量生成规则
  const added: EditableProxy[] = []
  for (let i = 0; i < lPorts.length; i++) {
    const lp = lPorts[i]
    const rp = rPorts[i] || 0
    const ruleName = `${batchForm.prefix.trim() || 'batch'}_${lp}`
    added.push({
      name: ruleName,
      type: batchForm.type,
      role: 'server',
      localIp: batchForm.localIp.trim() || '127.0.0.1',
      localPort: lp,
      remotePort: rp,
      customDomains: [],
      subdomain: '',
      secretKey: '',
      serverName: '',
      bindAddr: '127.0.0.1',
      bindPort: 0,
      hostHeaderRewrite: '',
      proxyProtocolVersion: '',
      bandwidthLimit: '',
      encryptTransport: false,
      customDomainsText: '',
      showAdvanced: false,
    })
  }

  // 如果原本只有一个空的默认规则，替换它
  if (draft.proxies.length === 1 && !draft.proxies[0].name) {
    draft.proxies = added
  } else {
    draft.proxies.push(...added)
  }

  batchModalOpen.value = false
  refreshPreview()
  showToast(`已批量添加 ${added.length} 条端口规则`)
}

function onTypeChange(r: EditableProxy) {
  if (r.type === 'stcp-visitor' || r.type === 'xtcp-visitor') {
    r.role = 'visitor'
    if (!r.bindPort) r.bindPort = r.localPort || 9000
    if (!r.bindAddr) r.bindAddr = '127.0.0.1'
  } else {
    r.role = 'server'
  }
  refreshPreview()
}

function getRuleSelectType(r: EditableProxy): string {
  if (r.role === 'visitor') {
    return r.type === 'xtcp' ? 'xtcp-visitor' : 'stcp-visitor'
  }
  return r.type
}

function setRuleSelectType(r: EditableProxy, val: string) {
  if (val === 'stcp-visitor') {
    r.type = 'stcp'
    r.role = 'visitor'
    if (!r.bindPort) r.bindPort = r.localPort || 9000
    if (!r.bindAddr) r.bindAddr = '127.0.0.1'
  } else if (val === 'xtcp-visitor') {
    r.type = 'xtcp'
    r.role = 'visitor'
    if (!r.bindPort) r.bindPort = r.localPort || 9000
    if (!r.bindAddr) r.bindAddr = '127.0.0.1'
  } else {
    r.type = val
    r.role = 'server'
  }
  refreshPreview()
}

function toPayload(): Project {
  return {
    id: draft.id,
    name: draft.name.trim(),
    version: draft.version,
    createdAt: draft.createdAt,
    updatedAt: draft.updatedAt,
    server: {
      serverAddr: draft.server.serverAddr.trim(),
      serverPort: draft.server.serverPort,
      token: draft.server.token,
      protocol: draft.server.protocol || 'tcp',
      proxyUrl: draft.server.proxyUrl?.trim() || '',
      tlsEnable: draft.server.tlsEnable,
      useEncryption: draft.server.useEncryption,
      useCompression: draft.server.useCompression,
      logLevel: draft.server.logLevel || 'info',
    },
    proxies: draft.proxies.map(r => {
      const { customDomainsText, showAdvanced, ...rest } = r
      void customDomainsText
      void showAdvanced
      return {
        ...rest,
        name: r.name.trim(),
        role: r.role || 'server',
        localIp: r.localIp?.trim() || '127.0.0.1',
        serverName: r.serverName?.trim() || '',
        bindAddr: r.bindAddr?.trim() || '127.0.0.1',
        hostHeaderRewrite: r.hostHeaderRewrite?.trim() || '',
        proxyProtocolVersion: r.proxyProtocolVersion?.trim() || '',
        bandwidthLimit: r.bandwidthLimit?.trim() || '',
        customDomains: customDomainsText
          ? customDomainsText.split(',').map(s => s.trim()).filter(Boolean)
          : [],
      }
    }),
  }
}

let previewTimer: ReturnType<typeof setTimeout> | null = null
function refreshPreview() {
  if (editorMode.value === 'toml') return
  if (previewTimer) clearTimeout(previewTimer)
  previewTimer = setTimeout(async () => {
    try {
      tomlPreview.value = await FrpcAPI.GenerateToml(toPayload() as any)
    } catch (e: any) {
      tomlPreview.value = `# 配置校验中: ${e?.message ?? e}`
    }
  }, 250)
}

watch(() => draft, refreshPreview, { deep: true, immediate: true })

async function switchMode(target: 'form' | 'toml') {
  if (target === editorMode.value) return
  if (target === 'toml') {
    // 表单 -> 源码模式：生成 TOML
    try {
      rawTomlContent.value = await FrpcAPI.GenerateToml(toPayload() as any)
      tomlParseError.value = ''
      editorMode.value = 'toml'
    } catch (err: any) {
      errorMsg.value = `切换至源码模式失败（当前表单有误）: ${err?.message || err}`
    }
  } else {
    // 源码 -> 表单模式：解析 TOML 还原
    try {
      const parsed = await FrpcAPI.ParseToml(rawTomlContent.value)
      // 更新 draft
      draft.server = {
        serverAddr: parsed.server.serverAddr || '',
        serverPort: parsed.server.serverPort || 7000,
        token: parsed.server.token || '',
        protocol: parsed.server.protocol || 'tcp',
        proxyUrl: parsed.server.proxyUrl || '',
        tlsEnable: parsed.server.tlsEnable ?? false,
        useEncryption: parsed.server.useEncryption ?? true,
        useCompression: parsed.server.useCompression ?? false,
        logLevel: parsed.server.logLevel || 'info',
      }
      draft.proxies = (parsed.proxies || []).map(toEditable)
      tomlParseError.value = ''
      editorMode.value = 'form'
      refreshPreview()
    } catch (err: any) {
      tomlParseError.value = `TOML 语法错误，无法切回表单: ${err?.message || err}`
    }
  }
}

function isDraftValid(): boolean {
  if (!draft.name.trim()) { errorMsg.value = '请填写项目名称'; return false }
  if (!draft.server.serverAddr.trim()) { errorMsg.value = '请填写服务端地址'; return false }
  for (const r of draft.proxies) {
    if (!r.name.trim()) { errorMsg.value = '存在未命名的规则'; return false }
    if (r.role === 'visitor') {
      if (!r.serverName.trim()) { errorMsg.value = `访客规则「${r.name}」必须指定目标服务端服务名 (serverName)`; return false }
      if (!r.bindPort || r.bindPort < 1 || r.bindPort > 65535) { errorMsg.value = `访客规则「${r.name}」的本地监听端口 (bindPort) 无效`; return false }
    } else {
      if (!r.localPort || r.localPort < 1 || r.localPort > 65535) { errorMsg.value = `规则「${r.name}」的本地端口无效`; return false }
      if ((r.type === 'tcp' || r.type === 'udp') && (!r.remotePort || r.remotePort < 1 || r.remotePort > 65535)) {
        errorMsg.value = `规则「${r.name}」的公网远程端口 (remotePort) 无效`; return false
      }
    }
  }
  return true
}

function copyPreview() {
  const content = editorMode.value === 'toml' ? rawTomlContent.value : tomlPreview.value
  navigator.clipboard.writeText(content)
  showToast('TOML 已复制')
}

// 导出分享链接 (frp://<base64>)
function copyShareLink() {
  const p = toPayload()
  const sharePayload = {
    name: p.name,
    server: p.server,
    proxies: p.proxies,
  }
  const jsonStr = JSON.stringify(sharePayload)
  const b64 = btoa(encodeURIComponent(jsonStr).replace(/%([0-9A-F]{2})/g, (_, p1) => String.fromCharCode(parseInt(p1, 16))))
  const link = `frp://${b64}`
  navigator.clipboard.writeText(link)
  showToast('分享链接已复制 (frp://...)')
}

async function save() {
  errorMsg.value = ''
  saving.value = true

  try {
    let payload: Project
    if (editorMode.value === 'toml') {
      // 源码模式下保存：先校验并解析
      const parsed = await FrpcAPI.ParseToml(rawTomlContent.value)
      if (!draft.name.trim()) {
        errorMsg.value = '请填写顶部项目名称'
        saving.value = false
        return
      }
      payload = {
        ...parsed,
        id: draft.id,
        name: draft.name.trim(),
        version: draft.version,
        createdAt: draft.createdAt,
        updatedAt: draft.updatedAt,
      }
    } else {
      if (!isDraftValid()) {
        saving.value = false
        return
      }
      payload = toPayload()
    }

    const saved = await FrpcAPI.SaveProject(payload)
    showToast('项目已保存')
    emit('saved', saved)
  } catch (e: any) {
    errorMsg.value = `保存失败: ${e?.message ?? e}`
  } finally {
    saving.value = false
  }
}

function cancel() { emit('cancel') }
</script>

<template>
  <section class="page editor-page">
    <div class="header-row">
      <div class="title-with-mode">
        <div>
          <h1>{{ draft.id ? '编辑项目' : '新建项目' }}</h1>
          <p class="subtitle">全面支持 FRP v0.53+ TOML 标准规范、STCP/XTCP 访客端模式与高级传输优化。</p>
        </div>
        <!-- 模式切换 Tabs -->
        <div class="mode-tabs">
          <button
            class="mode-btn"
            :class="{ active: editorMode === 'form' }"
            @click="switchMode('form')"
          >📝 可视化表单</button>
          <button
            class="mode-btn"
            :class="{ active: editorMode === 'toml' }"
            @click="switchMode('toml')"
          >⚙️ TOML 源码模式</button>
        </div>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary btn-small" @click="copyShareLink">⚡ 分享链接</button>
        <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
      </div>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>
    <div v-if="tomlParseError" class="error-box">{{ tomlParseError }}</div>

    <!-- 模式 1：可视化表单布局 -->
    <div v-if="editorMode === 'form'" class="editor-layout">
      <!-- 左列：表单 -->
      <div class="editor-main">
        <!-- 基本信息 -->
        <div class="card">
          <h3 class="card-title">基本信息</h3>
          <div class="form-grid">
            <label class="form-item">
              <span>项目名称 <em>*</em></span>
              <input v-model="draft.name" class="input" placeholder="如：联调环境 / 远程桌面" maxlength="30" />
            </label>
            <label class="form-item">
              <span>绑定 frp 版本</span>
              <select v-model="draft.version" class="input">
                <option value="">未绑定（启动时选择）</option>
                <option v-for="v in installedVersions" :key="v" :value="v">{{ v }}</option>
              </select>
            </label>
          </div>
        </div>

        <!-- 服务端连接 -->
        <div class="card">
          <div class="card-head">
            <h3 class="card-title">服务端连接</h3>
            <button
              class="btn-text"
              @click="showServerAdvanced = !showServerAdvanced"
            >{{ showServerAdvanced ? '▲ 收起高级连接参数' : '▼ 展开高级连接参数 (KCP/QUIC/跳板)' }}</button>
          </div>
          <div class="form-grid">
            <label class="form-item">
              <span>服务器地址 <em>*</em></span>
              <input v-model="draft.server.serverAddr" class="input" placeholder="frp.example.com 或 1.2.3.4" />
            </label>
            <label class="form-item">
              <span>服务器端口</span>
              <input v-model.number="draft.server.serverPort" type="number" class="input" min="1" max="65535" />
            </label>
            <label class="form-item">
              <span>鉴权令牌 (Token)</span>
              <input v-model="draft.server.token" class="input" type="password" placeholder="与 frps.toml 的 token 一致" />
            </label>
            <label class="form-item">
              <span>日志级别</span>
              <select v-model="draft.server.logLevel" class="input">
                <option v-for="l in LOG_LEVELS" :key="l" :value="l">{{ l }}</option>
              </select>
            </label>
          </div>

          <!-- 高级连接选项 -->
          <div v-if="showServerAdvanced" class="advanced-box">
            <div class="form-grid">
              <label class="form-item">
                <span>底层传输协议 (transport.protocol)</span>
                <select v-model="draft.server.protocol" class="input">
                  <option v-for="p in PROTOCOLS" :key="p.value" :value="p.value">{{ p.label }}</option>
                </select>
              </label>
              <label class="form-item">
                <span>上游连接代理 (transport.proxyURL)</span>
                <input v-model="draft.server.proxyUrl" class="input" placeholder="socks5://127.0.0.1:1080 或 http://..." />
              </label>
            </div>
          </div>

          <div class="toggle-row">
            <label class="toggle-item">
              <input v-model="draft.server.tlsEnable" type="checkbox" />
              <span>启用 TLS 传输层加密</span>
            </label>
            <label class="toggle-item">
              <input v-model="draft.server.useEncryption" type="checkbox" />
              <span>报文加密 (useEncryption)</span>
            </label>
            <label class="toggle-item">
              <input v-model="draft.server.useCompression" type="checkbox" />
              <span>报文压缩 (useCompression)</span>
            </label>
          </div>
        </div>

        <!-- 代理与访客规则 -->
        <div class="card">
          <div class="card-head">
            <h3 class="card-title">穿透规则列表 ({{ draft.proxies.length }})</h3>
            <div class="card-head-tools">
              <button class="btn btn-secondary btn-small" @click="openBatchModal">⚡ 批量端口导入</button>
              <button class="btn btn-secondary btn-small" @click="addProxy">+ 添加规则</button>
            </div>
          </div>

          <div class="proxy-list">
            <div v-for="(r, i) in draft.proxies" :key="i" class="proxy-row" :class="{ 'is-visitor': r.role === 'visitor' }">
              <div class="proxy-head">
                <span class="proxy-index">#{{ i + 1 }}</span>
                <input v-model="r.name" class="input input-name" placeholder="规则唯一标识 (name)" maxlength="30" />

                <select
                  :value="getRuleSelectType(r)"
                  @change="setRuleSelectType(r, ($event.target as HTMLSelectElement).value)"
                  class="input input-type"
                >
                  <optgroup label="被访端 / 普通代理 (Proxies)">
                    <option value="tcp">TCP 端口映射</option>
                    <option value="udp">UDP 端口映射</option>
                    <option value="http">HTTP 网站</option>
                    <option value="https">HTTPS 网站</option>
                    <option value="stcp">STCP 安全点对点服务</option>
                    <option value="xtcp">XTCP P2P 直连服务</option>
                  </optgroup>
                  <optgroup label="访客端 / 对端接入 (Visitors)">
                    <option value="stcp-visitor">STCP 访客端 (Visitor 接收端)</option>
                    <option value="xtcp-visitor">XTCP 访客端 (Visitor 接收端)</option>
                  </optgroup>
                </select>

                <button
                  class="btn-text btn-adv-toggle"
                  @click="r.showAdvanced = !r.showAdvanced"
                  title="展开/收起 Host重写、限速、ProxyProtocol"
                >{{ r.showAdvanced ? '▲ 简略' : '⚙️ 高级' }}</button>
                <button class="btn-remove" title="删除该规则" @click="removeProxy(i)">✕</button>
              </div>

              <!-- 访客端 (Visitor) 专属表单 -->
              <template v-if="r.role === 'visitor'">
                <div class="proxy-fields visitor-fields">
                  <label class="form-item">
                    <span>目标服务名 (serverName) <em>*</em></span>
                    <input v-model="r.serverName" class="input" placeholder="对端配置的规则 name" />
                  </label>
                  <label class="form-item">
                    <span>访问密钥 (secretKey) <em>*</em></span>
                    <input v-model="r.secretKey" class="input" type="password" placeholder="需与对端一致" />
                  </label>
                  <label class="form-item">
                    <span>本地监听 IP (bindAddr)</span>
                    <input v-model="r.bindAddr" class="input" placeholder="127.0.0.1" />
                  </label>
                  <label class="form-item">
                    <span>本地监听端口 (bindPort) <em>*</em></span>
                    <input v-model.number="r.bindPort" type="number" class="input" min="1" max="65535" placeholder="如 9000" />
                  </label>
                </div>
              </template>

              <!-- 普通代理端 (Server Proxy) 表单 -->
              <template v-else>
                <div class="proxy-fields">
                  <label class="form-item n1">
                    <span>本地 IP</span>
                    <input v-model="r.localIp" class="input" placeholder="127.0.0.1" />
                  </label>
                  <label class="form-item n1">
                    <span>本地端口 <em>*</em></span>
                    <input v-model.number="r.localPort" type="number" class="input" min="1" max="65535" />
                  </label>

                  <!-- STCP/XTCP 服务端 -->
                  <template v-if="r.type === 'stcp' || r.type === 'xtcp'">
                    <label class="form-item n2">
                      <span>安全共享密钥 (secretKey) <em>*</em></span>
                      <input v-model="r.secretKey" class="input" placeholder="访客端连接时需校验此密钥" />
                    </label>
                  </template>

                  <!-- HTTP/HTTPS 域名配置 -->
                  <template v-else-if="r.type === 'http' || r.type === 'https'">
                    <label class="form-item n2">
                      <span>自定义域名 (customDomains，逗号分隔)</span>
                      <input v-model="r.customDomainsText" class="input" placeholder="dev.example.com, test.example.com" />
                    </label>
                    <label class="form-item n1">
                      <span>二级子域名</span>
                      <input v-model="r.subdomain" class="input" placeholder="如 web" />
                    </label>
                  </template>

                  <!-- TCP/UDP 远程端口 -->
                  <template v-else>
                    <label class="form-item n2">
                      <span>公网远程端口 (remotePort) <em>*</em></span>
                      <input v-model.number="r.remotePort" type="number" class="input" min="1" max="65535" placeholder="公网暴露端口" />
                    </label>
                  </template>
                </div>
              </template>

              <!-- 单条规则的高级参数抽屉 -->
              <div v-if="r.showAdvanced" class="rule-advanced-box">
                <div class="form-grid">
                  <label class="form-item" v-if="r.type === 'http' || r.type === 'https'">
                    <span>Host 请求头重写 (hostHeaderRewrite)</span>
                    <input v-model="r.hostHeaderRewrite" class="input" placeholder="如 localhost:3000 或 dev.local" />
                  </label>
                  <label class="form-item" v-if="r.role !== 'visitor'">
                    <span>真实 IP 透传 (proxyProtocolVersion)</span>
                    <select v-model="r.proxyProtocolVersion" class="input">
                      <option value="">关闭 (默认)</option>
                      <option value="v1">v1 (文本头)</option>
                      <option value="v2">v2 (二进制头)</option>
                    </select>
                  </label>
                  <label class="form-item" v-if="r.role !== 'visitor'">
                    <span>带宽限速 (bandwidthLimit)</span>
                    <input v-model="r.bandwidthLimit" class="input" placeholder="如 1MB, 500KB" />
                  </label>
                  <label class="toggle-item inline-check" v-if="r.role !== 'visitor'">
                    <input v-model="r.encryptTransport" type="checkbox" />
                    <span>独立启用传输加密 (覆盖全局)</span>
                  </label>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 右列：TOML 实时预览 -->
      <div class="editor-side">
        <div class="card preview-card">
          <div class="card-head">
            <h3 class="card-title">TOML 实时预览</h3>
            <button class="btn btn-secondary btn-small" @click="copyPreview">复制</button>
          </div>
          <pre class="toml-pre">{{ tomlPreview || '# 填写后自动生成配置...' }}</pre>
        </div>
      </div>
    </div>

    <!-- 模式 2：TOML 源码直接编辑模式 -->
    <div v-else class="toml-editor-layout">
      <div class="card toml-edit-card">
        <div class="card-head">
          <div>
            <h3 class="card-title">TOML 源码直接编辑</h3>
            <p class="card-desc">高级用户可在此直接编写、粘贴标准 frpc.toml 文件内容。系统将自动进行无损语法校验与语义映射。</p>
          </div>
          <button class="btn btn-secondary btn-small" @click="copyPreview">复制全文</button>
        </div>
        <textarea
          v-model="rawTomlContent"
          class="raw-toml-textarea"
          spellcheck="false"
          placeholder="serverAddr = &quot;x.x.x.x&quot;&#10;serverPort = 7000&#10;..."
        ></textarea>
      </div>
    </div>

    <div class="footer-actions">
      <button class="btn btn-secondary" @click="cancel">取消</button>
      <button class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存项目' }}</button>
    </div>

    <!-- 批量端口生成 Modal -->
    <div v-if="batchModalOpen" class="modal-backdrop" @click.self="batchModalOpen = false">
      <div class="modal-card">
        <div class="modal-head">
          <h3>批量生成端口规则</h3>
          <button class="btn-close" @click="batchModalOpen = false">✕</button>
        </div>
        <div v-if="batchError" class="error-box" style="margin-bottom: 12px;">{{ batchError }}</div>
        <div class="modal-body">
          <label class="form-item">
            <span>规则名称前缀</span>
            <input v-model="batchForm.prefix" class="input" placeholder="batch" />
          </label>
          <div class="form-grid">
            <label class="form-item">
              <span>协议类型</span>
              <select v-model="batchForm.type" class="input">
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
            </label>
            <label class="form-item">
              <span>本地 IP</span>
              <input v-model="batchForm.localIp" class="input" placeholder="127.0.0.1" />
            </label>
          </div>
          <label class="form-item">
            <span>本地端口范围 (支持区间或逗号)</span>
            <input v-model="batchForm.localPorts" class="input" placeholder="例如：8080-8085 或 8080,8081,8082" />
          </label>
          <label class="form-item">
            <span>远程端口范围 (与本地端口数量须一致)</span>
            <input v-model="batchForm.remotePorts" class="input" placeholder="例如：9080-9085 或 9080,9081,9082" />
          </label>
        </div>
        <div class="modal-actions">
          <button class="btn btn-secondary" @click="batchModalOpen = false">取消</button>
          <button class="btn btn-primary" @click="applyBatchPorts">确认生成并添加</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.editor-page { display: flex; flex-direction: column; gap: 16px; }
.header-row { display: flex; justify-content: space-between; align-items: center; }
.title-with-mode { display: flex; align-items: center; gap: 24px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.header-actions { display: flex; align-items: center; gap: 12px; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

/* 模式切换 Tabs */
.mode-tabs {
  display: flex; background: var(--bg-sidebar); border: 1px solid var(--border-color);
  border-radius: 8px; padding: 3px; gap: 2px;
}
.mode-btn {
  border: none; background: transparent; padding: 6px 14px; font-size: 13px;
  color: var(--text-muted); border-radius: 6px; cursor: pointer; font-weight: 500;
  transition: all 0.15s ease;
}
.mode-btn.active { background: #fff; color: var(--accent); font-weight: 600; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }

.editor-layout { display: grid; grid-template-columns: 1fr 420px; gap: 16px; align-items: start; }
@media (max-width: 1100px) { .editor-layout { grid-template-columns: 1fr; } }

.editor-main { display: flex; flex-direction: column; gap: 14px; }
.editor-side { position: sticky; top: 16px; }

.card { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; padding: 16px; }
.card-title { font-size: 14px; font-weight: 600; color: var(--text-muted); margin: 0 0 12px; }
.card-desc { font-size: 12px; color: var(--text-subtle); margin: 4px 0 0; }
.card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.card-head .card-title { margin: 0; }
.card-head-tools { display: flex; gap: 8px; }

.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-item { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-muted); }
.form-item em { color: var(--danger); font-style: normal; }
.input {
  padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px;
  font-size: 13px; background: #fff; color: var(--text-main); width: 100%;
  box-sizing: border-box;
}
.input:focus { border-color: var(--accent); outline: none; }

.btn-text { background: transparent; border: none; font-size: 12px; color: var(--accent); cursor: pointer; padding: 2px 4px; }
.btn-text:hover { text-decoration: underline; }
.btn-adv-toggle { font-size: 12px; }

.advanced-box {
  background: var(--bg-app); border: 1px dashed var(--border-color);
  border-radius: 6px; padding: 12px; margin-top: 12px;
}
.rule-advanced-box {
  background: #fff; border: 1px solid var(--border-color);
  border-radius: 6px; padding: 10px 12px; margin-top: 10px;
}

.toggle-row { display: flex; gap: 20px; margin-top: 12px; flex-wrap: wrap; }
.toggle-item { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-main); cursor: pointer; }
.toggle-item input { accent-color: var(--accent); }
.inline-check { margin-top: 20px; }

/* 代理规则 */
.proxy-list { display: flex; flex-direction: column; gap: 12px; }
.proxy-row { border: 1px solid var(--border-color); border-radius: 8px; padding: 12px; background: var(--bg-app); }
.proxy-row.is-visitor { border-left: 3px solid #8250df; background: #faf9ff; }
.proxy-head { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.proxy-index { font-size: 11px; color: var(--text-subtle); font-weight: 700; }
.input-name { max-width: 180px; }
.input-type { max-width: 210px; }
.btn-remove { width: 24px; height: 24px; border: 1px solid var(--border-color); border-radius: 6px; background: #fff; color: var(--text-subtle); cursor: pointer; font-size: 11px; margin-left: auto; }
.btn-remove:hover { border-color: var(--danger); color: var(--danger); }

.proxy-fields { display: grid; grid-template-columns: 1fr 1fr 1.5fr 1fr; gap: 10px; }
.proxy-fields .n1 { grid-column: span 1; }
.proxy-fields .n2 { grid-column: span 2; }
.visitor-fields { display: grid; grid-template-columns: 1.2fr 1.2fr 1fr 1fr; gap: 10px; }

/* TOML 预览 */
.toml-pre {
  margin: 0; padding: 12px; background: #0f172a; color: #e2e8f0; border-radius: 6px;
  font-family: Consolas, monospace; font-size: 12px; line-height: 1.55;
  max-height: 560px; overflow: auto; white-space: pre; user-select: text;
}

/* 源码模式编辑器 */
.toml-editor-layout { width: 100%; }
.toml-edit-card { display: flex; flex-direction: column; height: 620px; }
.raw-toml-textarea {
  flex: 1; margin-top: 10px; padding: 14px; background: #0f172a; color: #e2e8f0;
  border-radius: 6px; font-family: Consolas, monospace; font-size: 13px; line-height: 1.6;
  border: 1px solid var(--border-color); resize: none; outline: none; width: 100%; box-sizing: border-box;
}
.raw-toml-textarea:focus { border-color: var(--accent); }

.footer-actions { display: flex; justify-content: flex-end; gap: 12px; padding: 12px 0 24px; }

/* Modal 弹窗 */
.modal-backdrop {
  position: fixed; inset: 0; z-index: 100;
  background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: #fff; border-radius: 10px; width: 460px; max-width: 90vw;
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

.btn { padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.45; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
</style>
