<script setup lang="ts">
// 设置分区·系统直达：hosts / 网络 / 环境变量 / 控制面板等 Windows 组件一键调起，
// 外加统一通知通道诊断（前台卡片 + 后台原生气泡）。
// 拆分自原 SettingsView 单页：系统管理工具统一走后端白名单 OpenSystemTool，
// 注册表/计算机管理会触发系统 UAC 确认（行为不变）。
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'
import type { IconName } from '../../constants/icons'

const { showToast } = useToast()

interface SystemTool {
  id: string
  name: string
  badge: string
  desc: string
  icon: IconName
  actionLabel: string
  primary?: boolean
}

// 调起入口清单：hosts/网络/环境变量走专用 API，其余走 OpenSystemTool 白名单
const tools: SystemTool[] = [
  { id: 'hosts', name: '系统 hosts 文件', badge: '域名映射', desc: 'C:\\Windows\\System32\\drivers\\etc\\hosts', icon: 'pen-line', actionLabel: '记事本编辑', primary: true },
  { id: 'network', name: '网络适配器管理', badge: 'ncpa.cpl', desc: '控制面板 · 网络连接列表', icon: 'network', actionLabel: '打开管理' },
  { id: 'env', name: '系统环境变量', badge: 'sysdm.cpl', desc: 'Path 与用户/系统变量配置', icon: 'sliders', actionLabel: '打开设置' },
  { id: 'control', name: '控制面板', badge: 'control.exe', desc: 'Windows 经典控制面板主页', icon: 'layout', actionLabel: '打开面板' },
  { id: 'regedit', name: '注册表编辑器', badge: 'regedit · 需 UAC', desc: '改动前建议先导出目标项备份', icon: 'box', actionLabel: '打开注册表' },
  { id: 'firewall', name: 'Windows 防火墙', badge: 'firewall.cpl', desc: '防火墙放行规则与通知配置', icon: 'shield', actionLabel: '打开防火墙' },
  { id: 'compmgmt', name: '计算机管理', badge: 'compmgmt · 需 UAC', desc: '任务计划、服务、设备管理器与磁盘管理', icon: 'monitor', actionLabel: '打开管理' },
]

const toolNames: Record<string, string> = Object.fromEntries(tools.map((t) => [t.id, t.name]))

async function runTool(id: string) {
  const name = toolNames[id] ?? id
  try {
    if (id === 'hosts') {
      await AppAPI.AppService.OpenHostsFile()
      showToast('已调起编辑器打开 hosts 文件')
      return
    }
    if (id === 'network') {
      await AppAPI.AppService.OpenNetworkConnections()
      showToast('已打开网络连接面板')
      return
    }
    if (id === 'env') {
      await AppAPI.AppService.OpenSystemEnvSettings()
      showToast('已打开环境变量设置')
      return
    }
    await AppAPI.AppService.OpenSystemTool(id)
    showToast(`已调起${name}`)
  } catch (e: unknown) {
    showToast(`打开${name}失败: ${getErrorMessage(e)}`)
  }
}

async function triggerTestNotification() {
  try {
    // 走完整统一通知管道：后端 notify → notify:received 事件 → 顶层卡片
    await AppAPI.AppService.SendTestNotification()
  } catch (e: unknown) {
    showToast(`后端通知分发异常: ${getErrorMessage(e)}`)
  }
}

async function triggerDelayedTestNotification() {
  try {
    await AppAPI.AppService.SendDelayedTestNotification(4)
    showToast('已设定 4 秒倒计时！请立即将主窗口最小化或点击右上角关闭(后台)，稍后将弹出 Windows 原生气泡通知')
  } catch (e: unknown) {
    showToast(`触发延迟测试通知失败: ${getErrorMessage(e)}`)
  }
}
</script>

<template>
  <section class="page">
    <PageHeader title="系统直达" subtitle="一键调起 Windows 常用配置文件与管理组件，附统一通知通道自检。" />

    <div class="card tool-list">
      <div v-for="t in tools" :key="t.id" class="setting-row">
        <span class="tool-icon" aria-hidden="true"><AppIcon :name="t.icon" :size="16" /></span>
        <span class="setting-main tool-main">
          <span class="setting-name">{{ t.name }} <span class="chip chip-neutral tool-badge">{{ t.badge }}</span></span>
          <code class="setting-desc tool-desc">{{ t.desc }}</code>
        </span>
        <button class="btn btn-small" :class="t.primary ? 'btn-primary' : 'btn-secondary'" @click="runTool(t.id)">
          {{ t.actionLabel }}
        </button>
      </div>
    </div>

    <!-- 通知通道诊断：统一通知管道的端到端冒烟入口 -->
    <div class="card tool-list">
      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">通知通道测试 <span class="chip chip-neutral tool-badge">Native & Toast</span></span>
          <span class="setting-desc">前台顶层卡片测试；后台 4 秒倒计时原生气泡测试（先最小化或关闭到托盘）</span>
        </span>
        <span class="setting-actions">
          <button class="btn btn-secondary btn-small" @click="triggerTestNotification" title="前台即时测试">
            <AppIcon name="bell" :size="14" /> 前台卡片
          </button>
          <button class="btn btn-secondary btn-small" @click="triggerDelayedTestNotification" title="4秒倒计时后派发，用于最小化后测试原生系统通知">
            <AppIcon name="clock" :size="14" /> 4秒后后台气泡
          </button>
        </span>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 行骨架复用全局 .setting-row/.card/.chip/.btn 原子，此处仅图标盒与徽标节奏 */
.tool-list { display: flex; flex-direction: column; gap: 8px; }
.tool-icon {
  width: 30px; height: 30px; flex: none; border-radius: var(--radius-control);
  background: var(--surface-panel); border: 1px solid var(--color-border);
  color: var(--color-text-muted); display: flex; align-items: center; justify-content: center;
}
.tool-main { flex: 1; }
.tool-badge { margin-left: 6px; vertical-align: 1px; }
.tool-desc { font-family: var(--font-mono); font-size: var(--text-xs); }
</style>
