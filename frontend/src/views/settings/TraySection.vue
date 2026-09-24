<script setup lang="ts">
// 设置分区·托盘右键菜单：编辑面已抽成共享组件 TrayItemsEditor（N5-C1），
// 与快捷菜单轮盘页（QuickMenuView）同挂同一份 settings.TrayMenu 编辑面——
// 勾选添加 / 外部程序 / 分组子条目 / 脏态保存 / 失败回滚等语义都在组件内逐字保留，
// 本宿主只保留页面骨架与页头文案（读写链路与脏标记冲刷逻辑无改动）。
import PageHeader from '../../components/ui/PageHeader.vue'
import TrayItemsEditor from '../../components/tray/TrayItemsEditor.vue'
</script>

<template>
  <section class="page">
    <PageHeader title="托盘右键菜单" subtitle="自定义右键托盘图标时的快捷入口：启动托管工具、直达模块页面或拉起外部程序，保存后立即生效；快捷菜单轮盘共用本配置，分组条目在托盘中呈现子菜单。" />

    <div class="card">
      <TrayItemsEditor />
    </div>
  </section>
</template>
