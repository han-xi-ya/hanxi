import { computed, reactive, ref } from 'vue'
import * as VSCodeAPI from '../../bindings/hanxi/internal/modules/vscode/vscodeservice'
import type { ControlOutcome, QuitOutcome, Status } from '../../bindings/hanxi/internal/modules/vscode/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/vscode/instance/models'
import type { DownloadProgress, Release, VersionInfo } from '../../bindings/hanxi/internal/modules/vscode/version/models'
import { useConfirm } from '../composables/useConfirm'
import { usePolling } from '../composables/usePolling'
import { usePrompt } from '../composables/usePrompt'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import type {
  ManagedActionResult,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedVersionRecord,
  NormalizedProgress,
} from '../components/managed/adapter'

export type VSCodeForm = 'portable' | 'installer'
export type VSCodeInstalledVersion = ManagedVersionRecord<{ verified: boolean }>

export interface VSCodeRuntime {
  readonly status: Status | null
  readonly portable: Snapshot | null
  readonly installer: Snapshot | null
  readonly installedApp: Status['installed'] | null
  readonly releasesPortable: Release[]
  readonly releasesInstaller: Release[]
  readonly installed: VersionInfo[]
  readonly activeVersion: string
  readonly loading: boolean
  readonly listError: string
  readonly downloading: Record<string, NormalizedProgress>
  readonly uptimePortable: number
  readonly uptimeInstaller: number
  loadVersions(): Promise<void>
  refreshStatus(): Promise<void>
  openWindow(form: VSCodeForm): Promise<ControlOutcome>
  quit(form: VSCodeForm): Promise<QuitOutcome>
  download(form: VSCodeForm, release: Release): Promise<ManagedActionResult>
  setActive(info: VersionInfo): Promise<ManagedActionResult>
  openDir(path: string): Promise<void>
  remove(info: VersionInfo): Promise<ManagedActionResult>
  importLocal(): Promise<ManagedActionResult>
}

export interface VSCodeAdapterBundle {
  adapter: ManagedModuleAdapter<Snapshot, { verified: boolean }>
  runtime: VSCodeRuntime
}

export function vscodeProgressKey(form: VSCodeForm, version: string): string {
  return `${form}:${version}`
}

function managedRelease(release: Release): ManagedReleaseRecord {
  return { version: release.version, size: release.size, published: '' }
}

/**
 * VS Code 双形态适配器。共享 store 只负责单次状态订阅、轮询与 KeepAlive 生命周期；
 * 双远程表、form:version 票据和确认闸由 custom orchestration 保留在本模块控制器。
 */
