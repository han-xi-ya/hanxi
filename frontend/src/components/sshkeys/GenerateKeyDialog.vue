<script setup lang="ts">
// 生成密钥对对话框。四条写动作里唯一"会创建 ~/.ssh 目录"的路径（manage.go：
// 建目录的副作用只挂在显式写动作上），所以对话框里如实预告这一句，不让人
// 事后才发现 hanxi 动过主目录。
//
// 两个不默默放行的门：
// ① 注释留空——注释是日后在别人机器的 authorized_keys 里认出这把钥匙的唯一
//    线索（没有它，一排 `ssh-ed25519 AAAA…` 全凭指纹人肉对照）。留空时给警告
//    并要求显式勾选"仍要留空生成"，绝不静默通过。
// ② 目标已存在——后端要 overwrite=true 才放行，界面对应的必须是危险确认：
//    默认不置真；用台账预判把门闩亮起来，勾选"覆盖"才随请求带 true。台账可能
//    过期，真裁判仍是后端，被拒时错误原样显示，不粉饰。
import { computed, ref, watch } from 'vue'
import UiBanner from '../ui/UiBanner.vue'
import KeyOpsModal from './KeyOpsModal.vue'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { GenerateResult, KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'
import { GENERATE_TYPES, RSA_BITS_DEFAULT, RSA_BITS_OPTIONS, nameTakenInLedger, rsaBitsOf } from './useKeyOps'

const props = defineProps<{
  open: boolean
  /** 台账当前条目（一把一条，name 为不含 .pub 的基名）：只用于"目标已存在"的预判门闩（裁判在后端）。 */
  keys: KeyEntry[]
}>()

const emit = defineEmits<{ close: []; changed: [] }>()

const name = ref('')
const type = ref<string>('ed25519')
const rsaBits = ref<number>(RSA_BITS_DEFAULT)
const comment = ref('')
const passphrase = ref('')
const showPass = ref(false)
const ackEmptyComment = ref(false)
const overwrite = ref(false)

const error = ref('')
const result = ref<GenerateResult | null>(null)
const { busy: working, run } = useAsyncAction()

const trimmedName = computed(() => name.value.trim())
const trimmedComment = computed(() => comment.value.trim())
const commentMissing = computed(() => trimmedComment.value === '')
// 目标存在性预判走台账（大小写按字面；真身判定在后端，误判漏判都有后端兜底）。
const targetTaken = computed(() => nameTakenInLedger(props.keys, trimmedName.value))

// 留空注释要显式勾选才放行；目标已存在时按钮变危险档——两者都是"不默默放行"。
const canSubmit = computed(() =>
  trimmedName.value !== '' && (commentMissing.value ? ackEmptyComment.value : true) && !working.value,
)

// 换名字就收回 overwrite：门闩是跟着"这一个目标"的，不许跟着肌肉记忆走。
watch(trimmedName, () => { overwrite.value = false })

async function submit() {
  if (!canSubmit.value) return
  error.value = ''
  result.value = null
  const req = {
    name: trimmedName.value,
    type: type.value,
    rsaBits: rsaBitsOf(type.value, rsaBits.value),
    comment: trimmedComment.value,
    passphrase: passphrase.value,
    overwrite: overwrite.value,
  }
  // 口令在请求成形的那一刻起就不再被本组件持有——先销毁，再发出、再处理结果。
  // （写在 await 之后"等回调补救"不算销毁：在途窗口里字段仍然满是明文。）
  passphrase.value = ''
  showPass.value = false
  const res = await run(() => SshKeysAPI.SshKeysService.GenerateKey(req))
  if (res.ok) {
    result.value = res.data ?? null
    emit('changed')
  } else {
    // 后端拒绝（含"已存在需 overwrite"与名字/注释合法性）原样显示，不粉饰。
    error.value = getErrorMessage(res.error)
  }
}

function resetForm() {
  name.value = ''
  type.value = 'ed25519'
  rsaBits.value = RSA_BITS_DEFAULT
  comment.value = ''
  passphrase.value = ''
  showPass.value = false
  ackEmptyComment.value = false
  overwrite.value = false
  error.value = ''
  result.value = null
}

function close() {
  if (working.value) return // 在途不换窗：结果还没落地就走等于吞回执
  resetForm()
  emit('close')
}

// 组件被上层回收（切页/卸载）时同样过 resetForm——口令不许活过组件。
watch(() => props.open, (v) => { if (!v) resetForm() })
</script>

<template>
  <KeyOpsModal :open="open" title="生成新密钥对" @close="close">
    <div class="kops-form keyops-generate">
      <UiBanner tone="info">
        密钥会落到 <code>~/.ssh</code> 下。<strong>若本机还没有这个目录，这一步会创建它</strong>
        （权限 0700）——这是本模块唯一会新建该目录的动作。
      </UiBanner>

      <template v-if="!result">
        <div class="form-item-block">
          <label class="form-label" for="kops-gen-name">文件名（私钥名，别带 .pub 后缀）</label>
          <input id="kops-gen-name" v-model="name" type="text" class="form-text-input"
                 placeholder="如 id_nas" autocomplete="off" :disabled="working" />
          <p class="hint-line">只收文件名字符，目录由后端定在 <code>~/.ssh</code>；合法性（Windows 保留名、控制字符等）由后端一处判并如实报。</p>
        </div>

        <div v-if="targetTaken" class="form-item-block">
          <UiBanner tone="error">
            台账里已有 <code>{{ trimmedName }}</code>（私钥或配套公钥任一在场都算）——
            不勾选下方「覆盖」就会被拒绝。要保留旧钥匙请换一个文件名。
          </UiBanner>
        </div>

        <div class="form-item-block">
          <!-- 覆盖门恒可见、恒默认关：台账可能过期，若只在预判命中时才出现，
               盘上有文件而台账没列到的用户会被错误卡死。危险确认的裁判是勾选
               这个动作本身，不是门闩亮不亮。 -->
          <label class="toggle-label">
            <input id="kops-gen-overwrite" v-model="overwrite" type="checkbox" :disabled="working" />
            覆盖既有同名文件（旧私钥与配套公钥被顶掉、永久丢失——确认无误再勾）
          </label>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="kops-gen-type">算法</label>
          <select id="kops-gen-type" v-model="type" class="form-text-input" :disabled="working">
            <option v-for="t in GENERATE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
          </select>
        </div>

        <div v-if="type === 'rsa'" class="form-item-block">
          <label class="form-label" for="kops-gen-bits">RSA 位数</label>
          <select id="kops-gen-bits" v-model.number="rsaBits" class="form-text-input" :disabled="working">
            <option v-for="b in RSA_BITS_OPTIONS" :key="b" :value="b">{{ b }}</option>
          </select>
          <p class="hint-line">默认 {{ RSA_BITS_DEFAULT }}；越大生成越慢，日常 {{ RSA_BITS_OPTIONS[1] }}–{{ RSA_BITS_OPTIONS[2] }} 足够。</p>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="kops-gen-comment">注释（强烈建议填）</label>
          <input id="kops-gen-comment" v-model="comment" type="text" class="form-text-input"
                 placeholder="如 laptop-hanxi@2026（日后全凭它认钥匙）" autocomplete="off" :disabled="working" />
        </div>

        <div v-if="commentMissing" class="form-item-block">
          <UiBanner tone="warn">
            注释留空：<strong>公钥被复制到别人的 authorized_keys 后，没有任何文字线索说明这把
            钥匙是谁、从哪把机器生成的</strong>——清理与追责只能逐条对指纹。
            <label class="toggle-label">
              <input id="kops-gen-ack" v-model="ackEmptyComment" type="checkbox" :disabled="working" />
              我了解后果，仍要留空生成
            </label>
          </UiBanner>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="kops-gen-pass">口令（可留空 = 不加保护）</label>
          <input id="kops-gen-pass" v-model="passphrase" :type="showPass ? 'text' : 'password'"
                 class="form-text-input" autocomplete="off" spellcheck="false" :disabled="working" />
          <label class="toggle-label">
            <input id="kops-gen-show" v-model="showPass" type="checkbox" />
            显示口令
          </label>
          <p class="hint-line">口令不进任何返回、日志与展示面；提交后（不论成败）本字段立即清空。</p>
        </div>

        <div v-if="error" class="error-box">{{ error }}</div>

        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" :disabled="working" @click="close">取消</button>
          <button id="kops-gen-submit" class="btn btn-primary" :disabled="!canSubmit" @click="submit">
            {{ working ? '生成中…' : (overwrite ? '覆盖生成' : '生成') }}
          </button>
        </div>
      </template>

      <template v-else>
        <UiBanner tone="ok">{{ result.message }}</UiBanner>
        <dl class="kops-receipt">
          <div><dt>私钥</dt><dd class="mono">{{ result.privatePath }}</dd></div>
          <div><dt>公钥</dt><dd class="mono">{{ result.publicPath }}</dd></div>
          <div><dt>算法</dt><dd>{{ result.type }}</dd></div>
          <div><dt>指纹</dt><dd class="mono">{{ result.fingerprint }}</dd></div>
          <div><dt>口令</dt><dd>{{ result.guarded ? '已开启保护' : '未设置——文件被读到即可用，建议随后补上' }}</dd></div>
        </dl>
        <p class="hint-line">台账已按新指纹刷新。要装到某台机器，回台账对这一把点「部署公钥」；要让它进名册，去「主机清单」登记。</p>
        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" @click="close">完成</button>
        </div>
      </template>
    </div>
  </KeyOpsModal>
</template>

<!-- 共享布局表单源（keyopsDialogs.css，形制循 portscanPanels.css 先例）；
     底色/边框/警告条/按钮档全部走全局原子，本层零彩底（配色契约盯着）。 -->
<style scoped src="./keyopsDialogs.css"></style>
