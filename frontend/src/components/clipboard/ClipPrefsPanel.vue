<script setup lang="ts">
// ClipPrefsPanel：「记录设置」折叠面板（契约 §12 v1.8，借鉴 TieZ 三件套的
// persistent_limit / app_cleanup_policies / delete_after_paste）。
//
// 交互裁决（显式保存，不防抖自动落盘）：SetPrefs 是**整表替换**语义——漏发
// 域=回落默认（后端 normalize 钉死），数值域输入中途态（"200" 打到一半是
// "2"）一旦被自动保存会静默钳出用户没按下的键。本地持全量草稿 + 显式「保存」
// 是唯一诚实通路；「放弃修改」回卷到上次生效值。
//
// 钳制如实回显：保存成功后用后端返回的**生效值**覆写草稿（越界值展示钳后
// 形，所见即所运行）；坏正则整表拒绝，红字逐字上浮（"以为设了其实没设"
// 不接受——与后端 fail-loud 同谱）。
//
// 域覆盖：容量/粘贴后即删/排除表/按应用保留（提交 1）+ 预览遮蔽（提交 3）；
// 队列键位走独立 SetQueueHotkey 事务域（不进本面板整表写路，见 Prefs 头注）。
// 草稿字段全量透传（整表语义要求全量回发，未渲染域不丢别人的值）。
import { computed, onMounted, ref } from 'vue'
import type { ClipPrefs } from '../../types/clipboard'
import { clipboardGetPrefs, clipboardSetPrefs } from '../../adapters/clipboard'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'

const emit = defineEmits<{
  /** 保存成功（宿主重拉状态与列表——降容静默裁后账本已变） */
  (e: 'saved'): void
  /** 面板收起意图（面板自身无关闭钮，预留对位） */
  (e: 'close'): void
}>()

const { showToast, showErrorToast } = useToast()

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const saveError = ref('')

const draft = ref<ClipPrefs | null>(null)
/** 上次生效值深拷贝（dirty 判据与「放弃修改」的回卷源） */
let baseline = ''

function clone(p: ClipPrefs): ClipPrefs {
  return { ...p, excludedFactory: [...p.excludedFactory], excludedCustom: [...p.excludedCustom], maskKinds: [...p.maskKinds], maskCustomPatterns: [...p.maskCustomPatterns], appRetentionDays: { ...p.appRetentionDays } }
}

// 有效草稿：行编辑只在 rows ref 里，须折进 dirty 判定与保存载荷——
// 漏折则"加了行但保存钮不亮"（行改动态对整表语义不可见）。
const effectiveDraft = computed<ClipPrefs | null>(() => {
  if (!draft.value) return null
  return { ...draft.value, appRetentionDays: rowsToMap() }
})
const isDirty = computed(() => !!effectiveDraft.value && JSON.stringify(effectiveDraft.value) !== baseline)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const p = await clipboardGetPrefs()
    draft.value = clone(p)
    baseline = JSON.stringify(p)
    syncFromDraft() // 存量按应用规则展开成编辑行——漏这步首次保存会折空 map
  } catch (err: unknown) {
    loadError.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!draft.value || saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const echo = await clipboardSetPrefs(draft.value)
    // 钳制如实回显：以后端生效值覆写草稿并重设基线（天数钳制也回显到行）
    draft.value = clone(echo)
    baseline = JSON.stringify(echo)
    syncFromDraft()
    showToast('记录设置已保存并即时生效')
    emit('saved')
  } catch (err: unknown) {
    saveError.value = getErrorMessage(err)
    showErrorToast(`设置保存失败: ${saveError.value}`)
  } finally {
    saving.value = false
  }
}

function revert() {
  if (!draft.value) return
  try {
    draft.value = clone(JSON.parse(baseline) as ClipPrefs)
  } catch {
    void load()
  }
  saveError.value = ''
}

// ---- 排除表（custom 域）编辑 ----
const newExe = ref('')

function addExcluded() {
  const v = newExe.value.trim().toLowerCase()
  if (!v || !draft.value) return
  if (!draft.value.excludedCustom.includes(v)) draft.value.excludedCustom.push(v)
  newExe.value = ''
}

function removeExcluded(i: number) {
  draft.value?.excludedCustom.splice(i, 1)
}

