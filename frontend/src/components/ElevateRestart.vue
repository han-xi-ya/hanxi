<script setup lang="ts">
// 提权重启入口：requireAdministrator 托管模块（BCU/Rufus/LiteMonitor）被
// 740 直拒后（elevateHint 文案），除了"手动关掉右键管理员重启"再给一键通道。
// 后端 RestartElevated 弹一次 UAC → 新实例提权后经 -route 回航当前页。
// 已提权运行（用户本来就右键管理员启动）时整个组件自动隐身。
import { onMounted, ref } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import UiButton from './ui/UiButton.vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const props = defineProps<{
  /** 重启后应直达的前端路由（如 "/ext/bcu"） */
  route: string
}>()

const { showToast } = useToast()

// 默认隐身：IsElevated 拿不到结果（非 Windows 语义/绑定异常）时不显示按钮，
// 宁缺毋滥——按钮只在"确认未提权"时才有一键价值。
const visible = ref(false)
const confirming = ref(false)
const busy = ref(false)

onMounted(async () => {
  try {
    visible.value = !(await AppAPI.AppService.IsElevated())
  } catch (e) {
    console.warn('IsElevated failed:', getErrorMessage(e))
  }
})

async function restart() {
  busy.value = true
  try {
    await AppAPI.AppService.RestartElevated(props.route)
    // 成功即进入 300ms 后的退出流程，toast 只是退出前的瞬时示意
    showToast('已在 UAC 授权，正在以管理员身份重启…', { duration: 4000 })
  } catch (e) {
    confirming.value = false
    showToast(`提权重启失败: ${getErrorMessage(e)}`, { duration: 4000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div v-if="visible" class="elevate-restart">
    <UiButton variant="primary" small @click="confirming = true">↟ 以管理员身份重启</UiButton>
    <span class="elevate-note">将弹出一次 UAC 确认；重启后自动回到本页。正在运行的托管工具会随本次重启退出，需重新启动。</span>
    <ConfirmDialog
      :open="confirming"
      title="以管理员身份重启 Hanxi"
      description="Hanxi 将退出并弹出 UAC 提权对话框，请点击「是」。重启完成后自动回到当前页面；已启动的托管工具会随退出流程关闭，需要重新打开。"
      confirm-label="提权重启"
      cancel-label="取消"
      tone="warning"
      :busy="busy"
      @confirm="restart"
      @cancel="confirming = false"
      @update:open="confirming = $event"
    />
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
  font-size: 12px;
  color: var(--color-text-muted);
}
</style>
