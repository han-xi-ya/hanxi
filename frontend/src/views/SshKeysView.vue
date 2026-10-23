<script setup lang="ts">
// SSH 密钥管理模块页：三块能力三页签。
//
// 分层照 PortScanView：本视图只管页头与切换，**不持有第四份状态**——
// 台账的数据在 components/sshkeys/useKeyring，主机清单与导出对账在
// HostsPanel + useHosts，连接测试的状态与 RPC 在 SSHCheckPanel + useSSHCheck。
//
// 切页用 v-show 不用 v-if：连接测试里填过的主机、端口、上一次的结论，
// 清单里填了一半的新增草稿与刚摊开的对账状态，都不该因为切页就蒸发
// （这条是 portscan 家族已经写死的取舍）。
//
// 文案红线：页面上只写当前做得到的事，做不到的能力名不进按钮也不进文案，
// 面板改了什么就同步这里的说法，别让页头替别人吹没落地的牛
// （portscan 那句"集成 Nmap 深度服务指纹识别"的教训就写在它的包注释里）。
import { ref, watch } from 'vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import KeyringPanel from '../components/sshkeys/KeyringPanel.vue'
import HostsPanel from '../components/sshkeys/HostsPanel.vue'
import SSHCheckPanel from '../components/sshkeys/SSHCheckPanel.vue'

const SSHKEYS_TABS = [
  { key: 'keyring', label: '密钥台账' },
  { key: 'hosts', label: '主机清单' },
  { key: 'check', label: '连接测试' },
]

// 类型取 string 而非窄联合：MainTabNav 的 v-model 契约是 string。
const activeTab = ref('keyring')

const checkPanel = ref<InstanceType<typeof SSHCheckPanel> | null>(null)

// 切走「连接测试」时主动清口令：v-show 保留的是主机/端口/上次结论这些**该留**的东西，
// 口令不在其列。面板侧的 onDeactivated 只在换路由时响，页签内切换它无感知，
// 所以这一刀只能由宿主下（对应 SSHCheckPanel 的 defineExpose({ clearPass })）。
watch(activeTab, (next, prev) => {
  if (prev === 'check' && next !== 'check') checkPanel.value?.clearPass()
})
</script>

<template>
  <section class="page sshkeys-page">
    <PageHeader
      title="SSH 密钥管理"
      subtitle="本机 SSH 密钥与主机清单。清单的改动只动账本，点了「同步给 ssh」才会写进 ~/.ssh。各步后果由对应面板就地说明，这里不重复。"
    >
      <template #actions>
        <MainTabNav v-model="activeTab" :tabs="SSHKEYS_TABS" id-prefix="sshkeys" label="SSH 密钥管理页签切换" />
      </template>
    </PageHeader>

    <!-- 新增操作按钮必须挂 .btn-group（全局档含 flex-wrap）：窄窗下整颗换行而不是
         被挤出窗口，这是机主实测踩过的坑（"刷新网卡"那一条）。 -->
    <div v-show="activeTab === 'keyring'" id="sshkeys-keyring-panel" class="tab-panel"
         role="tabpanel" aria-labelledby="sshkeys-keyring-tab">
      <KeyringPanel />
    </div>

    <div v-show="activeTab === 'hosts'" id="sshkeys-hosts-panel" class="tab-panel"
         role="tabpanel" aria-labelledby="sshkeys-hosts-tab">
      <HostsPanel />
    </div>

    <div v-show="activeTab === 'check'" id="sshkeys-check-panel" class="tab-panel"
         role="tabpanel" aria-labelledby="sshkeys-check-tab">
      <SSHCheckPanel ref="checkPanel" />
    </div>
  </section>
</template>

<style scoped>
/* 页头/卡片/按钮/表格家族由 PageHeader 与全局原子接管，本层只留页面骨架
   （与 .portscan-page 同形制，不新造尺寸）。 */
.sshkeys-page { display: flex; flex-direction: column; gap: 14px; height: 100%; }
</style>
