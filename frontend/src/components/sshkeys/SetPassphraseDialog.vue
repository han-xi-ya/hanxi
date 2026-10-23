<script setup lang="ts">
// 改口令对话框：加 / 换 / 去三合一（后端就是 SetKeyPassphrase 一个 RPC，
// 三种走向由"新口令是否为空"与当前状态决定，界面不另立三套流程）。
//
// 两条后端实测出来的事实，必须在这里显示给用户、不能藏：
// ① 改口令会把老格式私钥就地升级为 OpenSSH 新格式（PKCS#1 →
//    `BEGIN OPENSSH PRIVATE KEY`）。形态不可逆、密钥内容与指纹不变——不说，
//    用户换个口令却发现文件头变了，会以为自己钥匙被换了。
// ② 带口令又没有配套 .pub 的钥匙，注释与密钥一起被加密、可能取不回——
//    这条不是界面编的：成功回执 Message 里后端会带"未取回原注释"一句，
//    回执必须原样渲染，不得吞掉。
// 指纹跨口令变更不变（"改口令不会让已部署的授权失效"的依据），回执文案里
// 后端每次都带这句，同样原样上屏。
//
// 禁走 usePrompt/UiPromptDialog：实盘它是 type="text"（UiPromptDialog.vue），
// 口令进它等于明文。这里所有口令字段 type="password" + 显式「显示」勾选。
import { computed, ref, watch } from 'vue'
import UiBanner from '../ui/UiBanner.vue'
import KeyOpsModal from './KeyOpsModal.vue'
import { guardBadge } from './useKeyring'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { KeyEntry, KeyOpResult } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'

const props = defineProps<{
  open: boolean
  /** 目标钥匙（台账行，一把一条）。null 时对话框不渲染内容——打开动作由面板负责带上目标。 */
  target: KeyEntry | null
}>()

const emit = defineEmits<{ close: []; changed: [] }>()

const oldPass = ref('')
const newPass = ref('')
const showOld = ref(false)
const showNew = ref(false)
// 去掉口令的回显确认（confirm 字段）。相等判定**只在后端做**：它比的是解析后
// 路径的基名（8.3 短名/大小写/`..` 都可能与台账字符串不同形），前端复制一份
// 相等逻辑只会造出"前端放行、后端拒绝"或反向的分叉。前端只把这道门呈现出来
// 并要求非空，不符时后端错误原样上屏。
const confirmEcho = ref('')

const error = ref('')
const result = ref<KeyOpResult | null>(null)
const { busy: working, run } = useAsyncAction()

// 三态判定与台账徽标同源（guardBadge 的"判不出来"绝不塌成两态）。
const unprotect = computed(() => !!props.target?.guarded && newPass.value === '')
const noChange = computed(() => props.target != null && !props.target.guarded && newPass.value === '')

async function submit() {
  if (!props.target || working.value || noChange.value) return
  error.value = ''
  result.value = null
  const req = {
    path: props.target.privatePath!,
    oldPassphrase: oldPass.value,
    newPassphrase: newPass.value,
    confirm: confirmEcho.value,
  }
  // 三个口令向的 ref 先销毁再发出：成功、失败、之后的取消/关窗都不该再持有它，
  // 在途窗口里字段也不该仍是明文（写在 await 之后补救不算销毁）。
  oldPass.value = ''
  newPass.value = ''
  confirmEcho.value = ''
  showOld.value = false
  showNew.value = false
  const res = await run(() => SshKeysAPI.SshKeysService.SetKeyPassphrase(req))
  if (res.ok) {
    result.value = res.data ?? null
    emit('changed')
  } else {
    error.value = getErrorMessage(res.error)
  }
}

// 销毁与"关窗"分开写：close 走 emit，watch 到父层置 false 只做销毁，
// 不再回 emit（否则会向父级补一次关窗事件，双发）。
function wipe() {
  oldPass.value = ''
  newPass.value = ''
  confirmEcho.value = ''
  showOld.value = false
  showNew.value = false
  error.value = ''
  result.value = null
}

function close() {
  if (working.value) return
  wipe()
  emit('close')
}

// 关窗即清（含父层直接置 open=false 的路径），口令不活过窗口。
watch(() => props.open, (v) => { if (!v) wipe() })
</script>

