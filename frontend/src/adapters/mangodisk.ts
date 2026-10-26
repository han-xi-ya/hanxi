// ============================================================================
// MangoDisk → 托管控制台 adapter
//
// 事件面：`mangodisk:instance-state` / `mangodisk:version-download`（进度键=version，
// 单键最简形态）；动词面：OpenWindow→control.primary、Quit→control.quit。
//
// 本视图保留自绘版式（integrity 方言表版式超出共享面板形态），但编排全部
// 回契约单源（编号见 components/managed/adapter.ts「增强批」条目表）：
//  - 主钮翻转词（running/external→「打开窗口」，其余→「启动 MangoDisk」）由
//    label 纯函数声明（①），视图经 store.primaryLabel 取词、不自持词表；
//  - 启动成功「状态+版本区」双复刷：primary.run 回 { reloadVersions: true }，
//    由 runControl 消费（⑥）；
//  - 「已装完整性（drifted/invalid）压过运行态」横幅经 banner 第二参版本区
//    投影判定（②）；方言完整性字段（integrity/integrityNote/currentSha256/
//    fileVersion）经 ManagedVersionRecord<V> 泛型（③）由 store.installed 直读，
//    视图与投影函数均免 cast。
// 「共享件零模块知识」的另一半契约：方言面自留、编排面单源。
// ============================================================================

import * as MangoDiskAPI from '../../bindings/hanxi/internal/modules/mangodisk/mangodiskservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/mangodisk/instance/models'
import type { DownloadProgress, MangoDiskVersionInfo } from '../../bindings/hanxi/internal/modules/mangodisk/version/models'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'
import { bindInstanceState, bindVersionProgress, downloadOutcome, setActiveResult, uninstalledResult } from './managedCommon'

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

    subscribeInstanceState: (cb) => bindInstanceState<Snapshot>('mangodisk:instance-state', cb),

    subscribeProgress: (cb) => bindVersionProgress<DownloadProgress>('mangodisk:version-download', cb),

    versions: {
      listInstalled: () => MangoDiskAPI.ListInstalledVersions(),
      listReleases: () => MangoDiskAPI.ListReleases(),
      getActive: () => MangoDiskAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        return setActiveResult(await MangoDiskAPI.SetActiveVersion(version))
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        // 回执词单源 downloadOutcome（机主裁决：安装回执统一「版本 X 已安装」）
        return downloadOutcome(await MangoDiskAPI.DownloadVersion(rel.version), rel.version)
      },

      async remove(v: ManagedVersionRecord<MangoDiskVersionDialect>): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例）
        const accepted = await confirm({
          title: `确定卸载 MangoDisk ${v.version}？`,
          description: '仅删除 Hanxi 版本目录，不会删除 %LOCALAPPDATA%\\app.mangodisk.desktop 中的数据。',
          confirmLabel: '卸载',
          tone: 'danger',
        })
        if (!accepted) return {}
        await MangoDiskAPI.RemoveVersion(v.version)
        return uninstalledResult(v.version)
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例）
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
        // ①：钮面随状态翻转的动态词
        label: (state) => (state === 'running' || state === 'external' ? '打开窗口' : '启动 MangoDisk'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await MangoDiskAPI.Quit()
          return { message: out.message }
        },
        label: '退出',
        // 禁用式只声明状态半边（busy 半边由共享闩负责）
        disabledFor: (state) => state !== 'running' && state !== 'starting',
      },
    },

    // ②：完整性优先级横幅——drifted/invalid 压过运行态，经第二参版本区投影判定
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
