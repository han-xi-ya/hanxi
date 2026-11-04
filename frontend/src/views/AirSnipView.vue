<script setup lang="ts">
// AirSnip 托管工作台（托管控制台标准形）：共享面全部收敛进 components/managed
// 托管控制台家族——adapter（src/adapters/airsnip）承载业务投影（RPC/事件/文案），
// ManagedConsoleShell 管页头与页签骨架。
// 本模块特化：AirSnip 托盘常驻、无主窗口，且上游"第二实例唤起已有实例"是它自己
// 未实现的死代码——控制面因此走上游内部托盘菜单命令 ID（PostMessage WM_COMMAND），
// 除声明式 primary（打开设置面板）/quit 外，另在 #primary-action 槽自绘
// 「截图」「贴图」两个模块自有动作钮（钮序：设置面板 → 截图 → 贴图 → 退出）；
// 740 提权直拒后的一键提权重启入口走 #console-extra 槽。
import { createAirsnipAdapter, type AirsnipActionSpec } from '../adapters/airsnip'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import ElevateRestart from '../components/ElevateRestart.vue'
import type { ManagedConsoleStore } from '../components/managed/store'
import { needsElevationRestart } from '../components/managed/adapter'

const adapter = createAirsnipAdapter()

// 动作钮统一走共享槽动词编排（runExclusive→settle→toast）：成败两分支都刷快照
// ——冷启动失败（740）时要把 failed 态与提权重启钮如实刷出来，不能只 toast。
function runAction(store: ManagedConsoleStore, action: AirsnipActionSpec) {
  void store.runSlotVerb({ run: () => action.run() }, { refreshAfter: 'always' })
}
</script>

<template>
  <ManagedConsoleShell
    class="airsnip-view"
    :adapter="adapter"
    title="AirSnip 轻截"
    subtitle="托管 1 MB 原生截图标注工具 AirSnip：版本管理、JobObject 启停与截图/贴图/设置命令通道。"
    console-tab-label="⚡ 控制台"
  >
    <!-- 模块自有动作钮：截图 / 从剪贴板贴图（后端在未运行态先冷启动再投递） -->
    <template #primary-action="{ busy, state, store }">
      <button
        v-for="action in adapter.actions"
        :key="action.label"
        class="btn btn-small"
        :class="action.cssClass"
        :disabled="busy || state === 'starting'"
        :title="action.titleFor(state)"
        @click="runAction(store, action)"
      >{{ action.label }}</button>
    </template>

    <!-- 控制台内联件：740 提权直拒后的一键提权重启入口 -->
    <template #console-extra="{ snap }">
      <ElevateRestart v-if="needsElevationRestart(snap)" route="/ext/airsnip" />
    </template>

    <!-- 控制台 Tab 主体：说明卡（折叠） -->
    <details class="info-details">
      <summary class="info-summary">关于 AirSnip</summary>
      <div class="info-body">
        <p>
          AirSnip（轻截）是约 1 MB 的 Windows 原生截图标注工具：热键冻结全屏 → 框选 →
          矩形 / 直线与曲线箭头 / 文本 / 荧光笔 / 高斯模糊 / 聚光灯标注 → 复制或另存
          PNG·JPEG·WebP，另含长截图（手动滚动拼接）与贴图（Pin）。界面与渲染全部由 Rust
          直接绘制（tiny-skia + DirectWrite，抓屏走 GDI），不依赖 GPU、不套 Electron、不装运行时，
          待机 0% CPU、常驻 1 MB 量级——核显迷你机、虚拟机、远程桌面里同样流畅。
          上游 <a class="inline-link" href="https://github.com/bd-dxg/airsnip" target="_blank" rel="noopener">bd-dxg/airsnip</a>（GPL-3.0-only）。
        </p>
        <p class="hint-dim">
          本模块只做托管：版本下载走官方 GitHub Releases（官方 SHA-256 摘要四层校验），启停受 JobObject
          管控；截图覆盖层、标注工具、长截图、贴图窗与全部设置（快捷键改键、主题、开机自启）都在 AirSnip
          自己的界面里完成，Hanxi 不代改也不复制这套配置。
        </p>
        <p class="hint-dim">
          三点上游事实需要知道：① <strong>release 版要求管理员权限</strong>（否则无法覆盖管理员窗口），
          Hanxi 未提权时启动会被系统直接拒绝，页面会给出「以管理员身份重启 Hanxi」一键入口；
          ② <strong>exe 未做代码签名</strong>，首次运行 SmartScreen 可能提示「已保护你的电脑」，
          点「更多信息 → 仍要运行」即可；③ 上游自带自动更新，Hanxi 每次托管启动前会关闭它
          （只改 <span class="mono">%APPDATA%\airsnip\config.toml</span> 的
          <span class="mono">auto_check_update</span> 一个键），版本升级统一走本页「版本管理」——
          该文件是上游的全局配置，此改写同样作用于你自己另外安装的 AirSnip。
        </p>
        <p class="hint-dim">
          注意：本页的「截图 / 贴图 / 打开设置面板」通过 AirSnip 内部托盘菜单命令投递，属上游未公开的
          内部约定，上游改版可能失效；且 Hanxi 与实例权限等级不同时，Windows 的 UIPI 会静默丢弃这类消息
          （表现为"点了没反应"）——同样以管理员身份运行 Hanxi 即可。日常最快的路径仍是直接按
          <span class="mono">F1</span> 截图、<span class="mono">F3</span> 贴图。
        </p>
        <p class="hint-dim">
          与已托管的 <strong>Snipaste</strong> 同属截图标注工具：两者<strong>默认热键都是 F1</strong>，
          同时运行会互相抢占按键，请二选一或在上游设置面板改键。AirSnip 的优势是不吃 GPU、
          体积极小，适合核显迷你主机 / 虚拟机 / 远程桌面。
        </p>
      </div>
    </details>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 模块自有类名前缀 as-：App.vue 全局原子（.status-dot 等）不与本模块自绘件冲突。
   当前自绘钮全部复用全局 .btn 体系，无额外样式；保留本块作为后续特化落点。 */
</style>
