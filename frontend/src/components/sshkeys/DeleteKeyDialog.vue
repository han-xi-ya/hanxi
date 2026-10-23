<script setup lang="ts">
// 删除对话框的**一个实现、两种对象**（mode 分支在 props，不复制窗体——两份对话框
// 文案会各自演化，这是本仓反复清的病）：
//
// mode='pair'（默认，对象=一把钥匙）：后端成对删私钥与配套 .pub。口径逐条如实搬：
// ① **成对删**：私钥 + 配套 .pub 一起走，只删一半会留下更难判断的孤儿；
// ② **失去访问的实话**：本地无法知道这把公钥被装到了哪些机器——若某台机器只接受
//    密钥登录，删掉就是失去访问。这句必须出现在确认文案里，藏了等于骗人；
// ③ **清单引用直接打在对话框里**：台账行的 usedBy（主机清单在用它）非空时先摆出
//    别名清单——后端仍会在删之前拒绝并列出清单（台账可能过期，裁判在后端），
//    拒绝原文一个字符不改地上屏；
//
// mode='orphan-pub'（对象=只剩 .pub 的孤儿行）：后端 DeletePublicKey **只接受
// 确认没有配套私钥的公钥路径**（私钥还在就坚决拒，逼那种场景走成对删除）。话术
// 必须换一套：这里没有什么"失去私钥"，但"远端机器上若装过它，那一行授权不会被
// 回收"同样是实话，必须说。usedBy 在此模式下恒为空（引用判定要靠私钥比对，
// 孤儿行后端判不了），所以清单横幅天然不出——不给"未判"发"没人用"的通行证。
//
// 两种模式共用的确认门：④ **confirm 回显**必须把文件名原样输入一遍。相等判定
// 只在后端做（它比的是解析后的基名，前端比字面会因 8.3 短名/大小写产生分叉），
// 前端只要求非空；不符时后端错误原样上屏。
// 回执路径是解析后的真身（8.3 短名/大小写/`..` 都可能与台账字符串不同形），
// 界面标成"实际删除位置（解析后路径）"，既不拿它做字符串相等比较，也绝不因
// 路径字面不同而说"你输入的路径不存在"。
import { computed, ref, watch } from 'vue'
import UiBanner from '../ui/UiBanner.vue'
import KeyOpsModal from './KeyOpsModal.vue'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { KeyEntry, KeyOpResult } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import type { DeleteMode } from './useKeyOps'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'
import { usedByOf } from './useKeyring'

const props = withDefaults(defineProps<{
  open: boolean
  /** 目标行（台账一把一条）。打开动作由面板负责带上目标，null 时窗内不渲染内容。 */
  target: KeyEntry | null
  /** 'pair'=成对删私钥与配套公钥（默认）；'orphan-pub'=只删孤儿 .pub 文件。 */
  mode?: DeleteMode
}>(), {
  mode: 'pair',
})

const emit = defineEmits<{ close: []; changed: [] }>()

const orphan = computed(() => props.mode === 'orphan-pub')
const title = computed(() => (orphan.value ? '删除这个公钥' : '删除这把钥匙'))
// 删除对象的路径：成对删以私钥为锚（后端顺带走配套 .pub），孤儿模式以公钥为锚。
const targetPath = computed(() =>
  (orphan.value ? props.target?.publicPath : props.target?.privatePath) ?? '')

const confirmEcho = ref('')
const error = ref('')
const result = ref<KeyOpResult | null>(null)
const { busy: working, run } = useAsyncAction()

// usedBy 直接渲染后端的值（防误删信号），不自行推断。
//
// **未判归一成空数组在这里是安全的**，因为这个值只用于"非空才挂横幅"：
// null（后端没判过）与 []（判过、确实没人用）在界面上都表现为"这句话不说"。
// 反过来若把它渲染成"没有主机在用"，就是拿未判给误删发通行证——那句子只在
// 台账行上按 usedByKnown 分诊后才允许出现（见 useKeyring.usedByOf 的注释）。
const usedBy = computed<string[]>(() => usedByOf(props.target ?? ({} as KeyEntry)) ?? [])

// 非空即可提交：相等与否归后端裁判（见文件头确认门），前端不复制判定；
// usedBy 非空也不在这里硬拦——台账可能过期，清单护栏的裁判在后端。
const canSubmit = computed(() => confirmEcho.value.trim() !== '' && !working.value)

