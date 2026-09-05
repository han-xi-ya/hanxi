<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { ModuleInfo, NavEntry } from '../../bindings/hanxi/internal/extapi/models'
import type { AppInfo } from '../../bindings/hanxi/internal/app/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { showToast } = useToast()

const modules = shallowRef<ModuleInfo[]>([])
const navs = shallowRef<NavEntry[]>([])
const appInfo = ref<AppInfo | null>(null)
const toggling = ref<string | null>(null)
let unlistenExtChanged: (() => void) | null = null

// 模块图标与首选路由映射
const MODULE_META: Record<string, { icon: string; route: string }> = {
  frpc: { icon: '⚡', route: '/frpc' },
  fileshare: { icon: '📁', route: '/ext/fileshare' },
  memo: { icon: '📝', route: '/ext/memo' },
  lan: { icon: '◉', route: '/ext/lan' },
  portscan: { icon: '🔍', route: '/ext/portscan' },
  wechat: { icon: '💬', route: '/ext/wechat' },
  publicip: { icon: '≋', route: '/ext/publicip' },
  portkill: { icon: '✕', route: '/ext/portkill' },
  wifi: { icon: '📶', route: '/ext/wifi' },
  markeron: { icon: '✎', route: '/ext/markeron' },
  everything: { icon: '🔎', route: '/ext/everything' },
  ccswitch: { icon: '🔀', route: '/ext/ccswitch' },
  snipaste: { icon: '✂', route: '/ext/snipaste' },
  nanazip: { icon: 'NZ', route: '/ext/nanazip' },
  eartrumpet: { icon: '🔊', route: '/ext/eartrumpet' },
  mangodisk: { icon: '🥭', route: '/ext/mangodisk' },
  bcu: { icon: '🧹', route: '/ext/bcu' },
  flclash: { icon: '⚡', route: '/ext/flclash' },
  recordly: { icon: '🎬', route: '/ext/recordly' },
  papertodo: { icon: '📄', route: '/ext/papertodo' },
  piclite: { icon: '🖼️', route: '/ext/piclite' },
  keyviz: { icon: '⌨️', route: '/ext/keyviz' },
  quicklook: { icon: '👁️', route: '/ext/quicklook' },
  litemonitor: { icon: '📊', route: '/ext/litemonitor' },
  guoheview: { icon: '🏞️', route: '/ext/guoheview' },
  ddnsgo: { icon: '🌐', route: '/ext/ddnsgo' },
  subnetdesk: { icon: '🖥', route: '/ext/subnetdesk' },
  rustdesk: { icon: '🌍', route: '/ext/rustdesk' },
}

async function loadData() {
  try {
    const [mods, navList, info] = await Promise.all([
      AppAPI.AppService.ListModules(),
      AppAPI.AppService.GetNavs(),
      AppAPI.AppService.GetAppInfo(),
    ])
    modules.value = mods ?? []
    navs.value = navList ?? []
    appInfo.value = info
  } catch (err: unknown) {
    showToast(`获取工作台信息失败: ${getErrorMessage(err)}`)
  }
}

const enabledModules = computed(() => modules.value.filter(m => m.enabled))
const disabledModules = computed(() => modules.value.filter(m => !m.enabled))

function getModuleIcon(id: string): string {
  return MODULE_META[id]?.icon || '📦'
}

function getModuleRoute(id: string): string {
  if (MODULE_META[id]?.route) {
    return MODULE_META[id].route
  }
  const match = navs.value.find(n => n.id === id || n.route.includes(id))
  return match?.route || '/'
}

function enterModule(m: ModuleInfo) {
  const route = getModuleRoute(m.id)
  emit('navigate', route)
}

async function toggle(m: ModuleInfo) {
  toggling.value = m.id
  try {
    const nextState = !m.enabled
    await AppAPI.AppService.SetModuleEnabled(m.id, nextState)
    showToast(nextState ? `已启用「${m.name}」` : `已停用「${m.name}」，已回收运行时资源`)
    await loadData()
    // 后端 SetModuleEnabled 已通过 ext:changed 事件广播导航热更新，前端无需重复补发
  } catch (err: unknown) {
    showToast(`操作失败: ${getErrorMessage(err)}`)
  } finally {
    toggling.value = null
  }
}

