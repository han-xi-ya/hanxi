<script setup lang="ts">
// 「网页应用」管理页——编排纯壳：条目状态、窗口编排、表单提交与事件订阅的
// 单一来源在 composables/useWebApp.ts；本文件只保留标记、呈现派生与
// 表单面板折叠这一纯 UI 开关（同 WechatBotView 拆分契约）。
// 设计语言：hanxi-workbench-ui——表面四层 + 全局 :where 原子（.page/.card/
// .btn/.chip/.state-box）复用，自定义类一律 webapp- 前缀 scoped；
// 徽标不靠颜色单传：色 + 文字 + 形状（圆点）三通道齐备。
import { ref, computed } from 'vue'
import type { WebAppEntryView } from '../../bindings/hanxi/internal/modules/webapp/models'
import { NAME_MAX, entryDefaultOpen, useWebApp } from '../composables/useWebApp'
import PageHeader from '../components/ui/PageHeader.vue'
import AppIcon from '../components/ui/AppIcon.vue'

const {
  entries,
  loading,
  loadError,
  refresh,
  anyWindowShown,
  collapsingAll,
  collapseAll,
  isRowBusy,
  openEntry,
  collapseEntry,
  openExternal,
  removeEntry,
  formName,
  formUrl,
  formIcon,
  formDefaultOpen,
  saving,
  isEditing,
  resetForm,
  startEdit,
  submitForm,
} = useWebApp()

// —— 纯 UI：新增/编辑共用表单面板的折叠开关 ——
const showForm = ref(false)

function toggleCreate() {
  if (showForm.value && !isEditing.value) {
    cancelForm()
    return
  }
  resetForm()
  showForm.value = true
}

function beginEdit(entry: WebAppEntryView) {
  startEdit(entry)
  showForm.value = true
}

function cancelForm() {
  resetForm()
  showForm.value = false
}

async function onSubmit() {
  if (await submitForm()) showForm.value = false
}

// —— 窗态徽标呈现派生（视图本地纯函数，同 getAvatarColor 就近安放纪律）——
interface WindowStateView {
  text: string
  chip: string
  live: boolean
  dot: boolean
}

function windowState(entry: WebAppEntryView): WindowStateView {
  if (entry.windowOpen) return { text: '打开中', chip: 'chip-positive', live: true, dot: true }
  if (entry.windowHidden) return { text: '已收起', chip: 'chip-information', live: false, dot: true }
  return { text: '无窗', chip: 'chip-neutral', live: false, dot: false }
}

// 「默认：窗口/浏览器」小字标文案（存量未设值经 entryDefaultOpen 归一化为窗口）。
function defaultOpenText(entry: WebAppEntryView): string {
  return entryDefaultOpen(entry) === 'browser' ? '默认：浏览器' : '默认：窗口'
}

// 每行一次派生，避免模板内多处重复调用 windowState。
const rows = computed(() => entries.value.map(e => ({
  entry: e,
  state: windowState(e),
  defaultText: defaultOpenText(e),
})))
</script>

