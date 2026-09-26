<script setup lang="ts">
// Piik 控制台：ManagedConsoleShell 标准壳装配（服务型骨架首个消费件，PLAN_PIIK
// _HOSTING ②/⑥ 裁决对位）——业务 RPC/事件/文案投影全部收在 src/adapters/piik，
// 本页仅剩三件事：装配、#primary-action 第三钮「打开界面」（后端 OpenWindow
// 负裁决——绝不冷启动，钮位形制照 ddnsgo openConsole 先例）、专属「邀请面板」
// 区块（读 GetStatus 组合快照投影：本机界面地址 + LAN/公网邀请链接 + 访问口令
// 徽标）。
//
// 安全红线（models.go gateViewFromSnapshot 结构性纪律）：LocalAccess 访问口令
// 只呈现「已设访问口令」布尔徽标，**永不渲染口令值**——本视图只读 PiikStatus
// 声明的投影字段，即便后端快照意外回带口令原文也无任何渲染路径触达它
// （spec 有 not.toContain 反证锁）。
import type { PiikStatus } from '../adapters/piik'
import { createPiikAdapter } from '../adapters/piik'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import type { ManagedConsoleStore } from '../components/managed/store'
import { useClipboard } from '../composables/useClipboard'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const adapter = createPiikAdapter()
const { copyWithToast } = useClipboard()
const { showToast } = useToast()

// 快照业务扩展字段经此单一 cast 位收口（共享壳按 ManagedSnapshot 宽型回传）
function piikOf(snap: unknown): PiikStatus {
  return (snap as PiikStatus | null) ?? EMPTY_STATUS
}
const EMPTY_STATUS: PiikStatus = {
  state: '',
  version: '',
  pid: 0,
  error: '',
  startedAt: '',
  listenPort: 0,
  consoleUrl: '',
  localAccessOpen: false,
  passwordSet: false,
  lanInvitation: '',
  publicInvitation: '',
  noBrowser: false,
  dataDir: '',
  configPath: '',
  logDir: '',
  drifted: false,
  driftNote: '',
}

// 「打开界面」单飞走共享 store.runExclusive（P0 批 3·4.5 口径，ddnsgo
// openConsole 同形制）：在途时启停/退出钮一并闩住，杜绝多份 busy 真相。
async function openUI(store: ManagedConsoleStore) {
  const r = await store.runExclusive(async () => {
    try {
      return { ok: true as const, data: await adapter.openUI.run() }
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
</script>

<template>
  <ManagedConsoleShell
    class="piik-view"
    :adapter="adapter"
    title="Piik"
    subtitle="托管 headless 屏幕分享服务：版本管理、启停与邀请链接分发，操作界面在系统浏览器。"
    tab-id-prefix="piik"
    tab-label="Piik 主选项卡"
    console-tab-label="🌐 控制台"
  >
    <!-- 第三钮：打开界面（后端 OpenWindow，负裁决不冷启动——未运行时如实禁用
         并在 title 指路「启动」）；钮序保持"启动 → 打开界面 → 退出" -->
    <template #primary-action="{ busy, state, store }">
      <button
        class="btn btn-primary btn-small"
        :disabled="busy || adapter.openUI.disabledFor?.(state)"
        :title="adapter.openUI.titleFor?.(state)"
        @click="openUI(store)"
      >{{ adapter.openUI.label }}</button>
    </template>

    <!-- 控制台 Tab 主体：邀请面板（running 态专属）+ 说明卡 -->
    <template #default="{ snap, state, store }">
      <div v-if="state === 'running'" class="extras-card share-card">
        <div class="share-head">
          <span class="share-title">邀请与链接</span>
          <span
            v-if="piikOf(snap).passwordSet"
            class="chip chip-warning pwd-chip"
            title="已设访问口令；口令值不在本页展示，请在你自己的配置（client.json）中查看"
          >已设访问口令</span>
        </div>

        <div class="extras-row share-row">
          <span class="share-label">本机界面地址</span>
          <span class="mono share-url">{{ piikOf(snap).consoleUrl || '（后端未回带地址）' }}</span>
          <button
            class="btn btn-secondary btn-small"
            :disabled="!piikOf(snap).consoleUrl"
            title="复制本机界面地址"
            @click="copyWithToast(piikOf(snap).consoleUrl, '本机界面地址已复制')"
          >复制</button>
          <button
            class="btn btn-primary btn-small"
            title="用系统浏览器打开本机界面（Piik 无自有窗口，界面恒在浏览器）"
            @click="openUI(store)"
          >在浏览器打开</button>
        </div>

        <div v-if="piikOf(snap).lanInvitation" class="extras-row share-row">
          <span class="share-label">局域网邀请链接</span>
          <span class="mono share-url">{{ piikOf(snap).lanInvitation }}</span>
          <button
            class="btn btn-secondary btn-small"
            title="复制局域网邀请链接"
            @click="copyWithToast(piikOf(snap).lanInvitation, '局域网邀请链接已复制')"
          >复制</button>
          <span class="hint-dim">同网段设备可直接加入</span>
        </div>

        <div v-if="piikOf(snap).publicInvitation" class="extras-row share-row">
          <span class="share-label">公网邀请链接</span>
          <span class="mono share-url">{{ piikOf(snap).publicInvitation }}</span>
          <button
            class="btn btn-secondary btn-small"
            title="复制公网邀请链接"
            @click="copyWithToast(piikOf(snap).publicInvitation, '公网邀请链接已复制')"
          >复制</button>
          <span class="chip chip-information">经 Cloudflare 中转</span>
        </div>
      </div>

      <details class="info-details">
        <summary class="info-summary">什么是 Piik 托管</summary>
        <div class="info-body">
          <p>Piik 是 <b>headless</b> 屏幕分享服务（上游 TNTcraftHIM/Piik，MIT）：没有自有窗口，操作界面在系统浏览器（本机 <code class="mono">http://127.0.0.1:&lt;port&gt;/</code>，默认 8787、被占自动上移）。Hanxi 统一接管版本（官方 GitHub Releases 裸 zip，官方 sha256 唯一信任根，或导入本地既有安装）、JobObject 管控启停与子进程树回收，并把本机界面地址与 LAN/公网邀请链接分发到这里——开播/邀请/观众入会全在 piik 页面内完成，Hanxi 不代理。</p>
          <p class="hint-dim">要点：piik 监听 <b>0.0.0.0</b>，起服务即在局域网敞开分享端口（启动须您明示，LAN 链接含本机内网 IP、原样展示）；公网链接由页面内拉起 cloudflared 经 <b>Cloudflare 临时隧道</b>中转，Hanxi 不代管、只负责把它关进 JobObject；已设访问口令时本页只显示徽标、<b>永不展示口令值</b>；配置与日志留存 Hanxi 数据根，卸载任何版本不删数据。</p>
        </div>
      </details>
    </template>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 邀请面板：card/row 皮用全局原子 extras-card/extras-row，此处仅补链接行私有形
   （extras-row 缺省 space-between 为两列设置行形，链接行要左排布故覆写排布） */
.share-head { display: flex; align-items: center; gap: 8px; }
.share-title { font-size: var(--text-base); font-weight: 600; }
.pwd-chip { align-self: center; white-space: nowrap; }
.share-row { justify-content: flex-start; }
.share-label { font-size: var(--text-sm); color: var(--color-text-subtle); min-width: 6.5em; }
.share-url { font-size: var(--text-sm); word-break: break-all; user-select: text; }
</style>