onMounted(async () => {
  await loadData()
  unlistenExtChanged = Events.On('ext:changed', () => {
    loadData()
  })
})

onUnmounted(() => {
  if (unlistenExtChanged) {
    unlistenExtChanged()
    unlistenExtChanged = null
  }
})
</script>

<template>
  <section class="page home-dashboard">
    <!-- 顶部状态栏 -->
    <div class="header-row">
      <div class="title-group">
        <div class="title-with-badge">
          <h1>工具工作台</h1>
          <span class="version-tag" v-if="appInfo">v{{ appInfo.version }}</span>
        </div>
        <p class="subtitle">
          按需启闭功能模块；未启用的工具零开销、不占后台内存，即开即用。
        </p>
      </div>

      <div class="stats-card">
        <div class="stat-item">
          <span class="stat-num text-success">{{ enabledModules.length }}</span>
          <span class="stat-label">已启用</span>
        </div>
        <div class="stat-divider">/</div>
        <div class="stat-item">
          <span class="stat-num text-muted">{{ modules.length }}</span>
          <span class="stat-label">总功能</span>
        </div>
      </div>
    </div>

    <!-- 已启用的功能 -->
    <div class="section-container">
      <div class="section-title">
        <span class="dot-status ok"></span>
        <h2>已启用的功能 ({{ enabledModules.length }})</h2>
        <span class="section-hint">点击卡片可快速直达对应功能操作页</span>
      </div>

      <div v-if="enabledModules.length > 0" class="cards-grid">
        <div
          v-for="m in enabledModules"
          :key="m.id"
          class="module-card enabled-card"
          @click="enterModule(m)"
        >
          <div class="card-top">
            <div class="icon-wrap active-icon">
              <span class="mod-icon">{{ getModuleIcon(m.id) }}</span>
            </div>
            <div class="card-actions" @click.stop>
              <button
                class="btn-toggle-off"
                :disabled="toggling === m.id"
                title="停用此模块以释放内存"
                @click="toggle(m)"
              >
                {{ toggling === m.id ? '处理中…' : '停用' }}
              </button>
            </div>
          </div>

          <div class="card-info">
            <div class="card-title-row">
              <h3 class="mod-name">{{ m.name }}</h3>
              <span class="level-badge" :class="m.initialized ? 'badge-active' : 'badge-idle'">
                {{ m.initialized ? '活跃运行中' : '待命懒加载' }}
              </span>
            </div>
            <p class="mod-desc">{{ m.description }}</p>
          </div>

          <div class="card-bottom">
            <span class="enter-link">进入功能 →</span>
            <span class="author-tag">v{{ m.version }}</span>
          </div>
        </div>
      </div>

      <div v-else class="empty-state">
        <p>暂无启用的功能模块，可从下方列表中挑选并开启。</p>
      </div>
    </div>

    <!-- 未启用的功能 -->
    <div class="section-container">
      <div class="section-title">
        <span class="dot-status off"></span>
        <h2>待启用 / 未激活功能 ({{ disabledModules.length }})</h2>
        <span class="section-hint">已停用状态下不占用任何后台系统资源与内存</span>
      </div>

      <div v-if="disabledModules.length > 0" class="cards-grid">
        <div
          v-for="m in disabledModules"
          :key="m.id"
          class="module-card disabled-card"
        >
          <div class="card-top">
            <div class="icon-wrap idle-icon">
              <span class="mod-icon">{{ getModuleIcon(m.id) }}</span>
            </div>
            <button
              class="btn-toggle-on"
              :disabled="toggling === m.id"
              @click="toggle(m)"
            >
              {{ toggling === m.id ? '正在启用…' : '+ 启用模块' }}
            </button>
          </div>

          <div class="card-info">
            <div class="card-title-row">
              <h3 class="mod-name">{{ m.name }}</h3>
              <span class="level-badge badge-off">已休眠</span>
            </div>
            <p class="mod-desc">{{ m.description }}</p>
          </div>

          <div class="card-bottom">
            <span class="zero-cost-tag">⚡ 零后台常驻</span>
            <span class="author-tag">v{{ m.version }}</span>
          </div>
        </div>
      </div>

      <div v-else class="empty-state ok-state">
        <p>🎉 所有功能模块均已开启！</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.home-dashboard {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 1200px;
}

