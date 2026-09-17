<script setup lang="ts">
// 提权重启入口：requireAdministrator 托管模块（BCU/Rufus/LiteMonitor）被
// 740 直拒后（elevateHint 文案），除了"手动关掉右键管理员重启"再给一键通道。
// 后端 RestartElevated 弹一次 UAC → 新实例提权后经 -route 回航当前页。
// 已提权运行（用户本来就右键管理员启动）时整个组件自动隐身。
import { onMounted, ref } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import UiButton from './ui/UiButton.vue'
import { useToast } from '../composables/useToast'
import { useConfirm } from '../composables/useConfirm'
import { getErrorMessage } from '../utils/errors'

const props = defineProps<{
  /** 重启后应直达的前端路由（如 "/ext/bcu"） */
  route: string
}>()

const { showToast } = useToast()
const { confirm } = useConfirm()

// 默认隐身：IsElevated 拿不到结果（非 Windows 语义/绑定异常）时不显示按钮，
// 宁缺毋滥——按钮只在"确认未提权"时才有一键价值。
const visible = ref(false)

onMounted(async () => {
  try {
    visible.value = !(await AppAPI.AppService.IsElevated())
  } catch (e) {
    console.warn('IsElevated failed:', getErrorMessage(e))
  }
})

// 确认经全局 useConfirm 单例发出（App.vue 顶层唯一实例），本组件不再自挂弹层。
// 确认后对话框即落定关闭；UAC 与退出流程的进度由 toast 承载，
// 失败（如 UAC 取消）toast 报错，用户可重新点击按钮再次发起。
async function restart() {
  const accepted = await confirm({
    title: '以管理员身份重启 Hanxi',
    description: 'Hanxi 将退出并弹出 UAC 提权对话框，请点击「是」。重启完成后自动回到当前页面；已启动的托管工具会随退出流程关闭，需要重新打开。',
    confirmLabel: '提权重启',
    tone: 'warning',
  })
  if (!accepted) return
  try {
    await AppAPI.AppService.RestartElevated(props.route)
    // 成功即进入 300ms 后的退出流程，toast 只是退出前的瞬时示意
    showToast('已在 UAC 授权，正在以管理员身份重启…', { duration: 4000 })
  } catch (e) {
    showToast(`提权重启失败: ${getErrorMessage(e)}`, { duration: 4000 })
  }
}
</script>

<template>
  <div v-if="visible" class="elevate-restart">
    <UiButton variant="primary" small @click="restart">↟ 以管理员身份重启</UiButton>
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
</style>