// ---- 按应用保留天数行编辑器（exe→days 映射展开成行，保存时回卷成 map） ----
// days 放宽：number 输入框在 .number 解析失败时会落回原字符串/空串，
// rowsToMap 的 typeof 守卫统一折叠（见上）。
interface RetentionRow { exe: string; days: number | string | null }
const rows = ref<RetentionRow[]>([])

function rowsToMap(): Record<string, number> {
  const m: Record<string, number> = {}
  for (const r of rows.value) {
    const exe = r.exe.trim().toLowerCase()
    if (!exe) continue
    // 空输入/.number 解析失败（''/NaN）一律折 0——后端 normalize "天数≤0=删
    // 该规则"同款语义；非整截断，防小数混进 Go int 字段。
    const d = typeof r.days === 'number' && Number.isFinite(r.days) ? Math.floor(r.days) : 0
    m[exe] = d
  }
  return m
}

function mapToRows(m: Record<string, number>): RetentionRow[] {
  return Object.entries(m).map(([exe, days]) => ({ exe, days }))
}

function addRow() {
  rows.value.push({ exe: '', days: 7 })
}

function removeRow(i: number) {
  rows.value.splice(i, 1)
}

// ---- 预览遮蔽（mask 域，提交 3） ----
// 内置 kind 固定序渲染；点击翻 included。后端 normalize 会重排为固定序并
// 剔未知，本地序只影响点击手感不影响语义。
const MASK_KIND_ORDER = ['phone', 'idcard', 'email', 'secret'] as const
const MASK_KIND_LABELS: Record<string, string> = { phone: '手机号', idcard: '证件号', email: '邮箱', secret: '密钥令牌' }

function toggleKind(k: string) {
  if (!draft.value) return
  const i = draft.value.maskKinds.indexOf(k)
  if (i >= 0) draft.value.maskKinds.splice(i, 1)
  else draft.value.maskKinds.push(k)
}

const newPattern = ref('')

function addPattern() {
  const v = newPattern.value.trim()
  if (!v || !draft.value) return
  if (!draft.value.maskCustomPatterns.includes(v)) draft.value.maskCustomPatterns.push(v)
  newPattern.value = ''
}

function removePattern(i: number) {
  draft.value?.maskCustomPatterns.splice(i, 1)
}

// draft ↔ rows 同步：装载/回卷后重建行；保存前把行折回 map。
function syncFromDraft() {
  if (draft.value) rows.value = mapToRows(draft.value.appRetentionDays)
}

onMounted(() => { void load() })

async function saveAll() {
  if (!effectiveDraft.value) return
  draft.value = clone(effectiveDraft.value)
  await save()
}

function revertAll() {
  revert()
  syncFromDraft()
}
</script>

