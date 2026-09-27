// ============================================================================
// BCUninstaller → 托管控制台 adapter（Wave 5 · 批 1 迁移件，模式照抄 ccswitch）
//
// 逐字迁移自 BCUView 原编排段（启停/版本/联动/仓库全部 RPC 与文案零变化）。
// 事件面：`bcu:instance-state` / `bcu:version-download`。
// 波 2E 回迁裁决：meta 行族/已装卡/首用空态三段纯抄全部换面板默认体；
// 双变体远程表为实证超纲方言——每行两把下载钮、version|variant 复合进度键
// （铁律②：防同版本双变体互相覆盖）、按变体独立票行与变体前缀阶段词，面板
// progressKey/statusOf 均为「行→单键」判定（statusOf 仅在无票据时回调且签名
// 不携 downloading map），行级双票聚合不可硬套——表体经面板 #remote-table
// 整表替换槽留视图，词面与判定逐字原形。
// GetDotnetEnvironment 诊断态（.NET 环境徽标与推荐变体）自本波起收编进 adapter
// 闭包：copy.firstUseDownloadLabel 推荐词与 download 缺省变体需同源投影，
// 视图 #meta-extra 徽标读同一 ref；进度票据仍经 store.runDownload 统一回执，
// 视图以 variantRelease() 给行注入 variant。
// ============================================================================

import { computed, ref } from 'vue'
import type { ComputedRef, Ref } from 'vue'
import * as BCUAPI from '../../bindings/hanxi/internal/modules/bcu/bcuservice'
import type { DotnetEnv } from '../../bindings/hanxi/internal/modules/bcu/models'
import type { Snapshot } from '../../bindings/hanxi/internal/modules/bcu/instance/models'
import type { BCURelease, DownloadProgress } from '../../bindings/hanxi/internal/modules/bcu/version/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useConfirm } from '../composables/useConfirm'
import { usePrompt } from '../composables/usePrompt'
import { getErrorMessage } from '../utils/errors'
import type { ManagedModuleAdapter, ManagedVersionRecord } from '../components/managed/adapter'

/** BCU 下载变体（与后端 DownloadVersion 的 variant 词一致）。 */
export type BCUVariant = 'portable' | 'fdd'

/** 变体短词（波 2E 起视图方言表与 copy 首用推荐词族共用单源，免互抄）。 */
export function variantLabel(variant: string): string {
  return variant === 'fdd' ? '精简版' : '便携版'
}

/** 方言远程行：BCURelease 天然超集 ManagedReleaseRecord（version/published/size/isPre 齐备）。 */
export type BCUReleaseRow = BCURelease & { variant?: BCUVariant }

/** 给远程行注入下载变体（store.runDownload 入参构造器，方言表与空态共用；
 *  返回值为方言类型，赋值兼容共享 ManagedReleaseRecord 消费面）。 */
export function variantRelease(rel: BCURelease, variant: BCUVariant): BCUReleaseRow {
  return { ...rel, variant }
}

/** BCU adapter = 共享总面 + 方言诊断扩展。 */
export type BCUAdapter = ManagedModuleAdapter & {
  /** .NET 桌面运行时探测（方言视图：版本 Tab「.NET 环境徽标」与推荐变体输入）。 */
  getDotnetEnv(): PromiseLike<DotnetEnv | null>
  /** 探测结果反应式源（视图 #meta-extra 徽标与 copy 推荐词族同源；null=未加载完毕）。 */
  dotnetEnv: Ref<DotnetEnv | null>
  /** 探测在途位（徽标行「正在检测…」词的输出）。 */
  envLoading: Ref<boolean>
  /** 推荐变体：有 .NET 8 桌面运行时→精简版（省 ~60MB），无→自包含便携版；null=未加载完毕。 */
  recommendedVariant: ComputedRef<BCUVariant | null>
  /** 一次性环境探测（视图 mounted 触发；失败静默留空不拦版本区）。 */
  loadDotnetEnv(): Promise<void>
}

