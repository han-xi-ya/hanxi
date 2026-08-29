<script setup lang="ts">
// 开发环境检测：并发探测本机工具链（git/node/java/python/npm/pnpm/go），
// 并检测 Go gRPC 的 protoc-gen-go-grpc 代码生成插件。
// 无事件、无轮询：进入页面自动检测一次（KeepAlive 缓存后不重复），刷新靠按钮。
import { ref, computed, onMounted } from 'vue'
import * as EnvCheckAPI from '../../bindings/hanxi/internal/modules/envcheck/envcheckservice'
import type { ToolInfo } from '../../bindings/hanxi/internal/modules/envcheck/detect/models'
import { getErrorMessage } from '../utils/errors'

// ---------- 状态 ----------
const tools = ref<ToolInfo[]>([])
const loading = ref(false)
const loadError = ref('')
const everLoaded = ref(false)

async function refresh() {
  if (loading.value) return // 防重入
  loading.value = true
  loadError.value = ''
  try {
    tools.value = (await EnvCheckAPI.DetectAll()) ?? []
    everLoaded.value = true
  } catch (e) {
    loadError.value = `检测失败: ${getErrorMessage(e)}`
  } finally {
    loading.value = false
  }
}

const okCount = computed(() => tools.value.filter(t => t.status === 'installed').length)
const totalCount = computed(() => tools.value.length)

// ---------- 状态渲染映射（颜色与全局 CSS 变量 / BCUView 琥珀警告色对齐）----------
const STATUS_META: Record<string, { text: string; icon: string; cls: string }> = {
  'installed': { text: '已安装', icon: '✓', cls: 'chip-installed' },
  'missing': { text: '未安装', icon: '○', cls: 'chip-missing' },
  'error': { text: '检测失败', icon: '!', cls: 'chip-error' },
  'store-stub': { text: '商店存根', icon: '⚠', cls: 'chip-stub' },
}

function metaOf(t: ToolInfo) {
  return STATUS_META[t.status] ?? STATUS_META['error']
}

onMounted(refresh) // 首次进入自动检测；KeepAlive 缓存组件，onMounted 仅一次
</script>

<template>
  <section class="page env-view">
    <div class="header-row">
      <div>
        <h1>开发环境检测</h1>
        <p class="subtitle">
          探测本机开发工具链的安装路径与版本：git · node · java · python · npm · pnpm · go · Go gRPC。
          Go gRPC 检测项指 <code>protoc-gen-go-grpc</code> 代码生成插件；项目运行时依赖仍由各项目的 <code>go.mod</code> 管理。
        </p>
      </div>
      <div class="btn-group">
        <span v-if="everLoaded" class="stat-text">
          ✓ {{ okCount }} / {{ totalCount }} 已安装
        </span>
        <button class="btn btn-primary btn-small" :disabled="loading" @click="refresh">
          {{ loading ? '检测中…' : '↻ 重新检测' }}
        </button>
      </div>
    </div>

    <div v-if="loadError" class="hint-banner banner-error">{{ loadError }}</div>

    <!-- 首次加载空态 -->
    <div v-if="loading && !everLoaded" class="empty-state">
      <p>正在检测开发环境（约 1~5 秒）…</p>
    </div>

    <!-- 工具卡片网格：刷新中保留旧数据原位更新（半透明而非清空） -->
    <div v-else-if="everLoaded" class="tool-grid" :class="{ refreshing: loading }">
      <div v-for="t in tools" :key="t.name" class="tool-card" :class="`status-${t.status}`">
        <div class="tool-card-top">
          <span class="tool-name">{{ t.display }}</span>
          <span class="status-chip" :class="metaOf(t).cls">
            {{ metaOf(t).icon }} {{ metaOf(t).text }}
          </span>
        </div>

        <div class="inst-meta">
          <div class="meta-line">
            <span class="k">版本</span>
            <code class="mono">{{ t.version || '—' }}</code>
          </div>
          <div class="meta-line">
            <span class="k">路径</span>
            <code class="mono tool-path" :title="t.path || undefined">{{ t.path || '—' }}</code>
          </div>
        </div>

        <div v-if="t.hint" class="tool-hint" :class="t.status === 'store-stub' ? 'hint-warn' : 'hint-error'">
          {{ t.hint }}
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.env-view { display: flex; flex-direction: column; gap: 14px; }
.header-row { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.header-row h1 { margin: 0 0 6px; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 0; line-height: 1.6; }
.btn-group { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.stat-text { font-size: 12px; color: var(--text-muted); }

/* ---------- 提示条 ---------- */
.hint-banner { padding: 10px 14px; border-radius: 6px; font-size: 13px; border: 1px solid transparent; }
.banner-error { background: #ffebe9; border-color: rgba(207, 34, 46, 0.25); color: var(--danger); }

/* ---------- 首次加载空态 ---------- */
.empty-state {
  text-align: center; padding: 40px 24px; background: var(--bg-sidebar);
  border: 1px dashed var(--border-color); border-radius: 8px;
}
.empty-state p { margin: 0; color: var(--text-muted); font-size: 13px; }

/* ---------- 工具卡片网格 ---------- */
.tool-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 12px; transition: opacity 0.15s ease; }
.tool-grid.refreshing { opacity: 0.55; }
.tool-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  border-left: 3px solid var(--border-color); border-radius: 8px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 8px;
}
/* 左缘状态色条：与徽章同色的第二视觉通道 */
.tool-card.status-installed { border-left-color: #1a7f37; }
.tool-card.status-missing { border-left-color: #8c959f; }
.tool-card.status-error { border-left-color: #cf222e; }
.tool-card.status-store-stub { border-left-color: #9a6700; }

.tool-card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.tool-name { font-size: 14px; font-weight: 700; color: var(--text-main); }

.status-chip {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; white-space: nowrap;
}
.chip-installed { background: #dafbe1; color: #1a7f37; }
.chip-missing { background: #eaeef2; color: #656d76; }
.chip-error { background: #ffebe9; color: #cf222e; }
.chip-stub { background: #fff8c5; color: #9a6700; }

.inst-meta { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.meta-line { display: flex; gap: 8px; color: var(--text-muted); align-items: baseline; }
.meta-line .k { color: var(--text-subtle); width: 36px; flex-shrink: 0; }
.mono { font-family: Consolas, monospace; color: var(--text-main); font-size: 11px; word-break: break-all; }

.tool-hint { font-size: 12px; border-radius: 5px; padding: 6px 8px; line-height: 1.5; }
.hint-warn { background: #fff8c5; color: #9a6700; }
.hint-error { background: #ffebe9; color: var(--danger); }

/* ---------- 通用按钮（与 BCUView 同款） ---------- */
.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-small { padding: 4px 12px; font-size: 12px; }
</style>