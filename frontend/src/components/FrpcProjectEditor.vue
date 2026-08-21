<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
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

// 扩展类型：编辑态追加 customDomains 的文本输入载体（显式定义，避免类派生推断退化）
interface EditableProxy {
  name: string
  type: string
  localIp: string
  localPort: number
  remotePort: number
  customDomains: string[]
  subdomain: string
  secretKey: string
  encryptTransport: boolean
  customDomainsText: string
}

function toEditable(r: ProxyRule): EditableProxy {
  return {
    name: r.name ?? '',
    type: r.type ?? 'tcp',
    localIp: r.localIp ?? '127.0.0.1',
    localPort: r.localPort ?? 0,
    remotePort: r.remotePort ?? 0,
    customDomains: [...(r.customDomains ?? [])],
    subdomain: r.subdomain ?? '',
    secretKey: r.secretKey ?? '',
    encryptTransport: r.encryptTransport ?? false,
    customDomainsText: (r.customDomains ?? []).join(', '),
  }
}

// 本地编辑副本（深拷贝，避免污染列表数据）
const draft = reactive<{ id: string; name: string; version: string; createdAt: string; updatedAt: string; server: Project['server']; proxies: EditableProxy[] }>({
  id: props.project?.id ?? '',
  name: props.project?.name ?? '',
  version: props.project?.version ?? '',
  createdAt: props.project?.createdAt ?? '',
  updatedAt: props.project?.updatedAt ?? '',
  server: {
    serverAddr: props.project?.server?.serverAddr ?? '',
    serverPort: props.project?.server?.serverPort ?? 7000,
    token: props.project?.server?.token ?? '',
    tlsEnable: props.project?.server?.tlsEnable ?? false,
    useEncryption: props.project?.server?.useEncryption ?? true,
    useCompression: props.project?.server?.useCompression ?? false,
    logLevel: props.project?.server?.logLevel ?? 'info',
  },
  proxies: props.project?.proxies?.length
    ? props.project.proxies.map(toEditable)
    : [toEditable({ name: '', type: 'tcp', localIp: '127.0.0.1', localPort: 8080, remotePort: 0, customDomains: [], subdomain: '', secretKey: '', encryptTransport: false } as ProxyRule)],
})

const tomlPreview = ref('')

const LOG_LEVELS = ['trace', 'debug', 'info', 'warn', 'error']

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

function newProxy(): EditableProxy {
  return toEditable({ name: '', type: 'tcp', localIp: '127.0.0.1', localPort: 8080, remotePort: 0, customDomains: [], subdomain: '', secretKey: '', encryptTransport: false } as ProxyRule)
}

function addProxy() {
  draft.proxies.push(newProxy())
  refreshPreview()
}

function removeProxy(i: number) {
  draft.proxies.splice(i, 1)
  refreshPreview()
}

function typeHint(type: string): string {
  switch (type) {
    case 'tcp': case 'udp': return '远程端口 (1-65535)'
    case 'http': case 'https': return '自定义域名逗号分隔'
    case 'stcp': case 'xtcp': return '共享密钥（对端配置 serverName 指向本规则）'
    default: return ''
  }
}

function toPayload(): Project {
  return {
    id: draft.id,
    name: draft.name.trim(),
    version: draft.version,
    createdAt: draft.createdAt,
    updatedAt: draft.updatedAt,
    server: { ...draft.server },
    proxies: draft.proxies.map(r => {
      const { customDomainsText, ...rest } = r
      void customDomainsText
      return {
        ...rest,
        customDomains: customDomainsText
          ? customDomainsText.split(',').map(s => s.trim()).filter(Boolean)
          : [],
      }
    }),
  }
}

let previewTimer: ReturnType<typeof setTimeout> | null = null
function refreshPreview() {
  if (previewTimer) clearTimeout(previewTimer)
  previewTimer = setTimeout(async () => {
    try {
      tomlPreview.value = await FrpcAPI.GenerateToml(toPayload() as any)
    } catch (e: any) {
      tomlPreview.value = `# 配置校验中: ${e?.message ?? e}`
    }
  }, 350)
}

