<script setup lang="ts">
// Bili23 下载控制台（Wave 5 · 批 0 契约迁万件，模式照抄 CCSwitchView）：
// 共享面全部收敛进 components/managed 托管控制台家族——adapter（src/adapters/bili23）
// 承载业务投影（RPC/事件归一/文案/退出三态如实回执），ManagedConsoleShell 管
// 页头与页签骨架，状态头/启停钮/版本区/联动辅助卡全由共享件按声明渲染。
// 退出语义差异（三态回执 stopped/hidden/asked+message）经 control.quit 的
// ManagedActionResult.message 原样上墙，禁止吞态；本视图仅余两件私有物：
// 业务说明卡（默认槽）与「强制结束」危险动作（#danger-extra 契约位）。
// 增强批⑦：危险钮现态/在途闩全部回归共享件——钮禁用与 title 用槽作用域
// {state, busy}（busy 已含 adapter.dangerBusy 联动），执行后经 {store} 回读
// 现态；adapter 内的快照镜像与并行 busy 源自此退役。running+hidden 琥珀灯
// 经 adapter.statusTone（⑧）、联动卡「托管位置」行经 copy.dataDirRow（⑨）。
import { createBili23Adapter } from '../adapters/bili23'
import type { ManagedConsoleStore } from '../components/managed/store'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'

const adapter = createBili23Adapter()

/** 强杀点击：整段交共享单飞闩，adapter 内确认/回执，最后在同一 busy 窗口内回读现态。 */
async function runDanger(store: ManagedConsoleStore): Promise<void> {
  await store.runExclusive(async () => {
    try {
      await adapter.danger.run()
    } finally {
      // 强杀 RPC 报错也可能已产生副作用，成功/失败均立即回读真实状态。
      await store.refresh()
    }
  })
}
</script>

<template>
  <ManagedConsoleShell
    class="bili23-view"
    :adapter="adapter"
    title="Bili23 下载"
    subtitle="托管开源 B 站视频下载器 Bili23 Downloader：版本管理、启停与窗口唤起。"
    console-tab-label="📺 控制台"
  >
    <!-- 控制台 Tab 主体：说明卡（可折叠，文案逐字保留） -->
    <details class="info-details">
      <summary class="info-summary">什么是 Bili23 Downloader</summary>
      <div class="info-body">
        <p>开源跨平台 B 站视频下载器（<a class="inline-link" href="https://github.com/ScottSloan/Bili23-Downloader" target="_blank" rel="noopener">ScottSloan/Bili23-Downloader</a>，GPL-3.0），支持投稿/番剧/课程/收藏夹批量解析、多线程下载、弹幕字幕、NFO 元数据与自定义命名规则。版本下载自官方 GitHub Releases（sha256 四层校验），启停受 JobObject 管控。</p>
        <p class="hint-dim">便携包为"自带 Python 运行时 + 程序"整目录（展开约 108MB），无需任何本机依赖。退出语义说明：本应用的「退出」按你在 Bili23 设置中的"关闭窗口"行为执行（询问/最小化到托盘/直接退出），结果会如实提示；需要立即终结时用「强制结束」。</p>
      </div>
    </details>

    <!-- 危险动作位（契约：联动卡之后、恒居控制台尾部）：强制结束——
         ForceStop 不在共享七动词内，确认/回执收在 adapter.danger 内；
         现态与在途闩（含 dangerBusy 联动）经 ⑦ 作用域槽直给 -->
    <template #danger-extra="{ state, busy, store }">
      <div class="control-bar b23-danger-extra">
        <div class="control-top">
          <span class="hint-line">危险操作：「退出」按 Bili23 自身「关闭窗口」设置执行，可能被收入托盘或弹窗询问拦截；需要立即终结时用「强制结束」。</span>
          <div class="control-btns">
            <button
              class="btn btn-danger-outline btn-small"
              :disabled="busy || adapter.danger.disabledFor(state)"
              :title="adapter.danger.titleFor(state)"
              @click="runDanger(store)"
            >{{ adapter.danger.label }}</button>
          </div>
        </div>
      </div>
    </template>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/版本区/联动卡/页签与 flex 骨架全部由 managed 组件 + components.css
   全局原子接管；本页仅余说明卡内联链接与危险位说明行两条私有形 */
.inline-link { color: var(--color-primary); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }
.b23-danger-extra .hint-line { flex: 1; min-width: 220px; }
</style>
