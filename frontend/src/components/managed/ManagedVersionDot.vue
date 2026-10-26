<!--
  版本行状态点徽标标准形：托管家族 .ver-status 7px 圆点的单一来源件（源出
  ManagedVersionPanel 原 scoped 标准形块，该副本已出清、面板直挂本件）。
  消费面：Panel/BCU/Douzy/FrpcVersionsTab/EverythingReleaseTable 等远程行；
  RustDesk/SubnetDesk 仍持 rd-/sd- 前缀本地复制体（类名带前缀系刻意隔离，
  防样式碰撞——markeron 垂直字体事故教训），SysInfoView 的两态最小实现属
  合法分叉——二者均不在本件收编面内。
  状态语义 = 各视图 statusOf 四档（ManagedRowState，与 Panel 现用法一致）；
  词面由消费方逐字传入——「已装/可安装」「安装中/下载中/装机中」等词面各模块有差，
  组件不做推断。
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
/* 形制标准源自 ManagedVersionPanel 原 scoped 块（副本已出清，本件即唯一来源；
   全局原子 .ver-status 不存在，scoped 属性隔离无碰撞；hx-pulse 为 components.css 全局 keyframe） */
.ver-status { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); white-space: nowrap; }
.ver-status::before { content: ''; width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
.ver-status.installed::before { background: var(--state-positive); }
.ver-status.downloading::before { background: var(--state-information); animation: hx-pulse 1.8s ease-in-out infinite; }
.ver-status.error::before { background: var(--state-danger); }
.ver-status.idle::before { background: var(--color-text-subtle); }
</style>