<template>
  <section class="cp-panel" aria-label="记录设置">
    <div v-if="loading" class="state-box" role="status">正在读取记录设置…</div>

    <div v-else-if="loadError" class="banner banner-error" role="alert">
      读取设置失败: {{ loadError }}
      <button type="button" class="btn btn-small btn-secondary" @click="load">重试</button>
    </div>

    <template v-else-if="draft">
      <div class="cp-groups">
        <!-- 容量与行为 -->
        <div class="cp-group">
          <h3 class="cp-title">容量与行为</h3>
          <div class="cp-field">
            <label class="cp-label" for="cp-max-entries">历史最大条数</label>
            <input
              id="cp-max-entries" v-model.number="draft.maxEntries" class="text-input mono cp-num"
              type="number" min="50" max="2000" step="50"
            />
            <p class="cp-hint">50–2000 条，越界保存时钳制并如实回显；留空/0 回默认 500</p>
          </div>
          <div class="cp-field">
            <label class="cp-label" for="cp-max-blob">图片总量上限（MiB）</label>
            <input
              id="cp-max-blob" v-model.number="draft.maxBlobMB" class="text-input mono cp-num"
              type="number" min="10" max="1024" step="10"
            />
            <p class="cp-hint">10–1024 MiB；留空/0 回默认 100。调小立即裁图片条目（置顶豁免）</p>
          </div>
          <label class="cp-check">
            <input type="checkbox" class="cp-switch" v-model="draft.deleteAfterPaste" />
            粘贴成功后立即删除该条目（置顶与固定片段豁免）
          </label>
        </div>

        <!-- 排除应用 -->
        <div class="cp-group">
          <h3 class="cp-title">排除的应用（其复制不入库）</h3>
          <div class="cp-factory" aria-label="出厂默认排除表">
            <span v-for="exe in draft.excludedFactory" :key="exe" class="cp-chip cp-chip-locked" :title="`${exe}：出厂项，不可移除`">
              🔒 <span class="cp-chip-txt">{{ exe }}</span>
            </span>
          </div>
          <div class="cp-factory">
            <span v-for="(exe, i) in draft.excludedCustom" :key="exe" class="cp-chip">
              <span class="cp-chip-txt" :title="exe">{{ exe }}</span>
              <button type="button" class="cp-chip-x" :aria-label="`移除 ${exe}`" @click="removeExcluded(i)">✕</button>
            </span>
          </div>
          <div class="cp-addrow">
            <input
              v-model="newExe" class="text-input mono cp-add-input" type="text"
              placeholder="追加进程名，如 wechat 或 chrome.exe" aria-label="追加排除应用"
              @keydown.enter.prevent="addExcluded"
            />
            <button type="button" class="btn btn-secondary btn-small" :disabled="!newExe.trim()" @click="addExcluded">添加</button>
          </div>
          <p class="cp-hint">名称自动转小写补 .exe；进程名含 "password" 的一律排除，与本表无关、不可关</p>
        </div>

        <!-- 按应用保留天数 -->
        <div class="cp-group">
          <h3 class="cp-title">按应用保留天数</h3>
          <div v-if="!rows.length" class="cp-hint">未配置——只有列出的应用会被按天清扫，其余仅受容量闸约束。</div>
          <div v-for="(r, i) in rows" :key="i" class="cp-retention-row">
            <input v-model="r.exe" class="text-input mono cp-ret-exe" type="text" placeholder="app.exe" :aria-label="`第 ${i + 1} 条应用名`" />
            <input v-model.number="r.days" class="text-input mono cp-ret-days" type="number" min="0" max="365" :aria-label="`第 ${i + 1} 条保留天数`" />
            <span class="cp-ret-unit">天</span>
            <button type="button" class="btn btn-ghost btn-small cp-ret-del" :aria-label="`移除第 ${i + 1} 条`" @click="removeRow(i)">移除</button>
          </div>
          <button type="button" class="btn btn-secondary btn-small" @click="addRow">添加应用</button>
          <p class="cp-hint">自「最近一次复制或粘贴」起算 N 天后清理（置顶/片段豁免；留空天数=删除该规则）</p>
        </div>

        <!-- 预览遮蔽（提交 3） -->
        <div class="cp-group">
          <h3 class="cp-title">预览遮蔽</h3>
          <div class="cp-kinds" role="group" aria-label="内置遮蔽类别">
            <button
              v-for="k in MASK_KIND_ORDER"
              :key="k"
              type="button"
              class="cp-kind"
              :class="{ active: draft.maskKinds.includes(k) }"
              :aria-pressed="draft.maskKinds.includes(k) ? 'true' : 'false'"
              @click="toggleKind(k)"
            >{{ MASK_KIND_LABELS[k] }}</button>
          </div>
          <p class="cp-hint">只遮列表与浮层的摘要行；搜索原文照样命中、详情正文与粘贴不受影响（敏感条目正文另有默认遮罩）</p>
          <div class="cp-factory">
            <span v-for="(pat, i) in draft.maskCustomPatterns" :key="pat" class="cp-chip">
              <span class="cp-chip-txt" :title="pat">{{ pat }}</span>
              <button type="button" class="cp-chip-x" :aria-label="`移除规则 ${pat}`" @click="removePattern(i)">✕</button>
            </span>
          </div>
          <div class="cp-addrow">
            <input
              v-model="newPattern" class="text-input mono cp-add-input" type="text"
              placeholder="自定义正则，如 订单号\d{8}" aria-label="新增自定义遮蔽规则"
              @keydown.enter.prevent="addPattern"
            />
            <button type="button" class="btn btn-secondary btn-small" :disabled="!newPattern.trim()" @click="addPattern">添加</button>
          </div>
          <p class="cp-hint">命中整段替换为 ****；保存即校验，坏正则整表拒绝并红字标出</p>
        </div>
      </div>

      <div class="cp-foot">
        <!-- 错误与 dirty 并显（拒绝保存后既见红字又见"有未保存"，不互斥遮蔽） -->
        <span v-if="saveError" class="cp-error" role="alert">{{ saveError }}</span>
        <span class="cp-dirty">{{ isDirty ? '有未保存的修改' : '设置均已生效' }}</span>
        <div class="cp-foot-btns">
          <button type="button" class="btn btn-secondary btn-small" :disabled="!isDirty || saving" @click="revertAll">放弃修改</button>
          <button type="button" class="btn btn-primary btn-small" :disabled="!isDirty || saving" @click="saveAll">
            {{ saving ? '保存中…' : '保存设置' }}
          </button>
        </div>
      </div>
    </template>

    <div v-else class="state-box">设置数据不可用，请关闭面板后重试。</div>
  </section>
