<script setup lang="ts">
// SSH 检查面板。2026-10-22 自 PortScanView 第三个标签页随能力搬入 sshkeys 模块，
// 现由 SshKeysView 承载（本面板不再挂在 PortScanView 下；搬入记录见
// docs/plans/PLAN_SSH_KEYS.md）。
//
// 两个语义不同的动作分成两个区块，而不是同一张卡里并列的两组按钮：
//   「连接测试」→ CheckSSH：协议第一回合版本交换，不碰凭据、不在对端 sshd 日志
//     留记录；结论止于"能不能连、是不是 SSH"，**不等于能登录**（成功横幅旁常驻
//     这句边界标注）。失败原文按后端结论呈现，**不按文本猜归类**——四态分桶等
//     结果对象补 outcome 枚举（PLAN_SSH_HOSTS_UX §12-1）。
//   「凭据核验」→ CheckSSHAuth：真的发起一次登录尝试，会在对端 sshd 留下
//     Failed password 记录、可能触发 fail2ban。整卡降一级（沉底卡片 + 危险邻近
//     色标注区），且后果说明**常驻 DOM**——hover 与 toast 不是发现途径。
// 口令纪律：默认掩码（本框历史上刻意明文，理由"测试场景要能看见"在跨机器凭据
// 场景已废，见模板内注释）；成功/失败/清除/切页/关窗任一出口即清空；不进复制
// 全文、结果对象、localStorage、console。行为锁在 __tests__/SSHCheckPanel.spec.ts。
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import { SSH_AUTH_LABEL, SSH_AUTH_TONE, SSH_PORT_PRESETS, useSSHCheck } from './useSSHCheck'

const {
  form, cred, showPass, optsOpen, timeoutMs,
  result, auth, connectError, authError, checking, busyKind,
  test, testAuth, copyResult, clearPass,
} = useSSHCheck()

// 暴露给宿主视图：切走本页签时清口令。
// 为什么必须外部补这一刀：页签切换用 v-show（刻意保状态），**不触发生命周期**；
// onDeactivated 只在换路由时响。所以"填了口令没点试登录就切去别的签"这条，
// 面板自己看不见——口令不属于该保留的状态，必须由持有者收口。
defineExpose({ clearPass })
</script>

