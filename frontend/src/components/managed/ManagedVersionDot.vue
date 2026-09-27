<!--
  版本行状态点徽标标准形（前端冗余治理 · 波 0，只增不删）：托管家族 .ver-status
  7px 圆点复制体的单一来源件。样式逐字抄 components/managed/ManagedVersionPanel.vue
  scoped 块（334-339 行，各视图同源方言的真模板）；同源复制体——
  views/{BCU,QuickLook,Rufus,GuoheView}View.vue 的 .{bcu,ql,rf,gv}-ver-status、
  DouzyView.vue 的 .dz-status、{RustDesk,SubnetDesk}View.vue 的 .{rd,sd}-ver-status、
  everything/EverythingReleaseTable.vue 的 .ver-status——由后续波次替换，本波不动消费点。
  状态语义 = 各视图 statusOf 四档（ManagedRowState，与 Panel 现用法一致）；
  词面由消费方逐字传入——「已装/可安装」「安装中/下载中/装机中」等词面各模块有差，
  组件不做推断（SysInfoView 的两态最小实现属合法分叉，不在本件收编面内）。
-->
<script setup lang="ts">
import type { ManagedRowState } from './managedProgress'

defineProps<{
  status: ManagedRowState
  text: string
}>()
</script>

<template>
  <span :class="['ver-status', status]">{{ text }}</span>
</template>

<style scoped>
/* 逐字抄自 ManagedVersionPanel.vue 的 scoped 块（全局原子 .ver-status 不存在，
   scoped 属性隔离无碰撞；hx-pulse 为 components.css 全局 keyframe） */
.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: var(--state-positive); }
.ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1s infinite; }
.ver-status.error::before { background: var(--state-danger); }
.ver-status.idle::before { background: var(--color-text-subtle); }
</style>
