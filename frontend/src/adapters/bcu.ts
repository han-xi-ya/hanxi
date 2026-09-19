// ============================================================================
// BCUninstaller → 托管控制台 adapter（Wave 5 · 批 1 迁移件，模式照抄 ccswitch）
//
// 逐字迁移自 BCUView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化）。
// 事件面：`bcu:instance-state` / `bcu:version-download`。
// 方言注记：BCU 远程表为双变体下载（自包含便携版 + 框架依赖精简版），
// ManagedVersionPanel 的单钮形态无法承载，版本 Tab 表体留在视图方言区；
// 进度键按铁律②收编为复合键 `${version}|${variant}`（防同版本双变体互相覆盖），
// 视图经 variantRelease() 给行注入 variant 后走 store.runDownload 统一回执。
// GetDotnetEnvironment 为方言视图私有的诊断 RPC（推荐变体高亮依据），经本
// adapter 的扩展面 getDotnetEnv 收编，视图不直连绑定。
// ============================================================================

import * as BCUAPI from '../../bindings/hanxi/internal/modules/bcu/bcuservice'
import type { DotnetEnv } from '../../bindings/hanxi/internal/modules/bcu/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/bcu/instance/models'
import type { BCURelease, DownloadProgress } from '../../bindings/hanxi/internal/modules/bcu/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

/** BCU 下载变体（与后端 DownloadVersion 的 variant 词一致）。 */
export type BCUVariant = 'portable' | 'fdd'

/** 方言远程行：BCURelease 天然超集 ManagedReleaseRecord（version/published/size/isPre 齐备）。 */
export type BCUReleaseRow = BCURelease & { variant?: BCUVariant }

/** 给远程行注入下载变体（store.runDownload 入参构造器，方言表与空态共用；
 *  返回值为方言类型，赋值兼容共享 ManagedReleaseRecord 消费面）。 */
export function variantRelease(rel: BCURelease, variant: BCUVariant): BCUReleaseRow {
  return { ...rel, variant }
}

/** BCU adapter = 共享总面 + 方言诊断扩展。 */
export type BCUAdapter = ManagedModuleAdapter & {
  /** .NET 桌面运行时探测（方言视图：版本 Tab「.NET 环境徽标」与推荐变体输入）。 */
  getDotnetEnv(): PromiseLike<DotnetEnv | null>
}

export function createBCUAdapter(): BCUAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => BCUAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('bcu:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('bcu:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: `${t.version}|${t.variant || 'portable'}`, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => BCUAPI.ListInstalledVersions(),
      listReleases: () => BCUAPI.ListReleases(),
      getActive: () => BCUAPI.GetActiveVersion(),

      async setActive(version) {
        const ver = await BCUAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel) {
        // variant 由方言视图经 variantRelease() 注入；缺省回退自包含便携版
        const variant = (rel as BCUReleaseRow).variant ?? 'portable'
        const res = await BCUAPI.DownloadVersion(rel.version, variant)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord) {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const ok = await confirm({
          title: `确定卸载 BCU ${v.version}？`,
          description: '该版本隔离目录（含卸载历史与设置数据）将被删除，不可恢复。',
          tone: 'danger',
        })
        if (!ok) return {}
        await BCUAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal() {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地安装',
          label: '请输入 BCU 便携目录完整路径（含 BCUninstaller.exe）',
          description: '将整套迁入：exe + 运行时 + BCUninstaller.settings 卸载历史均保留',
          placeholder: 'C:\\Program Files\\BCUninstaller',
        })
        if (!path) return {}
        const info = await BCUAPI.ImportLocal(path.trim())
        return { message: `已导入 BCU ${info.version}（设置与数据一并迁移）`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord) {
        await BCUAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run() {
          const out = await BCUAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 BCU 并打开主窗口'),
      },
      quit: {
        async run() {
          const out = await BCUAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 BCU 窗口内关闭' : '关闭窗口消息（挂起时强杀兜底）'),
      },
    },

    // 条件提示条（三个变体互斥；文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 BCUninstaller 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 BCU 窗口内关闭。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'BCU 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'BCU 正在运行：批量卸载在其窗口内完成，设置数据（BCUninstaller.settings）保存在版本目录内。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 BCU，批量卸载在其窗口内完成。便携包自含 .NET 运行时（约 76MB），无需系统预装。BCU 上游 manifest 强制管理员权限：需以管理员身份运行 Hanxi 方可启动（否则系统直拒，不代弹 UAC）。'
      }
      if (s.state === 'starting') {
        return '正在拉起 BCU（自包含 .NET 首次启动约 3~10 秒）…'
      }
      return null
    },

    extras: {
      followOnExit: {
        get: () => BCUAPI.GetFollowOnExit(),
        async set(next) {
          await BCUAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create() {
          await BCUAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      repo: {
        url: () => BCUAPI.RepositoryURL(),
        async open() {
          await BCUAPI.OpenRepository()
          return {}
        },
      },
    },

    // ---- 方言扩展（见文件头注记） ----
    getDotnetEnv: () => BCUAPI.GetDotnetEnvironment(),
  }
}