<template>
  <div class="ssh-check">
    <!-- 区块一：连接测试（主动作档） -->
    <section class="section-card ssh-connect-block">
      <div class="card-header">
        <div class="header-left">
          <h2>连接测试</h2>
          <span v-if="result?.ok" class="badge-tag mono">{{ result.protocol === '2.0' ? 'SSH 2.0' : `SSH ${result.protocol}` }}</span>
        </div>
        <button v-if="result || auth" class="btn btn-ghost btn-small" @click="copyResult">📋 复制结果</button>
      </div>

      <p class="hint-line">只做协议版本交换，不碰凭据，也不在对端 sshd 日志留记录。</p>

      <div class="check-row btn-group">
        <input v-model="form.host" type="text" class="input check-host" placeholder="127.0.0.1" aria-label="SSH 主机地址" :disabled="checking" />
        <span class="check-colon" aria-hidden="true">:</span>
        <input v-model.number="form.port" type="number" min="1" max="65535" class="input check-port" aria-label="SSH 端口" :disabled="checking" />
        <button class="btn btn-secondary" :disabled="checking" @click="test">
          {{ checking ? '⏳ 进行中…' : '🔌 连接测试' }}
        </button>
      </div>

      <div class="quick-targets btn-group">
        <span class="quick-label">常用端口:</span>
        <button
          v-for="preset in SSH_PORT_PRESETS"
          :key="preset.port"
          type="button"
          class="btn btn-quick"
          :title="preset.title"
          :disabled="checking"
          @click="form.port = preset.port"
        >{{ preset.port }}</button>
      </div>

      <div class="check-advanced btn-group">
        <button type="button" class="btn btn-ghost btn-small" :disabled="checking" :aria-expanded="optsOpen" @click="optsOpen = !optsOpen">超时</button>
      </div>

      <div v-if="optsOpen" class="check-subrow">
        <label class="param-item">连接预算
          <input v-model.number="timeoutMs" type="number" step="500" min="0" max="30000" class="input check-num" :disabled="checking" />
          <span class="unit">ms</span>
        </label>
        <span class="hint-line">两个测试共用这一条预算</span>
      </div>

      <UiBanner v-if="connectError" tone="error">⚠️ {{ connectError }}</UiBanner>
      <div v-if="checking && busyKind === 'connect'" class="state-box">
        <span class="live-pulse">⏳ 正在与对端交换 SSH 版本横幅…</span>
      </div>

      <div v-if="result" class="check-verdict">
        <UiStatusChip :tone="result.ok ? 'positive' : 'danger'">{{ result.ok ? '可达' : '不可达' }}</UiStatusChip>
        <div class="check-verdict-body">
          <div class="check-meta">{{ result.addr }} · {{ result.latencyMs }}ms</div>
          <template v-if="result.ok">
            <div class="check-server mono">{{ result.serverVersion }}</div>
            <div class="verdict-scope">这只说明对方是 SSH 服务，不代表你能登录——要测这组用户名密码，用「凭据核验」。</div>
          </template>
          <template v-else>
            <div class="check-reason">{{ result.error || '未知原因' }}</div>
            <div class="verdict-scope">失败原因按后端原文呈现、未做归类（不拿文本猜结论）；待结果对象补 outcome 枚举后再分四态。</div>
          </template>
        </div>
      </div>
      <div v-else-if="!checking && !connectError" class="empty-hint">填好主机与端口，点「连接测试」</div>
    </section>

    <!-- 区块二：凭据核验（次级档卡片；后果说明常驻，见 .auth-notice） -->
    <section class="section-card ssh-auth-block">
      <div class="card-header">
        <div class="header-left">
          <h2>凭据核验</h2>
          <span class="badge-tag">真实登录尝试</span>
        </div>
      </div>

      <div class="auth-notice">
        ⚠️ 这一步会向对方发起一次真实登录尝试，会在其 sshd 日志留记录，可能触发 fail2ban。
        密码只活在这次调用里：不保存、不进结果、复制全文里也没有。
      </div>
      <p class="hint-line">对端地址与超时预算取自「连接测试」那一卡。</p>

      <div class="check-subrow">
        <label class="param-item">用户名
          <input v-model="cred.user" type="text" class="input check-cred-input" placeholder="登录用户名" autocomplete="off" :disabled="checking" />
        </label>
      </div>

      <div class="check-subrow">
        <label class="param-item">密码
          <!-- 默认掩码。本框历史上是 type="text"，旧注释的理由是"测试场景要能看见
               粘贴来的字符"——那个理由在跨机器凭据场景不成立，已废：
               ① 泄露后果量级不同：这里装的是别人家 sshd 的登录口令，肩屏、截图、
                  远程协助一眼过去就是账户被盗的素材，不是本机可弃参数；
               ② 暴露时长不同：明文是常驻可见（DOM 与屏幕上都摆着），旧理由只覆盖
                  "输入那一瞬"的核对需求；
               ③ 掩码 + 显式「显示」勾选是更好的载体：要核对粘贴字符时勾一下，
                  暴露这个代价由用户主动、显式地承担，而不是默认白送。 -->
          <input
            v-model="cred.pass"
            :type="showPass ? 'text' : 'password'"
            class="input check-cred-input ssh-pass"
            placeholder="登录密码"
            autocomplete="off"
            :disabled="checking"
          />
        </label>
        <label class="checkbox-label">
          <input type="checkbox" v-model="showPass" :disabled="checking" />
          显示
        </label>
      </div>

      <div class="btn-group">
        <button class="btn btn-secondary" :disabled="checking || !cred.user.trim() || !cred.pass" @click="testAuth">
          🔑 试登录
        </button>
        <button class="btn btn-ghost btn-small" :disabled="!cred.pass" @click="clearPass">清除口令</button>
      </div>

      <UiBanner v-if="authError" tone="error">⚠️ {{ authError }}</UiBanner>
      <div v-if="checking && busyKind === 'auth'" class="state-box">
        <span class="live-pulse">⏳ 正在向对端发起登录尝试…这一步会在其日志留记录</span>
      </div>

      <div v-if="auth" class="check-verdict">
        <UiStatusChip :tone="SSH_AUTH_TONE[auth.verdict] ?? 'neutral'">{{ SSH_AUTH_LABEL[auth.verdict] ?? auth.verdict }}</UiStatusChip>
        <div class="check-verdict-body">
          <div class="check-meta">凭据核验 · {{ auth.addr }} · {{ auth.latencyMs }}ms</div>
          <div class="check-reason">{{ auth.reason || '（无说明）' }}</div>
          <div v-if="auth.verdict === 'wrong-credentials'" class="verdict-scope">
            「被拒绝」有两种可能：密码不对，或对方不接受密码登录（例如只允许密钥）——后端在 API 层面区分不了，两种可能并列给出，不猜。
          </div>
          <div v-if="auth.hostKeyFingerprint" class="check-meta">
            主机密钥 <span class="mono">{{ auth.hostKeyType }}</span> 指纹
            <span class="mono">{{ auth.hostKeyFingerprint }}</span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<!-- 布局层沿用 portscan 家族那份单源共享表（卡片壳、check-* 行、verdict 条）。
     **刻意不复制第二份**——那正是这仓库反复清理的"同一形制各写一遍"病根；
     跨模块目录引用是临时耦合，已登记为 docs/plans/PLAN_SSH_KEYS.md 的已知债，
     待家族共享形制上收为真共享层后再改指。下方本地块只补两区块网格、卡片
     降档与标注线：颜色全部取自既有 token，零自写彩色底。 -->
<style scoped src="../portscan/portscanPanels.css"></style>
<style scoped>
/* 两区块并排；窄屏 <760 转单列（机主纪律，与全库 760 断点口径一致）。 */
.ssh-check { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; align-items: start; }
@media (max-width: 760px) { .ssh-check { grid-template-columns: 1fr; } }

/* 凭据核验整卡沉一级（surface-soft），从形制上从属于「连接测试」主卡；
   降级由卡片本体与危险邻近色标注区承担，动作钮**不降档**——两区块主钮同档
   同高（.btn-secondary 标准高），次级钮同档同高（.btn-ghost .btn-small）。 */
.ssh-auth-block { background: var(--surface-soft); }
.ssh-auth-block .card-header h2 { color: var(--color-text-muted); }
.ssh-auth-block .check-verdict { background: var(--surface-panel); }

/* 危险邻近色提示区：后果说明常驻于此，不进 tooltip、不进 toast。红色只落在
   左边框做结构强调，内容仍由文字承载（色不作唯一通道）；3px 左边框循
   FrpcProjectEditor .is-visitor 既有先例。 */
.auth-notice {
  font-size: var(--text-sm); line-height: 1.65; color: var(--color-text);
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-left: 3px solid var(--state-danger);
  border-radius: var(--radius-control);
  padding: 8px 12px;
}

/* 结论的"边界标注"行：比正文小一档、subtle 色——它不抢结论本体，但必须在场。 */
.verdict-scope { font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.5; }
</style>
