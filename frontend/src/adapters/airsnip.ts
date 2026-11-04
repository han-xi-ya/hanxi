// ============================================================================
// AirSnip → 托管控制台 adapter
//
// 参照实现见 src/adapters/litemonitor.ts（同为托盘常驻 + requireAdministrator +
// 无 CLI 唤窗的托管件）。与 litemonitor 的两处形状差异：
//   1. 控制面除 primary（打开设置面板）/quit 外另有两个模块自有动作钮
//      （截图 / 贴图），走 #primary-action 槽由视图自绘——本 adapter 把它们
//      以 `actions` 位暴露（不进 ManagedModuleAdapter.control，那里只有两位）；
//   2. 「打开位置」不是版本目录而是上游配置目录 %APPDATA%\airsnip（配置不随
//      exe 走，与 litemonitor 的便携目录语义相反），故 dataDir 直调 OpenConfigDir，
//      无需三源镜像解析。
// 事件面：`airsnip:instance-state` / `airsnip:version-download`（进度键=version）。
// ============================================================================

import * as AirsnipAPI from '../../bindings/hanxi/internal/modules/airsnip/airsnipservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/airsnip/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/airsnip/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type { ManagedActionResult, ManagedModuleAdapter, ManagedReleaseRecord, ManagedVersionRecord } from '../components/managed/adapter'
import { bindVersionProgress, downloadOutcome, setActiveResult, uninstalledResult } from './managedCommon'

/** 模块自有动作钮（视图 #primary-action 槽消费；后端未运行时同一路径先冷启动再投递）。 */
export interface AirsnipActionSpec {
  label: string
  cssClass: string
  run(): Promise<ManagedActionResult>
  titleFor(state: string): string
}

