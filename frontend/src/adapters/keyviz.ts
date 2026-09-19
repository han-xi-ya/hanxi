// ============================================================================
// Keyviz → 托管控制台 adapter（Wave 5 · 批 1 · MSI 提取族）
//
// 逐字迁移自 KeyvizView 原编排段（启停/版本/联动全部 RPC 与文案零变化），
// 参照实现见 src/adapters/ccswitch.ts（批 0 黄金样本）；与 piclite 同族——
// MSI 提取族特殊安装文案全部经 copy 钩子与 adapter 动词回执承载，表内
// 「下载中/校验解压安装…」等共享面板标准词按批 0 契约定档。
// 差异点：主钮为 StartKeyviz（▶ 启动可视化，btn-primary，运行中禁用——
// 非唤窗语义）；上游无 CreateDesktopShortcut，extras 无快捷方式条目。
// 事件面：`keyviz:instance-state` / `keyviz:version-download`
// （进度键=version，单键最简形态）。
// ============================================================================

import * as KeyvizAPI from '../../bindings/hanxi/internal/modules/keyviz/keyvizservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/keyviz/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/keyviz/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

export function createKeyvizAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => KeyvizAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('keyviz:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('keyviz:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => KeyvizAPI.ListInstalledVersions(),
      listReleases: () => KeyvizAPI.ListReleases(),
      getActive: () => KeyvizAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await KeyvizAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await KeyvizAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 Keyviz ${v.version}？`,
          description: '该版本托管目录将被删除，不可恢复。\n（你的 %APPDATA%\\org.keyviz 样式配置不受影响，后续版本继续共用）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await KeyvizAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入本机 Keyviz 安装目录完整路径（含 keyviz.exe，如 C:\\Program Files\\keyviz）',
          description: '提示：配置恒在 %APPDATA%\\org.keyviz，与安装位置无关',
        })
        if (!path) return {}
        const info = await KeyvizAPI.ImportLocal(path.trim())
        return { message: `已导入 Keyviz ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await KeyvizAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await KeyvizAPI.StartKeyviz()
          return { message: out.message }
        },
        label: '▶ 启动可视化',
        cssClass: 'btn-primary',
        disabledFor: (state) => state === 'running' || state === 'starting',
        titleFor: (state) =>
          state === 'running' || state === 'starting' ? '实例已在运行' : '启动 Keyviz：驻托盘并全局可视化按键/鼠标',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await KeyvizAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external' ? '外部实例请在 Keyviz 托盘菜单退出' : '托管退出：直接终止实例（样式配置即时写盘不受影响）',
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return {
          tone: 'warn',
          text: '检测到外部 Keyviz 实例（非 Hanxi 托管）。按键可视化正在生效；退出与样式设置都在其托盘图标菜单（左键即弹）中完成。',
        }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Keyviz 异常退出' }
      }
      if (s.state === 'running') {
        return {
          tone: 'ok',
          text: 'Keyviz 正在运行：全局按键/鼠标可视化即时生效。样式设置请左键点击系统托盘图标 → Settings（上游未提供程序化唤窗入口）。配置存于 %APPDATA%\\org.keyviz。',
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「启动可视化」拉起 Keyviz，按键/鼠标特效即刻全局生效；样式、快捷键与过滤器在其托盘菜单 → Settings 中调整。配置恒存于 %APPDATA%\\org.keyviz，与托管版本切换无关。'
      }
      if (s.state === 'starting') {
        return '正在拉起 Keyviz（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '安装源为官方 GitHub Releases 的 Windows MSI（官方 digest sha256 四层校验），经 msiexec 管理提取落进隔离目录，不触碰系统',
        '「导入本地」可把你机器上已安装的 Keyviz（Program Files\\keyviz）收纳进托管；配置在 %APPDATA%，两者互不影响',
      ],
      firstUseEmpty: '尚未安装 Keyviz —— 下载官方 MSI 免安装提取，或「导入本地安装」把现有 Keyviz 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 Keyviz',
    },

    extras: {
      followOnExit: {
        get: () => KeyvizAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await KeyvizAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，Keyviz 继续独立运行（下次启动生效）',
          }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 Keyviz 用户数据目录（%APPDATA%\\org.keyviz，样式配置）',
        async open(): Promise<ManagedActionResult> {
          await KeyvizAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => KeyvizAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await KeyvizAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
