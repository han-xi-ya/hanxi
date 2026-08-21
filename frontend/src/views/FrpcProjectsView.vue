<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as FrpcAPI from '../../bindings/hubkit/internal/modules/frpc/frpcservice'
import type { Project } from '../../bindings/hubkit/internal/domain/models'
import FrpcProjectEditor from '../components/FrpcProjectEditor.vue'

const projects = ref<Project[]>([])
const loading = ref(false)
const errorMsg = ref('')
const toastMsg = ref('')

// 编辑器状态：null = 关闭；{project: null} = 新建；{project} = 编辑
const editorOpen = ref(false)
const editingProject = ref<Project | null>(null)
const installedVersions = ref<string[]>([])

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

async function loadProjects() {
  loading.value = true
  errorMsg.value = ''
  try {
    projects.value = (await FrpcAPI.ListProjects()) ?? []
  } catch (e: any) {
    errorMsg.value = `加载项目失败: ${e?.message ?? e}`
  } finally {
    loading.value = false
  }
}

async function loadInstalledVersions() {
  try {
    const list = (await FrpcAPI.ListInstalledVersions()) ?? []
    installedVersions.value = list.map(v => v.version)
  } catch (e) {
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

async function onSaved() {
  editorOpen.value = false
  await loadProjects()
}

async function deleteProject(p: Project) {
  if (!window.confirm(`确定删除项目「${p.name}」？\n配置将被永久移除。`)) return
  try {
    await FrpcAPI.DeleteProject(p.id)
    showToast(`已删除「${p.name}」`)
    await loadProjects()
  } catch (e: any) {
    showToast(`删除失败: ${e?.message ?? e}`)
  }
}

function typeCount(p: Project): string {
  const map = new Map<string, number>()
  for (const r of p.proxies ?? []) map.set(r.type, (map.get(r.type) ?? 0) + 1)
  return [...map.entries()].map(([t, n]) => `${t}×${n}`).join(' · ')
}

onMounted(async () => {
  await Promise.all([loadProjects(), loadInstalledVersions()])
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
          <h1>frpc 项目</h1>
          <p class="subtitle">项目 = 一份 frp 配置。可为每个项目绑定独立版本，支持 TCP/UDP/HTTP/HTTPS/STCP/XTCP 隧道，多实例并行互不干扰。</p>
        </div>
        <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
      </div>

      <div class="control-panel">
        <div class="meta-info">
          <span>共 {{ projects.length }} 个项目</span>
        </div>
        <button class="btn btn-primary" @click="openCreate">+ 新建项目</button>
      </div>

      <div v-if="errorMsg" class="error-box">{{ errorMsg }}</div>

      <div v-if="projects.length === 0 && !loading" class="empty-state">
        <div class="empty-icon">⧉</div>
        <p>还没有 frp 项目</p>
        <button class="btn btn-primary" @click="openCreate">创建第一个项目</button>
      </div>

      <div class="project-grid">
        <div v-for="p in projects" :key="p.id" class="project-card">
          <div class="proj-top">
            <div class="proj-title-box">
              <span class="proj-name">{{ p.name }}</span>
              <span v-if="p.version" class="badge badge-version">{{ p.version }}</span>
              <span v-else class="badge badge-unbound">未绑定版本</span>
            </div>
            <span class="proj-status stopped"><span class="dot"></span>未启动</span>
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
          </div>

          <div class="proj-actions">
            <button class="btn btn-secondary btn-small" :disabled="!p.version" title="多实例启停（M4.3 落地）">
              ▶ 启动
            </button>
            <button class="btn btn-secondary btn-small" @click="openEdit(p)">编辑</button>
            <button class="btn btn-danger-outline btn-small" @click="deleteProject(p)">删除</button>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.projects-page { display: flex; flex-direction: column; gap: 16px; }
.header-row { display: flex; justify-content: space-between; align-items: center; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }

.control-panel {
  display: flex; align-items: center; justify-content: space-between;
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  padding: 12px 16px; border-radius: 8px;
}
.meta-info { font-size: 13px; color: var(--text-muted); }
.error-box { padding: 10px 14px; background: #ffebe9; color: var(--danger); border: 1px solid rgba(207, 34, 46, 0.2); border-radius: 6px; font-size: 13px; }

.empty-state { text-align: center; padding: 56px 0; color: var(--text-subtle); display: flex; flex-direction: column; align-items: center; gap: 10px; }
.empty-icon { font-size: 40px; }

.project-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 14px; }
.project-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color); border-radius: 10px;
  padding: 16px; display: flex; flex-direction: column; gap: 12px; transition: box-shadow 0.15s ease;
}
.project-card:hover { box-shadow: 0 2px 12px rgba(0, 0, 0, 0.06); }

.proj-top { display: flex; justify-content: space-between; align-items: center; }
.proj-title-box { display: flex; align-items: center; gap: 8px; min-width: 0; }
.proj-name { font-size: 15px; font-weight: 600; color: var(--text-main); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; white-space: nowrap; }
.badge-version { background: #ddf4ff; color: #0969da; }
.badge-unbound { background: var(--bg-hover); color: var(--text-muted); }

.proj-status { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; }
.proj-status .dot { width: 8px; height: 8px; border-radius: 50%; }
.proj-status.stopped { color: var(--text-subtle); }
.proj-status.stopped .dot { background: #c1c7cd; }

.proj-server { display: flex; align-items: center; justify-content: space-between; background: var(--bg-app); border: 1px solid var(--border-color); border-radius: 6px; padding: 8px 10px; }
.server-addr { font-family: Consolas, monospace; font-size: 12px; color: var(--text-main); }
.server-flags { display: flex; gap: 4px; }
.flag { font-size: 10px; padding: 1px 6px; border-radius: 3px; background: #f0f7ff; color: #0969da; }

.proj-proxies { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); }
.proxies-label { font-weight: 600; }
.proxies-types { color: var(--text-subtle); }

.proj-actions { display: flex; gap: 8px; border-top: 1px solid var(--border-color); padding-top: 12px; }

.btn { padding: 6px 16px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-hover); }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); margin-right: auto; }
.btn-secondary:hover { background: var(--bg-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
.btn-danger-outline { background: #fff; border-color: #ff8170; color: var(--danger); }
.btn-danger-outline:hover { background: #ffebe9; }

@keyframes fadeIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }
</style>