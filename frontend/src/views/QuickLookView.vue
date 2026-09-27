<script setup lang="ts">
// QuickLook 空格预览托管工作台（Wave 5 · 批 0 共享契约迁入件，波 2E 版本区回迁收口）：
// adapter（src/adapters/quicklook）承载 RPC/事件/文案；控制台壳位三段共用共享件——
// ManagedControlBar（状态头 + 启停钮 + banner/hint 投影，slim 档现状保持）、
// ManagedVersionPanel（版本 Tab 全量接管）、ManagedExtrasCard（随关 + 仓库行）；
// store（useManagedConsole）单源接管快照轮询/事件订阅/进度 map/busy 闩/版本区加载。
// zip 解压词形（「导入本地便携目录」「安装最新版」「官方 zip」「安装中」
// 「哈希校验…/解压安装…」）并非超出面板通用形——全部经 adapter.copy 词面覆写
// 逐字声明（波 2E 审计实证），视图手抄方言表退役。
// 本模块契约形状之外仅剩一处按"共享件零模块知识"纪律留视图：
//  三钮序「启动托管 · 重载配置 · 退出」——中间的重载钮经 ManagedControlBar 的
//  #primary-action 具名槽自绘，动词取 adapter.reset（扩展槽，视图侧接线，busy 共用 store）。
import { ref } from 'vue'
import { createQuickLookAdapter } from '../adapters/quicklook'
import { useManagedConsole } from '../components/managed/store'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const adapter = createQuickLookAdapter()
const store = useManagedConsole(adapter)

const { showToast } = useToast()

// 顶层主选项卡：console = 控制台，versions = 版本管理
const activeMainTab = ref<string>('console')
const MAIN_TABS = [
  { key: 'console', label: '👁️ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// ---------- 重载配置（adapter.reset 扩展槽的视图侧接线：共享件批 0 不消费该槽） ----------
async function runReload(): Promise<void> {
  const reset = adapter.reset
  if (!reset) return
  await store.runExclusive(async () => {
    try {
      const res = await reset.run()
      if (res?.message !== undefined) showToast(res.message)
    } catch (e) {
      showToast(`重载失败: ${getErrorMessage(e)}`)
    }
  })
}

</script>

<template>
  <section class="page quicklook-view">
    <PageHeader
      title="QuickLook 预览"
      subtitle="托管开源空格秒预览工具 QuickLook：官方便携 zip 解压安装、JobObject 启停、命名管道优雅退出与运行状态探测；样式设置在其托盘菜单完成。"
    >
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头与提示条走共享控制条（banner slim 档现状保持） -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store">
        <!-- 三钮序不变：主钮（声明位）→ 重载（本槽自绘）→ 退出（声明位恒居末） -->
        <template #primary-action="{ state, busy }">
          <button
            class="btn btn-secondary btn-small"
            :disabled="busy || state !== 'running'"
            :title="state === 'running' ? '请求运行中的实例重载配置（命名管道 Reload）' : '仅运行中可重载'"
            @click="runReload"
          >↻ 重载配置</button>
        </template>
      </ManagedControlBar>

      <!-- 说明卡（可折叠，文案逐字保留） -->
      <details class="info-details">
        <summary class="info-summary">什么是 QuickLook</summary>
        <div class="info-body">
          <p>开源的 Windows 版 macOS「快速查看」工具（<a class="inline-link" href="https://github.com/QL-Win/QuickLook" target="_blank" rel="noopener">QL-Win/QuickLook</a>，GPL-3.0，.NET WPF）：在资源管理器、桌面、通用文件对话框乃至 Directory Opus / Everything 等第三方文件管理器中选中文件按 <kbd>空格</kbd>，即时预览图片、文档、压缩包、代码、音视频等，无需真正打开应用。</p>
          <p class="hint-dim">「按空格」能力由其主进程内的全局低级键盘钩子实现（非注入资源管理器），故进程终止即钩子自动摘除、系统零残渣；托管安装走官方便携 zip 解压（免安装、不写注册表、不提权），启停受 JobObject 管控。设置窗口无程序化唤起入口（托盘图标左键即弹菜单），因此本控制台不设「打开窗口」按钮；退出优先经命名管道投递 Quit 优雅收尾，宽限后 JobObject 强杀兜底。默认随 Hanxi 退出，可在下方改为独立常驻。</p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（随关 + 仓库行；本模块无快捷方式/数据目录，自动缺席） -->
    <ManagedExtrasCard :adapter="adapter" />

    <!-- 版本管理 Tab：共享面板全量接管（波 2E 回迁）——zip 解压词形经 adapter.copy
         逐字声明，本地手抄方言表与 ql-ver-status 锚点族一并退役 -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store" />
    </div>
  </section>
</template>

<style scoped>
/* 控制条四件套/版本面板/联动卡/banner/hint-line 由 managed 共享件 + components.css
   全局原子接管（波 2E：方言版本表随迁移删净，本页不再触版本区 DOM）；
   本页保留：页级 flex 骨架与说明卡内联形。 */
.quicklook-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 提示与说明卡（hint-line/info-details/info-summary/info-body p 等由全局原子接管；
   .inline-link 两行 scoped 副本已删净，落回 components.css :where 全局原子） ---------- */
.info-body kbd { font-family: var(--font-mono); font-size: var(--text-xs); background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 0 4px; }
</style>
