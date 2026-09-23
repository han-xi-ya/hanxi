<script setup lang="ts">
// ddns-go 控制台（Wave 5 · 批 0 共享契约收敛件；模式照抄 ccswitch 黄金样本）：
// 共享面全部收敛进 components/managed 托管控制台家族——adapter（src/adapters/ddnsgo）
// 承载业务投影（启停/版本/联动 RPC + log/port 数据面），ManagedConsoleShell 管页头
// 与页签骨架，store 单源轮询/uptime/进度 map/busy 闩。本视图仅剩装配与
// #primary-action 第三钮、#console-extra 业务大件（监听地址行、进程日志面板、
// Web 监听端口行）与说明卡。
import { computed, nextTick, onMounted, ref } from 'vue'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/ddnsgo/instance/models'
import { createDdnsGoAdapter } from '../adapters/ddnsgo'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import { useWailsEvent } from '../composables/useWailsEvent'
import type { ManagedConsoleStore } from '../components/managed/store'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const adapter = createDdnsGoAdapter()
const { showToast } = useToast()

// ---------- listenAddr 投影 ----------
// listenAddr 属 ddnsgo 快照扩展字段（ManagedSnapshot 基型之外），共享状态头
// 不渲染模块字段；本视图经 Shell 槽作用域 snap 以 addrOf 收口 cast 后展示。
function addrOf(snap: unknown): string {
  return (snap as Snapshot | null | undefined)?.listenAddr ?? ''
}

// ---------- 进程输出日志面板（log 槽首个实战：数据面经 adapter.log，UI 在本视图） ----------
const MAX_LOG_LINES = 400
const logLines = ref<string[]>([])
const logAutoScroll = ref(true)
const logBodyRef = ref<HTMLElement | null>(null)

// DDNS 更新日志的成败着色（上游输出含中英文两种语言）
function logWarnish(line: string): boolean {
  return /失败|错误|异常|error|fail|refused|timeout/i.test(line) && !/未变化|no change/i.test(line)
}

async function loadLogHistory() {
  try {
    const lines = await adapter.log.pull()
    if (Array.isArray(lines) && lines.length) {
      logLines.value = lines.slice(-MAX_LOG_LINES)
      scrollToBottom()
    }
  } catch (e) {
    console.warn('ddnsgo Logs failed:', getErrorMessage(e))
  }
}

function appendLog(line: string) {
  logLines.value.push(line)
  if (logLines.value.length > MAX_LOG_LINES) {
    logLines.value.splice(0, logLines.value.length - MAX_LOG_LINES)
  }
  void scrollToBottom()
}

async function scrollToBottom() {
  await nextTick()
  const el = logBodyRef.value
  if (el && logAutoScroll.value) el.scrollTop = el.scrollHeight
}

function clearLogs() {
  logLines.value = []
}

// 事件流逐行接入（视图 setup 期订阅 adapter.log，随宿主卸载自动注销）
adapter.log.subscribe(appendLog)

// 状态从非运行切到 running 时重取日志历史（引擎环形缓冲随新进程重启）。
// instance-state 的快照订阅已被 store 占用，这里另起一个独立订阅只看该边沿——
// Wails runtime 支持同事件多监听器，互不影响；切换以事件推送为准（2.5s 轮询
// 仅是快照兜底，非迁移前 watch(snap) 的同义，实际转移恒有事件到达）。
let lastState = ''
useWailsEvent<Snapshot>('ddnsgo:instance-state', (s) => {
  if (!s) return
  const prev = lastState
  lastState = s.state
  if (s.state === 'running' && prev !== 'running' && prev !== 'starting') void loadLogHistory()
})

// ---------- Web 监听端口行（port 槽：读写经 adapter.port，输入态校验/回滚在本视图） ----------
const listenPort = ref(9876)
const portInput = ref(9876)
const portDirty = computed(() => String(portInput.value) !== String(listenPort.value))