<template>
  <section class="page">
    <PageHeader
      title="网页应用"
      subtitle="把常用网站当作独立窗口使用：一键开内嵌网页窗，也可随时改走系统默认浏览器。"
    >
      <template #actions>
        <div class="setting-actions">
          <button
            class="btn btn-secondary btn-small"
            :disabled="!anyWindowShown || collapsingAll"
            title="隐藏全部网页窗（短暂保留可秒开恢复，闲置 5 分钟后自动销毁）"
            @click="collapseAll"
          >{{ collapsingAll ? '收起中…' : '收起全部' }}</button>
          <button class="btn btn-primary btn-small" @click="toggleCreate">
            <AppIcon name="plus" :size="14" /> {{ showForm && !isEditing ? '关闭表单' : '新增网址' }}
          </button>
        </div>
      </template>
    </PageHeader>

    <div class="card webapp-card">
      <!-- 新增 / 编辑共用表单（可折叠；校验错误以服务端中文为准经 toast 回显） -->
      <form v-if="showForm" class="webapp-form" @submit.prevent="onSubmit">
        <div class="webapp-form-head">
          <span class="webapp-form-title">{{ isEditing ? '编辑网址' : '新增网址' }}</span>
          <span class="webapp-hint">仅支持 http / https 地址；图标可留空（未填以全局标记呈现）。</span>
        </div>
        <div class="webapp-form-row">
          <label class="webapp-field">
            <span class="webapp-field-label">名称</span>
            <input
              v-model="formName"
              class="webapp-input webapp-name-input"
              :maxlength="NAME_MAX"
              placeholder="如：微信文件传输助手"
            />
          </label>
          <label class="webapp-field webapp-url-field">
            <span class="webapp-field-label">网址</span>
            <input
              v-model="formUrl"
              class="webapp-input webapp-url-input"
              inputmode="url"
              placeholder="https://example.com"
            />
          </label>
          <label class="webapp-field">
            <span class="webapp-field-label">图标（emoji，可选）</span>
            <input v-model="formIcon" class="webapp-input webapp-icon-input" maxlength="8" placeholder="🌐" />
          </label>
          <div class="webapp-field">
            <span class="webapp-field-label">默认打开方式</span>
            <div class="webapp-seg" role="radiogroup" aria-label="默认打开方式">
              <button
                type="button" class="webapp-seg-btn" :class="{ active: formDefaultOpen === 'window' }"
                role="radio" :aria-checked="formDefaultOpen === 'window'" @click="formDefaultOpen = 'window'"
              >独立窗口</button>
              <button
                type="button" class="webapp-seg-btn" :class="{ active: formDefaultOpen === 'browser' }"
                role="radio" :aria-checked="formDefaultOpen === 'browser'" @click="formDefaultOpen = 'browser'"
              >系统浏览器</button>
            </div>
          </div>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? '保存中…' : isEditing ? '保存修改' : '添加网址' }}
          </button>
          <button type="button" class="btn btn-ghost btn-small" @click="cancelForm">取消</button>
        </div>
      </form>

      <!-- 列表三态：加载 / 错误（可重试）/ 空（引导添加），复用全局 state-box 虚线面板语法；
           已有列表时刷新失败走「陈旧保留」纪律——列表不清屏，顶部挂警示条 + 重试 -->
      <div v-if="loading && entries.length === 0" class="state-box">正在加载网址列表…</div>
      <div v-else-if="loadError && entries.length === 0" class="state-box state-error">
        加载网址列表失败：{{ loadError }}
        <button class="state-action" @click="refresh">重试</button>
      </div>
      <div v-else-if="entries.length === 0" class="state-box">
        还没有网址——添加常用网站，以独立窗口即用即开。
        <button class="state-action" @click="toggleCreate">新增网址</button>
      </div>
      <div v-else class="webapp-list">
        <div v-if="loadError" class="state-box state-warning">
          列表刷新失败（显示上次结果）：{{ loadError }}
          <button class="state-action" @click="refresh">重试</button>
        </div>
        <div v-for="row in rows" :key="row.entry.id" class="webapp-row">
          <span class="webapp-icon-well" aria-hidden="true">
            <template v-if="row.entry.icon">{{ row.entry.icon }}</template>
            <AppIcon v-else name="globe" :size="15" />
          </span>
          <div class="webapp-row-main">
            <span class="webapp-name" :title="row.entry.name">{{ row.entry.name }}</span>
            <code class="webapp-url" :title="row.entry.url">{{ row.entry.url }}</code>
          </div>
          <span class="chip webapp-state" :class="row.state.chip">
            <i v-if="row.state.dot" class="webapp-dot" :class="{ 'webapp-dot-live': row.state.live }" />
            {{ row.state.text }}
          </span>
          <span class="chip chip-neutral webapp-default" title="轮盘/托盘里点击该应用时，按此默认执行">
            {{ row.defaultText }}
          </span>
          <div class="setting-actions">
            <button
              class="btn btn-secondary btn-small"
              :disabled="row.entry.windowOpen || isRowBusy(row.entry.id)"
              title="以独立网页窗打开（已收起的窗口点击即恢复）"
              @click="openEntry(row.entry)"
            >打开</button>
            <button
              class="btn btn-secondary btn-small"
              :disabled="!row.entry.windowOpen || isRowBusy(row.entry.id)"
              title="收起隐藏（短暂保留秒开恢复，闲置 5 分钟后自动销毁）"
              aria-label="收起"
              @click="collapseEntry(row.entry)"
            ><AppIcon name="chevron-down" :size="14" /></button>
            <button
              class="btn btn-secondary btn-small"
              :disabled="isRowBusy(row.entry.id)"
              title="用系统默认浏览器打开"
              aria-label="默认浏览器打开"
              @click="openExternal(row.entry)"
            ><AppIcon name="globe" :size="14" /></button>
            <button
              class="btn btn-secondary btn-small"
              :disabled="isRowBusy(row.entry.id)"
              title="编辑该网址"
              aria-label="编辑"
              @click="beginEdit(row.entry)"
            ><AppIcon name="pen-line" :size="14" /></button>
            <button
              class="btn btn-secondary btn-small webapp-remove"
              :disabled="isRowBusy(row.entry.id)"
              title="删除条目（其存活窗口一并关闭）"
              aria-label="删除"
              @click="removeEntry(row.entry)"
            ><AppIcon name="trash-2" :size="14" /></button>
          </div>
        </div>
      </div>

      <p v-if="!loading && !loadError && entries.length > 0" class="webapp-note">
        网页窗共享统一的浏览器数据目录：窗口收起或闲置销毁后再打开，网站登录态不丢。
        轮盘/托盘里点击该应用时，按此默认执行；列表中「打开」「默认浏览器打开」两颗钮为显式选择，即时生效、优先于默认。
      </p>
    </div>
  </section>
