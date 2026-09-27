<script setup lang="ts">
// ClipDetail：右页面板的"正在读的那一页"（随手记 v2 阅读态同谱——列表即账，
// 这里落全貌；Get 拉回的完整条目按 kind 三分支）。
//
// R-F2 分支契约（workbench skill「正在读的那一页」版心语言）：
//   - text → 等宽原文进阅读框：正文档 13px（skill 底线：紧凑不许缩关键文本）、
//     行高 1.7、pre-wrap 折行、软底细边自滚——剪贴板内容语义未知，不做
//     Markdown 结构猜测（猜了只会把 URL/口令排花）；版心 72ch 封顶；
//   - image → blob 预览放井位托底（soft 底 + 细边 + 元素半径）：只给 max-*
//     约束不给 min，小图不拉伸（不糊）、大图收进容器（不溢），尺寸/体积
//     机器值注记在图下；blob 未回填（文件丢失/未压缩进 wire）如实占位，
//     不放空 <img>；
//   - file → 结构化清单：序号 + 基名（可辨层）+ 父目录（mono 机器层，可缺省）
//     两行制，每行独立"复制该路径"走浏览器剪贴板（useClipboard 统一回执）；
//     整表回填系统剪贴板仍走页脚 Set——表头一行明说这个分工。
//
// 页脚三档动作分明（skill：primary/secondary/destructive 不给同等视觉权重、
// 危险件空间上隔开）：主钮"复制到剪贴板"= Set 回填系统剪贴板（不抢焦点——
// 主界面没有浮层那次"原窗"上下文，粘回原窗是浮层的主场，此处不设 Paste 入口）；
// 次钮置顶随态翻面；危险删除经 spacer 推到行尾远端，确认闸在视图。
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
import {
  blobDataUrl,
  filePathDir,
  filePathName,
  fmtClipBytes,
  fmtClipDateTime,
  kindLabel,
  rowGlyph,
} from './clipboardFormat'

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
const files = computed(() => props.entry?.files ?? [])

function copyFilePath(path: string) {
  void copyWithToast(path, '路径已复制到剪贴板')
}
</script>

<template>
  <div class="cd">
    <header v-if="entry && !loading && !error" class="cd-head">
      <span class="cd-kind">
        <ClipGlyph :name="rowGlyph(entry)" :size="14" />
        {{ kindLabel(entry.kind) }}
      </span>
      <span v-if="entry.manual" class="chip chip-information cd-id">固定片段</span>
      <span v-if="entry.pinned" class="chip cd-id cd-id-pin">
        <ClipGlyph name="pin" :size="11" /> 已置顶
      </span>
      <button type="button" class="cd-btn cd-close" title="关闭详情（Esc）" aria-label="关闭详情" @click="emit('close')">
        <ClipGlyph name="close" :size="14" />
      </button>
    </header>

    <div v-if="error" class="banner banner-error cd-error" role="alert">
      {{ error }}
      <button type="button" class="btn btn-small btn-secondary" @click="emit('retry')">重试</button>
    </div>

    <div v-else-if="loading" class="state-box" role="status">正在取回完整内容（本机留底直读）…</div>

    <template v-else-if="entry">
      <div v-if="entry.sensitive" class="banner banner-warn slim cd-sensitive" role="note">
        疑似含密钥/令牌：此条目不进 AI（MCP 检索）通道，也不会被语义搜索下发。
      </div>

      <div class="cd-body">
        <!-- 文本：等宽原样，不猜结构 -->
        <pre v-if="entry.kind === 'text'" class="cd-plain mono">{{ entry.text || '（空文本）' }}</pre>

        <!-- 图片：井位托底预览 + 尺寸/体积机器值 -->
        <div v-else-if="entry.kind === 'image'" class="cd-image">
          <div v-if="dataUrl" class="cd-imgbox">
            <img :src="dataUrl" class="cd-img" :alt="`剪贴板图片预览 ${entry.width ?? ''}×${entry.height ?? ''}`" />
          </div>
          <div v-else class="state-box">图片数据未回填——blob 文件可能已被清理或超单条上限未入库。</div>
          <div v-if="entry.width || entry.height" class="cd-img-meta">
            {{ entry.width }}×{{ entry.height }} px · {{ fmtClipBytes(entry.byteSize) }}
          </div>
        </div>

        <!-- 文件：结构化两行制清单，逐条可复制 -->
        <div v-else-if="entry.kind === 'file'" class="cd-fileswrap">
          <div v-if="files.length" class="cd-files-head">
            <span class="cd-files-count">共 {{ files.length }} 个文件</span>
            <span class="cd-files-hint">逐行钮只复制单条；页脚钮整表回填</span>
          </div>
          <ul v-if="files.length" class="cd-files">
            <li v-for="(p, i) in files" :key="`${i}:${p}`" class="cd-file">
              <span class="cd-file-idx mono" aria-hidden="true">{{ i + 1 }}</span>
              <span class="cd-file-main">
                <span class="cd-file-name" :title="p">{{ filePathName(p) || p }}</span>
                <span v-if="filePathDir(p)" class="cd-file-dir mono">{{ filePathDir(p) }}</span>
              </span>
              <button type="button" class="cd-btn cd-file-copy" title="仅复制这一行路径" aria-label="复制该路径" @click="copyFilePath(p)">
                <ClipGlyph name="copy" :size="13" />
              </button>
            </li>
          </ul>
          <div v-else class="state-box">文件列表为空。</div>
        </div>
      </div>

      <footer class="cd-meta">
        <span class="cd-meta-item">复制于 <span class="mono">{{ fmtClipDateTime(entry.createdAt) }}</span></span>
        <span class="cd-meta-item">来源 <span class="cd-src" :title="entry.sourceApp ? `复制时前台窗口：${entry.sourceApp}` : undefined">{{ entry.sourceApp || '未知窗口' }}</span></span>
        <span class="cd-meta-item">体积 <span class="mono">{{ fmtClipBytes(entry.byteSize) }}</span></span>
        <span v-if="useCount > 0" class="cd-meta-item">已使用 <span class="mono">{{ useCount }}</span> 次</span>
      </footer>

      <footer class="cd-foot">
        <button
          type="button"
          class="btn btn-primary btn-small cd-copy-all"
          title="回填到系统剪贴板（Ctrl+V 即贴），不抢其它窗口焦点"
          @click="emit('copy')"
        >
          <ClipGlyph name="copy" :size="13" /> 复制到剪贴板
        </button>
        <button type="button" class="btn btn-secondary btn-small" @click="emit('togglePin')">
          <ClipGlyph name="pin" :size="13" /> {{ entry.pinned ? '取消置顶' : '固定置顶' }}
        </button>
        <span class="cd-foot-spacer" aria-hidden="true"></span>
        <button type="button" class="btn btn-ghost btn-small text-danger" title="删除此条（图片连带删 blob），需确认" @click="emit('delete')">
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
.cd-id { flex: none; font-size: var(--text-micro); padding: 1px 8px; }
.cd-id-pin { background: var(--color-primary-soft); color: var(--color-primary); gap: 3px; }
.cd-close { margin-left: auto; }