<template>
  <KeyOpsModal :open="open" title="修改私钥口令" @close="close">
    <div v-if="target" class="kops-form keyops-pass">
      <div class="kops-target">
        <span class="mono">{{ target.name }}</span>
        <span class="hint-dim">当前：{{ guardBadge(target).label }}</span>
      </div>

      <template v-if="!result">
        <UiBanner v-if="!target.guardKnown" tone="warn">
          这把文件口令层「判不出来」——改口令大概率被后端拒绝（它不是本层认得的私钥形态）。
          可以照常尝试，错误会原样显示。
        </UiBanner>

        <div class="form-item-block">
          <label class="form-label" for="kops-pass-old">旧口令（当前无口令就留空）</label>
          <input id="kops-pass-old" v-model="oldPass" :type="showOld ? 'text' : 'password'"
                 class="form-text-input" autocomplete="off" spellcheck="false" :disabled="working" />
          <label class="toggle-label"><input id="kops-pass-showold" v-model="showOld" type="checkbox" /> 显示</label>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="kops-pass-new">
            新口令{{ target.guarded ? '（留空 = 去掉口令保护）' : '（留空 = 维持无保护，什么都不改）' }}
          </label>
          <input id="kops-pass-new" v-model="newPass" :type="showNew ? 'text' : 'password'"
                 class="form-text-input" autocomplete="off" spellcheck="false" :disabled="working" />
          <label class="toggle-label"><input id="kops-pass-shownew" v-model="showNew" type="checkbox" /> 显示</label>
        </div>

        <UiBanner tone="info">
          改口令会把老格式私钥<strong>就地升级为 OpenSSH 新格式</strong>（文件头变成
          <code>BEGIN OPENSSH PRIVATE KEY</code>）：形态不可逆，密钥内容与指纹不变。
          新格式里注释与口令同层加密——带口令又缺配套 <code>.pub</code> 的钥匙，注释可能
          取不回，若发生会写在成功回执里。
        </UiBanner>

        <div v-if="unprotect" class="form-item-block">
          <UiBanner tone="error">
            新口令留空 = <strong>去掉口令保护，这是降级动作</strong>：这把钥匙退回
            "文件被读到就能用"的状态——任何能以你身份读到该文件的程序都能直接用它
            登录你已授权的机器。
          </UiBanner>
          <label class="form-label" for="kops-pass-confirm">
            确认降级：把文件名原样输入一遍（后端按解析后基名核对，不符则什么都不改）
          </label>
          <input id="kops-pass-confirm" v-model="confirmEcho" type="text" class="form-text-input"
                 autocomplete="off" spellcheck="false" :disabled="working" :placeholder="target.name" />
        </div>

        <div v-if="noChange" class="hint-line">这把私钥当前本无口令，新口令再留空就无事可做——要加保护请填新口令。</div>

        <div v-if="error" class="error-box">{{ error }}</div>

        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" :disabled="working" @click="close">取消</button>
          <button
            id="kops-pass-submit"
            class="btn"
            :class="unprotect ? 'btn-danger' : 'btn-primary'"
            :disabled="working || noChange || (unprotect && confirmEcho.trim() === '')"
            @click="submit"
          >
            {{ working ? '写入中…' : (unprotect ? '去掉口令' : (target.guarded ? '更换口令' : '加口令')) }}
          </button>
        </div>
      </template>

      <template v-else>
        <UiBanner tone="ok">改口令完成</UiBanner>
        <!-- 回执原样渲染：指纹未变的说明、（如有）"未取回原注释"的实话，
             都在后端 Message 里，界面吞掉任何一句都算隐瞒。 -->
        <p class="kops-message">{{ result.message }}</p>
        <dl class="kops-receipt">
          <div><dt>实际路径</dt><dd class="mono">{{ result.path }}</dd></div>
          <div><dt>指纹</dt><dd class="mono">{{ result.fingerprint }}</dd></div>
          <div><dt>口令</dt><dd>{{ result.guarded ? '已保护' : '无保护' }}</dd></div>
        </dl>
        <p class="hint-line">指纹不变 = 你已部署到服务端的授权照常可用，配套 <code>.pub</code> 无需重新分发。</p>
        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" @click="close">完成</button>
        </div>
      </template>
    </div>
  </KeyOpsModal>
</template>

<!-- 共享布局表单源（keyopsDialogs.css）；本件无真差异样式。 -->
<style scoped src="./keyopsDialogs.css"></style>
