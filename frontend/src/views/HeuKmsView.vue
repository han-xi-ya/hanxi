<script setup lang="ts">
// HEU KMS Activator 托管工作台：共享面全部收敛进 components/managed 托管控制台
// 家族——adapter（src/adapters/heukms）承载业务投影（RPC/事件/文案），
// ManagedConsoleShell 管页头与页签骨架。
//
// 本模块的两处特化：
//  1. 远程表**追加**一行「↗ 发布页」动作位（后端 OpenReleasePage 走系统浏览器）
//     ——经壳的 #release-actions 槽转发位直落共享面板（波 2G 前需 #versions-body
//     整替照抄默认体，自此退役）。主安装路径是共享面板自带的「下载并安装」
//     （rar 通道），发布页是拿免密 iso、看该版说明、或取"上游未发布摘要而
//     不可自动安装"旧版的备援入口。
//  2. 740 提权直拒后的一键提权重启入口走 #console-extra 槽（与 litemonitor
//     同判据：后端 elevateHint 文案统一含"管理员"关键词）。
import { createHeuKmsAdapter, openHeuReleasePage } from '../adapters/heukms'
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import ElevateRestart from '../components/ElevateRestart.vue'
import { needsElevationRestart } from '../components/managed/adapter'
import { getErrorMessage } from '../utils/errors'

const adapter = createHeuKmsAdapter()

// 740 提权直拒判据走共享件 needsElevationRestart（单源，区外 bcu/rufus 同款）→
// 追加一键提权重启入口。

// 逐版打开上游发布页（备援入口）：浏览器拉不起来属边缘场景，失败只记控制台、
// 不打一条 toast 挡用户的路（与"下载并安装"的失败回执区分开，后者由共享件统一弹）。
async function openPage(version: string): Promise<void> {
  try {
    await openHeuReleasePage(version)
  } catch (e) {
    console.warn('打开 HEU 发布页失败:', getErrorMessage(e))
  }
}
</script>

<template>
  <ManagedConsoleShell
    class="heukms-view"
    :adapter="adapter"
    title="HEU KMS Activator"
    subtitle="托管 HEU KMS Activator：版本台账、导入本地主程序与 JobObject 启停。"
    console-tab-label="⚡ 控制台"
  >
    <!-- 控制台内联大件：提权直拒后的一键提权重启入口 -->
    <template #console-extra="{ snap }">
      <ElevateRestart v-if="needsElevationRestart(snap)" route="/ext/heukms" />
    </template>

    <!-- 控制台 Tab 主体：说明卡（折叠） -->
    <details class="info-details">
      <summary class="info-summary">关于 HEU KMS Activator 与本页的边界</summary>
      <div class="info-body">
        <p>
          HEU KMS Activator 是 Windows / Office 的 KMS·数字权利激活工具，上游
          <a class="inline-link" href="https://github.com/zbezj/HEU_KMS_Activator" target="_blank" rel="noopener">zbezj/HEU_KMS_Activator</a>。
          本页只做三件事：版本台账、把主程序收纳进托管目录、受 Job Object 管控的启停——所有激活操作都在它自己的窗口里完成，Hanxi 不代为点击、不改写它的任何设置。
        </p>
        <p class="hint-dim">
          <strong>安装是自动的</strong>：点版本行「下载并安装」，Hanxi 取上游 GitHub Releases 的
          <code>.rar</code>（官方摘要整包校验 → 纯 Go RAR5 解包 → 解出后核 PE 身份与版本号 → 原子落位）。同版双发的
          <code>.iso</code> 是 UDF 光盘镜像、Hanxi 不解析，需要它就点「↗ 发布页」自取；62.0.0 及更早版本上游没发布官方摘要，不进可安装列表，那种情况用「导入本地」收纳你手上的 exe。
        </p>
        <p class="hint-dim">
          <strong>两条须知</strong>：主程序 manifest 强制管理员权限——Hanxi 未提权时启动会直接失败，请用页面出现的提权重启入口；主程序无代码签名，Windows Defender 常会删除或隔离它，"突然变成未安装"多半是这个原因，请先在安全中心处理排除项。
        </p>
        <p class="hint-dim">
          上游后期已闭源（仓库仅剩说明手册与发布资产），Hanxi 仅代为收纳官方发布，不镜像分发其代码与二进制。
        </p>
      </div>
    </details>

    <!-- 远程行「取包」动作位经壳转发的 #release-actions 直落共享面板（无 channel，
         壳默认版本体即纯 ManagedVersionPanel，无需整替抄骨） -->
    <template #release-actions="{ release }">
      <button
        class="btn btn-secondary btn-small"
        title="在浏览器打开该版发布页：看更新说明、取免密 iso，或取未发布官方摘要而无法自动安装的旧版"
        @click="openPage(release.version)"
      >
        ↗ 发布页
      </button>
    </template>
  </ManagedConsoleShell>
</template>
