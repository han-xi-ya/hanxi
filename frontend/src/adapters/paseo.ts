// ============================================================================
// Paseo → 托管控制台 adapter（Wave 5 · 批 0 真实消费件；共享契约增强批收编件）
//
// 逐字迁移自 PaseoView 原编排段（启停/版本/通道/仓库全部 RPC 与文案零变化）。
// 增强批收编（批 0 四处「adapter 内吃掉/留视图」差异的回迁）：
//  ①「active 为空=自动最新已装」：契约新增 versions.implicitActive 位（⑤）——
//     最新已装卡由共享面板亮隐式「使用版本」徽标（copy.activeBadge）且不显
//     「设为使用」钮；引导行经 hint 上下文（②）引用将启版本，视图方言表退役。
//  ② 动词词面「安装」+ 失败现词 '安装失败: '：不再自捕获，download 直接抛错，
//     前缀经 copy.errorPrefix.download 回 store 词源（⑥）。
//  ③ 设定版本失败后重拉核对：失败抛错走 store 统一 '设置失败: ' 前缀；
//     失败后不 reload 与共享件口径一致——activeVersion 现值本就以成功回执
//     settle 为准，重拉属过度防御，收编后视图不再互抄。
//  ④ 双数据目录入口不变：Electron 数据走 extras.dataDir，daemon 数据钮经
//     视图 #extras-action 槽（⑦ 已带作用域）。
// 另：verifiedHash 方言徽标——ManagedVersionRecord<V> 泛型化（③）后
//     store.installed 直接带该字段，面板 #version-row-extra 槽渲染免 cast。
// 事件面：`paseo:instance-state` / `paseo:version-download`（进度键=version）。
// ============================================================================

import * as PaseoAPI from '../../bindings/hanxi/internal/modules/paseo/paseoservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/paseo/instance/models'
import type { DownloadProgress, PaseoVersionInfo } from '../../bindings/hanxi/internal/modules/paseo/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { fmtSize } from '../utils/format'
import type {
  ManagedActionResult,
  ManagedModuleAdapter,
  ManagedProjection,
  ManagedReleaseRecord,
  ManagedVersionRecord,
} from '../components/managed/adapter'

/** 已装记录方言字段包（③）：共享面板/视图经 store.installed 直读 verifiedHash，零 cast。 */
export type PaseoVersionDialect = Pick<PaseoVersionInfo, 'verifiedHash'>

/** 规范版本目录名（后端已新→旧排序；imported- 等非常规目录不参与隐式判定）。 */
function canonicalVersion(v: string): boolean {
  return /^\d+\.\d+\.\d+/.test(v)
}

// 核心版本号（去预发布后缀）：升级判定用数值核心对比，不可解析退化字典序
function coreOf(v: string): string {
  return v.replace(/-.*$/, '')
}

function coreCompare(a: string, b: string): number {
  const pa = coreOf(a).split('.').map(Number)
  const pb = coreOf(b).split('.').map(Number)
  for (let i = 0; i < 3; i++) {
    const na = pa[i], nb = pb[i]
    if (Number.isNaN(na) || Number.isNaN(nb)) return coreOf(a) < coreOf(b) ? -1 : coreOf(a) > coreOf(b) ? 1 : 0
    if (na !== nb) return na > nb ? 1 : -1
  }
  return 0
}

/** 升级谓词（视图升级警告条唯一入口）：最新已装核心 < 当前通道最新核心。 */
export function paseoUpgradeAvailable(installedVersion: string, latestVersion: string): boolean {
  return coreCompare(installedVersion, latestVersion) < 0
}

/**
 * 「🐾 daemon 数据」钮的 RPC（OpenDaemonHome 族）：extras.dataDir 槽批 0 只有一位
 * （差异④），第二枚数据目录钮经视图 #extras-action 槽渲染，调用面随本 adapter
 * 家族承载——视图不直碰 bindings。失败经消费方统一 toast '打开目录失败: '。
 */
export async function openPaseoDaemonHome(): Promise<void> {
  await PaseoAPI.OpenDaemonHome()
}

