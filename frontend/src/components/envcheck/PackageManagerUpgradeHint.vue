<script setup lang="ts">
import { computed } from 'vue'
import { useToast } from '../../composables/useToast'
import { getErrorMessage } from '../../utils/errors'

const props = defineProps<{
  tool: 'npm' | 'pnpm'
  installed: boolean
}>()

const { showToast } = useToast()

const command = computed(() => props.tool === 'npm'
  ? 'npm install --global npm@latest'
  : 'pnpm self-update')

const sourceHint = computed(() => props.tool === 'npm'
  ? '如果 Node.js 由 nvm、fnm、Volta、Scoop 或 Chocolatey 管理，请优先使用原管理器，避免覆盖 shim 或写入错误的全局 prefix。'
  : '如果 pnpm 由 Corepack、Volta、Scoop、Chocolatey 或其他包管理器提供，请遵循原安装方式；self-update 并不适用于所有来源。')

function fallbackCopy(text: string): boolean {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand('copy')
  document.body.removeChild(textarea)
  return copied
}

async function copyCommand() {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(command.value)
    } else if (!fallbackCopy(command.value)) {
      throw new Error('剪贴板接口不可用')
    }
    showToast(`已复制 ${props.tool} 升级命令`)
  } catch (error) {
    showToast(`复制失败: ${getErrorMessage(error)}`)
  }
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
.upgrade-hint { margin-top: 3px; padding-top: 11px; border-top: 1px solid var(--border-color); display: flex; flex-direction: column; gap: 7px; }
.hint-heading { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
.hint-heading strong { color: var(--text-main); font-size: 12px; }
.manual-chip { padding: 1px 6px; border-radius: 10px; background: #eaeef2; color: #656d76; font-size: 10px; }
.state-text, .source-hint, .safety-hint { margin: 0; color: var(--text-muted); font-size: 11px; line-height: 1.5; }
.command-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.command { flex: 1; min-width: 0; padding: 6px 8px; border-radius: 5px; background: var(--bg-main); border: 1px solid var(--border-color); color: var(--text-main); font-family: Consolas, monospace; font-size: 11px; overflow-wrap: anywhere; }
.copy-button { flex-shrink: 0; min-height: 28px; padding: 4px 10px; border: 1px solid var(--border-color); border-radius: 6px; background: transparent; color: var(--accent); font-size: 11px; cursor: pointer; }
.copy-button:hover { border-color: var(--accent); background: var(--bg-main); }
.copy-button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.safety-hint { color: #9a6700; }
@media (max-width: 460px) { .command-row { align-items: stretch; flex-direction: column; } .copy-button { min-height: 36px; } }
@media (pointer: coarse) { .copy-button { min-height: 44px; } }
</style>
