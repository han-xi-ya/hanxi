// ============================================================================
// PaperTodo → 托管控制台 adapter（Wave 5 · 批 0 · variant 槽实战样本）
//
// 逐字迁移自 PaperTodoView 原编排段（启停/版本/变体/联动全部 RPC 与文案零变化），
// 是契约 `adapter.variant` 槽（papertodo Get/SetVariant 预留位）的首个接线者：
// 单版本双变体（self-contained 完整版 / no-runtime 精简版）——下载变体偏好由
// adapter 内部单源持有（variant.get/set 与 versions.download 同源），共享件
// 对"变体"零感知即可正确下载。
//
// 单目录覆盖布局相对 ccswitch（多版本隔离）的三处契约折算（批 0 定型口径）：
//   ① versions.listInstalled 退化为 0/1：GetInstalledVersion 至多一条记录 →
//      投影成 0 或 1 项的 ManagedVersionRecord[]。共享已装卡装不下的业务成色
//      （当前托管变体、便签数据在册、sha256 审计指纹、单目录覆盖升级话术）经
//      copy.remoteSummary 与 copy.metaHints（动态 getter，随 store.installed
//      变更的渲染轮次重估）按现状对齐补回。
//   ② GetRuntimeStatus（系统 .NET 桌面运行时探测）不在引擎快照里：按契约
//      「快照业务扩展字段以可选字段并入模块快照类型」口径，合流为
//      PaperSnapshot 的可选扩展字段 `runtime`（getStatus 合流、listInstalled
//      随版本区刷新重探），视图的变体可用性注记据此投影。
//   ③ HidePapers（收拢纸片，hide 命令信使）= 契约 reset 槽的 dismiss 族清态
//      动词；钮位由视图经 ManagedControlBar 既有 #primary-action 控制位注入，
//      与「唤回纸片 / 退出」并列同排。
//
// 事件面：`papertodo:instance-state` / `papertodo:version-download`（进度键
// =version，单键最简形态）。动词面：OpenWindow→control.primary（唤回纸片）、
// Quit→control.quit。
// ============================================================================

import * as PaperAPI from '../../bindings/hanxi/internal/modules/papertodo/papertodoservice'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/papertodo/instance/models'
import type { DownloadProgress, PaperVersionInfo } from '../../bindings/hanxi/internal/modules/papertodo/version/models'
import type { RuntimeStatus } from '../../bindings/hanxi/internal/modules/papertodo/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import type {
  ManagedActionResult,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedVersionRecord,
} from '../components/managed/adapter'

export type PaperVariant = 'self-contained' | 'no-runtime'

/** PaperTodo 扩展快照：`runtime` 为契约口径并入的可选扩展字段（见文件头②）。 */
export type PaperSnapshot = Snapshot & { runtime: RuntimeStatus | null }

/**
 * PaperTodo adapter 扩展面：在共享契约之上追加模块私有的类型收窄与只读钩子
 * （共享件仍按 ManagedModuleAdapter 消费，这些成员不破坏赋值兼容）。
 */
export interface PaperTodoAdapter extends ManagedModuleAdapter<PaperSnapshot> {
  /** 本模块必有变体槽/reset 动词（收窄基契约的可选声明，视图免判空）。 */
  variant: NonNullable<ManagedModuleAdapter<PaperSnapshot>['variant']>
  reset: NonNullable<ManagedModuleAdapter<PaperSnapshot>['reset']>
  /**
   * GetInstalledVersion 原始记录（含变体/hasData/指纹），未安装为 null。
   * 契约的 ManagedVersionRecord 装不下这些方言字段，视图变体卡的
   * 「换装」目标反查与现状话术经此只读钩子取用（随 listInstalled 刷新）。
   */
  installedInfo(): PaperVersionInfo | null
}

/** 变体 → 中文名（逐字沿用视图现词：完整版 / 精简版）。 */
export function variantName(v: string | null | undefined): string {
  return v === 'no-runtime' ? '精简版' : '完整版'
}

function normalizeVariant(v: string | null | undefined): PaperVariant {
  return v === 'no-runtime' ? 'no-runtime' : 'self-contained'
}

