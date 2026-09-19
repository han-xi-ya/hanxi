// ============================================================================
// PicLite → 托管控制台 adapter（Wave 5 · 批 1 · MSI 提取族）
//
// 逐字迁移自 PicLiteView 原编排段（启停/版本/联动全部 RPC 与文案零变化），
// 参照实现见 src/adapters/ccswitch.ts（批 0 黄金样本）。
// MSI 提取族特殊安装文案（来源说明/首用空态/卸载指引/确认与输入话术）全部
// 经 copy 钩子与 adapter 动词回执承载；表内「下载中/校验解压安装…」等
// 共享面板标准词按批 0 契约定档（差异登记见迁移报告）。
// 事件面：`piclite:instance-state` / `piclite:version-download`
// （进度键=version，单键最简形态）；动词面：OpenWindow→control.primary、
// Quit→control.quit。
// ============================================================================

import * as PicLiteAPI from '../../bindings/hanxi/internal/modules/piclite/picliteservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/piclite/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/piclite/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

export function createPicLiteAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => PicLiteAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('piclite:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('piclite:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => PicLiteAPI.ListInstalledVersions(),
      listReleases: () => PicLiteAPI.ListReleases(),
      getActive: () => PicLiteAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await PicLiteAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await PicLiteAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 PicLite ${v.version}？`,
          description: '该版本托管目录将被删除，不可恢复。\n（你的 %APPDATA%\\com.piclite.desktop 配置与图床设置不受影响，后续版本继续共用）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await PicLiteAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入本机 PicLite 安装目录完整路径（含 piclite.exe，如 C:\\Program Files\\PicLite）',
          description: '提示：配置恒在 %APPDATA%\\com.piclite.desktop，与安装位置无关',
        })
        if (!path) return {}
        const info = await PicLiteAPI.ImportLocal(path.trim())
        return { message: `已导入 PicLite ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await PicLiteAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await PicLiteAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running' ? '唤起主窗口（单实例协议）' : state === 'starting' ? '启动中…' : '启动 PicLite 并打开工作台窗口',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await PicLiteAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external' ? '外部实例请在 PicLite 托盘菜单退出' : '托管退出：直接终止实例（配置即时写盘不受影响；进行中的批量压缩会中断）',
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 PicLite 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 PicLite 托盘图标菜单操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'PicLite 异常退出' }
      }
      if (s.state === 'running') {
        return {
          tone: 'ok',
          text: 'PicLite 正在运行：压缩工作台、悬浮结果与图床上传都在它自己的窗口/悬浮层操作，配置存于 %APPDATA%\\com.piclite.desktop。无可见窗口且闲置 3 分钟自动退出。',
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 PicLite，图片压缩与批量处理在它的窗口/悬浮层内完成。配置恒存于 %APPDATA%\\com.piclite.desktop，与托管版本切换无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 PicLite（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '安装源为官方 GitHub Releases 的 Windows x64 MSI（官方 digest sha256 四层校验），经 msiexec 管理提取落进隔离目录，不触碰系统',
        '「导入本地」可把你机器上已安装的 PicLite（Program Files\\PicLite）收纳进托管；配置在 %APPDATA%，两者互不影响',
      ],
      firstUseEmpty: '尚未安装 PicLite —— 下载官方 MSI 免安装提取，或「导入本地安装」把现有 PicLite 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 PicLite',
    },

    extras: {
      followOnExit: {
        get: () => PicLiteAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await PicLiteAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，PicLite 继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await PicLiteAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 PicLite 用户数据目录（%APPDATA%\\com.piclite.desktop，配置与图床设置）',
        async open(): Promise<ManagedActionResult> {
          await PicLiteAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => PicLiteAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await PicLiteAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
