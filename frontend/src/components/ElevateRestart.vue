<script setup lang="ts">
// 提权重启入口：requireAdministrator 托管模块（BCU/Rufus/LiteMonitor）被
// 740 直拒后（elevateHint 文案），除了"手动关掉右键管理员重启"再给一键通道。
// 后端 RestartElevated 弹一次 UAC → 新实例提权后经 -route 回航当前页。
// 已提权运行（用户本来就右键管理员启动）时整个组件自动隐身。
import { onMounted, ref } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import UiButton from './ui/UiButton.vue'
import AppIcon from './ui/AppIcon.vue'
import { useToast } from '../composables/useToast'
import { useConfirm } from '../composables/useConfirm'
import { getErrorMessage } from '../utils/errors'

const props = defineProps<{
  /** 重启后应直达的前端路由（如 "/ext/bcu"） */
  route: string
}>()

const { showToast, showErrorToast } = useToast()
const { confirm } = useConfirm()

// 默认隐身：IsElevated 拿不到结果（非 Windows 语义/绑定异常）时不显示按钮，
// 宁缺毋滥——按钮只在"确认未提权"时才有一键价值。
const visible = ref(false)

// busy：RestartElevated RPC 在途（按钮禁用 + aria-busy + spinner，防连点二次发起）。
// confirming：确认框挂起在途（此时按钮尚未 disabled，靠本门禁挡住重复点击再开确认）。
const busy = ref(false)
const confirming = ref(false)

onMounted(async () => {
  try {
    visible.value = !(await AppAPI.AppService.IsElevated())
  } catch (e) {
    console.warn('IsElevated failed:', getErrorMessage(e))
  }
})

// 确认经全局 useConfirm 单例发出（App.vue 顶层唯一实例），本组件不再自挂弹层。
// 确认后对话框即落定关闭；UAC 与退出流程的进度由 toast 承载，
// 失败（如 UAC 取消）经 showErrorToast 报错（8s 保活契约），用户可重新点击再次发起。
async function restart() {
  // 双门禁：确认在途或 RPC 在途都直接 return，杜绝连点二次发起。
  if (busy.value || confirming.value) return
  confirming.value = true
  let accepted = false
  try {
    accepted = await confirm({
      title: '以管理员身份重启 Hanxi',
      description: 'Hanxi 将退出并弹出 UAC 提权对话框，请点击「是」。重启完成后自动回到当前页面；开了「随 Hanxi 关闭」的托管工具会随退出流程关闭，默认独立运行的工具不受影响。',
      confirmLabel: '提权重启',
      tone: 'warning',
    })
  } finally {
    confirming.value = false
  }
  if (!accepted) return
  busy.value = true
  try {
    await AppAPI.AppService.RestartElevated(props.route)
    // 成功即进入 300ms 后的退出流程，toast 只是退出前的瞬时示意
    showToast('已在 UAC 授权，正在以管理员身份重启…', { duration: 4000 })
  } catch (e) {
    // 失败回执走 showErrorToast 默认 8s 保活（TROUBLESHOOTING #49），禁手写 duration
    showErrorToast(`提权重启失败: ${getErrorMessage(e)}`)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div v-if="visible" class="elevate-restart">
    <UiButton variant="primary" small :disabled="busy" :aria-busy="busy" @click="restart">
      <AppIcon name="shield" :size="14" /> 以管理员身份重启<span v-if="busy" class="elevate-spin" aria-hidden="true"></span>
    </UiButton>
    <span class="elevate-note">将弹出一次 UAC 确认；重启后自动回到本页。正在运行的托管工具会随本次重启退出，需重新启动。</span>
  </div>
</template>

<style scoped>
.elevate-restart {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
}
.elevate-note {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
}

/* 忙时内联 spinner：仅 RPC 在途才转（动效纪律）；文案恒定保宽，尾部追加 spinner 表忙。
   减弱动效由 base.css 全局块兜底，此处再显式停转一份。 */
.elevate-spin {
  width: 12px; height: 12px; flex: none;
  border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%;
  animation: elevate-spin 750ms linear infinite;
}
@keyframes elevate-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .elevate-spin { animation: none; } }
</style>