.cd-error { padding: 10px 12px; font-size: var(--text-sm); }
.cd-sensitive { padding: 8px 10px; font-size: var(--text-xs); }

.cd-body { flex: 1; min-height: 0; overflow-y: auto; max-width: 72ch; }
/* 阅读框：正文档字号不缩（skill 底线）、行高放宽、软底细边自滚 */
.cd-plain {
  margin: 0;
  font-size: var(--text-base);
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--color-text);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  padding: 12px 14px;
}

.cd-image { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
/* 井位托底：细边 + 元素半径 + soft 底承半透明 PNG；
   图只受 max-* 约束——小图不拉伸（不糊）、大图收进容器（不溢） */
.cd-imgbox {
  display: inline-flex; max-width: 100%;
  padding: 10px;
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
}
.cd-img {
  max-width: 100%;
  max-height: min(56vh, 560px);
  object-fit: contain;
  display: block;
}
.cd-img-meta { font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }

.cd-fileswrap { display: flex; flex-direction: column; gap: 6px; }
.cd-files-head { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--color-text-subtle); }
.cd-files-count { font-weight: 600; color: var(--color-text-muted); font-variant-numeric: tabular-nums; }
.cd-files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.cd-file {
  display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 8px;
  min-width: 0;
  background: var(--surface-soft); border: 1px solid var(--color-border);
  border-radius: var(--radius-control); padding: 6px 8px 6px 10px;
}
.cd-file-idx { font-size: var(--text-micro); color: var(--color-text-subtle); min-width: 16px; text-align: right; }
.cd-file-main { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.cd-file-name { font-size: var(--text-sm); font-weight: 600; color: var(--color-text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* 目录行截尾不折行（完整路径恒在文件名的 title 上，不靠这行示全貌） */
.cd-file-dir { font-size: var(--text-xs); color: var(--color-text-subtle); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cd-file-copy { flex: none; }

/* 脚注账目行：与正文用细线分隔，值走 mono */
.cd-meta {
  display: flex; align-items: center; gap: 4px 14px; flex-wrap: wrap; flex: none;
  padding-top: 8px; border-top: 1px solid var(--color-border);
  font-size: var(--text-xs); color: var(--color-text-subtle);
}
.cd-meta-item { display: inline-flex; align-items: baseline; gap: 4px; min-width: 0; }
.cd-meta-item .mono { font-size: var(--text-xs); color: var(--color-text-muted); }
.cd-src { max-width: 46ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-muted); }

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