</template>

<style scoped>
/* 面板形态：账本页下的低频配置区——挂 --surface-soft 浅底与细描边（页面
   级 panel 容器语义，非浮层卡），不重写共享原子类（skill 纪律） */
.cp-panel {
  container: cprefs / inline-size;
  display: flex; flex-direction: column; gap: 10px;
  border: 1px solid var(--color-border); border-radius: var(--radius-element);
  background: var(--surface-soft); padding: 12px 14px;
}
.cp-groups { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 14px; min-width: 0; }
.cp-group { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.cp-title { margin: 0 0 2px; font-size: var(--text-sm); font-weight: 600; color: var(--color-text); }
.cp-field { display: flex; flex-direction: column; gap: 3px; }
.cp-label { font-size: var(--text-sm); color: var(--color-text-muted); }
.cp-num { width: 140px; max-width: 100%; }
.cp-hint { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.6; }
.cp-check { display: flex; align-items: center; gap: 8px; font-size: var(--text-sm); color: var(--color-text); cursor: pointer; }
.cp-switch { width: 16px; height: 16px; accent-color: var(--color-primary); flex: none; cursor: pointer; }

/* 排除表两列 chip：出厂锁定态（🔒+灰底不可点）与 custom 可删态（✕ 挂钮内） */
.cp-factory { display: flex; flex-wrap: wrap; gap: 4px; }
.cp-chip {
  display: inline-flex; align-items: center; gap: 4px; max-width: 100%;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-pill);
  padding: 1px 8px; font-size: var(--text-xs); color: var(--color-text-muted);
  font-family: var(--font-mono);
}
/* 长正则/长进程名在窄列里收 ellipsis 不撑爆（title 恒给全文） */
.cp-chip-txt { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cp-chip-locked { background: var(--surface-hover); color: var(--color-text-subtle); }
.cp-chip-x {
  background: none; border: none; cursor: pointer; color: var(--color-text-subtle);
  font-size: var(--text-micro); padding: 0 2px; flex: none;
}
.cp-chip-x:hover { color: var(--state-danger); }
.cp-chip-x:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; border-radius: 2px; }

.cp-addrow { display: flex; gap: 6px; min-width: 0; }
.cp-add-input { flex: 1 1 120px; min-width: 0; }

/* 遮蔽 kind 开关丸：受控 aria-pressed，点亮=生效（色+文字双通道） */
.cp-kinds { display: flex; flex-wrap: wrap; gap: 4px; }
.cp-kind {
  display: inline-flex; align-items: center; gap: 4px; flex: none;
  background: var(--surface-panel); border: 1px solid var(--color-border); color: var(--color-text-muted);
  padding: 2px 10px; border-radius: var(--radius-pill); font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.cp-kind:hover { background: var(--surface-hover); color: var(--color-text); }
.cp-kind.active { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-on-primary); }
.cp-kind:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }

.cp-retention-row { display: flex; align-items: center; gap: 6px; min-width: 0; }
.cp-ret-exe { flex: 1 1 0; min-width: 0; }
.cp-ret-days { width: 72px; flex: none; }
.cp-ret-unit { font-size: var(--text-xs); color: var(--color-text-subtle); flex: none; }
.cp-ret-del { flex: none; color: var(--state-danger); }

.cp-foot { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; border-top: 1px solid var(--color-border); padding-top: 8px; }
.cp-foot-btns { margin-left: auto; display: flex; gap: 6px; }
.cp-dirty { font-size: var(--text-xs); color: var(--color-text-subtle); }
.cp-error { font-size: var(--text-xs); color: var(--state-danger); max-width: 60ch; line-height: 1.6; }

@container cprefs (max-width: 560px) {
  .cp-groups { grid-template-columns: minmax(0, 1fr); }
}
</style>