async function applyPort() {
  const n = Number(portInput.value)
  if (!Number.isInteger(n) || n < 1024 || n > 65535) {
    showToast('端口需为 1024~65535 的整数')
    return
  }
  try {
    const res = await adapter.port.set(n)
    listenPort.value = n
    if (res.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(`设置失败: ${getErrorMessage(e)}`)
    portInput.value = listenPort.value
  }
}

// ---------- 第三钮「打开控制台」（#primary-action 槽注入） ----------
// P0 批 3·4.5：单飞从视图自建 busy 收编进共享 store.runExclusive——
// 本钮在途时启停/退出钮一并禁用，反之亦然，杜绝「多份 busy 真相」交叉并发；
// 成功后状态刷新由 instance-state 事件与轮询兜底。
async function openConsole(store: ManagedConsoleStore) {
  const r = await store.runExclusive(async () => {
    try {
      return { ok: true as const, data: await adapter.openConsole.run() }
    } catch (e) {
      return { ok: false as const, error: e as unknown }
    }
  })
  if (!r) return // 共享互斥闩占用中：重复点击直接丢弃
  if (r.ok) {
    if (r.data?.message !== undefined) showToast(r.data.message)
  } else {
    showToast(getErrorMessage(r.error))
  }
}

onMounted(async () => {
  void loadLogHistory()
  try {
    const p = await adapter.port.get()
    listenPort.value = p
    portInput.value = p
  } catch (e) {
    console.warn('ddnsgo GetListenPort failed:', getErrorMessage(e))
  }
})
</script>

<template>
  <ManagedConsoleShell
    class="ddnsgo-view"
    :adapter="adapter"
    title="ddns-go"
    subtitle="托管动态域名解析工具：版本管理、JobObject 启停与内嵌 Web 控制台。"
    console-tab-label="🌐 控制台"
    :banner-slim="false"
  >
    <!-- 第三钮：打开内嵌 Web 控制台（钮序保持原"启动 → 打开控制台 → 退出"） -->
    <template #primary-action="{ busy, state, store }">
      <button
        class="btn btn-primary btn-small"
        :disabled="busy || adapter.openConsole.disabledFor?.(state)"
        :title="adapter.openConsole.titleFor?.(state)"
        @click="openConsole(store)"
      >{{ adapter.openConsole.label }}</button>
    </template>

    <!-- 控制台内联大件：监听地址行 + 进程日志面板（log 槽 UI）+ Web 端口行（port 槽 UI） -->
    <template #console-extra="{ snap, state, busy }">
      <!-- listenAddr 展示：文案与样式类逐字保留，仅从状态头迁入槽位顶行 -->
      <div v-if="state === 'running' && addrOf(snap)" class="dd-addr-line">
        <span class="mono addr-tag">🖥 {{ addrOf(snap) }}</span>
      </div>

      <div class="dd-log-card">
        <div class="dd-log-head">
          <span class="dd-log-title">进程输出（更新动态 / 错误）</span>
          <div class="dd-log-tools">
            <label class="dd-auto-scroll"><input v-model="logAutoScroll" type="checkbox" />自动滚动</label>
            <button class="btn btn-secondary btn-small" @click="clearLogs">清屏</button>
          </div>
        </div>
        <div
          ref="logBodyRef"
          class="dd-log-body"
          @scroll.passive="logAutoScroll = ($event.target as HTMLElement).scrollTop + ($event.target as HTMLElement).clientHeight >= ($event.target as HTMLElement).scrollHeight - 40"
        >
          <template v-if="logLines.length">
            <div v-for="(line, i) in logLines" :key="i" class="dd-log-line" :class="{ 'dd-log-warn': logWarnish(line) }">{{ line }}</div>
          </template>
          <div v-else class="dd-log-empty">{{ state === 'running' ? '暂无输出（DDNS 按周期更新，静默即正常）' : '实例未运行，无进程输出' }}</div>
        </div>
      </div>

      <!-- Web 监听端口设置：原 extras-row 的 .port-ctrl 整行迁入（校验/回执文案逐字保留），
           复用 extras-card/extras-row 全局原子皮 -->
      <div class="extras-card">
        <div class="extras-row">
          <span class="port-ctrl">
            <span class="hint-dim">Web 监听端口</span>
            <input v-model.number="portInput" class="dd-port-input mono" type="number" min="1024" max="65535" />
            <button class="btn btn-secondary btn-small" :disabled="!portDirty || busy" @click="applyPort">应用</button>
          </span>
        </div>
      </div>
    </template>

    <!-- 控制台 Tab 主体：说明卡（可折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">什么是 ddns-go</summary>
      <div class="info-body">
        <p>开源动态域名解析工具（<a class="inline-link" href="https://github.com/jeessy2/ddns-go" target="_blank" rel="noopener">jeessy2/ddns-go</a>，MIT）：家用宽带无固定公网 IP 时，自动把最新 IP 同步到阿里云 / DNSPod / Cloudflare 等域名解析，支持 IPv4/IPv6 与十余家服务商。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
        <p class="hint-dim">安全边界：Hanxi 托管实例固定绑定 127.0.0.1（仅本机可访问面板）；需要把面板暴露到局域网请自行运行原版。首次使用请在面板设置用户名/密码。</p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/提示条/联动卡/版本区/页签与 flex 骨架全部由 managed 组件 +
   components.css 全局原子接管（原 .control-bar/.ver-pill 小圆角方片私有覆写
   与 .dd-status-light 复制体随迁入共享件而废止，落回标准形）；
   本页仅余 #console-extra 业务件样式。 */

/* 监听地址行：状态头无模块字段位，addr-tag 迁入控制台槽顶行 */
.dd-addr-line { display: flex; align-items: center; gap: 8px; }
.addr-tag { font-size: var(--text-xs); color: var(--color-text-subtle); }

/* 说明卡内联链接 */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

/* ---------- 进程输出日志面板（终端态：固定深底不随主题反相，tokens.css 设计决策；
   色阶由 --terminal-* 与 --ansi-* 派生，不再散落裸色。
   字号豁免：dd-log-title / dd-auto-scroll / dd-log-body 的 12px 属 ANSI 终端区，
   与后端输出行距互锁（§9.5-4 约定），不改、登记现值） ---------- */
.dd-log-card { border: 1px solid var(--terminal-border); border-radius: var(--radius-control); overflow: hidden; }
.dd-log-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 7px 12px; background: var(--terminal-head-bg);
  border-bottom: 1px solid var(--terminal-border);
}
.dd-log-title { font-size: 12px; font-weight: 600; color: var(--terminal-fg); }
.dd-log-tools { display: flex; align-items: center; gap: 8px; }
.dd-auto-scroll { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--terminal-meta-fg); cursor: pointer; }
.dd-auto-scroll input { accent-color: var(--color-primary); }
.dd-log-body {
  height: 180px; overflow-y: auto; padding: 10px 12px; background: var(--terminal-bg);
  font-family: var(--font-mono); font-size: 12px; line-height: 1.55;
  user-select: text;
}
.dd-log-line { white-space: pre-wrap; word-break: break-all; color: var(--terminal-row-fg); }
.dd-log-warn { color: var(--terminal-warn); }
.dd-log-empty { color: var(--terminal-faint-fg); text-align: center; padding: 28px 0; }

/* ---------- Web 端口行（card 皮用全局原子 extras-card/extras-row；本视图私有形） ---------- */
.port-ctrl { display: flex; align-items: center; gap: 8px; font-size: var(--text-base); }
.dd-port-input { width: 84px; padding: 4px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--surface-panel); font-size: var(--text-sm); }
</style>
