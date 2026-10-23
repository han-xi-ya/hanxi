<script setup lang="ts">
// 密钥台账的六个写动作对话框的编排层：面板只管"开了哪个窗、目标是谁"，
// 每个对话框内部自理请求构造、口令销毁与回执（状态机不跨层共享，各窗
// 彼此串行——台账一次只该有一个写动作在途）。
//
// 各窗常挂（v-if 交给 KeyOpsModal 的 open 翻转）：这是 useDialogA11y 的
// ConfirmDialog 同款接线形制——监听挂在 false→true 的翻转上，组件必须先在位；
// 若用 v-if 把整个对话框挂上摘下，watch 永远看不到翻转，Esc/Tab 环会静默失灵
// （TROUBLESHOOTING §25.4 那条老坑的镜像形态）。
import { ref } from 'vue'
import GenerateKeyDialog from './GenerateKeyDialog.vue'
import SetPassphraseDialog from './SetPassphraseDialog.vue'
import DeleteKeyDialog from './DeleteKeyDialog.vue'
import ExportPubDialog from './ExportPubDialog.vue'
import DeployKeyDialog from './DeployKeyDialog.vue'
import type { KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import type { DeleteMode } from './useKeyOps'

defineProps<{
  /** 台账当前条目（一把钥匙一条）：生成查重用、部署只列有 .pub 的。 */
  keys: KeyEntry[]
}>()

const emit = defineEmits<{ changed: [] }>()

type Mode = 'none' | 'generate' | 'passphrase' | 'delete' | 'export' | 'deploy'

const mode = ref<Mode>('none')
const target = ref<KeyEntry | null>(null)
const presetPubPath = ref('')
// 删除对话框的两种对象共用一个窗（DeleteMode 在 useKeyOps 钉类型）：
// 成对删除与孤儿 .pub 删除话术不同、RPC 不同，但确认门与回执形制相同——
// 复制一份对话框，两套文案会各自演化，这是本仓反复清的病。
const deleteKind = ref<DeleteMode>('pair')

function openGenerate() {
  target.value = null
  mode.value = 'generate'
}

function openPassphrase(k: KeyEntry) {
  target.value = k
  mode.value = 'passphrase'
}

function openDelete(k: KeyEntry) {
  target.value = k
  deleteKind.value = 'pair'
  mode.value = 'delete'
}

// 孤儿 .pub 行的删除入口（面板只在 canDeletePub 的行给出这颗钮）。
function openDeletePub(k: KeyEntry) {
  target.value = k
  deleteKind.value = 'orphan-pub'
  mode.value = 'delete'
}

// 只有私钥、缺配套 .pub 的行（canExportPub）：从私钥补出公钥，让这把可部署。
function openExportPub(k: KeyEntry) {
  target.value = k
  mode.value = 'export'
}

// 从台账某一行进入：预先把"装哪把公钥"选好（可改）；该行没有配套 .pub 时
// 面板不会给出这颗按钮，这里拿到的一定是 publicPath 非空的行。
function openDeploy(k?: KeyEntry) {
  target.value = k ?? null
  presetPubPath.value = k?.publicPath ?? ''
  mode.value = 'deploy'
}

function closeCurrent() {
  mode.value = 'none'
  target.value = null
  presetPubPath.value = ''
  deleteKind.value = 'pair'
}

defineExpose({ openGenerate, openPassphrase, openDelete, openDeletePub, openExportPub, openDeploy })
</script>

<template>
  <div class="keyops-dialogs">
    <GenerateKeyDialog
      :open="mode === 'generate'" :keys="keys"
      @close="closeCurrent" @changed="emit('changed')"
    />
    <SetPassphraseDialog
      :open="mode === 'passphrase'" :target="target"
      @close="closeCurrent" @changed="emit('changed')"
    />
    <DeleteKeyDialog
      :open="mode === 'delete'" :target="target" :mode="deleteKind"
      @close="closeCurrent" @changed="emit('changed')"
    />
    <ExportPubDialog
      :open="mode === 'export'" :target="target"
      @close="closeCurrent" @changed="emit('changed')"
    />
    <DeployKeyDialog
      :open="mode === 'deploy'" :keys="keys" :preset-pub-path="presetPubPath"
      @close="closeCurrent"
    />
  </div>
</template>