.header-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  padding: 16px 20px;
  border-radius: 10px;
}

.title-with-badge {
  display: flex;
  align-items: center;
  gap: 8px;
}

.title-with-badge h1 {
  font-size: 20px;
  font-weight: 700;
  margin: 0;
}

.version-tag {
  font-size: 11px;
  font-family: Consolas, monospace;
  background: var(--bg-app);
  color: var(--text-muted);
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--border-color);
}

.subtitle {
  color: var(--text-muted);
  font-size: 13px;
  margin: 6px 0 0;
}

.stats-card {
  display: flex;
  align-items: center;
  gap: 12px;
  background: var(--bg-app);
  border: 1px solid var(--border-color);
  padding: 8px 18px;
  border-radius: 8px;
}

.stat-item {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.stat-num {
  font-size: 18px;
  font-weight: 700;
  font-family: Consolas, monospace;
}

.text-success { color: var(--success); }
.text-muted { color: var(--text-muted); }

.stat-label {
  font-size: 11px;
  color: var(--text-subtle);
}

.stat-divider {
  font-size: 16px;
  color: var(--border-color);
}

.toast {
  position: fixed;
  top: 20px;
  right: 20px;
  background: var(--text-main);
  color: #fff;
  padding: 8px 16px;
  border-radius: 6px;
  font-size: 13px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  animation: fadeIn 0.2s ease;
  z-index: 1000;
}

.section-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-title h2 {
  font-size: 15px;
  font-weight: 600;
  margin: 0;
}

.section-hint {
  font-size: 12px;
  color: var(--text-subtle);
  margin-left: 4px;
}

.dot-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.dot-status.ok { background: var(--success); }
.dot-status.off { background: var(--text-subtle); }

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 14px;
}

.module-card {
  background: var(--bg-sidebar);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  transition: all 0.2s ease;
}

.enabled-card {
  cursor: pointer;
}
.enabled-card:hover {
  border-color: var(--accent);
  box-shadow: 0 4px 16px rgba(47, 111, 237, 0.08);
  transform: translateY(-2px);
}

.disabled-card {
  opacity: 0.85;
  background: #fafbfc;
}
.disabled-card:hover {
  opacity: 1;
  border-color: #c0c6d0;
}

.card-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.icon-wrap {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.active-icon {
  background: var(--bg-active);
}

.idle-icon {
  background: var(--bg-hover);
}

.mod-icon {
  font-size: 18px;
}

.btn-toggle-off {
  font-size: 11px;
  padding: 4px 10px;
  border-radius: 5px;
  border: 1px solid var(--border-color);
  background: #fff;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}
.btn-toggle-off:hover {
  background: #ffebe9;
  border-color: rgba(207, 34, 46, 0.3);
  color: var(--danger);
}

.btn-toggle-on {
  font-size: 11px;
  padding: 5px 12px;
  border-radius: 5px;
  border: 1px solid var(--accent);
  background: var(--bg-active);
  color: var(--accent);
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}
.btn-toggle-on:hover {
  background: var(--accent);
  color: #fff;
}

.card-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1;
}

.card-title-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.mod-name {
  font-size: 15px;
  font-weight: 600;
  margin: 0;
  color: var(--text-main);
}

.level-badge {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 500;
}

.badge-active {
  background: #dafbe1;
  color: #1a7f37;
}

.badge-idle {
  background: #e8f0fe;
  color: #1e5cd8;
}

.badge-off {
  background: #eaeef2;
  color: #656d76;
}

.mod-desc {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.card-bottom {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 8px;
  border-top: 1px solid var(--border-color);
  font-size: 11px;
}

.enter-link {
  color: var(--accent);
  font-weight: 600;
}

.author-tag {
  color: var(--text-subtle);
  font-family: Consolas, monospace;
}

.zero-cost-tag {
  color: var(--text-muted);
}

.empty-state {
  background: var(--bg-sidebar);
  border: 1px dashed var(--border-color);
  border-radius: 8px;
  padding: 24px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}

.ok-state {
  border-style: solid;
  background: #f6f8fa;
  color: var(--success);
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
