<script setup lang="ts">
// 补出公钥对话框：只有私钥、缺配套 .pub 的钥匙，由后端从私钥算出公钥写出 <名>.pub。
//
// 存在的理由（操作死角的收口）：这类钥匙在界面上部署不出去——「部署公钥」要选公钥
// 文件；典型来源就是从服务器直接下回来的私钥，天生没有配套公钥。
//
// 三条必须显示在后端行为之上的实话（对着 manage.go / keyops/pubpair.go 逐条搬）：
// ① **带口令的私钥也补得动，全程不接触口令**——OpenSSH 新格式只加密私钥段，公钥段
//    在外层明文里。所以入口条件不看 guarded（useKeyOps.canExportPub），本窗里也
//    没有任何口令字段；能不能补（文件形态认不认得）由后端判，拒绝原文上屏。
// ② 注释由这里给：台账读得到原注释（无口令私钥）时沿用进输入框；**留空就真空着**，
//    后端不会拿文件名编一个——那会在别人的 authorized_keys 里留下认不出的标签。
//    带口令的私钥原注释在加密段里本就取不到，这条实话必须出现（成功回执里后端
//    message 讲什么就显示什么，界面不覆盖）。
// ③ 成功后亮出算出的指纹并触发台账刷新；已存在配套 .pub 时后端拒绝、不覆盖。
//    code 在失败路径到不了前端（Wails 对 (T, error) 把 T 丢弃，types.go 顶部在案），
//    所以一律原文显示，绝不按 code 分支。
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
  /** 目标行（台账一把一条）。面板只在 canExportPub 的行给出入口，这里拿到的一定有私钥。 */
  target: KeyEntry | null
}>()

const emit = defineEmits<{ close: []; changed: [] }>()

const comment = ref('')
const error = ref('')
const result = ref<KeyOpResult | null>(null)
const { busy: working, run } = useAsyncAction()

// 开窗：沿用台账里已有的注释（无口令私钥台账读得到原注释），读不到就是空串——
// 空串对应的界面是「留空后果」那句实话，绝不替用户编一个。关窗即清，不留上把的输入。
watch(() => props.open, (v) => {
  if (v) comment.value = props.target?.comment ?? ''
  else wipe()
})

const commentBlank = computed(() => comment.value.trim() === '')

async function submit() {
  if (!props.target?.privatePath || working.value) return
  error.value = ''
  result.value = null
  const res = await run(() => SshKeysAPI.SshKeysService.ExportPublicKey({
    path: props.target!.privatePath!,
    comment: comment.value,
  }))
  if (res.ok) {
    result.value = res.data ?? null
    // 成功才刷台账：被拒时行还是原样，假装补过会把下一次判断建立在旧数据上。
    emit('changed')
  } else {
    error.value = getErrorMessage(res.error)
  }
}

function wipe() {
  comment.value = ''
  error.value = ''
  result.value = null
}

function close() {
  if (working.value) return
  emit('close')
}
</script>

<template>
  <KeyOpsModal :open="open" title="补出配套公钥" @close="close">
    <div v-if="target" class="kops-form keyops-export">
      <div class="kops-target">
        <span class="mono">{{ target.name }}</span>
        <span class="hint-dim">当前：{{ guardBadge(target).label }}</span>
      </div>

      <template v-if="!result">
        <UiBanner tone="info">
          公钥段存在私钥文件的<strong>明文外层</strong>——带口令的私钥也补得动，这一步
          <strong>不需要也不会接触口令</strong>（本窗没有任何口令字段），私钥文件不做任何修改。
          配套 <code>.pub</code> 已存在时后端会拒绝：这个动作只补缺，不重写。
        </UiBanner>

        <div class="form-item-block">
          <label class="form-label" for="kops-exp-comment">公钥行尾注释（可空；台账读得到原注释时已沿用，可改）</label>
          <input id="kops-exp-comment" v-model="comment" type="text" class="form-text-input"
                 autocomplete="off" spellcheck="false" :disabled="working"
                 placeholder="例如 laptop@2026 或 nas-backup" />
          <!-- 留空的后果必须如实说在明面，不粉饰成"没什么影响"。 -->
          <p v-if="commentBlank" class="hint-line">
            取不到原注释就留空：日后在别人机器的 <code>authorized_keys</code> 里只能靠指纹认这把。
          </p>
          <!-- 带口令的私钥原注释在加密段里，本就取不到——这条实话对这类行常驻，
               与输入框当前有没有内容无关。 -->
          <p v-if="target.guarded" class="hint-line">
            这把带口令：原注释在私钥的加密段里，本就取不到——这里留空就真空着，填了什么就是什么。
          </p>
        </div>

        <div v-if="error" class="error-box">{{ error }}</div>

        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" :disabled="working" @click="close">取消</button>
          <button id="kops-exp-submit" class="btn btn-primary" :disabled="working" @click="submit">
            {{ working ? '补出中…' : '补出公钥' }}
          </button>
        </div>
      </template>

      <template v-else>
        <UiBanner tone="ok">公钥已补出</UiBanner>
        <!-- 后端 message 原样上屏（写了哪个文件、"私钥未作任何修改"），界面不覆盖不改写。 -->
        <p class="kops-message">{{ result.message }}</p>
        <dl class="kops-receipt">
          <div><dt>指纹</dt><dd class="mono">{{ result.fingerprint }}</dd></div>
          <div><dt>私钥</dt><dd class="mono">{{ result.path }}</dd></div>
          <!-- 新建出来的那份公钥：走结构化字段，不只活在 message 散文里——
               用户要能核对文件名、要能复制去别处用。 -->
          <div v-if="result.publicPath">
            <dt>新建公钥</dt><dd class="mono">{{ result.publicPath }}</dd>
          </div>
        </dl>
        <p class="hint-line">台账已触发刷新：刷新到位后这一行的「部署公钥」即可用；若没变化，点右上角「刷新」重读。</p>
        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" @click="close">完成</button>
        </div>
      </template>
    </div>
  </KeyOpsModal>
</template>

<!-- 共享布局表单源（keyopsDialogs.css）；本件无真差异样式。 -->
<style scoped src="./keyopsDialogs.css"></style>
