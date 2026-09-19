// ============================================================================
// MangoDisk → 托管控制台 adapter（Wave 5 · 批 0 共享契约迁入件；增强批收编版）
//
// 逐字迁移自 MangoDiskView 原编排段（启停/版本/联动全部 RPC 与文案零变化）。
// 事件面：`mangodisk:instance-state` / `mangodisk:version-download`（进度键=version，
// 单键最简形态）；动词面：OpenWindow→control.primary、Quit→control.quit。
//
// 增强批收编（本视图保留自绘版式，但「留视图」的三条编排差异全部回契约）：
//  - 主钮翻转词（running/external→「打开窗口」，其余→「启动 MangoDisk」）改由
//    label 纯函数声明（①），视图经 store.primaryLabel 取词、不再自持词表；
//  - 启动成功「状态+版本区」双复刷：primary.run 回 { reloadVersions: true }，
//    runControl 消费（⑥）——视图侧手写编排退役；
//  - 「已装完整性（drifted/invalid）压过运行态」横幅：banner 入参升级为投影
//    上下文（②），installed 进投影面后判定回 adapter 单源。
// 版本记录方言（integrity 族）经 ManagedVersionRecord<V> 泛型展开（③）：
// store.installed 直读 integrity/integrityNote/currentSha256/fileVersion，
// 视图与投影函数均免 cast。方言完整性表版式超出共享面板形态，仍由视图自留，
// 数据与动作全走 store（「共享件零模块知识」的另一半契约：方言面自留、编排面单源）。
// ============================================================================

import * as MangoDiskAPI from '../../bindings/hanxi/internal/modules/mangodisk/mangodiskservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/mangodisk/instance/models'
import type { DownloadProgress, MangoDiskVersionInfo } from '../../bindings/hanxi/internal/modules/mangodisk/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'

/** 已装记录方言字段包（③）：完整性族字段随 store.installed 类型直读。 */
export type MangoDiskVersionDialect = Pick<
  MangoDiskVersionInfo,
  'integrity' | 'integrityNote' | 'expectedSha256' | 'currentSha256' | 'fileVersion' | 'productName'
>

export function createMangoDiskAdapter(): ManagedModuleAdapter<Snapshot, MangoDiskVersionDialect> {
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

      async remove(v: ManagedVersionRecord<MangoDiskVersionDialect>): Promise<ManagedActionResult> {
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

      async openDir(v: ManagedVersionRecord<MangoDiskVersionDialect>): Promise<ManagedActionResult> {
        await MangoDiskAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await MangoDiskAPI.OpenWindow()
          // ⑥：冷启动可能激活自动版本——成功回执要求重拉版本区（runControl 双复刷）
          return { message: out.message, reloadVersions: true }
        },
        // ①：钮面随状态翻转的动态词（原视图词表迁入 label 纯函数）
        label: (state) => (state === 'running' || state === 'external' ? '打开窗口' : '启动 MangoDisk'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await MangoDiskAPI.Quit()
          return { message: out.message }
        },
        label: '退出',
        // 原视图禁用式 store.busy || !isRunningOrStarting 的状态半边收编
        disabledFor: (state) => state !== 'running' && state !== 'starting',
      },
    },

    // ②：完整性优先级横幅——drifted/invalid 压过运行态（第二参版本区投影后
    // 判定回 adapter；文案三变体逐字保留视图现词）
    banner: (s, ctx) => {
      const currentVersion = s.version || ctx.active || ''
      const currentInstalled = currentVersion ? ctx.installed.find((item) => item.version === currentVersion) ?? null : null
      if (currentInstalled?.integrity === 'drifted') return { tone: 'warn', text: currentInstalled.integrityNote }
      if (currentInstalled?.integrity === 'invalid') return { tone: 'error', text: currentInstalled.integrityNote }
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到安装版、portable 或其他版本的外部实例。Hanxi 可唤起窗口，但不会强制终止它。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'MangoDisk 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'MangoDisk 正在运行；磁盘扫描、清理与系统设置均在原版窗口内完成。' }
      }
      return null
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