export function createAirsnipAdapter(): ManagedModuleAdapter<Snapshot> & { actions: AirsnipActionSpec[] } {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // 动作钮回执：后端 RPC 返回 ControlOutcome{message}，失败走 throw（共享件转 toast）
  async function runAction(call: () => Promise<{ message?: string }>): Promise<ManagedActionResult> {
    const out = await call()
    return { message: out?.message ?? '' }
  }

  const actions: AirsnipActionSpec[] = [
    {
      label: '✂ 截图',
      cssClass: 'btn-secondary',
      run: () => runAction(() => AirsnipAPI.Shot()),
      // 后端 Shot 在未运行态会先冷启动再投递（一次点击走完），故 stopped 也可点；
      // starting 态留给前序启动收口，避免与上游单实例竞速。
      titleFor: (state) =>
        state === 'starting'
          ? '启动中，请稍候…'
          : state === 'running'
            ? '冻结屏幕并框选（等价上游热键 F1 / 托盘「截图」）'
            : '启动 AirSnip 并立即截图（上游要求管理员权限，未提权时会给出提权指引）',
    },
    {
      label: '📌 贴图',
      cssClass: 'btn-secondary',
      run: () => runAction(() => AirsnipAPI.Pin()),
      titleFor: (state) =>
        state === 'starting'
          ? '启动中，请稍候…'
          : state === 'running'
            ? '把剪贴板内容钉在屏幕上（等价上游热键 F3 / 托盘「从剪贴板贴图」）'
            : '启动 AirSnip 并把剪贴板内容贴到屏幕（剪贴板无位图时上游不会创建贴图）',
    },
  ]

  return {
    actions,

    async getStatus() {
      return (await AirsnipAPI.GetStatus()) ?? null
    },

    subscribeInstanceState: (cb) => {
      // 事件载荷是值类型 Snapshot（后端 Emit 同形），指针/值不匹配会被 Wails 静默丢弃
      useWailsEvent<Snapshot>('airsnip:instance-state', (s) => {
        if (s) cb(s)
      })
    },

    subscribeProgress: (cb) => bindVersionProgress<DownloadProgress>('airsnip:version-download', cb),

    versions: {
      listInstalled: () => AirsnipAPI.ListInstalledVersions(),
      listReleases: () => AirsnipAPI.ListReleases(),
      async getActive() {
        return (await AirsnipAPI.GetActiveVersion()) ?? ''
      },
      async setActive(version): Promise<ManagedActionResult> {
        const ver = await AirsnipAPI.SetActiveVersion(version)
        return setActiveResult(ver)
      },
      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        return downloadOutcome(await AirsnipAPI.DownloadVersion(rel.version), rel.version)
      },
      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        const accepted = await confirm({
          title: `确定卸载 AirSnip ${v.version}？`,
          description: '该版本隔离目录（含 airsnip.exe 与随包文档）将被删除，不可恢复。截图保存目录与设置不受影响（配置在 %APPDATA%\\airsnip）。',
          tone: 'danger',
        })
        if (!accepted) return {}
        await AirsnipAPI.RemoveVersion(v.version)
        return uninstalledResult(v.version)
      },
      async importLocal(): Promise<ManagedActionResult> {
        const path = await prompt({
          title: '请输入 AirSnip 所在目录完整路径（含 airsnip.exe；可带一层解压目录）',
          description: '提示：只收纳 airsnip.exe 与随包文档。设置与快捷键在 %APPDATA%\\airsnip\\config.toml，属上游全局配置，不随导入搬迁。',
        })
        // 先 trim 再判空：用户只敲空格/误粘贴空白串时按"取消"处理，
        // 否则会把空路径送进后端换一句看不懂的路径错误。
        const trimmed = path?.trim() ?? ''
        if (!trimmed) return {}
        const info = await AirsnipAPI.ImportLocal(trimmed)
        return { message: `已导入 AirSnip ${info.version}`, reloadVersions: true }
      },
      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await AirsnipAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await AirsnipAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🛈 打开设置面板',
        cssClass: 'btn-primary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running'
            ? '打开 AirSnip 设置面板（改键、主题、开机自启等都在其中）'
            : state === 'starting'
              ? '启动中…'
              : '启动 AirSnip 并打开设置面板（上游无主窗口，设置面板就是它的界面入口）',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await AirsnipAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 AirSnip 托盘右键菜单内退出' : '等价上游托盘「退出」（WM_CLOSE，异常滞留时宽限后强杀兜底）'),
      },
    },

    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部运行的 AirSnip（非 Hanxi 托管）。可继续投去截图/贴图命令；如需退出请在其托盘菜单操作。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'AirSnip 异常退出' }
      }
      if (s.state === 'running') {
        return {
          tone: 'ok',
          text: 'AirSnip 正在运行：按 F1 截图、F3 贴图，托盘左键开设置（改键/主题/开机自启都在其内）。',
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点「打开设置面板」或「截图」即可启动。上游 release 版要求管理员权限（否则无法覆盖管理员窗口），若 Hanxi 未提权会直接拒绝启动并给出提权重启入口；exe 未做代码签名，首次运行被 SmartScreen 拦时选「更多信息 → 仍要运行」。注意：与 Snipaste 默认热键同为 F1，两者同时运行会互相抢占，请二选一或在上游设置面板改键。'
      }
      if (s.state === 'starting') {
        return '正在拉起 AirSnip（首次提权需在系统弹窗中放行）…'
      }
      return null
    },

    copy: {
      metaHints: [
        '便携包下载自 GitHub Releases（airsnip-v<版本>-win-x64.zip，官方 digest 四层校验）；或「导入本地」把你机器上已有的 airsnip.exe 收纳进托管',
        'Hanxi 每次托管启动前会关闭其内置更新检查（config.toml 的 auto_check_update），版本升级只走本页「版本管理」',
      ],
      firstUseEmpty: '尚未安装 AirSnip —— 下载官方便携版（约 0.7 MB），或「导入本地」把已有 airsnip.exe 收纳进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 AirSnip',
    },

    extras: {
      followOnExit: {
        get: () => AirsnipAPI.GetFollowOnExit(),
        note: '（关闭后 Hanxi 退出完全不影响该工具；AirSnip 常驻托盘待机仅 1 MB 量级，一般无需开启）',
        async set(next): Promise<ManagedActionResult> {
          await AirsnipAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await AirsnipAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本；双击仍会弹 UAC）' }
        },
      },
      dataDir: {
        label: '📂 打开配置目录',
        title: '打开 %APPDATA%\\airsnip（config.toml 所在处；快捷键改键、主题、贴图位置都在里面）',
        async open(): Promise<ManagedActionResult> {
          await AirsnipAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => AirsnipAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await AirsnipAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}

