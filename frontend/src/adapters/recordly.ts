// ============================================================================
// Recordly → 托管控制台 adapter（Wave 5 · 批 0 真实消费件；共享契约增强批收编件）
//
// 逐字迁移自 RecordlyView 原编排段（启停/版本/通道/仓库全部 RPC 与文案零变化）。
// 增强批收编（批 0 三处「adapter 内吃掉」的差异全部回契约表达）：
//  ① NSIS 单目录覆盖式托管：versions 省略 getActive/setActive（不变，契约特性）；
//  ② 动词词面「安装」：不再自捕获自弹 toast——声明 copy.errorPrefix.download
//     「安装失败: 」（⑥ 前缀覆写位），download 失败直接抛错走 store 统一词源；
//  ③ hint 引用已装版本号：投影入参升级为上下文（②），引导行自视图模板迁回
//     adapter.hint——视图不再互抄引导话术。
// 版本互认（核心版本对比）经 versions.sameVersion 钩子进共享面板（⑤），
// 远程表「已安装」判定与「运行中」徽标互认自此由面板执行；视图仅余升级检测
// （banner 第二行）一处，经导出的 recordlyUpgradeAvailable 谓词复用同一份比较器。
// 事件面：`recordly:instance-state` / `recordly:version-download`（进度键=version）。
// ============================================================================

import * as RecordlyAPI from '../../bindings/hanxi/internal/modules/recordly/recordlyservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/recordly/instance/models'
import type { DownloadProgress } from '../../bindings/hanxi/internal/modules/recordly/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { fmtSize } from '../utils/format'
import type {
  ManagedActionResult,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedVersionRecord,
} from '../components/managed/adapter'

// 核心版本号（去预发布后缀）：NSIS 单目录语义下 beta 的 PE 版本与 tag 互认依据
function coreOf(v: string): string {
  return v.trim().replace(/^v(?=\d)/i, '').replace(/-.*$/, '')
}

/**
 * 核心版本比较（a>b 返回 1；不可解析退化为字典序）——recordly 全模块唯一一份
 * 比较器：面板互认钩子（sameVersion）与视图升级检测横幅共用，杜绝双实现漂移。
 */
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

/** 核心互认同一性：精确 tag 命中或数值核心一致（PE 版本抹掉 -beta 后缀的互认场）。 */
function sameVersion(a: string, b: string): boolean {
  return a === b || coreCompare(a, b) === 0
}

/**
 * 升级判定（视图升级警告条的唯一谓词）：已装核心 < 当前通道最新核心。
 * 比较器不出模块边界，视图只消费布尔结论，避免 coreCompare 二次拷贝。
 */
export function recordlyUpgradeAvailable(installedVersion: string, latestVersion: string): boolean {
  return coreCompare(installedVersion, latestVersion) < 0
}



