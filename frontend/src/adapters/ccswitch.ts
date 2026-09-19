// ============================================================================
// CC Switch → 托管控制台 adapter（Wave 5 · 批 0 黄金样本）
//
// 逐字迁移自 CCSwitchView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化），
// 作为「如何把一份现有 service 绑定适配成 ManagedModuleAdapter」的参考实现。
// 事件面：`ccswitch:instance-state` / `ccswitch:version-download`（进度键=version，
// 单键最简形态）；动词面：OpenWindow→control.primary、Quit→control.quit。
// ============================================================================

import * as CCSwitchAPI from '../../bindings/hanxi/internal/modules/ccswitch/ccswitchservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/ccswitch/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/ccswitch/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

export function createCCSwitchAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => CCSwitchAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('ccswitch:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('ccswitch:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => CCSwitchAPI.ListInstalledVersions(),
      listReleases: () => CCSwitchAPI.ListReleases(),
      getActive: () => CCSwitchAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await CCSwitchAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await CCSwitchAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 CC Switch ${v.version}？`,
          description: '该版本隔离目录将被删除，不可恢复。\n（你的 ~/.cc-switch 供应商配置不受影响，后续版本继续共用）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await CCSwitchAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地 CC Switch',
          description: '提示：供应商配置恒在 ~/.cc-switch，与安装位置无关',
          label: '安装目录完整路径（安装版或绿色版均可，含 cc-switch.exe）',
        })
        if (!path) return {}
        const info = await CCSwitchAPI.ImportLocal(path.trim())
        return { message: `已导入 CC Switch ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await CCSwitchAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await CCSwitchAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 CC Switch 并打开窗口'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await CCSwitchAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 CC Switch 托盘退出' : '关闭窗口消息（驻留托盘设置下会兜底强杀）'),
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 CC Switch 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 CC Switch 托盘操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'CC Switch 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'CC Switch 正在运行：供应商切换在其窗口内操作（作用于 ~/.cc-switch 配置，跨版本共享）。闲置 3 分钟自动退出。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 CC Switch，供应商切换在它的窗口内完成。配置恒存于 ~/.cc-switch，与托管版本切换无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 CC Switch（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（Windows-Portable.zip，官方 digest 校验）；或「导入本地」把你机器上已有的安装版/绿色版收纳进来',
        '绿色版标记（portable.ini）随包内置，向上游 Updater 已禁用，更新由 Hanxi 版本管理统一接管',
      ],
      firstUseEmpty: '尚未安装 CC Switch —— 下载官方便携版，或「导入本地安装」把现有 CC Switch 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 CC Switch',
    },

    extras: {
      followOnExit: {
        get: () => CCSwitchAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await CCSwitchAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await CCSwitchAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 CC Switch 用户数据目录（~/.cc-switch，供应商配置与工作区）',
        async open(): Promise<ManagedActionResult> {
          await CCSwitchAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => CCSwitchAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await CCSwitchAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
