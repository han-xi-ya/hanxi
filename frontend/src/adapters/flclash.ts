// ============================================================================
// FlClash → 托管控制台 adapter（Wave 5 · 批 0 共享契约迁入件）
//
// 逐字迁移自 FlClashView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化），
// 模式照抄批 0 黄金样本 src/adapters/ccswitch.ts。
// 事件面：`flclash:instance-state` / `flclash:version-download`（进度键=version，
// 单键最简形态）；动词面：OpenWindow→control.primary、Quit→control.quit。
// ============================================================================

import * as FlClashAPI from '../../bindings/hanxi/internal/modules/flclash/flclashservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/flclash/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/flclash/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

export function createFlClashAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => FlClashAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('flclash:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('flclash:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => FlClashAPI.ListInstalledVersions(),
      listReleases: () => FlClashAPI.ListReleases(),
      getActive: () => FlClashAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await FlClashAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await FlClashAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 FlClash ${v.version}？`,
          description: '该版本隔离目录将被删除，不可恢复。\n（%APPDATA% 下的代理配置不受影响，后续版本继续共用）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await FlClashAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入 FlClash 安装目录完整路径（便携目录，含 FlClash.exe）',
          description: '提示：代理配置在 %APPDATA%，与安装位置无关',
        })
        if (!path) return {}
        const info = await FlClashAPI.ImportLocal(path.trim())
        return { message: `已导入 FlClash ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await FlClashAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await FlClashAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 FlClash 并打开窗口'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await FlClashAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 FlClash 托盘退出' : '关闭窗口消息（驻留托盘设置下会兜底强杀）'),
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 FlClash 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 FlClash 托盘操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'FlClash 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'FlClash 正在运行：代理配置在其窗口内操作（配置数据在 %APPDATA% 用户目录，各版本共享）。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 FlClash，代理配置在它的窗口内完成。配置存于 %APPDATA% 用户目录，与版本切换无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 FlClash（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（Windows-Portable.zip，官方 digest 校验）；或「导入本地」把你机器上已有的安装版/绿色版收纳进来',
        '上游另有 SHA256SUMS 清单资产；本模块以 GitHub API digest 为校验主依据（四层完整性）',
      ],
      firstUseEmpty: '尚未安装 FlClash —— 下载官方便携版，或「导入本地安装」把现有 FlClash 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 FlClash',
    },

    extras: {
      followOnExit: {
        get: () => FlClashAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await FlClashAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await FlClashAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 FlClash 用户数据目录（%APPDATA%\\com.follow\\clash，订阅与配置）',
        async open(): Promise<ManagedActionResult> {
          await FlClashAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => FlClashAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await FlClashAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
