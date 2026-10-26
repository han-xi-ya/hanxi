<script setup lang="ts">
// MemoSheetSettings：悬浮速记卡热键配置块（N16 B 批能力，MemoView v2 拆分批
// 自视图索引栏底原样抽出，语义零改动）。
//
// 自治面：实况自拉（GetQuickSheetState）、改键自写（SetQuickSheetHotkey）、
// 失败回滚对齐与播报全在本件——视图只负责在栏底挂上它。实况拉不到整段收起
// （不给假配置可编）；热键未在位的警示横幅恒外露（收合态也可见）。
// 后端事务自带冲突回滚（旧键全程活着、配置不动），失败原文上浮中文指引；
// 唤出通道（ShowQuickSheet，菜单「浮窗速记」）不归本件，留视图溢出菜单。
// 皮逐字迁自宿主拆分前口径（拆分不动相）：sheet-* 家族与 memo-sheet-settings
// 容器、栏底 banner-warn 微调同迁。
import { computed, onMounted, ref } from 'vue'
import * as MemoAPI from '../../../bindings/hanxi/internal/modules/memo'
import type { QuickSheetState } from '../../../bindings/hanxi/internal/modules/memo/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import UiButton from '../ui/UiButton.vue'
import MemoGlyph from './MemoGlyph.vue'

const { showToast } = useToast()

// QuickSheetState 用绑定官方型别（生成件已含该 typedef，旧版本地同构声明退役）。
const sheetState = ref<QuickSheetState | null>(null)
const sheetHotkeyDraft = ref('')
const sheetSaving = ref(false)
const sheetOpen = ref(false)

async function refreshQuickSheet() {
  try {
    const st = await MemoAPI.MemoService.GetQuickSheetState()
    sheetState.value = st
    sheetHotkeyDraft.value = st?.hotkey ?? ''
  } catch {
    sheetState.value = null // 拉不到实况就收整段，不给假配置可编
  }
}

// 速记热键未在位（开机被抢注等）：如实横幅提示改键或走菜单唤出（msgboard 先例语义）。
const sheetHotkeyMissing = computed(
  () => !!sheetState.value && sheetState.value.hotkey !== '' && !sheetState.value.hotkeyActive,
)

async function saveQuickHotkey() {
  if (sheetSaving.value || !sheetState.value) return
  sheetSaving.value = true
  try {
    // 后端事务自带冲突回滚（旧键全程活着、配置不动），失败原文上浮中文指引
    await MemoAPI.MemoService.SetQuickSheetHotkey(sheetHotkeyDraft.value.trim())
    await refreshQuickSheet()
    showToast('速记热键已更新')
  } catch (err: unknown) {
    const msg = getErrorMessage(err)
    showToast(`热键设置失败: ${msg}`)
    sheetHotkeyDraft.value = sheetState.value.hotkey // 回滚后草稿对齐生效值
    await refreshQuickSheet()
  } finally {
    sheetSaving.value = false
  }
}

onMounted(refreshQuickSheet)
</script>

<template>
  <!-- 实况拉不到整段收起（不给假配置可编）；栏底折叠：低频件不占独立大面板 -->
  <section v-if="sheetState" class="memo-sheet-settings" aria-label="悬浮速记卡">
    <button class="sheet-disclosure" :aria-expanded="sheetOpen" @click="sheetOpen = !sheetOpen">
      <span class="sheet-disc-name">悬浮速记卡 · 全局热键</span>
      <span class="sheet-disc-now mono">{{ sheetState.hotkey || '—' }}</span>
      <span v-if="sheetState.hotkey === ''" class="chip chip-neutral memo-hotkey-off">热键已停用</span>
      <MemoGlyph name="chevron" :size="13" class="sheet-disc-chevron" />
    </button>
    <div v-show="sheetOpen" class="sheet-body">
      <p class="sheet-desc">
        随处唤出极简速记小窗：回车即存、失焦即收。留空 = 停用（菜单「浮窗速记」不受影响）；改键即时注册，被占用会报错并保旧键。
      </p>
      <div class="sheet-row">
        <input
          v-model="sheetHotkeyDraft"
          class="text-input memo-hotkey-input mono"
          type="text"
          placeholder="Ctrl+Alt+N"
          aria-label="速记卡全局热键"
        />
        <UiButton variant="secondary" small :disabled="sheetSaving" @click="saveQuickHotkey">
          {{ sheetSaving ? '保存中…' : '保存热键' }}
        </UiButton>
      </div>
    </div>
    <div v-if="sheetHotkeyMissing" class="banner banner-warn" role="note">
      热键「{{ sheetState.hotkey }}」未在位：多半已被其它程序抢占。换个组合保存，或留空停用——期间可用菜单「浮窗速记」唤出。
    </div>
  </section>
</template>

<style scoped>
/* 栏底折叠配置（低频件，收起态一行字即够；未在位警示恒外露）——逐字迁自宿主 */
.memo-sheet-settings { margin-top: 8px; border-top: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 6px; }
.sheet-disclosure {
  display: flex; align-items: center; gap: 6px; width: 100%;
  background: none; border: none; cursor: pointer; padding: 8px 2px 6px;
  font-size: var(--text-xs); color: var(--color-text-muted);
}
.sheet-disclosure:hover { color: var(--color-text); }
.sheet-disc-name { font-weight: 600; }
.sheet-disc-now { margin-left: auto; }
.sheet-disc-chevron { transition: transform var(--motion-fast) ease; }
.sheet-disclosure[aria-expanded="true"] .sheet-disc-chevron { transform: rotate(180deg); }
.sheet-body { display: flex; flex-direction: column; gap: 6px; padding: 0 2px 4px; }
.sheet-desc { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.6; }
.sheet-row { display: flex; align-items: center; gap: 8px; }
.memo-hotkey-input { width: 150px; flex: none; }
.memo-hotkey-off { flex: none; }
.memo-sheet-settings .banner-warn { padding: 8px 10px; font-size: var(--text-xs); }
</style>
