<script setup lang="ts">
// 本机密钥台账（钥匙环）。
//
// 结构取舍（对着上一轮五条硬伤逐条解）：
// ① **行 = 一把钥匙**：数据源本就是 KeyEntry（一把一条），私钥与配套公钥同在一行，
//    不再出现"同指纹两行、删错东西"的形状。
// ② 整页级解释**只说一次**：页头一句边界话 + 表头上方一条风险概要（可摊开看含义），
//    行内只有短标签与展开区里与"这一把"相关的短句。
// ③ 空态给三步出路 + 页内唯一主按钮，不停在"暂无数据"。
// ④ 动作分权重：动作段只放非破坏动作且同档同高（「部署公钥」常驻，不靠 hover 出现；
//    缺配套 .pub 的行就地多给一颗「补出公钥」——否则部署钮永远灰着，是操作死角）；
//    改口令 / 复制指纹 / 成对删除 / 删除公钥收进展开区（次要区），删除类用危险
//    前景色并靠间距与其它项分离——它们不可逆。入口条件由 useKeyOps 的
//    canExportPub/canDeletePub 单源给出（两类半把行互斥，模板不散判）。
// ⑤ 「在用」列直接渲染后端 usedBy（防误删的主信号），不自行推断。
//
// 全部列/按钮/表格都用全局原子（.card/.chip/.link-button/.btn-*/.mono/.hint-line），
// 本层只加行布局；不新增彩色底（buttonColorContract 扫 .vue scoped），动作容器挂
// .btn-group（含 flex-wrap，窄窗整颗换行——"刷新网卡被挤出窗口"是实测事故）。
import { computed, onMounted, ref } from 'vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import UiBanner from '../ui/UiBanner.vue'
import KeyOpsDialogs from './KeyOpsDialogs.vue'
import { guardBadge, keyRisk, usedByOf, useKeyring } from './useKeyring'
import { canDeletePub, canExportPub } from './useKeyOps'
import type { KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { useClipboard } from '../../composables/useClipboard'

const { keys, loading, error, stale, lastLoaded, refresh } = useKeyring()
const { copyWithToast } = useClipboard()

// 六个写动作（生成 / 改口令 / 成对删除 / 删除公钥 / 补出公钥 / 部署公钥）的对话框
// 编排层。面板只负责把"行 + 意图"递给对应的窗，窗里成功回执后再触发台账刷新。
const ops = ref<InstanceType<typeof KeyOpsDialogs> | null>(null)

// 展开区（次要区）的开合按钥匙基名记；一把钥匙一行，键即唯一。
const expanded = ref<Set<string>>(new Set())
// 风险概要的含义说明默认收起（概要一句话常驻，点开了才见长解释——整页只此一处）。
const riskOpen = ref(false)

onMounted(refresh)

const risk = computed(() => keyRisk(keys.value))
const riskLine = computed(() => {
  const parts: string[] = []
  if (risk.value.unguarded > 0) parts.push(`${risk.value.unguarded} 把无口令保护`)
  if (risk.value.unknown > 0) parts.push(`${risk.value.unknown} 把判不出来`)
  if (!parts.length) return `${risk.value.withPrivate} 把都有口令保护`
  return `${risk.value.withPrivate} 把里有 ${parts.join(' · ')}`
})

function isOpen(name: string): boolean {
  return expanded.value.has(name)
}

function toggleRow(name: string) {
  const next = new Set(expanded.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  expanded.value = next
}

// metaLine：一行元数据（算法 · 位数 · 注释）。mono 只给机器值（名称/路径/指纹），
// 这行走普通字——注释是自然语言，塞进等宽反而难读。
function metaLine(k: KeyEntry): string {
  const parts: string[] = []
  if (k.type) parts.push(k.bits ? `${k.type} · ${k.bits}` : k.type)
  else parts.push('算法判不出')
  if (k.comment) parts.push(k.comment)
  return parts.join(' · ')
}

function fmtMtime(ts: number): string {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString()
}

// copyFingerprint 把"可选字段"的判空收在脚本侧（模板里 v-if 的缩窄 vue-tsc
// 不会带进事件处理器）；没有指纹时钮根本不在场，这里再兜一层。
function copyFingerprint(k: KeyEntry) {
  if (k.fingerprint) copyWithToast(k.fingerprint)
}

// useLine：只在**判过**的时候说话。null=未判 → 整条不渲染（宁缺勿错），
// 因为"清单里没有主机用它"会被当成"可以放心删"的凭据。
function useLine(k: KeyEntry): string {
  const u = usedByOf(k)
  if (u === null) return ''
  return u.length ? `被 ${u.length} 台主机在用：${u.join('、')}` : '清单里没有主机用它'
}

// keyNote：展开区里"与这一把相关"的一句。写这把的结构性事实（缺什么、能不能部署、
// 用哪颗钮），不搬整页级的安全长句——那句在风险概要里全页说一次，行内复读就是上轮的硬伤②。
// 旧文案"带口令的私钥本层不解密，只能你本人 ssh-keygen -y"已撤：公钥段在私钥文件的
// 明文外层，后端 ExportPub 带口令也补得动且不接触口令——那句把能点的钮说成了人工活。
function keyNote(k: KeyEntry): string {
  if (!k.hasPrivate) {
    return '只有公钥文件（孤儿 .pub）：改口令与成对删除都以私钥为对象，对它不适用；要清掉这一行就用下方「删除公钥」——只删本机这个文件，远端机器上若装过它，那一行授权不会被回收。'
  }
  if (!k.hasPublic) {
    return `缺配套 .pub：补出来之前不能部署；用上方「补出公钥」从私钥算出公钥写入 ${k.name}.pub（公钥段本就明文，带口令也补得动，全程不碰口令）。`
  }
  return '改口令不改变指纹——已部署到服务器的授权照常可用；删除只动本机文件，远端那一行不会被回收。'
}
</script>

<template>
  <section class="card">
    <div class="card-top">
      <div>
        <h2>本机密钥台账</h2>
        <!-- 边界话全页只在这里说一句（上一轮页头+卡片+行内三层复读，是点名的硬伤②）。 -->
        <p class="hint-line">
          列出 <code>~/.ssh</code> 里的钥匙，<strong>一把钥匙一行</strong>。
          指纹与注释取自公钥侧，本台账不读取私钥内容。
        </p>
      </div>
      <!-- 主操作与次级钮同档同高（都是 btn-secondary btn-small）。空台账时这里
           不放「生成密钥」——出路在下方空态的主按钮，避免页内出现两颗生成入口。 -->
      <div class="btn-group">
        <button v-if="keys.length" id="kops-open-generate" class="btn btn-secondary btn-small" :disabled="loading" @click="ops?.openGenerate()">
          生成密钥
        </button>
        <button id="kops-refresh" class="btn btn-secondary btn-small" :disabled="loading" @click="refresh">
          {{ loading ? '读取中…' : '刷新' }}
        </button>
      </div>
    </div>

    <!-- 读失败：错误如实上屏；有旧数据时**保留上次的表**并明说它是旧的（不只用
         降透明度表达——状态必须可读出来，这是本仓的无障碍红线）。 -->
    <div v-if="error" class="error-box">{{ error }}</div>
    <UiBanner v-if="stale" tone="warn">
      下表仍是 <strong>{{ lastLoaded }}</strong> 的读取结果，<strong>可能已过期</strong>；
      恢复刷新成功前，别据它做删除或部署决定。
    </UiBanner>

    <!-- 加载中指名在读什么，不是一句"加载中"。 -->
    <div v-if="loading && !keys.length" class="empty-hint">正在读取 <code>~/.ssh</code> 里的密钥台账…</div>

    <!-- 空态：三步出路 + 页内唯一主按钮（他现在的真实状态就是这个，不能停在"暂无"）。 -->
    <div v-else-if="!keys.length && !error" class="empty-state keyring-empty">
      <p class="keyring-empty-lead">本机还没有 SSH 密钥。三步开始：</p>
      <ol class="keyring-steps">
        <li>生成一把（注释填用途——日后在服务器名册里全靠它认钥匙）</li>
        <li>把公钥部署到目标服务器（需要一次密码，仅这一次）</li>
        <li>登记进主机清单，以后一句话就能连</li>
      </ol>
      <button id="kops-empty-generate" class="btn btn-primary" :disabled="loading" @click="ops?.openGenerate()">
        生成第一把钥匙
      </button>
      <p class="hint-line">目录不存在属于正常空态，不是故障。</p>
    </div>

    <template v-else-if="keys.length">
      <!-- 风险概要：整页唯一一处安全解释，一句话常驻 + 点开看含义。行内只有短标签。 -->
      <div v-if="risk.withPrivate" class="keyrisk">
        <button
          v-if="risk.unguarded || risk.unknown"
          type="button"
          class="link-button keyrisk-toggle"
          :aria-expanded="riskOpen ? 'true' : 'false'"
          @click="riskOpen = !riskOpen"
        >{{ riskLine }}（看含义）</button>
        <span v-else class="keyrisk-plain">{{ riskLine }}</span>
        <p v-if="riskOpen" class="keyrisk-mean">
          <strong>无口令</strong>——任何能以你身份读到该文件的程序，都能直接用它登录已授权过的机器。
          <strong>判不出来</strong>——文件形态本层认不得，未做猜测；请自行确认，别当成已加固。
          删除前看每行的「在用」：清单还在引用的钥匙后端会拒绝删除。
        </p>
      </div>

      <div class="keylist">
        <div v-for="k in keys" :key="k.name" class="keyrow">
          <div class="keyrow-head">
            <!-- 主段：名称（mono，机器值）+ 一行元数据。路径不占主视觉，进展开区。 -->
            <div class="keyrow-main">
              <span class="mono keyrow-name">{{ k.name }}</span>
              <div class="keyrow-meta">{{ metaLine(k) }}</div>
            </div>

            <div class="keyrow-side">
              <!-- 状态段：口令短标签 chip + 「在用」信号（防误删的主信号，直接渲染 usedBy）。 -->
              <div class="keyrow-status">
                <UiStatusChip :tone="guardBadge(k).tone">{{ guardBadge(k).label }}</UiStatusChip>
                <span v-if="!k.hasPrivate" class="keyrow-note">公钥不涉及口令</span>
                <span v-if="useLine(k)" class="keyrow-use">{{ useLine(k) }}</span>
              </div>
              <!-- 动作段只放非破坏动作：「部署公钥」常驻；缺配套 .pub 的行同排多给
                   一颗「补出公钥」（canExportPub），把"部署永远灰着"的死角接上。
                   删除类/改口令不在这排——它们收进展开区，见下。 -->
              <div class="btn-group keyrow-acts">
                <button
                  v-if="canExportPub(k)"
                  type="button"
                  class="btn btn-secondary btn-small kops-act-export"
                  :disabled="loading"
                  title="从私钥补出配套 .pub（公钥段在明文外层，带口令也补得动，不接触口令）"
                  @click="ops?.openExportPub(k)"
                >补出公钥</button>
                <button
                  type="button"
                  class="btn btn-secondary btn-small kops-act-deploy"
                  :disabled="!k.hasPublic || loading"
                  :title="k.hasPublic ? '把这把公钥装到目标服务器' : '缺配套 .pub，无法部署——先点「补出公钥」补出来'"
                  @click="ops?.openDeploy(k)"
                >部署公钥</button>
                <button
                  type="button"
                  class="link-button keyrow-toggle"
                  :aria-expanded="isOpen(k.name) ? 'true' : 'false'"
                  :aria-controls="`keyrow-detail-${k.name}`"
                  @click="toggleRow(k.name)"
                >{{ isOpen(k.name) ? '收起' : '展开' }}</button>
              </div>
            </div>
          </div>

          <!-- 展开区 = 次要区：路径、完整指纹（可复制）、修改时间、这一把的一句说明，
               以及改口令 / 删除。删除与其余项空间分离并用危险色。 -->
          <div v-if="isOpen(k.name)" :id="`keyrow-detail-${k.name}`" class="keyrow-detail">
            <dl class="keyrow-dl">
              <div>
                <dt>私钥</dt>
                <dd class="mono">{{ k.privatePath || '无——这把只有公钥' }}</dd>
              </div>
              <div>
                <dt>公钥</dt>
                <dd class="mono">{{ k.publicPath || '缺配套 .pub' }}</dd>
              </div>
              <div>
                <dt>指纹</dt>
                <dd class="mono">
                  <template v-if="k.fingerprint">
                    {{ k.fingerprint }}
                    <button
                      type="button"
                      class="link-button kops-act-fpcopy"
                      :title="`复制完整指纹 ${k.fingerprint}`"
                      @click="copyFingerprint(k)"
                    >复制</button>
                  </template>
                  <template v-else>取不到（缺 .pub 且私钥带口令时本层读不了私钥侧）</template>
                </dd>
              </div>
              <div>
                <dt>修改</dt>
                <dd>{{ fmtMtime(k.mtimeUnix) }}</dd>
              </div>
            </dl>
            <p class="hint-line">{{ keyNote(k) }}</p>
            <!-- 依赖私钥的动作（改口令/成对删除）只在有私钥的行出现；孤儿 .pub 行
                 给的是「删除公钥」（canDeletePub）——两类半把行各给各的出口，互斥。 -->
            <div v-if="k.hasPrivate" class="btn-group keyrow-sec">
              <button type="button" class="link-button kops-act-pass" @click="ops?.openPassphrase(k)">改口令</button>
              <button type="button" class="link-button kops-act-del" @click="ops?.openDelete(k)">删除（成对删，不可逆）</button>
            </div>
            <div v-else-if="canDeletePub(k)" class="btn-group keyrow-sec">
              <button type="button" class="link-button kops-act-delpub" @click="ops?.openDeletePub(k)">删除公钥（不可逆）</button>
            </div>
          </div>
        </div>
      </div>

      <p class="hint-line keyring-count">共 {{ keys.length }} 把 · 上次读取 {{ lastLoaded }}</p>
    </template>

    <!-- 六个写动作的对话框常挂在这（不随表格分支进出，见 KeyOpsDialogs 头注）。 -->
    <KeyOpsDialogs ref="ops" :keys="keys" @changed="refresh" />
  </section>
</template>

<style scoped>
/* 只补行布局；卡片、芯片、按钮档、错误框、空态全部走全局原子。本层零彩底。 */
.card-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

/* —— 风险概要条 —— */
.keyrisk {
  margin: 0 0 10px;
}
.keyrisk-toggle {
  font-weight: 600;
}
.keyrisk-plain {
  color: var(--color-text-muted);
  font-size: var(--text-sm);
}
.keyrisk-mean {
  margin: 6px 0 0;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  line-height: 1.7;
}

/* —— 行 = 一把钥匙：桌面 minmax(0,1fr) auto（MemoCard 列表档同款语法），
      左主段、右侧状态+动作；~760 以下竖排转段，不挤列。 —— */
.keylist {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-panel);
  overflow: hidden;
}
.keyrow + .keyrow {
  border-top: 1px solid var(--color-border);
}
.keyrow-head {
  display: grid;
  /* 三段主轨：主内容 / 状态 / 动作。两段式（1fr auto）会把状态与动作挤进同一列，
     别名一多就换行、行高抖动——这是上一轮按规格实现后现场报备的硬凑点，改回三段。 */
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: start;
  gap: 6px 14px;
  padding: 10px 12px;
}
.keyrow-main {
  min-width: 0;
}
.keyrow-name {
  font-weight: 600;
  overflow-wrap: anywhere;
}
.keyrow-meta {
  margin-top: 3px;
  color: var(--color-text-muted);
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}
.keyrow-side {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px 14px;
}
.keyrow-status {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.keyrow-note {
  color: var(--color-text-subtle);
  font-size: var(--text-xs);
}
.keyrow-use {
  color: var(--color-text-muted);
  font-size: var(--text-xs);
  overflow-wrap: anywhere;
}

/* —— 展开区 —— */
.keyrow-detail {
  padding: 2px 12px 10px;
  border-top: 1px dashed var(--color-border);
}
.keyrow-dl {
  margin: 8px 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.keyrow-dl > div {
  display: grid;
  grid-template-columns: minmax(72px, auto) minmax(0, 1fr);
  gap: 10px;
  align-items: baseline;
}
.keyrow-dl dt {
  color: var(--color-text-subtle);
  font-size: var(--text-xs);
}
.keyrow-dl dd {
  margin: 0;
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}
.keyrow-sec {
  margin-top: 6px;
}
/* 危险项（成对删除 / 删除公钥）：与其它项空间分离 + 危险前景色
   （只上色与距离，不造彩底——配色契约盯着）。 */
.keyrow-sec .kops-act-del,
.keyrow-sec .kops-act-delpub {
  margin-left: 18px;
  color: var(--state-danger);
}

.keyring-empty {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}
.keyring-empty-lead {
  margin: 0;
  font-weight: 600;
}
.keyring-steps {
  margin: 0;
  padding-left: 22px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  line-height: 1.7;
}
.keyring-count {
  margin-top: 8px;
}

@media (max-width: 760px) {
  /* 窄屏：行转竖排（主段 / 状态段 / 动作段依次成行），而不是把三段挤进一列。 */
  .keyrow-head {
    grid-template-columns: 1fr;
  }
  .keyrow-side {
    justify-content: flex-start;
    width: 100%;
  }
  .keyrow-status {
    justify-content: flex-start;
  }
}
</style>
