<script setup lang="ts">
// ClipDetail：右页面板的"正在读的那一页"（随手记 v2 阅读态同谱——列表即账，
// 这里落全貌；Get 拉回的完整条目就地渲染，三分支按 kind 分流）。
//
// 分支契约：
//   - text → 等宽正文（<pre> 原样呈现，不做 Markdown 渲染——剪贴板内容语义
//     未知，结构猜测只会把 URL/口令排花）+ 页脚"复制"走宿主 Set 回填；
//   - image → blobData（Get 回填的 base64 PNG）data URL 预览 + 尺寸/体积机器值；
//     blob 未回填（文件丢失/未压缩进 wire）如实给占位说明，不放空 <img>；
//   - file → 路径逐行列（mono + 断词），每行独立"复制该路径"走浏览器剪贴板
//     （useClipboard 统一回执通道）——整表回填系统剪贴板仍走页脚 Set。
//
// 敏感位（sensitive）在详情顶部恒挂警示条：本机照常可看可贴，但明确告知
// 此条目不进 AI（MCP 检索）通道——警示是给"要不要转给别人"做决策用的。
//
// 动作边界：close/retry/copy/togglePin/delete 只发意图；Get/TogglePin/Delete
// 的后端调用与确认闸全归视图（展示件零 backend import）。
import { computed } from 'vue'
import type { ClipEntry } from '../../types/clipboard'
import { useClipboard } from '../../composables/useClipboard'
import ClipGlyph from './ClipGlyph.vue'
import { blobDataUrl, fmtClipBytes, fmtClipDateTime, kindLabel, rowGlyph } from './clipboardFormat'

const props = defineProps<{
  /** Get 回填的完整条目（含 text/blobData）；加载中为 null */
  entry: ClipEntry | null
  loading?: boolean
  /** 详情拉取失败的可读错误（视图已组好话术前缀） */
  error?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'retry'): void
  /** 回填系统剪贴板（宿主走 Set(id)，非浏览器复制） */
  (e: 'copy'): void
  (e: 'togglePin'): void
  (e: 'delete'): void
}>()

const { copyWithToast } = useClipboard()

const dataUrl = computed(() => blobDataUrl(props.entry?.blobData))
const useCount = computed(() => props.entry?.useCount ?? 0)

function copyFilePath(path: string) {
  void copyWithToast(path, '路径已复制到剪贴板')
}
</script>

<template>
  <div class="cd">
    <header v-if="entry && !loading && !error" class="cd-head">
      <span class="cd-kind">
        <ClipGlyph :name="rowGlyph(entry)" :size="14" />
        {{ kindLabel(entry.kind) }}<template v-if="entry.manual"> · 固定片段</template><template v-if="entry.pinned"> · 已置顶</template>
      </span>
      <button type="button" class="cd-btn cd-close" title="关闭详情（Esc）" aria-label="关闭详情" @click="emit('close')">
        <ClipGlyph name="close" :size="14" />
      </button>
    </header>

    <div v-if="error" class="banner banner-error cd-error" role="alert">
      {{ error }}
      <button type="button" class="btn btn-small btn-secondary" @click="emit('retry')">重试</button>
    </div>

    <div v-else-if="loading" class="state-box" role="status">正在取回完整内容（DPAPI 解密）…</div>

    <template v-else-if="entry">
      <div v-if="entry.sensitive" class="banner banner-warn slim cd-sensitive" role="note">
        疑似含密钥/令牌：此条目不进 AI（MCP 检索）通道，也不会被语义搜索下发。
      </div>

      <div class="cd-body">
        <!-- 文本：等宽原样，不猜结构 -->
        <pre v-if="entry.kind === 'text'" class="cd-plain mono">{{ entry.text || '（空文本）' }}</pre>

        <!-- 图片：blob 预览 + 机器值注记 -->
        <div v-else-if="entry.kind === 'image'" class="cd-image">
          <img v-if="dataUrl" :src="dataUrl" class="cd-img" :alt="`剪贴板图片预览 ${entry.width ?? ''}×${entry.height ?? ''}`" />
          <div v-else class="state-box">图片数据未回填——blob 文件可能已被清理或超单条上限未入库。</div>
          <div v-if="entry.width || entry.height" class="cd-img-meta">
            {{ entry.width }}×{{ entry.height }} px · {{ fmtClipBytes(entry.byteSize) }}
          </div>
        </div>

        <!-- 文件：路径逐行，可逐条复制 -->
        <ul v-else-if="entry.kind === 'file'" class="cd-files">
          <li v-for="(p, i) in entry.files ?? []" :key="`${i}:${p}`" class="cd-file">
            <span class="mono cd-path" :title="p">{{ p }}</span>
            <button type="button" class="cd-btn cd-file-copy" title="仅复制这一行路径" aria-label="复制该路径" @click="copyFilePath(p)">
              <ClipGlyph name="copy" :size="13" />
            </button>
          </li>
          <li v-if="!(entry.files && entry.files.length)" class="state-box">文件列表为空。</li>
        </ul>
      </div>

      <footer class="cd-meta">
        <span>复制于 {{ fmtClipDateTime(entry.createdAt) }}</span>
        <span aria-hidden="true">·</span>
        <span class="cd-src" :title="entry.sourceApp ? `复制时前台窗口：${entry.sourceApp}` : undefined">来源 {{ entry.sourceApp || '未知窗口' }}</span>
        <span aria-hidden="true">·</span>
        <span>{{ fmtClipBytes(entry.byteSize) }}</span>
        <span v-if="useCount > 0" aria-hidden="true">·</span>
        <span v-if="useCount > 0">已使用 {{ useCount }} 次</span>
      </footer>

      <footer class="cd-foot">
        <button
          type="button"
          class="btn btn-primary btn-small cd-copy-all"
          title="回填到系统剪贴板（Ctrl+V 即贴）"
          @click="emit('copy')"
        >
          <ClipGlyph name="copy" :size="13" /> 复制到剪贴板
        </button>
        <button type="button" class="btn btn-secondary btn-small" @click="emit('togglePin')">
          <ClipGlyph name="pin" :size="13" /> {{ entry.pinned ? '取消置顶' : '固定置顶' }}
        </button>
        <span class="cd-foot-spacer" aria-hidden="true"></span>
        <button type="button" class="btn btn-ghost btn-small text-danger" @click="emit('delete')">
          <ClipGlyph name="trash" :size="13" /> 删除
        </button>
      </footer>
    </template>
  </div>
