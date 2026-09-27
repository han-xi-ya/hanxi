<script setup lang="ts">
// Rufus 控制台（Wave 5 · 批 1 收敛件，模式照抄 CCSwitchView）：共享面全部进托管
// 控制台家族——adapter（src/adapters/rufus）承载业务投影（RPC/事件/文案/确认输入），
// useManagedConsole 单源状态轮询/uptime/进度 map/busy 闩，ManagedControlBar 管
// 状态头与启停钮，ManagedExtrasCard 管随关与仓库联动卡。Rufus 是一次性工具
// （写完盘即关窗）：不声明桌面快捷方式等自启类条目，缺项由联动卡自动缺席。
// 波 2E：版本 Tab 全量回迁 ManagedVersionPanel——单文件版阶段词表含 install 不含
// extract（「校验落位…」非面板缺省「校验解压安装…」），词面差异经 adapter.copy
// .stageWord 覆写位逐字声明，视图手抄表退役；extras 卡的「打开位置」钮仍经
// #extras-action 具名位注入（title/disabled 依赖已装清单动态解析，静态 dataDir
// 条目表达不下）。
import { computed, ref } from 'vue'
import { createRufusAdapter } from '../adapters/rufus'
import { useManagedConsole } from '../components/managed/store'
import ManagedControlBar from '../components/managed/ManagedControlBar.vue'
import ManagedExtrasCard from '../components/managed/ManagedExtrasCard.vue'
import ManagedVersionPanel from '../components/managed/ManagedVersionPanel.vue'
import PageHeader from '../components/ui/PageHeader.vue'
import MainTabNav from '../components/ui/MainTabNav.vue'
import ElevateRestart from '../components/ElevateRestart.vue'

const adapter = createRufusAdapter()
const store = useManagedConsole(adapter)

// 顶层主选项卡：console = 控制台，versions = 版本管理（与 ccswitch/litemonitor 同构）
const activeMainTab = ref<string>('console')
const MAIN_TABS = [
  { key: 'console', label: '⚡ 控制台' },
  { key: 'versions', label: '📦 版本管理' },
]

// 打开安装目录目标：优先当前运行版本，其次 active 版本，最后任一已装
const openDirVersion = computed(() => {
  const prefer = store.state === 'running' && store.runningVersion ? store.runningVersion : store.activeVersion
  return store.installed.find((v) => v.version === prefer) ?? store.installed[0] ?? null
})

// 740 提权直拒（后端 elevateHint 文案统一含"管理员"）→ 追加一键提权重启入口
const needsElevate = computed(() => store.state === 'failed' && (store.snap?.error || '').includes('管理员'))
</script>

<template>
  <section class="page rufus-view">
    <PageHeader title="Rufus" subtitle="托管 USB 启动盘制作工具 Rufus：版本管理、JobObject 启停与窗口唤起。">
      <template #actions>
        <MainTabNav v-model="activeMainTab" :tabs="MAIN_TABS" />
      </template>
    </PageHeader>

    <div v-if="store.listError" class="error-box">{{ store.listError }}</div>

    <!-- 控制台 Tab：状态头/提示条/引导行由 ManagedControlBar 按 adapter 投影渲染 -->
    <div v-show="activeMainTab === 'console'" class="tab-body">
      <ManagedControlBar :adapter="adapter" :store="store" />
      <ElevateRestart v-if="needsElevate" route="/ext/rufus" />

      <!-- 说明卡（折叠） -->
      <details class="info-details">
        <summary class="info-summary">关于 Rufus</summary>
        <div class="info-body">
        <p>
          Rufus 是老牌 USB 启动盘制作工具（格式化 U 盘、写入 Windows/Linux/PE 镜像，支持大量奇技淫巧），
          上游 <a class="inline-link" href="https://github.com/pbatard/rufus" target="_blank" rel="noopener">pbatard/rufus</a>（C/Win32 原生，GPL-3.0）。
          本模块仅托管其运行：版本下载自官方 GitHub Releases（官方 digest sha256 + 字节数 + MZ 魔数三重校验），启停受 JobObject 管控。
        </p>
        <p class="hint-dim">
          便携版与安装版是同一个二进制（上游实证同哈希），Hanxi 收纳为单 rufus.exe 隔离目录；首次托管启动时自动预置 rufus.ini
          ——全部设置保存在版本目录内（零注册表污染），并顺带关闭其内置更新检查（版本升级走本页「版本管理」）。
          卸载版本会把该目录连同 rufus.ini 设置一并删除。
        </p>
        <p class="hint-dim">
          磁盘级写入不可逆：Rufus 界面内的所有确认对话框（分区方案、覆盖警告）请逐条阅读；写入过程中不要点「退出」或关闭 Hanxi。
        </p>
        </div>
      </details>
    </div>

    <!-- 联动与辅助设置卡（控制台与版本 Tab 均可见；随关与仓库行在 adapter，
         「打开位置」为清单驱动的动态钮，经具名位注入） -->
    <ManagedExtrasCard :adapter="adapter">
      <template #extras-action>
        <div class="extras-btns">
          <button
            class="btn btn-secondary btn-small"
            :disabled="!openDirVersion"
            :title="openDirVersion ? `打开 ${openDirVersion.version} 的便携目录（rufus.exe 与 rufus.ini 所在处）` : '尚未安装任何版本'"
            @click="openDirVersion && store.runOpenDir(openDirVersion)"
          >📂 打开位置</button>
        </div>
      </template>
    </ManagedExtrasCard>

    <!-- 版本管理 Tab：共享面板全量接管（波 2E 回迁）——「校验落位…」等单文件版
         词面经 adapter.copy 逐字声明，rf-ver-status 锚点族随手抄表一并退役 -->
    <div v-show="activeMainTab === 'versions'" class="tab-body">
      <ManagedVersionPanel :adapter="adapter" :store="store" />
    </div>
  </section>
</template>

<style scoped>
/* 页头/状态头/提示条/联动卡由 managed 组件 + components.css 全局原子接管
   （原 .rf-status-light 复制体已由 .status-light 标准形替代），此处只保留本视图业务样式。 */
.rufus-view { display: flex; flex-direction: column; gap: 10px; }
.tab-body { display: flex; flex-direction: column; gap: 10px; }

/* ---------- 提示与说明卡（.banner.slim 由全局原子接管；hint-line 标准形定档；
   info-details/info-summary/info-body p 等由全局原子接管；
   .inline-link 两行 scoped 副本已删净，落回 components.css :where 全局原子） ---------- */

/* 版本区全部渲染在 ManagedVersionPanel 子件内（波 2E 回迁：手抄表与其样式补差
   一并删净，本页不再触版本区 DOM） */

/* ---------- 联动与辅助设置卡（extras-card/extras-row/toggle-label/repo-row(.k)/repo-addr 由全局原子接管） ---------- */
.extras-btns { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
