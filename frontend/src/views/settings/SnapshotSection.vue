<script setup lang="ts">
// 设置分区·历史版本（数据自动快照，PLAN_SNAPSHOT §3.5）：
// 状态行 → 偏好行 → 历史列表（.tbl，行尾预览）→ 预览弹窗（文件清单+内容预览+单文件恢复）。
// 恢复走 useConfirm（danger + 明细）；非便签文件写盘后提示重启生效，便签热生效。
// 降级（git 缺失）模式按 Q3 裁定退化为"打开备份目录"一个动作。
import { ref, computed, onMounted } from 'vue'
import * as SnapshotAPI from '../../../bindings/hanxi/internal/snapshot'
import type { StatusInfo, Revision, RevisionFile, FilePreview } from '../../../bindings/hanxi/internal/snapshot/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const { showToast } = useToast()
const { confirm } = useConfirm()

const status = ref<StatusInfo | null>(null)
const revisions = ref<Revision[]>([])
const loading = ref(false)
const busy = ref(false)

// 偏好控件（回填自 status，改动即存）
const enabled = ref(true)
const idleSeconds = ref(300)
const intervalMinutes = ref(5)
const savingPrefs = ref(false)

// 预览弹窗态
const previewRev = ref<Revision | null>(null)
const previewFiles = ref<RevisionFile[]>([])
const previewDetail = ref<FilePreview | null>(null)
const previewLoading = ref(false)

const backupMode = computed(() => !!status.value && status.value.mode === 'backup')
const gitMode = computed(() => !!status.value && status.value.mode === 'git')

const modeChip = computed(() => {
  if (!status.value) return '正在读取快照状态…'
  if (!status.value.enabled) return '已停用 · 不再自动留版本'
  return status.value.mode === 'git' ? 'Git 历史仓库 · 本机静默提交' : '本机备份模式 · 未检测到可用 Git'
})

const modeChipClass = computed(() => {
  if (!status.value) return 'chip-neutral'
  if (!status.value.enabled) return 'chip-neutral'
  return status.value.mode === 'git' ? 'chip-information' : 'chip-warning'
})

/** RFC3339 → 本地可读 `MM-DD HH:mm`（列表紧凑列宽用）。 */
function fmtTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function refresh() {
  loading.value = true
  try {
    status.value = await SnapshotAPI.CheckpointService.GetStatus()
    enabled.value = status.value.enabled
    idleSeconds.value = status.value.idleSeconds
    intervalMinutes.value = status.value.intervalMinutes
    if (status.value.mode === 'git') {
      revisions.value = (await SnapshotAPI.CheckpointService.ListRevisions()) ?? []
    } else {
      revisions.value = []
    }
  } catch (e: unknown) {
    showToast(`获取历史版本状态失败: ${getErrorMessage(e)}`)
  } finally {
    loading.value = false
  }
}

async function savePrefs() {
  savingPrefs.value = true
  try {
    await SnapshotAPI.CheckpointService.SetPreferences({
      enabled: enabled.value,
      idleSeconds: Number(idleSeconds.value) || 300,
      intervalMinutes: Number(intervalMinutes.value) || 5,
    })
    showToast('历史版本偏好已更新')
    status.value = await SnapshotAPI.CheckpointService.GetStatus()
  } catch (e: unknown) {
    showToast(`保存失败: ${getErrorMessage(e)}`)
    await refresh()
  } finally {
    savingPrefs.value = false
  }
}