</template>

<style scoped>
/* 页面板：一条细边立起"正在读的这页"，不叠阴影 */
.cd {
  display: flex;
  flex-direction: column;
  gap: 10px;
  flex: 1;
  min-height: 0;
  min-width: 0;
  padding: 12px 14px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
}
.cd-head { display: flex; align-items: center; gap: 8px; min-width: 0; flex: none; }
.cd-kind {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted);
}
.cd-close { margin-left: auto; }

.cd-error { padding: 10px 12px; font-size: var(--text-sm); }
.cd-sensitive { padding: 8px 10px; font-size: var(--text-xs); }

.cd-body { flex: 1; min-height: 0; overflow-y: auto; max-width: 72ch; }
.cd-plain {
  margin: 0;
  font-size: var(--text-sm);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--color-text);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  padding: 10px 12px;
}

.cd-image { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
/* 棋盘浅底承半透明 PNG，图不外溢版心 */
.cd-img {
  max-width: 100%;
  max-height: min(46vh, 420px);
  object-fit: contain;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-micro);
  background: var(--surface-soft);
}
.cd-img-meta { font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }

.cd-files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.cd-file {
  display: flex; align-items: center; gap: 6px; min-width: 0;
  background: var(--surface-soft); border: 1px solid var(--color-border);
  border-radius: var(--radius-micro); padding: 4px 8px;
}
.cd-path { flex: 1; min-width: 0; font-size: var(--text-sm); word-break: break-all; }
.cd-file-copy { flex: none; }

.cd-meta {
  display: flex; align-items: center; gap: 6px; flex-wrap: wrap; flex: none;
  font-size: var(--text-xs); color: var(--color-text-subtle);
}
.cd-src { max-width: 46%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cd-foot { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; flex: none; }
.cd-foot-spacer { flex: 1; }

/* 窄版心：页脚主操作满行宽好够（叠放档由宿主 clip 容器触发，件内只顺排布） */
@container clip (max-width: 560px) {
  .cd-foot .btn { flex: 1; justify-content: center; }
}
</style>

<!-- 行内微钮基座（与 ClipRow .cr-btn 同谱：scoped 不跨件，各自持有不互抄） -->
<style scoped>
.cd-btn {
  background: none; border: none; cursor: pointer;
  color: var(--color-text-muted); padding: 3px 5px; border-radius: var(--radius-micro);
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.cd-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.cd-btn:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
</style>
