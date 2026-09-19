// ============================================================================
// MangoDisk → 托管控制台 adapter（Wave 5 · 批 0 共享契约迁入件）
//
// 逐字迁移自 MangoDiskView 原编排段（启停/版本/联动全部 RPC 与文案零变化）。
// 事件面：`mangodisk:instance-state` / `mangodisk:version-download`（进度键=version，
// 单键最简形态）；动词面：OpenWindow→control.primary、Quit→control.quit。
//
// 模块特例（本视图保留自绘版式、只收敛数据与动作面）：
//  - 主钮文案随状态翻转（running/external→「打开窗口」，其余→「启动 MangoDisk」）——
//    契约 label 为静态词，翻转词与双复刷（成功后 status+versions 并列刷新、失败仅
//    复刷 versions）的编排留视图，经 control.primary.run 复用本 adapter 的 RPC/回执；
//  - 提示条含「已装版本完整性（drifted/invalid）压过运行态」的优先级判定——
//    banner 投影读不到 installed 列表，故 banner 位留在视图自算，本 adapter 不声明；
//  - 版本区为完整性方言表（integrity pill / SHA / 漂移详情），ManagedVersionPanel
//    无对应列形，由视图自留表格承接，数据与动作走共享 store。
// ============================================================================

import * as MangoDiskAPI from '../../bindings/hanxi/internal/modules/mangodisk/mangodiskservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/mangodisk/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/mangodisk/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

export function createMangoDiskAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => MangoDiskAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('mangodisk:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('mangodisk:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => MangoDiskAPI.ListInstalledVersions(),
      listReleases: () => MangoDiskAPI.ListReleases(),
      getActive: () => MangoDiskAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        const ver = await MangoDiskAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const res = await MangoDiskAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 MangoDisk ${v.version}？`,
          description: '仅删除 Hanxi 版本目录，不会删除 %LOCALAPPDATA%\\app.mangodisk.desktop 中的数据。',
          confirmLabel: '卸载',
          tone: 'danger',
        })
        if (!accepted) return {}
        await MangoDiskAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地 EXE',
          description: '请输入本地 MangoDisk EXE 完整路径。Hanxi 只导入该 EXE，不搬运用户数据。',
          label: 'EXE 完整路径',
        })
        if (!path) return {}
        const item = await MangoDiskAPI.ImportLocal(path.trim())
        return { message: `已导入 ${item.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await MangoDiskAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await MangoDiskAPI.OpenWindow()
          return { message: out.message }
        },
        // 静态位标签（实际钮面按状态翻转，翻转词表在视图）
        label: '启动 MangoDisk',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await MangoDiskAPI.Quit()
          return { message: out.message }
        },
        label: '退出',
      },
    },

    extras: {
      followOnExit: {
        get: () => MangoDiskAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await MangoDiskAPI.SetFollowOnExit(next)
          return {
            message: next ? '已启用退出联动（下次启动生效）' : '已关闭退出联动（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await MangoDiskAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      repo: {
        url: () => MangoDiskAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await MangoDiskAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