export function createVSCodeAdapter(): VSCodeAdapterBundle {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  const status = ref<Status | null>(null)
  const releasesPortable = ref<Release[]>([])
  const releasesInstaller = ref<Release[]>([])
  const installed = ref<VersionInfo[]>([])
  const activeVersion = ref('')
  const loading = ref(false)
  const listError = ref('')
  const downloading = ref<Record<string, NormalizedProgress>>({})
  const uptimePortable = ref(0)
  const uptimeInstaller = ref(0)

  // 请求代次（P0 批 3·4.4，口径对齐共享 store）：同类请求后发者拥有写权——
  // 并发刷新/下载事件重拉中，先发慢回的旧响应不得覆盖新状态、不得清掉新请求
  // 的 loading。状态轮询与版本加载各自独立计数：二者常态并发（轮询 2.5s），
  // 共享计数会让状态刷新废掉在途版本加载、把 loading 永久卡死。
  let statusSeq = 0
  let loadSeq = 0

  async function refreshStatus(): Promise<void> {
    const gen = ++statusSeq
    const s = await VSCodeAPI.GetStatus()
    if (gen === statusSeq) status.value = s
  }

  async function loadVersions(): Promise<void> {
    const gen = ++loadSeq
    loading.value = true
    listError.value = ''

    const localTask = Promise.all([VSCodeAPI.ListInstalledVersions(), VSCodeAPI.GetActiveVersion()])
      .then(([local, active]) => {
        if (gen !== loadSeq) return
        installed.value = local ?? []
        activeVersion.value = active ?? ''
      })
      .catch((error: unknown) => {
        if (gen === loadSeq) listError.value = `读取本地版本失败: ${getErrorMessage(error)}`
      })

    const portableTask = VSCodeAPI.ListRemoteVersions('portable')
      .then((remote) => {
        if (gen === loadSeq) releasesPortable.value = remote ?? []
      })
      .catch((error: unknown) => {
        if (gen === loadSeq) listError.value = `获取便携版列表失败: ${getErrorMessage(error)}`
      })

    const installerTask = VSCodeAPI.ListRemoteVersions('installer')
      .then((remote) => {
        if (gen === loadSeq) releasesInstaller.value = remote ?? []
      })
      .catch((error: unknown) => {
        if (gen === loadSeq) listError.value = `获取安装版列表失败: ${getErrorMessage(error)}`
      })

    void Promise.allSettled([portableTask, installerTask]).finally(() => {
      if (gen === loadSeq) loading.value = false
    })
    await localTask
  }

  function updateUptime() {
    const seconds = (snap: Snapshot | null): number => {
      if (snap?.state !== 'running' || !snap.startedAt) return 0
      const started = new Date(snap.startedAt).getTime()
      return Number.isNaN(started) ? 0 : Math.max(0, Math.floor((Date.now() - started) / 1000))
    }
    uptimePortable.value = seconds(status.value?.portable ?? null)
    uptimeInstaller.value = seconds(status.value?.installer ?? null)
  }
  usePolling(updateUptime, 1000)

  async function download(form: VSCodeForm, release: Release): Promise<ManagedActionResult> {
    let result = await VSCodeAPI.DownloadVersion(release.version, form, false)
    if (result.action === 'confirm-required') {
      const accepted = await confirm({
        title: `确认升级安装 VS Code ${release.version}？`,
        description: result.message,
        tone: 'danger',
      })
      if (!accepted) return {}
      result = await VSCodeAPI.DownloadVersion(release.version, form, true)
    }
    return {
      message: result.message || undefined,
      reloadVersions: result.action === 'already-installed',
    }
  }

  async function setActive(info: VersionInfo): Promise<ManagedActionResult> {
    const version = await VSCodeAPI.SetActiveVersion(info.version)
    activeVersion.value = version
    return { message: `已将便携版 ${version} 设为使用版本`, activeVersion: version }
  }

  async function remove(info: VersionInfo): Promise<ManagedActionResult> {
    const accepted = await confirm({
      title: `确定卸载便携版 VS Code ${info.version}？`,
      description: '该版本隔离目录（含 data\\ 内的配置与已装扩展）将被整体删除，不可恢复。\n（安装版与 %APPDATA%\\Code 的日常数据不受影响）',
      tone: 'danger',
    })
    if (!accepted) return {}
    await VSCodeAPI.RemoveVersion(info.version)
    return { message: `已卸载 ${info.version}`, reloadVersions: true }
  }

  async function importLocal(): Promise<ManagedActionResult> {
    const path = await prompt({
      title: '导入本地便携版 VS Code',
      description: '提示：整套迁移（data\\ 除外，导入即全新自包含环境）；安装版目录无需导入，会被自动感知',
      label: '便携版目录完整路径（含 Code.exe 与 bin\\code.cmd）',
    })
    if (!path) return {}
    const info = await VSCodeAPI.ImportLocal(path.trim())
    return { message: `已导入便携版 ${info.version}`, reloadVersions: true }
  }

  const adapter: ManagedModuleAdapter<Snapshot, { verified: boolean }> = {
    async getStatus() {
      await refreshStatus()
      return status.value?.portable ?? null
    },

    subscribeInstanceState(cb) {
      useWailsEvent<Snapshot>('vscode:instance-state', (snap) => {
        if (!snap || !status.value) return
        status.value = snap.form === 'installer'
          ? { ...status.value, installer: snap }
          : { ...status.value, portable: snap }
        if (snap.form === 'installer' && snap.state !== 'running') uptimeInstaller.value = 0
        if (snap.form !== 'installer' && snap.state !== 'running') uptimePortable.value = 0
        cb(status.value.portable)
      })
    },

    subscribeProgress(cb) {
      useWailsEvent<DownloadProgress>('vscode:version-download', (progress) => {
        if (!progress?.version || !progress.form || !['portable', 'installer'].includes(progress.form)) return
        cb({
          key: vscodeProgressKey(progress.form as VSCodeForm, progress.version),
          stage: progress.stage,
          done: progress.done,
          total: progress.total,
          message: progress.message,
        })
      })
    },

    onProgress(progress) {
      downloading.value = { ...downloading.value, [progress.key]: progress }
      if (progress.stage !== 'done') return
      setTimeout(() => {
        const next = { ...downloading.value }
        delete next[progress.key]
        downloading.value = next
      }, 800)
      void loadVersions()
      void refreshStatus().catch((error: unknown) => {
        console.warn('vscode GetStatus failed:', getErrorMessage(error))
      })
    },

    versions: {
      orchestration: 'custom',
      listInstalled: () => VSCodeAPI.ListInstalledVersions(),
      async listReleases() {
        return (await VSCodeAPI.ListRemoteVersions('portable'))?.map(managedRelease) ?? []
      },
      getActive: () => VSCodeAPI.GetActiveVersion(),
      async setActive(version) {
        return setActive({ version } as VersionInfo)
      },
      async download(release) {
        const source = releasesPortable.value.find((item) => item.version === release.version)
        return source ? download('portable', source) : {}
      },
      async remove(info) {
        return remove(info as VersionInfo)
      },
      importLocal,
      async openDir(info) {
        await VSCodeAPI.OpenDir(info.dir)
        return {}
      },
      progressKey: (release) => vscodeProgressKey('portable', release.version),
    },

    multiInstance: {
      instances: [
        { key: 'portable', label: '便携版' },
        { key: 'installer', label: '安装版' },
      ],
    },

    extras: {
      followOnExit: {
        get: () => VSCodeAPI.GetFollowOnExit(),
        label: '随 Hanxi 一起关闭',
        note: '（两形态共用；关闭后 Hanxi 退出完全不影响 VS Code）',
        async set(next) {
          await VSCodeAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭两形态托管实例'
              : '已关闭：Hanxi 退出不影响 VS Code，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create() {
          await VSCodeAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用便携版）' }
        },
      },
      repo: {
        label: '官方网站',
        copyToast: '官网地址已复制',
        url: () => VSCodeAPI.RepositoryURL(),
        async open() {
          await VSCodeAPI.OpenRepository()
          return {}
        },
      },
    },
  }

  const runtime = {
    status: computed(() => status.value),
    portable: computed(() => status.value?.portable ?? null),
    installer: computed(() => status.value?.installer ?? null),
    installedApp: computed(() => status.value?.installed ?? null),
    releasesPortable,
    releasesInstaller,
    installed,
    activeVersion,
    loading,
    listError,
    downloading,
    uptimePortable,
    uptimeInstaller,
    loadVersions,
    refreshStatus,
    openWindow: (form: VSCodeForm) => VSCodeAPI.OpenWindow(form),
    quit: (form: VSCodeForm) => VSCodeAPI.Quit(form),
    download,
    setActive,
    openDir: (path: string) => VSCodeAPI.OpenDir(path),
    remove,
    importLocal,
  }

  return { adapter, runtime: reactive(runtime) as unknown as VSCodeRuntime }
}
