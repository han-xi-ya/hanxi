<script setup lang="ts">
// 果核看图控制台（Wave 5 · 批 1 收敛件，波 2E 版本区回迁收口）：共享面全部进
// 托管控制台家族——adapter（src/adapters/guoheview）承载业务投影（RPC/事件/文案），
// useManagedConsole 单源状态轮询/uptime/进度 map/busy 闩，ManagedControlBar 管
// 状态头与启停钮（多实例 OpenWindow 聚焦/唤回/另开三分支在 adapter.control.primary
// run 内部消化，UI 恒为一钮），ManagedVersionPanel 管版本区，ManagedExtrasCard
// 管随关与官网联动卡。
// 官方发布接口方言的承接位（波 2E 审计实证，词面全部逐字保留）：stable/beta
// 「通道列」走面板 #release-extra-col(+head) 槽；「↻ 刷新发布接口」钮词走面板
// refreshLabel 位；「官方便携」「安装中」「MD5 校验…/解压安装…」等在 adapter.copy
// 声明——整表重画自此退役，发布时间/上游发布两列随六列标准形收敛（空值如实「—」）。
import { ref } from 'vue'
import { createGuoheViewAdapter } from '../adapters/guoheview'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'

const adapter = createGuoheViewAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/piclite 同构）
const activeMainTab = ref('console')
const MAIN_TABS = [
  { key: 'console', label: '🏞️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]
</script>

<template>
  <section class="page guoheview-view">
    <PageHeader title="果核看图" subtitle="托管极速 RAW 看图器 GuoheView：官方发布接口便携版安装（MD5 校验）、JobObject 启停与窗口唤起；浏览操作在 GuoheView 自有窗口完成（多实例：双击图片的窗口不受影响）。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/提示条/引导行由 ManagedControlBar 按 adapter 投影渲染 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" :banner-slim="false" />

      <!-- 说明卡（可折叠） -->
      <details class="info-details">
        <summary class="summary-text">什么是果核看图</summary>
        <div class="info-body">
          <p>果核（ghxi.com）出品的 Windows 极速 RAW 图片查看器（闭源免费软件，Certum 数字签名）：首帧 0.1 秒、自研解码内核覆盖 ARW/CR3/NEF/DNG/HEIC/WebP/TIFF 等格式，分块加载大图、ICC 色彩管理，纯净无广告。</p>
          <p class="hint-dim">上游闭源、发布挂官方自建接口（非 GitHub）：每次仅提供当前版本（stable/beta），无历史归档，旧版本可用「导入本地」收纳；完整性以官方 MD5 + 字节数 + zip CRC + 布局自检四层兜底。应用自带更新检查，升级建议一律回这里走托管安装。</p>
        </div>
      </details>
    </div>

    <!-- 联动与官网设置卡（随关 + 官网行，条目文案在 adapter） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab：共享面板接管（波 2E 回迁）——「通道」方言列经
         #release-extra-col(+head) 槽承接（th 词面逐字），刷新钮词走 refreshLabel 位，
         其余词面在 adapter.copy；gv-ver-status 锚点族随手抄表退役 -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store" refresh-label="↻ 刷新发布接口">
        <template #release-extra-col-head>通道</template>
        <template #release-extra-col="{ release }">
          <UiStatusChip v-if="release.isPre" tone="warning">beta</UiStatusChip>
          <UiStatusChip v-else tone="neutral">stable</UiStatusChip>
        </template>
      </ManagedVersionPanel>
    </div>
  </section>
</template>

<style scoped>
/* 页头/控制条/提示条/版本面板/联动卡由 managed 组件 + components.css 全局原子接管
   （原 .gv-status-light 复制体已由 .status-light 标准形替代）；
   波 2E 后本页仅余控制台 Tab 私有形（折叠说明卡/胶囊补差）。 */
.guoheview-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* 补差 against 全局原子 .ver-pill：版本胶囊数字等宽（渲染在 ManagedControlBar
   子件内部，父级 scoped 穿不过去，:deep 维持逐字同形） */
.guoheview-view :deep(.ver-pill) { font-variant-numeric: tabular-nums; }

/* ---------- 提示与说明卡（hint-line 原 line-height:1.6 散差按标准形定档删除；
   info-details/info-body p 由全局原子接管；本视图折叠标题为自名 .summary-text，非原子选择器，留局部） ---------- */
.summary-text { padding: 7px 12px; font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted); cursor: pointer; list-style: none; display: flex; align-items: center; user-select: none; }
.info-details summary::-webkit-details-marker { display: none; }
.summary-text::after { content: '▸'; font-size: var(--text-micro); margin-left: auto; transition: transform var(--motion-base); }
.info-details[open] .summary-text { border-bottom: 1px solid var(--color-border); }
.info-details[open] .summary-text::after { transform: rotate(90deg); }

/* 版本区（含通道列槽）全部渲染在 ManagedVersionPanel 子件内（波 2E 回迁：
   手抄表与其 control-panel、inst- 系列及表格补差随迁移删净，本视图 scoped 不再
   触版本区 DOM）；联动与官网设置卡家族由全局原子接管 */
</style>