async function snapshotNow() {
  busy.value = true
  try {
    await SnapshotAPI.CheckpointService.CheckpointNow()
    showToast('已触发立即快照，正在写入…')
    // 提交是异步单飞闸：短轮询到列表变化或超时为止
    const before = revisions.value.length
    for (let i = 0; i < 6; i++) {
      await new Promise((r) => setTimeout(r, 800))
      status.value = await SnapshotAPI.CheckpointService.GetStatus()
      if (status.value.mode === 'git') {
        const list = (await SnapshotAPI.CheckpointService.ListRevisions()) ?? []
        if (list.length !== before) {
          revisions.value = list
          break
        }
        revisions.value = list
      }
    }
  } catch (e: unknown) {
    showToast(`快照失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}

async function openBackupDir() {
  try {
    await SnapshotAPI.CheckpointService.OpenHistoryDir()
  } catch (e: unknown) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function openPreview(rev: Revision) {
  previewRev.value = rev
  previewFiles.value = []
  previewDetail.value = null
  previewLoading.value = true
  try {
    previewFiles.value = (await SnapshotAPI.CheckpointService.RevisionDetail(rev.id)) ?? []
  } catch (e: unknown) {
    showToast(`读取版本内容失败: ${getErrorMessage(e)}`)
  } finally {
    previewLoading.value = false
  }
}

function closePreview() {
  previewRev.value = null
  previewFiles.value = []
  previewDetail.value = null
}

async function showFileContent(file: RevisionFile) {
  if (!previewRev.value) return
  previewLoading.value = true
  try {
    previewDetail.value = await SnapshotAPI.CheckpointService.PreviewFile(previewRev.value.id, file.path)
  } catch (e: unknown) {
    showToast(`预览失败: ${getErrorMessage(e)}`)
    previewDetail.value = null
  } finally {
    previewLoading.value = false
  }
}

const statusLabel: Record<string, string> = { M: '修改', A: '新增', D: '删除', R: '改名' }

async function restoreOne(file: RevisionFile) {
  if (!previewRev.value) return
  const rev = previewRev.value
  const accepted = await confirm({
    title: `恢复「${file.path}」到该历史版本？`,
    description: file.path.startsWith('memo/')
      ? '该便签将即时生效（无需重启）。当前内容会被该版本覆盖。'
      : '文件将写回数据盘，重启 Hanxi 后生效。当前内容会被该版本覆盖。',
    confirmLabel: '恢复',
    tone: 'danger',
    details: [
      { label: '版本', value: `${fmtTime(rev.time)} · ${rev.id.slice(0, 8)}` },
      { label: '文件', value: file.path },
      { label: '变化', value: statusLabel[file.status] ?? file.status },
    ],
  })
  if (!accepted) return
  try {
    await SnapshotAPI.CheckpointService.RestoreFile(rev.id, file.path)
    showToast(file.path.startsWith('memo/') ? '便签已热恢复' : '已写回磁盘，重启 Hanxi 后生效')
    await refresh()
  } catch (e: unknown) {
    showToast(`恢复失败: ${getErrorMessage(e)}`)
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <PageHeader title="历史版本" subtitle="配置与便签每次落定自动留一个可回滚的版本；全程本机静默，永不上传。">
      <template #actions>
        <span class="chip" :class="modeChipClass">{{ modeChip }}</span>
      </template>
    </PageHeader>

    <div class="card pref-list">
      <label class="setting-row setting-row-tappable">
        <span class="setting-main">
          <span class="setting-name">自动保留历史版本</span>
          <span class="setting-desc">窗口失焦、空闲或退出时静默快照 config.json、模块状态与便签；关闭后立即停止</span>
        </span>
        <input v-model="enabled" type="checkbox" class="switch" :disabled="savingPrefs" @change="savePrefs" />
      </label>

      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">空闲阈值</span>
          <span class="setting-desc">数据静默该秒数后自动留版本（30–86400）</span>
        </span>
        <span class="input-inline">
          <input v-model.number="idleSeconds" type="number" class="input-number" min="30" max="86400" :disabled="savingPrefs" @change="savePrefs" />
          <span class="input-unit">秒</span>
        </span>
      </div>

      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">最小快照间隔</span>
          <span class="setting-desc">两次留版本的最小间隔，防止连环保存灌碎历史（1–1440 分钟）</span>
        </span>
        <span class="input-inline">
          <input v-model.number="intervalMinutes" type="number" class="input-number" min="1" max="1440" :disabled="savingPrefs" @change="savePrefs" />
          <span class="input-unit">分钟</span>
        </span>
      </div>

      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">存储位置</span>
          <code class="setting-desc dir-path" :title="status?.snapshotDir">{{ status?.snapshotDir || '—' }}</code>
        </span>
        <span class="row-actions">
          <button class="btn btn-secondary btn-small" @click="snapshotNow" :disabled="busy || !status">
            <AppIcon name="clock" :size="14" /> {{ busy ? '快照中…' : '立即快照' }}
          </button>
          <button class="btn btn-secondary btn-small" @click="openBackupDir">
            <AppIcon name="folder" :size="14" /> 打开目录
          </button>
        </span>
      </div>
    </div>

    <!-- 降级模式（Q3 裁定）：仅"打开备份目录"一个动作，列表隐藏 -->
    <div v-if="backupMode" class="card backup-note">
      <span class="backup-icon"><AppIcon name="hard-drive" :size="18" /></span>
      <div>
        <div class="backup-title">当前为本机备份模式</div>
        <div class="backup-desc">未检测到可用的 Git（或被商店存根占位），历史版本以时间戳备份目录形式滚动保留最近 30 份，可从此处打开目录手工找回文件。</div>
      </div>
    </div>

    <div v-else-if="gitMode" class="card">
      <div class="card-head">
        <span class="card-title">版本列表</span>
        <span class="card-meta">{{ revisions.length ? `共 ${revisions.length} 个版本（最多展示 50）` : '' }}</span>
      </div>
      <div v-if="loading" class="hist-empty">正在读取历史版本…</div>
      <div v-else-if="!revisions.length" class="hist-empty">
        暂无历史版本。改一处设置或便签，稍后自动留下第一版；也可以点上方「立即快照」。
      </div>
      <table v-else class="tbl">
        <thead>
          <tr>
            <th style="width: 108px">时间</th>
            <th>变更内容</th>
            <th style="width: 72px">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="rev in revisions" :key="rev.id">
            <td class="mono">{{ fmtTime(rev.time) }}</td>
            <td class="summary-cell" :title="rev.summary">{{ rev.summary }}</td>
            <td>
              <button class="btn btn-ghost btn-small" @click="openPreview(rev)">预览</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 预览弹窗：版本文件清单 → 点选看内容 → 单文件恢复 -->
    <div v-if="previewRev" class="modal-backdrop" @click.self="closePreview">
      <div class="modal-card">
        <div class="modal-head">
          <h3>版本 {{ previewRev.id.slice(0, 8) }} · {{ fmtTime(previewRev.time) }}</h3>
          <button class="btn-close" aria-label="关闭" @click="closePreview">✕</button>
        </div>
        <div class="modal-body">
          <div v-if="previewLoading" class="hist-empty">读取中…</div>
          <template v-else>
            <div class="file-list">
              <div v-for="f in previewFiles" :key="f.path" class="file-row" :class="{ active: previewDetail?.path === f.path }">
                <span class="file-st" :class="`st-${f.status}`">{{ statusLabel[f.status] ?? f.status }}</span>
                <code class="file-path" :title="f.path">{{ f.path }}</code>
                <span class="row-actions">
                  <button class="btn btn-ghost btn-small" @click="showFileContent(f)">内容</button>
                  <button v-if="f.status !== 'D'" class="btn btn-secondary btn-small" @click="restoreOne(f)">恢复</button>
                </span>
              </div>
              <div v-if="!previewFiles.length" class="hist-empty">该版本没有可展示的白名单文件。</div>
            </div>
            <div v-if="previewDetail" class="preview-pane">
              <div class="preview-head">
                <code>{{ previewDetail.path }}</code>
                <span v-if="previewDetail.truncated" class="chip chip-warning preview-cut">已截断（{{ previewDetail.size }} 字节）</span>
              </div>
              <pre class="preview-body">{{ previewDetail.content }}</pre>
            </div>
          </template>
        </div>
        <div class="modal-actions">
          <button class="btn btn-secondary" @click="closePreview">关闭</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 行骨架复用全局 .setting-row / .card / .chip / .tbl / .btn 原子，此处仅节奏与专属皮 */
.pref-list { display: flex; flex-direction: column; gap: 8px; margin-bottom: 16px; }
.switch { width: 18px; height: 18px; cursor: pointer; accent-color: var(--color-primary); flex: none; }
.input-inline { display: flex; align-items: center; gap: 6px; }
.input-number {
  width: 72px; padding: 5px 8px; border: 1px solid var(--color-border);
  border-radius: var(--radius-control); background: var(--surface-panel); color: var(--color-text); font-size: var(--text-base);
}
.input-unit { font-size: var(--text-sm); color: var(--color-text-muted); }
.dir-path {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 520px; display: inline-block; vertical-align: middle;
}
.row-actions { display: flex; gap: 6px; flex: none; }

.backup-note { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 16px; }
.backup-icon {
  flex: none; width: 34px; height: 34px; border-radius: var(--radius-element);
  display: inline-flex; align-items: center; justify-content: center;
  background: var(--surface-chrome); color: var(--color-text-muted);
}
.backup-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text); }
.backup-desc { font-size: var(--text-sm); color: var(--color-text-muted); margin-top: 2px; }

.card-head { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 8px; }
.card-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text); }
.card-meta { font-size: var(--text-xs); color: var(--color-text-subtle); }
.hist-empty { padding: 18px 4px; font-size: var(--text-sm); color: var(--color-text-muted); }
.mono { font-family: var(--font-mono); font-size: var(--text-xs); }
.summary-cell { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 0; }

/* 预览弹窗（皮照 FrpcProjectEditor batch modal，宽度按文件+内容双栏放宽） */
.modal-backdrop {
  position: fixed; inset: 0; z-index: 100;
  background: var(--overlay-mask);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: var(--surface-panel); border-radius: var(--radius-element); width: 680px; max-width: 92vw;
  box-shadow: var(--shadow-panel); overflow: hidden;
}
.modal-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 20px; border-bottom: 1px solid var(--color-border);
}
.modal-head h3 { margin: 0; font-size: var(--text-base); color: var(--color-text); font-family: var(--font-mono); font-weight: 500; }
.btn-close { background: transparent; border: none; font-size: var(--text-lg); cursor: pointer; color: var(--color-text-muted); }
.modal-body { padding: 14px 20px; display: flex; flex-direction: column; gap: 10px; max-height: 62vh; overflow: auto; }
.modal-actions {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 12px 20px; border-top: 1px solid var(--color-border); background: var(--surface-panel);
}

.file-list { display: flex; flex-direction: column; gap: 4px; }
.file-row {
  display: flex; align-items: center; gap: 8px; padding: 5px 8px;
  border: 1px solid transparent; border-radius: var(--radius-control);
}
.file-row.active { background: var(--surface-hover); border-color: var(--color-border); }
.file-st { flex: none; width: 40px; font-size: var(--text-xs); color: var(--color-text-muted); }
.file-st.st-A { color: var(--color-success, var(--color-primary)); }
.file-st.st-D { color: var(--color-danger, var(--color-text)); }
.file-path {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: 1; min-width: 0;
}
.preview-pane { border: 1px solid var(--color-border); border-radius: var(--radius-element); overflow: hidden; }
.preview-head {
  display: flex; justify-content: space-between; align-items: center; gap: 8px;
  padding: 8px 12px; background: var(--surface-chrome); border-bottom: 1px solid var(--color-border);
}
.preview-head code { font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text); overflow: hidden; text-overflow: ellipsis; }
.preview-cut { flex: none; }
.preview-body {
  margin: 0; padding: 10px 12px; max-height: 240px; overflow: auto;
  font-family: var(--font-mono); font-size: var(--text-xs); line-height: 1.5; color: var(--color-text);
  white-space: pre-wrap; word-break: break-all; background: var(--surface-panel);
}
</style>
