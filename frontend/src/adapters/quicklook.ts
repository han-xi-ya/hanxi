// ============================================================================
// QuickLook → 托管控制台 adapter（Wave 5 · 批 0 共享契约迁入件）
//
// 逐字迁移自 QuickLookView 原编排段（启停/重载/版本/联动/仓库全部 RPC 与文案零变化）。
// 事件面：`quicklook:instance-state` / `quicklook:version-download`（进度键=version，
// 单键最简形态）；动词面：StartQuickLook→control.primary、Quit→control.quit。
// 模块特例：本模块无「打开窗口」语义（样式设置走托盘菜单），主钮是「启动托管」，
// 另有第三个钮「↻ 重载配置」（Reload 命名管道动词）——契约的 control 只有
// primary/quit 两个声明位，reload 走扩展槽 adapter.reset（批 0 预留形状），
// 视图把钮体经 ManagedControlBar 的 #primary-action 槽自绘并接线（三钮序不变）。
// 版本 Tab 为方言表（安装源=便携 zip 解压、进度词「安装中/哈希校验…/解压安装…」等
// 文案位超出 ManagedVersionPanel 通用形），由视图自留表格承接，数据与动作走共享 store。
// ============================================================================

import * as QuickLookAPI from '../../bindings/hanxi/internal/modules/quicklook/quicklookservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/quicklook/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/quicklook/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

export function createQuickLookAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => QuickLookAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('quicklook:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('quicklook:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => QuickLookAPI.ListInstalledVersions(),
      listReleases: () => QuickLookAPI.ListReleases(),
      getActive: () => QuickLookAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await QuickLookAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await QuickLookAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 QuickLook ${v.version}？`,
          description: '该版本托管目录将被删除，不可恢复。\n（便携目录内的设置一并删除，不影响其它版本）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await QuickLookAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '请输入本机 QuickLook 便携目录完整路径（含 QuickLook.exe 与 portable.lock，如手动解压的 QuickLook-4.5.0 文件夹）',
          description: '提示：整个目录会被收纳进托管，配置随便携标记存于目录内',
        })
        if (!path) return {}
        const info = await QuickLookAPI.ImportLocal(path.trim())
        return { message: `已导入 QuickLook ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await QuickLookAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await QuickLookAPI.StartQuickLook()
          return { message: out.message }
        },
        label: '▶ 启动托管',
        cssClass: 'btn-primary',
        disabledFor: (state) => state === 'running' || state === 'starting',
        titleFor: (state) => (state === 'running' || state === 'starting' ? '实例已在运行' : '启动 QuickLook：驻托盘，空格预览全局生效'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await QuickLookAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 QuickLook 托盘菜单退出' : '托管退出：优先管道优雅退出，宽限后强杀兜底（键盘钩子随进程自动摘除，零残渣）'),
      },
    },

    // 扩展槽 reset：运行中实例的配置重载动词（best-effort 命名管道，成功不改快照）。
    // 共享件批 0 不接线；视图自绘「↻ 重载配置」钮消费本槽（busy 闩共用 store）。
    reset: {
      async run(): Promise<ManagedActionResult> {
        const msg = await QuickLookAPI.Reload()
        return { message: msg }
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 QuickLook 实例（非 Hanxi 托管）。空格预览正在生效；退出与样式设置都在其托盘图标菜单中完成。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'QuickLook 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'QuickLook 正在运行：在资源管理器/文件对话框中选中文件按空格键即可即时预览。样式设置请左键点击系统托盘图标 → 设置（上游未提供程序化唤窗入口）。便携配置随 portable.lock 存于安装目录内。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「启动托管」拉起 QuickLook，之后在资源管理器选中任意文件按空格键即可即时预览。样式、快捷键与插件在其托盘图标菜单中调整。配置随便携标记（portable.lock）存于安装目录内，与托管版本切换互不影响。'
      }
      if (s.state === 'starting') {
        return '正在拉起 QuickLook（约 1~3 秒）…'
      }
      return null
    },

    extras: {
      followOnExit: {
        get: () => QuickLookAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await QuickLookAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭 QuickLook（空格预览随即停用）'
              : '已关闭：Hanxi 退出不影响 QuickLook，其继续独立常驻运行（下次启动生效）',
          }
        },
        label: '随 Hanxi 一起关闭',
        note: '（关闭后 QuickLook 独立常驻，Hanxi 退出不影响；贴合其开机常驻本性）',
      },
      repo: {
        url: () => QuickLookAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await QuickLookAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
