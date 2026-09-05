<script setup lang="ts">
import { computed } from 'vue'
import { useToast } from '../../composables/useToast'
import { useClipboard } from '../../composables/useClipboard'

const props = defineProps<{
  tool: 'npm' | 'pnpm'
  installed: boolean
}>()

const { showToast } = useToast()
const { copy } = useClipboard()

const command = computed(() => props.tool === 'npm'
  ? 'npm install --global npm@latest'
  : 'pnpm self-update')

const sourceHint = computed(() => props.tool === 'npm'
  ? '如果 Node.js 由 nvm、fnm、Volta、Scoop 或 Chocolatey 管理，请优先使用原管理器，避免覆盖 shim 或写入错误的全局 prefix。'
  : '如果 pnpm 由 Corepack、Volta、Scoop、Chocolatey 或其他包管理器提供，请遵循原安装方式；self-update 并不适用于所有来源。')

async function copyCommand() {
  // 两级剪贴板策略收编进 useClipboard；成功文案逐字保留，失败文案收敛为固定话术
  //（useClipboard 无错误细节通道，原动态 message 罕达，登记为已接受微差）
  const ok = await copy(command.value)
  showToast(ok ? `已复制 ${props.tool} 升级命令` : '复制失败: 剪贴板不可用')
}
</script>

<template>
  <section class="upgrade-hint">
    <div class="hint-heading">
      <strong>手动升级提示</strong>
      <span class="manual-chip">Hanxi 不执行</span>
    </div>
    <p v-if="!installed" class="state-text">当前未检测到 {{ tool }}，以下命令仅供安装后参考。</p>
    <div class="command-row">
      <code class="command">{{ command }}</code>
      <button class="copy-button" type="button" @click="copyCommand">复制命令</button>
    </div>
    <p class="source-hint">{{ sourceHint }}</p>
    <p class="safety-hint">该命令会修改全局开发环境。请在你自己的终端确认路径与权限后执行；Hanxi 不自动运行，也不自动提权。</p>
  </section>
</template>

<style scoped>
.upgrade-hint { margin-top: 3px; padding-top: 11px; border-top: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 7px; }
.hint-heading { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
.hint-heading strong { color: var(--color-text); font-size: 12px; }
.manual-chip { padding: 1px 6px; border-radius: 10px; background: var(--surface-hover); color: var(--color-text-muted); font-size: 10px; }
.state-text, .source-hint, .safety-hint { margin: 0; color: var(--color-text-muted); font-size: 11px; line-height: 1.5; }
.command-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.command { flex: 1; min-width: 0; padding: 6px 8px; border-radius: 5px; background: var(--surface-soft); border: 1px solid var(--color-border); color: var(--color-text); font-family: var(--font-mono); font-size: 11px; overflow-wrap: anywhere; }
.copy-button { flex-shrink: 0; min-height: 28px; padding: 4px 10px; border: 1px solid var(--color-border); border-radius: 6px; background: transparent; color: var(--color-primary); font-size: 11px; cursor: pointer; }
.copy-button:hover { border-color: var(--color-primary); background: var(--surface-soft); }
/* 焦点环与 coarse-pointer 最小尺寸由 base.css 全局承载 */
.safety-hint { color: var(--state-warning); }
@media (max-width: 460px) { .command-row { align-items: stretch; flex-direction: column; } .copy-button { min-height: 36px; } }
</style>