async function submit() {
  if (!props.target || !targetPath.value || !canSubmit.value) return
  error.value = ''
  result.value = null
  const req = { path: targetPath.value, confirm: confirmEcho.value }
  const res = await run(() => orphan.value
    ? SshKeysAPI.SshKeysService.DeletePublicKey(req)
    : SshKeysAPI.SshKeysService.DeleteKey(req))
  confirmEcho.value = ''
  if (res.ok) {
    result.value = res.data ?? null
    // 成功才刷台账；被拒（护栏/回显不符/孤儿判定不过）时台账照旧，用户看得清为什么没删掉。
    emit('changed')
  } else {
    // 拒绝的原文（护栏自带"是哪几条在用"、回显不符自带期望名）一个字符都不改地显示。
    // code 在失败路径到不了前端（Wails 对 (T, error) 把 T 丢弃，types.go 顶部在案），
    // 所以一律原文上屏，绝不按 code 分支。
    error.value = getErrorMessage(res.error)
  }
}

function wipe() {
  confirmEcho.value = ''
  error.value = ''
  result.value = null
}

function close() {
  if (working.value) return
  wipe()
  emit('close')
}

watch(() => props.open, (v) => { if (!v) wipe() })
</script>

<template>
  <KeyOpsModal :open="open" :title="title" @close="close">
    <div v-if="target" class="kops-form keyops-delete">
      <template v-if="!result">
        <dl class="kops-receipt">
          <template v-if="orphan">
            <div><dt>公钥</dt><dd class="mono">{{ target.publicPath || target.name + '.pub' }}</dd></div>
          </template>
          <template v-else>
            <div><dt>私钥</dt><dd class="mono">{{ target.privatePath || target.name }}</dd></div>
            <div v-if="target.hasPublic"><dt>公钥</dt><dd class="mono">{{ target.publicPath }}</dd></div>
          </template>
          <div v-if="target.fingerprint"><dt>指纹</dt><dd class="mono">{{ target.fingerprint }}</dd></div>
        </dl>

        <UiBanner v-if="orphan" tone="error">
          这会<strong>只删这个公钥文件</strong>（
          <code>{{ target.name }}.pub</code> 在本机的这一份）——这把的配套私钥本就不存在，
          成对删除那条路明确不收 <code>.pub</code> 路径，所以才有这颗按钮。
          谈不上"失去私钥"，但<strong>远端机器上若装过它，那一行授权不会被回收</strong>——
          要撤远端授权，得登录那台机器自己删。
        </UiBanner>
        <UiBanner v-else tone="error">
          这会<strong>成对删除</strong>私钥 <code>{{ target.name }}</code> 与配套公钥
          <code>{{ target.name }}.pub</code>（公钥不存在时只删私钥，回执会说清）。
          私钥一删不可恢复——<strong>若这把已部署到某台只接受密钥登录的服务器，
          删掉后你会失去访问；本地无法知道它被装到了哪些机器</strong>，远端
          <code>authorized_keys</code> 里那行不会被回收。
        </UiBanner>

        <!-- 清单引用直接摆出来（行上 already 有「在用」信号，这里删除前再坐实一次）；
             后端随后仍会在删之前拒绝并列清单——台账可能过期，裁判在后端。 -->
        <UiBanner v-if="usedBy.length" tone="warn">
          主机清单里有 {{ usedBy.length }} 条正在用它：<strong>{{ usedBy.join('、') }}</strong>。
          后端会直接拒绝删除——先改掉或删掉那几条清单条目，再回来删钥匙。
        </UiBanner>

        <div class="form-item-block">
          <label class="form-label" for="kops-del-confirm">
            {{ orphan
              ? '要删就把公钥文件名原样输入一遍（含 .pub 后缀；后端按解析后的基名核对，不符则什么都不删）'
              : '要删就把文件名原样输入一遍（后端按解析后的基名核对，不符则什么都不删）' }}
          </label>
          <input id="kops-del-confirm" v-model="confirmEcho" type="text" class="form-text-input"
                 autocomplete="off" spellcheck="false"
                 :placeholder="orphan ? target.name + '.pub' : target.name" :disabled="working" />
        </div>

        <div v-if="error" class="error-box">{{ error }}</div>

        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" :disabled="working" @click="close">取消</button>
          <button id="kops-del-submit" class="btn btn-danger" :disabled="!canSubmit" @click="submit">
            {{ working ? '删除中…' : (orphan ? '删除公钥' : '删除') }}
          </button>
        </div>
      </template>

      <template v-else>
        <UiBanner tone="ok">{{ orphan ? '公钥已删除' : '已删除' }}</UiBanner>
        <!-- 回执原样渲染："配套私钥本就不存在""远端那一行不会被回收"这类实话
             都在后端 message 里，不许被界面简化掉。 -->
        <p class="kops-message">{{ result.message }}</p>
        <div class="hint-dim mono">{{ result.path }}</div>
        <p class="hint-line">上行为实际删除位置（解析后的路径，写法可能与台账列示不同形，属正常）。</p>
        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" @click="close">完成</button>
        </div>
      </template>
    </div>
  </KeyOpsModal>
</template>

<!-- 共享布局表单源（keyopsDialogs.css）；本件无真差异样式。 -->
<style scoped src="./keyopsDialogs.css"></style>
