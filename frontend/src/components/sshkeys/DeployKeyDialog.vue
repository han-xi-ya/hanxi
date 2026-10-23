<script setup lang="ts">
// 部署公钥对话框——本模块唯一"对外部机器产生写影响"的动作，也是全界面口令
// 纪律最重的一个。三条硬规矩都有测试钉着：
// ① 显式后果告知：这一步会向对方发起一次**真实登录尝试**（不是"检查一下"），
//    失败在对方 sshd 日志留 Failed password、可能触发 fail2ban；成功后往远端
//    authorized_keys **追加**一行公钥。执行前再过一道 useConfirm 确认步。
// ② 口令生命周期：type="password" + 显式「显示」勾选；请求一旦发出，本组件
//    立刻丢弃口令——成功、失败、取消、Esc、遮罩、卸载任一出口都清零；不进
//    结果展示、不进复制面、不进 console、不落任何前端持久化。
// ③ verdict 上色按枚举查表（useKeyOps 的 DEPLOY_TONE/LABEL），**绝不解析中文
//    reason 文本**——auth-rejected 的"两个可能"（密码不对 / 对方不接受密码登录）
//    由界面按 verdict 固定补一句，后端 reason 在场与否、怎么写都不影响这句。
//
// 禁走 usePrompt/UiPromptDialog（实盘 type="text"，见 PLAN_SSH_KEYS_UX §6.3）。
import { computed, ref, watch } from 'vue'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import KeyOpsModal from './KeyOpsModal.vue'
import * as SshKeysAPI from '../../../bindings/hanxi/internal/modules/sshkeys'
import type { DeployResult, KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { getErrorMessage } from '../../utils/errors'
import { useAsyncAction } from '../../composables/useAsyncAction'
import { useConfirm } from '../../composables/useConfirm'
import { DEPLOY_TIMEOUT_DEFAULT, DEPLOY_TIMEOUT_MAX, deployLabelOf, deployToneOf } from './useKeyOps'

const props = defineProps<{
  open: boolean
  keys: KeyEntry[]
  /** 从台账哪一把点的「部署公钥」（其 .pub 路径）；空串则用户在对话框内自选。 */
  presetPubPath: string
}>()

const emit = defineEmits<{ close: [] }>()

const pubKeyPath = ref('')
const host = ref('')
const port = ref<number>(22)
const user = ref('')
const password = ref('')
const showPass = ref(false)
const timeoutMs = ref<number>(DEPLOY_TIMEOUT_DEFAULT)

const error = ref('')
const result = ref<DeployResult | null>(null)
const { busy: working, run } = useAsyncAction()
const { confirm } = useConfirm()

// 只列有配套 .pub 的钥匙：部署装的是公钥，后端也刻意"读 .pub 文件，不读私钥"（types.go）。
// 台账现在一把一条，缺 .pub 的行没有可装的文件，不进选项（面板那行的部署钮也是禁用的）。
const pubOptions = computed(() => props.keys.filter(k => k.hasPublic && k.publicPath))

const chosenPub = computed(() => props.keys.find(k => k.publicPath === pubKeyPath.value) ?? null)

const formReady = computed(() =>
  pubKeyPath.value !== '' && host.value.trim() !== '' && user.value.trim() !== '' &&
  password.value !== '' && !working.value,
)

watch(() => props.open, (v) => {
  if (v) {
    // 开窗先把口令归零（上一轮的残留绝不带入这一次尝试），并带上行内预选的公钥。
    wipeSecret()
    pubKeyPath.value = props.presetPubPath
  }
})

async function submit() {
  if (!formReady.value) return
  error.value = ''
  result.value = null

  const addr = `${host.value.trim()}:${Number(port.value) || 0}`
  // 确认步：这是"真实登录尝试 + 改远端文件"的入口，只留一个显式按钮不够，
  // 还要一屏让人看清目标与公钥（PLAN_SSH_KEYS_UX §6.4 的裁断，落地在钥匙环侧
  // 同样成立）。取消/遮罩/Esc 都算否决，不发请求、口令留在原字段供改主意。
  const accepted = await confirm({
    title: `向 ${addr} 部署公钥？`,
    tone: 'warning',
    details: [
      { label: '目标', value: addr },
      { label: '登录用户', value: user.value.trim() },
      { label: '公钥', value: chosenPub.value?.name ?? pubKeyPath.value },
    ],
    description: '下一步会真的用你输入的密码登录这台机器一次：对端 sshd 日志会留下记录，'
      + '密码输错会留下 Failed password，装了 fail2ban 的主机连续失败可能临时封禁本机 IP。'
      + '登录成功后会向远端 ~/.ssh/authorized_keys 追加这一行公钥（已存在则跳过不重复写）。'
      + '这个密码只用于这一次登录：不保存、不回显、发出即销毁。',
    confirmLabel: '继续，发起部署',
  })
  if (!accepted) return

  const req = {
    host: host.value.trim(),
    port: Number(port.value) || 0,
    user: user.value.trim(),
    password: password.value,
    pubKeyPath: pubKeyPath.value,
    timeoutMs: Number(timeoutMs.value) || 0,
  }
  // 请求成形的那一刻起，本组件不再持有口令——先销毁，再在途，再处理结果。
  wipeSecret()

  const res = await run(() => SshKeysAPI.SshKeysService.DeployPublicKey(req))
  if (res.ok) {
    result.value = res.data ?? null
  } else {
    // RPC 层错误（入参被后端拒等）走错误通道；文本已过 redactSecret，可直显。
    error.value = getErrorMessage(res.error)
  }
}

function wipeSecret() {
  password.value = ''
  showPass.value = false
}

function close() {
  if (working.value) return // 在途不换窗：一次对外写影响的回执必须看得见
  wipeSecret()
  error.value = ''
  result.value = null
  emit('close')
}

// 父层置 open=false（含 Esc/遮罩走到 close 之外的路径、切页回收）同样销毁口令。
watch(() => props.open, (v) => { if (!v) { wipeSecret(); error.value = ''; result.value = null } })

// —— verdict 视图件：全部按枚举值分派，不读 reason 文本 ——
const verdictTone = computed(() => (result.value ? deployToneOf(result.value.verdict) : 'neutral'))
const verdictLabel = computed(() => (result.value ? deployLabelOf(result.value.verdict) : ''))
const isAuthRejected = computed(() => result.value?.verdict === 'auth-rejected')
const isHostKeyChanged = computed(() => result.value?.verdict === 'hostkey-changed')
const isRefused = computed(() => result.value?.verdict === 'refused')
const isDeployed = computed(() => result.value?.verdict === 'appended' || result.value?.verdict === 'already-present')

// 「再试一次」：保留表单其余字段，口令必须重新输入——失败路径上口令早已被
// 销毁，这里没有任何"缓存在途口令"可回填（PLAN_SSH_KEYS_UX §6.5 尾注）。
function resetForRetry() {
  result.value = null
  error.value = ''
}
</script>

<template>
  <KeyOpsModal :open="open" title="部署公钥到远端" @close="close">
    <div class="kops-form keyops-deploy">
      <UiBanner tone="warn">
        这不是"检查一下"：这一步会<strong>向对方发起一次真实登录尝试</strong>。失败会在对方
        sshd 日志留下 Failed password 记录，装了 fail2ban 的主机连续失败可能临时封禁本机 IP；
        成功会向远端 <code>~/.ssh/authorized_keys</code> <strong>追加</strong>一行公钥。
      </UiBanner>

      <template v-if="!result">
        <div class="form-item-block">
          <label class="form-label" for="kops-dep-pub">要装哪把公钥</label>
          <select id="kops-dep-pub" v-model="pubKeyPath" class="form-text-input" :disabled="working">
            <option value="" disabled>从台账选择（只列有配套 .pub 的钥匙）</option>
            <option v-for="k in pubOptions" :key="k.publicPath" :value="k.publicPath">
              {{ k.name }}{{ k.fingerprint ? `（${k.fingerprint.slice(0, 26)}…）` : '' }}
            </option>
          </select>
          <p v-if="!pubOptions.length" class="hint-line">台账里没有带配套 <code>.pub</code> 的钥匙——先「生成密钥」，或给那把补出公钥后点台账的「刷新」。</p>
        </div>

        <div class="kops-pair">
          <div class="form-item-block">
            <label class="form-label" for="kops-dep-host">目标地址</label>
            <input id="kops-dep-host" v-model="host" type="text" class="form-text-input"
                   placeholder="192.168.1.10 或 hostname" autocomplete="off" :disabled="working" />
          </div>
          <div class="form-item-block">
            <label class="form-label" for="kops-dep-port">端口</label>
            <input id="kops-dep-port" v-model.number="port" type="number" min="1" max="65535"
                   class="form-text-input kops-port" :disabled="working" />
          </div>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="kops-dep-user">登录用户</label>
          <input id="kops-dep-user" v-model="user" type="text" class="form-text-input"
                 placeholder="它现在还得靠密码登录，部署完才谈得上免密" autocomplete="off" :disabled="working" />
        </div>

        <div class="form-item-block kops-secret-field">
          <label class="form-label" for="kops-dep-pass">一次性登录密码</label>
          <input id="kops-dep-pass" v-model="password" :type="showPass ? 'text' : 'password'"
                 class="form-text-input" placeholder="仅本次使用，不会保存"
                 autocomplete="off" spellcheck="false" :disabled="working" />
          <label class="toggle-label"><input id="kops-dep-show" v-model="showPass" type="checkbox" /> 显示密码</label>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="kops-dep-timeout">单段预算（ms）</label>
          <input id="kops-dep-timeout" v-model.number="timeoutMs" type="number" step="500"
                 min="0" :max="DEPLOY_TIMEOUT_MAX" class="form-text-input kops-port" :disabled="working" />
          <p class="hint-line">KEX / 认证 / 远端执行各按这个预算；留 0 走后端缺省，超上限由后端夹回 {{ DEPLOY_TIMEOUT_MAX }}。</p>
        </div>

        <div v-if="error" class="error-box">{{ error }}</div>

        <div class="btn-group kops-actions">
          <button class="btn btn-secondary" :disabled="working" @click="close">取消</button>
          <button id="kops-dep-submit" class="btn btn-primary" :disabled="!formReady" @click="submit">
            {{ working ? '⏳ 部署中…' : '部署公钥' }}
          </button>
        </div>
        <p class="hint-line">按钮不会替你把密码存到别处——提交一发出，本字段立即清空；成功后远端就多了一行公钥，回收需要你自己到远端删。</p>
      </template>

      <template v-else>
        <div class="kops-verdict">
          <UiStatusChip :tone="verdictTone">{{ verdictLabel }}</UiStatusChip>
          <span class="hint-dim mono">{{ chosenPub?.name ?? pubKeyPath }}</span>
        </div>

        <!-- reason 原样显示（后端已过打码），下面各分支文案只按 verdict 值加，
             不据 reason 内容改行为。 -->
        <p class="kops-message">{{ result.reason || '（后端未给说明）' }}</p>

        <!-- auth-rejected：API 层区分不了"密码不对"与"对方不接受密码登录"，
             两个可能都必须出现在界面上（源头注记见 deploy.go / sshauth.go）。 -->
        <UiBanner v-if="isAuthRejected" tone="warn">
          被拒有两种可能，这一层区分不了，两种都摆出来：① 密码不对；② 对方根本不接受密码登录
          （比如只允许密钥）。没写入任何内容。可以再试一次，或换别的途径到远端手工添加。
        </UiBanner>

        <!-- hostkey-changed：独立分支（红且特殊），认证之前就被中止。 -->
        <UiBanner v-else-if="isHostKeyChanged" tone="error">
          这台机器的 host key 与你之前认下的（钉定）不一致——可能是它重装了系统，也可能有人冒充。
          <strong>这次尝试连密码都没有发出去。</strong>
          <dl class="kops-receipt">
            <div><dt>本次指纹</dt><dd class="mono">{{ result.hostKeyFingerprint || '（未取到）' }}</dd></div>
            <div><dt>目标</dt><dd class="mono">{{ host }}:{{ port }}</dd></div>
          </dl>
          出口：确认这确实是那台机器合法重装后，到「主机清单」页的主机密钥钉定列表里
          <strong>清除该地址的钉定</strong>，再回来重新部署——清除需要回显确认，钉定不会自动放行
          （自动放行等于 TOFU 只生效一次，之后与不校验没有区别）。
        </UiBanner>

        <UiBanner v-else-if="isRefused" tone="error">
          本地拒发：公钥形态不合（空文件/非法行/含注入内容）或本地钉定存储读写失败，
          <strong>一个字节都没有对外发送</strong>，对端不会有任何记录。
        </UiBanner>

        <UiBanner v-else-if="result.verdict === 'timeout'" tone="warn">
          预算耗尽，远端是否写入不确定。重新点一次部署即可对账——已写入的话会报「本来就有」，不会重复追加。
        </UiBanner>

        <UiBanner v-else-if="result.verdict === 'not-ssh'" tone="error">
          端口通，但应答不是 SSH 服务：地址或端口指到了别的程序上。部署未发生。
        </UiBanner>

        <UiBanner v-else-if="result.verdict === 'unreachable'" tone="error">
          没连上，或 SSH 会话没谈成；公钥未确认写入，远端不保证有改动。
        </UiBanner>

        <dl v-if="isDeployed" class="kops-receipt">
          <!-- 远端自报的路径：界面只转述，不背书。说"已验证"是吹的——本地无从核对
               远端文件真 content，顶多下一趟重跑报"本来就有"。 -->
          <div><dt>远端告知的写入位置</dt><dd class="mono">{{ result.authorizedKeysPath || '（远端未回）' }}</dd></div>
          <div><dt>装上的公钥</dt><dd class="mono">{{ result.pubKeyFingerprint || '（未取到）' }}</dd></div>
          <div v-if="result.hostKeyFingerprint"><dt>主机密钥</dt><dd class="mono">{{ result.hostKeyFingerprint }}</dd></div>
        </dl>
        <p v-if="isDeployed" class="hint-line">
          {{ result.verdict === 'appended' ? '本次新增了一行。' : '远端本来就有这一行，本次未改动。' }}
          建议核对你认知中这台机器的 host key 指纹；不放心就在远端删掉刚写入的行再回来重查。
        </p>

        <div class="btn-group kops-actions">
          <button id="kops-dep-retry" class="btn btn-secondary" :disabled="working" @click="resetForRetry">再试一次</button>
          <button class="btn btn-secondary" @click="close">关闭</button>
        </div>
      </template>
    </div>
  </KeyOpsModal>
</template>

<!-- 共享布局表单源（keyopsDialogs.css）；下面仅本件真差异：两列并排的紧凑形。 -->
<style scoped src="./keyopsDialogs.css"></style>
<style scoped>
.kops-pair { display: flex; gap: 10px; flex-wrap: wrap; }
.kops-pair .form-item-block { flex: 1; min-width: 160px; }
.kops-port { width: 110px; }
.kops-verdict { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
</style>
