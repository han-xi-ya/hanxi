<script setup lang="ts">
// 设置分区·托盘右键菜单：编辑面已抽成共享组件 TrayItemsEditor（N5-C1），轮盘独立批
// 后本宿主按默认 scope='tray' 挂托盘专属账（settings.TrayMenu），与快捷菜单轮盘页各记
// 各账、互不干扰——勾选添加 / 外部程序 / 分组子条目 / 实时自动保存 / 失败回滚等语义
// 全在组件内，本宿主只保留页面骨架与页头文案。
import PageHeader from '../../components/ui/PageHeader.vue'
import TrayItemsEditor from '../../components/tray/TrayItemsEditor.vue'
</script>

<template>
  <section class="page">
    <PageHeader title="托盘右键菜单" subtitle="自定义右键托盘图标时的快捷入口：启动托管工具、直达模块页面或拉起外部程序，改动自动保存并即时生效（不完整分组暂缓落盘直到补全或移除）；本配置为托盘专属，与快捷菜单轮盘相互独立、互不影响，分组条目在托盘中呈现子菜单。" />

    <div class="card">
      <TrayItemsEditor />
    </div>
  </section>
</template>