export function createPaseoAdapter(): ManagedModuleAdapter<Snapshot, PaseoVersionDialect> {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

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

      /** ⑤：隐式使用版本——active 为空 = 自动最新已装（规范版本目录首条）。 */
      implicitActive: (installedList) => installedList.find((v) => canonicalVersion(v.version))?.version ?? '',

      async setActive(version): Promise<ManagedActionResult> {
        // ③收编：失败直接抛错，store 统一 '设置失败: ' 前缀；不再自捕获重拉
        const ver = await PaseoAPI.SetActiveVersion(version)
        return { message: `下次启动将使用 Paseo ${version}`, activeVersion: ver }
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        // 运行中不拒绝（解压目标是独立新版本目录）：现视图无禁用钮、忽略返回值。
        // ②收编：失败抛错走词源表（copy.errorPrefix.download = '安装失败: '）。
        await PaseoAPI.DownloadVersion(rel.version)
        return {}
      },

      async remove(v: ManagedVersionRecord<PaseoVersionDialect>): Promise<ManagedActionResult> {
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

      async openDir(v: ManagedVersionRecord<PaseoVersionDialect>): Promise<ManagedActionResult> {
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

    // ②收编：stopped 引导行引用「将启动的版本」（active 空回退隐式自动最新）——
    // 第二参版本区投影携带已装列表，原视图模板的三态引导行逐字迁回 hint 槽。
    hint: (s, ctx: ManagedProjection<PaseoVersionDialect>) => {
      if (s.state === 'stopped') {
        const launch = ctx.active || (ctx.installed.find((v) => canonicalVersion(v.version))?.version ?? '')
        return launch
          ? `尚未运行：点击「打开窗口」启动 Paseo ${launch}。agent 编排与手机配对在其窗口内操作；数据在 %APPDATA%\\Paseo 与 ~/.paseo，与托管版本目录无关。`
          : '尚未安装：请到「版本管理」在线安装或导入本地副本。'
      }
      if (s.state === 'starting') {
        return '正在拉起 Paseo（Electron 冷启动 + 内置 daemon 拉起，约 2~15 秒）…'
      }
      return null
    },

    channel: {
      options: [
        { value: 'stable', label: 'Stable 稳定' },
        { value: 'beta', label: 'Beta 预发布', warn: 'beta 为上游预发布版，仅供尝鲜' },
      ],
      get: () => PaseoAPI.GetReleaseChannel(),
      async set(value: string): Promise<ManagedActionResult> {
        await PaseoAPI.SetReleaseChannel(value)
        // 通道变更影响远程列表取数口径：store.runChannel 消费回执重拉版本区
        return { reloadVersions: true }
      },
    },

    // ④收编：共享面板词面覆写（视图方言表退役后逐字现词全部回本表）。
    copy: {
      metaLead: (ctx) => {
        const implicit = ctx.installed.find((v) => canonicalVersion(v.version))?.version ?? ''
        const shown = ctx.active || (implicit ? `自动最新（${implicit}）` : '未安装')
        return `使用版本 ${shown}`
      },
      remoteSummary: (n) => `远程版本 ${n} 个`,
      metaHints: [
        '官方 Windows 便携 zip 解压进独立版本目录（GitHub digest sha256 + 字节数 + zip CRC + 布局四层校验），多版本共存，数据共享不受版本增删影响',
      ],
      installedSectionTitle: '托管版本',
      activeBadge: '使用版本',
      setActiveTitle: '下次启动使用该版本（运行中的实例不受影响）',
      firstUseEmpty: '尚未安装 Paseo —— 在线安装官方便携 zip 解压版，或「导入本地安装」把机器上已有的程序目录收编进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出该版本',
      uninstallIdleHint: '仅删本版本目录，共享数据不受影响',
      downloadLabel: () => '安装',
      firstUseDownloadLabel: (rel) => `安装最新版 ${rel.version}（约 ${fmtSize(rel.size)}）`,
      downloadingWord: '安装中',
      stageWord: (stage) => (['verify', 'extract'].includes(stage) ? '校验并解压…' : ''),
      installedChipTone: 'positive',
      errorPrefix: { download: '安装失败: ' },
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