export function createPaperTodoAdapter(): PaperTodoAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // ---------- 模块私有缓存（adapter 内单源；视图经声明式槽位投影） ----------
  let variantPref: PaperVariant = 'self-contained'
  let variantLoad: Promise<PaperVariant> | null = null
  let runtime: RuntimeStatus | null = null
  let runtimeLoad: Promise<void> | null = null
  let installed: PaperVersionInfo | null = null

  /** 偏好懒加载单飞：读失败保留旧默认值并允许下次重试（下载传空由后端 store 兜底）。 */
  function ensureVariant(): Promise<PaperVariant> {
    if (!variantLoad) {
      variantLoad = PaperAPI.GetVariant()
        .then((pref) => (variantPref = normalizeVariant(pref)))
        .catch(() => {
          variantLoad = null
          return variantPref
        })
    }
    return variantLoad
  }

  /** 运行时探测单飞：失败静默为 null（变体注记回退"探测中…"，与迁移前口径一致）。 */
  function loadRuntime(): Promise<void> {
    if (!runtimeLoad) {
      runtimeLoad = PaperAPI.GetRuntimeStatus()
        .then((rt) => {
          runtime = rt ?? null
        })
        .catch(() => {
          runtime = null
        })
        .finally(() => {
          runtimeLoad = null
        })
    }
    return runtimeLoad
  }

  return {
    installedInfo: () => installed,

    async getStatus() {
      const snap = await PaperAPI.GetStatus()
      await loadRuntime() // 探测结果合流进快照扩展字段（首探/重装后共享同一单飞）
      return { ...snap, runtime }
    },

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('papertodo:instance-state', (s) => {
        if (!s) return
        cb({ ...s, runtime })
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('papertodo:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: t.version, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      async listInstalled(): Promise<ManagedVersionRecord[]> {
        const local = await PaperAPI.GetInstalledVersion()
        installed = local && local.version ? local : null
        void loadRuntime() // 运行时探测随版本区刷新重读（对齐迁移前 loadVersions 时点）
        return installed ? [projectInstalled(installed)] : []
      },

      async listReleases(): Promise<ManagedReleaseRecord[]> {
        const variant = await ensureVariant()
        const remote = await PaperAPI.ListReleases()
        return (remote ?? []).map((rel) => ({
          version: rel.version,
          published: rel.published,
          // 单列 size 恒显"将以当前变体下载"的资产体量（变体切换后视图会触发重载）
          size: variant === 'no-runtime' ? (rel.noRuntime?.size ?? 0) : (rel.selfContained?.size ?? 0),
        }))
      },

      async download(rel: ManagedReleaseRecord): Promise<ManagedActionResult> {
        const variant = await ensureVariant()
        const res = await PaperAPI.DownloadVersion(rel.version, variant)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version}（${variantName(variant)}）已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(): Promise<ManagedActionResult> {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留；
        // "卸载保留数据"的承诺在确认框与回执里原样承载
        const hadData = installed?.hasData
        const accepted = await confirm({
          title: '确定卸载 PaperTodo？',
          description: `只删除程序本体与托管元信息——${hadData ? '你的便签数据（data.json、图片库、plugins）将原地保留' : '当前没有便签数据'}。\n下次安装或导入本地副本时数据自动接上。`,
          tone: 'danger',
        })
        if (!accepted) return {}
        await PaperAPI.RemoveInstalled()
        return { message: '已卸载 PaperTodo（便签数据已保留）', reloadVersions: true }
      },

      async importLocal(): Promise<ManagedActionResult> {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地副本',
          label: '请输入本机已有 PaperTodo 的目录完整路径（含 PaperTodo.exe）',
          description: '提示：便签数据（data.json 等）与 exe 同目录，导入时会一起收编进托管目录',
          placeholder: 'C:\\...\\PaperTodo',
        })
        if (!path) return {}
        const info = await PaperAPI.ImportLocal(path.trim())
        return {
          message: `已导入 PaperTodo ${info.version}${info.hasData ? '（便签数据随行）' : ''}`,
          reloadVersions: true,
        }
      },

      async openDir(v: ManagedVersionRecord): Promise<ManagedActionResult> {
        await PaperAPI.OpenDir(v.dir)
        return {}
      },
    },

    control: {
      primary: {
        async run(): Promise<ManagedActionResult> {
          const out = await PaperAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 唤回纸片',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) =>
          state === 'running'
            ? '唤回全部纸片（show 命令）'
            : state === 'starting'
              ? '启动中…'
              : '启动 PaperTodo 并把纸片放上桌面',
      },
      quit: {
        async run(): Promise<ManagedActionResult> {
          const out = await PaperAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) =>
          state === 'external' ? '外部实例请在其托盘/顶栏退出' : '发送 exit 命令优雅退出（宽限后强杀兜底）',
      },
    },

    // 收拢纸片（hide 命令信使）= dismiss 族清态动词 → 契约 reset 槽；
    // 钮位由视图经 #primary-action 既有控制位注入控制台钮区（见文件头③）
    reset: {
      async run(): Promise<ManagedActionResult> {
        const out = await PaperAPI.HidePapers()
        return { message: out.message }
      },
    },

    // variant 槽实战（见文件头）：options 为下载变体声明（label 全句逐字），
    // get 单源缓存、set 成功后同步缓存——versions.download 消费同源偏好
    variant: {
      options: [
        { value: 'self-contained', label: '完整版 self-contained（内嵌 .NET 10 运行时，约 71MB，零依赖）' },
        { value: 'no-runtime', label: '精简版 no-runtime（约 2.4MB，需系统 .NET 10 桌面运行时）' },
      ],
      async get(): Promise<string> {
        return await ensureVariant()
      },
      async set(value: string): Promise<ManagedActionResult> {
        const next = normalizeVariant(value)
        await PaperAPI.SetVariant(next)
        variantPref = next
        variantLoad = Promise.resolve(next) // set 成功即事实，缓存跟进（失败不落缓存）
        // ⑥：变体切换影响远程表 size 列（按变体资产投影）——reloadVersions
        // 回执交 store.runVariant 统一重拉，视图侧 load() 补丁退役
        return {
          message:
            next === 'no-runtime'
              ? '下载变体已切换为精简版（下次下载生效，不追溯已装版本）'
              : '下载变体已切换为完整版（下次下载生效）',
          reloadVersions: true,
        }
      },
    },

    // 条件提示条（三个变体互斥）；tone 对齐 UiBanner 语义（原文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return {
          tone: 'warn',
          text: '检测到外部 PaperTodo 实例（非 Hanxi 托管）。可代为唤回/收拢纸片；退出请在其纸片顶栏或托盘菜单操作。',
        }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'PaperTodo 异常退出' }
      }
      if (s.state === 'running') {
        return {
          tone: 'ok',
          text: 'PaperTodo 正在运行：便签在它自己的桌面纸片上编辑，内容自动保存。便签数据存于托管目录（data.json），升级不丢、卸载保留。',
        }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「唤回纸片」启动，便签直接在桌面纸片上书写。数据存于托管目录，Hanxi 不读取便签内容。'
      }
      if (s.state === 'starting') {
        return '正在拉起 PaperTodo（约 1~3 秒）…'
      }
      return null
    },

    copy: {
      // listInstalled 退化 0/1 的渲染成色补齐：meta 首行后半句携"当前托管 版本·变体"
      // （旧版未装话术逐字保留），方言徽标降级为 hint 行
      remoteSummary: (releaseCount) =>
        `远程版本 ${releaseCount} 个 · ${installed ? `当前托管 ${installed.version} · ${variantName(installed.variant)}` : '尚未安装托管版本'}`,
      // 动态 getter：共享件每次渲染重估（store.installed 变更必然触发），
      // 数据在册徽标与审计指纹行按现状原样透出
      get metaHints(): string[] {
        const lines: string[] = []
        if (installed?.hasData) lines.push('便签数据在册')
        else if (installed) lines.push('当前没有便签数据')
        if (installed?.assetSha256) {
          lines.push(`下载时计算的 sha256（上游未发布官方哈希，此为审计基线）：${installed.assetSha256}`)
        }
        lines.push('单目录覆盖升级：便签数据原地保留；「卸载」只删程序不删数据；回滚旧版 = 在下方重新安装该版本')
        return lines
      },
      firstUseEmpty:
        '尚未安装 PaperTodo —— 下载官方绿色版，或「导入本地副本」把你机器上已有的 PaperTodo 目录（连同便签数据）收编进来',
      remoteUnavailable: '无法加载远程版本列表（GitHub API 不可达）——可稍后点击「↻ 刷新远程列表」重试',
      uninstallRunningHint: '只删程序，便签数据原地保留',
    },

    extras: {
      followOnExit: {
        get: () => PaperAPI.GetFollowOnExit(),
        async set(next): Promise<ManagedActionResult> {
          await PaperAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭便签'
              : '已关闭：Hanxi 退出不影响便签，纸片继续常驻桌面（下次启动生效）',
          }
        },
        label: '随 Hanxi 一起关闭',
        note: '（关闭后 Hanxi 退出不影响便签，纸片继续常驻桌面）',
      },
      shortcut: {
        async create(): Promise<ManagedActionResult> {
          await PaperAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向托管安装）' }
        },
      },
      repo: {
        url: () => PaperAPI.RepositoryURL(),
        async open(): Promise<ManagedActionResult> {
          await PaperAPI.OpenRepository()
          return {}
        },
        copyToast: '仓库地址已复制',
      },
    },
  }
}

/** PaperVersionInfo → 契约记录投影（方言字段留在 installedInfo 钩子里供视图取用）。 */
function projectInstalled(info: PaperVersionInfo): ManagedVersionRecord {
  return {
    version: info.version,
    exePath: info.exePath,
    dir: info.dir,
    size: info.size,
    installedAt: info.installedAt,
    isImport: info.isImport,
    source: info.source,
  }
}
