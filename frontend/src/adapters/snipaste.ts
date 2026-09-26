import * as SnipasteAPI from '../../bindings/hanxi/internal/modules/snipaste/snipasteservice'
import type { LaunchOutcome, QuitOutcome } from '../../bindings/hanxi/internal/modules/snipaste/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/snipaste/instance/models'
import type {
  DownloadProgress,
  SnipasteRelease,
  SnipasteVersionInfo,
} from '../../bindings/hanxi/internal/modules/snipaste/version/models'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useWailsEvent } from '../composables/useWailsEvent'
import {
  soleVersionUninstallNote,
  type ManagedActionResult,
  type ManagedModuleAdapter,
  type ManagedVersionRecord,
  type NormalizedProgress,
} from '../components/managed/adapter'

export type SnipasteVersionDialect = Pick<
  SnipasteVersionInfo,
  'verificationMode' | 'packageSha256'
>

export interface SnipasteAdapter
  extends ManagedModuleAdapter<Snapshot, SnipasteVersionDialect> {
  snipaste: {
    listInstalled(): Promise<SnipasteVersionInfo[] | null | undefined>
    listReleases(): Promise<SnipasteRelease[] | null | undefined>
    getActive(): Promise<string | null | undefined>
    launch(): Promise<LaunchOutcome>
    quit(): Promise<QuitOutcome | null>
    showImages(): Promise<string | null | undefined>
    download(version: string): Promise<string>
    setActive(version: string): Promise<string>
    remove(info: SnipasteVersionInfo, canRemove: () => boolean): Promise<boolean>
    importLocal(): Promise<SnipasteVersionInfo | null>
    openDir(path: string): Promise<void>
    officialSiteURL(): Promise<string>
    openOfficialSite(): Promise<void>
  }
}

export function createSnipasteAdapter(
  onProgress: (progress: NormalizedProgress) => void,
): SnipasteAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  const snipaste: SnipasteAdapter['snipaste'] = {
    listInstalled: () => SnipasteAPI.ListInstalledVersions(),
    listReleases: () => SnipasteAPI.ListReleases(),
    getActive: () => SnipasteAPI.GetActiveVersion(),
    launch: () => SnipasteAPI.Launch(),

    showImages: () => SnipasteAPI.ShowImages(),

    async quit(): Promise<QuitOutcome | null> {
      const accepted = await confirm({
        title: '退出 Snipaste',
        description:
          'Hanxi 会先向在运行的 Snipaste 发送优雅关闭请求；宽限期内未退出将强制结束。本会话实例的未落盘状态可能丢失；外部实例的贴图由其自动备份机制兜底。以管理员权限运行的实例无法代杀，会如实转为指引。',
        tone: 'warning',
      })
      return accepted ? SnipasteAPI.Quit() : null
    },

    download: (version) => SnipasteAPI.DownloadVersion(version),
    setActive: (version) => SnipasteAPI.SetActiveVersion(version),

    async remove(info, canRemove): Promise<boolean> {
      if (!canRemove()) return false
      // 最后一个版本的卸载 = 模块回到未安装态（后端 guard 已相应放行），确认框如实预告
      const soleNote = await soleVersionUninstallNote(() => SnipasteAPI.ListInstalledVersions())
      const accepted = await confirm({
        title: `确定卸载 Snipaste ${info.version}？`,
        description: '将删除该版本的完整隔离目录，此操作不可恢复。' + soleNote,
        tone: 'danger',
      })
      if (!accepted || !canRemove()) return false
      await SnipasteAPI.RemoveVersion(info.version)
      return true
    },

    async importLocal(): Promise<SnipasteVersionInfo | null> {
      const path = await prompt({
        title: '请输入 Snipaste 免安装目录完整路径（目录内应包含 Snipaste.exe）',
      })
      if (!path) return null
      return SnipasteAPI.ImportLocal(path.trim())
    },

    openDir: (path) => SnipasteAPI.OpenDir(path),
    officialSiteURL: () => SnipasteAPI.OfficialSiteURL(),
    openOfficialSite: () => SnipasteAPI.OpenOfficialSite(),
  }

  return {
    getStatus: () => SnipasteAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('snipaste:instance-state', (snapshot) => {
        if (snapshot) cb(snapshot)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('snipaste:version-download', (progress) => {
        if (!progress?.version) return
        cb({
          key: progress.version,
          stage: progress.stage,
          done: progress.done,
          total: progress.total,
          message: progress.message,
        })
      })
    },

    onProgress,

    versions: {
      orchestration: 'custom',
      listInstalled: snipaste.listInstalled,
      listReleases: snipaste.listReleases,
      getActive: snipaste.getActive,
      async setActive(version): Promise<ManagedActionResult> {
        const activeVersion = await snipaste.setActive(version)
        return { activeVersion }
      },
      async download(release): Promise<ManagedActionResult> {
        await snipaste.download(release.version)
        return {}
      },
      async remove(info: ManagedVersionRecord): Promise<ManagedActionResult> {
        await SnipasteAPI.RemoveVersion(info.version)
        return {}
      },
      async openDir(info: ManagedVersionRecord): Promise<ManagedActionResult> {
        await snipaste.openDir(info.dir)
        return {}
      },
    },

    stateText: (snapshot) => ({
      stopped: '本会话未托管',
      starting: '正在启动',
      running: '本会话实例运行中',
      quitting: '正在退出',
      failed: '实例操作失败',
      external: '外部实例运行中',
    })[snapshot.state] ?? '本会话未托管',

    statusTone: (snapshot) => snapshot.state === 'quitting' ? 'starting' : snapshot.state,
    snipaste,
  }
}