watch(() => draft, refreshPreview, { deep: true })

function isDraftValid(): boolean {
  if (!draft.name.trim()) { errorMsg.value = '请填写项目名称'; return false }
  if (!draft.server.serverAddr.trim()) { errorMsg.value = '请填写服务端地址'; return false }
  for (const r of draft.proxies) {
    if (!r.name.trim()) { errorMsg.value = '存在未命名的代理规则'; return false }
    if (!r.localPort || r.localPort < 1 || r.localPort > 65535) { errorMsg.value = `规则 ${r.name} 的本地端口无效`; return false }
  }
  return true
}

function copyPreview() {
  navigator.clipboard.writeText(tomlPreview.value)
  showToast('TOML 已复制')
}

async function save() {
  if (!isDraftValid()) return
  saving.value = true
  errorMsg.value = ''
  try {
    const saved = await FrpcAPI.SaveProject(toPayload())
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
      <div>
        <h1>{{ draft.id ? '编辑项目' : '新建项目' }}</h1>
        <p class="subtitle">配置服务端连接与代理规则，右侧实时预览生成的 frpc TOML 配置。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

    <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

    <div class="editor-layout">
      <!-- 左列：表单 -->
      <div class="editor-main">
        <!-- 基本信息 -->
        <div class="card">
          <h3 class="card-title">基本信息</h3>
          <div class="form-grid">
            <label class="form-item">
              <span>项目名称 <em>*</em></span>
              <input v-model="draft.name" class="input" placeholder="如：生产联调" maxlength="30" />
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
          <h3 class="card-title">服务端连接</h3>
          <div class="form-grid">
            <label class="form-item">
              <span>服务器地址 <em>*</em></span>
              <input v-model="draft.server.serverAddr" class="input" placeholder="frp.example.com 或 IP" />
            </label>
            <label class="form-item">
              <span>服务器端口</span>
              <input v-model.number="draft.server.serverPort" type="number" class="input" min="1" max="65535" />
            </label>
            <label class="form-item">
              <span>鉴权令牌 (token)</span>
              <input v-model="draft.server.token" class="input" type="password" placeholder="与服务端一致" />
            </label>
            <label class="form-item">
              <span>日志级别</span>
              <select v-model="draft.server.logLevel" class="input">
                <option v-for="l in LOG_LEVELS" :key="l" :value="l">{{ l }}</option>
              </select>
            </label>
          </div>
          <div class="toggle-row">
            <label class="toggle-item">
              <input v-model="draft.server.tlsEnable" type="checkbox" />
              <span>启用 TLS 加密传输</span>
            </label>
            <label class="toggle-item">
              <input v-model="draft.server.useEncryption" type="checkbox" />
              <span>传输层加密</span>
            </label>
            <label class="toggle-item">
              <input v-model="draft.server.useCompression" type="checkbox" />
              <span>传输层压缩</span>
            </label>
          </div>
        </div>

        <!-- 代理规则 -->
        <div class="card">
          <div class="card-head">
            <h3 class="card-title">代理规则 ({{ draft.proxies.length }})</h3>
            <button class="btn btn-secondary btn-small" @click="addProxy">+ 添加规则</button>
          </div>

          <div class="proxy-list">
            <div v-for="(r, i) in draft.proxies" :key="i" class="proxy-row">
              <div class="proxy-head">
                <span class="proxy-index">#{{ i + 1 }}</span>
                <input v-model="r.name" class="input input-name" placeholder="规则名称" maxlength="30" />
                <select v-model="r.type" class="input input-type">
                  <option v-for="t in ['tcp','udp','http','https','stcp','xtcp']" :key="t" :value="t">{{ t.toUpperCase() }}</option>
                </select>
                <button class="btn-remove" title="删除该规则" @click="removeProxy(i)">✕</button>
              </div>
              <div class="proxy-fields">
                <label class="form-item n1"><span>本地 IP</span>
                  <input v-model="r.localIp" class="input" placeholder="127.0.0.1" /></label>
                <label class="form-item n1"><span>本地端口</span>
                  <input v-model.number="r.localPort" type="number" class="input" min="1" max="65535" /></label>
                <label class="form-item n2">
                  <span>{{ typeHint(r.type).replace(' (1-65535)', '') }}</span>
                  <template v-if="r.type === 'http' || r.type === 'https'">
                    <input v-model="r.customDomainsText" class="input" placeholder="dev.example.com, api.example.com" />
                  </template>
                  <template v-else-if="r.type === 'stcp' || r.type === 'xtcp'">
                    <input v-model="r.secretKey" class="input" placeholder="共享密钥" />
                  </template>
                  <template v-else>
                    <input v-model.number="r.remotePort" type="number" class="input" min="0" max="65535" placeholder="远程端口" />
                  </template>
                </label>
                <label class="form-item n1" v-if="r.type === 'http' || r.type === 'https'">
                  <span>子域名</span>
                  <input v-model="r.subdomain" class="input" placeholder="sub" />
                </label>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 右列：TOML 预览 -->
      <div class="editor-side">
        <div class="card preview-card">
          <div class="card-head">
            <h3 class="card-title">TOML 预览</h3>
            <button class="btn btn-secondary btn-small" @click="copyPreview">复制</button>
          </div>
          <pre class="toml-pre">{{ tomlPreview || '# 填写后自动生成配置...' }}</pre>
        </div>
      </div>
    </div>

    <div class="footer-actions">
      <button class="btn btn-secondary" @click="cancel">取消</button>
      <button class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存项目' }}</button>
    </div>
  </section>
</template>

<style scoped>
.editor-page { display: flex; flex-direction: column; gap: 16px; }
.header-row { display: flex; justify-content: space-between; align-items: center; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

.editor-layout { display: grid; grid-template-columns: 1fr 420px; gap: 16px; align-items: start; }
@media (max-width: 1100px) { .editor-layout { grid-template-columns: 1fr; } }

.editor-main { display: flex; flex-direction: column; gap: 14px; }
.editor-side { position: sticky; top: 16px; }

.card { background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 8px; padding: 16px; }
.card-title { font-size: 14px; font-weight: 600; color: var(--text-muted); margin: 0 0 12px; }
.card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.card-head .card-title { margin: 0; }

.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-item { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-muted); }
.form-item em { color: var(--danger); font-style: normal; }
.input {
  padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px;
  font-size: 13px; background: #fff; color: var(--text-main); width: 100%;
}
.input:focus { border-color: var(--accent); outline: none; }

.toggle-row { display: flex; gap: 20px; margin-top: 12px; }
.toggle-item { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-main); cursor: pointer; }
.toggle-item input { accent-color: var(--accent); }

/* 代理规则 */
.proxy-list { display: flex; flex-direction: column; gap: 12px; }
.proxy-row { border: 1px solid var(--border-color); border-radius: 8px; padding: 12px; background: var(--bg-app); }
.proxy-head { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.proxy-index { font-size: 11px; color: var(--text-subtle); font-weight: 700; }
.input-name { max-width: 180px; }
.input-type { max-width: 110px; }
.btn-remove { width: 24px; height: 24px; border: 1px solid var(--border-color); border-radius: 6px; background: #fff; color: var(--text-subtle); cursor: pointer; font-size: 11px; margin-left: auto; }
.btn-remove:hover { border-color: var(--danger); color: var(--danger); }
.proxy-fields { display: grid; grid-template-columns: 1fr 1fr 1.5fr 1fr; gap: 10px; }
.proxy-fields .n1 { grid-column: span 1; }
.proxy-fields .n2 { grid-column: span 2; }

/* TOML 预览 */
.toml-pre {
  margin: 0; padding: 12px; background: #0f172a; color: #e2e8f0; border-radius: 6px;
  font-family: Consolas, monospace; font-size: 12px; line-height: 1.55;
  max-height: 560px; overflow: auto; white-space: pre; user-select: text;
}

.btn { padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); margin-right: auto; }
.btn-secondary:hover { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; margin: 0; }

.footer-actions { display: flex; justify-content: flex-end; gap: 10px; }

@keyframes fadeIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }
</style>