export function createRecordlyAdapter(): ManagedModuleAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  return {
    getStatus: () => RecordlyAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('recordly:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('recordly:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => RecordlyAPI.ListInstalledVersions(),
      listReleases: () => RecordlyAPI.ListReleases(),
      // 单目录覆盖式托管无「设定版本」概念：getActive/setActive 缺省——
      // 共享件消费面（store 空串回退 / 面板不渲染使用中位）见 ManagedVersionPanel.spec。

      /** 核心版本互认进面板（⑤）：已装卡「运行中」与远程表「已安装」共用。 */
      sameVersion,

      /** NSIS 覆盖式安装：运行中/启动中禁覆写，title 逐字沿用视图现词（⑤ downloadBlock）。 */
      downloadBlock: (state) => (state === 'running' || state === 'starting' ? '请先退出运行中的 Recordly' : null),

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        // ⑥收编：不再自捕获——失败直接抛错，'安装失败: ' 前缀经 copy.errorPrefix
        // 回 store 词源表，toast 词面逐字不变、无双弹。
        const res = await RecordlyAPI.DownloadVersion(rel.version)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const accepted = await confirm({
          title: `确定卸载 Recordly ${v.version}？`,
          description: '托管安装目录将被删除。\n（%APPDATA%\\Recordly 中的配置与录像不受影响）',
          tone: 'danger',
        })
        if (!accepted) return {}
        await RecordlyAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地安装',
          description: '默认安装位置：%LOCALAPPDATA%\\Programs\\Recordly\n提示：配置与录像恒在 %APPDATA%\\Recordly，与安装位置无关',
          label: '请输入 Recordly 安装目录完整路径（含 Recordly.exe 与 resources\\app.asar 的整套目录）',
          placeholder: '%LOCALAPPDATA%\\Programs\\Recordly',
        })
        if (!path) return {}
        const info = await RecordlyAPI.ImportLocal(path.trim())
        return { message: `已导入 Recordly ${info.version}`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await RecordlyAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await RecordlyAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 Recordly 并打开窗口'),
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await RecordlyAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 Recordly 窗口内退出' : '关闭其窗口（多窗口收尾不及会兜底强杀）'),
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 Recordly 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 Recordly 窗口内关闭。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'Recordly 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'Recordly 正在运行：录制与剪辑在其窗口内操作（配置与录像存于 %APPDATA%\\Recordly，跨版本共享）。闲置 5 分钟自动退出。' }
      }
      return null
    },

    // ③收编：stopped 引导行引用已装版本号——第二参版本区投影（②），
    // 原视图模板内的三态引导行逐字迁回 hint 槽。
    hint: (s, ctx) => {
      if (s.state === 'stopped') {
        const installedInfo = ctx.installed[0] ?? null
        return installedInfo
          ? `尚未运行：点击「打开窗口」启动 Recordly ${installedInfo.version}，录制与剪辑在其窗口内完成。配置恒存于 %APPDATA%\\Recordly，与托管版本无关。`
          : '尚未安装：请到「版本管理」在线安装或导入本地副本。'
      }
      if (s.state === 'starting') {
        return '正在拉起 Recordly（Electron 冷启动约 2~10 秒）…'
      }
      return null
    },

    channel: {
      options: [
        { value: 'stable', label: 'Stable 稳定' },
        { value: 'beta', label: 'Beta 预发布', warn: 'beta 版上游标注"可能不稳定"，仅供尝鲜' },
      ],
      get: () => RecordlyAPI.GetReleaseChannel(),
      async set(value: string): Promise<ManagedActionResult> {
        await RecordlyAPI.SetReleaseChannel(value)
        // 通道变更影响远程列表取数口径：store.runChannel 消费回执重拉版本区
        return { reloadVersions: true }
      },
    },

    // 面板词面/色档全量覆写（④）：视图方言表迁入共享 ManagedVersionPanel 后，
    // recordly 现词逐字保留于本表——默认值即 ccswitch 标准词，此处只落差异。
    copy: {
      metaLead: (ctx) => `当前托管 ${ctx.installed[0] ? ctx.installed[0].version : '未安装'}`,
      remoteSummary: (n) => `远程版本 ${n} 个`,
      metaHints: [
        'Windows 仅提供 NSIS 在线安装器（GitHub digest + SHA256SUMS 双源校验），安装/升级统一静默落进托管目录，多版本不共存为上游安装器语义所限',
      ],
      installedSectionTitle: '托管安装',
      firstUseEmpty: '尚未安装 Recordly —— 在线安装官方版本，或「导入本地安装」把机器上已有的安装目录收编进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '请先退出 Recordly',
      downloadLabel: (ctx) => (ctx.installedCount ? '覆盖安装' : '安装'),
      firstUseDownloadLabel: (rel) => `安装最新版 ${rel.version}（约 ${fmtSize(rel.size)}）`,
      downloadingWord: '安装中',
      stageWord: (stage) => (['verify', 'install'].includes(stage) ? '校验并静默安装…' : ''),
      installedChipTone: 'positive',
      errorPrefix: { download: '安装失败: ' },
    },

    extras: {
      followOnExit: {
        get: () => RecordlyAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await RecordlyAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await RecordlyAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向托管安装）' }
        },
      },
      dataDir: {
        label: '🗂 数据目录',
        title: '打开 Recordly 用户数据目录（%APPDATA%\\Recordly，配置与录像库）',
        async open(): Promise<ManagedActionResult> {
          await RecordlyAPI.OpenConfigDir()
          return {}
        },
      },
      repo: {
        url: () => RecordlyAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await RecordlyAPI.OpenRepository()
          return {}
        },
      },
    },
  }
}
