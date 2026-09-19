// ============================================================================
// Paseo → 托管控制台 adapter（Wave 5 · 批 0 真实消费件）
//
// 逐字迁移自 PaseoView 原编排段（启停/版本/通道/仓库全部 RPC 与文案零变化）。
// 与黄金样本 ccswitch / 同批 recordly 的契约差异（批 0 成色实测，均不改共享件）：
//  ① 多版本目录并存但「使用版本」可为空（空=自动最新已装）：getActive/setActive
//     照常提供；视图侧的自动最新回退高亮（activeVersion 空时亮最新已装卡）属
//     业务方言，共享面板无此位——版本表留视图渲染，store.activeVersion 消费。
//  ② 动词词面「安装」+ 失败现词 '安装失败: ' 与 store 统一前缀 '下载失败: ' 冲突：
//     download 自捕获自弹（同 recordly 差异②）。
//  ③ 设定版本失败后现行为要求重拉列表核对：store 的失败路径不 reloadVersions，
//     setActive 内自捕获 toast 后回 {reloadVersions:true} 走成功通道达成重拉
//     （'设置失败: ' 词面与共享前缀相同，行为差异才是自捕获的理由）。
//  ④ 双数据目录入口（%APPDATA%\Paseo 与 ~/.paseo）：contract extras.dataDir
//     只有一位——Electron 数据走 dataDir 槽，daemon 数据钮留视图 #extras-action。
// 另：ManagedVersionRecord 无 verifiedHash 扩展位（快照有 S 泛型、版本记录没有），
//     该方言列的视图渲染走 cast 读取（见 PaseoView 的 unverifiedHash 辅助）。
// 事件面：`paseo:instance-state` / `paseo:version-download`（进度键=version）。
// ============================================================================

import * as PaseoAPI from '../../bindings/hanxi/internal/modules/paseo/paseoservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/paseo/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/paseo/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'
import type {
  ManagedActionResult,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedVersionRecord,
} from '../components/managed/adapter'

/**
 * 「🐾 daemon 数据」钮的 RPC（OpenDaemonHome 族）：extras.dataDir 槽批 0 只有一位
 * （差异④），第二枚数据目录钮经视图 #extras-action 槽渲染，调用面随本 adapter
 * 家族承载——视图不直碰 bindings。失败经消费方统一 toast '打开目录失败: '。
 */
export async function openPaseoDaemonHome(): Promise<void> {
  await PaseoAPI.OpenDaemonHome()
}

export function createPaseoAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()
  const { showToast } = useToast()

  return {
    getStatus: () => PaseoAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('paseo:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('paseo:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => PaseoAPI.ListInstalledVersions(),
      listReleases: () => PaseoAPI.ListReleases(),
      getActive: () => PaseoAPI.GetActiveVersion(),

      async setActive(version): Promise<ManagedActionResult> {
        try {
          const ver = await PaseoAPI.SetActiveVersion(version)
          return { message: `下次启动将使用 Paseo ${version}`, activeVersion: ver }
        } catch (e) {
          // 差异③：失败后重拉列表核对后端真实值（现视图逐字行为）
          showToast(`设置失败: ${getErrorMessage(e)}`)
          return { reloadVersions: true }
        }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        try {
          // 运行中不拒绝（解压目标是独立新版本目录）：现视图无禁用钮、忽略返回值
          await PaseoAPI.DownloadVersion(rel.version)
          return {}
        } catch (e) {
          // 差异②：词面「安装失败: 」保持逐字现词，静默回执避免双 toast
          showToast(`安装失败: ${getErrorMessage(e)}`)
          return {}
        }
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 Paseo ${v.version}？`,
          description: '仅删除该托管版本目录。\n（%APPDATA%\\Paseo 与 ~/.paseo 中的设置、会话与手机配对数据为共享用户数据，不受影响）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await PaseoAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地安装',
          description: '常见位置：安装版 %LOCALAPPDATA%\\Programs\\Paseo 或自定义目录\n提示：数据恒在 %APPDATA%\\Paseo 与 ~/.paseo，与程序目录无关，导入不搬数据',
          label: '请输入 Paseo 程序目录完整路径（含 Paseo.exe 与 resources\\app.asar 的整套目录）',
          placeholder: '%LOCALAPPDATA%\\Programs\\Paseo',
        })
        if (!path) return {}
        const info = await PaseoAPI.ImportLocal(path.trim())
        return { message: `已导入 Paseo ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await PaseoAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await PaseoAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起已运行窗口' : state === 'starting' ? '启动中…' : '启动 Paseo 并打开窗口'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await PaseoAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 Paseo 窗口内退出' : '关闭窗口优雅退出（daemon 清理不及会兜底强杀）'),
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到非 Hanxi 启动的 Paseo 实例（共享数据模式下同一实例锁组，全局仅一个桌面主实例）。可唤起其窗口；如需退出请在 Paseo 窗口内关闭。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Paseo 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'Paseo 正在运行：agent 编排在其窗口内完成（数据在 %APPDATA%\\Paseo 与 ~/.paseo，与自装实例共享；无窗运行 ≠ 空闲，不设自动退出）。' }
      }
      return null
    },

    // hint 缺位说明：stopped 引导行引用「将启动的版本」（activeVersion 空时回退
    // 最新已装），纯快照投影 + 无自动最新位——引导行留视图模板渲染。

    channel: {
      options: [
        { value: 'stable', label: 'Stable 稳定' },
        { value: 'beta', label: 'Beta 预发布' },
      ],
      get: () => PaseoAPI.GetReleaseChannel(),
      async set(value: string): Promise<ManagedActionResult> {
        await PaseoAPI.SetReleaseChannel(value)
        // 通道变更影响远程列表取数口径：请消费方重拉版本区
        return { reloadVersions: true }
      },
    },

    extras: {
      followOnExit: {
        get: () => PaseoAPI.GetFollowOnExit(),
        note: '（默认关闭：Hanxi 退出不影响 Paseo 及其 agent 会话）',
        async set(next): Promise<ManagedActionResult> {
          await PaseoAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出将连带终止 Paseo 及其上正在运行的 agent 会话（下次启动生效）'
              : '已关闭：Hanxi 退出不影响 Paseo，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await PaseoAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      dataDir: {
        label: '🗂 Electron 数据',
        title: '打开 Electron 数据目录（%APPDATA%\\Paseo：窗口状态与桌面设置）',
        async open(): Promise<ManagedActionResult> {
          await PaseoAPI.OpenElectronDataDir()
          return {}
        },
      },
      repo: {
        url: () => PaseoAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await PaseoAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