</template>

<style scoped>
.webapp-card { display: flex; flex-direction: column; gap: 12px; }

/* —— 新增/编辑表单：软面 + 虚线边（同设置页 exe-form 语法的 webapp 实例）—— */
.webapp-form {
  background: var(--surface-page); border: 1px dashed var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 8px;
}
.webapp-form-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.webapp-form-title { font-size: var(--text-sm); font-weight: 600; color: var(--color-text); }
.webapp-form-row { display: flex; gap: 8px; align-items: flex-end; flex-wrap: wrap; }
.webapp-field { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.webapp-field-label { font-size: var(--text-xs); color: var(--color-text-subtle); }
.webapp-input {
  padding: 5px 8px; border: 1px solid var(--color-border); border-radius: 6px;
  background: var(--surface-panel); color: var(--color-text); font-size: var(--text-sm);
}
.webapp-name-input { width: 170px; }
.webapp-url-field { flex: 1; min-width: 220px; }
.webapp-url-input { width: 100%; font-family: var(--font-mono); }
.webapp-icon-input { width: 90px; }
.webapp-hint { font-size: var(--text-sm); color: var(--color-text-muted); }

/* 默认打开方式微段控件：复刻设置页 .theme-seg 形制（同一组语义 token，零新色），
   表单行内紧凑一档（text-sm / 小 padding），选中态同为面板底 + 主色字。 */
.webapp-seg { display: flex; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 3px; gap: 2px; }
.webapp-seg-btn {
  display: inline-flex; align-items: center; border: none; background: transparent;
  padding: 4px 12px; border-radius: 6px; font-size: var(--text-sm); color: var(--color-text-muted);
  cursor: pointer; transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.webapp-seg-btn:hover { color: var(--color-text); }
.webapp-seg-btn.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }

/* —— 条目列表：资源行语法（主内容 + 状态徽标 + 行级动作），窄屏自然回折成两段 —— */
.webapp-list { display: flex; flex-direction: column; gap: 8px; }
.webapp-row {
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 8px 12px; min-height: 52px; display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  transition: border-color var(--motion-base) ease;
}
.webapp-row:hover { border-color: var(--color-border-strong); }
.webapp-icon-well {
  width: 30px; height: 30px; flex: none; display: inline-flex; align-items: center; justify-content: center;
  background: var(--color-primary-soft); color: var(--color-text-muted);
  border-radius: var(--radius-control); font-size: var(--text-md); line-height: 1;
}
.webapp-row-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1 1 220px; }
.webapp-name {
  font-size: var(--text-base); font-weight: 600; color: var(--color-text);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.webapp-url {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.webapp-state { flex: none; }
.webapp-default { flex: none; }
.webapp-dot { width: 6px; height: 6px; border-radius: var(--radius-pill); background: currentColor; flex: none; }
/* 脉冲仅限真实活态（窗口可见中）；减弱动效由 base.css 全局块统一熄火 */
.webapp-dot-live { animation: webapp-pulse 1.8s ease-in-out infinite; }
@keyframes webapp-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

.webapp-remove:hover:not(:disabled) { border-color: var(--state-danger); color: var(--state-danger); }

.webapp-note { margin: 0; font-size: var(--text-sm); color: var(--color-text-subtle); }
</style>