export function createBCUAdapter(): BCUAdapter {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // .NET 环境诊断态（波 2E 收编 adapter 闭包）：面板 copy 的推荐词族与 download
  // 缺省变体、视图 #meta-extra 徽标三处同源，视图不再自持状态。
  const dotnetEnv = ref<DotnetEnv | null>(null)
  const envLoading = ref(false)
  const recommendedVariant = computed<BCUVariant | null>(() => {
    if (!dotnetEnv.value) return null
    return dotnetEnv.value.hasNet8 ? 'fdd' : 'portable'
  })
  async function loadDotnetEnv(): Promise<void> {
    envLoading.value = true
    try {
      dotnetEnv.value = (await BCUAPI.GetDotnetEnvironment()) ?? null
    } catch (e) {
      console.warn('bcu GetDotnetEnvironment failed:', getErrorMessage(e))
    } finally {
      envLoading.value = false
    }
  }

  return {
    getStatus: () => BCUAPI.GetStatus(),

    subscribeInstanceState: (cb) => {
      useWailsEvent<Snapshot>('bcu:instance-state', (s) => {
        if (!s) return
        cb(s)
      })
    },

    subscribeProgress: (cb) => {
      useWailsEvent<DownloadProgress>('bcu:version-download', (t) => {
        if (!t || !t.version) return
        cb({ key: `${t.version}|${t.variant || 'portable'}`, stage: t.stage, done: t.done, total: t.total, message: t.message })
      })
    },

    versions: {
      listInstalled: () => BCUAPI.ListInstalledVersions(),
      listReleases: () => BCUAPI.ListReleases(),
      getActive: () => BCUAPI.GetActiveVersion(),

      async setActive(version) {
        const ver = await BCUAPI.SetActiveVersion(version)
        return { message: `已将 ${ver} 设为使用版本`, activeVersion: ver }
      },

      async download(rel) {
        // variant 由方言视图经 variantRelease() 注入；面板首用一键下载无注入位，
        // 缺省回落推荐变体（环境未测毕回自包含便携版——与原方言空态双分支逐字同语义）
        const variant = (rel as BCUReleaseRow).variant ?? recommendedVariant.value ?? 'portable'
        const res = await BCUAPI.DownloadVersion(rel.version, variant)
        if (res === 'already-installed') {
          return { message: `版本 ${rel.version} 已安装`, reloadVersions: true }
        }
        return {}
      },

      async remove(v: ManagedVersionRecord) {
        // 危险操作经全局可访问确认框（useConfirm 单例），文案逐字保留
        const ok = await confirm({
          title: `确定卸载 BCU ${v.version}？`,
          description: '该版本隔离目录（含卸载历史与设置数据）将被删除，不可恢复。',
          tone: 'danger',
        })
        if (!ok) return {}
        await BCUAPI.RemoveVersion(v.version)
        return { message: `已卸载 ${v.version}`, reloadVersions: true }
      },

      async importLocal() {
        // 路径输入经全局输入框（usePrompt 单例），提示文案逐字保留
        const path = await prompt({
          title: '导入本地安装',
          label: '请输入 BCU 便携目录完整路径（含 BCUninstaller.exe）',
          description: '将整套迁入：exe + 运行时 + BCUninstaller.settings 卸载历史均保留',
          placeholder: 'C:\\Program Files\\BCUninstaller',
        })
        if (!path) return {}
        const info = await BCUAPI.ImportLocal(path.trim())
        return { message: `已导入 BCU ${info.version}（设置与数据一并迁移）`, reloadVersions: true }
      },

      async openDir(v: ManagedVersionRecord) {
        await BCUAPI.OpenDir(v.dir)
        return {}
      },

      // 铁律②复合键反查（波 2E 起同时供面板首用一键下载取票）：与 download 的
      // 变体解析严格同源——行无注入 variant 时回推荐变体再回便携版，保证
      // 「预挂票据→事件回流→done 清票」全程同键不永挂（方言表按变体直读，
      // 本位缺省口径即其 `${version}|${variant}` 键形）。
      progressKey: (rel) => `${rel.version}|${(rel as BCUReleaseRow).variant ?? recommendedVariant.value ?? 'portable'}`,
    },

    control: {
      primary: {
        async run() {
          const out = await BCUAPI.OpenWindow()
          return { message: out.message }
        },
        label: '🗔 打开窗口',
        cssClass: 'btn-secondary',
        disabledFor: (state) => state === 'starting',
        titleFor: (state) => (state === 'running' ? '唤起窗口' : state === 'starting' ? '启动中…' : '启动 BCU 并打开主窗口'),
      },
      quit: {
        async run() {
          const out = await BCUAPI.Quit()
          return { message: out.message }
        },
        label: '⏻ 退出',
        disabledFor: (state) => state !== 'running' && state !== 'starting' && state !== 'external',
        titleFor: (state) => (state === 'external' ? '外部实例请在 BCU 窗口内关闭' : '关闭窗口消息（挂起时强杀兜底）'),
      },
    },

    // 条件提示条（三个变体互斥；文案逐字保留）
    banner: (s) => {
      if (s.state === 'external') {
        return { tone: 'warn', text: '检测到外部 BCUninstaller 实例（非 Hanxi 托管）。可唤起其窗口；如需彻底退出请在 BCU 窗口内关闭。' }
      }
      if (s.state === 'failed') {
        return { tone: 'error', text: s.error || 'BCU 异常退出' }
      }
      if (s.state === 'running') {
        return { tone: 'ok', text: 'BCU 正在运行：批量卸载在其窗口内完成，设置数据（BCUninstaller.settings）保存在版本目录内。' }
      }
      return null
    },

    hint: (s) => {
      if (s.state === 'stopped') {
        return '尚未运行：点击「打开窗口」启动 BCU，批量卸载在其窗口内完成。便携包自含 .NET 运行时（约 76MB），无需系统预装。BCU 上游 manifest 强制管理员权限：需以管理员身份运行 Hanxi 方可启动（否则系统直拒，不代弹 UAC）。'
      }
      if (s.state === 'starting') {
        return '正在拉起 BCU（自包含 .NET 首次启动约 3~10 秒）…'
      }
      return null
    },

    // 波 2E：meta 行族/已装卡/首用空态回迁面板默认体，原方言区纯抄词面逐字入本表。
    // 导入钮/刷新钮/区节标题/「使用中」等恰为面板缺省词，不落覆写位。
    copy: {
      metaHints: [
        '两种形态：自包含便携版（内嵌 .NET 运行时，76MB）+ 精简版（框架依赖，12MB，需本机 .NET 8 桌面运行时）——均经官方 digest 四层校验',
        '2024 年末前的旧版本无官方哈希不入列表（完整性第一优先）；6.1 起资产版本号与 tag 对齐校验防串版',
      ],
      firstUseEmpty: '尚未安装 BCU —— 下载官方便携版，或「导入本地安装」把现有 BCU 收纳进来',
      // 推荐变体已测毕走「（便携版/精简版，推荐）」词形，未测毕回朴素词
      // （原方言空态 v-if/v-else-if 双分支词面逐字对位）
      firstUseDownloadLabel: (rel) =>
        recommendedVariant.value
          ? `下载最新版 ${rel.version}（${variantLabel(recommendedVariant.value)}，推荐）`
          : `下载最新版 ${rel.version}`,
      uninstallRunningHint: '请先退出 BCU',
    },

    extras: {
      followOnExit: {
        get: () => BCUAPI.GetFollowOnExit(),
        async set(next) {
          await BCUAPI.SetFollowOnExit(next)
          return {
            message: next
              ? '已开启：Hanxi 退出时一并关闭该工具'
              : '已关闭：Hanxi 退出不影响该工具，继续独立运行（下次启动生效）',
          }
        },
      },
      shortcut: {
        async create() {
          await BCUAPI.CreateDesktopShortcut()
          return { message: '桌面快捷方式已创建（指向当前使用版本）' }
        },
      },
      repo: {
        url: () => BCUAPI.RepositoryURL(),
        async open() {
          await BCUAPI.OpenRepository()
          return {}
        },
      },
    },

    // ---- 方言扩展（见文件头注记） ----
    getDotnetEnv: () => BCUAPI.GetDotnetEnvironment(),
    dotnetEnv,
    envLoading,
    recommendedVariant,
    loadDotnetEnv,
  }
}
